package message

import (
	"context"
	"errors"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/totegamma/concurrent/x/core"
	"github.com/totegamma/concurrent/x/schema"
	"gorm.io/gorm"
	"log/slog"
	"strconv"
)

// Repository is the interface for message repository
type Repository interface {
	Create(ctx context.Context, message core.Message) (core.Message, error)
	Get(ctx context.Context, key string) (core.Message, error)
	GetWithOwnAssociations(ctx context.Context, key string, ccid string) (core.Message, error)
	Delete(ctx context.Context, key string) (core.Message, error)
	Count(ctx context.Context) (int64, error)
}

type repository struct {
	db     *gorm.DB
	mc     *memcache.Client
	schema schema.Service
}

// NewRepository creates a new message repository
func NewRepository(db *gorm.DB, mc *memcache.Client, schema schema.Service) Repository {

	var count int64
	err := db.Model(&core.Message{}).Count(&count).Error
	if err != nil {
		slog.Error(
			"failed to count messages",
			slog.String("error", err.Error()),
		)
	}

	mc.Set(&memcache.Item{Key: "message_count", Value: []byte(strconv.FormatInt(count, 10))})

	return &repository{db, mc, schema}
}

// Total returns the total number of messages
func (r *repository) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "Message.Repository.Count")
	defer span.End()

	item, err := r.mc.Get("message_count")
	if err != nil {
		span.RecordError(err)
		return 0, err
	}

	count, err := strconv.ParseInt(string(item.Value), 10, 64)
	if err != nil {
		span.RecordError(err)
		return 0, err
	}
	return count, nil
}

// Create creates new message
func (r *repository) Create(ctx context.Context, message core.Message) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Repository.Create")
	defer span.End()

	if message.ID == "" {
		return message, errors.New("message id is required")
	}

	if len(message.ID) == 27 {
		if message.ID[0] != 'm' {
			return message, errors.New("message id must start with 'm'")
		}
		message.ID = message.ID[1:]
	}

	if len(message.ID) != 26 {
		return message, errors.New("message id must be 26 characters long")
	}

	schemaID, err := r.schema.UrlToID(ctx, message.Schema)
	if err != nil {
		return message, err
	}
	message.SchemaID = schemaID

	err = r.db.WithContext(ctx).Create(&message).Error

	r.mc.Increment("message_count", 1)

	message.ID = "m" + message.ID

	return message, err
}

// Get returns a message by ID
func (r *repository) Get(ctx context.Context, key string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Repository.Get")
	defer span.End()

	if len(key) == 27 {
		if key[0] != 'm' {
			return core.Message{}, errors.New("message typed-id must start with 'm'")
		}
		key = key[1:]
	}

	var message core.Message
	err := r.db.WithContext(ctx).First(&message, "id = ?", key).Error
	if err != nil {
		return message, err
	}

	schemaUrl, err := r.schema.IDToUrl(ctx, message.SchemaID)
	if err != nil {
		return message, err
	}
	message.Schema = schemaUrl

	message.ID = "m" + message.ID

	return message, err
}

// GetWithOwnAssociations returns a message by ID with associations
func (r *repository) GetWithOwnAssociations(ctx context.Context, key string, ccid string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Repository.GetWithOwnAssociations")
	defer span.End()

	if len(key) == 27 {
		if key[0] != 'm' {
			return core.Message{}, errors.New("message typed-id must start with 'm'")
		}
		key = key[1:]
	}

	var message core.Message
	err := r.db.WithContext(ctx).First(&message, "id = ?", key).Error
	if err != nil {
		return message, err
	}

	schemaUrl, err := r.schema.IDToUrl(ctx, message.SchemaID)
	if err != nil {
		return message, err
	}
	message.Schema = schemaUrl
	message.ID = "m" + message.ID
	r.db.WithContext(ctx).Where("target = ? AND author = ?", message.ID, ccid).Find(&message.OwnAssociations)
	for i := range message.OwnAssociations {
		message.OwnAssociations[i].ID = "a" + message.OwnAssociations[i].ID

		schemaUrl, err := r.schema.IDToUrl(ctx, message.OwnAssociations[i].SchemaID)
		if err != nil {
			continue
		}
		message.OwnAssociations[i].Schema = schemaUrl
	}

	return message, err
}

// Delete deletes an message
func (r *repository) Delete(ctx context.Context, id string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Repository.Delete")
	defer span.End()

	if len(id) == 27 {
		if id[0] != 'm' {
			return core.Message{}, errors.New("message typed-id must start with 'm'")
		}
		id = id[1:]
	}

	var deleted core.Message
	if err := r.db.WithContext(ctx).First(&deleted, "id = ?", id).Error; err != nil {
		return deleted, err
	}
	err := r.db.WithContext(ctx).Where("id = $1", id).Delete(&deleted).Error
	if err != nil {
		return deleted, err
	}

	r.mc.Decrement("message_count", 1)

	deleted.ID = "m" + deleted.ID

	return deleted, nil
}
