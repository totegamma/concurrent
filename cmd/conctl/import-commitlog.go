package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/impl/interop"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/infra/repository/postgres"
	"github.com/concrnt/concrnt/internal/usecase"
)

var (
	importCommitlogInput  string
	importCommitlogDryRun bool
)

// importScanBuf bounds the per-line scan buffer; commit documents can be large.
const importScanBuf = 64 << 20

var importCommitlogCmd = &cobra.Command{
	Use:   "import-commitlog",
	Short: "Import commit logs from a JSONL dump (restore or transplant)",
	Long: "Reads a JSONL dump (as produced by dump-commitlog) from a file or stdin and applies it\n" +
		"directly against Postgres in three passes: entity_meta rows first, then entity commits,\n" +
		"then everything else. This ordering is required because (a) local entity registration\n" +
		"state (entity_meta) lives outside the commit log and entity commits depend on it, and\n" +
		"(b) a dump ordered by id is not necessarily in causal order (migrate-v1-to-v2 stamps\n" +
		"entity documents with the migration time). Import is idempotent: commit ids are\n" +
		"content+time derived and inserts skip existing rows, and entity commits are\n" +
		"accept-if-newer (older replays are a no-op), so re-running is safe. Policy checks\n" +
		"are skipped (faithful restore). Imports run as the system service account, so\n" +
		"document signatures are NOT re-verified — the dump is trusted as-is — and no\n" +
		"network access is required (dump-commitlog does not persist inlined references, so\n" +
		"verifying them would otherwise need the referencing servers reachable).\n" +
		"Note: writes bypass a running server's in-process caches.",
	RunE: withOperationContext(func(cmd *cobra.Command, args []string, op *operationContext) error {
		// conctl runs with the server's own private key, so it acts as the
		// "system" service account — the same authority the auth middleware
		// grants a system JWT. This is required for the commit path to accept
		// "none" proof documents (e.g. dumps produced by migrate-v1-to-v2).
		ctx := context.WithValue(cmd.Context(), interop.ServiceAccountTypeCtxKey, "system")

		// A pass reads the whole input once; when importing from a file we can
		// reopen it for later passes instead of buffering 100k+ lines. From
		// stdin (not seekable) we buffer the lines in memory.
		var open func() (io.ReadCloser, error)
		if importCommitlogInput != "" {
			open = func() (io.ReadCloser, error) { return os.Open(importCommitlogInput) }
		} else {
			var lines []string
			sc := bufio.NewScanner(os.Stdin)
			sc.Buffer(make([]byte, 0, 1<<20), importScanBuf)
			for sc.Scan() {
				lines = append(lines, sc.Text())
			}
			if err := sc.Err(); err != nil {
				return fmt.Errorf("failed to read stdin: %w", err)
			}
			open = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(strings.Join(lines, "\n"))), nil }
		}

		residenceRepo := postgres.NewResidenceRepository(op.DB, op.Client, op.GlobalConfig)
		recordRepo := postgres.NewRecordRepository(op.DB)
		recordUC := usecase.NewRecordUsecase(recordRepo, residenceRepo, &op.GlobalConfig, op.Client, nopSignal{}, nopPolicy{}, nopDelivery{}, nil)

		mode := domain.CommitModeLocalOnlyExecute
		if importCommitlogDryRun {
			mode = domain.CommitModeDryRun
		}

		// Three passes, each a full scan selecting one kind of line:
		//   0 = entity_meta (local registration state entity commits depend on)
		//   1 = entity commits (must exist before the records they authored)
		//   2 = everything else
		// Parse errors are only counted on the last pass so a malformed line is
		// not reported once per pass.
		passes := []struct {
			label      string
			want       string // "_meta", "entity", or "" for everything else
			countParse bool
		}{
			{"meta", "_meta", false},
			{"entity", "entity", false},
			{"other", "", true},
		}

		var okMeta, okEntity, okOther, failed int
		for _, pass := range passes {
			r, err := open()
			if err != nil {
				return fmt.Errorf("failed to open input: %w", err)
			}

			sc := bufio.NewScanner(r)
			sc.Buffer(make([]byte, 0, 1<<20), importScanBuf)
			lineNo := 0
			for sc.Scan() {
				lineNo++
				line := strings.TrimSpace(sc.Text())
				if line == "" {
					continue
				}

				// Classify: a {"meta":{...}} line, else a SignedDocument.
				var probe struct {
					Meta *domain.EntityMeta `json:"meta"`
				}
				if err := json.Unmarshal([]byte(line), &probe); err != nil {
					if pass.countParse {
						failed++
						fmt.Fprintf(os.Stderr, "line %d: failed to parse line: %v\n", lineNo, err)
					}
					continue
				}

				if probe.Meta != nil {
					if pass.want != "_meta" {
						continue
					}
					if importCommitlogDryRun {
						okMeta++
						continue
					}
					if err := residenceRepo.SaveMeta(ctx, *probe.Meta); err != nil {
						failed++
						fmt.Fprintf(os.Stderr, "line %d (meta %s): failed to save: %v\n", lineNo, probe.Meta.ID, err)
						continue
					}
					okMeta++
					continue
				}

				var sd concrnt.SignedDocument
				if err := json.Unmarshal([]byte(line), &sd); err != nil {
					if pass.countParse {
						failed++
						fmt.Fprintf(os.Stderr, "line %d: failed to parse SignedDocument: %v\n", lineNo, err)
					}
					continue
				}
				var head struct {
					Kind string `json:"kind"`
				}
				if err := json.Unmarshal([]byte(sd.Document), &head); err != nil {
					if pass.countParse {
						failed++
						fmt.Fprintf(os.Stderr, "line %d: failed to parse document kind: %v\n", lineNo, err)
					}
					continue
				}

				// pass.want == "" means "everything that is not meta/entity".
				if pass.want == "_meta" || (pass.want == "" && head.Kind == "entity") {
					continue
				}
				if pass.want == "entity" && head.Kind != "entity" {
					continue
				}

				if _, err := recordUC.Commit(ctx, "", sd, mode); err != nil {
					failed++
					fmt.Fprintf(os.Stderr, "line %d (%s): failed to commit: %v\n", lineNo, head.Kind, err)
					continue
				}
				if head.Kind == "entity" {
					okEntity++
				} else {
					okOther++
				}
			}
			readErr := sc.Err()
			r.Close()
			if readErr != nil {
				return fmt.Errorf("failed to read input: %w", readErr)
			}
		}

		ok := okMeta + okEntity + okOther
		verb := "imported"
		if importCommitlogDryRun {
			verb = "validated (dry-run)"
		}
		fmt.Fprintf(os.Stderr, "%s %d lines (%d meta, %d entities, %d others), %d failed\n", verb, ok, okMeta, okEntity, okOther, failed)
		if failed > 0 {
			return fmt.Errorf("%d lines failed to import", failed)
		}
		return nil
	}),
}

func init() {
	operationCmd.AddCommand(importCommitlogCmd)

	importCommitlogCmd.Flags().StringVarP(&importCommitlogInput, "input", "i", "", "Input JSONL file (default stdin)")
	importCommitlogCmd.Flags().BoolVar(&importCommitlogDryRun, "dry-run", false, "Validate and dry-run commits without persisting")
}
