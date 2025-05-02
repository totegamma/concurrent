package domain

import (
	"errors"
	"testing"

	"github.com/concrnt/concrnt/client/mock"
	"github.com/concrnt/concrnt/core"
	"github.com/concrnt/concrnt/x/domain/mock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestNewService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_domain.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	config := core.Config{FQDN: "local.example.com", Dimension: "testDimension", CSID: "ccs1localcsidaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}

	s := NewService(repo, client, config)
	assert.NotNil(t, s)
	assert.Implements(t, (*core.DomainService)(nil), s)
}

func TestService_Upsert(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_domain.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	config := core.Config{}
	s := NewService(repo, client, config)
	ctx := t.Context()

	domainToUpsert := core.Domain{ID: "upsert.example.com"}

	repo.EXPECT().Upsert(gomock.Any(), domainToUpsert).Return(domainToUpsert, nil).Times(1)

	result, err := s.Upsert(ctx, domainToUpsert)
	assert.NoError(t, err)
	assert.Equal(t, domainToUpsert, result)
}

func TestService_Get(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_domain.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	config := core.Config{FQDN: "local.example.com", CSID: "ccs1localcsidaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CCID: "con1localccidaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Dimension: "testDimension"}
	s := NewService(repo, client, config)
	ctx := t.Context()

	fqdn := "test.example.com"
	ccid := "con1ccid1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	csid := "ccs1csid1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	expectedDomain := core.Domain{ID: fqdn, CCID: ccid, CSID: csid}

	// Case 1: Get by FQDN
	t.Run("GetByFQDN_Success", func(t *testing.T) {
		repo.EXPECT().GetByFQDN(gomock.Any(), fqdn).Return(expectedDomain, nil).Times(1)
		result, err := s.Get(ctx, fqdn)
		assert.NoError(t, err)
		assert.Equal(t, expectedDomain, result)
	})

	// Case 2: Get by CCID
	t.Run("GetByCCID_Success", func(t *testing.T) {
		repo.EXPECT().GetByCCID(gomock.Any(), ccid).Return(expectedDomain, nil).Times(1)
		result, err := s.Get(ctx, ccid)
		assert.NoError(t, err)
		assert.Equal(t, expectedDomain, result)
	})

	// Case 3: Get by FQDN (Not Found in Repo, Fetched from Client)
	t.Run("GetByFQDN_Fetch_Success", func(t *testing.T) {
		repo.EXPECT().GetByFQDN(gomock.Any(), "fetch.example.com").Return(core.Domain{}, core.NewErrorNotFound()).Times(1)
		client.EXPECT().GetDomain(gomock.Any(), "fetch.example.com", nil).Return(core.Domain{ID: "fetch.example.com", Dimension: config.Dimension}, nil).Times(1)
		repo.EXPECT().Upsert(gomock.Any(), core.Domain{ID: "fetch.example.com", Dimension: config.Dimension}).Return(core.Domain{ID: "fetch.example.com", Dimension: config.Dimension}, nil).Times(1)
		result, err := s.GetByFQDN(ctx, "fetch.example.com")
		assert.NoError(t, err)
		assert.Equal(t, "fetch.example.com", result.ID)
	})

	// Case 4: Get by FQDN (Not Found in Repo, Fetch Fails)
	t.Run("GetByFQDN_Fetch_FailClient", func(t *testing.T) {
		repo.EXPECT().GetByFQDN(gomock.Any(), "fetchfail.example.com").Return(core.Domain{}, core.NewErrorNotFound()).Times(1)
		client.EXPECT().GetDomain(gomock.Any(), "fetchfail.example.com", nil).Return(core.Domain{}, errors.New("client error")).Times(1)
		_, err := s.GetByFQDN(ctx, "fetchfail.example.com")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "client error")
	})

	// Case 5: Get by FQDN (Not Found in Repo, Wrong Dimension)
	t.Run("GetByFQDN_Fetch_WrongDimension", func(t *testing.T) {
		repo.EXPECT().GetByFQDN(gomock.Any(), "wrongdim.example.com").Return(core.Domain{}, core.NewErrorNotFound()).Times(1)
		client.EXPECT().GetDomain(gomock.Any(), "wrongdim.example.com", nil).Return(core.Domain{ID: "wrongdim.example.com", Dimension: "wrongDimension"}, nil).Times(1)
		_, err := s.GetByFQDN(ctx, "wrongdim.example.com")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "domain is not in the same dimension")
	})
}

func TestService_ForceFetch(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_domain.NewMockRepository(ctrl)
	client := mock_client.NewMockClient(ctrl)
	config := core.Config{Dimension: "testDimension"}
	s := NewService(repo, client, config)
	ctx := t.Context()

	fqdn := "forcefetch.example.com"
	fetchedDomain := core.Domain{ID: fqdn, Dimension: config.Dimension, CCID: "fetchedCCID"}

	// Success Case
	client.EXPECT().GetDomain(gomock.Any(), fqdn, nil).Return(fetchedDomain, nil).Times(1)
	repo.EXPECT().Upsert(gomock.Any(), fetchedDomain).Return(fetchedDomain, nil).Times(1)
	result, err := s.ForceFetch(ctx, fqdn)
	assert.NoError(t, err)
	assert.Equal(t, fetchedDomain, result)

	// Fail Case: Client Error
	client.EXPECT().GetDomain(gomock.Any(), "clientfail.example.com", nil).Return(core.Domain{}, errors.New("client error")).Times(1)
	_, err = s.ForceFetch(ctx, "clientfail.example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "client error")

	// Fail Case: Wrong Dimension
	client.EXPECT().GetDomain(gomock.Any(), "wrongdim.example.com", nil).Return(core.Domain{ID: "wrongdim.example.com", Dimension: "wrong"}, nil).Times(1)
	_, err = s.ForceFetch(ctx, "wrongdim.example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "domain is not in the same dimension")

	// Fail Case: Repo Upsert Error
	client.EXPECT().GetDomain(gomock.Any(), "upsertfail.example.com", nil).Return(fetchedDomain, nil).Times(1)
	repo.EXPECT().Upsert(gomock.Any(), fetchedDomain).Return(core.Domain{}, errors.New("repo error")).Times(1)
	_, err = s.ForceFetch(ctx, "upsertfail.example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repo error")
}
