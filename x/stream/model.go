package stream

import (
    "time"
)

type postQuery struct {
    Stream string `json:"stream"`
    ID string `json:"id"`
}

type postRequest struct {
    SignedObject string `json:"signedObject"`
    Signature string `json:"signature"`
    ID string `json:"id"`
}

type signedObject struct {
    Signer string `json:"signer"`
    Type string `json:"type"`
    Schema string `json:"schema"`
    Body interface{} `json:"body"`
    Meta interface{} `json:"meta"`
    SignedAt time.Time `json:"signedAt"`
    Maintainer []string `json:"maintainer"`
    Writer []string `json:"writer"`
    Reader []string `json:"reader"`
}

// Element is stream element
type Element struct {
    Timestamp string `json:"timestamp"`
    ID string `json:"id"`
    Author string `json:"author"`
    Host string `json:"currenthost"`
}

type checkpointPacket struct {
    Stream string `json:"stream"`
    ID string `json:"id"`
    Author string `json:"author"`
}



