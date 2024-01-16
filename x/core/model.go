package core

import (
	"github.com/lib/pq"
	"time"
	"fmt"
	"strconv"
)

type SignedObject struct {
	Signer   string      `json:"signer"`
	Type     string      `json:"type"`
	Schema   string      `json:"schema,omitempty"`
    KeyID    string      `json:"keyID"`
	Body     interface{} `json:"body"`
	Meta     interface{} `json:"meta,omitempty"`
	SignedAt time.Time   `json:"signedAt"`
}

/*
# SignedObject Body Schemas

## Entity
### Domain Affiliation
{
    domain: string,
}
*/

// Association is one of a concurrent base object
// immutable
type Association struct {
	ID          string         `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Author      string         `json:"author" gorm:"type:char(42);uniqueIndex:uniq_association"`
	Schema      string         `json:"schema"  gorm:"type:text;uniqueIndex:uniq_association"`
	TargetID    string         `json:"targetID" gorm:"type:uuid;uniqueIndex:uniq_association"`
	TargetType  string         `json:"targetType" gorm:"type:string;uniqueIndex:uniq_association"`
	ContentHash string         `json:"contentHash" gorm:"type:char(64);uniqueIndex:uniq_association"`
	Variant	    string         `json:"variant" gorm:"type:text"`
	Payload     string         `json:"payload" gorm:"type:json"`
	Signature   string         `json:"signature" gorm:"type:char(130)"`
	CDate       time.Time      `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	Streams     pq.StringArray `json:"streams" gorm:"type:text[]"`
}

// Character is one of  a Concurrent base object
// mutable
type Character struct {
	ID           string        `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Author       string        `json:"author" gorm:"type:char(42)"`
	Schema       string        `json:"schema" gorm:"type:text"`
	Payload      string        `json:"payload" gorm:"type:json"`
	Signature    string        `json:"signature" gorm:"type:char(130)"`
	Associations []Association `json:"associations,omitempty" gorm:"polymorphic:Target"`
	CDate        time.Time     `json:"cdate" gorm:"->;<-:create;autoCreateTime"`
	MDate        time.Time     `json:"mdate" gorm:"autoUpdateTime"`
}

// Entity is one of a concurrent base object
// mutable
type Entity struct {
	ID        string    `json:"ccid" gorm:"type:char(42)"`
	Tag       string    `json:"tag" gorm:"type:text;"`
	Score     int       `json:"score" gorm:"type:integer;default:0"`
    Fixed     bool      `json:"fixed" gorm:"type:boolean;default:false"`
	Acking    Ack       `json:"acking" gorm:"foreignKey:From"`
	Acker     Ack       `json:"acker" gorm:"foreignKey:To"`
    Payload   string    `json:"payload" gorm:"type:json;default:'{}'"`
    Signature string    `json:"signature" gorm:"type:char(130)"`
	CDate     time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate     time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

type EntityMeta struct {
	ID      string    `json:"ccid" gorm:"type:char(42)"`
	Inviter string    `json:"inviter" gorm:"type:char(42)"`
	Info    string    `json:"info" gorm:"type:json;default:'null'"`
}

// Address
type Address struct {
    ID     string    `json:"ccid" gorm:"type:char(42)"`
    Domain string    `json:"domain" gorm:"type:text"`
	CDate   time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate   time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

// Domain is one of a concurrent base object
// mutable
type Domain struct {
	ID          string    `json:"fqdn" gorm:"type:text"` // FQDN
	CCID        string    `json:"ccid" gorm:"type:char(42)"`
	Tag         string    `json:"tag" gorm:"type:text;default:default"`
	Score       int       `json:"score" gorm:"type:integer;default:0"`
	Pubkey      string    `json:"pubkey" gorm:"type:text"`
	CDate       time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate       time.Time `json:"mdate" gorm:"autoUpdateTime"`
	LastScraped time.Time `json:"lastScraped" gorm:"type:timestamp with time zone"`
}

// Message is one of a concurrent base object
// immutable
type Message struct {
	ID           string         `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Author       string         `json:"author" gorm:"type:char(42)"`
	Schema       string         `json:"schema" gorm:"type:text"`
	Payload      string         `json:"payload" gorm:"type:json"`
	Signature    string         `json:"signature" gorm:"type:char(130)"`
	CDate        time.Time      `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	Associations []Association  `json:"associations,omitempty" gorm:"polymorphic:Target"`
	OwnAssociations []Association `json:"ownAssociations,omitempty" gorm:"-"`
	Streams      pq.StringArray `json:"streams" gorm:"type:text[]"`
}

// Stream is one of a base object of concurrent
// mutable
type Stream struct {
	ID         string         `json:"id" gorm:"primaryKey;type:char(20);"`
	Visible    bool           `json:"visible" gorm:"type:boolean;default:false"`
	Author     string         `json:"author" gorm:"type:char(42)"`
	Maintainer pq.StringArray `json:"maintainer" gorm:"type:char(42)[];default:'{}'"`
	Writer     pq.StringArray `json:"writer" gorm:"type:char(42)[];default:'{}'"`
	Reader     pq.StringArray `json:"reader" gorm:"type:char(42)[];default:'{}'"`
	Schema     string         `json:"schema" gorm:"type:text"`
	Payload    string         `json:"payload" gorm:"type:json"`
	CDate      time.Time      `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate      time.Time      `json:"mdate" gorm:"autoUpdateTime"`
}

// StreamItem is one of a base object of concurrent
// immutable
type StreamItem struct {
	Type     string    `json:"type" gorm:"type:text;"`
	ObjectID string    `json:"objectID" gorm:"primaryKey;type:uuid;"`
	StreamID string    `json:"streamID" gorm:"primaryKey;type:char(20);"`
	Owner    string    `json:"owner" gorm:"type:char(42);"`
	Author   string    `json:"author,omitempty" gorm:"type:char(42);"`
	CDate    time.Time `json:"cdate,omitempty" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
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
	Schema     string           `json:"schema" gorm:"type:text"`
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
	From      string `json:"from" gorm:"primaryKey;type:char(42)"`
	To        string `json:"to" gorm:"primaryKey;type:char(42)"`
	Payload   string `json:"payload" gorm:"type:json;default:'{}'"`
	Signature string `json:"signature" gorm:"type:char(130)"`
}

// Event is websocket root packet model
type Event struct {
	Stream string          `json:"stream"` // stream full id (ex: <streamID>@<domain>)
	Type   string          `json:"type"`
	Action string          `json:"action"`
	Item   StreamItem `json:"item"`
	Body   interface{}     `json:"body"`
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
