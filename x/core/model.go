package core

import (
	"fmt"
	"github.com/lib/pq"
	"strconv"
	"time"
)

type Schema struct {
	ID  uint   `json:"id" gorm:"primaryKey;auto_increment"`
	URL string `json:"url" gorm:"type:text"`
}

type Key struct {
	ID           string `json:"id" gorm:"primaryKey;type:char(42)"` // e.g. CK...
	Pubkey       string `json:"pubkey" gorm:"type:char(66)"`
	Root         string `json:"root" gorm:"type:char(42)"`
	Parent       string `json:"parent" gorm:"type:char(42)"`
	EnactPayload string `json:"enactPayload" gorm:"type:json"`
	/* type: enact
	   {
	       CKID: string,
	       root: string,
	       parent: string,
	   }
	*/
	EnactSignature string `json:"enactSignature" gorm:"type:char(130)"`
	RevokePayload  string `json:"revokePayload" gorm:"type:json;default:'null'"`
	/* type: revoke
	   {
	       CKID: string,
	   }
	*/
	RevokeSignature string    `json:"revokeSignature" gorm:"type:char(130)"`
	ValidSince      time.Time `json:"validSince" gorm:"type:timestamp with time zone"`
	ValidUntil      time.Time `json:"validUntil" gorm:"type:timestamp with time zone"`
}

// Association is one of a concurrent base object
// immutable
type Association struct {
	ID          string         `json:"id" gorm:"primaryKey;type:char(26)"`
	Author      string         `json:"author" gorm:"type:char(42);uniqueIndex:uniq_association"`
	SchemaID    uint           `json:"-" gorm:"uniqueIndex:uniq_association"`
	Schema      string         `json:"schema" gorm:"-"`
	TargetID    string         `json:"targetID" gorm:"type:char(26);uniqueIndex:uniq_association"`
	TargetType  string         `json:"targetType" gorm:"type:string;uniqueIndex:uniq_association"`
	ContentHash string         `json:"contentHash" gorm:"type:char(64);uniqueIndex:uniq_association"`
	Variant     string         `json:"variant" gorm:"type:text"`
	Payload     string         `json:"payload" gorm:"type:json"`
	Signature   string         `json:"signature" gorm:"type:char(130)"`
	CDate       time.Time      `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	Streams     pq.StringArray `json:"streams" gorm:"type:text[]"`
}

// Profile is one of a Concurrent base object
// mutable
type Profile struct {
	ID           string        `json:"id" gorm:"primaryKey;type:char(26)"`
	Author       string        `json:"author" gorm:"type:char(42)"`
	SchemaID     uint          `json:"-"`
	Schema       string        `json:"schema" gorm:"-"`
	Payload      string        `json:"payload" gorm:"type:json"`
	Signature    string        `json:"signature" gorm:"type:char(130)"`
	Associations []Association `json:"associations,omitempty" gorm:"polymorphic:Target"`
	CDate        time.Time     `json:"cdate" gorm:"->;<-:create;autoCreateTime"`
	MDate        time.Time     `json:"mdate" gorm:"autoUpdateTime"`
}

// Entity is one of a concurrent base object
// mutable
type Entity struct {
	ID                 string `json:"ccid" gorm:"type:char(42)"`
	Pubkey             string `json:"pubkey" gorm:"type:char(66)"`
	Tag                string `json:"tag" gorm:"type:text;"`
	Score              int    `json:"score" gorm:"type:integer;default:0"`
	IsScoreFixed       bool   `json:"isScoreFixed" gorm:"type:boolean;default:false"`
	AffiliationPayload string `json:"affiliationPayload" gorm:"type:json;default:'{}'"`
	/* Domain Affiliation
	   {
	       type: affiliation,
	       domain: string,
	   }
	*/
	AffiliationSignature string    `json:"affiliationSignature" gorm:"type:char(130)"`
	TombstonePayload     string    `json:"tombstonePayload" gorm:"type:json;default:'{}'"`
	TombstoneSignature   string    `json:"tombstoneSignature" gorm:"type:char(130)"`
	CDate                time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate                time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

type EntityMeta struct {
	ID      string `json:"ccid" gorm:"type:char(42)"`
	Inviter string `json:"inviter" gorm:"type:char(42)"`
	Info    string `json:"info" gorm:"type:json;default:'null'"`
}

// Address
type Address struct {
	ID       string    `json:"ccid" gorm:"type:char(42)"`
	Domain   string    `json:"domain" gorm:"type:text"`
	Score    int       `json:"score" gorm:"type:integer;default:0"`
	Payload  string    `json:"payload" gorm:"type:json;default:'{}'"`
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
	Pubkey       string    `json:"pubkey" gorm:"type:char(66)"`
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
	Payload         string         `json:"payload" gorm:"type:json"`
	Signature       string         `json:"signature" gorm:"type:char(130)"`
	CDate           time.Time      `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	Associations    []Association  `json:"associations,omitempty" gorm:"polymorphic:Target"`
	OwnAssociations []Association  `json:"ownAssociations,omitempty" gorm:"-"`
	Timelines       pq.StringArray `json:"timelines" gorm:"type:text[]"`
}

// Timeline is one of a base object of concurrent
// mutable
type Timeline struct {
	ID         string         `json:"id" gorm:"primaryKey;type:char(20);"`
	Visible    bool           `json:"visible" gorm:"type:boolean;default:false"`
	Author     string         `json:"author" gorm:"type:char(42)"`
	Maintainer pq.StringArray `json:"maintainer" gorm:"type:char(42)[];default:'{}'"`
	Writer     pq.StringArray `json:"writer" gorm:"type:char(42)[];default:'{}'"`
	Reader     pq.StringArray `json:"reader" gorm:"type:char(42)[];default:'{}'"`
	SchemaID   uint           `json:"-"`
	Schema     string         `json:"schema" gorm:"-"`
	Payload    string         `json:"payload" gorm:"type:json"`
	CDate      time.Time      `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate      time.Time      `json:"mdate" gorm:"autoUpdateTime"`
}

// TimelineItem is one of a base object of concurrent
// immutable
type TimelineItem struct {
	Type       string    `json:"type" gorm:"type:text;"`
	ObjectID   string    `json:"objectID" gorm:"primaryKey;type:char(26);"`
	TimelineID string    `json:"TimelineID" gorm:"primaryKey;type:char(20);"`
	Owner      string    `json:"owner" gorm:"type:char(42);"`
	Author     string    `json:"author,omitempty" gorm:"type:char(42);"`
	CDate      time.Time `json:"cdate,omitempty" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
}

// Collection is one of a base object of concurrent
// mutable
type Collection struct {
	ID         string           `json:"id" gorm:"primaryKey;type:char(20);"`
	Visible    bool             `json:"visible" gorm:"type:boolean;default:false"`
	Author     string           `json:"author" gorm:"type:char(42)"`
	Maintainer pq.StringArray   `json:"maintainer" gorm:"type:char(42)[];default:'{}'"`
	Writer     pq.StringArray   `json:"writer" gorm:"type:char(42)[];default:'{}'"`
	Reader     pq.StringArray   `json:"reader" gorm:"type:char(42)[];default:'{}'"`
	SchemaID   uint             `json:"-"`
	Schema     string           `json:"schema" gorm:"-"`
	CDate      time.Time        `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate      time.Time        `json:"mdate" gorm:"autoUpdateTime"`
	Items      []CollectionItem `json:"items" gorm:"foreignKey:Collection"`
}

// CollectionItem is one of a base object of concurrent
// mutable
type CollectionItem struct {
	ID         string `json:"id" gorm:"primaryKey;type:char(20);"`
	Collection string `json:"collection" gorm:"type:char(20)"`
	Payload    string `json:"payload" gorm:"type:json;default:'{}'"`
}

type Ack struct {
	From    string `json:"from" gorm:"primaryKey;type:char(42)"`
	To      string `json:"to" gorm:"primaryKey;type:char(42)"`
	Payload string `json:"payload" gorm:"type:json;default:'{}'"`
	/*
	   {
	       from: string,
	       to: string,
	   }
	*/
	Signature string `json:"signature" gorm:"type:char(130)"`
	Valid     bool   `json:"valid" gorm:"type:boolean;default:false"`
}

// Event is websocket root packet model
type Event struct {
	TimelineID string       `json:"timelineID"` // stream full id (ex: <streamID>@<domain>)
	Type       string       `json:"type"`
	Action     string       `json:"action"`
	Item       TimelineItem `json:"item"`
	Body       interface{}  `json:"body"`
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
