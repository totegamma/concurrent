// +build wireinject

package main

import (
    "gorm.io/gorm"
    "github.com/google/wire"
)

func SetupMessageService(db *gorm.DB) MessageService {
    wire.Build(NewMessageService, NewMessageRepository)
    return MessageService{}
}

func SetupCharacterService(db *gorm.DB) CharacterService {
    wire.Build(NewCharacterService, NewCharacterRepository)
    return CharacterService{}
}

func SetupAssociationService(db *gorm.DB) AssociationService {
    wire.Build(NewAssociationService, NewAssociationRepository)
    return AssociationService{}
}

