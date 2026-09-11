package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/concrnt/concrnt/internal/infra/database"
	"github.com/concrnt/concrnt/internal/infra/jobqueue"
)

var dumpDLQCmd = &cobra.Command{
	Use:   "dump-dlq",
	Short: "Dump the job queue's dead-letter stream to stdout as JSONL",
	Long: "Writes every job in the dead-letter stream (jobs that exhausted their retries,\n" +
		"plus any stream message that could not be parsed as a job) to stdout as JSONL,\n" +
		"oldest first, one jobqueue.DeadLetter per line: {\"streamId\", \"job\"} where job is\n" +
		"the queue envelope (id, type, payload, attempt, createdAt, lastError), or\n" +
		"{\"streamId\", \"raw\"} for an unparseable entry. Read-only: nothing is removed\n" +
		"from the stream. Talks to Redis only; safe to run against a live server.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		conf, err := loadConcrntConfig()
		if err != nil {
			return err
		}
		rdb := database.NewRedis(conf.Backends.RedisAddr, "", conf.Backends.RedisDB)
		defer rdb.Close()

		w := bufio.NewWriter(os.Stdout)
		defer w.Flush()

		count := 0
		err = jobqueue.NewRedisJobQueue(rdb).ScanDLQ(ctx, func(dl jobqueue.DeadLetter) error {
			line, err := json.Marshal(dl)
			if err != nil {
				return err
			}
			if _, err := w.Write(append(line, '\n')); err != nil {
				return err
			}
			count++
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to read dead-letter stream: %w", err)
		}
		fmt.Fprintf(os.Stderr, "dumped %d dead letters\n", count)
		return nil
	},
}

func init() {
	operationCmd.AddCommand(dumpDLQCmd)
}
