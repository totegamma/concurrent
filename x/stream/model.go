package stream

import (
	"github.com/totegamma/concurrent/x/core"
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
	Visible    bool        `json:"visible"`
}

// Event is websocket root packet model
type Event struct {
	Stream string          `json:"stream"` // stream full id (ex: <streamID>@<domain>)
	Type   string          `json:"type"`
	Action string          `json:"action"`
	Item   core.StreamItem `json:"item"`
	Body   interface{}     `json:"body"`
}

type checkpointPacket struct {
	Stream string          `json:"stream"` // stream full id (ex: <streamID>@<domain>)
	Item   core.StreamItem `json:"item"`
	Body   interface{}     `json:"body"`
}

type chunkResponse struct {
	Status string `json:"status"`
	Content map[string]Chunk `json:"content"`
}

type Chunk struct {
	Key	 string `json:"key"`
	Items []core.StreamItem `json:"items"`
}

