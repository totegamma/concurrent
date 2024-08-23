package schema

import (
	"context"
	"encoding/json"
	"github.com/totegamma/concurrent/core"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"net/http"
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

			client := new(http.Client)
			req, err := http.NewRequest("GET", schema, nil)
			if err != nil {
				return core.Schema{}, err
			}
			req.Header.Set("Accept", "application/json")
			res, err := client.Do(req)
			if err != nil {
				return core.Schema{}, err
			}
			defer res.Body.Close()

			var _schema any
			err = json.NewDecoder(res.Body).Decode(&_schema)
			if err != nil {
				return core.Schema{}, err
			}

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
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return core.Schema{}, core.NewErrorNotFound()
		}
		span.RecordError(err)
		return core.Schema{}, err
	}
	return s, err
}
