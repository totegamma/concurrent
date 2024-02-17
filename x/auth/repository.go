package auth

import (
	"context"

	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

type Repository interface {
	Enact(ctx context.Context, key core.Key) (core.Key, error)
	Revoke(ctx context.Context, keyID string, payload string, signature string) (core.Key, error)
	Get(ctx context.Context, keyID string) (core.Key, error)
	GetAll(ctx context.Context, owner string) ([]core.Key, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db}
}

func (r *repository) Get(ctx context.Context, keyID string) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "Repository.Get")
	defer span.End()

	var key core.Key
	err := r.db.Where("id = ?", keyID).First(&key).Error
	if err != nil {
		return core.Key{}, err
	}

	return key, nil
}

func (r *repository) Enact(ctx context.Context, key core.Key) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "Repository.Enact")
	defer span.End()

	err := r.db.Create(&key).Error
	if err != nil {
		return core.Key{}, err
	}

	return key, nil
}

func (r *repository) Revoke(ctx context.Context, keyID string, payload string, signature string) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "Repository.Revoke")
	defer span.End()

	err := r.db.Model(&core.Key{}).Where("id = ?", keyID).Updates(core.Key{RevokePayload: payload, RevokeSignature: signature}).Error
	if err != nil {
		return core.Key{}, err
	}

	var key core.Key
	err = r.db.Where("id = ?", keyID).First(&key).Error
	if err != nil {
		return core.Key{}, err
	}

	return key, nil
}

func (r *repository) GetAll(ctx context.Context, owner string) ([]core.Key, error) {
	ctx, span := tracer.Start(ctx, "Repository.GetAll")
	defer span.End()

	var keys []core.Key
	err := r.db.Where("root = ?", owner).Find(&keys).Error
	if err != nil {
		return nil, err
	}

	return keys, nil
}
