package character

import (
    "gorm.io/gorm"
)

type ICharacterRepository interface {
    Upsert(character Character)
    Get(owner string, schema string)
}

type CharacterRepository struct {
    db *gorm.DB
}

func NewCharacterRepository(db *gorm.DB) CharacterRepository {
    return CharacterRepository{db: db}
}

func (r *CharacterRepository) Upsert(character Character) {
    r.db.Save(&character)
}

func (r *CharacterRepository) Get(owner string, schema string) ([]Character, error) {
    var characters []Character
    if err := r.db.Where("author = $1 AND schema = $2", owner, schema).Find(&characters).Error; err != nil {
        return []Character{}, err
    }
    if characters == nil {
        return []Character{}, nil
    }
    return characters, nil
}

