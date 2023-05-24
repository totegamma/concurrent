package association

import (
    "time"
    "github.com/lib/pq"
)

// Association is one of a concurrent base object
type Association struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(42)"`
    Schema string `json:"schema"  gorm:"type:varchar(256)"`
    TargetID string `json:"targetID" gorm:"type:uuid"`
    TargetType string `json:"targetType" gorm:"type:string"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
    Streams pq.StringArray `json:"streams" gorm:"type:text[]"`
}

// StreamEvent is a message type which send to socket service
type StreamEvent struct {
    Type string `json:"type"`
    Action string `json:"action"`
    Body Association `json:"body"`
}

type deleteQuery struct {
    ID string `json:"id"`
}

type postRequest struct {
    SignedObject string `json:"signedObject"`
    Signature string `json:"signature"`
    Streams []string `json:"streams"`
    TargetType string `json:"targetType"`
    TargetHost string `json:"targetHost"`
}

type associationResponse struct {
    Association Association `json:"association"`
}

type signedObject struct {
    Signer string `json:"signer"`
    Type string `json:"type"`
    Schema string `json:"schema"`
    Body interface{} `json:"body"`
    Meta interface{} `json:"meta"`
    SignedAt time.Time `json:"signedAt"`
    Target string `json:"target"`
}

