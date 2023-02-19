// +build wireinject

package main

import (
	"concurrent/application"
	"concurrent/domain/repository"
	"concurrent/domain/service"
	"concurrent/presentation/handler"

	"github.com/google/wire"
	"gorm.io/gorm"
)

var messageHandlerProvider = wire.NewSet(handler.NewMessageHandler, service.NewMessageService, repository.NewMessageRepository)
var characterHandlerProvider = wire.NewSet(handler.NewCharacterHandler, service.NewCharacterService, repository.NewCharacterRepository)
var associationHandlerProvider = wire.NewSet(handler.NewAssociationHandler, service.NewAssociationService, repository.NewAssociationRepository)

func SetupConcurrentApp(db *gorm.DB) application.ConcurrentApp {
    wire.Build(application.NewConcurrentApp, messageHandlerProvider, characterHandlerProvider, associationHandlerProvider)
    return application.ConcurrentApp{}
}

func SetupMessageHandler(db *gorm.DB) handler.MessageHandler {
    wire.Build(messageHandlerProvider)
    return handler.MessageHandler{}
}

func SetupCharacterHandler(db *gorm.DB) handler.CharacterHandler {
    wire.Build(characterHandlerProvider)
    return handler.CharacterHandler{}
}

func SetupAssociationHandler(db *gorm.DB) handler.AssociationHandler {
    wire.Build(associationHandlerProvider)
    return handler.AssociationHandler{}
}

