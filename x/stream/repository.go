package stream

import (
    "time"
	"context"
	"github.com/totegamma/concurrent/x/core"
	"golang.org/x/exp/slices"
	"gorm.io/gorm"
)

// Repository is stream repository interface
type Repository interface {
    GetStream(ctx context.Context, key string) (core.Stream, error)
    CreateStream(ctx context.Context, stream core.Stream) (core.Stream, error)
    UpdateStream(ctx context.Context, stream core.Stream) (core.Stream, error)
    DeleteStream(ctx context.Context, key string) error

    GetItem(ctx context.Context, streamID string, objectID string) (core.StreamItem, error)
    CreateItem(ctx context.Context, item core.StreamItem) (core.StreamItem, error)
    DeleteItem(ctx context.Context, streamID string, objectID string) error

    ListStreamBySchema(ctx context.Context, schema string) ([]core.Stream, error)
    ListStreamByAuthor(ctx context.Context, author string) ([]core.Stream, error)
    HasWriteAccess(ctx context.Context, key string, author string) bool
    HasReadAccess(ctx context.Context, key string, author string) bool

    RangeStream(ctx context.Context, streamID string, start time.Time, end time.Time, limit int) ([]core.StreamItem, error)
}


type repository struct {
	db *gorm.DB
}

// NewRepository creates a new stream repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// GetItem returns a stream item by StreamID and ObjectID
func (r *repository) GetItem(ctx context.Context, streamID string, objectID string) (core.StreamItem, error) {
    ctx, span := tracer.Start(ctx, "RepositoryGetItem")
    defer span.End()

    var item core.StreamItem
    err := r.db.WithContext(ctx).First(&item, "stream_id = ? and object_id = ?", streamID, objectID).Error
    return item, err
}

// CreateItem creates a new stream item
func (r *repository) CreateItem(ctx context.Context, item core.StreamItem) (core.StreamItem, error) {
    ctx, span := tracer.Start(ctx, "RepositoryCreateItem")
    defer span.End()

    err := r.db.WithContext(ctx).Create(&item).Error
    return item, err
}

// DeleteItem deletes a stream item
func (r *repository) DeleteItem(ctx context.Context, streamID string, objectID string) error {
    ctx, span := tracer.Start(ctx, "RepositoryDeleteItem")
    defer span.End()

    return r.db.WithContext(ctx).Delete(&core.StreamItem{}, "stream_id = ? and object_id = ?", streamID, objectID).Error
}

// RangeStream returns a list of stream items by StreamID and time range
func (r *repository) RangeStream(ctx context.Context, streamID string, start time.Time, end time.Time, limit int) ([]core.StreamItem, error) {
    ctx, span := tracer.Start(ctx, "RepositoryRangeStream")
    defer span.End()

    var items []core.StreamItem
    err := r.db.WithContext(ctx).Where("stream_id = ? and created_at >= ? and created_at <= ?", streamID, start, end).Limit(limit).Find(&items).Error
    return items, err
}

// GetStream returns a stream by ID
func (r *repository) GetStream(ctx context.Context, key string) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetStream")
	defer span.End()

	var stream core.Stream
	err := r.db.WithContext(ctx).First(&stream, "id = ?", key).Error
	return stream, err
}

// Create updates a stream
func (r *repository) CreateStream(ctx context.Context, stream core.Stream) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreateStream")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&stream).Error
	return stream, err
}

// Update updates a stream
func (r *repository) UpdateStream(ctx context.Context, stream core.Stream) (core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryUpdateStream")
	defer span.End()

	var obj core.Stream
	err := r.db.WithContext(ctx).First(&obj, "id = ?", stream.ID).Error
	if err != nil {
		return core.Stream{}, err
	}
	err = r.db.WithContext(ctx).Model(&obj).Updates(stream).Error
	return stream, err
}

// GetListBySchema returns list of schemas by schema
func (r *repository) ListStreamBySchema(ctx context.Context, schema string) ([]core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryListStream")
	defer span.End()

	var streams []core.Stream
	err := r.db.WithContext(ctx).Where("Schema = ? and visible = true", schema).Find(&streams).Error
	return streams, err
}

// GetListByAuthor returns list of schemas by owner
func (r *repository) ListStreamByAuthor(ctx context.Context, author string) ([]core.Stream, error) {
	ctx, span := tracer.Start(ctx, "RepositoryListStream")
	defer span.End()

	var streams []core.Stream
	err := r.db.WithContext(ctx).Where("Author = ?", author).Find(&streams).Error
	return streams, err
}

// Delete deletes a stream
func (r *repository) DeleteStream(ctx context.Context, streamID string) error {
	ctx, span := tracer.Start(ctx, "RepositoryDeleteStream")
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
