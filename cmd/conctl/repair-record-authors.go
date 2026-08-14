package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/infra/database/models"
)

var repairRecordAuthorsDryRun bool

var repairRecordAuthorsCmd = &cobra.Command{
	Use:   "repair-record-authors",
	Short: "Backfill records.author from the stored documents",
	Long: "records gained an author column (the document's author, enabling the query API's\n" +
		"author filter) after existing rows were written, so rows created by older server\n" +
		"versions have no author. This reads each such row's commit log and copies the\n" +
		"document's author field onto the record. The document itself is unchanged, so ids\n" +
		"stay stable and no rows are rewritten. Run after deploying the server version that\n" +
		"adds the column (server startup creates it via AutoMigrate).\n" +
		"Reads and writes Postgres directly; no running server required. Re-running is a no-op.",
	Args: cobra.NoArgs,
	RunE: withOperationContext(func(cmd *cobra.Command, args []string, op *operationContext) error {
		ctx := cmd.Context()

		repaired := 0
		total := 0
		var records []models.Record
		// Batching keeps the preload's IN list under the 65535-parameter
		// protocol limit.
		err := op.DB.WithContext(ctx).
			Preload("Document").
			Where("author IS NULL OR author = ''").
			FindInBatches(&records, 500, func(_ *gorm.DB, _ int) error {
				total += len(records)
				for _, record := range records {
					if record.Document.ID == "" {
						fmt.Fprintf(os.Stderr, "skipping %s: commit log not found\n", record.DocumentID)
						continue
					}

					var doc concrnt.Document[any]
					if err := json.Unmarshal([]byte(record.Document.Document), &doc); err != nil {
						fmt.Fprintf(os.Stderr, "skipping %s: failed to parse document: %v\n", record.DocumentID, err)
						continue
					}
					if doc.Author == "" {
						fmt.Fprintf(os.Stderr, "skipping %s: document has no author\n", record.DocumentID)
						continue
					}

					repaired++
					fmt.Printf("%s: author -> %s\n", record.DocumentID, doc.Author)
					if repairRecordAuthorsDryRun {
						continue
					}

					if err := op.DB.WithContext(ctx).Model(&models.Record{}).
						Where("document_id = ?", record.DocumentID).
						Update("author", doc.Author).Error; err != nil {
						return fmt.Errorf("failed to repair %s: %w", record.DocumentID, err)
					}
				}
				return nil
			}).Error
		if err != nil {
			return fmt.Errorf("failed to query records: %w", err)
		}

		action := "repaired"
		if repairRecordAuthorsDryRun {
			action = "would repair"
		}
		fmt.Fprintf(os.Stderr, "%s %d of %d records\n", action, repaired, total)
		return nil
	}),
}

func init() {
	operationCmd.AddCommand(repairRecordAuthorsCmd)

	repairRecordAuthorsCmd.Flags().BoolVar(&repairRecordAuthorsDryRun, "dry-run", false, "Only list the records that would be repaired")
}
