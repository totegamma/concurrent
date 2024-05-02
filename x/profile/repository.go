package profile

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/bradfitz/gomemcache/memcache"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/core"
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

func (r *repository) normalizeDBID(id string) (string, error) {

	normalized := id

	if len(normalized) == 27 {
		if normalized[0] != 'p' {
			return "", errors.New("profile id must start with 'p'")
		}
		normalized = normalized[1:]
	}

	if len(normalized) != 26 {
		return "", errors.New("profile id must be 26 characters long")
	}

	return normalized, nil
}

func (r *repository) preProcess(ctx context.Context, profile *core.Profile) error {

	var err error
	profile.ID, err = r.normalizeDBID(profile.ID)
	if err != nil {
		return err
	}

	if profile.SchemaID == 0 {
		schemaID, err := r.schema.UrlToID(ctx, profile.Schema)
		if err != nil {
			return err
		}
		profile.SchemaID = schemaID
	}

	if profile.PolicyID == 0 && profile.Policy != "" {
		policyID, err := r.schema.UrlToID(ctx, profile.Policy)
		if err != nil {
			return err
		}
		profile.PolicyID = policyID
	}

	return nil
}

func (r *repository) postProcess(ctx context.Context, profile *core.Profile) error {

	if len(profile.ID) == 26 {
		profile.ID = "p" + profile.ID
	}

	if profile.SchemaID != 0 && profile.Schema == "" {
		schemaUrl, err := r.schema.IDToUrl(ctx, profile.SchemaID)
		if err != nil {
			return err
		}
		profile.Schema = schemaUrl
	}

	if profile.PolicyID != 0 && profile.Policy == "" {
		policyUrl, err := r.schema.IDToUrl(ctx, profile.PolicyID)
		if err != nil {
			return err
		}
		profile.Policy = policyUrl
	}

	return nil
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

	err := r.preProcess(ctx, &profile)
	if err != nil {
		return profile, err
	}

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

	err = r.postProcess(ctx, &profile)
	if err != nil {
		return profile, err
	}

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
		err := r.postProcess(ctx, &profiles[i])
		if err != nil {
			return []core.Profile{}, err
		}
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
		err := r.postProcess(ctx, &profiles[i])
		if err != nil {
			return []core.Profile{}, err
		}
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
		err := r.postProcess(ctx, &profiles[i])
		if err != nil {
			return []core.Profile{}, err
		}
	}

	return profiles, nil
}

func (r *repository) Delete(ctx context.Context, id string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Repository.Delete")
	defer span.End()

	id, err := r.normalizeDBID(id)
	if err != nil {
		return core.Profile{}, err
	}

	var profile core.Profile
	if err := r.db.WithContext(ctx).Where("id = $1", id).Delete(&profile).Error; err != nil {
		return core.Profile{}, err
	}

	err = r.postProcess(ctx, &profile)
	if err != nil {
		return core.Profile{}, err
	}

	return profile, nil
}

func (r *repository) Get(ctx context.Context, id string) (core.Profile, error) {
	ctx, span := tracer.Start(ctx, "Profile.Repository.Get")
	defer span.End()

	id, err := r.normalizeDBID(id)
	if err != nil {
		return core.Profile{}, err
	}

	var profile core.Profile
	if err := r.db.WithContext(ctx).Where("id = $1", id).First(&profile).Error; err != nil {
		return core.Profile{}, err
	}

	err = r.postProcess(ctx, &profile)
	if err != nil {
		return core.Profile{}, err
	}

	return profile, nil
}
