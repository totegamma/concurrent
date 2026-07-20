package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/concrnt/concrnt/chunkline"
)

// removedItemsTTL bounds how long a timeline advertises a removed item;
// mirrors v1's 2-day retracted window. Readers only use the list to filter
// already-cached chunks, so entries outliving their usefulness are harmless.
const removedItemsTTL = 48 * time.Hour

// removedItemsKey namespaces the KVS set of item IDs recently removed from a
// timeline, written on delete and served via /chunkline/removed.
func removedItemsKey(timeline string) string { return "removed:" + timeline }

type ChunklineUsecase struct {
	repo    ChunklineRepository
	gateway ChunklineGateway
	kvs     KVS
}

// ChunklineRepository defines storage operations for chunkline timelines.
type ChunklineRepository interface {
	GetChunklineManifest(ctx context.Context, uri string) (*chunkline.Manifest, error)
	LookupLocalItrs(ctx context.Context, uris []string, chunkID int64) (map[string]int64, error)
	LoadLocalBody(ctx context.Context, uri string, chunkID int64) ([]chunkline.BodyItem, error)
}

// ChunklineGateway encapsulates external timeline resolution.
type ChunklineGateway interface {
	QueryDescending(ctx context.Context, uris []string, until time.Time, limit int) ([]chunkline.BodyItemWithSource, error)
}

func NewChunklineUsecase(repo ChunklineRepository, gateway ChunklineGateway, kvs KVS) *ChunklineUsecase {
	return &ChunklineUsecase{
		repo:    repo,
		gateway: gateway,
		kvs:     kvs,
	}
}

func (uc *ChunklineUsecase) GetChunklineManifest(ctx context.Context, uri string) (*chunkline.Manifest, error) {
	return uc.repo.GetChunklineManifest(ctx, uri)
}

func (uc *ChunklineUsecase) LookupLocalItrs(ctx context.Context, uris []string, chunkID int64) (map[string]int64, error) {
	return uc.repo.LookupLocalItrs(ctx, uris, chunkID)
}

func (uc *ChunklineUsecase) LoadLocalBody(ctx context.Context, uri string, chunkID int64) ([]chunkline.BodyItem, error) {
	return uc.repo.LoadLocalBody(ctx, uri, chunkID)
}

// GetLocalRemovedItems serves the /chunkline/removed endpoint: the item IDs
// recently removed from a locally-hosted timeline, advertised so readers can
// drop them from already-cached chunks.
func (uc *ChunklineUsecase) GetLocalRemovedItems(ctx context.Context, uri string) ([]string, error) {
	if uc.kvs == nil {
		return []string{}, nil
	}
	items, err := uc.kvs.SetMembers(ctx, removedItemsKey(uri))
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []string{}
	}
	return items, nil
}

func (uc *ChunklineUsecase) GetRecent(ctx context.Context, uris []string, until time.Time, limit int) ([]chunkline.BodyItemWithSource, error) {

	if uc.gateway == nil {
		return nil, fmt.Errorf("chunkline gateway not configured")
	}

	items, err := uc.gateway.QueryDescending(ctx, uris, until, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query descending: %v", err)
	}

	return items, nil
}
