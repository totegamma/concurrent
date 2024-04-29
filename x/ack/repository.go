package ack

import (
	"context"

	"gorm.io/gorm"

	"github.com/totegamma/concurrent/core"
)

// Repository is the interface for host repository
type Repository interface {
	Ack(ctx context.Context, ack *core.Ack) error
	Unack(ctx context.Context, ack *core.Ack) error
	GetAcker(ctx context.Context, key string) ([]core.Ack, error)
	GetAcking(ctx context.Context, key string) ([]core.Ack, error)
}

type repository struct {
	db *gorm.DB
}

// NewRepository creates a new host repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db}
}

// Ack creates a new ack
func (r *repository) Ack(ctx context.Context, ack *core.Ack) error {
	ctx, span := tracer.Start(ctx, "Ack.Repository.Ack")
	defer span.End()

	ack.Valid = true

	return r.db.WithContext(ctx).Save(&ack).Error
}

// Unack deletes a ack
func (r *repository) Unack(ctx context.Context, ack *core.Ack) error {
	ctx, span := tracer.Start(ctx, "Ack.Repository.Unack")
	defer span.End()

	ack.Valid = false

	return r.db.WithContext(ctx).Save(&ack).Error
}

// GetAcker returns all acks for a entity
func (r *repository) GetAcker(ctx context.Context, key string) ([]core.Ack, error) {
	ctx, span := tracer.Start(ctx, "Ack.Repository.GetAcker")
	defer span.End()

	var acks []core.Ack
	err := r.db.WithContext(ctx).Where("valid = true and \"to\" = ?", key).Find(&acks).Error
	return acks, err
}

// GetAcking returns all acks for a entity
func (r *repository) GetAcking(ctx context.Context, key string) ([]core.Ack, error) {
	ctx, span := tracer.Start(ctx, "Ack.Repository.GetAcking")
	defer span.End()

	var acks []core.Ack
	err := r.db.WithContext(ctx).Where("valid = true and \"from\" = ?", key).Find(&acks).Error
	return acks, err
}
