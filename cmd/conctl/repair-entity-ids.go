package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/cdid"
	"github.com/concrnt/concrnt/internal/infra/database/models"
)

var repairEntityIDsDryRun bool

var repairEntityIDsCmd = &cobra.Command{
	Use:   "repair-entity-ids",
	Short: "Recompute entity document ids stored under a wrong CDID",
	Long: "Recomputes each entity's document id from its stored commit-log document and rewrites\n" +
		"the rows where the stored id doesn't match. Entities imported by migrate-v1-to-v2 before\n" +
		"the CreatedAt fix carry a zero createdAt whose CDID wrapped around into the far future,\n" +
		"so every legitimate re-commit of the entity loses the accept-if-newer comparison and is\n" +
		"silently discarded. The repaired id (zero time now clamps to the CDID minimum) loses to\n" +
		"any real commit, so affected accounts recover organically the next time their client\n" +
		"re-signs the entity. The documents and proofs themselves are left untouched.\n" +
		"Reads and writes Postgres directly; no running server required. Re-running is a no-op.",
	Args: cobra.NoArgs,
	RunE: withOperationContext(func(cmd *cobra.Command, args []string, op *operationContext) error {
		ctx := cmd.Context()

		var entities []models.Entity
		if err := op.DB.WithContext(ctx).Preload("Document").Find(&entities).Error; err != nil {
			return fmt.Errorf("failed to query entities: %w", err)
		}

		repaired := 0
		for _, entity := range entities {
			if entity.Document.ID == "" {
				fmt.Fprintf(os.Stderr, "skipping %s: commit log %s not found\n", entity.ID, entity.DocumentID)
				continue
			}

			var doc concrnt.Document[any]
			if err := json.Unmarshal([]byte(entity.Document.Document), &doc); err != nil {
				fmt.Fprintf(os.Stderr, "skipping %s: failed to parse document %s: %v\n", entity.ID, entity.DocumentID, err)
				continue
			}

			hash := concrnt.GetHash([]byte(entity.Document.Document))
			var hash10 [10]byte
			copy(hash10[:], hash[:10])
			newID := cdid.New(hash10, doc.CreatedAt).String()

			if newID == entity.DocumentID {
				continue
			}

			repaired++
			fmt.Printf("%s: %s -> %s\n", entity.ID, entity.DocumentID, newID)
			if repairEntityIDsDryRun {
				continue
			}

			// entities.document_id references commit_logs.id with ON DELETE
			// CASCADE, so the commit log must be re-inserted under the new id and
			// every referrer repointed before the old row can go — deleting it
			// first would cascade the entity row away.
			err := op.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				oldLog := entity.Document
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.CommitLog{
					ID:          newID,
					IP:          oldLog.IP,
					Document:    oldLog.Document,
					Proof:       oldLog.Proof,
					GcCandidate: oldLog.GcCandidate,
					CDate:       oldLog.CDate,
				}).Error; err != nil {
					return err
				}
				if err := tx.Model(&models.CommitOwner{}).
					Where("commit_log_id = ?", entity.DocumentID).
					Update("commit_log_id", newID).Error; err != nil {
					return err
				}
				if err := tx.Model(&models.Entity{}).
					Where("id = ? AND document_id = ?", entity.ID, entity.DocumentID).
					Update("document_id", newID).Error; err != nil {
					return err
				}
				return tx.Delete(&models.CommitLog{}, "id = ?", entity.DocumentID).Error
			})
			if err != nil {
				return fmt.Errorf("failed to repair entity %s: %w", entity.ID, err)
			}
		}

		action := "repaired"
		if repairEntityIDsDryRun {
			action = "would repair"
		}
		fmt.Fprintf(os.Stderr, "%s %d of %d entities\n", action, repaired, len(entities))
		return nil
	}),
}

func init() {
	operationCmd.AddCommand(repairEntityIDsCmd)

	repairEntityIDsCmd.Flags().BoolVar(&repairEntityIDsDryRun, "dry-run", false, "Only list the entities that would be repaired")
}
