// Code generated by MockGen. DO NOT EDIT.
// Source: repository.go
//
// Generated by this command:
//
//	mockgen -source=repository.go -destination=mock/repository.go
//

// Package mock_userkv is a generated GoMock package.
package mock_userkv

import (
	context "context"
	reflect "reflect"

	gomock "go.uber.org/mock/gomock"
)

// MockRepository is a mock of Repository interface.
type MockRepository struct {
	ctrl     *gomock.Controller
	recorder *MockRepositoryMockRecorder
}

// MockRepositoryMockRecorder is the mock recorder for MockRepository.
type MockRepositoryMockRecorder struct {
	mock *MockRepository
}

// NewMockRepository creates a new mock instance.
func NewMockRepository(ctrl *gomock.Controller) *MockRepository {
	mock := &MockRepository{ctrl: ctrl}
	mock.recorder = &MockRepositoryMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRepository) EXPECT() *MockRepositoryMockRecorder {
	return m.recorder
}

// Clean mocks base method.
func (m *MockRepository) Clean(ctx context.Context, ccid string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Clean", ctx, ccid)
	ret0, _ := ret[0].(error)
	return ret0
}

// Clean indicates an expected call of Clean.
func (mr *MockRepositoryMockRecorder) Clean(ctx, ccid any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Clean", reflect.TypeOf((*MockRepository)(nil).Clean), ctx, ccid)
}

// Get mocks base method.
func (m *MockRepository) Get(ctx context.Context, owner, key string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", ctx, owner, key)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get.
func (mr *MockRepositoryMockRecorder) Get(ctx, owner, key any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockRepository)(nil).Get), ctx, owner, key)
}

// Upsert mocks base method.
func (m *MockRepository) Upsert(ctx context.Context, owner, key, value string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Upsert", ctx, owner, key, value)
	ret0, _ := ret[0].(error)
	return ret0
}

// Upsert indicates an expected call of Upsert.
func (mr *MockRepositoryMockRecorder) Upsert(ctx, owner, key, value any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Upsert", reflect.TypeOf((*MockRepository)(nil).Upsert), ctx, owner, key, value)
}
