package stream

import (
    "gorm.io/gorm"
)


type Repository struct {
    db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

func (r *Repository) Get(key string) Stream {
    var stream Stream
    r.db.First(&stream, "id = ?", key)
    return stream
}

func (r *Repository) Upsert(stream *Stream) {
    r.db.Save(&stream)
}

func (r *Repository) GetList(schema string) []Stream {
    var streams []Stream
    r.db.Where("Schema = ?", schema).Find(&streams)
    return streams
}

