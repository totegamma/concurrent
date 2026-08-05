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

var repairTimelineSchemasDryRun bool

var repairTimelineSchemasCmd = &cobra.Command{
	Use:   "repair-timeline-schemas",
	Short: "Normalize migrated profile-timeline schemas to t/user.json",
	Long: "v1 clients created profile timelines under t/empty.json (and subprofile homes under\n" +
		"t/subprofile.json), and migrate-v1-to-v2 used to carry those schemas over verbatim,\n" +
		"while native v2 clients create the same timelines as t/user.json. Clients that filter\n" +
		"by schema therefore never recognize a migrated user timeline. This recomputes the\n" +
		"affected home/notify/activity timeline records — and the list reference records that\n" +
		"copied the broken schema — under t/user.json. Changing the document changes its\n" +
		"content-hash CDID, so each commit log is re-inserted under its new id and every\n" +
		"referrer repointed. CreatedAt is preserved, so any later re-commit by the owner still\n" +
		"wins the accept-if-newer comparison.\n" +
		"Reads and writes Postgres directly; no running server required. Re-running is a no-op.",
	Args: cobra.NoArgs,
	RunE: withOperationContext(func(cmd *cobra.Command, args []string, op *operationContext) error {
		ctx := cmd.Context()

		brokenSchemas := []string{
			"https://schema.concrnt.world/t/empty.json",
			"https://schema.concrnt.world/t/subprofile.json",
		}
		const fixedSchema = "https://schema.concrnt.world/t/user.json"

		var records []models.Record
		if err := op.DB.WithContext(ctx).Preload("Document").Where("schema IN ?", brokenSchemas).Find(&records).Error; err != nil {
			return fmt.Errorf("failed to query records: %w", err)
		}

		repaired := 0
		for _, record := range records {
			oldID := record.DocumentID
			if record.Document.ID == "" {
				fmt.Fprintf(os.Stderr, "skipping %s: commit log not found\n", oldID)
				continue
			}

			var rk models.RecordKey
			if err := op.DB.WithContext(ctx).Where("record_id = ?", oldID).First(&rk).Error; err != nil {
				fmt.Fprintf(os.Stderr, "skipping %s: record key not found: %v\n", oldID, err)
				continue
			}

			var doc concrnt.Document[any]
			if err := json.Unmarshal([]byte(record.Document.Document), &doc); err != nil {
				fmt.Fprintf(os.Stderr, "skipping %s: failed to parse document: %v\n", rk.URI, err)
				continue
			}

			// A reference record's effective schema lives in value.schema and its
			// timeline-ness is judged by the href it points at; a timeline record is
			// judged and rewritten on the document itself.
			target := rk.URI
			var refValue map[string]any
			if doc.Schema == schemas.ReferenceURL {
				var ok bool
				refValue, ok = doc.Value.(map[string]any)
				if !ok {
					fmt.Fprintf(os.Stderr, "skipping %s: malformed reference value\n", rk.URI)
					continue
				}
				target, _ = refValue["href"].(string)
			}
			if !strings.HasSuffix(target, "/home-timeline") &&
				!strings.HasSuffix(target, "/notify-timeline") &&
				!strings.HasSuffix(target, "/activity-timeline") {
				fmt.Fprintf(os.Stderr, "skipping %s: not a profile timeline\n", rk.URI)
				continue
			}
			if refValue != nil {
				refValue["schema"] = fixedSchema
			} else {
				doc.Schema = fixedSchema
			}

			newBytes, err := json.Marshal(doc)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skipping %s: failed to serialize document: %v\n", rk.URI, err)
				continue
			}
			hash := concrnt.GetHash(newBytes)
			var hash10 [10]byte
			copy(hash10[:], hash[:10])
			newID := cdid.New(hash10, doc.CreatedAt).String()

			if newID == oldID {
				continue
			}

			repaired++
			fmt.Printf("%s: %s -> %s\n", rk.URI, oldID, newID)
			if repairTimelineSchemasDryRun {
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
					Schema:        fixedSchema,
					Policies:      record.Policies,
					Distributions: record.Distributions,
					CreatedAt:     record.CreatedAt,
					CDate:         record.CDate,
				}).Error; err != nil {
					return err
				}
				if err := tx.Model(&models.RecordKey{}).
					Where("id = ? AND record_id = ?", rk.ID, oldID).
					Update("record_id", newID).Error; err != nil {
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

		action := "repaired"
		if repairTimelineSchemasDryRun {
			action = "would repair"
		}
		fmt.Fprintf(os.Stderr, "%s %d of %d records\n", action, repaired, len(records))
		return nil
	}),
}

func init() {
	operationCmd.AddCommand(repairTimelineSchemasCmd)

	repairTimelineSchemasCmd.Flags().BoolVar(&repairTimelineSchemasDryRun, "dry-run", false, "Only list the records that would be repaired")
}
