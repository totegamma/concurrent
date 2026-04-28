package chunkline

import (
	"fmt"
	"github.com/zeebo/xxh3"
	"time"
)

type Endpoint struct {
	Iterator string `json:"iterator"`
	Body     string `json:"body"`
}

type Manifest struct {
	Version    string    `json:"version"`
	ChunkSize  int64     `json:"chunk_size"`
	FirstChunk *int64    `json:"first_chunk"`
	LastChunk  *int64    `json:"last_chunk"`
	Ascending  *Endpoint `json:"ascending,omitempty"`
	Descending *Endpoint `json:"descending,omitempty"`
	Metadata   any       `json:"metadata"`
}

func (m Manifest) Time2Chunk(t time.Time) int64 {
	return t.Unix() / m.ChunkSize
}

func (m Manifest) Chunk2Time(c int64) time.Time {
	return time.Unix(c*m.ChunkSize, 0)
}

type ItrNode string

type BodyChunk struct {
	URI     string     `json:"uri"`
	ChunkID int64      `json:"chunk_id"`
	Items   []BodyItem `json:"items"`
}

type BodyItem struct {
	Timestamp   time.Time `json:"timestamp"`
	Content     string    `json:"content,omitempty"`
	ContentType string    `json:"content_type,omitempty"`
	Href        string    `json:"href,omitempty"`
}

type BodyItemWithSource struct {
	BodyItem
	Source string `json:"source"`
}

func (b BodyItem) ID() string {
	if b.Href != "" {
		return b.Href
	}
	hash := xxh3.HashString(b.Content)
	return fmt.Sprintf("urn:xxh3:%x", hash)
}
