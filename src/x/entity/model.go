package entity 

import (
    "time"
)

type Entity struct {
    ID string `json:"id" gorm:"type:char(42)"`
    Enabled bool `json:"enabled" gorm:"type:boolean;default:0"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}


