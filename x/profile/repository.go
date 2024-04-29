package profile

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/bradfitz/gomemcache/memcache"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/x/core"
)

// Repository is the interface for profile repository
type Repository interface {
	Upsert(ctx context.Context, profile core.Profile) (core.Profile, error)
	Get(ctx context.Context, id string) (core.Profile, error)
	GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]core.Profile, error)
	GetByAuthor(ctx context.Context, owner string) ([]core.Profile, error)
	GetBySchema(ctx context.Context, schema string) ([]core.Profile, error)
	Delete(ctx context.Context, id string) (core.Profile, error)
	Count(ctx context.Context) (int64, error)
}

type repository struct {
	db     *gorm.DB
	mc     *memcache.Client
	schema core.SchemaService
}

// NewRepository creates a new profile repository
func NewRepository(db *gorm.DB, mc *memcache.Client, schema core.SchemaService) Repository {

	var count int64
	err := db.Model(&core.Profile{}).Count(&count).Error
	if err != nil {
		slog.Error(
			"failed to count profiles",
			slog.String("error", err.Error()),
		)
	}

	mc.Set(&memcache.Item{Key: "profile_count", Value: []byte(strconv.FormatInt(count, 10))})

	return &repository{db, mc, schema}
}

// Total returns the total number of profiles
func (r *repository) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "Profile.Repository.Count")
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
func (r *repository) Upsert(ctx context.Context, profile core.Profile) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Repository.Upsert")
	defer span.End()

	if profile.ID == "" {
		return profile, errors.New("profile id is required")
	}

	if len(profile.ID) == 27 {
		if profile.ID[0] != 'p' {
			return profile, errors.New("profile id must start with 'p'")
		}
		profile.ID = profile.ID[1:]
	}

	if len(profile.ID) != 26 {
		return profile, errors.New("profile id must be 26 characters long")
	}

	schemaID, err := r.schema.UrlToID(ctx, profile.Schema)
	if err != nil {
		return profile, err
	}
	profile.SchemaID = schemaID

	err = r.db.WithContext(ctx).Save(&profile).Error
	if err != nil {
		span.RecordError(err)
		return profile, err
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

	profile.ID = "p" + profile.ID

	return profile, nil
}

// Get returns a profile by owner and schema
func (r *repository) GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Repository.GetByAuthorAndSchema")
	defer span.End()

	var profiles []core.Profile
	if err := r.db.WithContext(ctx).Where("author = $1 AND schema = $2", owner, schema).Find(&profiles).Error; err != nil {
		return []core.Profile{}, err
	}
	if profiles == nil {
		return []core.Profile{}, nil
	}

	for i := range profiles {
		profiles[i].ID = "p" + profiles[i].ID

		schemaUrl, err := r.schema.IDToUrl(ctx, profiles[i].SchemaID)
		if err != nil {
			return []core.Profile{}, err
		}
		profiles[i].Schema = schemaUrl
	}

	return profiles, nil
}

func (r *repository) GetByAuthor(ctx context.Context, owner string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Repository.GetByAuthor")
	defer span.End()

	var profiles []core.Profile
	if err := r.db.WithContext(ctx).Where("author = $1", owner).Find(&profiles).Error; err != nil {
		return []core.Profile{}, err
	}
	if profiles == nil {
		return []core.Profile{}, nil
	}

	for i := range profiles {
		profiles[i].ID = "p" + profiles[i].ID

		schemaUrl, err := r.schema.IDToUrl(ctx, profiles[i].SchemaID)
		if err != nil {
			return []core.Profile{}, err
		}
		profiles[i].Schema = schemaUrl
	}

	return profiles, nil
}

func (r *repository) GetBySchema(ctx context.Context, schema string) ([]core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Repository.GetBySchema")
	defer span.End()

	var profiles []core.Profile
	if err := r.db.WithContext(ctx).Where("schema = $1", schema).Find(&profiles).Error; err != nil {
		return []core.Profile{}, err
	}
	if profiles == nil {
		return []core.Profile{}, nil
	}

	for i := range profiles {
		profiles[i].ID = "p" + profiles[i].ID

		schemaUrl, err := r.schema.IDToUrl(ctx, profiles[i].SchemaID)
		if err != nil {
			return []core.Profile{}, err
		}
		profiles[i].Schema = schemaUrl

	}

	return profiles, nil
}

func (r *repository) Delete(ctx context.Context, id string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Repository.Delete")
	defer span.End()

	if len(id) == 27 {
		if id[0] != 'p' {
			return core.Profile{}, errors.New("profile id must start with 'p'")
		}
		id = id[1:]
	}

	if len(id) != 26 {
		return core.Profile{}, errors.New("profile id must be 26 characters long")
	}

	var profile core.Profile
	if err := r.db.WithContext(ctx).Where("id = $1", id).Delete(&profile).Error; err != nil {
		return core.Profile{}, err
	}

	schemaUrl, err := r.schema.IDToUrl(ctx, profile.SchemaID)
	if err != nil {
		return core.Profile{}, err
	}
	profile.Schema = schemaUrl

	profile.ID = "p" + profile.ID

	return profile, nil
}

func (r *repository) Get(ctx context.Context, id string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Repository.Get")
	defer span.End()

	if len(id) == 27 {
		if id[0] != 'p' {
			return core.Profile{}, errors.New("profile id must start with 'p'")
		}
		id = id[1:]
	}

	if len(id) != 26 {
		return core.Profile{}, errors.New("profile id must be 26 characters long")
	}

	var profile core.Profile
	if err := r.db.WithContext(ctx).Where("id = $1", id).First(&profile).Error; err != nil {
		return core.Profile{}, err
	}

	schemaUrl, err := r.schema.IDToUrl(ctx, profile.SchemaID)
	if err != nil {
		return core.Profile{}, err
	}
	profile.Schema = schemaUrl

	profile.ID = "p" + profile.ID

	return profile, nil
}
