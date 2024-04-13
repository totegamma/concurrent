package core

import (
	"fmt"
	"strconv"
	"time"

	"github.com/lib/pq"
)

type Schema struct {
	ID  uint   `json:"id" gorm:"primaryKey;auto_increment"`
	URL string `json:"url" gorm:"type:text"`
}

type Key struct {
	ID              string    `json:"id" gorm:"primaryKey;type:char(42)"` // e.g. CK...
	Root            string    `json:"root" gorm:"type:char(42)"`
	Parent          string    `json:"parent" gorm:"type:char(42)"`
	EnactDocument   string    `json:"enactDocument" gorm:"type:json"`
	EnactSignature  string    `json:"enactSignature" gorm:"type:char(130)"`
	RevokeDocument  string    `json:"revokeDocument" gorm:"type:json;default:'null'"`
	RevokeSignature string    `json:"revokeSignature" gorm:"type:char(130)"`
	ValidSince      time.Time `json:"validSince" gorm:"type:timestamp with time zone"`
	ValidUntil      time.Time `json:"validUntil" gorm:"type:timestamp with time zone"`
}

type SemanticID struct {
	ID        string    `json:"id" gorm:"primaryKey;type:text"`
	Owner     string    `json:"owner" gorm:"primaryKey;type:char(42)"`
	Target    string    `json:"target" gorm:"type:char(27)"`
	Document  string    `json:"document" gorm:"type:json"`
	Signature string    `json:"signature" gorm:"type:char(130)"`
	CDate     time.Time `json:"cdate" gorm:"->;<-:create;autoCreateTime"`
	MDate     time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

// Association is one of a concurrent base object
// immutable
type Association struct {
	ID        string         `json:"id" gorm:"primaryKey;type:char(26)"`
	Author    string         `json:"author" gorm:"type:char(42);uniqueIndex:uniq_association"`
	Owner     string         `json:"owner" gorm:"type:char(42);"`
	SchemaID  uint           `json:"-" gorm:"uniqueIndex:uniq_association"`
	Schema    string         `json:"schema" gorm:"-"`
	TargetID  string         `json:"targetID" gorm:"type:char(27);uniqueIndex:uniq_association"`
	Variant   string         `json:"variant" gorm:"type:text;uniqueIndex:uniq_association"`
	Document  string         `json:"document" gorm:"type:json"`
	Signature string         `json:"signature" gorm:"type:char(130)"`
	CDate     time.Time      `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	Timelines pq.StringArray `json:"timelines" gorm:"type:text[]"`
}

// Profile is one of a Concurrent base object
// mutable
type Profile struct {
	ID           string        `json:"id" gorm:"primaryKey;type:char(26)"`
	Author       string        `json:"author" gorm:"type:char(42)"`
	SchemaID     uint          `json:"-"`
	Schema       string        `json:"schema" gorm:"-"`
	Document     string        `json:"document" gorm:"type:json"`
	Signature    string        `json:"signature" gorm:"type:char(130)"`
	Associations []Association `json:"associations,omitempty" gorm:"-"`
	CDate        time.Time     `json:"cdate" gorm:"->;<-:create;autoCreateTime"`
	MDate        time.Time     `json:"mdate" gorm:"autoUpdateTime"`
}

// Entity is one of a concurrent base object
// mutable
type Entity struct {
	ID                   string    `json:"ccid" gorm:"type:char(42)"`
	Tag                  string    `json:"tag" gorm:"type:text;"`
	Score                int       `json:"score" gorm:"type:integer;default:0"`
	IsScoreFixed         bool      `json:"isScoreFixed" gorm:"type:boolean;default:false"`
	AffiliationDocument  string    `json:"affiliationDocument" gorm:"type:json;default:'{}'"`
	AffiliationSignature string    `json:"affiliationSignature" gorm:"type:char(130)"`
	TombstoneDocument    *string   `json:"tombstoneDocument" gorm:"type:json;default:'null'"`
	TombstoneSignature   *string   `json:"tombstoneSignature" gorm:"type:char(130)"`
	CDate                time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate                time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

type EntityMeta struct {
	ID        string  `json:"ccid" gorm:"type:char(42)"`
	Inviter   *string `json:"inviter" gorm:"type:char(42)"`
	Info      string  `json:"info" gorm:"type:json;default:'null'"`
	Signature string  `json:"signature" gorm:"type:char(130)"`
}

// Address
type Address struct {
	ID       string    `json:"ccid" gorm:"type:char(42)"`
	Domain   string    `json:"domain" gorm:"type:text"`
	Score    int       `json:"score" gorm:"type:integer;default:0"`
	Document string    `json:"document" gorm:"type:json;default:'{}'"`
	SignedAt time.Time `json:"validFrom" gorm:"type:timestamp with time zone"`
	CDate    time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
}

// Domain is one of a concurrent base object
// mutable
type Domain struct {
	ID           string    `json:"fqdn" gorm:"type:text"` // FQDN
	CCID         string    `json:"ccid" gorm:"type:char(42)"`
	Tag          string    `json:"tag" gorm:"type:text;default:default"`
	Score        int       `json:"score" gorm:"type:integer;default:0"`
	IsScoreFixed bool      `json:"isScoreFixed" gorm:"type:boolean;default:false"`
	DimensionID  string    `json:"dimensionID" gorm:"type:text"`
	CDate        time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate        time.Time `json:"mdate" gorm:"autoUpdateTime"`
	LastScraped  time.Time `json:"lastScraped" gorm:"type:timestamp with time zone"`
}

// Message is one of a concurrent base object
// immutable
type Message struct {
	ID              string         `json:"id" gorm:"primaryKey;type:char(26)"`
	Author          string         `json:"author" gorm:"type:char(42)"`
	SchemaID        uint           `json:"-"`
	Schema          string         `json:"schema" gorm:"-"`
	Document        string         `json:"document" gorm:"type:json"`
	Signature       string         `json:"signature" gorm:"type:char(130)"`
	CDate           time.Time      `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	Associations    []Association  `json:"associations,omitempty" gorm:"-"`
	OwnAssociations []Association  `json:"ownAssociations,omitempty" gorm:"-"`
	Timelines       pq.StringArray `json:"timelines" gorm:"type:text[]"`
}

// Timeline is one of a base object of concurrent
// mutable
type Timeline struct {
	ID          string    `json:"id" gorm:"primaryKey;type:char(26);"`
	Indexable   bool      `json:"indexable" gorm:"type:boolean;default:false"`
	Author      string    `json:"author" gorm:"type:char(42)"`
	DomainOwned bool      `json:"domainOwned" gorm:"type:boolean;default:false"`
	SchemaID    uint      `json:"-"`
	Schema      string    `json:"schema" gorm:"-"`
	Document    string    `json:"document" gorm:"type:json"`
	Signature   string    `json:"signature" gorm:"type:char(130)"`
	CDate       time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate       time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

// TimelineItem is one of a base object of concurrent
// immutable
type TimelineItem struct {
	ObjectID   string    `json:"objectID" gorm:"primaryKey;type:char(27);"`
	TimelineID string    `json:"timelineID" gorm:"primaryKey;type:char(26);"`
	Owner      string    `json:"owner" gorm:"type:char(42);"`
	Author     *string   `json:"author,omitempty" gorm:"type:char(42);"`
	CDate      time.Time `json:"cdate,omitempty" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
}

// Collection is one of a base object of concurrent
// mutable
type Collection struct {
	ID          string           `json:"id" gorm:"primaryKey;type:char(26);"`
	Indexable   bool             `json:"indexable" gorm:"type:boolean;default:false"`
	Author      string           `json:"author" gorm:"type:char(42)"`
	DomainOwned bool             `json:"domainOwned" gorm:"type:boolean;default:false"`
	SchemaID    uint             `json:"-"`
	Schema      string           `json:"schema" gorm:"-"`
	CDate       time.Time        `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate       time.Time        `json:"mdate" gorm:"autoUpdateTime"`
	Items       []CollectionItem `json:"items" gorm:"foreignKey:Collection"`
}

// CollectionItem is one of a base object of concurrent
// mutable
type CollectionItem struct {
	ID         string `json:"id" gorm:"primaryKey;type:char(26);"`
	Collection string `json:"collection" gorm:"type:char(26)"`
	Document   string `json:"document" gorm:"type:json;default:'{}'"`
	Signature  string `json:"signature" gorm:"type:char(130)"`
}

type Ack struct {
	From      string `json:"from" gorm:"primaryKey;type:char(42)"`
	To        string `json:"to" gorm:"primaryKey;type:char(42)"`
	Document  string `json:"document" gorm:"type:json;default:'{}'"`
	Signature string `json:"signature" gorm:"type:char(130)"`
	Valid     bool   `json:"valid" gorm:"type:boolean;default:false"`
}

// Subscription
type Subscription struct {
	ID          string             `json:"id" gorm:"primaryKey;type:char(26)"`
	Author      string             `json:"author" gorm:"type:char(42);"`
	Indexable   bool               `json:"indexable" gorm:"type:boolean;default:false"`
	DomainOwned bool               `json:"domainOwned" gorm:"type:boolean;default:false"`
	SchemaID    uint               `json:"-"`
	Schema      string             `json:"schema" gorm:"-"`
	Document    string             `json:"document" gorm:"type:json;default:'{}'"`
	Signature   string             `json:"signature" gorm:"type:char(130)"`
	Items       []SubscriptionItem `json:"items" gorm:"foreignKey:Subscription"`
	CDate       time.Time          `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate       time.Time          `json:"mdate" gorm:"autoUpdateTime"`
}

type ResolverType uint

const (
	ResolverTypeEntity ResolverType = iota
	ResolverTypeDomain
)

type SubscriptionItem struct {
	ID           string       `json:"id" gorm:"primaryKey;type:text;"`
	ResolverType ResolverType `json:"resolverType" gorm:"type:integer"`
	Entity       *string      `json:"entity" gorm:"type:char(42);"`
	Domain       *string      `json:"domain" gorm:"type:text;"`
	Subscription string       `json:"subscription" gorm:"type:char(26)"`
}

// Event is websocket root packet model
type Event struct {
	TimelineID string       `json:"timelineID"` // stream full id (ex: <streamID>@<domain>)
	Type       string       `json:"type"`
	Action     string       `json:"action"`
	Item       TimelineItem `json:"item"`
	Document   string       `json:"document"`
	Signature  string       `json:"signature"`
}

type UserKV struct {
	Owner string `json:"owner" gorm:"primaryKey;type:char(42)"`
	Key   string `json:"key" gorm:"primaryKey;type:text"`
	Value string `json:"value" gorm:"type:text"`
}

type Chunk struct {
	Key   string         `json:"key"`
	Items []TimelineItem `json:"items"`
}

func Time2Chunk(t time.Time) string {
	// chunk by 10 minutes
	return fmt.Sprintf("%d", (t.Unix()/600)*600)
}

func Chunk2RecentTime(chunk string) time.Time {
	i, _ := strconv.ParseInt(chunk, 10, 64)
	return time.Unix(i+600, 0)
}

func Chunk2ImmediateTime(chunk string) time.Time {
	i, _ := strconv.ParseInt(chunk, 10, 64)
	return time.Unix(i, 0)
}

func TypedIDToType(id string) string {
	if len(id) != 27 {
		return ""
	}
	prefix := id[0]
	switch prefix {
	case 'a':
		return "association"
	case 'm':
		return "message"
	default:
		return ""
	}
}

func hasChar(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

func IsCKID(keyID string) bool {
	return keyID[:3] == "cck" && len(keyID) == 42 && !hasChar(keyID, '.')
}

func IsCCID(keyID string) bool {
	return keyID[:3] == "con" && len(keyID) == 42 && !hasChar(keyID, '.')
}
