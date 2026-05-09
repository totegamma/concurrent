package usecase

import (
	"context"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/utils"
)

// RecordRepository defines storage operations for records/commits.
type RecordRepository interface {
	CreateRecord(ctx context.Context, write RecordWrite) (string, error)
	CreateAssociation(ctx context.Context, write AssociationWrite) error
	Acknowledge(ctx context.Context, write AckWrite) (string, error)
	UnAcknowledge(ctx context.Context, write AckWrite) error
	Delete(ctx context.Context, sd concrnt.SignedDocument) (string, error)

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

// CommitWrite is the storage-neutral representation of a commit log write.
type CommitWrite struct {
	ID       string
	IP       string
	Document string
	Proof    string
	Owners   []string
}

// RecordWrite contains all record data that can be derived without DB access.
type RecordWrite struct {
	Commit        CommitWrite
	DocumentID    string
	Key           string
	Owner         string
	Schema        string
	Policies      *string
	Distributions []string
	Redirect      *string
	CreatedAt     time.Time
}

// AssociationWrite contains all association data that can be derived without DB access.
type AssociationWrite struct {
	Commit     CommitWrite
	DocumentID string
	TargetURI  string
	Owner      string
	Author     string
	Schema     string
	Variant    *string
	Unique     string
	CreatedAt  time.Time
}

// AckWrite contains all ack/unack data that can be derived without DB access.
type AckWrite struct {
	Commit     CommitWrite
	DocumentID string
	From       string
	To         string
	Context    string
	Valid      bool
	CreatedAt  time.Time
	ResultURI  string
}
