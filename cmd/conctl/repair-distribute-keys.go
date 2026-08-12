package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/cdid"
	"github.com/concrnt/concrnt/internal/infra/database/models"
	"github.com/concrnt/concrnt/schemas"
)

var repairDistributeKeysDryRun bool

var repairDistributeKeysCmd = &cobra.Command{
	Use:   "repair-distribute-keys",
	Short: "Rekey auto-generated distribute references to hash-based CDID segments",
	Long: "Distribute references (CIP-7) used to be keyed <destination>/<time-based CDID of the\n" +
		"original document>, so every accept-if-newer overwrite of a record left an extra\n" +
		"reference row behind. The key segment is now the hash-based CDID of the reference's\n" +
		"href, which is stable across overwrites, and the delete sweep only looks keys up under\n" +
		"the new rule — rows still keyed under the old rule would never be swept. This rewrites\n" +
		"each old-rule row (document-reference proof, 26-char time-CDID key segment) to its\n" +
		"new-rule key, recomputing the document id since the key is part of the document. If a\n" +
		"new-rule row already exists for the same key (a post-deploy overwrite recreated it),\n" +
		"the old-rule row is simply dropped in its favor.\n" +
		"Reads and writes Postgres directly; no running server required. Re-running is a no-op.",
	Args: cobra.NoArgs,
	RunE: withOperationContext(func(cmd *cobra.Command, args []string, op *operationContext) error {
		ctx := cmd.Context()

		// reference records are stored with redirect = href and records.schema
		// rewritten to the target's schema, so redirect is the discriminator
		// here; the document's own schema is checked below. The key-segment
		// regex is the old rule (the original document's 26-char time-based
		// CDID — the CDID alphabet, so no i/l/o/x); new-rule hash-CDID rows and
		// arbitrary user keys fall out already in SQL. Batching keeps the
		// preloads' IN lists under the 65535-parameter protocol limit.
		repaired := 0
		total := 0
		var keys []models.RecordKey
		err := op.DB.WithContext(ctx).
			Preload("Record").Preload("Record.Document").
			Joins("JOIN records ON records.document_id = record_keys.record_id").
			Where("records.redirect IS NOT NULL").
			Where(`record_keys.uri ~ '/[0-9a-hjkmnp-wyz]{26}$'`).
			FindInBatches(&keys, 500, func(_ *gorm.DB, _ int) error {
				total += len(keys)
				for _, rk := range keys {
					slash := strings.LastIndex(rk.URI, "/")
					if slash < 0 {
						continue
					}
					dest, segment := rk.URI[:slash], rk.URI[slash+1:]
					if len(segment) != 26 || !cdid.IsTimeCDID(segment) {
						continue
					}
					record := rk.Record
					oldID := record.DocumentID
					if record.Document.ID == "" {
						fmt.Fprintf(os.Stderr, "skipping %s: commit log not found\n", rk.URI)
						continue
					}

					var proof concrnt.Proof
					if err := json.Unmarshal([]byte(record.Document.Proof), &proof); err != nil ||
						proof.Type != concrnt.ProofTypeDocumentReference {
						continue
					}

					var doc concrnt.Document[any]
					if err := json.Unmarshal([]byte(record.Document.Document), &doc); err != nil {
						fmt.Fprintf(os.Stderr, "skipping %s: failed to parse document: %v\n", rk.URI, err)
						continue
					}
					if doc.Schema != schemas.ReferenceURL {
						continue
					}
					refValue, ok := doc.Value.(map[string]any)
					if !ok {
						fmt.Fprintf(os.Stderr, "skipping %s: malformed reference value\n", rk.URI)
						continue
					}
					href, _ := refValue["href"].(string)
					if href == "" {
						fmt.Fprintf(os.Stderr, "skipping %s: reference has no href\n", rk.URI)
						continue
					}

					newURI := dest + "/" + cdid.MakeHash([]byte(href)).String()
					doc.Key = newURI
					newBytes, err := json.Marshal(doc)
					if err != nil {
						fmt.Fprintf(os.Stderr, "skipping %s: failed to serialize document: %v\n", rk.URI, err)
						continue
					}
					hash := concrnt.GetHash(newBytes)
					var hash10 [10]byte
					copy(hash10[:], hash[:10])
					newID := cdid.New(hash10, doc.CreatedAt).String()

					// the stored document already carries the new-rule key (only the
					// record_keys row is stale — half-applied runs end up here): the
					// document id is unchanged, so just move the key and never let the
					// cleanup below delete the commit log it would share with the new id
					if newID == oldID {
						repaired++
						fmt.Printf("%s -> %s (key only)\n", rk.URI, newURI)
						if repairDistributeKeysDryRun {
							continue
						}
						if err := op.DB.WithContext(ctx).Model(&models.RecordKey{}).
							Where("id = ? AND record_id = ?", rk.ID, oldID).
							Update("uri", newURI).Error; err != nil {
							return fmt.Errorf("failed to repair %s: %w", rk.URI, err)
						}
						continue
					}

					var existing int64
					if err := op.DB.WithContext(ctx).Model(&models.RecordKey{}).
						Where("uri = ?", newURI).Count(&existing).Error; err != nil {
						return fmt.Errorf("failed to check for existing key %s: %w", newURI, err)
					}

					repaired++
					if existing > 0 {
						// a post-deploy overwrite already distributed under the new
						// rule; that row is by construction the newer one, so the
						// old-rule row just goes away (commit-log cascade takes the
						// record and record key with it)
						fmt.Printf("%s: superseded by existing %s\n", rk.URI, newURI)
						if repairDistributeKeysDryRun {
							continue
						}
						if err := op.DB.WithContext(ctx).Delete(&models.CommitLog{}, "id = ?", oldID).Error; err != nil {
							return fmt.Errorf("failed to drop superseded %s: %w", rk.URI, err)
						}
						continue
					}

					fmt.Printf("%s -> %s (%s -> %s)\n", rk.URI, newURI, oldID, newID)
					if repairDistributeKeysDryRun {
						continue
					}

					// records.document_id references commit_logs.id and record_keys.record_id
					// references records.document_id, both with ON DELETE CASCADE, so the new
					// rows must exist and every referrer be repointed before the old rows can
					// go — deleting first would cascade the record key away.
					err = op.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
						oldLog := record.Document
						if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.CommitLog{
							ID:          newID,
							IP:          oldLog.IP,
							Document:    string(newBytes),
							Proof:       oldLog.Proof,
							GcCandidate: oldLog.GcCandidate,
							CDate:       oldLog.CDate,
						}).Error; err != nil {
							return err
						}
						if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.Record{
							DocumentID:    newID,
							Owner:         record.Owner,
							Redirect:      record.Redirect,
							Schema:        record.Schema,
							Policies:      record.Policies,
							Distributions: record.Distributions,
							CreatedAt:     record.CreatedAt,
							CDate:         record.CDate,
						}).Error; err != nil {
							return err
						}
						if err := tx.Model(&models.RecordKey{}).
							Where("id = ? AND record_id = ?", rk.ID, oldID).
							Updates(map[string]any{"uri": newURI, "record_id": newID}).Error; err != nil {
							return err
						}
						if err := tx.Model(&models.CommitOwner{}).
							Where("commit_log_id = ?", oldID).
							Update("commit_log_id", newID).Error; err != nil {
							return err
						}
						if err := tx.Delete(&models.Record{}, "document_id = ?", oldID).Error; err != nil {
							return err
						}
						return tx.Delete(&models.CommitLog{}, "id = ?", oldID).Error
					})
					if err != nil {
						return fmt.Errorf("failed to repair %s: %w", rk.URI, err)
					}
				}
				return nil
			}).Error
		if err != nil {
			return fmt.Errorf("failed to repair reference record keys: %w", err)
		}

		action := "repaired"
		if repairDistributeKeysDryRun {
			action = "would repair"
		}
		fmt.Fprintf(os.Stderr, "%s %d of %d candidate reference records\n", action, repaired, total)
		return nil
	}),
}

func init() {
	operationCmd.AddCommand(repairDistributeKeysCmd)

	repairDistributeKeysCmd.Flags().BoolVar(&repairDistributeKeysDryRun, "dry-run", false, "Only list the record keys that would be repaired")
}
