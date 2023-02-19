package repository

import (
    "gorm.io/gorm"
    "concurrent/domain/model"
)

type IAssociationRepository interface {
    Create(association model.Association)
}

type AssociationRepository struct {
    db *gorm.DB
}

func NewAssociationRepository(db *gorm.DB) AssociationRepository {
    return AssociationRepository{db: db}
}

func (r *AssociationRepository) Create(association model.Association) {
    r.db.Create(&association)
}

