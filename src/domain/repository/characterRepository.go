package repository

import (
    "gorm.io/gorm"
    "concurrent/domain/model"
)

type ICharacterRepository interface {
    Upsert(character model.Character)
    Get(owner string, schema string)
}

type CharacterRepository struct {
    db *gorm.DB
}

func NewCharacterRepository(db *gorm.DB) CharacterRepository {
    return CharacterRepository{db: db}
}

func (r *CharacterRepository) Upsert(character model.Character) {
    r.db.Save(&character)
}

func (r *CharacterRepository) Get(owner string, schema string) ([]model.Character, error) {
    var characters []model.Character
    if err := r.db.Where("author = $1 AND schema = $2", owner, schema).Find(&characters).Error; err != nil {
        return []model.Character{}, err
    }
    if characters == nil {
        return []model.Character{}, nil
    }
    return characters, nil
}

