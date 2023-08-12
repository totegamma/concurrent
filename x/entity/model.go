// Package entity handles concurrent object Entity
package entity

import (
	"time"
)

type postRequest struct {
	CCID string `json:"ccid"`
	Meta   string `json:"meta"`
	Token  string `json:"token"`
}

// SafeEntity is safe verison of entity
type SafeEntity struct {
	ID    string    `json:"ccid"`
	Role  string    `json:"role"`
	Score int       `json:"score"`
	Domain string    `json:"domain"`
	Certs string    `json:"certs"`
	CDate time.Time `json:"cdate"`
	MDate time.Time `json:"mdate"`
}
