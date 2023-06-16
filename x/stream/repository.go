package stream

import (
    "context"
    "gorm.io/gorm"
    "golang.org/x/exp/slices"
    "github.com/totegamma/concurrent/x/core"
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
    ctx, childSpan := tracer.Start(ctx, "RepositoryGet")
    defer childSpan.End()

    var stream core.Stream
    err := r.db.WithContext(ctx).First(&stream, "id = ?", key).Error
    return stream, err
}

// Upsert updates a stream
func (r *Repository) Upsert(ctx context.Context, stream *core.Stream) error {
    ctx, childSpan := tracer.Start(ctx, "RepositoryUpsert")
    defer childSpan.End()

    return r.db.WithContext(ctx).Save(&stream).Error
}

// GetList returns list of schemas by schema
func (r *Repository) GetList(ctx context.Context, schema string) ([]core.Stream, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGetList")
    defer childSpan.End()

    var streams []core.Stream
    err := r.db.WithContext(ctx).Where("Schema = ?", schema).Find(&streams).Error
    return streams, err
}


// HasWriteAccess returns true if the user has write access
func (r *Repository) HasWriteAccess(ctx context.Context, streamID string, userAddress string) bool {
    ctx, childSpan := tracer.Start(ctx, "RepositoryHasWriteAccess")
    defer childSpan.End()

    var stream core.Stream
    r.db.WithContext(ctx).First(&stream, "id = ?", streamID)
    if len(stream.Writer) == 0 {
        return true
    }
    return slices.Contains(stream.Writer, userAddress)
}

// HasReadAccess returns true if the user has read access
func (r *Repository) HasReadAccess(ctx context.Context, streamID string, userAddress string) bool {
    ctx, childSpan := tracer.Start(ctx, "RepositoryHasReadAccess")
    defer childSpan.End()

    var stream core.Stream
    r.db.WithContext(ctx).First(&stream, "id = ?", streamID)
    if len(stream.Reader) == 0 {
        return true
    }
    return slices.Contains(stream.Reader, userAddress)
}


