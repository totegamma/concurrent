package semanticid

import (
	"context"
	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("semanticid")

type Repository interface {
	Upsert(ctx context.Context, item core.SemanticID) (core.SemanticID, error)
	Get(ctx context.Context, id, owner string) (core.SemanticID, error)
	Delete(ctx context.Context, id, owner string) error
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db}
}

func (r *repository) Upsert(ctx context.Context, item core.SemanticID) (core.SemanticID, error) {
	ctx, span := tracer.Start(ctx, "SemanticID.Repository.Upsert")
	defer span.End()

	if err := r.db.WithContext(ctx).Save(&item).Error; err != nil {
		return core.SemanticID{}, err
	}

	return item, nil
}

func (r *repository) Get(ctx context.Context, id, owner string) (core.SemanticID, error) {
	ctx, span := tracer.Start(ctx, "SemanticID.Repository.Get")
	defer span.End()

	var item core.SemanticID
	if err := r.db.WithContext(ctx).Where("id = ? AND owner = ?", id, owner).First(&item).Error; err != nil {
		return core.SemanticID{}, err
	}

	return item, nil
}

func (r *repository) Delete(ctx context.Context, id, owner string) error {
	ctx, span := tracer.Start(ctx, "SemanticID.Repository.Delete")
	defer span.End()

	if err := r.db.Where("id = ? AND owner = ?", id, owner).Delete(&core.SemanticID{}).Error; err != nil {
		return err
	}

	return nil
}
