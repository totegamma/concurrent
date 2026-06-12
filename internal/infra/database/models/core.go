package models

import (
	"time"

	"github.com/lib/pq"
)

type CommitOwner struct {
	CommitLogID string    `json:"commit_log_id" gorm:"type:text;primaryKey"`
	CommitLog   CommitLog `json:"-" gorm:"constraint:OnDelete:CASCADE;"`
	Owner       string    `json:"owner" gorm:"type:text;primaryKey"`
}

type CommitLog struct {
	ID          string    `json:"id" gorm:"primaryKey;type:text"`
	IP          string    `json:"ip" gorm:"type:text"`
	Document    string    `json:"document" gorm:"type:text"`
	Proof       string    `json:"proof" gorm:"type:text"`
	GcCandidate bool      `json:"gcCandidate" gorm:"type:boolean;not null;default:false;index"`
	CDate       time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

type RecordKey struct {
	ID              int64      `json:"id" gorm:"primaryKey;autoIncrement"`
	ParentID        *int64     `json:"parentID" gorm:"index:idx_record_keys_parent_id_record_id,priority:1;index:idx_record_keys_parent_created_at_record_id,priority:1,where:parent_id IS NOT NULL AND record_created_at IS NOT NULL"`
	URI             string     `json:"uri" gorm:"type:text;unique"`
	RecordID        *string    `json:"recordID" gorm:"type:text;uniqueIndex;index:idx_record_keys_parent_id_record_id,priority:2;index:idx_record_keys_parent_created_at_record_id,priority:3,where:parent_id IS NOT NULL AND record_created_at IS NOT NULL"`
	Record          Record     `json:"record" gorm:"foreignKey:RecordID;references:DocumentID;constraint:OnDelete:CASCADE;"`
	RecordCreatedAt *time.Time `json:"recordCreatedAt,omitempty" gorm:"type:timestamp with time zone;index:idx_record_keys_parent_created_at_record_id,priority:2,sort:desc,where:parent_id IS NOT NULL AND record_created_at IS NOT NULL"`
	CleanOnUpdate   bool       `json:"cleanOnUpdate" gorm:"type:boolean;not null;default:false"`
}

type Record struct {
	DocumentID    string         `json:"id" gorm:"primaryKey;type:text;index:idx_records_schema_created_at_document_id,priority:3;index:idx_records_document_id_created_at,priority:1"`
	Document      CommitLog      `json:"documnet" gorm:"foreignKey:DocumentID;references:ID;constraint:OnDelete:CASCADE;"`
	Owner         string         `json:"owner" gorm:"type:text"`
	Redirect      *string        `json:"redirect" gorm:"type:text"`
	Schema        string         `json:"schema" gorm:"type:text;index:idx_records_schema_created_at_document_id,priority:1"`
	Policies      *string        `json:"policies" gorm:"type:text"`
	Distributions pq.StringArray `json:"distributions" gorm:"type:text[]"`
	// user-provided creation time
	CreatedAt time.Time `json:"createdAt" gorm:"type:timestamp with time zone;not null;index:idx_records_schema_created_at_document_id,priority:2;index:idx_records_document_id_created_at,priority:2"`
	// record creation time in the system
	CDate time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
}

type Ack struct {
	From    string `json:"from" gorm:"type:text;index:idx_ack_from_to_context,unique"`
	To      string `json:"to" gorm:"type:text;index:idx_ack_from_to_context,unique"`
	Context string `json:"schema" gorm:"type:text;index:idx_ack_from_to_context,unique"`

	DocumentID string    `json:"id" gorm:"primaryKey;type:text"`
	Document   CommitLog `json:"-" gorm:"foreignKey:DocumentID;references:ID;constraint:OnDelete:CASCADE;"`

	Valid bool `json:"valid" gorm:"type:boolean;not null;default:true"`

	CreatedAt time.Time `json:"createdAt" gorm:"type:timestamp with time zone;not null"` // user-provided creation time
	CDate     time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
}

type Association struct {
	DocumentID string    `json:"id" gorm:"primaryKey;type:text"`
	Document   CommitLog `json:"-" gorm:"foreignKey:DocumentID;references:ID;constraint:OnDelete:CASCADE;"`

	TargetID int64     `json:"targetID" gorm:"type:bigint;index"`
	Target   RecordKey `json:"-" gorm:"foreignKey:TargetID;references:ID;constraint:OnDelete:CASCADE;"`

	Owner  string `json:"owner" gorm:"type:text"`
	Author string `json:"author" gorm:"type:text"`

	Schema  string  `json:"schema" gorm:"type:text"`
	Variant *string `json:"variant" gorm:"type:text"`
	Unique  string  `json:"unique" gorm:"type:text;unique"`

	CreatedAt time.Time `json:"createdAt" gorm:"type:timestamp with time zone;not null"` // user-provided creation time
	CDate     time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
}

type Server struct {
	ID          string    `json:"fqdn" gorm:"type:text;primaryKey"` // FQDN
	CSID        string    `json:"csid" gorm:"type:text"`
	Tag         string    `json:"tag" gorm:"type:text"`
	Layer       string    `json:"layer" gorm:"type:text"`
	WellKnown   string    `json:"wellKnown" gorm:"type:jsonb"`
	CDate       time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate       time.Time `json:"mdate" gorm:"autoUpdateTime"`
	LastScraped time.Time `json:"lastScraped" gorm:"type:timestamp with time zone"`
}

type Entity struct {
	ID     string  `json:"ccid" gorm:"type:text;primaryKey"`
	Alias  *string `json:"alias,omitempty" gorm:"type:text;index"`
	Domain string  `json:"domain" gorm:"type:text"`
	Tag    string  `json:"tag" gorm:"type:text;"`

	DocumentID string    `json:"id" gorm:"type:text"`
	Document   CommitLog `json:"-" gorm:"foreignKey:DocumentID;references:ID;constraint:OnDelete:CASCADE;"`

	CDate time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

type EntityMeta struct {
	ID      string    `json:"ccid" gorm:"type:text;primaryKey"`
	Inviter *string   `json:"inviter" gorm:"type:text"`
	Info    string    `json:"info" gorm:"type:jsonb;default:'null'"`
	CDate   time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
	MDate   time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

type AbuseReport struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	IP        string    `json:"ip" gorm:"type:text"`
	Reporter  string    `json:"reporter" gorm:"type:text"`
	TargetURI string    `json:"target" gorm:"type:text"`
	Body      string    `json:"body" gorm:"type:text"`
	CDate     time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
}
