package stream

import (
	"time"
)

type postQuery struct {
	Stream string `json:"stream"`
	ID     string `json:"id"`
}

type postRequest struct {
	SignedObject string `json:"signedObject"`
	Signature    string `json:"signature"`
	ID           string `json:"id"`
}

type signedObject struct {
	Signer     string      `json:"signer"`
	Type       string      `json:"type"`
	Schema     string      `json:"schema"`
	Body       interface{} `json:"body"`
	Meta       interface{} `json:"meta"`
	SignedAt   time.Time   `json:"signedAt"`
	Maintainer []string    `json:"maintainer"`
	Writer     []string    `json:"writer"`
	Reader     []string    `json:"reader"`
}

// Event is websocket root packet model
type Event struct {
	Stream string  `json:"stream"`
	Type   string  `json:"type"`
	Action string  `json:"action"`
	Body   Element `json:"body"`
}

// Element is stream element
type Element struct {
	Timestamp string `json:"timestamp"`
	ID        string `json:"id"`
	Type      string `json:"type"`
	Author    string `json:"author"`
	Owner     string `json:"owner"`
	Domain    string `json:"domain"`
}

type checkpointPacket struct {
	Stream string `json:"stream"`
	ID     string `json:"id"`
	Type   string `json:"type"`
	Author string `json:"author"`
	Host   string `json:"host"`
	Owner  string `json:"owner"`
}
