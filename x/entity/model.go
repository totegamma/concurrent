// Package entity handles concurrent object Entity
package entity 

import (
    "time"
)

type postRequest struct {
    CCAddr string `json:"ccaddr"`
    Meta string `json:"meta"`
}

// SafeEntity is safe verison of entity
type SafeEntity struct {
    ID string `json:"ccaddr"`
    Role string `json:"role"`
    Score int `json:"score"`
    Host string `json:"host"`
    CDate time.Time `json:"cdate"`
}

