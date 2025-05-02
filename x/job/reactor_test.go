package job

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/concrnt/concrnt/core"
	mock_core "github.com/concrnt/concrnt/core/mock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestNewReactor(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mock_core.NewMockStoreService(ctrl)
	mockJob := mock_core.NewMockJobService(ctrl)

	r := NewReactor(mockStore, mockJob)
	assert.NotNil(t, r)
	assert.Implements(t, (*Reactor)(nil), r)
}

// TestReactor_dispatchJob tests the synchronous execution of a job function
// and the subsequent call to Complete.
func TestReactor_dispatchJob(t *testing.T) {
	jobHello := func(ctx context.Context, j *core.Job) (string, error) {
		return "hello!", nil
	}

	jobCleanSuccess := func(ctx context.Context, j *core.Job) (string, error) {
		// Simulate successful clean
		return "cleaned", nil
	}

	jobCleanFailure := func(ctx context.Context, j *core.Job) (string, error) {
		// Simulate failed clean
		return "cleaning error", errors.New("clean failed")
	}

	tests := []struct {
		name           string
		job            *core.Job
		jobFunc        func(ctx context.Context, j *core.Job) (string, error)
		mockComplete   func(mockJob *mock_core.MockJobService, job *core.Job, expectedStatus, expectedResult string)
		expectedStatus string // Status passed to Complete
		expectedResult string // Result passed to Complete
	}{
		{
			name:    "Success Hello Job",
			job:     &core.Job{ID: "job1", Type: "hello", Author: "author1"},
			jobFunc: jobHello,
			mockComplete: func(mockJob *mock_core.MockJobService, job *core.Job, expectedStatus, expectedResult string) {
				mockJob.EXPECT().Complete(gomock.Any(), job.ID, expectedStatus, expectedResult).Return(*job, nil).Times(1)
			},
			expectedStatus: "completed",
			expectedResult: "hello!",
		},
		{
			name:    "Success Clean Job",
			job:     &core.Job{ID: "job_clean_ok", Type: "clean", Author: "author_clean_ok"},
			jobFunc: jobCleanSuccess,
			mockComplete: func(mockJob *mock_core.MockJobService, job *core.Job, expectedStatus, expectedResult string) {
				mockJob.EXPECT().Complete(gomock.Any(), job.ID, expectedStatus, expectedResult).Return(*job, nil).Times(1)
			},
			expectedStatus: "completed",
			expectedResult: "cleaned",
		},
		{
			name:    "Failure Clean Job",
			job:     &core.Job{ID: "job_clean_fail", Type: "clean", Author: "author_clean_fail"},
			jobFunc: jobCleanFailure,
			mockComplete: func(mockJob *mock_core.MockJobService, job *core.Job, expectedStatus, expectedResult string) {
				// Note: The status includes the result string prefix in case of failure
				mockJob.EXPECT().Complete(gomock.Any(), job.ID, expectedStatus, "clean failed").Return(*job, nil).Times(1)
			},
			expectedStatus: "failed: cleaning error", // Status includes result prefix on failure
			expectedResult: "clean failed",           // The error message itself
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mock_core.NewMockStoreService(ctrl) // Store mock needed for reactor creation
			mockJob := mock_core.NewMockJobService(ctrl)
			r := NewReactor(mockStore, mockJob).(*reactor)
			ctx := t.Context()

			tt.mockComplete(mockJob, tt.job, tt.expectedStatus, tt.expectedResult)

			// Call dispatchJob directly as it runs synchronously
			r.dispatchJob(ctx, tt.job, tt.jobFunc)
		})
	}
}

// TestReactor_dispatchJobs tests the routing logic within dispatchJobs.
// It uses WaitGroup to handle goroutines for 'hello' and 'clean' jobs.
func TestReactor_dispatchJobs(t *testing.T) {
	tests := []struct {
		name            string
		setupMocks      func(wg *sync.WaitGroup, mockJob *mock_core.MockJobService, mockStore *mock_core.MockStoreService)
		expectGoroutine bool // Indicates if a goroutine (and thus wg.Add/Done) is expected
	}{
		{
			name: "Dispatch Hello Job",
			setupMocks: func(wg *sync.WaitGroup, mockJob *mock_core.MockJobService, mockStore *mock_core.MockStoreService) {
				job := &core.Job{ID: "helloJob", Type: "hello", Author: "author1"}
				mockJob.EXPECT().Dequeue(gomock.Any()).Return(job, nil).Times(1)
				// Expect Complete in goroutine and signal WaitGroup
				mockJob.EXPECT().Complete(gomock.Any(), job.ID, "completed", "hello!").
					DoAndReturn(func(_ context.Context, _, _, _ string) (core.Job, error) {
						wg.Done() // Signal completion
						return *job, nil
					}).Times(1)
			},
			expectGoroutine: true,
		},
		{
			name: "Dispatch Clean Job", // Covers both success/failure path implicitly via dispatchJob test
			setupMocks: func(wg *sync.WaitGroup, mockJob *mock_core.MockJobService, mockStore *mock_core.MockStoreService) {
				job := &core.Job{ID: "cleanJob", Type: "clean", Author: "author2"}
				mockJob.EXPECT().Dequeue(gomock.Any()).Return(job, nil).Times(1)
				// We need to expect the call to CleanUserAllData within the goroutine
				mockStore.EXPECT().CleanUserAllData(gomock.Any(), job.Author).Return(nil).Times(1)
				// Expect Complete in goroutine and signal WaitGroup
				mockJob.EXPECT().Complete(gomock.Any(), job.ID, gomock.Any(), gomock.Any()). // Status/Result depends on CleanUserAllData, tested in dispatchJob
														DoAndReturn(func(_ context.Context, _, _, _ string) (core.Job, error) {
						wg.Done()
						return *job, nil
					}).Times(1)
			},
			expectGoroutine: true,
		},
		{
			name: "Dispatch Unknown Job",
			setupMocks: func(wg *sync.WaitGroup, mockJob *mock_core.MockJobService, mockStore *mock_core.MockStoreService) {
				job := &core.Job{ID: "unknownJob", Type: "unknown", Author: "author4"}
				mockJob.EXPECT().Dequeue(gomock.Any()).Return(job, nil).Times(1)
				// Complete is called directly within dispatchJobs.
				mockJob.EXPECT().Complete(gomock.Any(), job.ID, "failed", "unknown job type").Return(*job, nil).Times(1)
			},
			expectGoroutine: false, // No goroutine for unknown type
		},
		{
			name: "Dequeue Error", // Covers general errors from Dequeue
			setupMocks: func(wg *sync.WaitGroup, mockJob *mock_core.MockJobService, mockStore *mock_core.MockStoreService) {
				mockJob.EXPECT().Dequeue(gomock.Any()).Return(nil, errors.New("some dequeue error")).Times(1)
				// No Complete call expected.
			},
			expectGoroutine: false, // No goroutine if Dequeue errors
		},
		{
			name: "Dequeue No Job Found Error", // Specific case where Dequeue returns an error indicating no job
			setupMocks: func(wg *sync.WaitGroup, mockJob *mock_core.MockJobService, mockStore *mock_core.MockStoreService) {
				// Mock Dequeue to return nil job and a specific "not found" error
				mockJob.EXPECT().Dequeue(gomock.Any()).Return(nil, errors.New("job not found")).Times(1)
				// No Complete call expected, dispatchJobs should return early due to the error.
			},
			expectGoroutine: false, // No goroutine if Dequeue returns error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			// Use defer ctrl.Finish() at the end, after wg.Wait()

			mockStore := mock_core.NewMockStoreService(ctrl)
			mockJob := mock_core.NewMockJobService(ctrl)
			r := NewReactor(mockStore, mockJob).(*reactor)
			ctx := t.Context()

			var wg sync.WaitGroup
			if tt.expectGoroutine {
				wg.Add(1) // Expect one goroutine to call wg.Done()
			}

			tt.setupMocks(&wg, mockJob, mockStore)

			r.dispatchJobs(ctx) // Call the function

			if tt.expectGoroutine {
				wg.Wait() // Wait for the goroutine to signal completion
			}

			ctrl.Finish() // Verify mocks *after* waiting for goroutines
		})
	}
}

// Note: Testing Start() directly is difficult due to the infinite loop and ticker.
// We test the core logic (dispatchJobs, dispatchJob) instead.
