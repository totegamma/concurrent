package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/concrnt/concrnt/cdid"
	"github.com/concrnt/concrnt/internal/domain"
)

var (
	gcCommitlogDryRun    bool
	gcCommitlogRetention time.Duration
)

var gcCommitlogCmd = &cobra.Command{
	Use:   "gc-commitlog",
	Short: "Delete GC-flagged commit logs older than the replay window",
	Long: "When a record key is overwritten, the superseded document's commit log is kept\n" +
		"with gc_candidate set: it acts as a replay tombstone, making a re-commit of the\n" +
		"captured old document a no-op. Unregister (account deletion) likewise flags\n" +
		"every commit log owned by the departing user. Once the document's\n" +
		"createdAt has fallen out of the backdate window a replay is rejected as too\n" +
		"old anyway, so the tombstone is redundant and the row can be deleted\n" +
		"(any remaining record/association/entity rows cascade with it — for unregistered\n" +
		"users this is what actually removes their data; ack state rows only lose the\n" +
		"anchor pointing at the deleted commit, and are removed here once their last\n" +
		"anchor goes).\n" +
		"This deletes every gc_candidate commit log whose document createdAt is older\n" +
		"than now minus --retention. Retention below the backdate window would reopen\n" +
		"the replay hole, so shorter values are refused.\n" +
		"Reads and writes Postgres directly; safe to run against a live server. Re-running is a no-op.",
	Args: cobra.NoArgs,
	RunE: withOperationContext(func(cmd *cobra.Command, args []string, op *operationContext) error {
		ctx := cmd.Context()

		if gcCommitlogRetention < domain.MaxBackdate {
			return fmt.Errorf("retention %s is shorter than the backdate window %s: a captured document could be replayed after its tombstone is gone", gcCommitlogRetention, domain.MaxBackdate)
		}

		// Commit log ids are time-prefixed CDIDs of the document's author-signed
		// createdAt, so a lexicographic bound on id is a bound on createdAt; the
		// zero data suffix sorts below every real id in the cutoff millisecond,
		// keeping the match strictly older-than. Hash-based ids ('x'-prefixed)
		// sort above any realistic time prefix and are never gc candidates.
		cutoff := cdid.New([10]byte{}, time.Now().Add(-gcCommitlogRetention)).String()

		if gcCommitlogDryRun {
			var count int64
			err := op.DB.WithContext(ctx).
				Raw("SELECT count(*) FROM commit_logs WHERE gc_candidate AND id < ?", cutoff).
				Scan(&count).Error
			if err != nil {
				return fmt.Errorf("failed to count commit logs: %w", err)
			}
			fmt.Fprintf(os.Stderr, "would delete %d commit logs\n", count)
			return nil
		}

		var total, totalAcks int64
		for {
			// Postgres has no DELETE ... LIMIT; batching via an id list keeps
			// each statement (and its cascades) bounded.
			var doomed []string
			err := op.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				doomed = nil
				if err := tx.Raw(
					"SELECT id FROM commit_logs WHERE gc_candidate AND id < ? LIMIT 1000", cutoff,
				).Scan(&doomed).Error; err != nil {
					return err
				}
				if len(doomed) == 0 {
					return nil
				}
				// The ack anchors are ON DELETE SET NULL (one side's GC must
				// leave the other side's holding intact — see models.Ack), so a
				// row losing its LAST anchor in this batch is removed explicitly
				// here. Rows with both anchors already NULL are untouched: that
				// is pre-backfill state or a mirror still in flight, not this
				// batch's doing.
				res := tx.Exec(`DELETE FROM acks
					WHERE (ack_commit_id IN ? OR acked_commit_id IN ?)
					  AND (ack_commit_id IS NULL OR ack_commit_id IN ?)
					  AND (acked_commit_id IS NULL OR acked_commit_id IN ?)`,
					doomed, doomed, doomed, doomed)
				if res.Error != nil {
					return res.Error
				}
				totalAcks += res.RowsAffected
				res = tx.Exec("DELETE FROM commit_logs WHERE id IN ?", doomed)
				if res.Error != nil {
					return res.Error
				}
				total += res.RowsAffected
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to delete commit logs: %w", err)
			}
			if len(doomed) == 0 {
				break
			}
			fmt.Fprintf(os.Stderr, "deleted %d commit logs so far...\n", total)
		}
		fmt.Fprintf(os.Stderr, "deleted %d commit logs and %d fully-unanchored ack rows\n", total, totalAcks)
		return nil
	}),
}

func init() {
	operationCmd.AddCommand(gcCommitlogCmd)

	gcCommitlogCmd.Flags().BoolVar(&gcCommitlogDryRun, "dry-run", false, "Only count the commit logs that would be deleted")
	gcCommitlogCmd.Flags().DurationVar(&gcCommitlogRetention, "retention", domain.MaxBackdate, "How far back to keep GC-flagged commit logs; must be at least the backdate window")
}
