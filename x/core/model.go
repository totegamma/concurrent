package core

import (
    "time"
    "github.com/lib/pq"
)

// Association is one of a concurrent base object
// immutable
type Association struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(42);uniqueIndex:uniq_association"`
    Schema string `json:"schema"  gorm:"type:varchar(256);uniqueIndex:uniq_association"`
    TargetID string `json:"targetID" gorm:"type:uuid;uniqueIndex:uniq_association"`
    TargetType string `json:"targetType" gorm:"type:string;uniqueIndex:uniq_association"`
    ContentHash string `json:"contentHash" gorm:"type:char(64);uniqueIndex:uniq_association"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
    Streams pq.StringArray `json:"streams" gorm:"type:text[]"`
}

// Character is one of  a Concurrent base object
// mutable
type Character struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(42);uniqueIndex:uniq_character"`
    Schema string `json:"schema" gorm:"type:varchar(256);uniqueIndex:uniq_character"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    Associations []Association `json:"associations" gorm:"polymorphic:Target"`
    CDate time.Time `json:"cdate" gorm:"->;<-:create;autoCreateTime"`
    MDate time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

// Entity is one of a concurrent base object
// mutable
type Entity struct {
    ID string `json:"id" gorm:"type:char(42)"`
    Role string `json:"role" gorm:"type:text;default:default"`
    Host string `json:"host" gorm:"type:text"`
    Certs string `json:"certs" gorm:"type:json;default:'null'"`
    Meta string `json:"meta" gorm:"type:json;default:'null'"`
    Score int `json:"score" gorm:"type:integer;default:0"`
    CDate time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
    MDate time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

// Host is one of a concurrent base object
// mutable
type Host struct {
    ID string `json:"fqdn" gorm:"type:text"` // FQDN
    CCAddr string `json:"ccaddr" gorm:"type:char(42)"`
    Role string `json:"role" gorm:"type:text;default:default"`
    Score int `json:"score" gorm:"type:integer;default:0"`
    Pubkey string `json:"pubkey" gorm:"type:text"`
    CDate time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
    MDate time.Time `json:"mdate" gorm:"autoUpdateTime"`
    LastScraped time.Time `json:"lastScraped" gorm:"type:timestamp with time zone"`
}

// Message is one of a concurrent base object
// immutable
type Message struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(42)"`
    Schema string `json:"schema" gorm:"type:varchar(256)"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
    Associations []Association `json:"associations" gorm:"polymorphic:Target"`
    Streams pq.StringArray `json:"streams" gorm:"type:text[]"`
}

// Stream is one of a base object of concurrent
// mutable
type Stream struct {
    ID string `json:"id" gorm:"primaryKey;type:char(20);"`
    Author string `json:"author" gorm:"type:char(42)"`
    Maintainer pq.StringArray `json:"maintainer" gorm:"type:char(42)[];default:'{}'"`
    Writer pq.StringArray `json:"writer" gorm:"type:char(42)[];default:'{}'"`
    Reader pq.StringArray `json:"reader" gorm:"type:char(42)[];default:'{}'"`
    Schema string `json:"schema" gorm:"type:varchar(256)"`
    Payload string `json:"payload" gorm:"type:json;default:'{}'"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"->;<-:create;type:timestamp with time zone;not null;default:clock_timestamp()"`
    MDate time.Time `json:"mdate" gorm:"autoUpdateTime"`
}

