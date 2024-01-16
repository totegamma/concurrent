// Package entity handles concurrent object Entity
package entity

import (
	"time"
)

type createRequest struct {
	CCID string `json:"ccid"`
	Meta string `json:"meta"`
}

type registerRequest struct {
	CCID    string `json:"ccid"`
	Meta    string `json:"meta"`
	Token   string `json:"token"`
	Captcha string `json:"captcha"`
}

type AckSignedObject struct {
	Type     string    `json:"type"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	SignedAt time.Time `json:"signedAt"`
}

type ackRequest struct {
	SignedObject string `json:"signedObject"`
	Signature    string `json:"signature"`
}
