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
func (r *Repository) Create(association *core.Association) {
    r.db.Create(&association)
}

// Get returns a Association by ID
func (r *Repository) Get(id string) core.Association {
    var association core.Association
    r.db.Where("id = $1", id).First(&association)
    return association
}

// GetOwn returns all associations which owned by specified owner
func (r *Repository) GetOwn(author string) []core.Association {
    var associations []core.Association
    r.db.Where("author = $1", author)
    return associations 
}

// Delete deletes a association by ID
func (r *Repository) Delete(id string) core.Association {
    var deleted core.Association
    if err := r.db.First(&deleted, "id = ?", id).Error; err != nil {
        fmt.Printf("Error finding association: %v\n", err)
        return core.Association{}
    }
    result := r.db.Where("id = $1", id).Delete(&core.Association{})
    if result.Error != nil {
        fmt.Printf("Error deleting association: %v\n", result.Error)
        return core.Association{}
    }
    return deleted
}

