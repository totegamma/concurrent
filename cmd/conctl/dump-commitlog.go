package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/cdid"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/infra/database/models"
)

var (
	dumpCommitlogOutput  string
	dumpCommitlogSince   string
	dumpCommitlogSinceID string
	dumpCommitlogUntil   string
	dumpCommitlogUntilID string
	dumpCommitlogOwner   string
)

const dumpCommitlogPageSize = 1000

// metaLine is the dump representation of an entity_meta row. It is emitted as a
// distinct JSONL line ({"meta":{...}}) so import can restore local-entity
// registration state (inviter/info) — which lives outside the commit log —
// before replaying the entity commits that depend on it.
type metaLine struct {
	Meta *domain.EntityMeta `json:"meta"`
}

var dumpCommitlogCmd = &cobra.Command{
	Use:   "dump-commitlog",
	Short: "Dump commit logs as JSONL for backup or transplant",
	Long: "Streams the server's commit logs (the canonical immutable ledger) to a file or\n" +
		"stdout as JSONL, one concrnt.SignedDocument per line, ordered chronologically by id,\n" +
		"preceded by the entity_meta rows ({\"meta\":{...}}) needed to restore local entities.\n" +
		"Use --since/--since-id (and optionally --until/--until-id) to export only commits from\n" +
		"a given point in time onward. Reads directly from Postgres; no running server required.",
	RunE: withOperationContext(func(cmd *cobra.Command, args []string, op *operationContext) error {
		ctx := cmd.Context()

		// Resolve the inclusive id bounds from --since*/--until*. Time-kind CDIDs
		// are lexicographically ordered by their embedded millisecond timestamp,
		// so a plain string comparison on the id column selects a time window.
		if dumpCommitlogSince != "" && dumpCommitlogSinceID != "" {
			return fmt.Errorf("--since and --since-id are mutually exclusive")
		}
		if dumpCommitlogUntil != "" && dumpCommitlogUntilID != "" {
			return fmt.Errorf("--until and --until-id are mutually exclusive")
		}
		lowerBound := dumpCommitlogSinceID
		if dumpCommitlogSinceID != "" {
			if _, err := cdid.Parse(dumpCommitlogSinceID); err != nil {
				return fmt.Errorf("invalid --since-id %q: %w", dumpCommitlogSinceID, err)
			}
		}
		if dumpCommitlogSince != "" {
			t, err := time.Parse(time.RFC3339, dumpCommitlogSince)
			if err != nil {
				return fmt.Errorf("invalid --since %q (expected RFC3339): %w", dumpCommitlogSince, err)
			}
			lowerBound = cdid.New([10]byte{}, t.UTC()).String() // smallest id at t
		}
		upperBound := dumpCommitlogUntilID
		if dumpCommitlogUntilID != "" {
			if _, err := cdid.Parse(dumpCommitlogUntilID); err != nil {
				return fmt.Errorf("invalid --until-id %q: %w", dumpCommitlogUntilID, err)
			}
		}
		if dumpCommitlogUntil != "" {
			t, err := time.Parse(time.RFC3339, dumpCommitlogUntil)
			if err != nil {
				return fmt.Errorf("invalid --until %q (expected RFC3339): %w", dumpCommitlogUntil, err)
			}
			maxData := [10]byte{}
			for i := range maxData {
				maxData[i] = 0xff
			}
			upperBound = cdid.New(maxData, t.UTC()).String() // largest id at t
		}

		out := os.Stdout
		if dumpCommitlogOutput != "" {
			f, err := os.Create(dumpCommitlogOutput)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			defer f.Close()
			out = f
		}
		w := bufio.NewWriter(out)
		defer w.Flush()

		// Emit entity_meta rows first. They carry local-entity registration
		// state (inviter/info) that is not part of the commit log, and import
		// must restore them before replaying entity commits. Time filters don't
		// apply to meta; --owner does.
		var metas []models.EntityMeta
		mq := op.DB.WithContext(ctx)
		if dumpCommitlogOwner != "" {
			mq = mq.Where("id = ?", dumpCommitlogOwner)
		}
		if err := mq.Find(&metas).Error; err != nil {
			return fmt.Errorf("failed to query entity meta: %w", err)
		}
		for _, m := range metas {
			line, err := json.Marshal(metaLine{Meta: &domain.EntityMeta{
				ID:      m.ID,
				Inviter: m.Inviter,
				Info:    m.Info,
			}})
			if err != nil {
				return fmt.Errorf("failed to marshal entity meta %s: %w", m.ID, err)
			}
			if _, err := w.Write(append(line, '\n')); err != nil {
				return fmt.Errorf("failed to write output: %w", err)
			}
		}

		// Page through commit logs by id. Order/paginate/filter by the id
		// column's own collation so the primary-key index backs the range scan
		// (forcing COLLATE "C" would defeat the index and full-scan the table on
		// every page). This is correct because time-kind CDIDs are fixed-length
		// lowercase base32 whose byte order encodes the embedded timestamp, and
		// every standard collation orders that alphabet identically to byte order.
		cursor := lowerBound
		first := true
		total := 0
		for {
			var logs []models.CommitLog
			q := op.DB.WithContext(ctx).Order("commit_logs.id ASC").Limit(dumpCommitlogPageSize)
			if first {
				q = q.Where("commit_logs.id >= ?", cursor)
			} else {
				q = q.Where("commit_logs.id > ?", cursor)
			}
			if upperBound != "" {
				q = q.Where("commit_logs.id <= ?", upperBound)
			}
			if dumpCommitlogOwner != "" {
				q = q.Joins("JOIN commit_owners co ON co.commit_log_id = commit_logs.id").
					Where("co.owner = ?", dumpCommitlogOwner)
			}
			if err := q.Find(&logs).Error; err != nil {
				return fmt.Errorf("failed to query commit logs: %w", err)
			}
			if len(logs) == 0 {
				break
			}

			for _, cl := range logs {
				var proof concrnt.Proof
				if err := json.Unmarshal([]byte(cl.Proof), &proof); err != nil {
					return fmt.Errorf("failed to parse proof for commit %s: %w", cl.ID, err)
				}
				line, err := json.Marshal(concrnt.SignedDocument{
					Document: cl.Document,
					Proof:    proof,
				})
				if err != nil {
					return fmt.Errorf("failed to marshal commit %s: %w", cl.ID, err)
				}
				if _, err := w.Write(append(line, '\n')); err != nil {
					return fmt.Errorf("failed to write output: %w", err)
				}
			}

			total += len(logs)
			cursor = logs[len(logs)-1].ID
			first = false
		}

		fmt.Fprintf(os.Stderr, "dumped %d entity meta, %d commit logs\n", len(metas), total)
		return nil
	}),
}

func init() {
	operationCmd.AddCommand(dumpCommitlogCmd)

	dumpCommitlogCmd.Flags().StringVarP(&dumpCommitlogOutput, "output", "o", "", "Output file (default stdout)")
	dumpCommitlogCmd.Flags().StringVar(&dumpCommitlogSince, "since", "", "Only dump commits at/after this RFC3339 time (inclusive)")
	dumpCommitlogCmd.Flags().StringVar(&dumpCommitlogSinceID, "since-id", "", "Only dump commits at/after this commit-log CDID (inclusive)")
	dumpCommitlogCmd.Flags().StringVar(&dumpCommitlogUntil, "until", "", "Only dump commits at/before this RFC3339 time (inclusive)")
	dumpCommitlogCmd.Flags().StringVar(&dumpCommitlogUntilID, "until-id", "", "Only dump commits at/before this commit-log CDID (inclusive)")
	dumpCommitlogCmd.Flags().StringVar(&dumpCommitlogOwner, "owner", "", "Restrict dump to commits owned by this CCID")
}
