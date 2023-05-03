package message

import (
    "time"
    "github.com/lib/pq"
    "github.com/totegamma/concurrent/x/association"
)

type Message struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(64)"`
    Schema string `json:"schema" gorm:"type:varchar(1024)"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
    Associations pq.StringArray `json:"associations" gorm:"type:uuid[]"`
    AssociationsData []association.Association `json:"associations_data" gorm:"-"`
    Streams string `json:"streams" gorm:"type:text"`
}

type MessageStreamEvent struct {
    Type string `json:"type"`
    Action string `json:"action"`
    Body Message `json:"body"`
}

type MessagesResponse struct {
    Messages []Message `json:"messages"`
}

type MessageResponse struct {
    Message Message `json:"message"`
}

type deleteQuery struct {
    Id string `json:"id"`
}

