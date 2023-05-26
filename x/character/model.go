package character

import (
    "time"
    "github.com/totegamma/concurrent/x/core"
)

// CharactersResponse is used to return charcter query
type CharactersResponse struct {
    Characters []core.Character `json:"characters"`
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
}

