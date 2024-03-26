//go:generate go run go.uber.org/mock/mockgen -source=repository.go -destination=mock/repository.go
package userkv

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

// Repository is the interface for userkv repository
type Repository interface {
	Get(ctx context.Context, owner, key string) (string, error)
	Upsert(ctx context.Context, owner, key, value string) error
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
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var kv core.UserKV
	if err := r.db.Where("owner = ? AND key = ?", owner, key).First(&kv).Error; err != nil {
		return "", err
	}

	return kv.Value, nil
}

// Upsert updates a userkv
func (r *repository) Upsert(ctx context.Context, owner, key, value string) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpsert")
	defer span.End()

	kv := &core.UserKV{
		Owner: owner,
		Key:   key,
		Value: value,
	}

	return r.db.Save(kv).Error
}
