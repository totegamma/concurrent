package character

import "time"

type Character struct {
    Author string `json:"author" gorm:"primaryKey;type:varchar(64)"`
    Schema string `json:"schema" gorm:"primaryKey;type:varchar(1024)"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

type CharactersResponse struct {
    Characters []Character `json:"characters"`
}

