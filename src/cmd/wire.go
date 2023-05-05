// +build wireinject

package main

import (
    "gorm.io/gorm"
    "github.com/google/wire"
    "github.com/redis/go-redis/v9"

    "github.com/totegamma/concurrent/x/socket"
    "github.com/totegamma/concurrent/x/stream"
    "github.com/totegamma/concurrent/x/message"
    "github.com/totegamma/concurrent/x/character"
    "github.com/totegamma/concurrent/x/association"
    "github.com/totegamma/concurrent/x/activitypub"
)

var messageHandlerProvider = wire.NewSet(message.NewMessageHandler, message.NewMessageService, message.NewMessageRepository)
var characterHandlerProvider = wire.NewSet(character.NewCharacterHandler, character.NewCharacterService, character.NewCharacterRepository)
var associationHandlerProvider = wire.NewSet(association.NewAssociationHandler, association.NewAssociationService, association.NewAssociationRepository)
var streamHandlerProvider = wire.NewSet(stream.NewStreamHandler, stream.NewStreamService)

func SetupMessageHandler(db *gorm.DB, client *redis.Client, socketService *socket.SocketService) message.MessageHandler {
    wire.Build(messageHandlerProvider, stream.NewStreamService)
    return message.MessageHandler{}
}

func SetupCharacterHandler(db *gorm.DB) character.CharacterHandler {
    wire.Build(characterHandlerProvider)
    return character.CharacterHandler{}
}

func SetupAssociationHandler(db *gorm.DB, client *redis.Client, socketService *socket.SocketService) association.AssociationHandler {
    wire.Build(associationHandlerProvider, stream.NewStreamService)
    return association.AssociationHandler{}
}

func SetupWebfingerHandler(db *gorm.DB) activitypub.WebfingerHandler {
    wire.Build(activitypub.NewWebfingerHandler, character.NewCharacterService, character.NewCharacterRepository)
    return activitypub.WebfingerHandler{}
}

func SetupActivityPubHandler(db *gorm.DB) activitypub.ActivityPubHandler {
    wire.Build(activitypub.NewActivityPubHandler, character.NewCharacterService, character.NewCharacterRepository)
    return activitypub.ActivityPubHandler{}
}

func SetupStreamHandler(client *redis.Client) stream.StreamHandler {
    wire.Build(streamHandlerProvider)
    return stream.StreamHandler{}
}

func SetupSocketHandler(socketService *socket.SocketService) *socket.SocketHandler {
    wire.Build(socket.NewSocketHandler)
    return &socket.SocketHandler{}
}

