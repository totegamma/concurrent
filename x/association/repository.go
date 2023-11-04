package association

import (
	"context"
	"fmt"
	"github.com/totegamma/concurrent/x/core"
	"gorm.io/gorm"
)

// Repository is the interface for association repository
type Repository interface {
	Create(ctx context.Context, association core.Association) (core.Association, error)
	Get(ctx context.Context, id string) (core.Association, error)
	GetOwn(ctx context.Context, author string) ([]core.Association, error)
	Delete(ctx context.Context, id string) (core.Association, error)
	GetByTarget(ctx context.Context, targetID string) ([]core.Association, error)
	GetCountsBySchema(ctx context.Context, messageID string) (map[string]int64, error)
	GetBySchema(ctx context.Context, messageID string, schema string) ([]core.Association, error)
	GetCountsBySchemaAndVariant(ctx context.Context, messageID string, schema string) (map[string]int64, error)
	GetBySchemaAndVariant(ctx context.Context, messageID string, schema string, variant string) ([]core.Association, error)
	GetOwnByTarget(ctx context.Context, targetID, author string) ([]core.Association, error)
}

type repository struct {
	db *gorm.DB
}

// NewRepository creates a new association repository
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// Create creates new association
func (r *repository) Create(ctx context.Context, association core.Association) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryCreate")
	defer span.End()

	err := r.db.WithContext(ctx).Create(&association).Error

	return association, err
}

// Get returns a Association by ID
func (r *repository) Get(ctx context.Context, id string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGet")
	defer span.End()

	var association core.Association
	err := r.db.WithContext(ctx).Where("id = $1", id).First(&association).Error
	return association, err
}

// GetOwn returns all associations which owned by specified owner
func (r *repository) GetOwn(ctx context.Context, author string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetOwn")
	defer span.End()

	var associations []core.Association
	err := r.db.WithContext(ctx).Where("author = $1", author).Error
	return associations, err
}

// Delete deletes a association by ID
func (r *repository) Delete(ctx context.Context, id string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryDelete")
	defer span.End()

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

// GetByTarget returns all associations which target is specified message
func (r *repository) GetByTarget(ctx context.Context, targetID string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetByTarget")
	defer span.End()

	var associations []core.Association
	err := r.db.WithContext(ctx).Where("target_id = ?", targetID).Find(&associations).Error
	return associations, err
}

// GetCountsBySchema returns the number of associations for a given schema
func (r *repository) GetCountsBySchema(ctx context.Context, messageID string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetCountsBySchema")
	defer span.End()

	var counts []struct {
		Schema string
		Count  int64
	}

	err := r.db.WithContext(ctx).Model(&core.Association{}).Select("schema, count(*) as count").Where("target_id = ?", messageID).Group("schema").Scan(&counts).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]int64)
	for _, count := range counts {
		result[count.Schema] = count.Count
	}
	
	return result, nil
}

// GetOwnByTarget returns all associations which target is specified message and owned by specified owner
func (r *repository) GetOwnByTarget(ctx context.Context, targetID, author string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetOwnByTarget")
	defer span.End()

	var associations []core.Association
	err := r.db.WithContext(ctx).Where("target_id = ? AND author = ?", targetID, author).Find(&associations).Error
	return associations, err
}

// GetBySchema returns the associations for a given schema
func (r *repository) GetBySchema(ctx context.Context, messageID, schema string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetBySchema")
	defer span.End()

	var associations []core.Association
	err := r.db.WithContext(ctx).Where("target_id = ? AND schema = ?", messageID, schema).Find(&associations).Error
	return associations, err
}

// GetCountsBySchemaAndVariant returns the number of associations for a given schema and variant
func (r *repository) GetCountsBySchemaAndVariant(ctx context.Context, messageID, schema string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetCountsBySchemaAndVariant")
	defer span.End()

	var counts []struct {
		Variant string
		Count  int64
	}

	err := r.db.WithContext(ctx).Model(&core.Association{}).Select("variant, count(*) as count").Where("target_id = ? AND schema = ?", messageID, schema).Group("variant").Scan(&counts).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]int64)
	for _, count := range counts {
		result[count.Variant] = count.Count
	}

	return result, nil
}

// GetBySchemaAndVariant returns the associations for a given schema and variant
func (r *repository) GetBySchemaAndVariant(ctx context.Context, messageID, schema, variant string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "RepositoryGetBySchemaAndVariant")
	defer span.End()

	var associations []core.Association

	err := r.db.WithContext(ctx).Where("target_id = ? AND schema = ? AND variant = ?", messageID, schema, variant).Find(&associations).Error
	if err != nil {
		return nil, err
	}

	return associations, nil
}

