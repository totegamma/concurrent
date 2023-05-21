package character

import "time"

// Character is one of  a Concurrent base object
type Character struct {
    Author string `json:"author" gorm:"primaryKey;type:varchar(42)"`
    Schema string `json:"schema" gorm:"primaryKey;type:varchar(256)"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

// CharactersResponse is used to return charcter query
type CharactersResponse struct {
    Characters []Character `json:"characters"`
}

