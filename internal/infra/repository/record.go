package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/infra/database/models"
	"github.com/concrnt/concrnt/internal/usecase"
	"github.com/concrnt/concrnt/internal/utils"
	"github.com/concrnt/concrnt/schemas"
)

type RecordRepository struct {
	db *gorm.DB
}

func NewRecordRepository(db *gorm.DB) *RecordRepository {
	return &RecordRepository{db: db}
}

func (r *RecordRepository) CreateRecord(ctx context.Context, write usecase.RecordWrite) (string, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.CreateRecord")
	defer span.End()

	record := models.Record{
		DocumentID:    write.DocumentID,
		Owner:         write.Owner,
		Redirect:      write.Redirect,
		Schema:        write.Schema,
		Policies:      write.Policies,
		Distributions: write.Distributions,
		CreatedAt:     write.CreatedAt,
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := createCommitLogAndOwners(tx, write.Commit); err != nil {
			return err
		}

		if err := tx.Clauses(clause.OnConflict{
			DoNothing: true,
		}).Create(&record).Error; err != nil {
			span.RecordError(err)
			return err
		}

		// update RecordKey
		var oldRecordKey models.RecordKey
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("uri = ?", write.Key).
			Take(&oldRecordKey).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}

		// ParentのRecordKeyを探す
		parentRK, err := getOrCreateParentRecordKey(ctx, tx, write.Key)
		if err != nil {
			return err
		}

		var pid *int64
		if parentRK != nil {
			pid = &parentRK.ID
		}

		// RecordKeyを作る
		rk := models.RecordKey{
			URI:      write.Key,
			ParentID: pid,
			RecordID: &write.DocumentID,
		}

		err = tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "uri"}},
			DoUpdates: clause.Assignments(map[string]any{"record_id": write.DocumentID}),
		}).Create(&rk).Error
		if err != nil {
			return err
		}

		// 古いRecordKeyが指していたCommitのGCフラグを立て、Recordは消す
		if oldRecordKey.RecordID != nil && *oldRecordKey.RecordID != write.DocumentID {
			if err := tx.Model(&models.CommitLog{}).
				Where("id = ?", oldRecordKey.RecordID).
				Update("gc_candidate", true).Error; err != nil {
				return err
			}
			if err := tx.Delete(&models.Record{}, "document_id = ?", oldRecordKey.RecordID).Error; err != nil {
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
	ctx, span := tracer.Start(ctx, "Repository.Record.CreateAssociation")
	defer span.End()

	targetRK, err := GetRecordKeyByURI(ctx, r.db, write.TargetURI)
	if err != nil {
		span.RecordError(err)
		return err
	}

	association := models.Association{
		TargetID:   targetRK.ID,
		DocumentID: write.DocumentID,
		Unique:     write.Unique,

		Owner:     write.Owner,
		Author:    write.Author,
		Variant:   write.Variant,
		Schema:    write.Schema,
		CreatedAt: write.CreatedAt,
	}

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := createCommitLogAndOwners(tx, write.Commit); err != nil {
			return err
		}

		if err := tx.Create(&association).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		span.RecordError(err)
	}

	return err
}

func (r *RecordRepository) Acknowledge(ctx context.Context, write usecase.AckWrite) (string, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.Acknowledge")
	defer span.End()

	err := r.saveAck(ctx, write)
	if err != nil {
		span.RecordError(err)
	}

	return write.ResultURI, err
}

func (r *RecordRepository) UnAcknowledge(ctx context.Context, write usecase.AckWrite) error {
	ctx, span := tracer.Start(ctx, "Repository.Record.Unacknowledge")
	defer span.End()

	err := r.saveAck(ctx, write)
	if err != nil {
		span.RecordError(err)
	}

	return err
}

func (r *RecordRepository) saveAck(ctx context.Context, write usecase.AckWrite) error {
	ack := models.Ack{
		From:       write.From,
		To:         write.To,
		Context:    write.Context,
		DocumentID: write.DocumentID,
		Valid:      write.Valid,
		CreatedAt:  write.CreatedAt,
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := createCommitLogAndOwners(tx, write.Commit); err != nil {
			return err
		}

		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "from"}, {Name: "to"}, {Name: "context"}},
			DoUpdates: clause.Assignments(map[string]any{"valid": write.Valid, "document_id": write.DocumentID}),
		}).Create(&ack).Error
		return err
	})
}

func createCommitLogAndOwners(tx *gorm.DB, commit usecase.CommitWrite) error {
	commitLog := models.CommitLog{
		ID:       commit.ID,
		IP:       commit.IP,
		Document: commit.Document,
		Proof:    commit.Proof,
	}

	if err := tx.Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&commitLog).Error; err != nil {
		return err
	}

	for _, owner := range commit.Owners {
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "commit_log_id"}, {Name: "owner"}},
			DoNothing: true,
		}).Create(&models.CommitOwner{
			CommitLogID: commit.ID,
			Owner:       owner,
		}).Error
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *RecordRepository) GetHierarchicalRecordPolicies(ctx context.Context, uri string) ([]concrnt.Policy, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetHierarchicalRecordPolicies")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	cckv := uri
	if parsed.Scheme == "ccfs" {
		var rk models.RecordKey
		err = r.db.WithContext(ctx).
			Joins("JOIN records r ON r.document_id = record_keys.record_id").
			Where("r.document_id = ?", parsed.CDID).
			Take(&rk).Error
		if err != nil {
			span.RecordError(err)
			return nil, errors.Join(domain.NotFoundError{Resource: uri}, err)
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

	type tuple struct {
		Uri           string         `gorm:"column:uri"`
		Policy        *string        `gorm:"column:policy"`
		Distributions pq.StringArray `gorm:"column:distributions;type:text[]"`
	}

	var entries []tuple
	err = r.db.WithContext(ctx).
		Model(&models.Record{}).
		Select("rk.uri AS uri, records.policies AS policy, records.distributions AS distributions").
		Joins("JOIN record_keys rk ON rk.record_id = records.document_id").
		Where("rk.uri IN ?", hierarchy).
		Find(&entries).Error
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	tupleMap := make(map[string]tuple)
	for _, res := range entries {
		if res.Policy != nil {
			tupleMap[res.Uri] = res
		}
	}

	policies := []concrnt.Policy{}
	for i := len(hierarchy) - 1; i >= 0; i-- {
		uri := hierarchy[i]
		if t, ok := tupleMap[uri]; ok {
			var policyDoc concrnt.Policy
			err := json.Unmarshal([]byte(*t.Policy), &policyDoc)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			policyDoc.Source = uri
			// convert pq.StringArray to []string
			virtualParents := []string{}
			for _, dist := range t.Distributions {
				virtualParents = append(virtualParents, dist)
			}
			policyDoc.VirtualParents = &virtualParents

			policies = append(policies, policyDoc)
		}
	}

	return policies, nil
}

func (r *RecordRepository) GetSignedDocument(ctx context.Context, uri string) (*concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetSignedDocument")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	switch parsed.Scheme {
	case "cckv":
		var recordKey models.RecordKey
		err = r.db.WithContext(ctx).
			Preload("Record").
			Preload("Record.Document").
			Where("uri = ?", uri).
			Take(&recordKey).Error
		if err != nil {
			span.RecordError(errors.Join(errors.New("failed to query recordkey from cckv"), err))
			return nil, errors.Join(domain.NotFoundError{Resource: uri}, err)
		}

		if recordKey.RecordID == nil {
			err := fmt.Errorf("record key found but record is nil for uri: %s", uri)
			span.RecordError(err)
			return nil, errors.Join(domain.NotFoundError{Resource: uri}, err)
		}

		var proof concrnt.Proof
		err = json.Unmarshal([]byte(recordKey.Record.Document.Proof), &proof)
		if err != nil {
			span.RecordError(errors.Join(errors.New("failed to unmarshal proof from recordkey"), err))
			return nil, err
		}

		ccfs := concrnt.ComposeCCURI("ccfs", parsed.Owner, parsed.CDID)

		return &concrnt.SignedDocument{
			CCKV:     &uri,
			CCFS:     &ccfs,
			Document: recordKey.Record.Document.Document,
			Proof:    proof,
		}, nil

	case "ccfs":
		var commitLog models.CommitLog
		err = r.db.WithContext(ctx).
			Where("id = ?", parsed.CDID).
			Take(&commitLog).Error
		if err != nil {
			span.RecordError(errors.Join(errors.New("failed to query commitlog from ccfs"), err))
			return nil, errors.Join(domain.NotFoundError{Resource: uri}, err)
		}

		var cckv *string = nil
		var recordKey models.RecordKey
		err = r.db.WithContext(ctx).
			Preload("Record").
			Where("record_id = ?", commitLog.ID).
			Take(&models.RecordKey{}).Error
		if err == nil {
			cckv = &recordKey.URI
		}

		var proof concrnt.Proof
		err = json.Unmarshal([]byte(commitLog.Proof), &proof)
		if err != nil {
			span.RecordError(errors.Join(errors.New("failed to unmarshal proof from commitlog"), err))
			return nil, err
		}

		return &concrnt.SignedDocument{
			CCFS:     &uri,
			CCKV:     cckv,
			Document: commitLog.Document,
			Proof:    proof,
		}, nil
	default:
		err := fmt.Errorf("unsupported uri scheme: %s", parsed.Scheme)
		span.RecordError(err)
		return nil, err
	}
}

func (r *RecordRepository) Delete(ctx context.Context, sd concrnt.SignedDocument) (string, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.Delete")
	defer span.End()

	var doc concrnt.Document[schemas.Delete]
	err := json.Unmarshal([]byte(sd.Document), &doc)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	target := string(doc.Value)
	parsed, err := concrnt.ParseCCURI(target)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	switch parsed.Scheme {
	case "cckv":
		var recordKey models.RecordKey
		err = r.db.WithContext(ctx).Preload("Record").
			Preload("Record.Document").
			Where("uri = ?", target).
			Take(&recordKey).Error
		if err != nil {
			span.RecordError(err)
			return "", err
		}

		id := recordKey.Record.DocumentID
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Delete(&models.CommitLog{}, "id = ?", id).Error; err != nil {
				span.RecordError(err)
				return err
			}
			return nil
		})
		if err != nil {
			span.RecordError(err)
			return "", err
		}

		return target, nil

	case "ccfs":
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Delete(&models.CommitLog{}, "id = ?", parsed.CDID).Error; err != nil {
				span.RecordError(err)
				return err
			}
			return nil
		})
		if err != nil {
			span.RecordError(err)
			return "", err
		}
		return target, nil
	default:
		err := fmt.Errorf("unsupported uri scheme: %s", parsed.Scheme)
		span.RecordError(err)
		return "", err
	}
}

func getOrCreateParentRecordKey(ctx context.Context, db *gorm.DB, uri string) (*models.RecordKey, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.getOrCreateParentRecordKey")
	defer span.End()

	parentURI, err := url.JoinPath(uri, "..")
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	parsed, err := url.Parse(parentURI)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if parsed.Path == "" || parsed.Path == "/" {
		return nil, nil
	}

	parentRK, err := GetRecordKeyByURI(ctx, db, parentURI)
	if err != nil {
		if errors.Is(err, domain.NotFoundError{}) {

			parentID, err := getOrCreateParentRecordKey(ctx, db, parentURI)
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			var pid *int64
			if parentID != nil {
				pid = &parentID.ID
			}

			newRecordKey := models.RecordKey{
				URI:      parentURI,
				RecordID: nil,
				ParentID: pid,
			}

			err = db.WithContext(ctx).Create(&newRecordKey).Error
			if err != nil {
				span.RecordError(err)
				return nil, err
			}

			return &newRecordKey, nil

		} else {
			span.RecordError(err)
			return nil, err
		}
	}

	return parentRK, nil
}

func GetRecordKeyByURI(ctx context.Context, db *gorm.DB, uri string) (*models.RecordKey, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetRecordKeyByURI")
	defer span.End()

	var recordKey models.RecordKey
	err := db.WithContext(ctx).
		Where("uri = ?", uri).
		Take(&recordKey).Error
	if err != nil {
		span.RecordError(err)
		return nil, domain.NotFoundError{Resource: "record key"}
	}

	return &recordKey, nil
}

func (r *RecordRepository) GetDistributions(ctx context.Context, uri string) ([]string, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetDistributions")
	defer span.End()

	parsed, err := concrnt.ParseCCURI(uri)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	switch parsed.Scheme {
	case "cckv":
		var recordKey models.RecordKey
		err = r.db.WithContext(ctx).Preload("Record").
			Preload("Record.Document").
			Where("uri = ?", uri).
			Take(&recordKey).Error
		if err != nil {
			span.RecordError(err)
			return nil, errors.Join(domain.NotFoundError{Resource: uri}, err)
		}
		return recordKey.Record.Distributions, nil
	case "ccfs":
		var record models.Record
		err = r.db.WithContext(ctx).
			Where("document_id = ?", parsed.CDID).
			Take(&record).Error
		if err != nil {
			span.RecordError(err)
			return nil, errors.Join(domain.NotFoundError{Resource: uri}, err)
		}
		return record.Distributions, nil
	default:
		err := fmt.Errorf("unsupported uri scheme: %s", parsed.Scheme)
		span.RecordError(err)
		return nil, err
	}
}

func (r *RecordRepository) GetAssociatedRecords(
	ctx context.Context,
	targetURI, schema, variant, author string,
) ([]concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetAssociatedRecords")
	defer span.End()

	var associations []models.Association

	query := r.db.WithContext(ctx).
		Model(&models.Association{}).
		Preload("Document").
		Joins("JOIN record_keys rk ON rk.id = associations.target_id").
		Where("rk.uri = ?", targetURI)

	if schema != "" {
		query = query.Where("associations.schema = ?", schema)
	}
	if variant != "" {
		query = query.Where("associations.variant = ?", variant)
	}
	if author != "" {
		query = query.Where("associations.author = ?", author)
	}

	if err := query.Find(&associations).Error; err != nil {
		return nil, err
	}

	sds := make([]concrnt.SignedDocument, len(associations))
	for i, assoc := range associations {
		var proof concrnt.Proof
		err := json.Unmarshal([]byte(assoc.Document.Proof), &proof)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		ccfs := concrnt.ComposeCCURI("ccfs", assoc.Owner, assoc.DocumentID)

		sds[i] = concrnt.SignedDocument{
			CCFS:     &ccfs,
			Document: assoc.Document.Document,
			Proof:    proof,
		}
	}

	return sds, nil
}

func (r *RecordRepository) GetAssociatedRecordCountsBySchema(ctx context.Context, targetURI string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetAssociatedRecordCountsBySchema")
	defer span.End()

	var counts []struct {
		Schema string
		Count  int64
	}

	err := r.db.WithContext(ctx).
		Model(&models.Association{}).
		Select("schema, COUNT(*) AS count").
		Joins("JOIN record_keys rk ON rk.id = associations.target_id").
		Where("rk.uri = ?", targetURI).
		Group("schema").
		Scan(&counts).Error

	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	result := make(map[string]int64)
	for _, c := range counts {
		result[c.Schema] = c.Count
	}

	return result, nil
}

func (r *RecordRepository) GetAssociatedRecordCountsByVariant(ctx context.Context, targetURI, schema string) (*utils.OrderedKVMap[int64], error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetAssociatedRecordCountsByVariant")
	defer span.End()

	var counts []struct {
		Variant  string
		Count    int64
		MinCDate time.Time
	}

	err := r.db.WithContext(ctx).
		Model(&models.Association{}).
		Select("variant, COUNT(*) AS count, MIN(created_at) AS min_created_at").
		Joins("JOIN record_keys rk ON rk.id = associations.target_id").
		Where("rk.uri = ?", targetURI).
		Group("variant").
		Order("min_created_at ASC").
		Scan(&counts).Error
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	result := make(utils.OrderedKVMap[int64])
	for _, c := range counts {
		result[c.Variant] = utils.OrderedKV[int64]{
			Value: c.Count,
			Order: c.MinCDate.UnixNano(),
		}
	}

	return &result, nil
}

func (r *RecordRepository) QueryByPrefix(
	ctx context.Context,
	prefix, schema string,
	since, until *time.Time,
	limit int,
	order string,
) ([]concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.Query")
	defer span.End()

	var rks []models.RecordKey

	query := r.db.WithContext(ctx).
		Model(&models.RecordKey{}).
		Joins("JOIN records r ON r.document_id = record_keys.record_id").
		Where("uri LIKE ?", prefix+"%")

	if schema != "" {
		query = query.Where("r.schema = ?", schema)
	}
	if since != nil {
		query = query.Where("r.created_at >= ?", *since)
	}
	if until != nil {
		query = query.Where("r.created_at <= ?", *until)
	}

	if order == "desc" {
		query = query.Order("r.created_at DESC")
	} else {
		query = query.Order("r.created_at ASC")
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Preload("Record.Document").Find(&rks).Error; err != nil {
		span.RecordError(err)
		return nil, err
	}

	sds := make([]concrnt.SignedDocument, 0, len(rks))
	for _, rk := range rks {
		var proof concrnt.Proof
		err := json.Unmarshal([]byte(rk.Record.Document.Proof), &proof)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		ccfs := concrnt.ComposeCCURI("ccfs", rk.Record.Owner, rk.Record.DocumentID)

		sds = append(sds, concrnt.SignedDocument{
			CCKV:     &rk.URI,
			CCFS:     &ccfs,
			Document: rk.Record.Document.Document,
			Proof:    proof,
		})
	}

	return sds, nil
}

func (r *RecordRepository) QueryByParent(
	ctx context.Context,
	parent, schema string,
	since, until *time.Time,
	limit int,
	order string,
) ([]concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.QueryByParent")
	defer span.End()

	var rks []models.RecordKey

	query := r.db.WithContext(ctx).
		Model(&models.RecordKey{}).
		Joins("JOIN records r ON r.document_id = record_keys.record_id").
		Where("parent_id = (SELECT id FROM record_keys WHERE uri = ?)", parent)

	if schema != "" {
		query = query.Where("r.schema = ?", schema)
	}
	if since != nil {
		query = query.Where("r.created_at >= ?", *since)
	}
	if until != nil {
		query = query.Where("r.created_at <= ?", *until)
	}

	if order == "desc" {
		query = query.Order("r.created_at DESC")
	} else {
		query = query.Order("r.created_at ASC")
	}

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Preload("Record.Document").Find(&rks).Error; err != nil {
		span.RecordError(err)
		return nil, err
	}

	sds := make([]concrnt.SignedDocument, 0, len(rks))
	for _, rk := range rks {
		var proof concrnt.Proof
		err := json.Unmarshal([]byte(rk.Record.Document.Proof), &proof)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		ccfs := concrnt.ComposeCCURI("ccfs", rk.Record.Owner, rk.Record.DocumentID)

		sds = append(sds, concrnt.SignedDocument{
			CCKV:     &rk.URI,
			CCFS:     &ccfs,
			Document: rk.Record.Document.Document,
			Proof:    proof,
		})
	}

	return sds, nil
}

func (r *RecordRepository) GetAcknowledgeRecords(ctx context.Context, from, to, context string) ([]concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetAcknowledgeRecords")
	defer span.End()

	var acks []models.Ack
	query := r.db.WithContext(ctx).
		Model(&models.Ack{}).
		Preload("Document")

	if from != "" {
		query = query.Where("acks.from = ?", from)
	}
	if to != "" {
		query = query.Where("acks.to = ?", to)
	}
	if context != "" {
		query = query.Where("acks.context = ?", context)
	}

	if from != "" || to != "" || context != "" {
		query = query.Where("acks.valid = ?", true)
	}

	err := query.Order("acks.created_at ASC").Find(&acks).Error
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	result := make([]concrnt.SignedDocument, len(acks))
	for i, ack := range acks {
		var proof concrnt.Proof
		err := json.Unmarshal([]byte(ack.Document.Proof), &proof)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		ccfs := concrnt.ComposeCCURI("ccfs", ack.From, ack.DocumentID)

		result[i] = concrnt.SignedDocument{
			CCFS:     &ccfs,
			Document: ack.Document.Document,
			Proof:    proof,
		}
	}

	return result, nil
}

func (r *RecordRepository) GetAcknowledgeRecordCounts(ctx context.Context, from, to, context string) (map[string]int64, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetAcknowledgeRecordCounts")
	defer span.End()

	type result struct {
		Context string
		Count   int64
	}

	var results []result

	query := r.db.WithContext(ctx).
		Model(&models.Ack{}).
		Select("context, COUNT(*) AS count").
		Where("valid = ?", true)

	if from != "" {
		query = query.Where("acks.from = ?", from)
	}
	if to != "" {
		query = query.Where("acks.to = ?", to)
	}
	if context != "" {
		query = query.Where("acks.context = ?", context)
	}

	err := query.Group("context").Scan(&results).Error
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	counts := make(map[string]int64)
	for _, r := range results {
		counts[r.Context] = r.Count
	}

	return counts, nil
}

func (r *RecordRepository) GetAllCommitLogs(ctx context.Context, owner string) ([]concrnt.SignedDocument, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetAllCommitLogs")
	defer span.End()

	var commitLogs []models.CommitLog
	err := r.db.WithContext(ctx).
		Joins("JOIN commit_owners co ON co.commit_log_id = commit_logs.id").
		Where("co.owner = ?", owner).
		Order("commit_logs.created_at ASC").
		Find(&commitLogs).Error
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	sds := make([]concrnt.SignedDocument, len(commitLogs))
	for i, cl := range commitLogs {
		var proof concrnt.Proof
		err := json.Unmarshal([]byte(cl.Proof), &proof)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		sds[i] = concrnt.SignedDocument{
			Document: cl.Document,
			Proof:    proof,
		}
	}

	return sds, nil
}

/*
func (r *RecordRepository) GetReferencesByParent(
	ctx context.Context,
	parentURI string,
) ([]string, error) {
	ctx, span := tracer.Start(ctx, "Repository.Record.GetReferencesByParent")
	defer span.End()

	var records []models.Record

	err := r.db.WithContext(ctx).
		Joins("JOIN record_keys rk ON rk.record_id = records.document_id").
		Where("rk.parent_id = (SELECT id FROM record_keys WHERE uri = ?)", parentURI).
		Where("records.schema = ?", schemas.ReferenceURL).
		Find(&records).Error
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	references := []string{}
	for _, record := range records {
		if record.Redirect != nil {
			references = append(references, *record.Redirect)
		}
	}

	return references, nil
}
*/
