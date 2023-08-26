package userkv

import (
	"context"
	"github.com/totegamma/concurrent/x/userkv/mock"
	"go.uber.org/mock/gomock"
	"testing"
)

func TestServiceGet(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock_userkv.NewMockIRepository(ctrl)
	mockRepo.EXPECT().Get(gomock.Any(), "myuser:mykey").Return("myvalue", nil)

	s := NewService(mockRepo)
	result, err := s.Get(context.Background(), "myuser", "mykey")

	if err != nil {
		t.Fatal(err)
	}
	if result != "myvalue" {
		t.Fatalf("expected myvalue, got %s", result)
	}
}

func TestServiceUpsert(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock_userkv.NewMockIRepository(ctrl)
	mockRepo.EXPECT().Upsert(gomock.Any(), "myuser:mykey", "myvalue").Return(nil)

	s := NewService(mockRepo)
	err := s.Upsert(context.Background(), "myuser", "mykey", "myvalue")

	if err != nil {
		t.Fatal(err)
	}
}
