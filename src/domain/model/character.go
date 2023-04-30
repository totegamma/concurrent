package model

import "time"

type Character struct {
    Author string `json:"author" gorm:"primaryKey;type:varchar(1024)"`
    Schema string `json:"schema" gorm:"primaryKey;type:varchar(1024)"`
    Payload string `json:"payload" gorm:"type:json"`
    R string `json:"r" gorm:"type:char(64)"`
    S string `json:"s" gorm:"type:char(64)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

