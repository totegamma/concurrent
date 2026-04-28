package domain

import "github.com/totegamma/concrnt-playground/chunkline"

// Record represents a domain-level record content.
type Record struct {
	DocumentID  string    `json:"id"`
	Author      string    `json:"author"`
	Schema      string    `json:"schema"`
	ContentType string    `json:"contentType,omitempty"`
	Value       any       `json:"value"`
	URI         string    `json:"uri,omitempty"`
	MemberOf    *[]string `json:"memberOf,omitempty"`
	Owner       *string   `json:"owner,omitempty"`
	Associate   *string   `json:"associate,omitempty"`
}

// RecordKey associates a URI with a RecordID.
type RecordKey struct {
	URI      string `json:"uri"`
	RecordID string `json:"recordID"`
}

// ChunklineManifest represents timeline metadata for feeds.
type ChunklineManifest struct {
	Manifest chunkline.Manifest `json:"manifest"`
}
