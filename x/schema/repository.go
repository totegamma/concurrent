package schema

import (
	"context"
	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("schema")

type Repository interface {
	Upsert(ctx context.Context, schema string) (core.Schema, error)
	Get(ctx context.Context, id uint) (core.Schema, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{
		db: db,
	}
}

func (r *repository) Upsert(ctx context.Context, schema string) (core.Schema, error) {
	ctx, span := tracer.Start(ctx, "Schema.Repository.Upsert")
	defer span.End()

	var s core.Schema
	err := r.db.WithContext(ctx).Where("url = ?", schema).First(&s).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			s = core.Schema{
				URL: schema,
			}
			err = r.db.WithContext(ctx).Create(&s).Error
			return s, err
		} else {
			return s, err
		}
	}
	return s, nil
}

func (r *repository) Get(ctx context.Context, id uint) (core.Schema, error) {
	ctx, span := tracer.Start(ctx, "Schema.Repository.Get")
	defer span.End()

	var s core.Schema
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&s).Error
	return s, err
}
