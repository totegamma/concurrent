package record

import (
	"context"
	"errors"
	"time"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
	"github.com/concrnt/concrnt/internal/utils"
)

// paginateWindow derives pagination cursors from rows fetched with limit+1:
// the peeked row past the window becomes next, the window head becomes prev.
// Cursors are computed before any read-access filtering so that clients can
// page past rows that get filtered out.
func paginateWindow(rows []QueryRow, limit int) ([]concrnt.SignedDocument, *time.Time, *time.Time) {
	var prev, next *time.Time
	if len(rows) > limit {
		next = &rows[limit].CreatedAt
		rows = rows[:limit]
	}
	if len(rows) > 0 {
		prev = &rows[0].CreatedAt
	}
	items := make([]concrnt.SignedDocument, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.Row)
	}
	return items, prev, next
}

func (uc *Usecase) GetAcknowledgeRecords(ctx context.Context, from, to, schema string, since, until *time.Time, limit int, order string) (concrnt.QueryResult, error) {
	rows, err := uc.repo.GetAcknowledgeRecords(ctx, from, to, schema, since, until, limit+1, order)
	if err != nil {
		return concrnt.QueryResult{}, err
	}
	items, prev, next := paginateWindow(rows, limit)
	return concrnt.QueryResult{Items: items, Prev: prev, Next: next}, nil
}

func (uc *Usecase) GetAcknowledgeRecordCounts(ctx context.Context, from, to, schema string) (map[string]int64, error) {
	return uc.repo.GetAcknowledgeRecordCounts(ctx, from, to, schema)
}

func (uc *Usecase) GetAssociatedRecords(ctx context.Context, targetURI, schema, variant, author string, since, until *time.Time, limit int, order string) (concrnt.QueryResult, error) {
	rows, err := uc.repo.GetAssociatedRecords(ctx, targetURI, schema, variant, author, since, until, limit+1, order)
	if err != nil {
		return concrnt.QueryResult{}, err
	}
	items, prev, next := paginateWindow(rows, limit)
	return concrnt.QueryResult{Items: items, Prev: prev, Next: next}, nil
}

func (uc *Usecase) GetAssociatedRecordCountsBySchema(ctx context.Context, targetURI string) (map[string]int64, error) {
	return uc.repo.GetAssociatedRecordCountsBySchema(ctx, targetURI)
}

func (uc *Usecase) GetAssociatedRecordCountsByVariant(ctx context.Context, targetURI, schema string) (*utils.OrderedKVMap[int64], error) {
	return uc.repo.GetAssociatedRecordCountsByVariant(ctx, targetURI, schema)
}

func (uc *Usecase) Query(
	ctx context.Context,
	prefix, parent, schema, author string,
	since, until *time.Time,
	limit int,
	order string,
) (concrnt.QueryResult, error) {
	var (
		rows []QueryRow
		err  error
	)

	if prefix != "" && parent != "" {
		return concrnt.QueryResult{}, errors.New("prefix and parent cannot be specified at the same time")
	}

	if prefix != "" {
		rows, err = uc.repo.QueryByPrefix(ctx, prefix, schema, author, since, until, limit+1, order)
	} else if parent != "" {
		rows, err = uc.repo.QueryByParent(ctx, parent, schema, author, since, until, limit+1, order)
	} else {
		return concrnt.QueryResult{}, errors.New("either prefix or parent must be specified")
	}

	if err != nil {
		return concrnt.QueryResult{}, err
	}

	items, prev, next := paginateWindow(rows, limit)

	filtered := make([]concrnt.SignedDocument, 0, len(items))
	for _, sd := range items {
		if sd.CCKV == nil {
			return concrnt.QueryResult{}, errors.New("queried record has no cckv")
		}

		err := uc.checkReadAccess(ctx, *sd.CCKV, sd)
		if err != nil {
			if errors.Is(err, domain.ErrPermissionDenied) {
				continue
			}
			return concrnt.QueryResult{}, err
		}

		filtered = append(filtered, sd)
	}

	return concrnt.QueryResult{Items: filtered, Prev: prev, Next: next}, nil
}
