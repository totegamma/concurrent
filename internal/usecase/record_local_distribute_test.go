package usecase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/impl/interop"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/service"
	"github.com/concrnt/concrnt/internal/utils"
	"github.com/concrnt/concrnt/policy"
	"github.com/concrnt/concrnt/schemas"
)

func TestRecordCommitLocalTimelineDistributeRequiresExternalResolver(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, interop.ServiceAccountTypeCtxKey, "system")

	localOwner := "con" + strings.Repeat("a", 39)
	fqdn := "local.example"
	resolverDomain := "resolver.example"
	var resolverRequests atomic.Int64
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/concrnt" {
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(concrnt.WellKnownConcrnt{
				Domain: resolverDomain,
				Endpoints: map[string]string{
					"net.concrnt.core.resolve": "/resolve/{uri}",
				},
			}))
			return
		}

		resolverRequests.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(resolver.Close)

	cl := client.New(resolverDomain)
	cl.AddHostRemapping(resolverDomain, resolver.URL)

	repo := newLocalDistributeRecordRepository()
	entity := NewEntityUsecase(&localDistributeEntityRepository{
		entity: domain.Entity{
			ID:     localOwner,
			Domain: fqdn,
		},
	}, &domain.Config{FQDN: fqdn})
	uc := NewRecordUsecase(
		repo,
		&domain.Config{FQDN: fqdn},
		cl,
		entity,
		service.NewSignalService(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})),
		service.NewPolicyService(policy.Policy{
			Defaults: map[string]policy.Conclusion{
				"record:create": policy.ALLOW,
			},
		}, service.GlobalParameters{FQDN: fqdn}, cl),
	)

	timeline := concrnt.ComposeCCURI("cckv", localOwner, "timeline")
	sourceKey := concrnt.ComposeCCURI("cckv", localOwner, "posts/source")
	distributions := []string{timeline}
	sd := signedTestDocument(t, concrnt.Document[map[string]string]{
		Key:         sourceKey,
		Value:       map[string]string{"body": "source"},
		Author:      localOwner,
		Schema:      "https://schema.example/post.json",
		CreatedAt:   time.Date(2026, 5, 17, 6, 54, 50, 0, time.UTC),
		Distributes: &distributions,
	})

	_, err := uc.Commit(ctx, "192.0.2.10", sd, domain.CommitModeExecute)
	require.NoError(t, err)

	require.Greater(t, resolverRequests.Load(), int64(0), "local distribution tried to resolve the local CCID through the external resolver")
	require.True(t, repo.hasRecord(sourceKey), "the source record is committed")
	require.False(t, repo.hasRecordPrefix(timeline+"/"), "the local timeline reference record is not committed when resolver lookup fails")
}

type localDistributeRecordRepository struct {
	records map[string]string
}

func newLocalDistributeRecordRepository() *localDistributeRecordRepository {
	return &localDistributeRecordRepository{records: make(map[string]string)}
}

func (r *localDistributeRecordRepository) BeginTx(ctx context.Context) (RepositoryTx, error) {
	return localDistributeTx{}, nil
}

func (r *localDistributeRecordRepository) CreateCommitLog(ctx context.Context, tx RepositoryTx, id string, ip string, document string, proof any) error {
	return nil
}

func (r *localDistributeRecordRepository) CreateCommitOwners(ctx context.Context, tx RepositoryTx, id string, owners []string) error {
	return nil
}

func (r *localDistributeRecordRepository) CreateRecord(ctx context.Context, tx RepositoryTx, documentID string, key string, owner string, schema string, policies *string, distributions []string, redirect *string, createdAt time.Time) (string, error) {
	r.records[key] = documentID
	return key, nil
}

func (r *localDistributeRecordRepository) CreateAssociation(ctx context.Context, tx RepositoryTx, documentID string, targetURI string, owner string, author string, schema string, variant *string, unique string, createdAt time.Time) error {
	return nil
}

func (r *localDistributeRecordRepository) Acknowledge(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, ackContext string, valid bool, createdAt time.Time, resultURI string) (string, error) {
	return resultURI, nil
}

func (r *localDistributeRecordRepository) UnAcknowledge(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, ackContext string, valid bool, createdAt time.Time) error {
	return nil
}

func (r *localDistributeRecordRepository) Delete(ctx context.Context, tx RepositoryTx, targetURI string) (string, error) {
	return targetURI, nil
}

func (r *localDistributeRecordRepository) GetSignedDocument(ctx context.Context, uri string) (*concrnt.SignedDocument, error) {
	return nil, domain.ErrNotFound
}

func (r *localDistributeRecordRepository) GetHierarchicalRecordPolicies(ctx context.Context, uri string) ([]concrnt.Policy, error) {
	return nil, nil
}

func (r *localDistributeRecordRepository) GetAllCommitLogs(ctx context.Context, owner string) ([]concrnt.SignedDocument, error) {
	return nil, nil
}

func (r *localDistributeRecordRepository) GetDistributions(ctx context.Context, uri string) ([]string, error) {
	return nil, nil
}

func (r *localDistributeRecordRepository) GetAcknowledgeRecords(ctx context.Context, from, to, context string) ([]concrnt.SignedDocument, error) {
	return nil, nil
}

func (r *localDistributeRecordRepository) GetAcknowledgeRecordCounts(ctx context.Context, from, to, context string) (map[string]int64, error) {
	return nil, nil
}

func (r *localDistributeRecordRepository) GetAssociatedRecords(ctx context.Context, targetURI, schema, variant, author string) ([]concrnt.SignedDocument, error) {
	return nil, nil
}

func (r *localDistributeRecordRepository) GetAssociatedRecordCountsBySchema(ctx context.Context, targetURI string) (map[string]int64, error) {
	return nil, nil
}

func (r *localDistributeRecordRepository) GetAssociatedRecordCountsByVariant(ctx context.Context, targetURI, schema string) (*utils.OrderedKVMap[int64], error) {
	return nil, nil
}

func (r *localDistributeRecordRepository) QueryByPrefix(ctx context.Context, prefix, schema string, since, until *time.Time, limit int, order string) ([]concrnt.SignedDocument, error) {
	return nil, nil
}

func (r *localDistributeRecordRepository) QueryByParent(ctx context.Context, parent, schema string, since, until *time.Time, limit int, order string) ([]concrnt.SignedDocument, error) {
	return nil, nil
}

func (r *localDistributeRecordRepository) hasRecord(key string) bool {
	_, ok := r.records[key]
	return ok
}

func (r *localDistributeRecordRepository) hasRecordPrefix(prefix string) bool {
	for key := range r.records {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

type localDistributeTx struct{}

func (localDistributeTx) Commit(ctx context.Context) error {
	return nil
}

func (localDistributeTx) Rollback(ctx context.Context) error {
	return nil
}

type localDistributeEntityRepository struct {
	entity domain.Entity
}

func (r *localDistributeEntityRepository) SaveMeta(ctx context.Context, meta domain.EntityMeta) error {
	return nil
}

func (r *localDistributeEntityRepository) SaveEntity(ctx context.Context, sd concrnt.SignedDocument) (*concrnt.Document[schemas.Entity], error) {
	return nil, nil
}

func (r *localDistributeEntityRepository) Get(ctx context.Context, ccid string, hint *string) (*domain.Entity, error) {
	if parsed, err := concrnt.ParseCCURI(ccid); err == nil && parsed.Owner != "" {
		ccid = parsed.Owner
	}
	if ccid != r.entity.ID {
		return nil, domain.ErrNotFound
	}
	return &r.entity, nil
}

func (r *localDistributeEntityRepository) GetSD(ctx context.Context, ccid string, hint *string) (*concrnt.SignedDocument, error) {
	if ccid != r.entity.ID {
		return nil, domain.ErrNotFound
	}
	sd := signedTestDocumentFromDocument(concrnt.Document[schemas.Entity]{
		Key:    concrnt.ComposeCCURI("cckv", r.entity.ID, ""),
		Author: r.entity.ID,
		Schema: schemas.EntityURL,
		Value: schemas.Entity{
			Domain: r.entity.Domain,
		},
		CreatedAt: time.Date(2026, 5, 17, 6, 54, 50, 0, time.UTC),
	})
	return &sd, nil
}

func (r *localDistributeEntityRepository) GetDocument(ctx context.Context, ccid string, hint *string) (*concrnt.Document[schemas.Entity], error) {
	return nil, nil
}

func (r *localDistributeEntityRepository) GetByAlias(ctx context.Context, alias string) (*domain.Entity, error) {
	return nil, domain.ErrNotFound
}

func (r *localDistributeEntityRepository) GetMeta(ctx context.Context, ccid string) (*domain.EntityMeta, error) {
	return nil, domain.ErrNotFound
}

func signedTestDocument(t *testing.T, doc any) concrnt.SignedDocument {
	t.Helper()
	return signedTestDocumentFromDocument(doc)
}

func signedTestDocumentFromDocument(doc any) concrnt.SignedDocument {
	docBytes, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return concrnt.SignedDocument{
		Document: string(docBytes),
		Proof: concrnt.Proof{
			Type: concrnt.ProofTypeNone,
		},
	}
}
