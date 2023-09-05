package message

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

// Repository is message repository
type Repository struct {
	db *gorm.DB
}

// NewRepository is used for wire.go
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Create creates new message
func (r *Repository) Create(ctx context.Context, message *core.Message) (string, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreate")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&message).Error
	return message.ID, err
}

// Get returns a message with associaiton data
func (r *Repository) Get(ctx context.Context, key string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var message core.Message
	err := r.db.WithContext(ctx).Preload("Associations").First(&message, "id = ?", key).Error
	return message, err
}

// Delete deletes an message
func (r *Repository) Delete(ctx context.Context, id string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "RepositoryDelete")
	defer span.End()

	var deleted core.Message
	err := r.db.WithContext(ctx).Where("id = $1", id).Delete(&deleted).Error
	return deleted, err
}
