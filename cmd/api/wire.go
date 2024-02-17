//go:build wireinject

package main

import (
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/x/agent"
	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/auth"
	"github.com/totegamma/concurrent/x/character"
	"github.com/totegamma/concurrent/x/collection"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/socket"
	"github.com/totegamma/concurrent/x/stream"
	"github.com/totegamma/concurrent/x/userkv"
	"github.com/totegamma/concurrent/x/util"
)

var userkvHandlerProvider = wire.NewSet(userkv.NewHandler, userkv.NewService, userkv.NewRepository)
var collectionHandlerProvider = wire.NewSet(collection.NewHandler, collection.NewService, collection.NewRepository)

var domainServiceProvider = wire.NewSet(domain.NewService, domain.NewRepository)
var jwtServiceProvider = wire.NewSet(jwt.NewService, jwt.NewRepository)
var entityServiceProvider = wire.NewSet(entity.NewService, entity.NewRepository, jwtServiceProvider)
var streamServiceProvider = wire.NewSet(stream.NewService, stream.NewRepository, entityServiceProvider)
var associationServiceProvider = wire.NewSet(association.NewService, association.NewRepository, messageServiceProvider)
var characterServiceProvider = wire.NewSet(character.NewService, character.NewRepository)
var authServiceProvider = wire.NewSet(auth.NewService, auth.NewRepository, entityServiceProvider, domainServiceProvider, jwtServiceProvider)
var messageServiceProvider = wire.NewSet(message.NewService, message.NewRepository, streamServiceProvider, authServiceProvider)

func SetupMessageService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) message.Service {
	wire.Build(messageServiceProvider)
	return nil
}

func SetupCharacterService(db *gorm.DB, mc *memcache.Client, config util.Config) character.Service {
	wire.Build(characterServiceProvider)
	return nil
}

func SetupAssociationService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) association.Service {
	wire.Build(associationServiceProvider)
	return nil
}

func SetupStreamService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) stream.Service {
	wire.Build(streamServiceProvider)
	return nil
}

func SetupDomainService(db *gorm.DB, config util.Config) domain.Service {
	wire.Build(domainServiceProvider)
	return nil
}

func SetupEntityService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) entity.Service {
	wire.Build(entityServiceProvider)
	return nil
}

func SetupSocketHandler(rdb *redis.Client, manager socket.Manager, config util.Config) socket.Handler {
	wire.Build(socket.NewHandler, socket.NewService)
	return nil
}

func SetupAgent(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) agent.Agent {
	wire.Build(agent.NewAgent, jwtServiceProvider, domain.NewService, domain.NewRepository, entity.NewService, entity.NewRepository)
	return nil
}

func SetupAuthService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) auth.Service {
	wire.Build(authServiceProvider)
	return nil
}

func SetupUserkvHandler(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) userkv.Handler {
	wire.Build(userkvHandlerProvider, jwtServiceProvider, entity.NewService, entity.NewRepository)
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
