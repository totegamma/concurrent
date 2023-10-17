package association

import (
	"github.com/totegamma/concurrent/x/core"
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

type associationResponse struct {
	Association core.Association `json:"association"`
}

type SignedObject struct {
	Signer   string      `json:"signer"`
	Type     string      `json:"type"`
	Schema   string      `json:"schema"`
	Body     interface{} `json:"body"`
	Meta     interface{} `json:"meta"`
	SignedAt time.Time   `json:"signedAt"`
	Target   string      `json:"target"`
}
