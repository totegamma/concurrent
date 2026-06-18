package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/infra/database/models"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/stretchr/testify/require"
)

func TestChunklineRepositoryUsesRecordCreatedAt(t *testing.T) {
	db, cleanup := testutil.CreateDB()
	t.Cleanup(cleanup)

	ctx := context.Background()
	recordRepo := NewRecordRepository(db)
	chunklineRepo := NewChunklineRepository(db)

	parentURI := "cckv://con1owner/timeline"
	createdAt10 := time.Unix(10*600+10, 0).UTC()
	createdAt11 := time.Unix(11*600+20, 0).UTC()
	createdAt12 := time.Unix(12*600+30, 0).UTC()

	createChunklineRecord(t, ctx, recordRepo, "record-10", parentURI+"/record-10", createdAt10)
	createChunklineRecord(t, ctx, recordRepo, "record-11", parentURI+"/record-11", createdAt11)
	createChunklineRecord(t, ctx, recordRepo, "record-12", parentURI+"/record-12", createdAt12)

	var parent models.RecordKey
	require.NoError(t, db.Where("uri = ?", parentURI).Take(&parent).Error)
	require.Nil(t, parent.RecordCreatedAt)

	manifest, err := chunklineRepo.GetChunklineManifest(ctx, parentURI)
	require.NoError(t, err)
	require.NotNil(t, manifest.FirstChunk)
	require.EqualValues(t, 10, *manifest.FirstChunk)

	itrs, err := chunklineRepo.LookupLocalItrs(ctx, []string{parentURI}, 11)
	require.NoError(t, err)
	require.EqualValues(t, 11, itrs[parentURI])

	body, err := chunklineRepo.LoadLocalBody(ctx, parentURI, 11)
	require.NoError(t, err)
	require.Len(t, body, 2)
	require.Equal(t, parentURI+"/record-11", body[0].Href)
	require.True(t, body[0].Timestamp.Equal(createdAt11))
	require.Equal(t, parentURI+"/record-10", body[1].Href)
	require.True(t, body[1].Timestamp.Equal(createdAt10))
}

func createChunklineRecord(t *testing.T, ctx context.Context, repo usecase.RecordRepository, id string, key string, createdAt time.Time) {
	t.Helper()

	sd := repositorySignedDocument(t, concrnt.Document[map[string]string]{
		Kind:      "record",
		Key:       key,
		Value:     map[string]string{"body": id},
		Author:    "con1author",
		Schema:    "https://schema.example/post.json",
		CreatedAt: createdAt,
	})

	withRepositoryTx(t, ctx, repo, id, "127.0.0.1", sd, []string{"con1owner"}, func(tx usecase.RepositoryTx) error {
		_, err := repo.CreateRecord(ctx, tx, id, key, "con1owner", "https://schema.example/post.json", nil, nil, []string{}, nil, createdAt)
		return err
	})
}
