package activitypub

import (
    "strings"
    "gorm.io/gorm"
)

// Repository is a repository for ActivityPub.
type Repository struct {
    db *gorm.DB
}

// NewRepository returns a new Repository.
func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

// GetEntityByID returns an entity by ID.
func (r Repository) GetEntityByID(id string) (Entity, error) {
    var entity Entity
    result := r.db.Where("id = ?", id).First(&entity)
    return entity, result.Error
}

// UpsertEntity upserts an entity.
func (r Repository) UpsertEntity(entity Entity) (Entity, error) {
    entity.ID = strings.ToLower(entity.ID)
    result := r.db.Save(&entity)
    return entity, result.Error
}

