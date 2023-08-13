package stream

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"golang.org/x/exp/slices"
	"gorm.io/gorm"
)

// Repository is stream repository
type Repository struct {
	db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Get returns a stream by ID
func (r *Repository) Get(ctx context.Context, key string) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var stream core.Stream
	err := r.db.WithContext(ctx).First(&stream, "id = ?", key).Error
	return stream, err
}

// Upsert updates a stream
func (r *Repository) Upsert(ctx context.Context, stream *core.Stream) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpsert")
	defer span.End()

	return r.db.WithContext(ctx).Save(&stream).Error
}

// GetList returns list of schemas by schema
func (r *Repository) GetList(ctx context.Context, schema string) ([]core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetList")
	defer span.End()

	var streams []core.Stream
	err := r.db.WithContext(ctx).Where("Schema = ?", schema).Find(&streams).Error
	return streams, err
}

// Delete deletes a stream
func (r *Repository) Delete(ctx context.Context, streamID string) error {
	ctx, span := tracer.Start(ctx, "RepositoryDelete")
	defer span.End()

	return r.db.WithContext(ctx).Delete(&core.Stream{}, "id = ?", streamID).Error
}

// HasWriteAccess returns true if the user has write access
func (r *Repository) HasWriteAccess(ctx context.Context, streamID string, userAddress string) bool {
	ctx, span := tracer.Start(ctx, "RepositoryHasWriteAccess")
	defer span.End()

	var stream core.Stream
	r.db.WithContext(ctx).First(&stream, "id = ?", streamID)
	if len(stream.Writer) == 0 {
		return true
	}
	return slices.Contains(stream.Writer, userAddress)
}

// HasReadAccess returns true if the user has read access
func (r *Repository) HasReadAccess(ctx context.Context, streamID string, userAddress string) bool {
	ctx, span := tracer.Start(ctx, "RepositoryHasReadAccess")
	defer span.End()

	var stream core.Stream
	r.db.WithContext(ctx).First(&stream, "id = ?", streamID)
	if len(stream.Reader) == 0 {
		return true
	}
	return slices.Contains(stream.Reader, userAddress)
}
