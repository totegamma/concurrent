package association

import (
	"context"
	"fmt"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

// Repository is the interface for association repository
type Repository interface {
	Create(ctx context.Context, association core.Association) (core.Association, error)
	Get(ctx context.Context, id string) (core.Association, error)
	GetOwn(ctx context.Context, author string) ([]core.Association, error)
	Delete(ctx context.Context, id string) (core.Association, error)
}

type repository struct {
	db *gorm.DB
}

// NewRepository creates a new association repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// Create creates new association
func (r *repository) Create(ctx context.Context, association core.Association) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreate")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&association).Error

	return association, err
}

// Get returns a Association by ID
func (r *repository) Get(ctx context.Context, id string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var association core.Association
	err := r.db.WithContext(ctx).Where("id = $1", id).First(&association).Error
	return association, err
}

// GetOwn returns all associations which owned by specified owner
func (r *repository) GetOwn(ctx context.Context, author string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetOwn")
	defer span.End()

	var associations []core.Association
	err := r.db.WithContext(ctx).Where("author = $1", author).Error
	return associations, err
}

// Delete deletes a association by ID
func (r *repository) Delete(ctx context.Context, id string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryDelete")
	defer span.End()

	var deleted core.Association
	if err := r.db.WithContext(ctx).First(&deleted, "id = ?", id).Error; err != nil {
		fmt.Printf("Error finding association: %v\n", err)
		return core.Association{}, err
	}
	err := r.db.WithContext(ctx).Where("id = $1", id).Delete(&core.Association{}).Error
	if err != nil {
		fmt.Printf("Error deleting association: %v\n", err)
		return core.Association{}, err
	}
	return deleted, nil
}
