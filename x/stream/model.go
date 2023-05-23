package stream

import (
    "time"
    "github.com/lib/pq"
)

// Stream is one of a base object of concurrent
type Stream struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:char(42)"`
    Maintainer pq.StringArray `json:"maintainer" gorm:"type:char(42)[];default:'{}'"`
    Writer pq.StringArray `json:"writer" gorm:"type:char(42)[];default:'{}'"`
    Reader pq.StringArray `json:"reader" gorm:"type:char(42)[];default:'{}'"`
    Schema string `json:"schema" gorm:"type:varchar(256)"`
    Payload string `json:"payload" gorm:"type:json;default:'{}'"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

type postQuery struct {
    Stream string `json:"stream"`
    ID string `json:"id"`
}

type postRequest struct {
    SignedObject string `json:"signedObject"`
    Signature string `json:"signature"`
    ID string `json:"id"`
}

type signedObject struct {
    Signer string `json:"signer"`
    Type string `json:"type"`
    Schema string `json:"schema"`
    Body interface{} `json:"body"`
    Meta interface{} `json:"meta"`
    SignedAt time.Time `json:"signedAt"`
    Maintainer []string `json:"maintainer"`
    Writer []string `json:"writer"`
    Reader []string `json:"reader"`
}

// Element is stream element
type Element struct {
    Timestamp string `json:"timestamp"`
    ID string `json:"id"`
    Author string `json:"author"`
    Host string `json:"currenthost"`
}

type checkpointPacket struct {
    Stream string `json:"stream"`
    ID string `json:"id"`
    Author string `json:"author"`
}



