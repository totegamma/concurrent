// +build wireinject

package main

import (
    "gorm.io/gorm"
    "github.com/google/wire"
    "github.com/redis/go-redis/v9"

    "github.com/totegamma/concurrent/x/util"
    "github.com/totegamma/concurrent/x/host"
    "github.com/totegamma/concurrent/x/agent"
    "github.com/totegamma/concurrent/x/entity"
    "github.com/totegamma/concurrent/x/socket"
    "github.com/totegamma/concurrent/x/stream"
    "github.com/totegamma/concurrent/x/message"
    "github.com/totegamma/concurrent/x/character"
    "github.com/totegamma/concurrent/x/association"
)


var hostHandlerProvider = wire.NewSet(host.NewHandler, host.NewService, host.NewRepository)
var entityHandlerProvider = wire.NewSet(entity.NewHandler, entity.NewService, entity.NewRepository)
var streamHandlerProvider = wire.NewSet(stream.NewHandler, stream.NewService, stream.NewRepository, entity.NewService, entity.NewRepository)
var messageHandlerProvider = wire.NewSet(message.NewHandler, message.NewService, message.NewRepository)
var characterHandlerProvider = wire.NewSet(character.NewHandler, character.NewService, character.NewRepository)
var associationHandlerProvider = wire.NewSet(association.NewHandler, association.NewService, association.NewRepository)

func SetupMessageHandler(db *gorm.DB, client *redis.Client, socket *socket.Service, config util.Config) *message.Handler {
    wire.Build(messageHandlerProvider, stream.NewService, stream.NewRepository, entity.NewService, entity.NewRepository)
    return &message.Handler{}
}

func SetupCharacterHandler(db *gorm.DB, config util.Config) *character.Handler {
    wire.Build(characterHandlerProvider)
    return &character.Handler{}
}

func SetupAssociationHandler(db *gorm.DB, client *redis.Client, socket *socket.Service, config util.Config) *association.Handler {
    wire.Build(associationHandlerProvider, stream.NewService, stream.NewRepository, entity.NewService, entity.NewRepository)
    return &association.Handler{}
}

func SetupStreamHandler(db *gorm.DB, client *redis.Client, config util.Config) *stream.Handler {
    wire.Build(streamHandlerProvider)
    return &stream.Handler{}
}

func SetupHostHandler(db *gorm.DB, config util.Config) *host.Handler {
    wire.Build(hostHandlerProvider)
    return &host.Handler{}
}

func SetupEntityHandler(db *gorm.DB, config util.Config) *entity.Handler {
    wire.Build(entityHandlerProvider)
    return &entity.Handler{}
}

func SetupSocketHandler(socketService *socket.Service, config util.Config) *socket.Handler {
    wire.Build(socket.NewHandler)
    return &socket.Handler{}
}

func SetupAgent(db *gorm.DB, config util.Config) *agent.Agent {
    wire.Build(agent.NewAgent, host.NewService, host.NewRepository, entity.NewService, entity.NewRepository)
    return &agent.Agent{}
}

