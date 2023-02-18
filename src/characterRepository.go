package main

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

func (r *CharacterRepository) Get(owner string, schema string) []Character {
    var characters []Character
    r.db.Where("author = $1 AND schema = $2", owner, schema).Find(&characters);
    return characters
}

