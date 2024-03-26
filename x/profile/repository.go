package profile

import (
	"context"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
	"log/slog"
	"strconv"
)

// Repository is the interface for profile repository
type Repository interface {
	Upsert(ctx context.Context, profile core.Profile) error
	Get(ctx context.Context, id string) (core.Profile, error)
	GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]core.Profile, error)
	GetByAuthor(ctx context.Context, owner string) ([]core.Profile, error)
	GetBySchema(ctx context.Context, schema string) ([]core.Profile, error)
	Delete(ctx context.Context, id string) (core.Profile, error)
	Count(ctx context.Context) (int64, error)
}

type repository struct {
	db *gorm.DB
	mc *memcache.Client
}

// NewRepository creates a new profile repository
func NewRepository(db *gorm.DB, mc *memcache.Client) Repository {

	var count int64
	err := db.Model(&core.Profile{}).Count(&count).Error
	if err != nil {
		slog.Error(
			"failed to count profiles",
			slog.String("error", err.Error()),
		)
	}

	mc.Set(&memcache.Item{Key: "profile_count", Value: []byte(strconv.FormatInt(count, 10))})

	return &repository{db, mc}
}

// Total returns the total number of profiles
func (r *repository) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "RepositoryTotal")
	defer span.End()

	item, err := r.mc.Get("profile_count")
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

// Upsert creates and updates profile
func (r *repository) Upsert(ctx context.Context, profile core.Profile) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpsert")
	defer span.End()

	err := r.db.WithContext(ctx).Save(&profile).Error
	if err != nil {
		span.RecordError(err)
		return err
	}

	var count int64
	err = r.db.Model(&core.Profile{}).Count(&count).Error
	if err != nil {
		slog.Error(
			"failed to count associations",
			slog.String("error", err.Error()),
		)
	}

	r.mc.Set(&memcache.Item{Key: "profile_count", Value: []byte(strconv.FormatInt(count, 10))})

	return nil
}

// Get returns a profile by owner and schema
func (r *repository) GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetByAuthorAndSchema")
	defer span.End()

	var profiles []core.Profile
	if err := r.db.WithContext(ctx).Preload("Associations").Where("author = $1 AND schema = $2", owner, schema).Find(&profiles).Error; err != nil {
		return []core.Profile{}, err
	}
	if profiles == nil {
		return []core.Profile{}, nil
	}
	return profiles, nil
}

func (r *repository) GetByAuthor(ctx context.Context, owner string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetByAuthor")
	defer span.End()

	var profiles []core.Profile
	if err := r.db.WithContext(ctx).Preload("Associations").Where("author = $1", owner).Find(&profiles).Error; err != nil {
		return []core.Profile{}, err
	}
	if profiles == nil {
		return []core.Profile{}, nil
	}
	return profiles, nil
}

func (r *repository) GetBySchema(ctx context.Context, schema string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetBySchema")
	defer span.End()

	var profiles []core.Profile
	if err := r.db.WithContext(ctx).Preload("Associations").Where("schema = $1", schema).Find(&profiles).Error; err != nil {
		return []core.Profile{}, err
	}
	if profiles == nil {
		return []core.Profile{}, nil
	}
	return profiles, nil
}

func (r *repository) Delete(ctx context.Context, id string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "RepositoryDelete")
	defer span.End()

	var profile core.Profile
	if err := r.db.WithContext(ctx).Where("id = $1", id).Delete(&profile).Error; err != nil {
		return core.Profile{}, err
	}

	return profile, nil
}

func (r *repository) Get(ctx context.Context, id string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var profile core.Profile
	if err := r.db.WithContext(ctx).Preload("Associations").Where("id = $1", id).First(&profile).Error; err != nil {
		return core.Profile{}, err
	}
	return profile, nil
}
