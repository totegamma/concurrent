package record

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/impl/interop"
	"github.com/concrnt/concrnt/internal/domain"
)

func (uc *Usecase) DumpCommitLogs(ctx context.Context) (string, error) {
	ctx, span := tracer.Start(ctx, "Usecase.Record.GetCommitlog")
	defer span.End()

	requester, ok := ctx.Value(interop.RequesterCtxKey).(domain.Entity)
	if !ok {
		err := errors.New("requester not found in context")
		span.RecordError(err)
		return "", err
	}

	commitLogs, err := uc.repo.GetAllCommitLogs(ctx, requester.ID)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	var result string
	for _, log := range commitLogs {
		line, err := json.Marshal(log)
		if err != nil {
			span.RecordError(err)
			return "", err
		}
		result += string(line) + "\n"
	}

	return result, nil
}

type ImportResult struct {
	Document string `json:"document,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (uc *Usecase) ImportCommitLogs(ctx context.Context, ip string, jsonl string) []ImportResult {
	ctx, span := tracer.Start(ctx, "Usecase.Record.ImportCommitLogs")
	defer span.End()

	results := []ImportResult{}

	lines := strings.SplitSeq(jsonl, "\n")
	for line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var sd concrnt.SignedDocument
		err := json.Unmarshal([]byte(line), &sd)
		if err != nil {
			span.RecordError(err)
			result := ImportResult{
				Document: line,
				Error:    fmt.Sprintf("failed to parse line as SignedDocument: %v", err),
			}
			results = append(results, result)
			continue
		}

		_, err = uc.Commit(ctx, ip, sd, domain.CommitModeLocalOnlyExecute)
		if err != nil {
			span.RecordError(err)
			result := ImportResult{
				Document: line,
				Error:    fmt.Sprintf("failed to commit document: %v", err),
			}
			results = append(results, result)
			continue
		}
	}

	return results
}
