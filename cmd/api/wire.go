//go:build wireinject

package main

import (
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"github.com/bradfitz/gomemcache/memcache"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/x/agent"
	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/auth"
	"github.com/totegamma/concurrent/x/character"
	"github.com/totegamma/concurrent/x/collection"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/socket"
	"github.com/totegamma/concurrent/x/stream"
	"github.com/totegamma/concurrent/x/userkv"
	"github.com/totegamma/concurrent/x/util"
)

var domainHandlerProvider = wire.NewSet(domain.NewHandler, domain.NewService, domain.NewRepository)
var entityHandlerProvider = wire.NewSet(entity.NewHandler, entity.NewService, entity.NewRepository)
var streamHandlerProvider = wire.NewSet(stream.NewHandler, stream.NewService, stream.NewRepository, entity.NewService, entity.NewRepository)
var messageHandlerProvider = wire.NewSet(message.NewHandler, message.NewService, message.NewRepository)
var characterHandlerProvider = wire.NewSet(character.NewHandler, character.NewService, character.NewRepository)
var associationHandlerProvider = wire.NewSet(association.NewHandler, association.NewService, association.NewRepository, message.NewService, message.NewRepository)
var userkvHandlerProvider = wire.NewSet(userkv.NewHandler, userkv.NewService, userkv.NewRepository)
var collectionHandlerProvider = wire.NewSet(collection.NewHandler, collection.NewService, collection.NewRepository)

func SetupMessageHandler(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) message.Handler {
	wire.Build(messageHandlerProvider, stream.NewService, stream.NewRepository, entity.NewService, entity.NewRepository)
	return nil
}

func SetupCharacterHandler(db *gorm.DB, config util.Config) character.Handler {
	wire.Build(characterHandlerProvider)
	return nil
}

func SetupAssociationHandler(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) association.Handler {
	wire.Build(associationHandlerProvider, stream.NewService, stream.NewRepository, entity.NewService, entity.NewRepository)
	return nil
}

func SetupStreamHandler(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) stream.Handler {
	wire.Build(streamHandlerProvider)
	return nil
}

func SetupDomainHandler(db *gorm.DB, config util.Config) domain.Handler {
	wire.Build(domainHandlerProvider)
	return nil
}

func SetupEntityHandler(db *gorm.DB, rdb *redis.Client, config util.Config) entity.Handler {
	wire.Build(entityHandlerProvider)
	return nil
}

func SetupSocketHandler(rdb *redis.Client, manager socket.Manager, config util.Config) socket.Handler {
	wire.Build(socket.NewHandler, socket.NewService)
	return nil
}

func SetupAgent(db *gorm.DB, rdb *redis.Client, config util.Config) agent.Agent {
	wire.Build(agent.NewAgent, domain.NewService, domain.NewRepository, entity.NewService, entity.NewRepository)
	return nil
}

func SetupAuthHandler(db *gorm.DB, config util.Config) auth.Handler {
	wire.Build(auth.NewHandler, auth.NewService, entity.NewService, entity.NewRepository, domain.NewService, domain.NewRepository)
	return nil
}

func SetupAuthService(db *gorm.DB, config util.Config) auth.Service {
	wire.Build(auth.NewService, entity.NewService, entity.NewRepository, domain.NewService, domain.NewRepository)
	return nil
}

func SetupUserkvHandler(db *gorm.DB, rdb *redis.Client, config util.Config) userkv.Handler {
	wire.Build(userkvHandlerProvider, entity.NewService, entity.NewRepository)
	return nil
}

func SetupCollectionHandler(db *gorm.DB, rdb *redis.Client, config util.Config) collection.Handler {
	wire.Build(collectionHandlerProvider)
	return nil
}

func SetupSocketManager(mc *memcache.Client, db *gorm.DB, rdb *redis.Client, config util.Config) socket.Manager {
	wire.Build(socket.NewManager)
	return nil
}
