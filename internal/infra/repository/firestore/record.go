package firestore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	gcfirestore "cloud.google.com/go/firestore"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/concrnt/concrnt/internal/utils"
	"github.com/concrnt/concrnt/schemas"
)

type RecordRepository struct {
	*store
}

func NewRecordRepository(client *gcfirestore.Client, namespace string) usecase.RecordRepository {
	return &RecordRepository{store: newStore(client, namespace)}
}

func (r *RecordRepository) CreateRecord(ctx context.Context, write usecase.RecordWrite) (string, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.CreateRecord")
	defer span.End()

	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *gcfirestore.Transaction) error {
		if _, err := r.createCommitLogAndOwners(ctx, tx, write.Commit); err != nil {
			return err
		}

		parentURI, err := r.ensureParentRecordKeys(ctx, tx, write.Key)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		record := recordModel{
			DocumentID:    write.DocumentID,
			Owner:         write.Owner,
			Schema:        write.Schema,
			Distributions: write.Distributions,
			CreatedAt:     write.CreatedAt,
			CDate:         now,
		}
		if write.Policies != nil {
			record.Policies = *write.Policies
		}
		if write.Redirect != nil {
			record.Redirect = *write.Redirect
		}
		if err := tx.Set(r.doc(kindRecord, write.DocumentID), &record); err != nil {
			return err
		}

		var oldRecordKey recordKeyModel
		oldRecordID := ""
		recordRef := r.doc(kindRecordKey, write.Key)
		err = r.txGet(tx, recordRef, &oldRecordKey)
		if err == nil {
			oldRecordID = oldRecordKey.RecordID
		} else if !isNoSuchEntity(err) {
			return err
		}

		newRecordKey := recordKeyModel{
			URI:             write.Key,
			ParentURI:       parentURI,
			RecordID:        write.DocumentID,
			RecordOwner:     write.Owner,
			RecordSchema:    write.Schema,
			RecordCreatedAt: write.CreatedAt,
			Prefixes:        prefixesForURI(write.Key),
		}
		if write.Redirect != nil {
			newRecordKey.Redirect = *write.Redirect
		}
		if err := tx.Set(recordRef, &newRecordKey); err != nil {
			return err
		}

		if oldRecordID != "" && oldRecordID != write.DocumentID {
			var oldCommit commitLogModel
			oldCommitRef := r.doc(kindCommitLog, oldRecordID)
			err := r.txGet(tx, oldCommitRef, &oldCommit)
			if err == nil {
				oldCommit.GcCandidate = true
				if err := tx.Set(oldCommitRef, &oldCommit); err != nil {
					return err
				}
			} else if !isNoSuchEntity(err) {
				return err
			}
			if err := tx.Delete(r.doc(kindRecord, oldRecordID)); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	return write.Key, nil
}

func (r *RecordRepository) CreateAssociation(ctx context.Context, write usecase.AssociationWrite) error {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.CreateAssociation")
	defer span.End()

	if _, err := r.getRecordKey(ctx, write.TargetURI); err != nil {
		span.RecordError(err)
		return err
	}

	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *gcfirestore.Transaction) error {
		if _, err := r.createCommitLogAndOwners(ctx, tx, write.Commit); err != nil {
			return err
		}

		association := associationModel{
			TargetURI:  write.TargetURI,
			DocumentID: write.DocumentID,
			Owner:      write.Owner,
			Author:     write.Author,
			Schema:     write.Schema,
			Unique:     write.Unique,
			CreatedAt:  write.CreatedAt,
			CDate:      time.Now().UTC(),
		}
		if write.Variant != nil {
			association.Variant = *write.Variant
		}

		return tx.Set(r.doc(kindAssociation, write.Unique), &association)
	})
	if err != nil {
		span.RecordError(err)
	}
	return err
}

func (r *RecordRepository) Acknowledge(ctx context.Context, write usecase.AckWrite) (string, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.Acknowledge")
	defer span.End()

	err := r.saveAck(ctx, write)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	return write.ResultURI, nil
}

func (r *RecordRepository) UnAcknowledge(ctx context.Context, write usecase.AckWrite) error {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.UnAcknowledge")
	defer span.End()

	err := r.saveAck(ctx, write)
	if err != nil {
		span.RecordError(err)
	}
	return err
}

func (r *RecordRepository) saveAck(ctx context.Context, write usecase.AckWrite) error {
	err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *gcfirestore.Transaction) error {
		if _, err := r.createCommitLogAndOwners(ctx, tx, write.Commit); err != nil {
			return err
		}

		ref := r.doc(kindAck, compoundKey(write.From, write.To, write.Context))
		ack := ackModel{
			From:       write.From,
			To:         write.To,
			Context:    write.Context,
			DocumentID: write.DocumentID,
			Valid:      write.Valid,
			CreatedAt:  write.CreatedAt,
			CDate:      time.Now().UTC(),
		}

		var existing ackModel
		err := r.txGet(tx, ref, &existing)
		if err == nil {
			ack.CreatedAt = existing.CreatedAt
			ack.CDate = existing.CDate
		} else if !isNoSuchEntity(err) {
			return err
		}

		return tx.Set(ref, &ack)
	})
	return err
}

func (r *RecordRepository) GetSignedDocument(ctx context.Context, uri string) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.GetSignedDocument")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	switch parsed.Scheme {
	case "cckv":
		rk, err := r.getRecordKey(ctx, uri)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		if rk.RecordID == "" {
			err := domain.NotFoundError{Resource: uri}
			span.RecordError(err)
			return nil, err
		}
		commit, err := r.getCommit(ctx, rk.RecordID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		ccfs := concrnt.ComposeCCURI("ccfs", rk.RecordOwner, rk.RecordID)
		return signedDocumentFromCommit(commit, &uri, &ccfs)

	case "ccfs":
		commit, err := r.getCommit(ctx, parsed.CDID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		ccfs := uri
		var cckv *string
		if rk, ok, err := r.findRecordKeyByRecordID(ctx, parsed.CDID); err != nil {
			span.RecordError(err)
			return nil, err
		} else if ok {
			cckv = &rk.URI
		}
		return signedDocumentFromCommit(commit, cckv, &ccfs)

	default:
		err := fmt.Errorf("unsupported uri scheme: %s", parsed.Scheme)
		span.RecordError(err)
		return nil, err
	}
}

func (r *RecordRepository) GetHierarchicalRecordPolicies(ctx context.Context, uri string) ([]concrnt.Policy, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.GetHierarchicalRecordPolicies")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	cckv := uri
	if parsed.Scheme == "ccfs" {
		rk, ok, err := r.findRecordKeyByRecordID(ctx, parsed.CDID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		if !ok {
			err := domain.NotFoundError{Resource: uri}
			span.RecordError(err)
			return nil, err
		}
		cckv = rk.URI
	}

	hierarchy := []string{}
	currentURI := cckv
	for {
		hierarchy = append([]string{currentURI}, hierarchy...)
		parentURI, err := url.JoinPath(currentURI, "..")
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		if parentURI == currentURI {
			break
		}
		currentURI = parentURI
	}

	policies := []concrnt.Policy{}
	for i := len(hierarchy) - 1; i >= 0; i-- {
		rk, err := r.getRecordKey(ctx, hierarchy[i])
		if err != nil || rk.RecordID == "" {
			continue
		}
		record, err := r.getRecord(ctx, rk.RecordID)
		if err != nil {
			continue
		}
		if record.Policies == "" {
			continue
		}
		var policy concrnt.Policy
		if err := json.Unmarshal([]byte(record.Policies), &policy); err != nil {
			span.RecordError(err)
			return nil, err
		}
		policy.Source = hierarchy[i]
		virtualParents := append([]string(nil), record.Distributions...)
		policy.VirtualParents = &virtualParents
		policies = append(policies, policy)
	}

	return policies, nil
}

func (r *RecordRepository) GetDistributions(ctx context.Context, uri string) ([]string, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.GetDistributions")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	var recordID string
	switch parsed.Scheme {
	case "cckv":
		rk, err := r.getRecordKey(ctx, uri)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		recordID = rk.RecordID
	case "ccfs":
		recordID = parsed.CDID
	default:
		err := fmt.Errorf("unsupported uri scheme: %s", parsed.Scheme)
		span.RecordError(err)
		return nil, err
	}
	if recordID == "" {
		err := domain.NotFoundError{Resource: uri}
		span.RecordError(err)
		return nil, err
	}

	record, err := r.getRecord(ctx, recordID)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return append([]string(nil), record.Distributions...), nil
}

func (r *RecordRepository) Delete(ctx context.Context, sd concrnt.SignedDocument) (string, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.Delete")
	defer span.End()

	var doc concrnt.Document[schemas.Delete]
	if err := json.Unmarshal([]byte(sd.Document), &doc); err != nil {
		span.RecordError(err)
		return "", err
	}

	target := string(doc.Value)
	parsed, err := concrnt.ParseCCURI(target)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	var commitID string
	var targetURI *string
	switch parsed.Scheme {
	case "cckv":
		rk, err := r.getRecordKey(ctx, target)
		if err != nil {
			span.RecordError(err)
			return "", err
		}
		if rk.RecordID == "" {
			err := domain.NotFoundError{Resource: target}
			span.RecordError(err)
			return "", err
		}
		commitID = rk.RecordID
		targetURI = &target
	case "ccfs":
		commitID = parsed.CDID
	default:
		err := fmt.Errorf("unsupported uri scheme: %s", parsed.Scheme)
		span.RecordError(err)
		return "", err
	}

	if err := r.deleteCommitReadModels(ctx, commitID, targetURI); err != nil {
		span.RecordError(err)
		return "", err
	}
	return target, nil
}

func (r *RecordRepository) GetAssociatedRecords(ctx context.Context, targetURI, schema, variant, author string) ([]concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.GetAssociatedRecords")
	defer span.End()

	associations, _, err := r.allAssociations(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	filtered := make([]associationModel, 0, len(associations))
	for _, assoc := range associations {
		if assoc.TargetURI != targetURI {
			continue
		}
		if schema != "" && assoc.Schema != schema {
			continue
		}
		if variant != "" && assoc.Variant != variant {
			continue
		}
		if author != "" && assoc.Author != author {
			continue
		}
		filtered = append(filtered, assoc)
	}

	result := make([]concrnt.SignedDocument, 0, len(filtered))
	for _, assoc := range filtered {
		commit, err := r.getCommit(ctx, assoc.DocumentID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		ccfs := concrnt.ComposeCCURI("ccfs", assoc.Owner, assoc.DocumentID)
		sd, err := signedDocumentFromCommit(commit, nil, &ccfs)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		result = append(result, *sd)
	}
	return result, nil
}

func (r *RecordRepository) GetAssociatedRecordCountsBySchema(ctx context.Context, targetURI string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.GetAssociatedRecordCountsBySchema")
	defer span.End()

	associations, _, err := r.allAssociations(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	counts := map[string]int64{}
	for _, assoc := range associations {
		if assoc.TargetURI == targetURI {
			counts[assoc.Schema]++
		}
	}
	return counts, nil
}

func (r *RecordRepository) GetAssociatedRecordCountsByVariant(ctx context.Context, targetURI, schema string) (*utils.OrderedKVMap[int64], error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.GetAssociatedRecordCountsByVariant")
	defer span.End()

	associations, _, err := r.allAssociations(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	result := make(utils.OrderedKVMap[int64])
	for _, assoc := range associations {
		if assoc.TargetURI != targetURI {
			continue
		}
		if schema != "" && assoc.Schema != schema {
			continue
		}
		current := result[assoc.Variant]
		current.Value++
		order := assoc.CreatedAt.UnixNano()
		if current.Order == 0 || order < current.Order {
			current.Order = order
		}
		result[assoc.Variant] = current
	}
	return &result, nil
}

func (r *RecordRepository) QueryByPrefix(ctx context.Context, prefix, schema string, since, until *time.Time, limit int, order string) ([]concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.QueryByPrefix")
	defer span.End()

	var recordKeys []recordKeyModel
	recordKeys, refs, err := getAll[recordKeyModel](ctx, r.query(kindRecordKey).Where("prefixes", "array-contains", prefix))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if len(refs) == 0 {
		all, _, err := r.allRecordKeys(ctx)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		for _, rk := range all {
			if strings.HasPrefix(rk.URI, prefix) {
				recordKeys = append(recordKeys, rk)
			}
		}
	}

	recordKeys = filterRecordKeys(recordKeys, schema, since, until)
	sortRecordKeys(recordKeys, order)
	recordKeys = applyRecordKeyLimit(recordKeys, limit)
	return r.signedDocumentsFromRecordKeys(ctx, recordKeys)
}

func (r *RecordRepository) QueryByParent(ctx context.Context, parent, schema string, since, until *time.Time, limit int, order string) ([]concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.QueryByParent")
	defer span.End()

	var recordKeys []recordKeyModel
	recordKeys, _, err := getAll[recordKeyModel](ctx, r.query(kindRecordKey).Where("parentURI", "==", parent))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	recordKeys = filterRecordKeys(recordKeys, schema, since, until)
	sortRecordKeys(recordKeys, order)
	recordKeys = applyRecordKeyLimit(recordKeys, limit)
	return r.signedDocumentsFromRecordKeys(ctx, recordKeys)
}

func (r *RecordRepository) GetAcknowledgeRecords(ctx context.Context, from, to, contextName string) ([]concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.GetAcknowledgeRecords")
	defer span.End()

	acks, _, err := r.allAcks(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	filtered := make([]ackModel, 0, len(acks))
	for _, ack := range acks {
		if from != "" && ack.From != from {
			continue
		}
		if to != "" && ack.To != to {
			continue
		}
		if contextName != "" && ack.Context != contextName {
			continue
		}
		if (from != "" || to != "" || contextName != "") && !ack.Valid {
			continue
		}
		filtered = append(filtered, ack)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})

	result := make([]concrnt.SignedDocument, 0, len(filtered))
	for _, ack := range filtered {
		commit, err := r.getCommit(ctx, ack.DocumentID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		ccfs := concrnt.ComposeCCURI("ccfs", ack.From, ack.DocumentID)
		sd, err := signedDocumentFromCommit(commit, nil, &ccfs)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		result = append(result, *sd)
	}
	return result, nil
}

func (r *RecordRepository) GetAcknowledgeRecordCounts(ctx context.Context, from, to, contextName string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.GetAcknowledgeRecordCounts")
	defer span.End()

	acks, _, err := r.allAcks(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	counts := map[string]int64{}
	for _, ack := range acks {
		if !ack.Valid {
			continue
		}
		if from != "" && ack.From != from {
			continue
		}
		if to != "" && ack.To != to {
			continue
		}
		if contextName != "" && ack.Context != contextName {
			continue
		}
		counts[ack.Context]++
	}
	return counts, nil
}

func (r *RecordRepository) GetAllCommitLogs(ctx context.Context, owner string) ([]concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Record.GetAllCommitLogs")
	defer span.End()

	var commitOwners []commitOwnerModel
	commitOwners, _, err := getAll[commitOwnerModel](ctx, r.query(kindCommitOwner).Where("owner", "==", owner))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	sort.SliceStable(commitOwners, func(i, j int) bool {
		return commitOwners[i].CommitCreatedAt.Before(commitOwners[j].CommitCreatedAt)
	})

	result := make([]concrnt.SignedDocument, 0, len(commitOwners))
	for _, owner := range commitOwners {
		commit, err := r.getCommit(ctx, owner.CommitID)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		sd, err := signedDocumentFromCommit(commit, nil, nil)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		result = append(result, *sd)
	}
	return result, nil
}

func (r *RecordRepository) findRecordKeyByRecordID(ctx context.Context, recordID string) (recordKeyModel, bool, error) {
	recordKeys, _, err := r.allRecordKeys(ctx)
	if err != nil {
		return recordKeyModel{}, false, err
	}
	for _, rk := range recordKeys {
		if rk.RecordID == recordID {
			return rk, true, nil
		}
	}
	return recordKeyModel{}, false, nil
}

func (r *RecordRepository) signedDocumentsFromRecordKeys(ctx context.Context, recordKeys []recordKeyModel) ([]concrnt.SignedDocument, error) {
	result := make([]concrnt.SignedDocument, 0, len(recordKeys))
	for _, rk := range recordKeys {
		commit, err := r.getCommit(ctx, rk.RecordID)
		if err != nil {
			return nil, err
		}
		cckv := rk.URI
		ccfs := concrnt.ComposeCCURI("ccfs", rk.RecordOwner, rk.RecordID)
		sd, err := signedDocumentFromCommit(commit, &cckv, &ccfs)
		if err != nil {
			return nil, err
		}
		result = append(result, *sd)
	}
	return result, nil
}

func (r *RecordRepository) allAssociations(ctx context.Context) ([]associationModel, []*gcfirestore.DocumentRef, error) {
	return getAll[associationModel](ctx, r.query(kindAssociation))
}

func (r *RecordRepository) allAcks(ctx context.Context) ([]ackModel, []*gcfirestore.DocumentRef, error) {
	return getAll[ackModel](ctx, r.query(kindAck))
}

func (r *RecordRepository) deleteCommitReadModels(ctx context.Context, commitID string, targetURI *string) error {
	recordKeys, recordKeyKeys, err := r.allRecordKeys(ctx)
	if err != nil {
		return err
	}
	associations, associationKeys, err := r.allAssociations(ctx)
	if err != nil {
		return err
	}
	acks, ackKeys, err := r.allAcks(ctx)
	if err != nil {
		return err
	}
	var commitOwners []commitOwnerModel
	commitOwners, commitOwnerKeys, err := getAll[commitOwnerModel](ctx, r.query(kindCommitOwner))
	if err != nil {
		return err
	}

	err = r.client.RunTransaction(ctx, func(ctx context.Context, tx *gcfirestore.Transaction) error {
		if err := tx.Delete(r.doc(kindCommitLog, commitID)); err != nil {
			return err
		}
		if err := tx.Delete(r.doc(kindRecord, commitID)); err != nil {
			return err
		}

		for i, rk := range recordKeys {
			if rk.RecordID != commitID {
				continue
			}
			rk.RecordID = ""
			rk.RecordOwner = ""
			rk.RecordSchema = ""
			rk.RecordCreatedAt = time.Time{}
			rk.Redirect = ""
			if err := tx.Set(recordKeyKeys[i], &rk); err != nil {
				return err
			}
		}

		for i, assoc := range associations {
			deleteAssociation := assoc.DocumentID == commitID
			if targetURI != nil && assoc.TargetURI == *targetURI {
				deleteAssociation = true
			}
			if deleteAssociation {
				if err := tx.Delete(associationKeys[i]); err != nil {
					return err
				}
			}
		}

		for i, ack := range acks {
			if ack.DocumentID == commitID {
				if err := tx.Delete(ackKeys[i]); err != nil {
					return err
				}
			}
		}

		for i, owner := range commitOwners {
			if owner.CommitID == commitID {
				if err := tx.Delete(commitOwnerKeys[i]); err != nil {
					return err
				}
			}
		}

		return nil
	})
	return err
}

var _ usecase.RecordRepository = (*RecordRepository)(nil)
