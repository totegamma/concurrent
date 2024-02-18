package ack

type ackRequest struct {
	SignedObject string `json:"signedObject"`
	Signature    string `json:"signature"`
}
