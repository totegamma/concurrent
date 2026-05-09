package usecase

import (
	"context"
	"github.com/concrnt/concrnt"
)

type AbuseRepository interface {
	CreateAbuseReport(ctx context.Context, report *concrnt.AbuseReport, reporter, reposterIP string) error
}

type AbuseUsecase struct {
	repository AbuseRepository
}

func NewAbuseUsecase(repository AbuseRepository) *AbuseUsecase {
	return &AbuseUsecase{repository: repository}
}

func (uc *AbuseUsecase) ReportAbuse(ctx context.Context, report *concrnt.AbuseReport, reporter, reposterIP string) error {
	return uc.repository.CreateAbuseReport(ctx, report, reporter, reposterIP)
}
