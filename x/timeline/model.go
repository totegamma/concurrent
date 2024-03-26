package timeline

import (
	"github.com/totegamma/concurrent/x/core"
	"time"
)

type postQuery struct {
	Timeline string `json:"timeline"`
	ID       string `json:"id"`
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

type checkpointPacket struct {
	Timeline  string            `json:"timeline"` // timeline full id (ex: <timelineID>@<domain>)
	Item      core.TimelineItem `json:"item"`
	Body      interface{}       `json:"body"`
	Principal string            `json:"principal"`
}

type chunkResponse struct {
	Status  string           `json:"status"`
	Content map[string]Chunk `json:"content"`
}

type timelineResponse struct {
	Status  string        `json:"status"`
	Content core.Timeline `json:"content"`
}

type Chunk struct {
	Key   string              `json:"key"`
	Items []core.TimelineItem `json:"items"`
}
