//go:generate go run go.uber.org/mock/mockgen -source=repository.go -destination=mock/repository.go
package userkv

import (
	"context"
	"github.com/totegamma/concurrent/core"
	"gorm.io/gorm"
)

// Repository is the interface for userkv repository
type Repository interface {
	Get(ctx context.Context, owner, key string) (string, error)
	Upsert(ctx context.Context, owner, key, value string) error
	Clean(ctx context.Context, ccid string) error
}

type repository struct {
	db *gorm.DB
}

// NewRepository creates a new userkv repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db}
}

// Get returns a userkv by ID
func (r *repository) Get(ctx context.Context, owner, key string) (string, error) {
	ctx, span := tracer.Start(ctx, "UserKV.Repository.Get")
	defer span.End()

	var kv core.UserKV
	if err := r.db.Where("owner = ? AND key = ?", owner, key).First(&kv).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", core.NewErrorNotFound()
		}
		span.RecordError(err)
		return "", err
	}

	return kv.Value, nil
}

// Upsert updates a userkv
func (r *repository) Upsert(ctx context.Context, owner, key, value string) error {
	ctx, span := tracer.Start(ctx, "UserKV.Repository.Upsert")
	defer span.End()

	kv := &core.UserKV{
		Owner: owner,
		Key:   key,
		Value: value,
	}

	return r.db.Save(kv).Error
}

// Clean deletes all userkvs for a given owner
func (r *repository) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "UserKV.Repository.Clean")
	defer span.End()

	return r.db.Where("owner = ?", ccid).Delete(&core.UserKV{}).Error
}
