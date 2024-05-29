// Code generated by Wire. DO NOT EDIT.

//go:generate go run github.com/google/wire/cmd/wire
//go:build !wireinject
// +build !wireinject

package concurrent

import (
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/x/ack"
	"github.com/totegamma/concurrent/x/agent"
	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/auth"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/jwt"
	"github.com/totegamma/concurrent/x/key"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/policy"
	"github.com/totegamma/concurrent/x/profile"
	"github.com/totegamma/concurrent/x/schema"
	"github.com/totegamma/concurrent/x/semanticid"
	"github.com/totegamma/concurrent/x/store"
	"github.com/totegamma/concurrent/x/subscription"
	"github.com/totegamma/concurrent/x/timeline"
	"github.com/totegamma/concurrent/x/userkv"
	"gorm.io/gorm"
)

// Injectors from wire.go:

func SetupPolicyService(rdb *redis.Client, config core.Config) core.PolicyService {
	repository := policy.NewRepository(rdb)
	policyService := policy.NewService(repository, config)
	return policyService
}

func SetupJwtService(rdb *redis.Client) jwt.Service {
	repository := jwt.NewRepository(rdb)
	service := jwt.NewService(repository)
	return service
}

func SetupAckService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client2 client.Client, config core.Config) core.AckService {
	repository := ack.NewRepository(db)
	entityService := SetupEntityService(db, rdb, mc, client2, config)
	keyService := SetupKeyService(db, rdb, mc, client2, config)
	ackService := ack.NewService(repository, client2, entityService, keyService, config)
	return ackService
}

func SetupKeyService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client2 client.Client, config core.Config) core.KeyService {
	repository := key.NewRepository(db, mc)
	keyService := key.NewService(repository, config)
	return keyService
}

func SetupMessageService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client2 client.Client, config core.Config) core.MessageService {
	schemaService := SetupSchemaService(db)
	repository := message.NewRepository(db, mc, schemaService)
	entityService := SetupEntityService(db, rdb, mc, client2, config)
	domainService := SetupDomainService(db, client2, config)
	timelineService := SetupTimelineService(db, rdb, mc, client2, config)
	keyService := SetupKeyService(db, rdb, mc, client2, config)
	policyService := SetupPolicyService(rdb, config)
	messageService := message.NewService(repository, client2, entityService, domainService, timelineService, keyService, policyService, config)
	return messageService
}

func SetupProfileService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client2 client.Client, config core.Config) core.ProfileService {
	schemaService := SetupSchemaService(db)
	repository := profile.NewRepository(db, mc, schemaService)
	entityService := SetupEntityService(db, rdb, mc, client2, config)
	semanticIDService := SetupSemanticidService(db)
	profileService := profile.NewService(repository, entityService, semanticIDService)
	return profileService
}

func SetupAssociationService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client2 client.Client, config core.Config) core.AssociationService {
	schemaService := SetupSchemaService(db)
	repository := association.NewRepository(db, mc, schemaService)
	entityService := SetupEntityService(db, rdb, mc, client2, config)
	domainService := SetupDomainService(db, client2, config)
	timelineService := SetupTimelineService(db, rdb, mc, client2, config)
	messageService := SetupMessageService(db, rdb, mc, client2, config)
	keyService := SetupKeyService(db, rdb, mc, client2, config)
	associationService := association.NewService(repository, client2, entityService, domainService, timelineService, messageService, keyService, config)
	return associationService
}

func SetupTimelineService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client2 client.Client, config core.Config) core.TimelineService {
	schemaService := SetupSchemaService(db)
	repository := timeline.NewRepository(db, rdb, mc, client2, schemaService, config)
	entityService := SetupEntityService(db, rdb, mc, client2, config)
	domainService := SetupDomainService(db, client2, config)
	semanticIDService := SetupSemanticidService(db)
	subscriptionService := SetupSubscriptionService(db)
	policyService := SetupPolicyService(rdb, config)
	timelineService := timeline.NewService(repository, entityService, domainService, semanticIDService, subscriptionService, policyService, config)
	return timelineService
}

func SetupDomainService(db *gorm.DB, client2 client.Client, config core.Config) core.DomainService {
	repository := domain.NewRepository(db)
	domainService := domain.NewService(repository, client2, config)
	return domainService
}

func SetupEntityService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client2 client.Client, config core.Config) core.EntityService {
	schemaService := SetupSchemaService(db)
	repository := entity.NewRepository(db, mc, schemaService)
	keyService := SetupKeyService(db, rdb, mc, client2, config)
	service := SetupJwtService(rdb)
	entityService := entity.NewService(repository, client2, config, keyService, service)
	return entityService
}

func SetupAgent(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client2 client.Client, config core.Config, repositoryPath string) core.AgentService {
	storeService := SetupStoreService(db, rdb, mc, client2, config, repositoryPath)
	agentService := agent.NewAgent(mc, rdb, storeService, config, repositoryPath)
	return agentService
}

func SetupAuthService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client2 client.Client, config core.Config) core.AuthService {
	entityService := SetupEntityService(db, rdb, mc, client2, config)
	domainService := SetupDomainService(db, client2, config)
	keyService := SetupKeyService(db, rdb, mc, client2, config)
	authService := auth.NewService(config, entityService, domainService, keyService)
	return authService
}

func SetupUserkvService(db *gorm.DB) userkv.Service {
	repository := userkv.NewRepository(db)
	service := userkv.NewService(repository)
	return service
}

func SetupSchemaService(db *gorm.DB) core.SchemaService {
	repository := schema.NewRepository(db)
	schemaService := schema.NewService(repository)
	return schemaService
}

func SetupStoreService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client2 client.Client, config core.Config, repositoryPath string) core.StoreService {
	repository := store.NewRepository(rdb)
	keyService := SetupKeyService(db, rdb, mc, client2, config)
	entityService := SetupEntityService(db, rdb, mc, client2, config)
	messageService := SetupMessageService(db, rdb, mc, client2, config)
	associationService := SetupAssociationService(db, rdb, mc, client2, config)
	profileService := SetupProfileService(db, rdb, mc, client2, config)
	timelineService := SetupTimelineService(db, rdb, mc, client2, config)
	ackService := SetupAckService(db, rdb, mc, client2, config)
	subscriptionService := SetupSubscriptionService(db)
	storeService := store.NewService(repository, keyService, entityService, messageService, associationService, profileService, timelineService, ackService, subscriptionService, config, repositoryPath)
	return storeService
}

func SetupSubscriptionService(db *gorm.DB) core.SubscriptionService {
	schemaService := SetupSchemaService(db)
	repository := subscription.NewRepository(db, schemaService)
	subscriptionService := subscription.NewService(repository)
	return subscriptionService
}

func SetupSemanticidService(db *gorm.DB) core.SemanticIDService {
	repository := semanticid.NewRepository(db)
	semanticIDService := semanticid.NewService(repository)
	return semanticIDService
}

// wire.go:

// Lv0
var jwtServiceProvider = wire.NewSet(jwt.NewService, jwt.NewRepository)

var schemaServiceProvider = wire.NewSet(schema.NewService, schema.NewRepository)

var domainServiceProvider = wire.NewSet(domain.NewService, domain.NewRepository)

var semanticidServiceProvider = wire.NewSet(semanticid.NewService, semanticid.NewRepository)

var userKvServiceProvider = wire.NewSet(userkv.NewService, userkv.NewRepository)

var policyServiceProvider = wire.NewSet(policy.NewService, policy.NewRepository)

var keyServiceProvider = wire.NewSet(key.NewService, key.NewRepository)

// Lv1
var entityServiceProvider = wire.NewSet(entity.NewService, entity.NewRepository, SetupJwtService, SetupSchemaService, SetupKeyService)

var subscriptionServiceProvider = wire.NewSet(subscription.NewService, subscription.NewRepository, SetupSchemaService)

// Lv2
var timelineServiceProvider = wire.NewSet(timeline.NewService, timeline.NewRepository, SetupEntityService, SetupDomainService, SetupSchemaService, SetupSemanticidService, SetupSubscriptionService, SetupPolicyService)

// Lv3
var profileServiceProvider = wire.NewSet(profile.NewService, profile.NewRepository, SetupEntityService, SetupKeyService, SetupSchemaService, SetupSemanticidService)

var authServiceProvider = wire.NewSet(auth.NewService, SetupEntityService, SetupDomainService, SetupKeyService)

var ackServiceProvider = wire.NewSet(ack.NewService, ack.NewRepository, SetupEntityService, SetupKeyService)

// Lv4
var messageServiceProvider = wire.NewSet(message.NewService, message.NewRepository, SetupEntityService, SetupDomainService, SetupTimelineService, SetupKeyService, SetupPolicyService, SetupSchemaService)

// Lv5
var associationServiceProvider = wire.NewSet(association.NewService, association.NewRepository, SetupEntityService, SetupDomainService, SetupTimelineService, SetupMessageService, SetupKeyService, SetupSchemaService)

// Lv6
var storeServiceProvider = wire.NewSet(store.NewService, store.NewRepository, SetupKeyService,
	SetupMessageService,
	SetupAssociationService,
	SetupProfileService,
	SetupEntityService,
	SetupTimelineService,
	SetupAckService,
	SetupSubscriptionService,
)

// Lv7
var agentServiceProvider = wire.NewSet(agent.NewAgent, SetupStoreService)
