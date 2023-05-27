package association

import (
    "fmt"
    "gorm.io/gorm"
    "github.com/totegamma/concurrent/x/core"
)


// Repository is association repository
type Repository struct {
    db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

// Create creates new association
func (r *Repository) Create(association *core.Association) error {
    return r.db.Create(&association).Error
}

// Get returns a Association by ID
func (r *Repository) Get(id string) (core.Association, error) {
    var association core.Association
    err := r.db.Where("id = $1", id).First(&association).Error
    return association, err
}

// GetOwn returns all associations which owned by specified owner
func (r *Repository) GetOwn(author string) ([]core.Association, error) {
    var associations []core.Association
    err := r.db.Where("author = $1", author).Error
    return associations, err
}

// Delete deletes a association by ID
func (r *Repository) Delete(id string) (core.Association, error) {
    var deleted core.Association
    if err := r.db.First(&deleted, "id = ?", id).Error; err != nil {
        fmt.Printf("Error finding association: %v\n", err)
        return core.Association{}, err
    }
    err := r.db.Where("id = $1", id).Delete(&core.Association{}).Error
    if err != nil {
        fmt.Printf("Error deleting association: %v\n", err)
        return core.Association{}, err
    }
    return deleted, nil
}

