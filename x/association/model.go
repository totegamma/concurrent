package association

import (
	"time"
)

type Element struct {
	ID string `json:"id"`
}

type postRequest struct {
	SignedObject string   `json:"signedObject"`
	Signature    string   `json:"signature"`
	Streams      []string `json:"streams"`
	TargetType   string   `json:"targetType"`
	TargetHost   string   `json:"targetHost"`
}

type SignedObject struct {
	Signer   string      `json:"signer"`
	Type     string      `json:"type"`
	Schema   string      `json:"schema"`
	Body     interface{} `json:"body"`
	Variant  string      `json:"variant"`
	Meta     interface{} `json:"meta"`
	SignedAt time.Time   `json:"signedAt"`
	Target   string      `json:"target"`
}
