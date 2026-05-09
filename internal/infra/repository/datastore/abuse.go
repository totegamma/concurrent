package datastore

import (
	"context"
	"time"

	gcdatastore "cloud.google.com/go/datastore"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/usecase"
)

type AbuseRepository struct {
	*store
}

func NewAbuseRepository(client *gcdatastore.Client, namespace string) usecase.AbuseRepository {
	return &AbuseRepository{store: newStore(client, namespace)}
}

func (r *AbuseRepository) CreateAbuseReport(ctx context.Context, report *concrnt.AbuseReport, reporter, reporterIP string) error {
	ctx, span := tracer.Start(ctx, "Repository.Datastore.Abuse.CreateAbuseReport")
	defer span.End()

	model := abuseReportModel{
		IP:        reporterIP,
		Reporter:  reporter,
		TargetURI: report.TargetURI,
		Body:      report.Body,
		CDate:     time.Now().UTC(),
	}
	if _, err := r.client.Put(ctx, r.incompleteKey(kindAbuseReport), &model); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

var _ usecase.AbuseRepository = (*AbuseRepository)(nil)
