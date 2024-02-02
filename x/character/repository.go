package character

import (
	"context"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
	"log/slog"
	"strconv"
)

// Repository is the interface for character repository
type Repository interface {
	Upsert(ctx context.Context, character core.Character) error
	Get(ctx context.Context, owner string, schema string) ([]core.Character, error)
	Count(ctx context.Context) (int64, error)
}

type repository struct {
	db *gorm.DB
	mc *memcache.Client
}

// NewRepository creates a new character repository
func NewRepository(db *gorm.DB, mc *memcache.Client) Repository {

	var count int64
	err := db.Model(&core.Character{}).Count(&count).Error
	if err != nil {
		slog.Error(
			"failed to count characters",
			slog.String("error", err.Error()),
		)
	}

	mc.Set(&memcache.Item{Key: "character_count", Value: []byte(strconv.FormatInt(count, 10))})

	return &repository{db, mc}
}

// Total returns the total number of characters
func (r *repository) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "RepositoryTotal")
	defer span.End()

	item, err := r.mc.Get("character_count")
	if err != nil {
		span.RecordError(err)
		return 0, err
	}

	count, err := strconv.ParseInt(string(item.Value), 10, 64)
	if err != nil {
		span.RecordError(err)
		return 0, err
	}
	return count, nil
}

// Upsert creates and updates character
func (r *repository) Upsert(ctx context.Context, character core.Character) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpsert")
	defer span.End()

	err := r.db.WithContext(ctx).Save(&character).Error
	if err != nil {
		span.RecordError(err)
		return err
	}

	var count int64
	err = r.db.Model(&core.Character{}).Count(&count).Error
	if err != nil {
		slog.Error(
			"failed to count associations",
			slog.String("error", err.Error()),
		)
	}

	r.mc.Set(&memcache.Item{Key: "character_count", Value: []byte(strconv.FormatInt(count, 10))})

	return nil
}

// Get returns a character by owner and schema
func (r *repository) Get(ctx context.Context, owner string, schema string) ([]core.Character, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var characters []core.Character
	if err := r.db.WithContext(ctx).Preload("Associations").Where("author = $1 AND schema = $2", owner, schema).Find(&characters).Error; err != nil {
		return []core.Character{}, err
	}
	if characters == nil {
		return []core.Character{}, nil
	}
	return characters, nil
}
