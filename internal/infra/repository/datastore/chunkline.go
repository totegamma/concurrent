package datastore

import (
	"context"
	"net/url"
	"time"

	gcdatastore "cloud.google.com/go/datastore"

	"github.com/concrnt/concrnt/chunkline"
	"github.com/concrnt/concrnt/internal/usecase"
)

const defaultChunkSize = 32

type ChunklineRepository struct {
	*store
}

func NewChunklineRepository(client *gcdatastore.Client, namespace string) usecase.ChunklineRepository {
	return &ChunklineRepository{store: newStore(client, namespace)}
}

func (r *ChunklineRepository) GetChunklineManifest(ctx context.Context, uri string) (*chunkline.Manifest, error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Chunkline.GetChunklineManifest")
	defer span.End()

	if _, err := r.getRecordKey(ctx, uri); err != nil {
		span.RecordError(err)
		return nil, err
	}

	firstChunk := int64(0)
	children, err := r.children(ctx, uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if len(children) > 0 {
		sortRecordKeys(children, "asc")
		firstChunk = children[0].RecordCreatedAt.Unix() / 600
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
	}, nil
}

func (r *ChunklineRepository) LookupLocalItrs(ctx context.Context, uris []string, chunkID int64) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Chunkline.LookupLocalItrs")
	defer span.End()

	cutoff := time.Unix((chunkID+1)*600, 0)
	lookup := make(map[string]int64)

	for _, uri := range uris {
		children, err := r.children(ctx, uri)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		var max time.Time
		for _, child := range children {
			if child.RecordCreatedAt.After(cutoff) {
				continue
			}
			if max.IsZero() || child.RecordCreatedAt.After(max) {
				max = child.RecordCreatedAt
			}
		}
		if !max.IsZero() {
			lookup[uri] = max.Unix() / 600
		}
	}

	return lookup, nil
}

func (r *ChunklineRepository) LoadLocalBody(ctx context.Context, uri string, chunkID int64) ([]chunkline.BodyItem, error) {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Chunkline.LoadLocalBody")
	defer span.End()

	if _, err := r.getRecordKey(ctx, uri); err != nil {
		span.RecordError(err)
		return nil, err
	}

	chunkDate := time.Unix((chunkID+1)*600, 0)
	prevChunkDate := time.Unix((chunkID-1)*600, 0)

	children, err := r.children(ctx, uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	filtered := make([]recordKeyModel, 0, len(children))
	for _, child := range children {
		if child.RecordCreatedAt.After(chunkDate) {
			continue
		}
		filtered = append(filtered, child)
	}
	sortRecordKeys(filtered, "desc")
	if len(filtered) > defaultChunkSize {
		filtered = filtered[:defaultChunkSize]
	}

	if len(filtered) == 0 || filtered[len(filtered)-1].RecordCreatedAt.After(prevChunkDate) {
		filtered = filtered[:0]
		for _, child := range children {
			if child.RecordCreatedAt.After(chunkDate) {
				continue
			}
			if !child.RecordCreatedAt.After(prevChunkDate) {
				continue
			}
			filtered = append(filtered, child)
		}
		sortRecordKeys(filtered, "desc")
	}

	bodyItems := make([]chunkline.BodyItem, 0, len(filtered))
	for _, child := range filtered {
		href := child.URI
		if child.Redirect != "" {
			href = child.Redirect
		}
		bodyItems = append(bodyItems, chunkline.BodyItem{
			Timestamp:   child.RecordCreatedAt,
			Href:        href,
			ContentType: "application/concrnt.document+json",
		})
	}
	return bodyItems, nil
}

func (r *ChunklineRepository) children(ctx context.Context, parent string) ([]recordKeyModel, error) {
	var children []recordKeyModel
	_, err := r.client.GetAll(ctx, r.query(kindRecordKey).Filter("parentURI =", parent), &children)
	if err != nil {
		return nil, err
	}
	children = filterRecordKeys(children, "", nil, nil)
	return children, nil
}

var _ usecase.ChunklineRepository = (*ChunklineRepository)(nil)
