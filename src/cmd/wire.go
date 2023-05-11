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
var streamHandlerProvider = wire.NewSet(stream.NewStreamHandler, stream.NewStreamService, stream.NewRepository)

func SetupMessageHandler(db *gorm.DB, client *redis.Client, socketService *socket.SocketService) message.Handler {
    wire.Build(messageHandlerProvider, stream.NewStreamService, stream.NewRepository)
    return message.Handler{}
}

func SetupCharacterHandler(db *gorm.DB) character.Handler {
    wire.Build(characterHandlerProvider)
    return character.Handler{}
}

func SetupAssociationHandler(db *gorm.DB, client *redis.Client, socketService *socket.SocketService) association.Handler {
    wire.Build(associationHandlerProvider, stream.NewStreamService, stream.NewRepository)
    return association.Handler{}
}

func SetupWebfingerHandler(db *gorm.DB) activitypub.WebfingerHandler {
    wire.Build(activitypub.NewWebfingerHandler, character.NewCharacterService, character.NewCharacterRepository)
    return activitypub.WebfingerHandler{}
}

func SetupActivityPubHandler(db *gorm.DB) activitypub.ActivityPubHandler {
    wire.Build(activitypub.NewActivityPubHandler, character.NewCharacterService, character.NewCharacterRepository)
    return activitypub.ActivityPubHandler{}
}

func SetupStreamHandler(db *gorm.DB, client *redis.Client) stream.Handler {
    wire.Build(streamHandlerProvider)
    return stream.Handler{}
}

func SetupSocketHandler(socketService *socket.SocketService) *socket.Handler {
    wire.Build(socket.NewSocketHandler)
    return &socket.Handler{}
}

