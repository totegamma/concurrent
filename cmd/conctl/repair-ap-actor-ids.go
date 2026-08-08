package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var repairApActorIdsDryRun bool

// jwkModulus は ap_keys.public / rsaPrivPemToJwk が返す JWK-JSON から (n, e) を取り出す。
func jwkModulus(jwkJSON string) (n string, e string, err error) {
	var jwk struct {
		N string `json:"n"`
		E string `json:"e"`
	}
	if err := json.Unmarshal([]byte(jwkJSON), &jwk); err != nil {
		return "", "", err
	}
	if jwk.N == "" || jwk.E == "" {
		return "", "", fmt.Errorf("JWK has no RSA public components")
	}
	return jwk.N, jwk.E, nil
}

var repairApActorIdsCmd = &cobra.Command{
	Use:   "repair-ap-actor-ids",
	Short: "Restore original-case ActivityPub actor IDs and v1 RSA keys lost by migrate ap-v1-to-v2",
	Long: `migrate ap-v1-to-v2 used to lowercase every entity ID, but the actor URI
(https://<fqdn>/ap/acct/<id>) is the actor's identity in ActivityPub: remote servers
that followed "MKB098" now 404 on the actor they follow, and the v2 bridge signs
deliveries as a different actor ("mkb098") whose username collides with the known
one on Misskey-family servers, so every outbound activity is silently dropped.
The migration's ON CONFLICT DO NOTHING could also discard the converted v1 RSA key
when the v2 bridge had already lazily generated one, breaking HTTP signature
continuity the same way.

This restores both from the v1 bridge database (the source of truth):
  - renames v2 ap_entities.id / ap_keys.owner_id back to the v1 casing
    (stale keys the v2 bridge generated under the lowercased id are dropped)
  - re-converts the v1 RSA key and overwrites the v2 RSA key when the modulus
    differs (Ed25519 keys are v2-only and left as-is)

Run this while the v2 bridge is stopped: a running bridge would lazily re-generate
keys under the old id mid-repair. Reads and writes the two bridge databases
directly; the core server is not involved. Re-running is a no-op.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// not-found前提の探索が多いのでgormのErrRecordNotFoundログは抑止する
		gormConfig := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
		fromDB, err := gorm.Open(postgres.Open(apFromDsn), gormConfig)
		if err != nil {
			return fmt.Errorf("failed to connect v1 bridge database: %w", err)
		}
		destDB, err := gorm.Open(postgres.Open(apDestDsn), gormConfig)
		if err != nil {
			return fmt.Errorf("failed to connect v2 bridge database: %w", err)
		}

		if repairApActorIdsDryRun {
			fmt.Println("=== DRY RUN (no writes) ===")
		}

		var v1Entities []v1ApEntity
		if err := fromDB.Find(&v1Entities).Error; err != nil {
			return fmt.Errorf("failed to load v1 entities: %w", err)
		}
		fmt.Printf("total v1 entities: %d\n", len(v1Entities))

		// 移行のToLowerで別々のv1 IDが同じv2 IDに潰れていると片方のデータが
		// 既に失われている。自動では直せないので検出して除外する。
		byLower := map[string][]string{}
		for _, e := range v1Entities {
			lower := strings.ToLower(e.ID)
			byLower[lower] = append(byLower[lower], e.ID)
		}

		renamed, keysRestored, ok, warned := 0, 0, 0, 0

		err = destDB.Transaction(func(tx *gorm.DB) error {
			for _, e := range v1Entities {
				origID := e.ID
				lowerID := strings.ToLower(origID)
				ccid := strings.TrimSpace(e.CCID)

				if len(byLower[lowerID]) > 1 {
					fmt.Printf("  WARN %s: v1 IDs %v collide after lowercasing; skipping (resolve manually)\n", origID, byLower[lowerID])
					warned++
					continue
				}

				var dest v2ApEntity
				findErr := tx.Where("id = ?", origID).First(&dest).Error
				if findErr == gorm.ErrRecordNotFound && origID != lowerID {
					// 未修復: 小文字化された行を探して改名する
					findErr = tx.Where("id = ?", lowerID).First(&dest).Error
					if findErr == nil {
						if dest.CCID != ccid {
							fmt.Printf("  WARN %s: v2 entity %s has ccid %s (v1: %s); skipping\n", origID, lowerID, dest.CCID, ccid)
							warned++
							continue
						}
						fmt.Printf("  rename %s -> %s\n", lowerID, origID)
						renamed++
						if !repairApActorIdsDryRun {
							if err := tx.Model(&v2ApEntity{}).Where("id = ?", lowerID).Update("id", origID).Error; err != nil {
								return fmt.Errorf("failed to rename entity %s: %w", lowerID, err)
							}
						}
						dest.ID = origID
					}
				}
				if findErr != nil {
					if findErr == gorm.ErrRecordNotFound {
						fmt.Printf("  WARN %s: not found in v2 bridge database; skipping\n", origID)
						warned++
					} else {
						return fmt.Errorf("failed to look up v2 entity %s: %w", origID, findErr)
					}
					continue
				}
				if dest.CCID != ccid {
					fmt.Printf("  WARN %s: v2 ccid %s differs from v1 %s; skipping\n", origID, dest.CCID, ccid)
					warned++
					continue
				}

				// 改名後(または既修復後)に旧小文字idの下へ残った鍵の後始末。
				// ブリッジが稼働中に遅延生成したもので、正名の鍵があるなら不要品。
				rsaKeyStillUnderLower := false
				if origID != lowerID {
					var staleKeys []v2ApKey
					if err := tx.Where("owner_id = ?", lowerID).Find(&staleKeys).Error; err != nil {
						return fmt.Errorf("failed to load keys for %s: %w", lowerID, err)
					}
					for _, stale := range staleKeys {
						var exists int64
						if err := tx.Model(&v2ApKey{}).Where("owner_id = ? AND key_type = ?", origID, stale.KeyType).Count(&exists).Error; err != nil {
							return fmt.Errorf("failed to check key for %s: %w", origID, err)
						}
						if exists > 0 {
							fmt.Printf("  drop stale %s key generated under %s\n", stale.KeyType, lowerID)
						} else {
							fmt.Printf("  rename %s key %s -> %s\n", stale.KeyType, lowerID, origID)
							if stale.KeyType == apRsaKeyType {
								rsaKeyStillUnderLower = true
							}
						}
						if !repairApActorIdsDryRun {
							if exists > 0 {
								err = tx.Where("owner_id = ? AND key_type = ?", lowerID, stale.KeyType).Delete(&v2ApKey{}).Error
							} else {
								err = tx.Model(&v2ApKey{}).Where("owner_id = ? AND key_type = ?", lowerID, stale.KeyType).Update("owner_id", origID).Error
							}
							if err != nil {
								return fmt.Errorf("failed to move key for %s: %w", lowerID, err)
							}
						}
					}
				}

				// RSA鍵の連続性: リモートが信頼しているのはv1の鍵。modulusが違えば戻す
				privJWK, pubJWK, keyErr := rsaPrivPemToJwk(e.Privatekey)
				if keyErr != nil {
					fmt.Printf("  WARN %s: v1 key conversion failed: %s\n", origID, keyErr)
					warned++
					continue
				}
				v1N, v1E, err := jwkModulus(pubJWK)
				if err != nil {
					return fmt.Errorf("converted v1 key for %s is malformed: %w", origID, err)
				}

				// dry-runではrename予定の鍵がまだ旧id側に居るので、そちらを比較対象にする
				keyOwner := origID
				if repairApActorIdsDryRun && rsaKeyStillUnderLower {
					keyOwner = lowerID
				}
				var destKey v2ApKey
				keyFindErr := tx.Where("owner_id = ? AND key_type = ?", keyOwner, apRsaKeyType).First(&destKey).Error
				if keyFindErr == gorm.ErrRecordNotFound {
					fmt.Printf("  insert missing RSA key for %s\n", origID)
					keysRestored++
					if !repairApActorIdsDryRun {
						if err := tx.Create(&v2ApKey{OwnerID: origID, KeyType: apRsaKeyType, Private: privJWK, Public: pubJWK}).Error; err != nil {
							return fmt.Errorf("failed to insert key for %s: %w", origID, err)
						}
					}
					continue
				}
				if keyFindErr != nil {
					return fmt.Errorf("failed to look up v2 key for %s: %w", origID, keyFindErr)
				}

				destN, destE, err := jwkModulus(destKey.Public)
				if err != nil || destN != v1N || destE != v1E {
					fmt.Printf("  restore v1 RSA key for %s (v2 key was regenerated)\n", origID)
					keysRestored++
					if !repairApActorIdsDryRun {
						updates := map[string]any{"private": privJWK, "public": pubJWK}
						if err := tx.Model(&v2ApKey{}).Where("owner_id = ? AND key_type = ?", origID, apRsaKeyType).Updates(updates).Error; err != nil {
							return fmt.Errorf("failed to restore key for %s: %w", origID, err)
						}
					}
					continue
				}

				ok++
			}
			return nil
		})
		if err != nil {
			return err
		}

		fmt.Printf("entities renamed: %d, RSA keys restored: %d, already consistent: %d, warnings: %d\n",
			renamed, keysRestored, ok, warned)
		if !repairApActorIdsDryRun && (renamed > 0 || keysRestored > 0) {
			fmt.Println("restart the v2 ActivityPub bridge to pick up the changes")
		}
		return nil
	},
}

func init() {
	operationCmd.AddCommand(repairApActorIdsCmd)
	repairApActorIdsCmd.Flags().StringVar(&apFromDsn, "from-dsn", apFromDsn, "PostgreSQL DSN of the v1 ActivityPub bridge (source of truth)")
	repairApActorIdsCmd.Flags().StringVar(&apDestDsn, "dest-dsn", apDestDsn, "PostgreSQL DSN of the v2 ActivityPub bridge to repair")
	repairApActorIdsCmd.Flags().BoolVar(&repairApActorIdsDryRun, "dry-run", false, "Only list the repairs that would be made")

	repairApActorIdsCmd.MarkFlagRequired("from-dsn")
	repairApActorIdsCmd.MarkFlagRequired("dest-dsn")
}
