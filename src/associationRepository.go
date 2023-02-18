package main

import (
    "gorm.io/gorm"
)

type IAssociationRepository interface {
    Create(association Association)
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

