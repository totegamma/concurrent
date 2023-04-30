// +build wireinject

package main

import (
	"concurrent/application"
	"concurrent/domain/repository"
	"concurrent/domain/service"
	"concurrent/presentation/handler"
    "concurrent/x/stream"

	"github.com/google/wire"
	"gorm.io/gorm"
)

var messageHandlerProvider = wire.NewSet(handler.NewMessageHandler, service.NewMessageService, repository.NewMessageRepository)
var characterHandlerProvider = wire.NewSet(handler.NewCharacterHandler, service.NewCharacterService, repository.NewCharacterRepository)
var associationHandlerProvider = wire.NewSet(handler.NewAssociationHandler, service.NewAssociationService, repository.NewAssociationRepository)
var streamHandlerProvider = wire.NewSet(stream.NewStreamHandler, stream.NewStreamService)

func SetupConcurrentApp(db *gorm.DB) application.ConcurrentApp {
    wire.Build(application.NewConcurrentApp, messageHandlerProvider, characterHandlerProvider, associationHandlerProvider, streamHandlerProvider)
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

func SetupWebfingerHandler(db *gorm.DB) handler.WebfingerHandler {
    wire.Build(handler.NewWebfingerHandler, service.NewCharacterService, repository.NewCharacterRepository)
    return handler.WebfingerHandler{}
}

func SetupActivityPubHandler(db *gorm.DB) handler.ActivityPubHandler {
    wire.Build(handler.NewActivityPubHandler, service.NewCharacterService, repository.NewCharacterRepository)
    return handler.ActivityPubHandler{}
}

func SetupStreamHandler() stream.StreamHandler {
    wire.Build(streamHandlerProvider)
    return stream.StreamHandler{}
}

