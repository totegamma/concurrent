// Package core is the core of concurrent system
package core

import (
    "time"
)

// SignedObject is wrapper for any signed object
type SignedObject struct {
    Signer string `json:"signer"`
    Type string `json:"type"`
    Schema string `json:"schema"`
    Body interface{} `json:"body"`
    Meta interface{} `json:"meta"`
    SignedAt time.Time `json:"signedAt"`
}

