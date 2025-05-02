package domain

import (
	"errors"
	"log"
	"testing"
	"time"

	"github.com/concrnt/concrnt/core"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

var repo Repository
var db *gorm.DB

func TestMain(m *testing.M) {
	log.Println("Test Start")

	var cleanup func()
	db, cleanup = testutil.CreateDB()
	defer cleanup()

	repo = NewRepository(db)

	m.Run()

	log.Println("Test End")
}

func TestRepository(t *testing.T) {
	ctx := t.Context()

	ccid1 := "con1ccid1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	csid1 := "ccs1csid1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ccid2 := "con1ccid2bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	csid2 := "ccs1csid2bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	csid1new := "ccs1csid1newcccccccccccccccccccccccccccccc"

	domain1 := core.Domain{
		ID:           "test1.example.com",
		CCID:         ccid1,
		CSID:         csid1,
		Tag:          "testTag1",
		Score:        100,
		IsScoreFixed: false,
		LastScraped:  time.Now().Add(-time.Hour),
	}

	domain2 := core.Domain{
		ID:           "test2.example.com",
		CCID:         ccid2,
		CSID:         csid2,
		Tag:          "testTag2",
		Score:        200,
		IsScoreFixed: true,
		LastScraped:  time.Now().Add(-2 * time.Hour),
	}

	domain1Updated := core.Domain{
		ID:           "test1.example.com",
		CCID:         ccid1,
		CSID:         csid1new,
		Tag:          "testTag1Updated",
		Score:        150,
		IsScoreFixed: true,
		LastScraped:  time.Now().Add(-30 * time.Minute),
	}

	tests := []struct {
		name       string
		setup      func()
		operation  func() (any, error)
		assertions func(t *testing.T, result any, err error)
		cleanup    func()
	}{
		{
			name: "Upsert Domain 1",
			operation: func() (any, error) {
				return repo.Upsert(ctx, domain1)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				created, ok := result.(core.Domain)
				assert.True(t, ok)
				assert.Equal(t, domain1.ID, created.ID)
				assert.Equal(t, ccid1, created.CCID)
			},
		},
		{
			name: "Upsert Domain 2",
			operation: func() (any, error) {
				return repo.Upsert(ctx, domain2)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				created, ok := result.(core.Domain)
				assert.True(t, ok)
				assert.Equal(t, domain2.ID, created.ID)
				assert.Equal(t, ccid2, created.CCID)
			},
		},
		{
			name: "Get By FQDN (Domain 1)",
			operation: func() (any, error) {
				return repo.GetByFQDN(ctx, domain1.ID)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, ok := result.(core.Domain)
				assert.True(t, ok)
				assert.Equal(t, domain1.ID, fetched.ID)
				assert.Equal(t, ccid1, fetched.CCID)
			},
		},
		{
			name: "Get By FQDN (Not Found)",
			operation: func() (any, error) {
				return repo.GetByFQDN(ctx, "notfound.example.com")
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, core.ErrorNotFound))
			},
		},
		{
			name: "Get By CCID (Domain 2)",
			operation: func() (any, error) {
				return repo.GetByCCID(ctx, ccid2)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, ok := result.(core.Domain)
				assert.True(t, ok)
				assert.Equal(t, domain2.ID, fetched.ID)
				assert.Equal(t, ccid2, fetched.CCID)
			},
		},
		{
			name: "Get By CCID (Not Found)",
			operation: func() (any, error) {
				return repo.GetByCCID(ctx, "con1notfoundccidaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, core.ErrorNotFound))
			},
		},
		{
			name: "Get By CSID (Domain 1)",
			operation: func() (any, error) {
				return repo.GetByCSID(ctx, csid1)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, ok := result.(core.Domain)
				assert.True(t, ok)
				assert.Equal(t, domain1.ID, fetched.ID)
				assert.Equal(t, csid1, fetched.CSID)
			},
		},
		{
			name: "Get List",
			operation: func() (any, error) {
				return repo.GetList(ctx)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				list, ok := result.([]core.Domain)
				assert.True(t, ok)
				assert.Len(t, list, 2)
			},
		},
		{
			name: "Update Domain 1",
			operation: func() (any, error) {
				err := repo.Update(ctx, domain1Updated)
				return nil, err
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				fetched, errGet := repo.GetByFQDN(ctx, domain1.ID)
				assert.NoError(t, errGet)
				assert.Equal(t, csid1new, fetched.CSID)
				assert.Equal(t, domain1Updated.Tag, fetched.Tag)
				assert.Equal(t, domain1Updated.Score, fetched.Score)
				assert.Equal(t, domain1Updated.IsScoreFixed, fetched.IsScoreFixed)
			},
		},
		{
			name: "Update Scrape Time Domain 2",
			operation: func() (any, error) {
				newScrapeTime := time.Now()
				err := repo.UpdateScrapeTime(ctx, domain2.ID, newScrapeTime)
				return newScrapeTime, err
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				newScrapeTime, ok := result.(time.Time)
				assert.True(t, ok)
				fetched, errGet := repo.GetByFQDN(ctx, domain2.ID)
				assert.NoError(t, errGet)
				assert.WithinDuration(t, newScrapeTime, fetched.LastScraped, time.Second)
			},
		},
		{
			name: "Delete Domain 1",
			operation: func() (any, error) {
				return nil, repo.Delete(ctx, domain1.ID)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				_, errGet := repo.GetByFQDN(ctx, domain1.ID)
				assert.Error(t, errGet)
				assert.True(t, errors.Is(errGet, core.ErrorNotFound))
			},
		},
		{
			name: "Get List After Delete",
			operation: func() (any, error) {
				return repo.GetList(ctx)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				list, ok := result.([]core.Domain)
				assert.True(t, ok)
				assert.Len(t, list, 1)
				assert.Equal(t, domain2.ID, list[0].ID)
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
