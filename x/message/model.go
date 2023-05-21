package message

import (
    "time"

    "github.com/lib/pq"
    "github.com/totegamma/concurrent/x/association"
)

// Message is one of a concurrent base object
type Message struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(42)"`
    Schema string `json:"schema" gorm:"type:varchar(256)"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
    Associations []association.Association `json:"associations" gorm:"polymorphic:Target"`
    Streams pq.StringArray `json:"streams" gorm:"type:text[]"`
}

type streamEvent struct {
    Type string `json:"type"`
    Action string `json:"action"`
    Body Message `json:"body"`
}

type messagesResponse struct {
    Messages []Message `json:"messages"`
}

type deleteQuery struct {
    ID string `json:"id"`
}

type postRequest struct {
    SignedObject string `json:"signedObject"`
    Signature string `json:"signature"`
    Streams []string `json:"streams"`
}

