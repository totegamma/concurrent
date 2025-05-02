package job

import (
	"log"
	"testing"
	"time"

	"github.com/concrnt/concrnt/core"
	"github.com/concrnt/concrnt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

var repo Repository
var db *gorm.DB

func TestMain(m *testing.M) {
	log.Println("Test Start")

	var cleanup func()
	db, cleanup = testutil.CreateDB()
	defer cleanup()

	repo = NewRepository(db)

	m.Run()

	log.Println("Test End")
}

func TestJobRepository(t *testing.T) {
	ctx := t.Context()
	author1 := "con1author1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	author2 := "con1author2bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	job1 := core.Job{
		Author:    author1,
		Type:      "typeA",
		Payload:   `{"data":"payload1"}`,
		Scheduled: time.Now().Add(-time.Minute), // Scheduled in the past
		Status:    "pending",
	}

	job2 := core.Job{
		Author:    author1,
		Type:      "typeB",
		Payload:   `{"data":"payload2"}`,
		Scheduled: time.Now().Add(time.Hour), // Scheduled in the future
		Status:    "pending",
	}

	job3 := core.Job{
		Author:    author2,
		Type:      "typeA",
		Payload:   `{"data":"payload3"}`,
		Scheduled: time.Now().Add(-2 * time.Minute), // Scheduled in the past
		Status:    "pending",
	}

	var createdJob1, createdJob2, createdJob3 core.Job

	tests := []struct {
		name       string
		setup      func()
		operation  func() (any, error)
		assertions func(t *testing.T, result any, err error)
		cleanup    func()
	}{
		{
			name: "Enqueue Job 1",
			operation: func() (any, error) {
				j, err := repo.Enqueue(ctx, job1.Author, job1.Type, job1.Payload, job1.Scheduled)
				createdJob1 = j // Store for later use
				return j, err
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				j, ok := result.(core.Job)
				assert.True(t, ok)
				assert.NotEmpty(t, j.ID)
				assert.Equal(t, job1.Author, j.Author)
				assert.Equal(t, job1.Type, j.Type)
				assert.Equal(t, "pending", j.Status)
			},
		},
		{
			name: "Enqueue Job 2",
			operation: func() (any, error) {
				j, err := repo.Enqueue(ctx, job2.Author, job2.Type, job2.Payload, job2.Scheduled)
				createdJob2 = j
				return j, err
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Enqueue Job 3",
			operation: func() (any, error) {
				j, err := repo.Enqueue(ctx, job3.Author, job3.Type, job3.Payload, job3.Scheduled)
				createdJob3 = j
				return j, err
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "List Jobs for Author 1",
			operation: func() (any, error) {
				return repo.List(ctx, author1)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				jobs, ok := result.([]core.Job)
				assert.True(t, ok)
				assert.Len(t, jobs, 2) // job1 and job2
			},
		},
		{
			name: "Dequeue Job (should get job3 first)",
			operation: func() (any, error) {
				return repo.Dequeue(ctx)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				job, ok := result.(*core.Job)
				assert.True(t, ok)
				assert.NotNil(t, job)
				assert.Equal(t, createdJob3.ID, job.ID) // job3 was scheduled earliest
				assert.Equal(t, "running", job.Status)
				assert.NotEmpty(t, job.TraceID)
			},
		},
		{
			name: "Dequeue Job (should get job1 next)",
			operation: func() (any, error) {
				return repo.Dequeue(ctx)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				job, ok := result.(*core.Job)
				assert.True(t, ok)
				assert.NotNil(t, job)
				assert.Equal(t, createdJob1.ID, job.ID) // job1 was next earliest
				assert.Equal(t, "running", job.Status)
				assert.NotEmpty(t, job.TraceID)
			},
		},
		{
			name: "Dequeue Job (should get nothing - job2 is future)",
			operation: func() (any, error) {
				return repo.Dequeue(ctx)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.Error(t, err) // Expect gorm.ErrRecordNotFound
				assert.Nil(t, result)
			},
		},
		{
			name: "Complete Job 1",
			operation: func() (any, error) {
				return repo.Complete(ctx, createdJob1.ID, "completed", `{"output":"done"}`)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				job, ok := result.(core.Job)
				assert.True(t, ok)
				assert.Equal(t, "completed", job.Status)
				assert.Equal(t, `{"output":"done"}`, job.Result)
			},
		},
		{
			name: "Cancel Job 2",
			operation: func() (any, error) {
				return repo.Cancel(ctx, createdJob2.ID)
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				job, ok := result.(core.Job)
				assert.True(t, ok)
				assert.Equal(t, "canceled", job.Status)
			},
		},
		{
			name: "Clean Completed/Canceled Jobs (older than now)",
			operation: func() (any, error) {
				return repo.Clean(ctx, time.Now().Add(time.Minute)) // Clean jobs older than 1 minute from now
			},
			assertions: func(t *testing.T, result any, err error) {
				assert.NoError(t, err)
				jobs, ok := result.([]core.Job)
				assert.True(t, ok)
				// Should find job1 (completed, scheduled in past)
				// job2 is canceled but scheduled in future, so shouldn't match olderThan
				// job3 is running, so shouldn't match status
				assert.Len(t, jobs, 1)
				assert.Equal(t, createdJob1.ID, jobs[0].ID)
			},
		},
	}

	// Run tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			result, err := tt.operation()
			tt.assertions(t, result, err)
			if tt.cleanup != nil {
				tt.cleanup()
			}
		})
	}
}
