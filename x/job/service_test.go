package job

import (
	"errors"
	"testing"
	"time"

	"github.com/concrnt/concrnt/core"
	"github.com/concrnt/concrnt/x/job/mock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestNewService(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_job.NewMockRepository(ctrl)
	s := NewService(repo)
	assert.NotNil(t, s)
	assert.Implements(t, (*core.JobService)(nil), s)
}

func TestService_List(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_job.NewMockRepository(ctrl)
	s := NewService(repo)
	ctx := t.Context()
	requester := "testRequester"
	expectedJobs := []core.Job{{ID: "job1"}, {ID: "job2"}}

	repo.EXPECT().List(gomock.Any(), requester).Return(expectedJobs, nil).Times(1)

	jobs, err := s.List(ctx, requester)
	assert.NoError(t, err)
	assert.Equal(t, expectedJobs, jobs)
}

func TestService_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_job.NewMockRepository(ctrl)
	s := NewService(repo)
	ctx := t.Context()
	requester := "testRequester"
	jobType := "testType"
	payload := `{"data":"test"}`
	scheduled := time.Now().Add(time.Hour)
	expectedJob := core.Job{ID: "newJob", Author: requester, Type: jobType, Payload: payload, Scheduled: scheduled, Status: "pending"}

	repo.EXPECT().Enqueue(gomock.Any(), requester, jobType, payload, scheduled).Return(expectedJob, nil).Times(1)

	job, err := s.Create(ctx, requester, jobType, payload, scheduled)
	assert.NoError(t, err)
	assert.Equal(t, expectedJob, job)
}

func TestService_Dequeue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_job.NewMockRepository(ctrl)
	s := NewService(repo)
	ctx := t.Context()
	expectedJob := &core.Job{ID: "dequeuedJob", Status: "running"}

	repo.EXPECT().Dequeue(gomock.Any()).Return(expectedJob, nil).Times(1)

	job, err := s.Dequeue(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expectedJob, job)
}

func TestService_Complete(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_job.NewMockRepository(ctrl)
	s := NewService(repo)
	ctx := t.Context()
	jobID := "jobToComplete"
	status := "completed"
	resultStr := `{"output":"done"}`
	expectedJob := core.Job{ID: jobID, Status: status, Result: resultStr}

	repo.EXPECT().Complete(gomock.Any(), jobID, status, resultStr).Return(expectedJob, nil).Times(1)

	job, err := s.Complete(ctx, jobID, status, resultStr)
	assert.NoError(t, err)
	assert.Equal(t, expectedJob, job)
}

func TestService_Cancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_job.NewMockRepository(ctrl)
	s := NewService(repo)
	ctx := t.Context()
	jobID := "jobToCancel"
	expectedJob := core.Job{ID: jobID, Status: "canceled"}

	repo.EXPECT().Cancel(gomock.Any(), jobID).Return(expectedJob, nil).Times(1)

	job, err := s.Cancel(ctx, jobID)
	assert.NoError(t, err)
	assert.Equal(t, expectedJob, job)
}

// Test error cases for each method
func TestService_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_job.NewMockRepository(ctrl)
	s := NewService(repo)
	ctx := t.Context()
	testError := errors.New("repo error")

	t.Run("ListError", func(t *testing.T) {
		repo.EXPECT().List(gomock.Any(), "errorUser").Return(nil, testError).Times(1)
		_, err := s.List(ctx, "errorUser")
		assert.Error(t, err)
		assert.Equal(t, testError, err)
	})

	t.Run("CreateError", func(t *testing.T) {
		repo.EXPECT().Enqueue(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(core.Job{}, testError).Times(1)
		_, err := s.Create(ctx, "user", "type", "payload", time.Now())
		assert.Error(t, err)
		assert.Equal(t, testError, err)
	})

	t.Run("DequeueError", func(t *testing.T) {
		repo.EXPECT().Dequeue(gomock.Any()).Return(nil, testError).Times(1)
		_, err := s.Dequeue(ctx)
		assert.Error(t, err)
		assert.Equal(t, testError, err)
	})

	t.Run("CompleteError", func(t *testing.T) {
		repo.EXPECT().Complete(gomock.Any(), "errorID", "status", "result").Return(core.Job{}, testError).Times(1)
		_, err := s.Complete(ctx, "errorID", "status", "result")
		assert.Error(t, err)
		assert.Equal(t, testError, err)
	})

	t.Run("CancelError", func(t *testing.T) {
		repo.EXPECT().Cancel(gomock.Any(), "errorID").Return(core.Job{}, testError).Times(1)
		_, err := s.Cancel(ctx, "errorID")
		assert.Error(t, err)
		assert.Equal(t, testError, err)
	})
}
