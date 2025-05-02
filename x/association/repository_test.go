package association

import (
	"errors"
	"log"
	"testing"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/concrnt/concrnt/core"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/concrnt/concrnt/x/schema"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

var repo Repository
var db *gorm.DB
var mc *memcache.Client

func TestMain(m *testing.M) {
	log.Println("Test Start")

	var cleanup_db func()
	db, cleanup_db = testutil.CreateDB()
	defer cleanup_db()

	var cleanup_mc func()
	mc, cleanup_mc = testutil.CreateMC()
	defer cleanup_mc()

	schemaRepository := schema.NewRepository(db)
	schemaService := schema.NewService(schemaRepository)

	repo = NewRepository(db, mc, schemaService)

	m.Run()

	log.Println("Test End")
}

// TestRepositoryOperations uses table-driven tests for repository methods
func TestRepositoryOperations(t *testing.T) {
	ctx := t.Context()
	author1 := "con1n42l2lektua69gvza8xhksq3t2we8nnlkmzct4"
	author2 := "con1sh4vuw03nn20hn94tuk7h7u3ne5n20avfl5sjm"
	schemaLike := "https://schema.concrnt.world/a/like.json"
	schemaReaction := "https://schema.concrnt.world/a/reaction.json"

	message := core.Message{
		ID:        "D895NMA837R0C6B90676P2S1J4",
		Author:    "con18fyqn098jsf6cnw2r8hkjt7zeftfa0vqvjr6fe",
		Schema:    "https://schema.concrnt.world/m/markdown.json",
		Document:  "{}",
		Signature: "DUMMY",
	}
	err := db.WithContext(ctx).Create(&message).Error
	assert.NoError(t, err)
	messageID := "m" + message.ID

	assoc1 := core.Association{
		ID:        "EQB2YB2Q529837710676PETFAR",
		Author:    author1,
		Schema:    schemaLike,
		Target:    messageID,
		Document:  "{}",
		Variant:   "",
		Unique:    "0",
		Signature: "DUMMY",
	}
	assoc2 := core.Association{
		ID:        "5GBDM539MCXKY2GJ0676PETFAR",
		Author:    author1,
		Schema:    schemaReaction,
		Target:    messageID,
		Document:  "{}",
		Variant:   "smile",
		Unique:    "1",
		Signature: "DUMMY",
	}
	assoc3 := core.Association{
		ID:        "1EQW1AEZ3WC1J42C0676PETFAR",
		Author:    author1,
		Schema:    schemaReaction,
		Target:    messageID,
		Document:  "{}",
		Variant:   "ultrafastpolar",
		Unique:    "2",
		Signature: "DUMMY",
	}
	assoc4 := core.Association{
		ID:        "KRE2MN45QXFE3AV20676PETFAR",
		Author:    author2,
		Schema:    schemaReaction,
		Target:    messageID,
		Document:  "{}",
		Variant:   "ultrafastpolar",
		Unique:    "3",
		Signature: "DUMMY",
	}

	// Test Cases
	tests := []struct {
		name       string
		setup      func()
		operation  func() error
		assertions func(t *testing.T, err error)
		cleanup    func()
	}{
		{
			name: "Create Like Association",
			operation: func() error {
				_, err := repo.Create(ctx, assoc1)
				return err
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Create Reaction Smile",
			operation: func() error {
				_, err := repo.Create(ctx, assoc2)
				return err
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Create Reaction Ultrafastpolar 1",
			operation: func() error {
				_, err := repo.Create(ctx, assoc3)
				return err
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Create Reaction Ultrafastpolar 2",
			operation: func() error {
				_, err := repo.Create(ctx, assoc4)
				return err
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Get Association by ID (Like)",
			operation: func() error {
				_, err := repo.Get(ctx, assoc1.ID)
				return err
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Get Associations by Target",
			operation: func() error {
				assocs, err := repo.GetByTarget(ctx, messageID)
				if err != nil {
					return err
				}
				assert.Len(t, assocs, 4)
				return nil
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Get Counts By Schema",
			operation: func() error {
				counts, err := repo.GetCountsBySchema(ctx, messageID)
				if err != nil {
					return err
				}
				assert.Equal(t, int64(1), counts[schemaLike])
				assert.Equal(t, int64(3), counts[schemaReaction])
				assert.Len(t, counts, 2)
				return nil
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Get By Schema (Like)",
			operation: func() error {
				assocs, err := repo.GetBySchema(ctx, messageID, schemaLike)
				if err != nil {
					return err
				}
				assert.Len(t, assocs, 1)
				return nil
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Get By Schema (Reaction)",
			operation: func() error {
				assocs, err := repo.GetBySchema(ctx, messageID, schemaReaction)
				if err != nil {
					return err
				}
				assert.Len(t, assocs, 3)
				return nil
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Get Counts By Schema and Variant (Reaction)",
			operation: func() error {
				counts, err := repo.GetCountsBySchemaAndVariant(ctx, messageID, schemaReaction)
				if err != nil {
					return err
				}
				assert.Equal(t, int64(1), (*counts)["smile"].Value)
				assert.Equal(t, int64(2), (*counts)["ultrafastpolar"].Value)
				assert.Len(t, *counts, 2)
				return nil
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Get By Schema and Variant (Reaction Ultrafastpolar)",
			operation: func() error {
				assocs, err := repo.GetBySchemaAndVariant(ctx, messageID, schemaReaction, "ultrafastpolar")
				if err != nil {
					return err
				}
				assert.Len(t, assocs, 2)
				return nil
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Get Own By Target",
			operation: func() error {
				assocs, err := repo.GetOwnByTarget(ctx, messageID, author1)
				if err != nil {
					return err
				}
				assert.Len(t, assocs, 3)
				return nil
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Delete Association (Like)",
			operation: func() error {
				return repo.Delete(ctx, assoc1.ID)
			},
			assertions: func(t *testing.T, err error) {
				assert.NoError(t, err)
				_, errGet := repo.Get(ctx, assoc1.ID)
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
			err := tt.operation()
			tt.assertions(t, err)
			if tt.cleanup != nil {
				tt.cleanup()
			}
		})
	}
}
