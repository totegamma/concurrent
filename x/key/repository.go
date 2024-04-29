package key

import (
	"context"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/core"
)

type Repository interface {
	Enact(ctx context.Context, key core.Key) (core.Key, error)
	Revoke(ctx context.Context, keyID string, payload string, signature string, signedAt time.Time) (core.Key, error)
	Get(ctx context.Context, keyID string) (core.Key, error)
	GetAll(ctx context.Context, owner string) ([]core.Key, error)
	SetRemoteKeyValidationCache(ctx context.Context, keyID string, resovation string) error
	GetRemoteKeyValidationCache(ctx context.Context, keyID string) (string, error)
}

type repository struct {
	db *gorm.DB
	mc *memcache.Client
}

func NewRepository(db *gorm.DB, mc *memcache.Client) Repository {
	return &repository{db, mc}
}

func (r *repository) SetRemoteKeyValidationCache(ctx context.Context, keyID string, resovation string) error {
	ctx, span := tracer.Start(ctx, "Key.Repository.SetRemoteKeyValidationCache")
	defer span.End()

	// TTL 10 minutes
	err := r.mc.Set(&memcache.Item{Key: keyID, Value: []byte(resovation), Expiration: 600})
	if err != nil {
		return err
	}

	return nil
}

func (r *repository) GetRemoteKeyValidationCache(ctx context.Context, keyID string) (string, error) {
	ctx, span := tracer.Start(ctx, "Key.Repository.GetRemoteKeyValidationCache")
	defer span.End()

	item, err := r.mc.Get(keyID)
	if err != nil {
		return "", err
	}

	return string(item.Value), nil
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
