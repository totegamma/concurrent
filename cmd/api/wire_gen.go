// Code generated by Wire. DO NOT EDIT.

//go:generate go run github.com/google/wire/cmd/wire
//go:build !wireinject
// +build !wireinject

package main

import (
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
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
	"github.com/totegamma/concurrent/x/schema"
	"github.com/totegamma/concurrent/x/socket"
	"github.com/totegamma/concurrent/x/store"
	"github.com/totegamma/concurrent/x/timeline"
	"github.com/totegamma/concurrent/x/userkv"
	"github.com/totegamma/concurrent/x/util"
	"gorm.io/gorm"
)

// Injectors from wire.go:

func SetupJwtService(rdb *redis.Client) jwt.Service {
	repository := jwt.NewRepository(rdb)
	service := jwt.NewService(repository)
	return service
}

func SetupAckService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) ack.Service {
	repository := ack.NewRepository(db)
	service := SetupEntityService(db, rdb, mc, config)
	keyService := SetupKeyService(db, rdb, mc, config)
	ackService := ack.NewService(repository, service, keyService, config)
	return ackService
}

func SetupKeyService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) key.Service {
	repository := key.NewRepository(db, mc)
	service := SetupEntityService(db, rdb, mc, config)
	keyService := key.NewService(repository, service, config)
	return keyService
}

func SetupMessageService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) message.Service {
	service := SetupSchemaService(db)
	repository := message.NewRepository(db, mc, service)
	timelineService := SetupTimelineService(db, rdb, mc, manager, config)
	keyService := SetupKeyService(db, rdb, mc, config)
	messageService := message.NewService(rdb, repository, timelineService, keyService)
	return messageService
}

func SetupProfileService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) profile.Service {
	repository := profile.NewRepository(db, mc)
	service := SetupKeyService(db, rdb, mc, config)
	profileService := profile.NewService(repository, service)
	return profileService
}

func SetupAssociationService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) association.Service {
	service := SetupSchemaService(db)
	repository := association.NewRepository(db, mc, service)
	timelineService := SetupTimelineService(db, rdb, mc, manager, config)
	messageService := SetupMessageService(db, rdb, mc, manager, config)
	keyService := SetupKeyService(db, rdb, mc, config)
	associationService := association.NewService(repository, timelineService, messageService, keyService)
	return associationService
}

func SetupTimelineService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) timeline.Service {
	repository := timeline.NewRepository(db, rdb, mc, manager, config)
	service := SetupEntityService(db, rdb, mc, config)
	domainService := SetupDomainService(db, config)
	timelineService := timeline.NewService(repository, service, domainService, config)
	return timelineService
}

func SetupDomainService(db *gorm.DB, config util.Config) domain.Service {
	repository := domain.NewRepository(db)
	service := domain.NewService(repository, config)
	return service
}

func SetupEntityService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) entity.Service {
	repository := entity.NewRepository(db, mc)
	service := SetupJwtService(rdb)
	entityService := entity.NewService(repository, config, service)
	return entityService
}

func SetupSocketHandler(rdb *redis.Client, manager socket.Manager, config util.Config) socket.Handler {
	service := socket.NewService()
	handler := socket.NewHandler(service, rdb, manager)
	return handler
}

func SetupAgent(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) agent.Agent {
	service := SetupDomainService(db, config)
	entityService := SetupEntityService(db, rdb, mc, config)
	agentAgent := agent.NewAgent(rdb, config, service, entityService)
	return agentAgent
}

func SetupAuthService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, config util.Config) auth.Service {
	service := SetupEntityService(db, rdb, mc, config)
	domainService := SetupDomainService(db, config)
	keyService := SetupKeyService(db, rdb, mc, config)
	authService := auth.NewService(config, service, domainService, keyService)
	return authService
}

func SetupUserkvService(rdb *redis.Client) userkv.Service {
	repository := userkv.NewRepository(rdb)
	service := userkv.NewService(repository)
	return service
}

func SetupCollectionHandler(db *gorm.DB, rdb *redis.Client, config util.Config) collection.Handler {
	repository := collection.NewRepository(db)
	service := collection.NewService(repository)
	handler := collection.NewHandler(service)
	return handler
}

func SetupSocketManager(mc *memcache.Client, db *gorm.DB, rdb *redis.Client, config util.Config) socket.Manager {
	manager := socket.NewManager(mc, rdb, config)
	return manager
}

func SetupSchemaService(db *gorm.DB) schema.Service {
	repository := schema.NewRepository(db)
	service := schema.NewService(repository)
	return service
}

func SetupStoreService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, manager socket.Manager, config util.Config) store.Service {
	service := SetupKeyService(db, rdb, mc, config)
	messageService := SetupMessageService(db, rdb, mc, manager, config)
	associationService := SetupAssociationService(db, rdb, mc, manager, config)
	profileService := SetupProfileService(db, rdb, mc, config)
	storeService := store.NewService(service, messageService, associationService, profileService)
	return storeService
}

// wire.go:

var collectionHandlerProvider = wire.NewSet(collection.NewHandler, collection.NewService, collection.NewRepository)

// Lv0
var jwtServiceProvider = wire.NewSet(jwt.NewService, jwt.NewRepository)

var schemaServiceProvider = wire.NewSet(schema.NewService, schema.NewRepository)

var domainServiceProvider = wire.NewSet(domain.NewService, domain.NewRepository)

var userKvServiceProvider = wire.NewSet(userkv.NewService, userkv.NewRepository)

var entityServiceProvider = wire.NewSet(entity.NewService, entity.NewRepository, SetupJwtService)

// Lv1
var timelineServiceProvider = wire.NewSet(timeline.NewService, timeline.NewRepository, SetupEntityService, SetupDomainService)

// Lv2
var keyServiceProvider = wire.NewSet(key.NewService, key.NewRepository, SetupEntityService)

// Lv3
var profileServiceProvider = wire.NewSet(profile.NewService, profile.NewRepository, SetupKeyService)

var authServiceProvider = wire.NewSet(auth.NewService, SetupEntityService, SetupDomainService, SetupKeyService)

var ackServiceProvider = wire.NewSet(ack.NewService, ack.NewRepository, SetupEntityService, SetupKeyService)

// Lv4
var messageServiceProvider = wire.NewSet(message.NewService, message.NewRepository, SetupTimelineService, SetupKeyService, SetupSchemaService)

// Lv5
var associationServiceProvider = wire.NewSet(association.NewService, association.NewRepository, SetupTimelineService, SetupMessageService, SetupKeyService, SetupSchemaService)

// Lv6
var storeServiceProvider = wire.NewSet(store.NewService, SetupKeyService, SetupMessageService, SetupAssociationService, SetupProfileService)
