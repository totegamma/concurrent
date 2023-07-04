package message

import (
	"github.com/totegamma/concurrent/x/core"
	"time"
)

type streamEvent struct {
	Stream string       `json:"stream"`
	Type   string       `json:"type"`
	Action string       `json:"action"`
	Body   core.Message `json:"body"`
}

type messagesResponse struct {
	Messages []core.Message `json:"messages"`
}

type deleteQuery struct {
	ID string `json:"id"`
}

type postRequest struct {
	SignedObject string   `json:"signedObject"`
	Signature    string   `json:"signature"`
	Streams      []string `json:"streams"`
}

// SignedObject is user sign unit
type SignedObject struct {
	Signer   string      `json:"signer"`
	Type     string      `json:"type"`
	Schema   string      `json:"schema"`
	Body     interface{} `json:"body"`
	Meta     interface{} `json:"meta"`
	SignedAt time.Time   `json:"signedAt"`
}
