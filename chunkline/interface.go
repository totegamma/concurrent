package chunkline

import (
	"context"
	"time"
)

type Resolver interface {
	ResolveTimelines(ctx context.Context, timelines []string) (map[string]Manifest, error)
	GetRemovedItems(ctx context.Context, timelines []string) (map[string][]string, error)
	LookupChunkItrs(ctx context.Context, timelines []string, until time.Time) (map[string]string, error)
	LoadChunkBodies(ctx context.Context, query map[string]string) (map[string]BodyChunk, error)
}
