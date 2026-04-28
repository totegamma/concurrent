package service

import (
	"context"
	"github.com/concrnt/concrnt"
)

type AbuseRepository interface {
	CreateAbuseReport(ctx context.Context, report *concrnt.AbuseReport, reporter, reposterIP string) error
}

type AbuseService struct {
	repository AbuseRepository
}

func NewAbuseService(repository AbuseRepository) *AbuseService {
	return &AbuseService{repository: repository}
}

func (s *AbuseService) ReportAbuse(ctx context.Context, report *concrnt.AbuseReport, reporter, reposterIP string) error {
	return s.repository.CreateAbuseReport(ctx, report, reporter, reposterIP)
}
