package firestore

import (
	"context"
	"time"

	gcfirestore "cloud.google.com/go/firestore"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/usecase"
)

type AbuseRepository struct {
	*store
}

func NewAbuseRepository(client *gcfirestore.Client, namespace string) usecase.AbuseRepository {
	return &AbuseRepository{store: newStore(client, namespace)}
}

func (r *AbuseRepository) CreateAbuseReport(ctx context.Context, report *concrnt.AbuseReport, reporter, reporterIP string) error {
	ctx, span := tracer.Start(ctx, "Repository.Firestore.Abuse.CreateAbuseReport")
	defer span.End()

	model := abuseReportModel{
		IP:        reporterIP,
		Reporter:  reporter,
		TargetURI: report.TargetURI,
		Body:      report.Body,
		CDate:     time.Now().UTC(),
	}
	if _, err := r.newDoc(kindAbuseReport).Set(ctx, &model); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

var _ usecase.AbuseRepository = (*AbuseRepository)(nil)
