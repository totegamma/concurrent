package policy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/concrnt/concrnt/core"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRepositoryGet(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)
	ctx := t.Context()

	// Setup Redis using dockertest
	db, cleanup := testutil.CreateRDB()
	defer cleanup()

	// Setup Repository with Real Redis Client
	repo := NewRepository(db)

	// Mock Policy Document for HTTP Server
	mockStatement := core.Statement{Condition: core.Expr{Constant: true}}
	expectedPolicy := core.Policy{
		Statements: map[string]core.Statement{"read": mockStatement, "write": mockStatement},
		Defaults:   map[string]bool{"read": true, "write": false},
	}
	policyDoc := core.PolicyDocument{
		Versions: map[string]core.Policy{
			"2024-07-01": expectedPolicy,
		},
	}
	policyDocBytes, _ := json.Marshal(policyDoc)
	// expectedPolicy is already defined above
	expectedPolicyBytes, _ := json.Marshal(expectedPolicy)

	// Mock HTTP Server (remains the same)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(policyDocBytes)
	}))
	defer server.Close()

	// --- Test Case: Cache Miss, Successful Fetch ---
	t.Run("CacheMiss_FetchSuccess", func(subTest *testing.T) {
		url := server.URL + "/policy_miss.json"
		key := fmt.Sprintf("policy:%s", url)
		lockKey := fmt.Sprintf("lock:%s", key)

		// Ensure cache is empty before the test
		db.Del(ctx, key, lockKey)

		// Call the Get method
		policy, err := repo.Get(ctx, url)

		// Assertions
		assert.NoError(err)                  // Use subTest's assert
		assert.Equal(expectedPolicy, policy) // Use subTest's assert

		// Verify cache and lock were set in Redis
		cachedVal, err := db.Get(ctx, key).Result()
		assert.NoError(err)                                  // Use subTest's assert
		assert.Equal(string(expectedPolicyBytes), cachedVal) // Use subTest's assert

		lockVal, err := db.Get(ctx, lockKey).Result()
		assert.NoError(err)
		assert.Equal("1", lockVal)
	})

	// --- Test Case: Cache Hit, Lock Exists ---
	t.Run("CacheHit_LockExists", func(subTest *testing.T) {
		url := server.URL + "/policy_hit_lock_exists.json"
		key := fmt.Sprintf("policy:%s", url)
		lockKey := fmt.Sprintf("lock:%s", key)

		// Pre-populate cache and lock
		err := db.Set(ctx, key, expectedPolicyBytes, 0).Err()
		assert.NoError(err)
		err = db.Set(ctx, lockKey, "1", 5*time.Minute).Err()
		assert.NoError(err)

		// Call the Get method
		policy, err := repo.Get(ctx, url)

		// Assertions
		assert.NoError(err) // Use subTest's assert
		assert.Equal(expectedPolicy, policy)

	})

	// --- Test Case: Cache Hit, Lock Missing (Background Fetch) ---
	t.Run("CacheHit_LockMissing_BackgroundFetch", func(subTest *testing.T) {
		url := server.URL + "/policy_hit_lock_missing.json"
		key := fmt.Sprintf("policy:%s", url)
		lockKey := fmt.Sprintf("lock:%s", key)

		// Pre-populate cache, ensure lock is missing
		err := db.Set(ctx, key, expectedPolicyBytes, 0).Err()
		assert.NoError(err)
		db.Del(ctx, lockKey)

		// Call the Get method
		policy, err := repo.Get(ctx, url)

		// Assertions for the immediate return
		assert.NoError(err) // Use subTest's assert
		assert.Equal(expectedPolicy, policy)

		// Allow time for the background goroutine to run
		time.Sleep(200 * time.Millisecond)

		// Verify lock was set by the background fetch
		lockVal, err := db.Get(ctx, lockKey).Result()
		assert.NoError(err, "Background fetch should set the lock") // Use subTest's assert
		assert.Equal("1", lockVal)                                  // Use subTest's assert

		// Optionally: Verify cache was potentially updated (though content is same here)
		cachedVal, err := db.Get(ctx, key).Result()
		assert.NoError(err) // Use subTest's assert
		assert.Equal(string(expectedPolicyBytes), cachedVal)
	})

	// --- Test Case: Fetch Failure (HTTP Error) ---
	t.Run("FetchFailure_ServerError", func(subTest *testing.T) {
		errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer errorServer.Close()

		url := errorServer.URL + "/policy_fetch_error.json"
		key := fmt.Sprintf("policy:%s", url)
		lockKey := fmt.Sprintf("lock:%s", key)

		// Ensure cache is empty
		db.Del(ctx, key, lockKey)

		// Call the Get method
		policy, getErr := repo.Get(ctx, url) // Renamed err to getErr

		// Assertions
		assert.Error(getErr)
		assert.ErrorContains(getErr, "unexpected status code: 500")
		assert.Empty(policy)

		// Verify cache and lock were NOT set
		cacheErr := db.Get(ctx, key).Err() // Use different var name
		assert.Error(cacheErr)
		// assert.ErrorIs(t, cacheErr, redis.Nil)

		lockErr := db.Get(ctx, lockKey).Err() // Use different var name
		assert.Error(lockErr)
		// assert.ErrorIs(t, lockErr, redis.Nil)
	})

	// --- Test Case: Fetch Success, Invalid JSON Response ---
	t.Run("FetchSuccess_InvalidJSON", func(subTest *testing.T) {
		invalidJSONServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"invalid json`))
		}))
		defer invalidJSONServer.Close()

		url := invalidJSONServer.URL + "/policy_invalid_json.json"
		key := fmt.Sprintf("policy:%s", url)
		lockKey := fmt.Sprintf("lock:%s", key)

		// Ensure cache is empty
		db.Del(ctx, key, lockKey)

		// Call the Get method
		policy, getErr := repo.Get(ctx, url) // Renamed err to getErr

		// Assertions
		assert.Error(getErr)
		assert.ErrorContains(getErr, "unexpected end of JSON input", "Error should indicate invalid JSON")
		assert.Empty(policy)

		// Verify cache and lock were NOT set
		cacheErr := db.Get(ctx, key).Err() // Use different var name
		assert.Error(cacheErr)
		// assert.ErrorIs(t, cacheErr, redis.Nil)

		lockErr := db.Get(ctx, lockKey).Err() // Use different var name
		assert.Error(lockErr)
		// assert.ErrorIs(t, lockErr, redis.Nil)
	})

	// --- Test Case: Fetch Success, Version Missing (Fallback) ---
	t.Run("FetchSuccess_VersionMissing_Fallback", func(subTest *testing.T) {
		// Mock Policy Document without the specific version
		fallbackPolicy := core.Policy{
			Statements: map[string]core.Statement{"fallback": mockStatement},
			Defaults:   map[string]bool{"fallback": true},
		}
		fallbackPolicyBytes, _ := json.Marshal(fallbackPolicy)

		fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write(fallbackPolicyBytes)
		}))
		defer fallbackServer.Close()

		url := fallbackServer.URL + "/policy_fallback.json"
		key := fmt.Sprintf("policy:%s", url)
		lockKey := fmt.Sprintf("lock:%s", key)

		// Ensure cache is empty
		db.Del(ctx, key, lockKey)

		// Call the Get method
		policy, err := repo.Get(ctx, url)

		// Assertions
		assert.NoError(err)                  // Use subTest's assert
		assert.Equal(fallbackPolicy, policy) // Use subTest's assert

		// Verify cache and lock were set with fallback data
		cachedVal, err := db.Get(ctx, key).Result()
		assert.NoError(err)                                  // Use subTest's assert
		assert.Equal(string(fallbackPolicyBytes), cachedVal) // Use subTest's assert

		lockVal, err := db.Get(ctx, lockKey).Result()
		assert.NoError(err) // Use subTest's assert
		assert.Equal("1", lockVal)
	})

	// --- Test Case: Invalid JSON in Cache ---
	t.Run("CacheHit_InvalidJSON", func(subTest *testing.T) {
		url := server.URL + "/policy_invalid_cache.json"
		key := fmt.Sprintf("policy:%s", url)
		lockKey := fmt.Sprintf("lock:%s", key)

		// Pre-populate cache with invalid JSON, ensure lock is missing
		err := db.Set(ctx, key, `{"invalid cache json`, 0).Err()
		assert.NoError(err)
		db.Del(ctx, lockKey)

		// Call the Get method
		policy, err := repo.Get(ctx, url)

		// Assertions
		assert.NoError(err)                  // Use subTest's assert
		assert.Equal(expectedPolicy, policy) // Use subTest's assert

		// Verify cache and lock were updated correctly after fetch
		cachedVal, err := db.Get(ctx, key).Result()
		assert.NoError(err) // Use subTest's assert
		assert.Equal(string(expectedPolicyBytes), cachedVal)

		lockVal, err := db.Get(ctx, lockKey).Result()
		assert.NoError(err)
		assert.Equal("1", lockVal)
	})

}
