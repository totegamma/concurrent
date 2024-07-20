package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/codes"

	"github.com/totegamma/concurrent/core"
)

var (
	client = new(http.Client)
)

type Repository interface {
	Get(ctx context.Context, url string) (core.Policy, error)
}

type repository struct {
	rdb *redis.Client
}

func NewRepository(rdb *redis.Client) Repository {
	return &repository{rdb}
}

func (r *repository) Get(ctx context.Context, url string) (core.Policy, error) {
	ctx, span := tracer.Start(ctx, "Policy.Repository.Get")
	defer span.End()

	// check cache
	key := fmt.Sprintf("policy:%s", url)
	val, err := r.rdb.Get(ctx, key).Result()
	if err == nil {
		var policy core.Policy
		err = json.Unmarshal([]byte(val), &policy)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return core.Policy{}, err
		}
		return policy, nil
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return core.Policy{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return core.Policy{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		span.SetStatus(codes.Error, err.Error())
		return core.Policy{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	jsonStr, err := io.ReadAll(resp.Body)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return core.Policy{}, err
	}

	// cache policy
	var policyDoc core.PolicyDocument
	err = json.Unmarshal(jsonStr, &policyDoc)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return core.Policy{}, err
	}

	var policy core.Policy
	policy20240701, ok := policyDoc.Versions["2024-07-01"]
	if ok {
		span.AddEvent("use version 2024-07-01")
		policy = policy20240701
	} else {
		span.AddEvent("fallback to latest version")
		err = json.Unmarshal(jsonStr, &policy)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return core.Policy{}, err
		}
	}

	jsonStr, err = json.Marshal(policy)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return core.Policy{}, err
	}

	err = r.rdb.Set(ctx, key, jsonStr, 10*time.Minute).Err() // 10 minutes
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return core.Policy{}, err
	}

	return policy, nil
}
