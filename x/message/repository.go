package message

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

// Repository is the interface for message repository
type Repository interface {
	Create(ctx context.Context, message core.Message) (core.Message, error)
	Get(ctx context.Context, key string) (core.Message, error)
	GetWithAssociations(ctx context.Context, key string) (core.Message, error)
	GetWithOwnAssociations(ctx context.Context, key string, ccid string) (core.Message, error)
	Delete(ctx context.Context, key string) (core.Message, error)
	Total(ctx context.Context) (int64, error)
}

type repository struct {
	db *gorm.DB
}

// Total returns the total number of messages
func (r *repository) Total(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "RepositoryTotal")
	defer span.End()

	var count int64
	err := r.db.WithContext(ctx).Model(&core.Message{}).Count(&count).Error
	return count, err
}

// NewRepository creates a new message repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// Create creates new message
func (r *repository) Create(ctx context.Context, message core.Message) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreate")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&message).Error
	return message, err
}

// Get returns a message by ID
func (r *repository) Get(ctx context.Context, key string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var message core.Message
	err := r.db.WithContext(ctx).First(&message, "id = ?", key).Error
	return message, err
}

// GetWithOwnAssociations returns a message by ID with associations
func (r *repository) GetWithOwnAssociations(ctx context.Context, key string, ccid string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var message core.Message
	err := r.db.WithContext(ctx).First(&message, "id = ?", key).Error
	if err != nil {
		return message, err
	}

	r.db.WithContext(ctx).Where("target_id = ? AND author = ?", key, ccid).Find(&message.OwnAssociations)

	return message, err
}

// GetWithAssociations returns a message by ID with associations
func (r *repository) GetWithAssociations(ctx context.Context, key string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetWithAssociations")
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
	if err := r.db.WithContext(ctx).First(&deleted, "id = ?", id).Error; err != nil {
		return deleted, err
	}
	err := r.db.WithContext(ctx).Where("id = $1", id).Delete(&deleted).Error
	if err != nil {
		return deleted, err
	}

	return deleted, nil
}
