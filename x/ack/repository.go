package ack

//go:generate go run go.uber.org/mock/mockgen -source=repository.go -destination=mock/repository.go

import (
	"context"

	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/concrnt/concrnt/core"
)

// Repository is the interface for host repository
type Repository interface {
	Ack(ctx context.Context, ack *core.Ack) (core.Ack, error)
	Unack(ctx context.Context, ack *core.Ack) (core.Ack, error)
	Get(ctx context.Context, from, to string) (core.Ack, error)
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
func (r *repository) Ack(ctx context.Context, ack *core.Ack) (core.Ack, error) {
	ctx, span := tracer.Start(ctx, "Ack.Repository.Ack")
	defer span.End()

	ack.Valid = true
	err := r.db.WithContext(ctx).Save(&ack).Error

	return *ack, err
}

// Unack deletes a ack
func (r *repository) Unack(ctx context.Context, ack *core.Ack) (core.Ack, error) {
	ctx, span := tracer.Start(ctx, "Ack.Repository.Unack")
	defer span.End()

	ack.Valid = false
	err := r.db.WithContext(ctx).Save(&ack).Error

	return *ack, err
}

// Get returns a ack
func (r *repository) Get(ctx context.Context, from, to string) (core.Ack, error) {
	ctx, span := tracer.Start(ctx, "Ack.Repository.Get")
	defer span.End()
	var ack core.Ack
	err := r.db.WithContext(ctx).Where("valid = true and \"from\" = ? and \"to\" = ?", from, to).First(&ack).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return core.Ack{}, core.NewErrorNotFound()
		}
		span.RecordError(err)
		return core.Ack{}, err
	}

	return ack, nil
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
