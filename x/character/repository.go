package character

import (
    "gorm.io/gorm"
)

// Repository is a repository for character objects
type Repository struct {
    db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

// Upsert upserts existing character
func (r *Repository) Upsert(character Character) {
    r.db.Save(&character)
}

// Get returns character list which matches specified owner and chema
func (r *Repository) Get(owner string, schema string) ([]Character, error) {
    var characters []Character
    if err := r.db.Where("author = $1 AND schema = $2", owner, schema).Find(&characters).Error; err != nil {
        return []Character{}, err
    }
    if characters == nil {
        return []Character{}, nil
    }
    return characters, nil
}

