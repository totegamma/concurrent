package message

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

// Repository is the interface for message repository
type Repository interface {
    Create(ctx context.Context, message *core.Message) (string, error)
    Get(ctx context.Context, key string) (core.Message, error)
    Delete(ctx context.Context, key string) (core.Message, error)
}

type repository struct {
	db *gorm.DB
}

// NewRepository creates a new message repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// Create creates new message
func (r *repository) Create(ctx context.Context, message *core.Message) (string, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreate")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&message).Error
	return message.ID, err
}

// Get returns a message by ID
func (r *repository) Get(ctx context.Context, key string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var message core.Message
	err := r.db.WithContext(ctx).Preload("Associations").First(&message, "id = ?", key).Error
	return message, err
}

// Delete deletes an message
func (r *repository) Delete(ctx context.Context, id string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "RepositoryDelete")
	defer span.End()

	var deleted core.Message
	err := r.db.WithContext(ctx).Where("id = $1", id).Delete(&deleted).Error
	return deleted, err
}
