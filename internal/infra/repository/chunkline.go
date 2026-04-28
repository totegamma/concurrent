package repository

import (
	"context"
	"net/url"
	"time"

	"gorm.io/gorm"

	"github.com/totegamma/concrnt-playground/chunkline"
	"github.com/totegamma/concrnt-playground/internal/infra/database/models"
)

const (
	defaultChunkSize = 32
)

type ChunklineRepository struct {
	db *gorm.DB
}

func NewChunklineRepository(db *gorm.DB) *ChunklineRepository {
	return &ChunklineRepository{db: db}
}

func (r *ChunklineRepository) GetChunklineManifest(ctx context.Context, uri string) (*chunkline.Manifest, error) {
	ctx, span := tracer.Start(ctx, "Repository.Chunkline.GetChunklineManifest")
	defer span.End()

	recordKey, err := GetRecordKeyByURI(ctx, r.db, uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	firstChunk := int64(0)
	firstCollectionMember := models.RecordKey{}
	err = r.db.WithContext(ctx).
		Model(&models.RecordKey{}).
		Joins("JOIN records r ON r.document_id = record_keys.record_id").
		Where("record_keys.parent_id = ?", recordKey.ID).
		Order("r.created_at ASC").
		Limit(1).
		Preload("Record").
		Take(&firstCollectionMember).Error
	if err == nil {
		firstChunk = firstCollectionMember.Record.CreatedAt.Unix() / 600
	}

	safeURI := url.QueryEscape(uri)

	return &chunkline.Manifest{
		Version:    "1.0",
		ChunkSize:  600,
		FirstChunk: &firstChunk,
		Descending: &chunkline.Endpoint{
			Iterator: "/chunkline/itr/{chunk}?uri=" + safeURI,
			Body:     "/chunkline/body/{chunk}?uri=" + safeURI,
		},
		// Metadata: recordKey.Record.Value,
	}, nil
}

func (r *ChunklineRepository) LookupLocalItrs(ctx context.Context, uris []string, chunkID int64) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Repository.Chunkline.LookupLocalItrs")
	defer span.End()

	type TimelineRow struct {
		URI          string    `gorm:"column:uri"`
		MaxCreatedAt time.Time `gorm:"column:max_created_at"`
	}

	var res []TimelineRow

	cutoff := time.Unix((chunkID+1)*600, 0) // descending order

	err := r.db.WithContext(ctx).
		Table("record_keys AS parent").
		Joins("JOIN record_keys AS child ON child.parent_id = parent.id").
		Joins("JOIN records r ON r.document_id = child.record_id").
		Select("parent.uri AS uri, MAX(r.created_at) AS max_created_at").
		Where("parent.uri IN ? AND r.created_at <= ?", uris, cutoff).
		Group("parent.uri").
		Scan(&res).Error

	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	lookup := make(map[string]int64)
	for _, row := range res {
		lookup[row.URI] = row.MaxCreatedAt.Unix() / 600
	}
	return lookup, nil
}

func (r *ChunklineRepository) LoadLocalBody(ctx context.Context, uri string, chunkID int64) ([]chunkline.BodyItem, error) {
	ctx, span := tracer.Start(ctx, "Repository.Chunkline.LoadLocalBody")
	defer span.End()

	chunkDate := time.Unix((chunkID+1)*600, 0)
	prevChunkDate := time.Unix((chunkID-1)*600, 0)

	parentRecordKey, err := GetRecordKeyByURI(ctx, r.db, uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	var members []models.RecordKey
	err = r.db.WithContext(ctx).
		Joins("JOIN records r ON r.document_id = record_keys.record_id").
		Where("parent_id = ?", parentRecordKey.ID).
		Where("r.created_at <= ?", chunkDate).
		Order("r.created_at DESC").
		Limit(defaultChunkSize).
		Preload("Record").
		Preload("Record.Document").
		Find(&members).Error
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if len(members) == 0 || members[len(members)-1].Record.CreatedAt.After(prevChunkDate) {
		err = r.db.WithContext(ctx).
			Joins("JOIN records r ON r.document_id = record_keys.record_id").
			Where("parent_id = ?", parentRecordKey.ID).
			Where("r.created_at <= ?", chunkDate).
			Where("r.created_at > ?", prevChunkDate).
			Order("r.created_at DESC").
			Preload("Record").
			Preload("Record.Document").
			Find(&members).Error
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
	}

	bodyItems := make([]chunkline.BodyItem, 0, len(members))

	for _, member := range members {

		href := member.URI
		contentType := "application/concrnt.document+json"
		if member.Record.Redirect != nil {
			href = *member.Record.Redirect
		}

		item := chunkline.BodyItem{
			Timestamp:   member.Record.CreatedAt,
			Href:        href,
			ContentType: contentType,
		}
		bodyItems = append(bodyItems, item)
	}

	return bodyItems, nil
}
