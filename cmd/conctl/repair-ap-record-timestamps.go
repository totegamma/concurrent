package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var repairApRecordTimestampsDryRun bool

var repairApRecordTimestampsCmd = &cobra.Command{
	Use:   "repair-ap-record-timestamps",
	Short: "Uniquify created_at of migrated ActivityPub follow records",
	Long: "migrate ap-v1-to-v2 used to write every follow/follower/accept-state record with one\n" +
		"shared time.Now(). The query API pages on created_at alone (next = the peeked row's\n" +
		"timestamp, since is a closed interval, page size caps at 100), so the cursor can\n" +
		"never advance past that shared timestamp and the v2 bridge's startup load silently\n" +
		"stops after the first page — follows show as pending and follower lists come up\n" +
		"near-empty even though the records exist.\n" +
		"This spreads colliding created_at values under the " + apNamespace + " namespace\n" +
		"1µs apart in document_id order (the query API's tie-break, so the previous\n" +
		"pagination order is preserved; the first row keeps its timestamp) and re-syncs the\n" +
		"record_keys.record_created_at mirror column. Only the database sort columns move:\n" +
		"documents, their IDs, proofs and commit logs are untouched, so no events fire and\n" +
		"accept-if-newer comparisons are unaffected.\n" +
		"Reads and writes Postgres directly; no running server required. Re-running is a\n" +
		"no-op. Restart the ActivityPub bridge afterwards so it reloads the records.",
	Args: cobra.NoArgs,
	RunE: withOperationContext(func(cmd *cobra.Command, args []string, op *operationContext) error {
		ctx := cmd.Context()

		apKeyPattern := "cckv://%/" + apNamespace + "/%"

		var groups []struct {
			CreatedAt time.Time
			Count     int
		}
		err := op.DB.WithContext(ctx).
			Raw(`SELECT r.created_at, COUNT(*) AS count
				FROM records r JOIN record_keys rk ON rk.record_id = r.document_id
				WHERE rk.uri LIKE ?
				GROUP BY r.created_at HAVING COUNT(*) >= 2 ORDER BY r.created_at`,
				apKeyPattern).
			Scan(&groups).Error
		if err != nil {
			return fmt.Errorf("failed to scan for created_at collisions: %w", err)
		}

		if len(groups) == 0 {
			fmt.Println("no created_at collisions found")
			return nil
		}

		toMove := 0
		for _, g := range groups {
			fmt.Printf("%s: %d records\n", g.CreatedAt.Format(time.RFC3339Nano), g.Count)
			toMove += g.Count - 1
		}

		if repairApRecordTimestampsDryRun {
			fmt.Printf("[dry-run] would move %d records in %d collision groups\n", toMove, len(groups))
			return nil
		}

		result := op.DB.WithContext(ctx).Exec(`
			WITH ap AS (
				SELECT r.document_id, r.created_at
				FROM records r JOIN record_keys rk ON rk.record_id = r.document_id
				WHERE rk.uri LIKE ?
			), spread AS (
				SELECT document_id,
				       created_at + (ROW_NUMBER() OVER (PARTITION BY created_at ORDER BY document_id) - 1) * interval '1 microsecond' AS new_created_at
				FROM ap
				WHERE created_at IN (SELECT created_at FROM ap GROUP BY created_at HAVING COUNT(*) >= 2)
			), moved AS (
				UPDATE records r
				SET created_at = s.new_created_at
				FROM spread s
				WHERE r.document_id = s.document_id AND r.created_at <> s.new_created_at
				RETURNING r.document_id, r.created_at
			)
			UPDATE record_keys rk
			SET record_created_at = m.created_at
			FROM moved m
			WHERE rk.record_id = m.document_id`,
			apKeyPattern)
		if result.Error != nil {
			return fmt.Errorf("failed to repair created_at collisions: %w", result.Error)
		}

		fmt.Printf("moved %d records in %d collision groups (%d record keys re-synced)\n", toMove, len(groups), result.RowsAffected)
		return nil
	}),
}

func init() {
	operationCmd.AddCommand(repairApRecordTimestampsCmd)
	repairApRecordTimestampsCmd.Flags().BoolVar(&repairApRecordTimestampsDryRun, "dry-run", false, "Only list the collision groups that would be repaired")
}
