package stream

import (
    "gorm.io/gorm"
)


// Repository is stream repository
type Repository struct {
    db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

// Get returns a stream by ID
func (r *Repository) Get(key string) Stream {
    var stream Stream
    r.db.First(&stream, "id = ?", key)
    return stream
}

// Upsert updates a stream
func (r *Repository) Upsert(stream *Stream) {
    r.db.Save(&stream)
}

// GetList returns list of schemas by schema
func (r *Repository) GetList(schema string) []Stream {
    var streams []Stream
    r.db.Where("Schema = ?", schema).Find(&streams)
    return streams
}

