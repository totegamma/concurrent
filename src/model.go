package main

import (
    "time"
    "gorm.io/gorm"
)

type Backend struct {
    DB *gorm.DB
}

type Character struct {
    Author string `json:"author" gorm:"primaryKey;type:varchar(1024)"`
    Schema string `json:"schema" gorm:"primaryKey;type:varchar(1024)"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:varchar(1024)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

type Association struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(1024)"`
    Schema string `json:"schema"  gorm:"type:varchar(1024)"`
    Target string `json:"target" gorm:"type:uuid"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:varchar(1024)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

type Message struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(1024)"`
    Schema string `json:"schema" gorm:"type:varchar(1024)"`
    Payload string `json:"payload" gorm:"type:json"`
    Signature string `json:"signature" gorm:"type:varchar(1024)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
    Associations []string `json:"associations" gorm:"type:uuid[]"`
}

type MessagesResponse struct {
    Messages []Message `json:"messages"`
}

type CharactersResponse struct {
    Characters []Character `json:"characters"`
}

