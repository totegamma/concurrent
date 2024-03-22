package profile

import (
	"time"
)

type postRequest struct {
	SignedObject string `json:"signedObject"`
	Signature    string `json:"signature"`
	ID           string `json:"id"`
}

type signedObject struct {
	Signer   string      `json:"signer"`
	Type     string      `json:"type"`
	Schema   string      `json:"schema"`
	Body     interface{} `json:"body"`
	Meta     interface{} `json:"meta"`
	SignedAt time.Time   `json:"signedAt"`
}
