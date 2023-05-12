// Package host is...
package host

import (
    "time"
)

// Host is one of a concurrent base object
type Host struct {
    ID string `json:"fqdn" gorm:"type:text"` // FQDN
    CCAddr string `json:"ccaddr" gorm:"type:char(42)"`
    Role string `json:"role" gorm:"type:text;default:default"`
    Pubkey string `json:"pubkey" gorm:"type:text"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

// Profile is portable information of host
type Profile struct {
    ID string `json:"fqdn" gorm:"type:text"`
    CCAddr string `json:"ccaddr" gorm:"type:char(42)"`
    Pubkey string `json:"pubkey" gorm:"type:text"`
}

