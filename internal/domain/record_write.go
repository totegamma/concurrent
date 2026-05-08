package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/zeebo/xxh3"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/schemas"
)

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

func NewRecordWrite(ip string, documentID string, sd concrnt.SignedDocument) (RecordWrite, error) {
	var doc concrnt.Document[any]
	if err := json.Unmarshal([]byte(sd.Document), &doc); err != nil {
		return RecordWrite{}, err
	}

	parsed, err := concrnt.ParseCCURI(doc.Key)
	if err != nil {
		return RecordWrite{}, err
	}
	if parsed.Scheme != "cckv" {
		return RecordWrite{}, fmt.Errorf("invalid key: document key scheme must be cckv")
	}

	commit, err := newCommitWrite(ip, documentID, sd, []string{parsed.Owner})
	if err != nil {
		return RecordWrite{}, err
	}

	var policies *string
	if doc.Policy != nil {
		policyBytes, err := json.Marshal(doc.Policy)
		if err != nil {
			return RecordWrite{}, err
		}
		policyStr := string(policyBytes)
		policies = &policyStr
	}

	distributions := []string{}
	if doc.Distributes != nil {
		distributions = *doc.Distributes
	}

	write := RecordWrite{
		Commit:        commit,
		DocumentID:    documentID,
		Key:           doc.Key,
		Owner:         parsed.Owner,
		Schema:        doc.Schema,
		Policies:      policies,
		Distributions: distributions,
		CreatedAt:     doc.CreatedAt,
	}

	if doc.Schema == schemas.ReferenceURL {
		var refDoc concrnt.Document[schemas.Reference]
		if err := json.Unmarshal([]byte(sd.Document), &refDoc); err != nil {
			return RecordWrite{}, err
		}
		write.Redirect = &refDoc.Value.Href

		refSD, ok := sd.References[refDoc.Value.Href]
		if ok {
			var targetDoc concrnt.Document[any]
			if err := json.Unmarshal([]byte(refSD.Document), &targetDoc); err != nil {
				return RecordWrite{}, err
			}
			write.Schema = targetDoc.Schema
			write.CreatedAt = targetDoc.CreatedAt
		} else {
			if refDoc.Value.Schema != nil {
				write.Schema = refDoc.Schema
			}
			if refDoc.Value.CreatedAt != nil {
				write.CreatedAt = *refDoc.Value.CreatedAt
			}
		}
	}

	return write, nil
}

func NewAssociationWrite(ip string, documentID string, parsed concrnt.Document[any], sd concrnt.SignedDocument) (AssociationWrite, error) {
	if parsed.Associate == nil {
		return AssociationWrite{}, fmt.Errorf("associate is required")
	}

	targetURI, err := concrnt.ParseCCURI(*parsed.Associate)
	if err != nil {
		return AssociationWrite{}, err
	}
	if targetURI.Scheme != "cckv" {
		return AssociationWrite{}, fmt.Errorf("invalid associate: document associate scheme must be cckv")
	}

	commit, err := newCommitWrite(ip, documentID, sd, []string{targetURI.Owner})
	if err != nil {
		return AssociationWrite{}, err
	}

	uniqueKey := targetURI.Owner + parsed.Author + *parsed.Associate
	if parsed.AssociationVariant != nil {
		uniqueKey += *parsed.AssociationVariant
	}
	uniqueHash := xxh3.HashString(uniqueKey)

	return AssociationWrite{
		Commit:     commit,
		DocumentID: documentID,
		TargetURI:  *parsed.Associate,
		Owner:      targetURI.Owner,
		Author:     parsed.Author,
		Schema:     parsed.Schema,
		Variant:    parsed.AssociationVariant,
		Unique:     fmt.Sprintf("%x", uniqueHash),
		CreatedAt:  parsed.CreatedAt,
	}, nil
}

func NewAckWrite(ip string, documentID string, sd concrnt.SignedDocument, valid bool) (AckWrite, error) {
	var doc concrnt.Document[schemas.Acknowledge]
	if err := json.Unmarshal([]byte(sd.Document), &doc); err != nil {
		return AckWrite{}, err
	}
	if doc.Associate == nil {
		return AckWrite{}, fmt.Errorf("associate is required")
	}

	parsed, err := concrnt.ParseCCURI(*doc.Associate)
	if err != nil {
		return AckWrite{}, err
	}
	if parsed.Scheme != "cckv" {
		return AckWrite{}, fmt.Errorf("invalid associate: document associate scheme must be cckv")
	}

	commit, err := newCommitWrite(ip, documentID, sd, []string{doc.Author, parsed.Owner})
	if err != nil {
		return AckWrite{}, err
	}

	return AckWrite{
		Commit:     commit,
		DocumentID: documentID,
		From:       doc.Author,
		To:         parsed.Owner,
		Context:    doc.Value.Context,
		Valid:      valid,
		CreatedAt:  doc.CreatedAt,
		ResultURI:  concrnt.ComposeCCURI("ccfs", parsed.Owner, documentID),
	}, nil
}

func newCommitWrite(ip string, documentID string, sd concrnt.SignedDocument, owners []string) (CommitWrite, error) {
	proof, err := json.Marshal(sd.Proof)
	if err != nil {
		return CommitWrite{}, err
	}
	return CommitWrite{
		ID:       documentID,
		IP:       ip,
		Document: sd.Document,
		Proof:    string(proof),
		Owners:   owners,
	}, nil
}
