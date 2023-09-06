package stream

import (
	"context"
	"github.com/totegamma/concurrent/x/core"
	"golang.org/x/exp/slices"
	"gorm.io/gorm"
)

// Repository is stream repository interface
type Repository interface {
    Get(ctx context.Context, key string) (core.Stream, error)
    Upsert(ctx context.Context, stream *core.Stream) error
    GetListBySchema(ctx context.Context, schema string) ([]core.Stream, error)
    GetListByAuthor(ctx context.Context, author string) ([]core.Stream, error)
    Delete(ctx context.Context, key string) error
    HasWriteAccess(ctx context.Context, key string, author string) bool
    HasReadAccess(ctx context.Context, key string, author string) bool
}


type repository struct {
	db *gorm.DB
}

// NewRepository creates a new stream repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// Get returns a stream by ID
func (r *repository) Get(ctx context.Context, key string) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var stream core.Stream
	err := r.db.WithContext(ctx).First(&stream, "id = ?", key).Error
	return stream, err
}

// Upsert updates a stream
func (r *repository) Upsert(ctx context.Context, stream *core.Stream) error {
	ctx, span := tracer.Start(ctx, "RepositoryUpsert")
	defer span.End()

	return r.db.WithContext(ctx).Save(&stream).Error
}

// GetListBySchema returns list of schemas by schema
func (r *repository) GetListBySchema(ctx context.Context, schema string) ([]core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetList")
	defer span.End()

	var streams []core.Stream
	err := r.db.WithContext(ctx).Where("Schema = ? and visible = true", schema).Find(&streams).Error
	return streams, err
}

// GetListByAuthor returns list of schemas by owner
func (r *repository) GetListByAuthor(ctx context.Context, author string) ([]core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetList")
	defer span.End()

	var streams []core.Stream
	err := r.db.WithContext(ctx).Where("Author = ?", author).Find(&streams).Error
	return streams, err
}

// Delete deletes a stream
func (r *repository) Delete(ctx context.Context, streamID string) error {
	ctx, span := tracer.Start(ctx, "RepositoryDelete")
	defer span.End()

	return r.db.WithContext(ctx).Delete(&core.Stream{}, "id = ?", streamID).Error
}

// HasWriteAccess returns true if the user has write access
func (r *repository) HasWriteAccess(ctx context.Context, streamID string, userAddress string) bool {
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
func (r *repository) HasReadAccess(ctx context.Context, streamID string, userAddress string) bool {
	ctx, span := tracer.Start(ctx, "RepositoryHasReadAccess")
	defer span.End()

	var stream core.Stream
	r.db.WithContext(ctx).First(&stream, "id = ?", streamID)
	if len(stream.Reader) == 0 {
		return true
	}
	return slices.Contains(stream.Reader, userAddress)
}
