package entity

import (
    "gorm.io/gorm"
    "github.com/totegamma/concurrent/x/core"
)

// Repository is host repository
type Repository struct {
    db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

// Get returns a host by ID
func (r *Repository) Get(key string) (core.Entity, error) {
    var entity core.Entity
    err := r.db.First(&entity, "id = ?", key).Error
    return entity, err 
}

// Create creates a entity
func (r *Repository) Create(entity *core.Entity) error {
    return r.db.Create(&entity).Error
}

// Upsert updates a entity
func (r *Repository) Upsert(entity *core.Entity) error {
    return r.db.Save(&entity).Error
}

// GetList returns all entities
func (r *Repository) GetList() ([]SafeEntity, error) {
    var entities []SafeEntity
    err := r.db.Model(&core.Entity{}).Where("host IS NULL or host = ''").Find(&entities).Error
    return entities, err
}

