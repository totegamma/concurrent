package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/concrnt/concrnt"
	cdidv2 "github.com/concrnt/concrnt/cdid"
	cdidv1 "github.com/concrnt/concrnt/legacy/cdid"
	"github.com/concrnt/concrnt/legacy/core"
	"github.com/concrnt/concrnt/schemas"
)

var (
	dsn        string
	fromServer string
	fromCSID   string
	destServer string
	ignoreIDs  []string
)

const communitySchemaURL = "https://schema.concrnt.world/t/community.json"
const homeTimelineSchemaURL = "https://schema.concrnt.world/t/user.json"

var actionMap = map[string]string{
	"timeline.message.read": "net.concrnt.core.resolve",
}

func convertDomain(domain string) string {
	if domain == fromServer {
		return destServer
	}
	if domain == fromCSID {
		return destServer
	}
	return domain
}

func convertTimeline(timeline string) string {
	split := strings.Split(timeline, "@")
	if len(split) != 2 {
		return timeline
	}
	switch split[0] {
	case "world.concrnt.t-home":
		{
			return fmt.Sprintf("cckv://%s/concrnt.world/profiles/main/home-timeline", split[1])
		}
	case "world.concrnt.t-notify":
		{
			return fmt.Sprintf("cckv://%s/concrnt.world/profiles/main/notify-timeline", split[1])
		}
	case "world.concrnt.t-assoc":
		{
			return fmt.Sprintf("cckv://%s/concrnt.world/profiles/main/activity-timeline", split[1])
		}
	default:
		{

			if strings.HasPrefix(split[0], "world.concrnt.t-subhome.") {
				after := strings.TrimPrefix(split[0], "world.concrnt.t-subhome.")
				profileID := after
				return fmt.Sprintf("cckv://%s/concrnt.world/profiles/%s/home-timeline", convertDomain(split[1]), profileID)
			} else {
				return fmt.Sprintf("cckv://%s/concrnt.world/communities/%s", convertDomain(split[1]), split[0])
			}

		}
	}
}

func convertTimelines(timelines []string) []string {

	distributes := make([]string, 0, len(timelines))
	for _, timeline := range timelines {
		target := convertTimeline(timeline)
		if target != "" {
			distributes = append(distributes, target)
		}
	}
	return distributes
}

func hasSubprofileTimelien(timelines []string) (string, bool) {

	for _, timeline := range timelines {
		split := strings.Split(timeline, "@")
		if len(split) != 2 {
			continue
		}
		if strings.HasPrefix(split[0], "world.concrnt.t-subhome.") {
			after := strings.TrimPrefix(split[0], "world.concrnt.t-subhome.")
			profileID := after
			return profileID, true
		}
	}
	return "", false
}

func convertPolicy(policyURL string, policyParamsStr string, policyDefaultsStr string, timelines []string) *concrnt.Policy {

	entries := &[]concrnt.PolicyEntry{}
	if policyURL != "" {

		entry := concrnt.PolicyEntry{
			URL: &policyURL,
		}

		if policyParamsStr != "" {
			var policyParams map[string]any
			err := json.Unmarshal([]byte(policyParamsStr), &policyParams)
			if err != nil {
				fmt.Println("failed to unmarshal policy params: ", err)
			} else {
				entry.Params = &policyParams
			}
		}

		if policyDefaultsStr != "" {
			var policyDefaults map[string]string
			err := json.Unmarshal([]byte(policyDefaultsStr), &policyDefaults)
			if err != nil {
				fmt.Println("failed to unmarshal policy defaults: ", err)
			} else {
				defaults := make(map[string]string)
				for k, v := range policyDefaults {
					if action, ok := actionMap[v]; ok {
						defaults[k] = action
					} else {
						defaults[k] = v
					}
				}
				entry.Defaults = &defaults
			}
		}

		entries = &[]concrnt.PolicyEntry{entry}
	}

	return &concrnt.Policy{
		Entries:        *entries,
		VirtualParents: &timelines,
	}
}

func transferEntities(db *gorm.DB) {

	var entities []core.Entity
	db.Where("domain not in ?", ignoreIDs).Find(&entities)

	fmt.Println("total entities: ", len(entities))

	var batch string

	for _, entity := range entities {

		key := fmt.Sprintf("cckv://%s", entity.ID)

		domain := convertDomain(entity.Domain)

		v2doc := &concrnt.Document[any]{
			Key: key,
			Value: map[string]any{
				"domain": domain,
			},
			Author: entity.ID,
			Schema: "https://schema.concrnt.net/entity.json",
		}

		serializedDoc, err := json.Marshal(v2doc)
		if err != nil {
			fmt.Println("failed to serialize v2 document: ", err)
			continue
		}

		sd := concrnt.SignedDocument{
			Document: string(serializedDoc),
			Proof: concrnt.Proof{
				Type: "none",
			},
		}

		line, err := json.Marshal(sd)
		if err != nil {
			fmt.Println("failed to serialize signed document: ", err)
			continue
		}

		batch += string(line) + "\n"
	}

	resp, err := http.Post(fmt.Sprintf("https://%s/repository", destServer), "text/plain", strings.NewReader(batch))
	if err != nil {
		fmt.Println("failed to post document: ", err)
		return
	}
	resp.Body.Close()

	// print post result
	fmt.Println("traceID: ", resp.Header.Get("trace-id"))
}

func transferRecords(db *gorm.DB) {

	lastKey := uint(0)
	pageSize := 512

	keyTable := make(map[string]string) // v0id -> v2key

	for {
		var commits []core.CommitLog
		db.Where("id > ?", lastKey).
			Order("id asc").
			Limit(pageSize).
			Find(&commits)
		if len(commits) == 0 {
			break
		}

		lastKey = commits[len(commits)-1].ID

		var batch string

		for _, commit := range commits {

			// fmt.Printf("processing commit id: %d\n", commit.ID)
			// fmt.Printf("type: %s\n", commit.Type)

			var v1doc core.DocumentBase[any]
			err := json.Unmarshal([]byte(commit.Document), &v1doc)
			if err != nil {
				fmt.Println("failed to unmarshal document base: ", err)
				continue
			}

			if slices.Contains(ignoreIDs, v1doc.Signer) {
				// fmt.Printf("x")
				continue
			}

			hash := core.GetHash([]byte(commit.Document))
			hash10 := [10]byte{}
			copy(hash10[:], hash[:10])
			signedAt := v1doc.SignedAt
			cdidBase := cdidv1.New(hash10, signedAt).String()

			var v2doc *concrnt.Document[any]

			var v0id string

			switch v1doc.Type {
			case "message":
				{

					var v1msg core.MessageDocument[any]
					err := json.Unmarshal([]byte(commit.Document), &v1msg)
					if err != nil {
						fmt.Println("failed to unmarshal message document: ", err)
						continue
					}

					key := fmt.Sprintf("cckv://%s/concrnt.world/profiles/main/posts/m%s", v1msg.Signer, cdidBase)
					subprofileID, hasSubprofile := hasSubprofileTimelien(v1msg.Timelines)
					if hasSubprofile {
						key = fmt.Sprintf("cckv://%s/concrnt.world/profiles/%s/posts/m%s", v1msg.Signer, subprofileID, cdidBase)
					}

					distributes := convertTimelines(v1msg.Timelines)

					pol := convertPolicy(v1msg.Policy, v1msg.PolicyParams, v1msg.PolicyDefaults, distributes)

					v2doc = &concrnt.Document[any]{
						Key:         key,
						Value:       v1msg.Body,
						Author:      v1msg.Signer,
						Schema:      v1msg.Schema,
						CreatedAt:   v1msg.SignedAt,
						Distributes: &distributes,
						Policy:      pol,
					}

					v0id = "m" + cdidBase
				}
			case "profile":
				{
					var v1prof core.ProfileDocument[any]
					err := json.Unmarshal([]byte(commit.Document), &v1prof)
					if err != nil {
						fmt.Println("failed to unmarshal profile document: ", err)
						continue
					}

					key := fmt.Sprintf("cckv://%s/concrnt.world/profiles/main", v1prof.Signer)

					if v1prof.SemanticID != "" {
						v0id = v1prof.SemanticID
					} else if v1prof.ID != "" {
						v0id = v1prof.ID
					} else {
						v0id = "p" + cdidBase
					}

					if v1prof.SemanticID != "world.concrnt.p" {
						key = fmt.Sprintf("cckv://%s/concrnt.world/profiles/%s", v1prof.Signer, v0id)
					}

					v2doc = &concrnt.Document[any]{
						Key:       key,
						Value:     v1prof.Body,
						Author:    v1prof.Signer,
						Schema:    v1prof.Schema,
						CreatedAt: v1prof.SignedAt,
					}

				}
			case "association":
				{
					var v1ass core.AssociationDocument[any]
					err := json.Unmarshal([]byte(commit.Document), &v1ass)
					if err != nil {
						fmt.Println("failed to unmarshal association document: ", err)
						continue
					}

					target, ok := keyTable[v1ass.Target]
					if !ok {
						fmt.Printf("skipping association document with unknown target ID: %s\n", v1ass.Target)
						continue
					}

					distributes := convertTimelines(v1ass.Timelines)
					pol := convertPolicy(v1ass.Policy, v1ass.PolicyParams, v1ass.PolicyDefaults, distributes)

					var variant *string
					if v1ass.Variant != "" {
						variant = &v1ass.Variant
					}

					v2doc = &concrnt.Document[any]{
						Value:       v1ass.Body,
						Author:      v1ass.Signer,
						Schema:      v1ass.Schema,
						CreatedAt:   v1ass.SignedAt,
						Distributes: &distributes,
						Policy:      pol,

						Associate:          &target,
						AssociationVariant: variant,
					}

					v0id = "a" + cdidBase
				}
			case "timeline":
				{
					var v1tl core.TimelineDocument[any]
					err := json.Unmarshal([]byte(commit.Document), &v1tl)
					if err != nil {
						fmt.Println("failed to unmarshal timeline document: ", err)
						continue
					}

					owner := v1tl.Owner
					if owner == "" {
						owner = v1tl.Signer
					}
					if v1tl.DomainOwned {
						owner = destServer
					}

					id := "t" + cdidBase
					tlid := id /* + "@" + owner*/

					if v1tl.ID != "" {
						tlid = v1tl.ID
					}
					if v1tl.SemanticID != "" {
						tlid = v1tl.SemanticID
					}

					split := strings.Split(tlid, "@")
					if len(split) == 1 {
						tlid = tlid + "@" + owner
					}

					key := convertTimeline(tlid)
					//fmt.Println("converted timeline key: ", key)

					pol := convertPolicy(v1tl.Policy, v1tl.PolicyParams, v1tl.PolicyDefaults, []string{})

					v2doc = &concrnt.Document[any]{
						Key:       key,
						Value:     v1tl.Body,
						Author:    v1tl.Signer,
						Schema:    v1tl.Schema,
						CreatedAt: v1tl.SignedAt,
						Policy:    pol,
					}

					if v1tl.ID != "" {
						v0id = id
					} else {
						v0id = "t" + cdidBase
					}
				}
			case "subscription":
				// {"owner":"con1khzfsjl2prkfa2c7ckfsyk7hk9nd84872hvve8","signer":"con1khzfsjl2prkfa2c7ckfsyk7hk9nd84872hvve8","type":"subscription","schema":"https://schema.concrnt.world/s/list.json","body":{"name":"Home"},"signedAt":"2025-06-03T15:03:41.221Z","indexable":false}
				{
					var v1sub core.SubscriptionDocument[any]
					err := json.Unmarshal([]byte(commit.Document), &v1sub)
					if err != nil {
						fmt.Println("failed to unmarshal subscription document: ", err)
						continue
					}

					id := "s" + cdidBase
					if v1sub.ID != "" {
						id = v1sub.ID
					}

					key := fmt.Sprintf("cckv://%s/concrnt.world/profiles/main/lists/%s", v1sub.Signer, id)

					v2doc = &concrnt.Document[any]{
						Key:       key,
						Value:     v1sub.Body,
						Author:    v1sub.Signer,
						Schema:    v1sub.Schema,
						CreatedAt: v1sub.SignedAt,
					}

					v0id = "s" + cdidBase
				}
			case "subscribe":
				// {"signer":"con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2","type":"subscribe","target":"tv9x2a976tp31yt6s06b9p2axz4@ariake.concrnt.net","subscription":"sqaspcetf6xaf5hdg067y1rga3g","signedAt":"2025-05-13T08:26:16.746Z","keyID":"cck1x9ee0xf4s7qrjze4n85malrdkreqtujfzq8jqv"}
				{
					var v1sub core.SubscribeDocument[any]
					err := json.Unmarshal([]byte(commit.Document), &v1sub)
					if err != nil {
						fmt.Println("failed to unmarshal subscribe document: ", err)
						continue
					}

					target := convertTimeline(v1sub.Target)
					targetHash := cdidv2.MakeHash([]byte(target))

					key := fmt.Sprintf("cckv://%s/concrnt.world/profiles/main/lists/%s/%s", v1sub.Signer, v1sub.Subscription, targetHash.String())

					v2doc = &concrnt.Document[any]{
						Key: key,
						Value: schemas.Reference{
							Href: target,
						},
						Author:    v1sub.Signer,
						Schema:    schemas.ReferenceURL,
						CreatedAt: v1sub.SignedAt,
					}
				}
			case "unsubscribe":
				// {"signer":"con1t0tey8uxhkqkd4wcp4hd4jedt7f0vfhk29xdd2","type":"unsubscribe","target":"world.concrnt.t-home@con17hzd8gfpugmex6waxakrx3r42r2c33p6rgftna","subscription":"sg4gfzh4bqew8j7fp067v1wp9rr","signedAt":"2025-06-12T07:10:03.611Z","keyID":"cck1x9ee0xf4s7qrjze4n85malrdkreqtujfzq8jqv"}

				var v1unsub core.SubscribeDocument[any]
				err := json.Unmarshal([]byte(commit.Document), &v1unsub)
				if err != nil {
					fmt.Println("failed to unmarshal unsubscribe document: ", err)
					continue
				}

				target := convertTimeline(v1unsub.Target)
				targetHash := cdidv2.MakeHash([]byte(target))

				key := fmt.Sprintf("cckv://%s/concrnt.world/profiles/main/lists/%s/%s", v1unsub.Signer, v1unsub.Subscription, targetHash.String())

				v2doc = &concrnt.Document[any]{
					Value:     key,
					Author:    v1unsub.Signer,
					Schema:    "https://schema.concrnt.net/delete.json",
					CreatedAt: v1unsub.SignedAt,
				}

			case "delete":
				{
					var v1del core.DeleteDocument
					err := json.Unmarshal([]byte(commit.Document), &v1del)
					if err != nil {
						fmt.Println("failed to unmarshal delete document: ", err)
						continue
					}

					targetKey, ok := keyTable[v1del.Target]
					if !ok {
						fmt.Printf("skipping delete document with unknown target ID: %s\n", v1del.Target)
						continue
					}

					v2doc = &concrnt.Document[any]{
						Value:     targetKey,
						Author:    v1del.Signer,
						Schema:    "https://schema.concrnt.net/delete.json",
						CreatedAt: v1del.SignedAt,
					}
				}
			case "ack", "unack", "enact", "affiliation", "event":
				continue // skip these types for now
			default:
				{
					fmt.Printf("skipping document with unsupported type: %s\n", v1doc.Type)
					continue
				}
			}

			if v2doc == nil {
				fmt.Printf("skipping document with nil v2doc for type: %s\n", v1doc.Type)
				continue
			}

			if v0id != "" {
				keyTable[v0id] = v2doc.Key
			}

			serializedDoc, err := json.Marshal(v2doc)
			if err != nil {
				fmt.Println("failed to serialize v2 document: ", err)
				continue
			}

			sd := concrnt.SignedDocument{
				Document: string(serializedDoc),
				Proof: concrnt.Proof{
					Type: "none",
				},
			}

			line, err := json.Marshal(sd)
			if err != nil {
				fmt.Println("failed to serialize signed document: ", err)
				continue
			}

			batch += string(line) + "\n"
		}

		resp, err := http.Post(fmt.Sprintf("https://%s/repository", destServer), "text/plain", strings.NewReader(batch))
		if err != nil {
			fmt.Println("failed to post document: ", err)
			continue
		}
		resp.Body.Close()

		// print post result
		fmt.Println("traceID: ", resp.Header.Get("trace-id"))

		fmt.Println("indexed until -> ", lastKey)

		if len(commits) < pageSize { // no more commits
			break
		}

		// time.Sleep(1 * time.Second)
	}
}

var migrateV1toV2Cmd = &cobra.Command{
	Use:   "vapid",
	Short: "Generate a new VAPID key pair",
	Run: func(cmd *cobra.Command, args []string) {

		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			panic("failed to connect database")
		}

		transferEntities(db)
		transferRecords(db)
	},
}

func init() {
	migrateCmd.AddCommand(migrateV1toV2Cmd)
	migrateV1toV2Cmd.Flags().StringVar(&dsn, "dsn", dsn, "PostgreSQL DSN for the v1 database")
	migrateV1toV2Cmd.Flags().StringSliceVar(&ignoreIDs, "ignore-ccids", ignoreIDs, "List of entity IDs to ignore during migration")
	migrateV1toV2Cmd.Flags().StringVar(&fromServer, "from-fqdn", fromServer, "Domain of the v1 server to migrate from")
	migrateV1toV2Cmd.Flags().StringVar(&fromCSID, "from-csid", fromCSID, "CSID of the v1 server to migrate from")
	migrateV1toV2Cmd.Flags().StringVar(&destServer, "dest-fqdn", destServer, "Domain of the v2 server to migrate to")

	migrateV1toV2Cmd.MarkFlagRequired("dsn")
	migrateV1toV2Cmd.MarkFlagRequired("from-server")
	migrateV1toV2Cmd.MarkFlagRequired("from-csid")
	migrateV1toV2Cmd.MarkFlagRequired("dest-server")
}
