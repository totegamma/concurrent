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

	"github.com/lib/pq"
	"github.com/spf13/cobra"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	apFromDsn string
	apDestDsn string
	apDryRun  bool
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
	ID              string         `gorm:"column:id"`
	CCID            string         `gorm:"column:ccid"`
	ListenTimelines pq.StringArray `gorm:"column:listen_timelines;type:text[]"`
	Enabled         bool           `gorm:"column:enabled"`
}

func (v2ApEntity) TableName() string { return "ap_entities" }

type v2ApKey struct {
	OwnerID string `gorm:"column:owner_id"`
	KeyType string `gorm:"column:key_type"`
	Private string `gorm:"column:private"`
	Public  string `gorm:"column:public"`
}

func (v2ApKey) TableName() string { return "ap_keys" }

type v2ApFollow struct {
	Accepted              bool    `gorm:"column:accepted"`
	PublisherID           string  `gorm:"column:publisher_id"`
	SubscriberID          string  `gorm:"column:subscriber_id"`
	SubscriberInbox       *string `gorm:"column:subscriber_inbox"`
	SubscriberSharedInbox *string `gorm:"column:subscriber_shared_inbox"`
}

func (v2ApFollow) TableName() string { return "ap_follows" }

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
		id := strings.ToLower(e.ID)

		entity := v2ApEntity{
			ID:              id,
			CCID:            strings.TrimSpace(e.CCID),
			ListenTimelines: pq.StringArray{},
			Enabled:         e.Enabled,
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

func transferApFollows(fromDB, destDB *gorm.DB) {
	// 受信フォロワー (AP -> concrnt): v2は publisher=ローカルusername, subscriber=リモートURL
	var followers []v1ApFollower
	fromDB.Find(&followers)
	fmt.Printf("total ap followers (inbound): %d\n", len(followers))

	inboundCount := 0
	for _, f := range followers {
		inbox := f.SubscriberInbox
		row := v2ApFollow{
			Accepted:        true,
			PublisherID:     strings.ToLower(f.PublisherUserID),
			SubscriberID:    f.SubscriberPersonURL,
			SubscriberInbox: &inbox,
		}
		if apDryRun {
			inboundCount++
			continue
		}
		if err := destDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			fmt.Printf("failed to insert follower %s->%s: %s\n", f.SubscriberPersonURL, f.PublisherUserID, err)
			continue
		}
		inboundCount++
	}

	// 送信フォロー (concrnt -> AP): v2は publisher=リモートURL, subscriber=ローカルusername
	var follows []v1ApFollow
	fromDB.Find(&follows)
	fmt.Printf("total ap follows (outbound): %d\n", len(follows))

	outboundCount := 0
	for _, f := range follows {
		row := v2ApFollow{
			Accepted:     f.Accepted,
			PublisherID:  f.PublisherPersonURL,
			SubscriberID: strings.ToLower(f.SubscriberUserID),
		}
		if apDryRun {
			outboundCount++
			continue
		}
		if err := destDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			fmt.Printf("failed to insert follow %s->%s: %s\n", f.SubscriberUserID, f.PublisherPersonURL, err)
			continue
		}
		outboundCount++
	}

	fmt.Printf("migrated follows inbound: %d, outbound: %d\n", inboundCount, outboundCount)
}

var migrateApV1toV2Cmd = &cobra.Command{
	Use:   "ap-v1-to-v2",
	Short: "Migrate ActivityPub bridge entities/keys/follows from v1 to v2",
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
		transferApFollows(fromDB, destDB)

		fmt.Println("done")
	},
}

func init() {
	migrateCmd.AddCommand(migrateApV1toV2Cmd)
	migrateApV1toV2Cmd.Flags().StringVar(&apFromDsn, "from-dsn", apFromDsn, "PostgreSQL DSN of the v1 ActivityPub bridge to migrate from")
	migrateApV1toV2Cmd.Flags().StringVar(&apDestDsn, "dest-dsn", apDestDsn, "PostgreSQL DSN of the v2 ActivityPub bridge to migrate to")
	migrateApV1toV2Cmd.Flags().BoolVar(&apDryRun, "dry-run", false, "Convert and count without writing to the destination")

	migrateApV1toV2Cmd.MarkFlagRequired("from-dsn")
	migrateApV1toV2Cmd.MarkFlagRequired("dest-dsn")
}
