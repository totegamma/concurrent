package abuse

import (
	"context"

	"github.com/concrnt/concrnt"
)

type Repository interface {
	CreateAbuseReport(ctx context.Context, report *concrnt.AbuseReport, reporter, reposterIP string) error
}

type Usecase struct {
	repository Repository
}

func New(repository Repository) *Usecase {
	return &Usecase{repository: repository}
}

func (uc *Usecase) ReportAbuse(ctx context.Context, report *concrnt.AbuseReport, reporter, reposterIP string) error {
	return uc.repository.CreateAbuseReport(ctx, report, reporter, reposterIP)
}
