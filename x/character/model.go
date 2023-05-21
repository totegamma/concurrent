package character

import (
    "time"
    "github.com/totegamma/concurrent/x/association"
)

// Character is one of  a Concurrent base object
type Character struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(42)"`
    Schema string `json:"schema" gorm:"type:varchar(256)"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    Associations []association.Association `json:"associations" gorm:"polymorphic:Target"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

// CharactersResponse is used to return charcter query
type CharactersResponse struct {
    Characters []Character `json:"characters"`
}

