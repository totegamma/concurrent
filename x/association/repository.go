package association

import (
	"context"
	"gorm.io/gorm"
	"log/slog"
	"strconv"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/pkg/errors"
	"github.com/totegamma/concurrent/core"
)

// Repository is the interface for association repository
type Repository interface {
	Create(ctx context.Context, association core.Association) (core.Association, error)
	Get(ctx context.Context, id string) (core.Association, error)
	GetOwn(ctx context.Context, author string) ([]core.Association, error)
	Delete(ctx context.Context, id string) error
	GetByTarget(ctx context.Context, targetID string) ([]core.Association, error)
	GetCountsBySchema(ctx context.Context, messageID string) (map[string]int64, error)
	GetBySchema(ctx context.Context, messageID string, schema string) ([]core.Association, error)
	GetCountsBySchemaAndVariant(ctx context.Context, messageID string, schema string) (map[string]int64, error)
	GetBySchemaAndVariant(ctx context.Context, messageID string, schema string, variant string) ([]core.Association, error)
	GetOwnByTarget(ctx context.Context, targetID, author string) ([]core.Association, error)
	Count(ctx context.Context) (int64, error)
	Clean(ctx context.Context, ccid string) error
}

type repository struct {
	db     *gorm.DB
	mc     *memcache.Client
	schema core.SchemaService
}

// NewRepository creates a new association repository
func NewRepository(db *gorm.DB, mc *memcache.Client, schema core.SchemaService) Repository {
	return &repository{db, mc, schema}
}

func (r *repository) setCurrentCount() {
	var count int64
	err := r.db.Model(&core.Association{}).Count(&count).Error
	if err != nil {
		slog.Error(
			"failed to count associations",
			slog.String("error", err.Error()),
		)
	}
	r.mc.Set(&memcache.Item{Key: "association_count", Value: []byte(strconv.FormatInt(count, 10))})
}

// Total returns the total number of associations
func (r *repository) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "Association.Repository.Count")
	defer span.End()

	item, err := r.mc.Get("association_count")
	if err != nil {
		span.RecordError(err)
		if errors.Is(err, memcache.ErrCacheMiss) {
			r.setCurrentCount()
			return 0, errors.Wrap(err, "trying to fix...")
		}
		return 0, err
	}

	count, err := strconv.ParseInt(string(item.Value), 10, 64)
	if err != nil {
		span.RecordError(err)
		return 0, err
	}
	return count, nil
}

// Create creates new association
func (r *repository) Create(ctx context.Context, association core.Association) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Repository.Create")
	defer span.End()

	if association.ID == "" {
		return association, errors.New("association ID is required")
	}

	if len(association.ID) == 27 {
		if association.ID[0] != 'a' {
			return association, errors.New("association ID must start with 'a'. got " + association.ID)
		}
		association.ID = association.ID[1:]
	}

	if len(association.ID) != 26 {
		return association, errors.New("association ID must be 26 characters long. got " + association.ID)
	}

	schemaID, err := r.schema.UrlToID(ctx, association.Schema)
	if err != nil {
		return association, err
	}
	association.SchemaID = schemaID

	err = r.db.WithContext(ctx).Create(&association).Error
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return association, core.NewErrorAlreadyExists()
		}
		span.RecordError(err)
		return association, err
	}

	r.mc.Increment("association_count", 1)

	association.ID = "a" + association.ID

	return association, nil
}

// Get returns a Association by ID
func (r *repository) Get(ctx context.Context, id string) (core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Repository.Get")
	defer span.End()

	if len(id) == 27 {
		if id[0] != 'a' {
			return core.Association{}, errors.New("association typed-id must start with 'a'. got " + id)
		}
		id = id[1:]
	}

	if len(id) != 26 {
		return core.Association{}, errors.New("association ID must be 26 characters long. got " + id)
	}

	var association core.Association
	err := r.db.WithContext(ctx).Where("id = $1", id).First(&association).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return core.Association{}, core.NewErrorNotFound()
		}
	}

	schemaUrl, err := r.schema.IDToUrl(ctx, association.SchemaID)
	if err != nil {
		return association, err
	}
	association.Schema = schemaUrl

	association.ID = "a" + association.ID

	return association, err
}

// GetOwn returns all associations which owned by specified owner
func (r *repository) GetOwn(ctx context.Context, author string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Repository.GetOwn")
	defer span.End()

	var associations []core.Association
	err := r.db.WithContext(ctx).Where("author = $1", author).Error
	if err != nil {
		return nil, err
	}

	for i := range associations {
		schemaUrl, err := r.schema.IDToUrl(ctx, associations[i].SchemaID)
		if err != nil {
			continue
		}
		associations[i].Schema = schemaUrl
		associations[i].ID = "a" + associations[i].ID
	}

	return associations, err
}

// Delete deletes a association by ID
func (r *repository) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "Association.Repository.Delete")
	defer span.End()

	if len(id) == 27 {
		if id[0] != 'a' {
			return errors.New("association typed-id must start with 'a'. got " + id)
		}
		id = id[1:]
	}

	var deleted core.Association
	err := r.db.WithContext(ctx).Where("id = $1", id).Delete(&core.Association{}).Error
	if err != nil {
		span.RecordError(err)
		return err
	}

	r.mc.Decrement("association_count", 1)

	deleted.ID = "a" + deleted.ID

	return nil
}

// GetByTarget returns all associations which target is specified message
func (r *repository) GetByTarget(ctx context.Context, targetID string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Repository.GetByTarget")
	defer span.End()

	var associations []core.Association
	err := r.db.WithContext(ctx).Where("target = ?", targetID).Find(&associations).Error
	if err != nil {
		return nil, err
	}

	for i := range associations {
		schemaUrl, err := r.schema.IDToUrl(ctx, associations[i].SchemaID)
		if err != nil {
			continue
		}
		associations[i].Schema = schemaUrl
		associations[i].ID = "a" + associations[i].ID
	}

	return associations, err
}

// GetCountsBySchema returns the number of associations for a given schema
func (r *repository) GetCountsBySchema(ctx context.Context, messageID string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Association.Repository.GetCountsBySchema")
	defer span.End()

	var counts []struct {
		SchemaID uint
		Count    int64
	}

	err := r.db.WithContext(ctx).Model(&core.Association{}).Select("schema_id, count(*) as count").Where("target = ?", messageID).Group("schema_id").Scan(&counts).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]int64)
	for _, count := range counts {
		schemaUrl, err := r.schema.IDToUrl(ctx, count.SchemaID)
		if err != nil {
			continue
		}
		result[schemaUrl] = count.Count
	}

	return result, nil
}

// GetOwnByTarget returns all associations which target is specified message and owned by specified owner
func (r *repository) GetOwnByTarget(ctx context.Context, targetID, author string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Repository.GetOwnByTarget")
	defer span.End()

	var associations []core.Association
	err := r.db.WithContext(ctx).Where("target = ? AND author = ?", targetID, author).Find(&associations).Error
	if err != nil {
		return nil, err
	}

	for i := range associations {
		schemaUrl, err := r.schema.IDToUrl(ctx, associations[i].SchemaID)
		if err != nil {
			continue
		}
		associations[i].Schema = schemaUrl
		associations[i].ID = "a" + associations[i].ID
	}

	return associations, err
}

// GetBySchema returns the associations for a given schema
func (r *repository) GetBySchema(ctx context.Context, messageID, schema string) ([]core.Association, error) {
	ctx, span := tracer.Start(ctx, "Association.Repository.GetBySchema")
	defer span.End()

	schemaID, err := r.schema.UrlToID(ctx, schema)
	if err != nil {
		return nil, err
	}

	var associations []core.Association
	err = r.db.WithContext(ctx).Where("target = ? AND schema_id = ?", messageID, schemaID).Find(&associations).Error
	if err != nil {
		return nil, err
	}

	for i := range associations {
		schemaUrl, err := r.schema.IDToUrl(ctx, associations[i].SchemaID)
		if err != nil {
			continue
		}
		associations[i].Schema = schemaUrl
		associations[i].ID = "a" + associations[i].ID
	}

	return associations, err
}

// GetCountsBySchemaAndVariant returns the number of associations for a given schema and variant
func (r *repository) GetCountsBySchemaAndVariant(ctx context.Context, messageID, schema string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Association.Repository.GetCountsBySchemaAndVariant")
	defer span.End()

	var counts []struct {
		Variant string
		Count   int64
	}

	schemaID, err := r.schema.UrlToID(ctx, schema)
	if err != nil {
		return nil, err
	}

	err = r.db.WithContext(ctx).Model(&core.Association{}).Select("variant, count(*) as count").Where("target = ? AND schema_id = ?", messageID, schemaID).Group("variant").Scan(&counts).Error
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
	ctx, span := tracer.Start(ctx, "Association.Repository.GetBySchemaAndVariant")
	defer span.End()

	schemaID, err := r.schema.UrlToID(ctx, schema)
	if err != nil {
		return nil, err
	}

	var associations []core.Association

	err = r.db.WithContext(ctx).Where("target = ? AND schema_id = ? AND variant = ?", messageID, schemaID, variant).Find(&associations).Error
	if err != nil {
		return nil, err
	}

	for i := range associations {
		schemaUrl, err := r.schema.IDToUrl(ctx, associations[i].SchemaID)
		if err != nil {
			continue
		}
		associations[i].Schema = schemaUrl
		associations[i].ID = "a" + associations[i].ID
	}

	return associations, nil
}

func (r *repository) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "Association.Repository.Clean")
	defer span.End()

	err := r.db.WithContext(ctx).Where("owner = ?", ccid).Delete(&core.Association{}).Error
	if err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}
