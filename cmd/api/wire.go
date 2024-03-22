//go:build wireinject

package main

import (
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/x/ack"
	"github.com/totegamma/concurrent/x/agent"
	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/auth"
	"github.com/totegamma/concurrent/x/collection"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/profile"
	"github.com/totegamma/concurrent/x/socket"
	"github.com/totegamma/concurrent/x/timeline"
	"github.com/totegamma/concurrent/x/userkv"
	"github.com/totegamma/concurrent/x/util"
)

var collectionHandlerProvider = wire.NewSet(collection.NewHandler, collection.NewService, collection.NewRepository)

var jwtServiceProvider = wire.NewSet(jwt.NewService, jwt.NewRepository)
var domainServiceProvider = wire.NewSet(domain.NewService, domain.NewRepository)
var entityServiceProvider = wire.NewSet(entity.NewService, entity.NewRepository, SetupJwtService)
var timelineServiceProvider = wire.NewSet(timeline.NewService, timeline.NewRepository, SetupEntityService, SetupDomainService)
var associationServiceProvider = wire.NewSet(association.NewService, association.NewRepository, SetupTimelineService, SetupMessageService, SetupKeyService)
var profileServiceProvider = wire.NewSet(profile.NewService, profile.NewRepository, SetupKeyService)
var authServiceProvider = wire.NewSet(auth.NewService, SetupEntityService, SetupDomainService, SetupKeyService)
var messageServiceProvider = wire.NewSet(message.NewService, message.NewRepository, SetupTimelineService, SetupKeyService)
var keyServiceProvider = wire.NewSet(key.NewService, key.NewRepository, SetupEntityService)
var userKvServiceProvider = wire.NewSet(userkv.NewService, userkv.NewRepository)
var ackServiceProvider = wire.NewSet(ack.NewService, ack.NewRepository, SetupEntityService, SetupKeyService)

func SetupJwtService(rdb *redis.Client) jwt.Service {
	wire.Build(jwtServiceProvider)
	return nil
}

func SetupAckService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) ack.Service {
	wire.Build(ackServiceProvider)
	return nil
}

func SetupKeyService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) key.Service {
	wire.Build(keyServiceProvider)
	return nil
}

func SetupMessageService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) message.Service {
	wire.Build(messageServiceProvider)
	return nil
}

func SetupProfileService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) profile.Service {
	wire.Build(profileServiceProvider)
	return nil
}

func SetupAssociationService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) association.Service {
	wire.Build(associationServiceProvider)
	return nil
}

func SetupTimelineService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) timeline.Service {
	wire.Build(timelineServiceProvider)
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
	wire.Build(agent.NewAgent, SetupEntityService, SetupDomainService)
	return nil
}

func SetupAuthService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) auth.Service {
	wire.Build(authServiceProvider)
	return nil
}

func SetupUserkvService(rdb *redis.Client) userkv.Service {
	wire.Build(userKvServiceProvider)
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
