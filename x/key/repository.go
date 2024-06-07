package key

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
)

type Repository interface {
	Enact(ctx context.Context, key core.Key) (core.Key, error)
	Revoke(ctx context.Context, keyID string, payload string, signature string, signedAt time.Time) (core.Key, error)
	Get(ctx context.Context, keyID string) (core.Key, error)
	GetAll(ctx context.Context, owner string) ([]core.Key, error)
	GetRemoteKeyResolution(ctx context.Context, remote string, keyID string) ([]core.Key, error)
	Clean(ctx context.Context, ccid string) error
}

type repository struct {
	db     *gorm.DB
	mc     *memcache.Client
	client client.Client
}

func NewRepository(
	db *gorm.DB,
	mc *memcache.Client,
	client client.Client,
) Repository {
	return &repository{db, mc, client}
}

func (r *repository) GetRemoteKeyResolution(ctx context.Context, remote string, keyID string) ([]core.Key, error) {
	ctx, span := tracer.Start(ctx, "Key.Repository.GetRemoteKeyResolution")
	defer span.End()

	// check cache first
	item, err := r.mc.Get(keyID)
	if err == nil {
		var keys []core.Key
		err = json.Unmarshal(item.Value, &keys)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		return keys, nil
	}
	// get from remote
	keys, err := r.client.GetKey(ctx, remote, keyID, nil)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	_, err = ValidateKeyResolution(keys) // TODO: should have a negative cache
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	// cache
	value, err := json.Marshal(keys)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	// TTL 30 minutes
	err = r.mc.Set(&memcache.Item{Key: keyID, Value: value, Expiration: 1800})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return keys, nil
}

func (r *repository) Get(ctx context.Context, keyID string) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "Key.Repository.Get")
	defer span.End()

	var key core.Key
	err := r.db.Where("id = ?", keyID).First(&key).Error
	if err != nil {
		return core.Key{}, err
	}

	return key, nil
}

func (r *repository) Enact(ctx context.Context, key core.Key) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "Key.Repository.Enact")
	defer span.End()

	err := r.db.Create(&key).Error
	if err != nil {
		return core.Key{}, err
	}

	return key, nil
}

func (r *repository) Revoke(ctx context.Context, keyID string, payload string, signature string, signedAt time.Time) (core.Key, error) {
	ctx, span := tracer.Start(ctx, "Key.Repository.Revoke")
	defer span.End()

	err := r.db.Model(&core.Key{}).Where("id = ?", keyID).Updates(
		core.Key{
			RevokeDocument:  &payload,
			RevokeSignature: &signature,
			ValidUntil:      signedAt,
		},
	).Error
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
	ctx, span := tracer.Start(ctx, "Key.Repository.GetAll")
	defer span.End()

	var keys []core.Key
	err := r.db.Where("root = ?", owner).Find(&keys).Error
	if err != nil {
		return nil, err
	}

	return keys, nil
}

func (r *repository) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "Key.Repository.Clean")
	defer span.End()

	err := r.db.Where("root = ?", ccid).Delete(&core.Key{}).Error
	if err != nil {
		return err
	}

	return nil
}
