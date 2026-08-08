package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/spf13/cobra"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/concrnt/concrnt"
	cdidv2 "github.com/concrnt/concrnt/cdid"
)

var (
	apFromDsn     string
	apDestDsn     string
	apServiceCcid string
	apDryRun      bool
)

// --- v1 (ccworld-ap-bridge) source models ---
// v1構造体は別リポジトリのためimportできないので必要な列だけ再定義する。
// 列名はv1のGORMデフォルト命名(実列名はv1自身のクエリで確認済み)に合わせる。

type v1ApEntity struct {
	ID          string         `gorm:"column:id"`
	CCID        string         `gorm:"column:cc_id"`
	Enabled     bool           `gorm:"column:enabled"`
	Publickey   string         `gorm:"column:publickey"`
	Privatekey  string         `gorm:"column:privatekey"`
	AlsoKnownAs pq.StringArray `gorm:"column:also_known_as;type:text[]"`
}

func (v1ApEntity) TableName() string { return "ap_entities" }

// concrnt -> ActivityPub (このエンティティがフォローしているリモート)
type v1ApFollow struct {
	ID                 string `gorm:"column:id"`
	Accepted           bool   `gorm:"column:accepted"`
	PublisherPersonURL string `gorm:"column:publisher_person_url"`
	SubscriberUserID   string `gorm:"column:subscriber_user_id"`
}

func (v1ApFollow) TableName() string { return "ap_follows" }

// ActivityPub -> concrnt (このエンティティのリモートフォロワー)
type v1ApFollower struct {
	ID                  string `gorm:"column:id"`
	SubscriberPersonURL string `gorm:"column:subscriber_person_url"`
	PublisherUserID     string `gorm:"column:publisher_user_id"`
	SubscriberInbox     string `gorm:"column:subscriber_inbox"`
}

func (v1ApFollower) TableName() string { return "ap_followers" }

// --- v2 (activitypub) destination models ---
// drizzleが作成した列に合わせる。c_dateはDBデフォルト(now())に任せる。

type v2ApEntity struct {
	ID      string `gorm:"column:id"`
	CCID    string `gorm:"column:ccid"`
	Enabled bool   `gorm:"column:enabled"`
}

func (v2ApEntity) TableName() string { return "ap_entities" }

type v2ApKey struct {
	OwnerID string `gorm:"column:owner_id"`
	KeyType string `gorm:"column:key_type"`
	Private string `gorm:"column:private"`
	Public  string `gorm:"column:public"`
}

func (v2ApKey) TableName() string { return "ap_keys" }

// v2 の ap_keys が保持する Fedify 互換 JWK の key_type
const apRsaKeyType = "RSASSA-PKCS1-v1_5"

// rsaPrivPemToJwk は v1 の PKCS#1 (または PKCS#8) RSA 秘密鍵 PEM を、
// Fedify の importJwk が読める JWK-JSON (private / public) に変換する。
func rsaPrivPemToJwk(pemStr string) (privJSON string, pubJSON string, err error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return "", "", fmt.Errorf("failed to decode PEM block")
	}

	var key *rsa.PrivateKey
	if k, e := x509.ParsePKCS1PrivateKey(block.Bytes); e == nil {
		key = k
	} else if k8, e8 := x509.ParsePKCS8PrivateKey(block.Bytes); e8 == nil {
		rsaKey, ok := k8.(*rsa.PrivateKey)
		if !ok {
			return "", "", fmt.Errorf("PKCS#8 key is not RSA")
		}
		key = rsaKey
	} else {
		return "", "", fmt.Errorf("failed to parse RSA private key: %w", e)
	}

	key.Precompute()

	b64 := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

	n := b64(key.N.Bytes())
	e := b64(big.NewInt(int64(key.E)).Bytes())

	priv := map[string]any{
		"kty":     "RSA",
		"alg":     "RS256",
		"n":       n,
		"e":       e,
		"d":       b64(key.D.Bytes()),
		"p":       b64(key.Primes[0].Bytes()),
		"q":       b64(key.Primes[1].Bytes()),
		"dp":      b64(key.Precomputed.Dp.Bytes()),
		"dq":      b64(key.Precomputed.Dq.Bytes()),
		"qi":      b64(key.Precomputed.Qinv.Bytes()),
		"key_ops": []string{"sign"},
		"ext":     true,
	}
	pub := map[string]any{
		"kty":     "RSA",
		"alg":     "RS256",
		"n":       n,
		"e":       e,
		"key_ops": []string{"verify"},
		"ext":     true,
	}

	privBytes, err := json.Marshal(priv)
	if err != nil {
		return "", "", err
	}
	pubBytes, err := json.Marshal(pub)
	if err != nil {
		return "", "", err
	}
	return string(privBytes), string(pubBytes), nil
}

func transferApEntities(fromDB, destDB *gorm.DB) {
	var entities []v1ApEntity
	fromDB.Find(&entities)
	fmt.Printf("total ap entities: %d\n", len(entities))

	entityCount, keyCount := 0, 0
	for _, e := range entities {
		// actor URI(https://<fqdn>/ap/acct/<id>)がAP上のidentityなので、
		// v1のIDの大文字小文字をそのまま保持する(小文字化すると別actorになる)
		id := e.ID

		entity := v2ApEntity{
			ID:      id,
			CCID:    strings.TrimSpace(e.CCID),
			Enabled: e.Enabled,
		}

		privJWK, pubJWK, keyErr := rsaPrivPemToJwk(e.Privatekey)

		if apDryRun {
			entityCount++
			if keyErr == nil {
				keyCount++
			} else {
				fmt.Printf("  [dry-run] key conversion failed for %s: %s\n", id, keyErr)
			}
			continue
		}

		if err := destDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&entity).Error; err != nil {
			fmt.Printf("failed to insert entity %s: %s\n", id, err)
			continue
		}
		entityCount++

		if keyErr != nil {
			// キー変換に失敗してもエンティティは移行する。
			// v2は初回アクセス時に新しい鍵を遅延生成する(署名継続性は失われる)。
			fmt.Printf("  key conversion failed for %s (will be regenerated by v2): %s\n", id, keyErr)
			continue
		}

		key := v2ApKey{
			OwnerID: id,
			KeyType: apRsaKeyType,
			Private: privJWK,
			Public:  pubJWK,
		}
		if err := destDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&key).Error; err != nil {
			fmt.Printf("failed to insert key for %s: %s\n", id, err)
			continue
		}
		keyCount++
	}

	fmt.Printf("migrated entities: %d, keys: %d\n", entityCount, keyCount)
}

// v2ブリッジのフォロー関連cckvレコード。キー・スキーマ・値は
// activitypub/src/schemas.ts の規約に一致させること。
const (
	apNamespace         = "activitypub.concrnt.world"
	apFollowSchema      = "https://schema.concrnt.world/ap/follow.json"
	apFollowerSchema    = "https://schema.concrnt.world/ap/follower.json"
	apAcceptStateSchema = "https://schema.concrnt.world/ap/accept-state.json"
)

func apHashOf(s string) string {
	return cdidv2.MakeHash([]byte(s)).String()
}

func apFollowRecordKey(userCcid, actorURI string) string {
	return fmt.Sprintf("cckv://%s/%s/follows/%s", userCcid, apNamespace, apHashOf(actorURI))
}

func apFollowerRecordKey(svcCcid, entityCcid, actorURI string) string {
	return fmt.Sprintf("cckv://%s/%s/followers/%s", svcCcid, apNamespace, apHashOf(entityCcid+"->"+actorURI))
}

func apAcceptStateRecordKey(svcCcid, entityCcid, actorURI string) string {
	return fmt.Sprintf("cckv://%s/%s/accept-states/%s", svcCcid, apNamespace, apHashOf(entityCcid+"->"+actorURI))
}

// apRecordLine は record ドキュメントを none-proof の SignedDocument 1行に変換する。
// ユーザー本人名義のレコード(follows)はCLIでは署名できないため、汎用移行と同じ
// none-proof + /api/v2/repository のインポート経路で書き込む。
func apRecordLine(key, schema string, value any, author string, createdAt time.Time) (string, error) {
	doc := concrnt.Document[any]{
		Kind:      "record",
		Key:       key,
		Value:     value,
		Author:    author,
		Schema:    schema,
		CreatedAt: createdAt,
	}
	serialized, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	sd := concrnt.SignedDocument{
		Document: string(serialized),
		Proof: concrnt.Proof{
			Type: "none",
		},
	}
	line, err := json.Marshal(sd)
	if err != nil {
		return "", err
	}
	return string(line) + "\n", nil
}

// transferApFollowRecords はv1のフォロー関係をv2のcckvレコードとして移行する。
//   - 受信フォロワー (v1 ap_followers) → follower レコード(サービスAC空間)
//   - 送信フォロー   (v1 ap_follows)   → follow レコード(ユーザー空間)
//     Accepted=true の行は accept-state レコード(accepted)も作成。
//     Accepted=false はfollowレコードのみ(レコード無し=pending。後からリモートの
//     Acceptが届けばv2ブリッジのAcceptハンドラが状態レコードを書く)。
func transferApFollowRecords(fromDB *gorm.DB) {
	const batchLines = 500

	// フォローテーブルの参照キーは ccid ではなく ApEntity.ID なので、まず対応表を作る
	var entities []v1ApEntity
	fromDB.Find(&entities)
	ccidByEntityID := make(map[string]string, len(entities))
	for _, e := range entities {
		ccidByEntityID[e.ID] = strings.TrimSpace(e.CCID)
	}

	// query APIのカーソルはcreatedAtのみ(Postgres上はµs精度)なので、同一createdAtの
	// レコードが1ページ(100件)を超えるとカーソルが前進できず、v2ブリッジの起動時
	// ロードが先頭100件で止まる。全レコードに1µsずつずらしたユニークなcreatedAtを
	// 振ってこれを避ける。再実行時はbaseが前回より新しいため、同一キーのレコードは
	// accept-if-newerで全件置き換わる(修復もこのコマンドの再実行でよい)。
	base := time.Now()
	seq := 0
	nextCreatedAt := func() time.Time {
		t := base.Add(time.Duration(seq) * time.Microsecond)
		seq++
		return t
	}

	batch := ""
	batchCount := 0
	failedCount := 0

	flush := func() {
		if batch == "" || apDryRun {
			batch = ""
			batchCount = 0
			return
		}
		results, err := commit(batch)
		if err != nil {
			fmt.Printf("failed to commit batch of %d records: %s\n", batchCount, err)
			failedCount += batchCount
		} else {
			failedCount += reportImportErrors(results)
		}
		batch = ""
		batchCount = 0
	}

	push := func(line string) {
		batch += line
		batchCount++
		if batchCount >= batchLines {
			flush()
		}
	}

	// 受信フォロワー (AP -> concrnt)
	var followers []v1ApFollower
	fromDB.Find(&followers)
	fmt.Printf("total ap followers (inbound): %d\n", len(followers))

	inboundCount, skippedCount := 0, 0
	for _, f := range followers {
		ccid, ok := ccidByEntityID[f.PublisherUserID]
		if !ok || ccid == "" {
			fmt.Printf("  skipping follower %s -> %s: unknown entity\n", f.SubscriberPersonURL, f.PublisherUserID)
			skippedCount++
			continue
		}
		if f.SubscriberInbox == "" {
			// inboxが無いフォロワーは配送先を決定できないため移行しない
			fmt.Printf("  skipping follower %s -> %s: no inbox\n", f.SubscriberPersonURL, f.PublisherUserID)
			skippedCount++
			continue
		}

		line, err := apRecordLine(
			apFollowerRecordKey(apServiceCcid, ccid, f.SubscriberPersonURL),
			apFollowerSchema,
			map[string]any{
				"ccid":     ccid,
				"actorURI": f.SubscriberPersonURL,
				"inbox":    f.SubscriberInbox,
			},
			apServiceCcid,
			nextCreatedAt(),
		)
		if err != nil {
			fmt.Printf("  failed to serialize follower %s -> %s: %s\n", f.SubscriberPersonURL, f.PublisherUserID, err)
			skippedCount++
			continue
		}
		push(line)
		inboundCount++
	}

	// 送信フォロー (concrnt -> AP)
	var follows []v1ApFollow
	fromDB.Find(&follows)
	fmt.Printf("total ap follows (outbound): %d\n", len(follows))

	outboundCount, acceptedCount := 0, 0
	for _, f := range follows {
		ccid, ok := ccidByEntityID[f.SubscriberUserID]
		if !ok || ccid == "" {
			fmt.Printf("  skipping follow %s -> %s: unknown entity\n", f.SubscriberUserID, f.PublisherPersonURL)
			skippedCount++
			continue
		}

		line, err := apRecordLine(
			apFollowRecordKey(ccid, f.PublisherPersonURL),
			apFollowSchema,
			map[string]any{
				"actorURI": f.PublisherPersonURL,
			},
			ccid,
			nextCreatedAt(),
		)
		if err != nil {
			fmt.Printf("  failed to serialize follow %s -> %s: %s\n", f.SubscriberUserID, f.PublisherPersonURL, err)
			skippedCount++
			continue
		}
		push(line)
		outboundCount++

		if !f.Accepted {
			continue
		}

		stateLine, err := apRecordLine(
			apAcceptStateRecordKey(apServiceCcid, ccid, f.PublisherPersonURL),
			apAcceptStateSchema,
			map[string]any{
				"ccid":     ccid,
				"actorURI": f.PublisherPersonURL,
				"status":   "accepted",
			},
			apServiceCcid,
			nextCreatedAt(),
		)
		if err != nil {
			fmt.Printf("  failed to serialize accept-state %s -> %s: %s\n", f.SubscriberUserID, f.PublisherPersonURL, err)
			continue
		}
		push(stateLine)
		acceptedCount++
	}

	flush()

	fmt.Printf("migrated follow records inbound: %d, outbound: %d (accepted: %d), skipped: %d, failed: %d\n",
		inboundCount, outboundCount, acceptedCount, skippedCount, failedCount)
}

var migrateApV1toV2Cmd = &cobra.Command{
	Use:   "ap-v1-to-v2",
	Short: "Migrate ActivityPub bridge entities/keys/follows from v1 to v2",
	Long: `Migrate ActivityPub bridge data from v1 (ccworld-ap-bridge) to v2 (activitypub).

Entities and RSA keys are inserted directly into the v2 bridge database (--dest-dsn).
Follow relationships are written as cckv records via POST /api/v2/repository on the
v2 core server (--dest-fqdn), because the v2 bridge keeps follows/followers as
records instead of database rows.

Run this while the v2 bridge is stopped: importing follow records while the bridge
is running would trigger its record-created handler and re-send Follow activities
to remote servers. The bridge picks the records up at next startup.

The v2 bridge's service account (--ap-ccid) must already be registered on the
destination core server.`,
	Run: func(cmd *cobra.Command, args []string) {
		fromDB, err := gorm.Open(postgres.Open(apFromDsn), &gorm.Config{})
		if err != nil {
			panic("failed to connect v1 bridge database: " + err.Error())
		}

		destDB, err := gorm.Open(postgres.Open(apDestDsn), &gorm.Config{})
		if err != nil {
			panic("failed to connect v2 bridge database: " + err.Error())
		}

		if apDryRun {
			fmt.Println("=== DRY RUN (no writes) ===")
		}

		transferApEntities(fromDB, destDB)
		transferApFollowRecords(fromDB)

		fmt.Println("done")
	},
}

func init() {
	migrateCmd.AddCommand(migrateApV1toV2Cmd)
	migrateApV1toV2Cmd.Flags().StringVar(&apFromDsn, "from-dsn", apFromDsn, "PostgreSQL DSN of the v1 ActivityPub bridge to migrate from")
	migrateApV1toV2Cmd.Flags().StringVar(&apDestDsn, "dest-dsn", apDestDsn, "PostgreSQL DSN of the v2 ActivityPub bridge to migrate to")
	migrateApV1toV2Cmd.Flags().StringVar(&destFQDN, "dest-fqdn", destFQDN, "FQDN of the v2 core server to import follow records into")
	migrateApV1toV2Cmd.Flags().StringVar(&apServiceCcid, "ap-ccid", apServiceCcid, "CCID of the v2 ActivityPub bridge's service account (author of follower/accept-state records)")
	migrateApV1toV2Cmd.Flags().StringVar(&token, "token", token, "JWT token for the destination server (auto-generated from CONCRNT_CONFIG when omitted)")
	migrateApV1toV2Cmd.Flags().BoolVar(&apDryRun, "dry-run", false, "Convert and count without writing to the destination")

	migrateApV1toV2Cmd.MarkFlagRequired("from-dsn")
	migrateApV1toV2Cmd.MarkFlagRequired("dest-dsn")
	migrateApV1toV2Cmd.MarkFlagRequired("dest-fqdn")
	migrateApV1toV2Cmd.MarkFlagRequired("ap-ccid")
}
