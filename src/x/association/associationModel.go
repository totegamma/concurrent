package association

import (
    "time"
    "github.com/lib/pq"
)

type Association struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(64)"`
    Schema string `json:"schema"  gorm:"type:varchar(1024)"`
    Target string `json:"target" gorm:"type:uuid"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
    Streams pq.StringArray `json:"streams" gorm:"type:text[]"`
}

type AssociationStreamEvent struct {
    Type string `json:"type"`
    Action string `json:"action"`
    Body Association `json:"body"`
}

type deleteQuery struct {
    Id string `json:"id"`
}

type AssociationResponse struct {
    Association Association `json:"association"`
}

