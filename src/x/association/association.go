package association

import (
    "time"
)

type Association struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:varchar(1024)"`
    Schema string `json:"schema"  gorm:"type:varchar(1024)"`
    Target string `json:"target" gorm:"type:uuid"`
    Payload string `json:"payload" gorm:"type:json"`
    R string `json:"r" gorm:"type:char(64)"`
    S string `json:"s" gorm:"type:char(64)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

