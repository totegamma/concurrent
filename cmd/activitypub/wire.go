//go:build wireinject

package main

import (
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/x/activitypub"
	"github.com/totegamma/concurrent/x/auth"
	"github.com/totegamma/concurrent/x/domain"
	"github.com/totegamma/concurrent/x/entity"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/stream"
	"github.com/totegamma/concurrent/x/util"
	"github.com/totegamma/concurrent/x/association"
)

func SetupAuthService(db *gorm.DB, config util.Config) *auth.Service {
	wire.Build(auth.NewService, entity.NewService, entity.NewRepository, domain.NewService, domain.NewRepository)
	return &auth.Service{}
}

func SetupActivitypubHandler(db *gorm.DB, rdb *redis.Client, config util.Config, apConfig activitypub.APConfig) *activitypub.Handler {
	wire.Build(activitypub.NewHandler, activitypub.NewRepository, message.NewService, message.NewRepository, association.NewService, association.NewRepository, entity.NewService, entity.NewRepository, stream.NewService, stream.NewRepository)
	return &activitypub.Handler{}
}
