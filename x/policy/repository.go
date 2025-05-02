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

	"github.com/concrnt/concrnt/core"
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

// Get retrieves a policy document from a URL.
// It uses Redis for caching and locking to prevent redundant fetches.
// If the cache is empty or the lock has expired, it fetches the policy from the URL,
// updates the cache, and sets a short-lived lock.
func (r *repository) Get(ctx context.Context, url string) (core.Policy, error) {
	ctx, span := tracer.Start(ctx, "Policy.Repository.Get")
	defer span.End()

	var cache *core.Policy = nil

	// check cache
	key := fmt.Sprintf("policy:%s", url)
	lockKey := fmt.Sprintf("lock:%s", key)

	val, err := r.rdb.Get(ctx, key).Result()
	if err == nil {
		var policy core.Policy
		err = json.Unmarshal([]byte(val), &policy)
		if err == nil { // Only set cache if unmarshal succeeds
			cache = &policy
		} else {
			// Log the cache unmarshal error but treat it as a cache miss
			span.RecordError(fmt.Errorf("failed to unmarshal cached policy for %s: %w", url, err))
			// cache remains nil, so fetch() will be called
		}
	}
	// If rdb.Get failed OR json.Unmarshal failed, cache is nil here

	fetch := func() (core.Policy, error) {
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
			err = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			span.SetStatus(codes.Error, err.Error())
			return core.Policy{}, err
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

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = r.rdb.Set(ctx, key, jsonStr, 0).Err()
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return core.Policy{}, err
		}

		err = r.rdb.Set(ctx, lockKey, "1", 5*time.Minute).Err()
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return core.Policy{}, err
		}

		return policy, nil
	}

	if cache == nil {
		return fetch()
	}

	// check lock
	exists, err := r.rdb.Exists(ctx, lockKey).Result()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return *cache, nil
	}

	if exists == 0 {
		go fetch()
	}

	return *cache, nil
}
