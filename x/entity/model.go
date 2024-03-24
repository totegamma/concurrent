// Package entity handles concurrent object Entity
package entity

import (
	"github.com/totegamma/concurrent/x/core"
)

type registerRequest struct {
	CCID       string `json:"ccid"`
	Info       string `json:"info"`
	Signature  string `json:"signature"`
	Invitation string `json:"invitation"`
	Captcha    string `json:"captcha"`
}

type entityResponse struct {
	Status  string      `json:"status"`
	Content core.Entity `json:"content"`
}
