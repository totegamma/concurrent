// Package entity handles concurrent object Entity
package entity

import (
	"github.com/totegamma/concurrent/x/core"
)

type affiliationOption struct {
	Info       string `json:"info"`
	Invitation string `json:"invitation"`
}

type entityResponse struct {
	Status  string      `json:"status"`
	Content core.Entity `json:"content"`
}
