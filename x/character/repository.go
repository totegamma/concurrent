package character

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

// Repository is the interface for character repository
type Repository interface {
	Upsert(ctx context.Context, character core.Character) error
	Get(ctx context.Context, owner string, schema string) ([]core.Character, error)
}

type repository struct {
	db *gorm.DB
}

// NewRepository creates a new character repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// Upsert creates and updates character
func (r *repository) Upsert(ctx context.Context, character core.Character) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpsert")
	defer span.End()
	return r.db.WithContext(ctx).Save(&character).Error
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
