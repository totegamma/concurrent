package model

import "time"

type Character struct {
    Author string `json:"author" gorm:"primaryKey;type:varchar(1024)"`
    Schema string `json:"schema" gorm:"primaryKey;type:varchar(1024)"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature Signature `json:"signature"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

