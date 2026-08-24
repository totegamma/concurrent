package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/cdid"
	"github.com/concrnt/concrnt/internal/infra/database/models"
)

var repairAckedBackfillDryRun bool

var repairAckedBackfillCmd = &cobra.Command{
	Use:   "repair-acked-backfill",
	Short: "Backfill acked/unacked mirror commits and collapse commit ownership to commit_logs.owner",
	Long: "Commits used to allow multiple owners via the commit_owners join table — in practice\n" +
		"only ack/unack between two local users (owned by both). Ownership now lives on the\n" +
		"single commit_logs.owner column, an ack/unack commit is owned by its author alone,\n" +
		"and the ackee's side of the relationship is a separate acked/unacked mirror commit\n" +
		"(kind flipped, ack-reference proof embedding the original) owned by the ackee.\n" +
		"This backfills existing data into that model:\n" +
		"  1. commit_logs.owner from commit_owners (single-owner rows verbatim; multi-owner\n" +
		"     ack rows resolve to the document's author),\n" +
		"  2. one mirror commit per acks row whose ackee is local, anchored via\n" +
		"     acks.acked_commit_id,\n" +
		"  3. acks.ack_commit_id anchors for rows whose author is local,\n" +
		"  4. legacy recorded raw acks whose author is NOT local (the old receiving-side\n" +
		"     copies) are disowned and flagged gc_candidate — the ackee's holding is the\n" +
		"     mirror now.\n" +
		"Run this right after deploying the build that writes commit_logs.owner; dumps and\n" +
		"unregister GC are incomplete for pre-existing data until it has run. Re-running is\n" +
		"a no-op. commit_owners itself is left in place for the operator to drop.\n" +
		"Reads and writes Postgres directly; no running server required.",
	Args: cobra.NoArgs,
	RunE: withOperationContext(func(cmd *cobra.Command, args []string, op *operationContext) error {
		ctx := cmd.Context()
		fqdn := op.GlobalConfig.FQDN

		localCache := map[string]bool{}
		isLocal := func(ccid string) (bool, error) {
			if v, ok := localCache[ccid]; ok {
				return v, nil
			}
			var entity models.Entity
			err := op.DB.WithContext(ctx).Select("id", "domain").Where("id = ?", ccid).Take(&entity).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return false, err
			}
			local := err == nil && entity.Domain == fqdn
			localCache[ccid] = local
			return local, nil
		}

		// The legacy join table only exists on databases migrated from the
		// multi-owner era; a fresh v2 database never has it. Without it the
		// owner backfill has nothing to read — but the mirror/anchor walk
		// below still runs, so the command doubles as a recovery tool for
		// mirrors lost to post-process failures.
		hasLegacyOwners := op.DB.Migrator().HasTable("commit_owners")
		if !hasLegacyOwners {
			fmt.Fprintln(os.Stderr, "commit_owners table not found: skipping legacy ownership backfill")
		}

		multiOwned := 0
		if hasLegacyOwners {
			// Step 1a: single-owner commits carry their owner over verbatim.
			if repairAckedBackfillDryRun {
				var count int64
				err := op.DB.WithContext(ctx).Raw(
					`SELECT count(*) FROM commit_logs
					 WHERE owner IS NULL AND EXISTS (SELECT 1 FROM commit_owners co WHERE co.commit_log_id = commit_logs.id)`,
				).Scan(&count).Error
				if err != nil {
					return fmt.Errorf("failed to count ownerless commit logs: %w", err)
				}
				fmt.Fprintf(os.Stderr, "would backfill owner for up to %d commit logs\n", count)
			} else {
				res := op.DB.WithContext(ctx).Exec(
					`UPDATE commit_logs SET owner = co.owner
					 FROM commit_owners co
					 WHERE co.commit_log_id = commit_logs.id
					   AND commit_logs.owner IS NULL
					   AND NOT EXISTS (
					     SELECT 1 FROM commit_owners c2
					     WHERE c2.commit_log_id = commit_logs.id AND c2.owner <> co.owner)`,
				)
				if res.Error != nil {
					return fmt.Errorf("failed to backfill single-owner commit logs: %w", res.Error)
				}
				fmt.Fprintf(os.Stderr, "backfilled owner for %d single-owner commit logs\n", res.RowsAffected)
			}

			// Step 1b: multi-owner commits are ack/unack between two local
			// users; the author is the sole owner under the new model.
			var logs []models.CommitLog
			err := op.DB.WithContext(ctx).
				Where(`id IN (SELECT commit_log_id FROM commit_owners GROUP BY commit_log_id HAVING count(DISTINCT owner) > 1)`).
				FindInBatches(&logs, 500, func(_ *gorm.DB, _ int) error {
					for _, cl := range logs {
						var doc concrnt.Document[any]
						if err := json.Unmarshal([]byte(cl.Document), &doc); err != nil {
							fmt.Fprintf(os.Stderr, "skipping %s: failed to parse document: %v\n", cl.ID, err)
							continue
						}
						if cl.Owner != nil && *cl.Owner == doc.Author {
							continue
						}
						multiOwned++
						if repairAckedBackfillDryRun {
							fmt.Printf("owner %s -> %s (%s)\n", cl.ID, doc.Author, doc.Kind)
							continue
						}
						if err := op.DB.WithContext(ctx).Model(&models.CommitLog{}).
							Where("id = ?", cl.ID).
							Update("owner", doc.Author).Error; err != nil {
							return err
						}
					}
					return nil
				}).Error
			if err != nil {
				return fmt.Errorf("failed to backfill multi-owner commit logs: %w", err)
			}
		}

		// Step 1c: commits that never had ownership rows at all. A long-lived
		// bug (an FQDN compared under an IsCSID guard) meant records keyed
		// under the server's own namespace (cckv://<fqdn>/... or the CSID)
		// never got a commit_owners row, so step 1a has nothing to copy.
		// Re-derive the owner from the document the way the live server now
		// does; genuinely ownerless pass-throughs (remote targets, disowned
		// raw acks) derive to nothing and stay NULL.
		derived := 0
		var orphans []models.CommitLog
		if err := op.DB.WithContext(ctx).
			Where("owner IS NULL").
			FindInBatches(&orphans, 500, func(_ *gorm.DB, _ int) error {
				for _, cl := range orphans {
					var doc concrnt.Document[any]
					if err := json.Unmarshal([]byte(cl.Document), &doc); err != nil {
						fmt.Fprintf(os.Stderr, "skipping %s: failed to parse document: %v\n", cl.ID, err)
						continue
					}

					candidate := ""
					switch doc.Kind {
					case "record":
						if parsed, err := concrnt.ParseCCURI(doc.Key); err == nil {
							candidate = parsed.Owner
						}
					case "association", "acked", "unacked":
						if doc.Associate != nil {
							if parsed, err := concrnt.ParseCCURI(*doc.Associate); err == nil {
								candidate = parsed.Owner
							}
						}
					case "ack", "unack", "delete", "entity":
						candidate = doc.Author
					}
					if candidate == "" {
						continue
					}

					owner := ""
					switch {
					case doc.Kind == "entity":
						// entities are owned by their author unconditionally,
						// remote cached copies included (mirrors the live path)
						owner = candidate
					case concrnt.IsCCID(candidate):
						local, err := isLocal(candidate)
						if err != nil {
							return err
						}
						if local {
							owner = candidate
						}
					case candidate == fqdn || candidate == op.GlobalConfig.CSID:
						owner = candidate
					}
					if owner == "" {
						continue
					}

					derived++
					if repairAckedBackfillDryRun {
						fmt.Printf("derive %s -> %s (%s)\n", cl.ID, owner, doc.Kind)
						continue
					}
					if err := op.DB.WithContext(ctx).Model(&models.CommitLog{}).
						Where("id = ? AND owner IS NULL", cl.ID).
						Update("owner", owner).Error; err != nil {
						return err
					}
				}
				return nil
			}).Error; err != nil {
			return fmt.Errorf("failed to re-derive ownerless commit logs: %w", err)
		}

		// Steps 2-4: walk the ack state rows.
		mirrors, anchors, disowned, skipped := 0, 0, 0, 0
		var acks []models.Ack
		err := op.DB.WithContext(ctx).FindInBatches(&acks, 500, func(_ *gorm.DB, _ int) error {
			for _, ack := range acks {
				fromLocal, err := isLocal(ack.From)
				if err != nil {
					return err
				}
				toLocal, err := isLocal(ack.To)
				if err != nil {
					return err
				}

				var origLog models.CommitLog
				origErr := op.DB.WithContext(ctx).Where("id = ?", ack.DocumentID).Take(&origLog).Error
				if origErr != nil && !errors.Is(origErr, gorm.ErrRecordNotFound) {
					return origErr
				}
				origExists := origErr == nil

				// Step 3: the author-side commit anchors the row there.
				if fromLocal && origExists && ack.AckCommitID == nil {
					anchors++
					if !repairAckedBackfillDryRun {
						if err := op.DB.WithContext(ctx).Model(&models.Ack{}).
							Where("document_id = ?", ack.DocumentID).
							Update("ack_commit_id", ack.DocumentID).Error; err != nil {
							return err
						}
					}
				}

				// Step 4: a recorded raw ack whose author is remote is the old
				// receiving-side copy — pass-through history under the new
				// model. Disown it and let gc-commitlog collect it once the
				// mirror below carries the ackee's holding.
				if !fromLocal && origExists && (origLog.Owner != nil || !origLog.GcCandidate) {
					disowned++
					if repairAckedBackfillDryRun {
						fmt.Printf("disown %s (raw ack by remote %s)\n", origLog.ID, ack.From)
					} else {
						if err := op.DB.WithContext(ctx).Model(&models.CommitLog{}).
							Where("id = ?", origLog.ID).
							Updates(map[string]any{"owner": nil, "gc_candidate": true}).Error; err != nil {
							return err
						}
						// Drop the legacy ownership rows too, or step 1a would
						// resurrect the owner from them on a re-run.
						if hasLegacyOwners {
							if err := op.DB.WithContext(ctx).
								Exec(`DELETE FROM commit_owners WHERE commit_log_id = ?`, origLog.ID).Error; err != nil {
								return err
							}
						}
					}
				}

				// Step 2: materialize the ackee's mirror.
				if !toLocal {
					continue
				}
				if !origExists {
					skipped++
					fmt.Fprintf(os.Stderr, "skipping mirror for %s -> %s (%s): original ack commit %s not found\n",
						ack.From, ack.To, ack.Schema, ack.DocumentID)
					continue
				}

				var origProof concrnt.Proof
				if err := json.Unmarshal([]byte(origLog.Proof), &origProof); err != nil {
					skipped++
					fmt.Fprintf(os.Stderr, "skipping mirror for %s: failed to parse proof: %v\n", ack.DocumentID, err)
					continue
				}
				origSD := concrnt.SignedDocument{Document: origLog.Document, Proof: origProof}
				mirrorSD, err := origSD.DeriveAcked()
				if err != nil {
					skipped++
					fmt.Fprintf(os.Stderr, "skipping mirror for %s: %v\n", ack.DocumentID, err)
					continue
				}
				var orig concrnt.Document[any]
				if err := json.Unmarshal([]byte(origLog.Document), &orig); err != nil {
					skipped++
					fmt.Fprintf(os.Stderr, "skipping mirror for %s: failed to parse document: %v\n", ack.DocumentID, err)
					continue
				}

				hash := concrnt.GetHash([]byte(mirrorSD.Document))
				var hash10 [10]byte
				copy(hash10[:], hash[:10])
				mirrorID := cdid.New(hash10, orig.CreatedAt).String()

				if ack.AckedCommitID != nil && *ack.AckedCommitID == mirrorID {
					continue
				}

				mirrors++
				if repairAckedBackfillDryRun {
					fmt.Printf("mirror %s -> %s (%s): %s\n", ack.From, ack.To, ack.Schema, mirrorID)
					continue
				}

				proofBytes, err := json.Marshal(mirrorSD.Proof)
				if err != nil {
					return err
				}

				to := ack.To
				err = op.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.CommitLog{
						ID:       mirrorID,
						Owner:    &to,
						Document: mirrorSD.Document,
						Proof:    string(proofBytes),
					}).Error; err != nil {
						return err
					}
					return tx.Model(&models.Ack{}).
						Where("document_id = ?", ack.DocumentID).
						Update("acked_commit_id", mirrorID).Error
				})
				if err != nil {
					return fmt.Errorf("failed to backfill mirror for %s: %w", ack.DocumentID, err)
				}
			}
			return nil
		}).Error
		if err != nil {
			return fmt.Errorf("failed to walk ack rows: %w", err)
		}

		action := "backfilled"
		if repairAckedBackfillDryRun {
			action = "would backfill"
		}
		fmt.Fprintf(os.Stderr, "%s: %d multi-owner commits re-owned, %d ownerless commits re-derived, %d mirrors, %d author anchors, %d raw acks disowned, %d skipped\n",
			action, multiOwned, derived, mirrors, anchors, disowned, skipped)
		return nil
	}),
}

func init() {
	operationCmd.AddCommand(repairAckedBackfillCmd)
	repairAckedBackfillCmd.Flags().BoolVar(&repairAckedBackfillDryRun, "dry-run", false, "Report what would change without writing")
}

// moveLegacyCommitOwners repoints rows in the legacy commit_owners table (if
// it still exists) when a commit log is re-created under a new id, so a later
// repair-acked-backfill still finds them — the table has an ON DELETE CASCADE
// foreign key, so leaving the rows behind would silently drop them with the
// old log. No-op once the operator has dropped the table.
func moveLegacyCommitOwners(tx *gorm.DB, oldID string, newID string) error {
	if !tx.Migrator().HasTable("commit_owners") {
		return nil
	}
	return tx.Exec(`UPDATE commit_owners SET commit_log_id = ? WHERE commit_log_id = ?`, newID, oldID).Error
}
