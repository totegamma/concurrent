package firestore

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	gcfirestore "cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/usecase"
)

func isNoSuchEntity(err error) bool {
	return status.Code(err) == codes.NotFound
}

func wrapNotFound(resource string, err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(domain.NotFoundError{Resource: resource}, err)
}

func proofFromString(proof string) (concrnt.Proof, error) {
	var p concrnt.Proof
	if err := json.Unmarshal([]byte(proof), &p); err != nil {
		return concrnt.Proof{}, err
	}
	return p, nil
}

func signedDocumentFromCommit(commit commitLogModel, cckv, ccfs *string) (*concrnt.SignedDocument, error) {
	proof, err := proofFromString(commit.Proof)
	if err != nil {
		return nil, err
	}
	return &concrnt.SignedDocument{
		CCKV:     cckv,
		CCFS:     ccfs,
		Document: commit.Document,
		Proof:    proof,
	}, nil
}

func (s *store) get(ctx context.Context, ref *gcfirestore.DocumentRef, dest any) error {
	snapshot, err := ref.Get(ctx)
	if err != nil {
		return err
	}
	return snapshot.DataTo(dest)
}

func (s *store) txGet(tx *gcfirestore.Transaction, ref *gcfirestore.DocumentRef, dest any) error {
	snapshot, err := tx.Get(ref)
	if err != nil {
		return err
	}
	return snapshot.DataTo(dest)
}

func getAll[T any](ctx context.Context, query gcfirestore.Query) ([]T, []*gcfirestore.DocumentRef, error) {
	snapshots, err := query.Documents(ctx).GetAll()
	if err != nil {
		return nil, nil, err
	}

	items := make([]T, 0, len(snapshots))
	refs := make([]*gcfirestore.DocumentRef, 0, len(snapshots))
	for _, snapshot := range snapshots {
		var item T
		if err := snapshot.DataTo(&item); err != nil {
			return nil, nil, err
		}
		items = append(items, item)
		refs = append(refs, snapshot.Ref)
	}

	return items, refs, nil
}

func parentURIOf(uri string) (string, bool, error) {
	parentURI, err := url.JoinPath(uri, "..")
	if err != nil {
		return "", false, err
	}
	if parentURI == uri {
		return "", false, nil
	}
	parsed, err := url.Parse(parentURI)
	if err != nil {
		return "", false, err
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return "", false, nil
	}
	return parentURI, true, nil
}

func prefixesForURI(uri string) []string {
	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil || parsed.Scheme != "cckv" {
		return []string{uri}
	}

	seen := map[string]struct{}{}
	prefixes := []string{}
	add := func(prefix string) {
		if _, ok := seen[prefix]; ok {
			return
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}

	add(concrnt.ComposeCCURI("cckv", parsed.Owner, ""))

	parts := strings.Split(strings.Trim(parsed.Key, "/"), "/")
	current := ""
	for i, part := range parts {
		if part == "" {
			continue
		}
		current = path.Join(current, part)
		prefix := concrnt.ComposeCCURI("cckv", parsed.Owner, current)
		add(prefix)
		if i < len(parts)-1 {
			add(prefix + "/")
		}
	}
	add(uri)

	return prefixes
}

func (s *store) getCommit(ctx context.Context, id string) (commitLogModel, error) {
	var commit commitLogModel
	err := s.get(ctx, s.doc(kindCommitLog, id), &commit)
	if err != nil {
		if isNoSuchEntity(err) {
			return commitLogModel{}, wrapNotFound(id, err)
		}
		return commitLogModel{}, err
	}
	return commit, nil
}

func (s *store) getRecord(ctx context.Context, id string) (recordModel, error) {
	var record recordModel
	err := s.get(ctx, s.doc(kindRecord, id), &record)
	if err != nil {
		if isNoSuchEntity(err) {
			return recordModel{}, wrapNotFound(id, err)
		}
		return recordModel{}, err
	}
	return record, nil
}

func (s *store) getRecordKey(ctx context.Context, uri string) (recordKeyModel, error) {
	var rk recordKeyModel
	err := s.get(ctx, s.doc(kindRecordKey, uri), &rk)
	if err != nil {
		if isNoSuchEntity(err) {
			return recordKeyModel{}, wrapNotFound(uri, err)
		}
		return recordKeyModel{}, err
	}
	return rk, nil
}

func (s *store) createCommitLogAndOwners(ctx context.Context, tx *gcfirestore.Transaction, commit usecase.CommitWrite) (time.Time, error) {
	commitRef := s.doc(kindCommitLog, commit.ID)
	var existing commitLogModel
	err := s.txGet(tx, commitRef, &existing)
	now := time.Now().UTC()
	commitCreatedAt := now
	if err == nil {
		commitCreatedAt = existing.CDate
	} else if isNoSuchEntity(err) {
		newCommit := commitLogModel{
			ID:       commit.ID,
			IP:       commit.IP,
			Document: commit.Document,
			Proof:    commit.Proof,
			CDate:    now,
		}
		if err := tx.Set(commitRef, &newCommit); err != nil {
			return time.Time{}, err
		}
	} else {
		return time.Time{}, err
	}

	for _, owner := range commit.Owners {
		ownerRef := s.doc(kindCommitOwner, compoundKey(owner, commit.ID))
		var existingOwner commitOwnerModel
		err := s.txGet(tx, ownerRef, &existingOwner)
		if err == nil {
			continue
		}
		if !isNoSuchEntity(err) {
			return time.Time{}, err
		}

		commitOwner := commitOwnerModel{
			Owner:           owner,
			CommitID:        commit.ID,
			CommitCreatedAt: commitCreatedAt,
		}
		if err := tx.Set(ownerRef, &commitOwner); err != nil {
			return time.Time{}, err
		}
	}

	return commitCreatedAt, nil
}

func (s *store) ensureParentRecordKeys(ctx context.Context, tx *gcfirestore.Transaction, uri string) (string, error) {
	parentURI, ok, err := parentURIOf(uri)
	if err != nil || !ok {
		return "", err
	}

	grandParentURI, err := s.ensureParentRecordKeys(ctx, tx, parentURI)
	if err != nil {
		return "", err
	}

	ref := s.doc(kindRecordKey, parentURI)
	var existing recordKeyModel
	err = s.txGet(tx, ref, &existing)
	if err == nil {
		changed := false
		if existing.URI == "" {
			existing.URI = parentURI
			changed = true
		}
		if existing.ParentURI == "" && grandParentURI != "" {
			existing.ParentURI = grandParentURI
			changed = true
		}
		if len(existing.Prefixes) == 0 {
			existing.Prefixes = prefixesForURI(parentURI)
			changed = true
		}
		if changed {
			if err := tx.Set(ref, &existing); err != nil {
				return "", err
			}
		}
		return parentURI, nil
	}
	if !isNoSuchEntity(err) {
		return "", err
	}

	placeholder := recordKeyModel{
		URI:       parentURI,
		ParentURI: grandParentURI,
		Prefixes:  prefixesForURI(parentURI),
	}
	if err := tx.Set(ref, &placeholder); err != nil {
		return "", err
	}
	return parentURI, nil
}

func (s *store) allRecordKeys(ctx context.Context) ([]recordKeyModel, []*gcfirestore.DocumentRef, error) {
	return getAll[recordKeyModel](ctx, s.query(kindRecordKey))
}

func sortRecordKeys(recordKeys []recordKeyModel, order string) {
	sort.SliceStable(recordKeys, func(i, j int) bool {
		if recordKeys[i].RecordCreatedAt.Equal(recordKeys[j].RecordCreatedAt) {
			if order == "desc" {
				return recordKeys[i].URI > recordKeys[j].URI
			}
			return recordKeys[i].URI < recordKeys[j].URI
		}
		if order == "desc" {
			return recordKeys[i].RecordCreatedAt.After(recordKeys[j].RecordCreatedAt)
		}
		return recordKeys[i].RecordCreatedAt.Before(recordKeys[j].RecordCreatedAt)
	})
}

func filterRecordKeys(recordKeys []recordKeyModel, schema string, since, until *time.Time) []recordKeyModel {
	filtered := make([]recordKeyModel, 0, len(recordKeys))
	for _, rk := range recordKeys {
		if rk.RecordID == "" {
			continue
		}
		if schema != "" && rk.RecordSchema != schema {
			continue
		}
		if since != nil && rk.RecordCreatedAt.Before(*since) {
			continue
		}
		if until != nil && rk.RecordCreatedAt.After(*until) {
			continue
		}
		filtered = append(filtered, rk)
	}
	return filtered
}

func applyRecordKeyLimit(recordKeys []recordKeyModel, limit int) []recordKeyModel {
	if limit > 0 && len(recordKeys) > limit {
		return recordKeys[:limit]
	}
	return recordKeys
}
