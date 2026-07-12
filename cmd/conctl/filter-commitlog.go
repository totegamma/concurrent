package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/concrnt/concrnt"
)

var filterCommitlogCmd = &cobra.Command{
	Use:   "filter-commitlog <commits-file> [out-file]",
	Short: "Drop none-proof commits from a commit-log dump",
	Long: "Reads a <name>.commits.jsonl file produced by dump-commitlog and writes only the lines\n" +
		"whose proof type is not \"none\" — i.e. drops server-imported/migrated documents and keeps\n" +
		"user-signed commits and their document-reference records. Writes to [out-file], or stdout\n" +
		"when omitted. Lines that fail to parse are KEPT (with a warning on stderr) so a filter run\n" +
		"never loses data. Pure file transform: no config, server, or database access.",
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		in, err := os.Open(args[0])
		if err != nil {
			return fmt.Errorf("failed to open commits file: %w", err)
		}
		defer in.Close()

		var out io.Writer = os.Stdout
		if len(args) == 2 {
			f, err := os.Create(args[1])
			if err != nil {
				return fmt.Errorf("failed to create out file: %w", err)
			}
			defer f.Close()
			out = f
		}
		w := bufio.NewWriter(out)

		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 0, 1<<20), importScanBuf)
		lineNo := 0
		var kept, dropped, unparsed int
		for sc.Scan() {
			lineNo++
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var sd concrnt.SignedDocument
			if err := json.Unmarshal([]byte(line), &sd); err != nil {
				unparsed++
				fmt.Fprintf(os.Stderr, "line %d: failed to parse SignedDocument, keeping as-is: %v\n", lineNo, err)
			} else if sd.Proof.Type == concrnt.ProofTypeNone {
				dropped++
				continue
			}
			kept++
			if _, err := w.WriteString(line + "\n"); err != nil {
				return fmt.Errorf("failed to write output: %w", err)
			}
		}
		if err := sc.Err(); err != nil {
			return fmt.Errorf("failed to read commits file: %w", err)
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("failed to flush output: %w", err)
		}

		fmt.Fprintf(os.Stderr, "kept %d, dropped %d none-proof commits", kept, dropped)
		if unparsed > 0 {
			fmt.Fprintf(os.Stderr, " (%d unparsable lines kept)", unparsed)
		}
		fmt.Fprintln(os.Stderr)
		return nil
	},
}

func init() {
	utilCmd.AddCommand(filterCommitlogCmd)
}
