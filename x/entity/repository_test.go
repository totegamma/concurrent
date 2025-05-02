package entity

import (
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/concrnt/concrnt/core"
	mock_core "github.com/concrnt/concrnt/core/mock"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

var repo Repository
var db *gorm.DB
var mc *memcache.Client
var mockSchemaService *mock_core.MockSchemaService

func TestMain(m *testing.M) {
	log.Println("Test Start")

	var cleanupDB func()
	db, cleanupDB = testutil.CreateDB()
	defer cleanupDB()

	var cleanupMC func()
	mc, cleanupMC = testutil.CreateMC()
	defer cleanupMC()

	ctrl := gomock.NewController(nil)
	mockSchemaService = mock_core.NewMockSchemaService(ctrl)

	repo = NewRepository(db, mc, mockSchemaService)

	m.Run()

	log.Println("Test End")
}

func TestEntityRepository(t *testing.T) {
	ctx := t.Context()

	entity1ID := "con1entity1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	entity2ID := "con1entity2bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	alias1 := "alias1@example.com"
	alias2 := "alias2@example.com"

	entity1 := core.Entity{
		ID:                   entity1ID,
		Domain:               "test.example.com",
		Tag:                  "tag1",
		Score:                10,
		AffiliationDocument:  `{"type":"affiliation"}`,
		AffiliationSignature: "sig1",
		Alias:                &alias1,
		CDate:                time.Now().Add(-time.Hour),
		MDate:                time.Now().Add(-time.Hour),
	}

	entity2 := core.Entity{
		ID:                   entity2ID,
		Domain:               "test.example.com",
		Tag:                  "tag2",
		Score:                20,
		AffiliationDocument:  `{"type":"affiliation"}`,
		AffiliationSignature: "sig2",
		Alias:                &alias2,
		CDate:                time.Now(),
		MDate:                time.Now(),
	}

	meta1 := core.EntityMeta{
		ID:      entity1ID,
		Inviter: nil,
		Info:    `{"profile":"test1"}`,
	}

	meta1Updated := core.EntityMeta{
		ID:      entity1ID,
		Inviter: nil,
		Info:    `{"profile":"updated"}`,
	}

	tombstoneDoc := `{"type":"tombstone"}`
	tombstoneSig := "sigTombstone"

	tests := []struct {
		name       string
		setup      func()
		operation  func() (any, error)
		assertions func(t *testing.T, result any, err error)
		cleanup    func()
	}{
		{
			name: "Upsert Entity 1",
			operation: func() (any, error) {
				return repo.Upsert(ctx, entity1)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				created, ok := result.(core.Entity)
				assert.True(t, ok)
				assert.Equal(t, entity1.ID, created.ID)
			},
		},
		{
			name: "Upsert Entity 2",
			operation: func() (any, error) {
				return repo.Upsert(ctx, entity2)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Get Entity 1 by ID",
			operation: func() (any, error) {
				return repo.Get(ctx, entity1ID)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, ok := result.(core.Entity)
				assert.True(t, ok)
				assert.Equal(t, entity1.ID, fetched.ID)
				assert.Equal(t, entity1.Domain, fetched.Domain)
				assert.Equal(t, *entity1.Alias, *fetched.Alias)
			},
		},
		{
			name: "Get Entity 2 by Alias",
			operation: func() (any, error) {
				return repo.GetByAlias(ctx, alias2)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, ok := result.(core.Entity)
				assert.True(t, ok)
				assert.Equal(t, entity2.ID, fetched.ID)
				assert.Equal(t, alias2, *fetched.Alias)
			},
		},
		{
			name: "Get List",
			operation: func() (any, error) {
				return repo.GetList(ctx)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				list, ok := result.([]core.Entity)
				assert.True(t, ok)
				assert.Len(t, list, 2)
			},
		},
		{
			name: "Upsert Meta 1",
			operation: func() (any, error) {
				_, _, err := repo.UpsertWithMeta(ctx, entity1, meta1)
				return nil, err
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Get Meta 1",
			operation: func() (any, error) {
				return repo.GetMeta(ctx, entity1ID)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, ok := result.(core.EntityMeta)
				assert.True(t, ok)
				assert.Equal(t, entity1ID, fetched.ID)
				assert.Equal(t, meta1.Info, fetched.Info)
			},
		},
		{
			name: "Update Meta 1",
			operation: func() (any, error) {
				return nil, repo.UpdateMeta(ctx, entity1ID, meta1Updated.Info)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, errGet := repo.GetMeta(ctx, entity1ID)
				assert.NoError(t, errGet)
				assert.Equal(t, meta1Updated.Info, fetched.Info)
			},
		},
		{
			name: "Update Score Entity 2",
			operation: func() (any, error) {
				return nil, repo.UpdateScore(ctx, entity2ID, 500)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, errGet := repo.Get(ctx, entity2ID)
				assert.NoError(t, errGet)
				assert.Equal(t, 500, fetched.Score)
			},
		},
		{
			name: "Update Tag Entity 2",
			operation: func() (any, error) {
				return nil, repo.UpdateTag(ctx, entity2ID, "newTag")
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, errGet := repo.Get(ctx, entity2ID)
				assert.NoError(t, errGet)
				assert.Equal(t, "newTag", fetched.Tag)
			},
		},
		{
			name: "Set Tombstone Entity 1",
			operation: func() (any, error) {
				return nil, repo.SetTombstone(ctx, entity1ID, tombstoneDoc, tombstoneSig)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, errGet := repo.Get(ctx, entity1ID)
				assert.NoError(t, errGet)
				assert.NotNil(t, fetched.TombstoneDocument)
				assert.Equal(t, tombstoneDoc, *fetched.TombstoneDocument)
				assert.NotNil(t, fetched.TombstoneSignature)
				assert.Equal(t, tombstoneSig, strings.TrimSpace(*fetched.TombstoneSignature))
			},
		},
		{
			name: "Delete Meta 1",
			operation: func() (any, error) {
				return nil, repo.DeleteMeta(ctx, entity1ID)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				_, errGet := repo.GetMeta(ctx, entity1ID)
				assert.Error(t, errGet)
			},
		},
		{
			name: "Delete Entity 2",
			operation: func() (any, error) {
				return nil, repo.Delete(ctx, entity2ID)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				_, errGet := repo.Get(ctx, entity2ID)
				assert.Error(t, errGet)
				assert.True(t, errors.Is(errGet, core.ErrorNotFound))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			result, err := tt.operation()
			tt.assertions(t, result, err)
			if tt.cleanup != nil {
				tt.cleanup()
			}
		})
	}
}
