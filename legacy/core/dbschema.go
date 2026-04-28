package core

import (
	"github.com/lib/pq"
	"time"
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
	RevokeDocument  *string   `json:"revokeDocument" gorm:"type:json;default:null"`
	RevokeSignature *string   `json:"revokeSignature" gorm:"type:char(130);default:null"`
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
	Author    string         `json:"author" gorm:"type:char(42)"`
	Owner     string         `json:"owner" gorm:"type:char(42)"`
	SchemaID  uint           `json:"-"`
	Schema    string         `json:"schema" gorm:"-"`
	Target    string         `json:"target" gorm:"type:char(27)"`
	Variant   string         `json:"variant" gorm:"type:text"`
	Unique    string         `json:"unique" gorm:"type:char(32);uniqueIndex:uniq_association"`
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
	PolicyID     uint          `json:"-"`
	Policy       string        `json:"policy,omitempty" gorm:"-"`
	PolicyParams *string       `json:"policyParams,omitempty" gorm:"type:json"`
	CDate        time.Time     `json:"cdate" gorm:"->;<-:create;autoCreateTime"`
	MDate        time.Time     `json:"mdate" gorm:"autoUpdateTime"`
}

// Entity is one of a concurrent base object
// mutable
type Entity struct {
	ID                   string    `json:"ccid" gorm:"type:char(42)"`
	Domain               string    `json:"domain" gorm:"type:text"`
	Tag                  string    `json:"tag" gorm:"type:text;"`
	Score                int       `json:"score" gorm:"type:integer;default:0"`
	IsScoreFixed         bool      `json:"isScoreFixed" gorm:"type:boolean;default:false"`
	AffiliationDocument  string    `json:"affiliationDocument" gorm:"type:json"`
	AffiliationSignature string    `json:"affiliationSignature" gorm:"type:char(130)"`
	TombstoneDocument    *string   `json:"tombstoneDocument" gorm:"type:json;default:null"`
	TombstoneSignature   *string   `json:"tombstoneSignature" gorm:"type:char(130);default:null"`
	Alias                *string   `json:"alias,omitempty" gorm:"type:text"`
	CDate                time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate                time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

type EntityMeta struct {
	ID      string  `json:"ccid" gorm:"type:char(42)"`
	Inviter *string `json:"inviter" gorm:"type:char(42)"`
	Info    string  `json:"info" gorm:"type:json;default:'null'"`
}

// Domain is one of a concurrent base object
// mutable
type Domain struct {
	ID           string      `json:"fqdn" gorm:"type:text"` // FQDN
	CCID         string      `json:"ccid" gorm:"type:char(42)"`
	CSID         string      `json:"csid" gorm:"type:char(42)"`
	Tag          string      `json:"tag" gorm:"type:text"`
	Score        int         `json:"score" gorm:"type:integer;default:0"`
	Meta         interface{} `json:"meta" gorm:"-"`
	IsScoreFixed bool        `json:"isScoreFixed" gorm:"type:boolean;default:false"`
	Dimension    string      `json:"dimension" gorm:"-"`
	CDate        time.Time   `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate        time.Time   `json:"mdate" gorm:"autoUpdateTime"`
	LastScraped  time.Time   `json:"lastScraped" gorm:"type:timestamp with time zone"`
}

// Message is one of a concurrent base object
// immutable
type Message struct {
	ID              string         `json:"id" gorm:"primaryKey;type:char(26)"`
	Author          string         `json:"author" gorm:"type:char(42)"`
	SchemaID        uint           `json:"-"`
	Schema          string         `json:"schema" gorm:"-"`
	PolicyID        uint           `json:"-"`
	Policy          string         `json:"policy,omitempty" gorm:"-"`
	PolicyParams    *string        `json:"policyParams,omitempty" gorm:"type:json"`
	PolicyDefaults  *string        `json:"policyDefaults,omitempty" gorm:"type:json"`
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
	ID           string    `json:"id" gorm:"primaryKey;type:char(26);"`
	Indexable    bool      `json:"indexable" gorm:"type:boolean;default:false"`
	Owner        string    `json:"owner" gorm:"type:char(42)"`
	Author       string    `json:"author" gorm:"type:char(42)"`
	SchemaID     uint      `json:"-"`
	Schema       string    `json:"schema" gorm:"-"`
	PolicyID     uint      `json:"-"`
	Policy       string    `json:"policy,omitempty" gorm:"-"`
	PolicyParams *string   `json:"policyParams,omitempty" gorm:"type:json"`
	Document     string    `json:"document" gorm:"type:json"`
	Signature    string    `json:"signature" gorm:"type:char(130)"`
	CDate        time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate        time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

// TimelineItem is one of a base object of concurrent
// immutable
type TimelineItem struct {
	ResourceID string    `json:"resourceID" gorm:"primaryKey;type:char(27);"`
	TimelineID string    `json:"timelineID" gorm:"primaryKey;type:char(26);index:idx_timeline_id_c_date"`
	Owner      string    `json:"owner" gorm:"type:char(42);"`
	Author     *string   `json:"author,omitempty" gorm:"type:char(42);"`
	SchemaID   uint      `json:"-"`
	Schema     string    `json:"schema,omitempty" gorm:"-"`
	CDate      time.Time `json:"cdate,omitempty" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp();index:idx_timeline_id_c_date"`
}

type Ack struct {
	From      string `json:"from" gorm:"primaryKey;type:char(42)"`
	To        string `json:"to" gorm:"primaryKey;type:char(42)"`
	Document  string `json:"document" gorm:"type:json"`
	Signature string `json:"signature" gorm:"type:char(130)"`
	Valid     bool   `json:"valid" gorm:"type:boolean;default:false"`
}

// Subscription
type Subscription struct {
	ID           string             `json:"id" gorm:"primaryKey;type:char(26)"`
	Owner        string             `json:"owner" gorm:"type:char(42)"`
	Author       string             `json:"author" gorm:"type:char(42);"`
	Indexable    bool               `json:"indexable" gorm:"type:boolean;default:false"`
	SchemaID     uint               `json:"-"`
	Schema       string             `json:"schema" gorm:"-"`
	PolicyID     uint               `json:"-"`
	Policy       string             `json:"policy,omitempty" gorm:"-"`
	PolicyParams *string            `json:"policyParams,omitempty" gorm:"type:json"`
	Document     string             `json:"document" gorm:"type:json"`
	Signature    string             `json:"signature" gorm:"type:char(130)"`
	Items        []SubscriptionItem `json:"items" gorm:"foreignKey:Subscription"`
	CDate        time.Time          `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate        time.Time          `json:"mdate" gorm:"autoUpdateTime"`

	DomainOwned bool `json:"domainOwned" gorm:"type:boolean;default:false"`
}

type ResolverType uint

const (
	ResolverTypeEntity ResolverType = iota
	ResolverTypeDomain
)

type SubscriptionItem struct {
	ID           string       `json:"id" gorm:"primaryKey;type:text;"`
	Subscription string       `json:"subscription" gorm:"primaryKey;type:char(26)"`
	ResolverType ResolverType `json:"resolverType" gorm:"type:integer"`
	Entity       *string      `json:"entity" gorm:"type:char(42);"`
	Domain       *string      `json:"domain" gorm:"type:text;"`
}

type UserKV struct {
	Owner string `json:"owner" gorm:"primaryKey;type:char(42)"`
	Key   string `json:"key" gorm:"primaryKey;type:text"`
	Value string `json:"value" gorm:"type:text"`
}

type Job struct {
	ID          string    `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Author      string    `json:"author" gorm:"type:char(42)"`
	Type        string    `json:"type" gorm:"type:text"`
	Payload     string    `json:"payload" gorm:"type:json"`
	Scheduled   time.Time `json:"scheduled" gorm:"type:timestamp with time zone"`
	Status      string    `json:"status" gorm:"type:text"` // pending, running, completed, failed
	Result      string    `json:"result" gorm:"type:text"`
	CreatedAt   time.Time `json:"createdAt" gorm:"autoCreateTime"`
	CompletedAt time.Time `json:"completedAt" gorm:"autoUpdateTime"`
	TraceID     string    `json:"traceID" gorm:"type:text"`
}

type CommitOwner struct {
	ID          uint   `json:"id" gorm:"primaryKey;auto_increment"`
	CommitLogID uint   `json:"commitLogID" gorm:"index;uniqueIndex:idx_commit_owner"`
	Owner       string `json:"owner" gorm:"type:char(42);index;uniqueIndex:idx_commit_owner"`
}

type CommitLog struct {
	ID           uint          `json:"id" gorm:"primaryKey;auto_increment"`
	IP           string        `json:"ip" gorm:"type:text"`
	DocumentID   string        `json:"documentID" gorm:"type:char(26);uniqueIndex:idx_document_id"`
	IsEphemeral  bool          `json:"isEphemeral" gorm:"type:boolean;default:false"`
	Type         string        `json:"type" gorm:"type:text"`
	Document     string        `json:"document" gorm:"type:json"`
	Signature    string        `json:"signature" gorm:"type:char(130)"`
	SignedAt     time.Time     `json:"signedAt" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
	CommitOwners []CommitOwner `json:"commitOwners" gorm:"foreignKey:CommitLogID"`
	Owners       []string      `json:"owners" gorm:"-"`
	CDate        time.Time     `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

type NotificationSubscription struct {
	VendorID     string         `json:"vendorID" gorm:"primaryKey;type:text"`
	Owner        string         `json:"owner" gorm:"primaryKey;type:text"`
	Schemas      pq.StringArray `json:"schemas" gorm:"type:text[]"`
	Timelines    pq.StringArray `json:"timelines" gorm:"type:text[]"`
	Subscription string         `json:"subscription" gorm:"type:text"`
	CDate        time.Time      `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate        time.Time      `json:"mdate" gorm:"autoUpdateTime"`
}
