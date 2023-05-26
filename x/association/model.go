package association

import (
    "time"
    "github.com/totegamma/concurrent/x/core"
)

// StreamEvent is a message type which send to socket service
type StreamEvent struct {
    Type string `json:"type"`
    Action string `json:"action"`
    Body core.Association `json:"body"`
}

type deleteQuery struct {
    ID string `json:"id"`
}

type postRequest struct {
    SignedObject string `json:"signedObject"`
    Signature string `json:"signature"`
    Streams []string `json:"streams"`
    TargetType string `json:"targetType"`
    TargetHost string `json:"targetHost"`
}

type associationResponse struct {
    Association core.Association `json:"association"`
}

type signedObject struct {
    Signer string `json:"signer"`
    Type string `json:"type"`
    Schema string `json:"schema"`
    Body interface{} `json:"body"`
    Meta interface{} `json:"meta"`
    SignedAt time.Time `json:"signedAt"`
    Target string `json:"target"`
}

