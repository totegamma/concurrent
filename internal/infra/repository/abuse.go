package repository

import (
	"context"
	"gorm.io/gorm"

	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/internal/infra/database/models"
)

type AbuseRepository struct {
	db *gorm.DB
}

func NewAbuseRepository(db *gorm.DB) *AbuseRepository {
	return &AbuseRepository{db: db}
}

func (r *AbuseRepository) CreateAbuseReport(ctx context.Context, report *concrnt.AbuseReport, reporter, reposterIP string) error {
	ctx, span := tracer.Start(ctx, "AbuseRepository.CreateAbuseReport")
	defer span.End()

	abuseReport := &models.AbuseReport{
		IP:        reposterIP,
		Reporter:  reporter,
		TargetURI: report.TargetURI,
		Body:      report.Body,
	}

	if err := r.db.WithContext(ctx).Create(abuseReport).Error; err != nil {
		return err
	}
	return nil
}
