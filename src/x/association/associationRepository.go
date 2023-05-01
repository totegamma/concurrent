package association

import (
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

func (r *AssociationRepository) Delete(id string) {
    r.db.Where("id = $1", id).Delete(&Association{})
}

