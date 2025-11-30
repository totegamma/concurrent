package core

import (
	"time"
)

type TimelineEndpoint struct {
	Iterator string `json:"iterator"`
	Body     string `json:"body"`
}

type ChunkedTimeline struct {
	Version    string            `json:"version"`
	ChunkSize  int64             `json:"chunk_size"`
	FirstChunk int64             `json:"first_chunk"`
	LastChunk  int64             `json:"last_chunk,omitempty"`
	Ascending  *TimelineEndpoint `json:"ascending,omitempty"`
	Descending *TimelineEndpoint `json:"descending,omitempty"`
	Metadata   any               `json:"metadata"`
}

type ChunkBodyNode struct {
	Timestamp time.Time `json:"timestamp"`
	Href      string    `json:"href"`
}
