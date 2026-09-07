package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/concrnt/concrnt"
)

var (
	filterCommitlogExcludeProofs []string
	filterCommitlogExcludeOwners []string
	filterCommitlogOnlyKinds     []string
	filterCommitlogOnlySchemas   []string
)

var filterCommitlogCmd = &cobra.Command{
	Use:   "filter-commitlog <commits-file> [out-file]",
	Short: "Filter a commit-log dump by proof type, owner, kind, and schema",
	Long: "Reads a <name>.commits.jsonl file produced by dump-commitlog and writes the lines that\n" +
		"pass every given filter. --exclude-proof drops commits by proof type (e.g. \"none\" to\n" +
		"drop server-imported/migrated documents while keeping user-signed commits and their\n" +
		"document-reference records). --exclude-owner drops commits belonging to the given CCIDs\n" +
		"(e.g. to erase test accounts): a commit is considered owned by a CCID when it is the\n" +
		"document's author, the owner of the document's key, or the owner of its associate\n" +
		"target — a superset of the single owner the server records on each commit. --only-kind\n" +
		"keeps only commits whose document kind matches (entity / record / association / delete /\n" +
		"ack / unack / acked / unacked); --only-schema keeps only commits whose document schema\n" +
		"exactly matches the given URL — documents without a schema (entity, delete, ...) are dropped when\n" +
		"--only-schema is given, so combine with --only-kind deliberately. Repeated values of\n" +
		"one flag are OR; different flags are AND. With no filters the input passes through\n" +
		"unchanged. Writes to [out-file], or stdout when omitted. Lines that fail to parse are\n" +
		"KEPT (with a warning on stderr) so a filter run never loses data. Pure file transform:\n" +
		"no config, server, or database access.",
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
		var kept, droppedProof, droppedOwner, droppedKind, droppedSchema, unparsed int
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
				kept++
				if _, err := w.WriteString(line + "\n"); err != nil {
					return fmt.Errorf("failed to write output: %w", err)
				}
				continue
			}
			if slices.Contains(filterCommitlogExcludeProofs, sd.Proof.Type) {
				droppedProof++
				continue
			}
			if len(filterCommitlogExcludeOwners) > 0 || len(filterCommitlogOnlyKinds) > 0 || len(filterCommitlogOnlySchemas) > 0 {
				var doc struct {
					Kind      string  `json:"kind"`
					Schema    string  `json:"schema"`
					Author    string  `json:"author"`
					Key       string  `json:"key"`
					Associate *string `json:"associate"`
				}
				if err := json.Unmarshal([]byte(sd.Document), &doc); err != nil {
					unparsed++
					fmt.Fprintf(os.Stderr, "line %d: failed to parse document, keeping as-is: %v\n", lineNo, err)
				} else {
					if len(filterCommitlogExcludeOwners) > 0 {
						owners := []string{doc.Author}
						if doc.Key != "" {
							if parsed, err := concrnt.ParseCCURI(doc.Key); err == nil {
								owners = append(owners, parsed.Owner)
							}
						}
						if doc.Associate != nil {
							if parsed, err := concrnt.ParseCCURI(*doc.Associate); err == nil {
								owners = append(owners, parsed.Owner)
							}
						}
						if slices.ContainsFunc(owners, func(o string) bool {
							return slices.Contains(filterCommitlogExcludeOwners, o)
						}) {
							droppedOwner++
							continue
						}
					}
					if len(filterCommitlogOnlyKinds) > 0 && !slices.Contains(filterCommitlogOnlyKinds, doc.Kind) {
						droppedKind++
						continue
					}
					if len(filterCommitlogOnlySchemas) > 0 && !slices.Contains(filterCommitlogOnlySchemas, doc.Schema) {
						droppedSchema++
						continue
					}
				}
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

		fmt.Fprintf(os.Stderr, "kept %d", kept)
		if len(filterCommitlogExcludeProofs) > 0 {
			fmt.Fprintf(os.Stderr, ", dropped %d by proof type", droppedProof)
		}
		if len(filterCommitlogExcludeOwners) > 0 {
			fmt.Fprintf(os.Stderr, ", dropped %d by owner", droppedOwner)
		}
		if len(filterCommitlogOnlyKinds) > 0 {
			fmt.Fprintf(os.Stderr, ", dropped %d by kind", droppedKind)
		}
		if len(filterCommitlogOnlySchemas) > 0 {
			fmt.Fprintf(os.Stderr, ", dropped %d by schema", droppedSchema)
		}
		if unparsed > 0 {
			fmt.Fprintf(os.Stderr, " (%d unparsable lines kept)", unparsed)
		}
		fmt.Fprintln(os.Stderr)
		return nil
	},
}

func init() {
	utilCmd.AddCommand(filterCommitlogCmd)

	filterCommitlogCmd.Flags().StringSliceVar(&filterCommitlogExcludeProofs, "exclude-proof", nil, "Drop commits with this proof type (repeatable, e.g. none)")
	filterCommitlogCmd.Flags().StringSliceVar(&filterCommitlogExcludeOwners, "exclude-owner", nil, "Drop commits owned by this CCID (repeatable)")
	filterCommitlogCmd.Flags().StringSliceVar(&filterCommitlogOnlyKinds, "only-kind", nil, "Keep only commits with this document kind (repeatable, e.g. record)")
	filterCommitlogCmd.Flags().StringSliceVar(&filterCommitlogOnlySchemas, "only-schema", nil, "Keep only commits whose document schema exactly matches this URL (repeatable)")
}
