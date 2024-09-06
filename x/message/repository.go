package message

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/core"
)

// Repository is the interface for message repository
type Repository interface {
	Create(ctx context.Context, message core.Message) (core.Message, error)
	Get(ctx context.Context, key string) (core.Message, error)
	GetWithOwnAssociations(ctx context.Context, key string, ccid string) (core.Message, error)
	Delete(ctx context.Context, key string) error
	Clean(ctx context.Context, ccid string) error
	Count(ctx context.Context) (int64, error)
}

type repository struct {
	db     *gorm.DB
	mc     *memcache.Client
	schema core.SchemaService
}

// NewRepository creates a new message repository
func NewRepository(db *gorm.DB, mc *memcache.Client, schema core.SchemaService) Repository {
	return &repository{db, mc, schema}
}

func (r *repository) setCurrentCount() {
	var count int64
	err := r.db.Model(&core.Message{}).Count(&count).Error
	if err != nil {
		slog.Error(
			"failed to count messages",
			slog.String("error", err.Error()),
		)
	}

	r.mc.Set(&memcache.Item{Key: "message_count", Value: []byte(strconv.FormatInt(count, 10))})
}

func (r *repository) normalizeDBID(id string) (string, error) {

	normalized := id

	if len(normalized) == 27 {
		if normalized[0] != 'm' {
			return "", errors.New("message id must start with 'm'. got " + normalized)
		}
		normalized = normalized[1:]
	}

	if len(normalized) != 26 {
		return "", errors.New("message id must be 26 characters long. got " + normalized)
	}

	return normalized, nil
}

func (r *repository) preProcess(ctx context.Context, message *core.Message) error {

	var err error
	message.ID, err = r.normalizeDBID(message.ID)
	if err != nil {
		return err
	}

	if message.SchemaID == 0 {
		schemaID, err := r.schema.UrlToID(ctx, message.Schema)
		if err != nil {
			return err
		}
		message.SchemaID = schemaID
	}

	if message.PolicyID == 0 && message.Policy != "" {
		policyID, err := r.schema.UrlToID(ctx, message.Policy)
		if err != nil {
			return err
		}
		message.PolicyID = policyID
	}

	return nil
}

func (r *repository) postProcess(ctx context.Context, message *core.Message) error {

	if len(message.ID) == 26 {
		message.ID = "m" + message.ID
	}

	if message.SchemaID != 0 && message.Schema == "" {
		schemaUrl, err := r.schema.IDToUrl(ctx, message.SchemaID)
		if err != nil {
			return err
		}
		message.Schema = schemaUrl
	}

	if message.PolicyID != 0 && message.Policy == "" {
		policyUrl, err := r.schema.IDToUrl(ctx, message.PolicyID)
		if err != nil {
			return err
		}
		message.Policy = policyUrl
	}

	return nil
}

// Total returns the total number of messages
func (r *repository) Count(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "Message.Repository.Count")
	defer span.End()

	item, err := r.mc.Get("message_count")
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

// Create creates new message
func (r *repository) Create(ctx context.Context, message core.Message) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Repository.Create")
	defer span.End()

	err := r.preProcess(ctx, &message)
	if err != nil {
		return core.Message{}, err
	}

	err = r.db.WithContext(ctx).Create(&message).Error
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return core.Message{}, core.NewErrorAlreadyExists()
		}
		return core.Message{}, err
	}

	err = r.postProcess(ctx, &message)
	if err != nil {
		return core.Message{}, err
	}

	r.mc.Increment("message_count", 1)
	return message, err
}

// Get returns a message by ID
func (r *repository) Get(ctx context.Context, id string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Repository.Get")
	defer span.End()

	id, err := r.normalizeDBID(id)
	if err != nil {
		return core.Message{}, err
	}

	var message core.Message
	err = r.db.WithContext(ctx).First(&message, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return core.Message{}, core.NewErrorNotFound()
		}
		return message, err
	}

	err = r.postProcess(ctx, &message)
	if err != nil {
		return core.Message{}, err
	}

	return message, err
}

// GetWithOwnAssociations returns a message by ID with associations
func (r *repository) GetWithOwnAssociations(ctx context.Context, id string, ccid string) (core.Message, error) {
	ctx, span := tracer.Start(ctx, "Message.Repository.GetWithOwnAssociations")
	defer span.End()

	id, err := r.normalizeDBID(id)

	var message core.Message
	err = r.db.WithContext(ctx).First(&message, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return core.Message{}, core.NewErrorNotFound()
		}
		return message, err
	}

	err = r.postProcess(ctx, &message)
	if err != nil {
		return core.Message{}, err
	}

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
func (r *repository) Delete(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "Message.Repository.Delete")
	defer span.End()

	id, err := r.normalizeDBID(id)

	var deleted core.Message
	err = r.db.WithContext(ctx).Where("id = $1", id).Delete(&deleted).Error
	if err != nil {
		return err
	}

	r.mc.Decrement("message_count", 1)

	return nil
}

func (r *repository) Clean(ctx context.Context, ccid string) error {
	ctx, span := tracer.Start(ctx, "Message.Repository.Clean")
	defer span.End()

	err := r.db.WithContext(ctx).Where("author = ?", ccid).Delete(&core.Message{}).Error
	if err != nil {
		return err
	}

	return nil
}
