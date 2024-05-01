package core

import ()

// Event is websocket root packet model
type Event struct {
	Timeline  string       `json:"timeline"` // stream full id (ex: <streamID>@<domain>)
	Item      TimelineItem `json:"item,omitempty"`
	Resource  any          `json:"resource,omitempty"`
	Document  string       `json:"document"`
	Signature string       `json:"signature"`
}

type Chunk struct {
	Key   string         `json:"key"`
	Items []TimelineItem `json:"items"`
}
