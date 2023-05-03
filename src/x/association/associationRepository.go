package association

import (
    "fmt"
    "gorm.io/gorm"
)

type IAssociationRepository interface {
    Create(association Association)
    GetOwn(author string)
}

type AssociationRepository struct {
    db *gorm.DB
}

func NewAssociationRepository(db *gorm.DB) AssociationRepository {
    return AssociationRepository{db: db}
}

func (r *AssociationRepository) Create(association Association) {
    r.db.Create(&association)
}

func (r *AssociationRepository) GetOwn(author string) []Association {
    var associations []Association
    r.db.Where("author = $1", author)
    return associations 
}

func (r *AssociationRepository) Delete(id string) Association {
    var deleted Association
    if err := r.db.First(&deleted, "id = ?", id).Error; err != nil {
        fmt.Printf("Error finding association: %v\n", err)
        return Association{}
    }
    result := r.db.Where("id = $1", id).Delete(&Association{})
    if result.Error != nil {
        fmt.Printf("Error deleting association: %v\n", result.Error)
        return Association{}
    }
    return deleted
}

