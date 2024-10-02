//go:build wireinject

package concurrent

import (
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"

	"github.com/totegamma/concurrent/x/ack"
	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/auth"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/job"
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
)

// Lv0
var jwtServiceProvider = wire.NewSet(jwt.NewService, jwt.NewRepository)
var schemaServiceProvider = wire.NewSet(schema.NewService, schema.NewRepository)
var domainServiceProvider = wire.NewSet(domain.NewService, domain.NewRepository)
var semanticidServiceProvider = wire.NewSet(semanticid.NewService, semanticid.NewRepository)
var userKvServiceProvider = wire.NewSet(userkv.NewService, userkv.NewRepository)
var policyServiceProvider = wire.NewSet(policy.NewService, policy.NewRepository)
var keyServiceProvider = wire.NewSet(key.NewService, key.NewRepository)
var jobServiceProvider = wire.NewSet(job.NewService, job.NewRepository)

// Lv1
var entityServiceProvider = wire.NewSet(entity.NewService, entity.NewRepository, SetupJwtService, SetupSchemaService, SetupKeyService)

// Lv2
var timelineServiceProvider = wire.NewSet(timeline.NewService, timeline.NewRepository, SetupEntityService, SetupDomainService, SetupSchemaService, SetupSemanticidService, SetupSubscriptionService)
var subscriptionServiceProvider = wire.NewSet(subscription.NewService, subscription.NewRepository, SetupSchemaService, SetupEntityService)

// Lv3
var profileServiceProvider = wire.NewSet(profile.NewService, profile.NewRepository, SetupEntityService, SetupKeyService, SetupSchemaService, SetupSemanticidService)
var authServiceProvider = wire.NewSet(auth.NewService, SetupEntityService, SetupDomainService, SetupKeyService)
var ackServiceProvider = wire.NewSet(ack.NewService, ack.NewRepository, SetupEntityService, SetupKeyService)

// Lv4
var messageServiceProvider = wire.NewSet(message.NewService, message.NewRepository, SetupEntityService, SetupDomainService, SetupTimelineService, SetupKeyService, SetupSchemaService)

// Lv5
var associationServiceProvider = wire.NewSet(association.NewService, association.NewRepository, SetupEntityService, SetupDomainService, SetupTimelineService, SetupMessageService, SetupKeyService, SetupSchemaService, SetupProfileService, SetupSubscriptionService)

// Lv6
var storeServiceProvider = wire.NewSet(
	store.NewService,
	store.NewRepository,
	SetupKeyService,
	SetupMessageService,
	SetupAssociationService,
	SetupProfileService,
	SetupEntityService,
	SetupTimelineService,
	SetupAckService,
	SetupSubscriptionService,
	SetupSemanticidService,
)

// -----------

func SetupPolicyService(rdb *redis.Client, globalPolicy core.Policy, config core.Config) core.PolicyService {
	wire.Build(policyServiceProvider)
	return nil
}

func SetupJwtService(rdb *redis.Client) jwt.Service {
	wire.Build(jwtServiceProvider)
	return nil
}

func SetupJobService(db *gorm.DB) core.JobService {
	wire.Build(jobServiceProvider)
	return nil
}

func SetupAckService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client client.Client, policy core.PolicyService, config core.Config) core.AckService {
	wire.Build(ackServiceProvider)
	return nil
}

func SetupKeyService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client client.Client, config core.Config) core.KeyService {
	wire.Build(keyServiceProvider)
	return nil
}

func SetupMessageService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, keeper timeline.Keeper, client client.Client, policy core.PolicyService, config core.Config) core.MessageService {
	wire.Build(messageServiceProvider)
	return nil
}

func SetupProfileService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client client.Client, policy core.PolicyService, config core.Config) core.ProfileService {
	wire.Build(profileServiceProvider)
	return nil
}

func SetupAssociationService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, keeper timeline.Keeper, client client.Client, policy core.PolicyService, config core.Config) core.AssociationService {
	wire.Build(associationServiceProvider)
	return nil
}

func SetupTimelineService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, keeper timeline.Keeper, client client.Client, policy core.PolicyService, config core.Config) core.TimelineService {
	wire.Build(timelineServiceProvider)
	return nil
}

func SetupDomainService(db *gorm.DB, client client.Client, config core.Config) core.DomainService {
	wire.Build(domainServiceProvider)
	return nil
}

func SetupEntityService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client client.Client, policy core.PolicyService, config core.Config) core.EntityService {
	wire.Build(entityServiceProvider)
	return nil
}

func SetupAuthService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client client.Client, policy core.PolicyService, config core.Config) core.AuthService {
	wire.Build(authServiceProvider)
	return nil
}

func SetupUserkvService(db *gorm.DB) userkv.Service {
	wire.Build(userKvServiceProvider)
	return nil
}

func SetupSchemaService(db *gorm.DB) core.SchemaService {
	wire.Build(schemaServiceProvider)
	return nil
}

func SetupStoreService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, keeper timeline.Keeper, client client.Client, policy core.PolicyService, config core.Config, repositoryPath string) core.StoreService {
	wire.Build(storeServiceProvider)
	return nil
}

func SetupSubscriptionService(db *gorm.DB, rdb *redis.Client, mc *memcache.Client, client client.Client, policy core.PolicyService, config core.Config) core.SubscriptionService {
	wire.Build(subscriptionServiceProvider)
	return nil
}

func SetupSemanticidService(db *gorm.DB) core.SemanticIDService {
	wire.Build(semanticidServiceProvider)
	return nil
}
