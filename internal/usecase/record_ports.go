package usecase

import (
	"context"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/utils"
)

// RecordRepository defines storage operations for records/commits.
type RecordRepository interface {
	BeginTx(ctx context.Context) (RepositoryTx, error)
	Commit(ctx context.Context, tx RepositoryTx, id string, ip string, document string, proof string, owners []string) error
	CreateRecord(ctx context.Context, tx RepositoryTx, documentID string, key string, owner string, schema string, policies *string, distributions []string, redirect *string, createdAt time.Time) (string, error)
	CreateAssociation(ctx context.Context, tx RepositoryTx, documentID string, targetURI string, owner string, author string, schema string, variant *string, unique string, createdAt time.Time) error
	Acknowledge(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, ackContext string, valid bool, createdAt time.Time, resultURI string) (string, error)
	UnAcknowledge(ctx context.Context, tx RepositoryTx, documentID string, from string, to string, ackContext string, valid bool, createdAt time.Time) error
	Delete(ctx context.Context, tx RepositoryTx, targetURI string) (string, error)

	GetSignedDocument(ctx context.Context, uri string) (*concrnt.SignedDocument, error)
	GetHierarchicalRecordPolicies(ctx context.Context, uri string) ([]concrnt.Policy, error)
	GetAllCommitLogs(ctx context.Context, owner string) ([]concrnt.SignedDocument, error)

	GetDistributions(ctx context.Context, uri string) ([]string, error)

	GetAcknowledgeRecords(ctx context.Context, from, to, context string) ([]concrnt.SignedDocument, error)
	GetAcknowledgeRecordCounts(ctx context.Context, from, to, context string) (map[string]int64, error)
	GetAssociatedRecords(ctx context.Context, targetURI, schema, variant, author string) ([]concrnt.SignedDocument, error)
	GetAssociatedRecordCountsBySchema(ctx context.Context, targetURI string) (map[string]int64, error)
	GetAssociatedRecordCountsByVariant(ctx context.Context, targetURI, schema string) (*utils.OrderedKVMap[int64], error)

	QueryByPrefix(ctx context.Context, prefix, schema string, since, until *time.Time, limit int, order string) ([]concrnt.SignedDocument, error)
	QueryByParent(ctx context.Context, parent, schema string, since, until *time.Time, limit int, order string) ([]concrnt.SignedDocument, error)
}

type RepositoryTx interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}
