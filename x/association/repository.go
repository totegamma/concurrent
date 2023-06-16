package association

import (
    "fmt"
    "context"
    "gorm.io/gorm"
    "github.com/totegamma/concurrent/x/core"
)


// Repository is association repository
type Repository struct {
    db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

// Create creates new association
func (r *Repository) Create(ctx context.Context, association *core.Association) error {
    ctx, childSpan := tracer.Start(ctx, "RepositoryCreate")
    defer childSpan.End()

    return r.db.WithContext(ctx).Create(&association).Error
}

// Get returns a Association by ID
func (r *Repository) Get(ctx context.Context, id string) (core.Association, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGet")
    defer childSpan.End()

    var association core.Association
    err := r.db.WithContext(ctx).Where("id = $1", id).First(&association).Error
    return association, err
}

// GetOwn returns all associations which owned by specified owner
func (r *Repository) GetOwn(ctx context.Context, author string) ([]core.Association, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryGetOwn")
    defer childSpan.End()

    var associations []core.Association
    err := r.db.WithContext(ctx).Where("author = $1", author).Error
    return associations, err
}

// Delete deletes a association by ID
func (r *Repository) Delete(ctx context.Context, id string) (core.Association, error) {
    ctx, childSpan := tracer.Start(ctx, "RepositoryDelete")
    defer childSpan.End()

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

