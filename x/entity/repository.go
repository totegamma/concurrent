package entity

import (
    "gorm.io/gorm"
)

// Repository is host repository
type Repository struct {
    db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) Repository {
    return Repository{db: db}
}

// Get returns a host by ID
func (r *Repository) Get(key string) Entity {
    var entity Entity
    r.db.First(&entity, "id = ?", key)
    return entity
}

// Create creates a entity
func (r *Repository) Create(entity *Entity) {
    r.db.Create(&entity)
}

// Upsert updates a entity
func (r *Repository) Upsert(entity *Entity) {
    r.db.Save(&entity)
}

// GetList returns all entities
func (r *Repository) GetList() []SafeEntity {
    var entities []SafeEntity
    r.db.Model(&Entity{}).Where("host IS NULL").Find(&entities)
    return entities
}

