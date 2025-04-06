package job

import (
	"context"
	"errors"
	"testing"
	//"time" // Unused import

	"github.com/stretchr/testify/assert"
	"github.com/totegamma/concurrent/core"
	mock_core "github.com/totegamma/concurrent/core/mock"
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

func TestReactor_DispatchJob_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mock_core.NewMockStoreService(ctrl)
	mockJob := mock_core.NewMockJobService(ctrl)
	r := NewReactor(mockStore, mockJob).(*reactor) // Cast to access internal methods
	ctx := context.Background()

	job := &core.Job{ID: "job1", Type: "hello", Author: "author1"}
	expectedResult := "hello!"

	// Expect Complete to be called with success status
	mockJob.EXPECT().Complete(gomock.Any(), job.ID, "completed", expectedResult).Return(*job, nil).Times(1)

	// Call dispatchJob directly for testing the success path
	r.dispatchJob(ctx, job, r.JobHello)
}

func TestReactor_DispatchJob_Failure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mock_core.NewMockStoreService(ctrl)
	mockJob := mock_core.NewMockJobService(ctrl)
	r := NewReactor(mockStore, mockJob).(*reactor)
	ctx := context.Background()

	job := &core.Job{ID: "job2", Type: "clean", Author: "author2"}
	jobError := errors.New("clean failed")
	expectedResult := "cleaning error" // Example result string from the job function on error

	// Mock the job function (jobClean) to return an error
	mockStore.EXPECT().CleanUserAllData(gomock.Any(), job.Author).Return(jobError).Times(1)

	// Expect Complete to be called with failure status and error message
	mockJob.EXPECT().Complete(gomock.Any(), job.ID, "failed: "+expectedResult, jobError.Error()).
		DoAndReturn(func(_ context.Context, id, status, result string) (core.Job, error) {
			// Simulate the job function returning a result string even on error
			return *job, nil
		}).Times(1)

	// Call dispatchJob directly for testing the failure path
	// We need a wrapper for jobClean to simulate the return signature of dispatchJob's fn argument
	jobCleanWrapper := func(ctx context.Context, j *core.Job) (string, error) {
		err := r.store.CleanUserAllData(ctx, j.Author) // Use 'r' instead of 'a'
		if err != nil {
			return expectedResult, err
		}
		return "", nil
	}
	r.dispatchJob(ctx, job, jobCleanWrapper)
}

func TestReactor_DispatchJobs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mock_core.NewMockStoreService(ctrl)
	mockJob := mock_core.NewMockJobService(ctrl)
	r := NewReactor(mockStore, mockJob).(*reactor)
	ctx := context.Background()

	helloJob := &core.Job{ID: "helloJob", Type: "hello", Author: "author1"}
	cleanJob := &core.Job{ID: "cleanJob", Type: "clean", Author: "author2"}
	unknownJob := &core.Job{ID: "unknownJob", Type: "unknown", Author: "author3"}

	// --- Test Hello Job ---
	t.Run("DispatchHello", func(t *testing.T) {
		mockJob.EXPECT().Dequeue(gomock.Any()).Return(helloJob, nil).Times(1)
		mockJob.EXPECT().Complete(gomock.Any(), helloJob.ID, "completed", "hello!").Return(*helloJob, nil).Times(1)
		r.dispatchJobs(ctx)
	})

	// --- Test Clean Job (Success) ---
	t.Run("DispatchClean_Success", func(t *testing.T) {
		mockJob.EXPECT().Dequeue(gomock.Any()).Return(cleanJob, nil).Times(1)
		mockStore.EXPECT().CleanUserAllData(gomock.Any(), cleanJob.Author).Return(nil).Times(1)
		mockJob.EXPECT().Complete(gomock.Any(), cleanJob.ID, "completed", "").Return(*cleanJob, nil).Times(1)
		r.dispatchJobs(ctx)
	})

	// --- Test Clean Job (Failure) ---
	t.Run("DispatchClean_Failure", func(t *testing.T) {
		jobError := errors.New("db clean error")
		mockJob.EXPECT().Dequeue(gomock.Any()).Return(cleanJob, nil).Times(1)
		mockStore.EXPECT().CleanUserAllData(gomock.Any(), cleanJob.Author).Return(jobError).Times(1)
		// Expect Complete with failure status. The result string from jobClean on error is empty.
		mockJob.EXPECT().Complete(gomock.Any(), cleanJob.ID, "failed: ", jobError.Error()).Return(*cleanJob, nil).Times(1)
		r.dispatchJobs(ctx)
	})

	// --- Test Unknown Job ---
	t.Run("DispatchUnknown", func(t *testing.T) {
		mockJob.EXPECT().Dequeue(gomock.Any()).Return(unknownJob, nil).Times(1)
		mockJob.EXPECT().Complete(gomock.Any(), unknownJob.ID, "failed", "unknown job type").Return(*unknownJob, nil).Times(1)
		r.dispatchJobs(ctx)
	})

	// --- Test Dequeue Error ---
	t.Run("DispatchDequeueError", func(t *testing.T) {
		mockJob.EXPECT().Dequeue(gomock.Any()).Return(nil, errors.New("dequeue error")).Times(1)
		// No Complete call expected
		r.dispatchJobs(ctx)
	})
}

// Note: Testing Start() directly is difficult due to the infinite loop and ticker.
// We test the core logic (dispatchJobs, dispatchJob) instead.
