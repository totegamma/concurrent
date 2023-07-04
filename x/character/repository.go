package character

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

// Repository is a repository for character objects
type Repository struct {
	db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Upsert upserts existing character
func (r *Repository) Upsert(ctx context.Context, character core.Character) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpsert")
	defer span.End()
	return r.db.WithContext(ctx).Save(&character).Error
}

// Get returns character list which matches specified owner and chema
func (r *Repository) Get(ctx context.Context, owner string, schema string) ([]core.Character, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var characters []core.Character
	if err := r.db.WithContext(ctx).Where("author = $1 AND schema = $2", owner, schema).Find(&characters).Error; err != nil {
		return []core.Character{}, err
	}
	if characters == nil {
		return []core.Character{}, nil
	}
	return characters, nil
}
