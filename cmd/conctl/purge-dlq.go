package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/concrnt/concrnt/internal/infra/database"
	"github.com/concrnt/concrnt/internal/infra/jobqueue"
)

var (
	purgeDLQOlderThan time.Duration
	purgeDLQDryRun    bool
)

var purgeDLQCmd = &cobra.Command{
	Use:   "purge-dlq",
	Short: "Delete dead letters older than a given age",
	Long: "Removes every entry from the job queue's dead-letter stream whose dead-letter time\n" +
		"(the timestamp part of the streamId shown by dump-dlq, i.e. when the job exhausted its\n" +
		"retries) is more than --older-than ago (a Go duration such as 72h or 90m).\n" +
		"Use dump-dlq first to keep a copy. Talks to Redis only;\n" +
		"safe to run against a live server.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if purgeDLQOlderThan <= 0 {
			return fmt.Errorf("--older-than must be positive, got %s", purgeDLQOlderThan)
		}
		before := time.Now().Add(-purgeDLQOlderThan)

		conf, err := loadConcrntConfig()
		if err != nil {
			return err
		}
		rdb := database.NewRedis(conf.Backends.RedisAddr, "", conf.Backends.RedisDB)
		defer rdb.Close()
		q := jobqueue.NewRedisJobQueue(rdb)

		if purgeDLQDryRun {
			// stream ids are "<unix ms>-<seq>" in ascending order, so stop at the
			// first entry at or after the cutoff
			cutoff := before.UnixMilli()
			var count int64
			stop := errors.New("cutoff reached")
			err := q.ScanDLQ(ctx, func(dl jobqueue.DeadLetter) error {
				ms, _ := strconv.ParseInt(strings.SplitN(dl.StreamID, "-", 2)[0], 10, 64)
				if ms >= cutoff {
					return stop
				}
				count++
				return nil
			})
			if err != nil && !errors.Is(err, stop) {
				return fmt.Errorf("failed to read dead-letter stream: %w", err)
			}
			fmt.Fprintf(os.Stderr, "would delete %d dead letters\n", count)
			return nil
		}

		deleted, err := q.PurgeDLQ(ctx, before)
		if err != nil {
			return fmt.Errorf("failed to purge dead-letter stream: %w", err)
		}
		fmt.Fprintf(os.Stderr, "deleted %d dead letters\n", deleted)
		return nil
	},
}

func init() {
	operationCmd.AddCommand(purgeDLQCmd)

	purgeDLQCmd.Flags().DurationVar(&purgeDLQOlderThan, "older-than", 0, "Delete dead letters dead-lettered more than this long ago (Go duration, e.g. 72h, 90m)")
	purgeDLQCmd.Flags().BoolVar(&purgeDLQDryRun, "dry-run", false, "Only count the dead letters that would be deleted")
	purgeDLQCmd.MarkFlagRequired("older-than")
}
