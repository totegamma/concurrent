// Code generated by MockGen. DO NOT EDIT.
// Source: service.go

// Package mock_timeline is a generated GoMock package.
package mock_timeline

import (
	context "context"
	reflect "reflect"
	time "time"

	core "github.com/totegamma/concurrent/x/core"
	gomock "go.uber.org/mock/gomock"
)

// MockService is a mock of Service interface.
type MockService struct {
	ctrl     *gomock.Controller
	recorder *MockServiceMockRecorder
}

// MockServiceMockRecorder is the mock recorder for MockService.
type MockServiceMockRecorder struct {
	mock *MockService
}

// NewMockService creates a new mock instance.
func NewMockService(ctrl *gomock.Controller) *MockService {
	mock := &MockService{ctrl: ctrl}
	mock.recorder = &MockServiceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockService) EXPECT() *MockServiceMockRecorder {
	return m.recorder
}

// Count mocks base method.
func (m *MockService) Count(ctx context.Context) (int64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Count", ctx)
	ret0, _ := ret[0].(int64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Count indicates an expected call of Count.
func (mr *MockServiceMockRecorder) Count(ctx interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Count", reflect.TypeOf((*MockService)(nil).Count), ctx)
}

// DeleteTimeline mocks base method.
func (m *MockService) DeleteTimeline(ctx context.Context, mode core.CommitMode, document string) (core.Timeline, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteTimeline", ctx, mode, document)
	ret0, _ := ret[0].(core.Timeline)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DeleteTimeline indicates an expected call of DeleteTimeline.
func (mr *MockServiceMockRecorder) DeleteTimeline(ctx, mode, document interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteTimeline", reflect.TypeOf((*MockService)(nil).DeleteTimeline), ctx, mode, document)
}

// Event mocks base method.
func (m *MockService) Event(ctx context.Context, mode core.CommitMode, document, signature string) (core.Event, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Event", ctx, mode, document, signature)
	ret0, _ := ret[0].(core.Event)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Event indicates an expected call of Event.
func (mr *MockServiceMockRecorder) Event(ctx, mode, document, signature interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Event", reflect.TypeOf((*MockService)(nil).Event), ctx, mode, document, signature)
}

// GetChunks mocks base method.
func (m *MockService) GetChunks(ctx context.Context, timelines []string, pivot time.Time) (map[string]core.Chunk, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetChunks", ctx, timelines, pivot)
	ret0, _ := ret[0].(map[string]core.Chunk)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetChunks indicates an expected call of GetChunks.
func (mr *MockServiceMockRecorder) GetChunks(ctx, timelines, pivot interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetChunks", reflect.TypeOf((*MockService)(nil).GetChunks), ctx, timelines, pivot)
}

// GetChunksFromRemote mocks base method.
func (m *MockService) GetChunksFromRemote(ctx context.Context, host string, timelines []string, pivot time.Time) (map[string]core.Chunk, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetChunksFromRemote", ctx, host, timelines, pivot)
	ret0, _ := ret[0].(map[string]core.Chunk)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetChunksFromRemote indicates an expected call of GetChunksFromRemote.
func (mr *MockServiceMockRecorder) GetChunksFromRemote(ctx, host, timelines, pivot interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetChunksFromRemote", reflect.TypeOf((*MockService)(nil).GetChunksFromRemote), ctx, host, timelines, pivot)
}

// GetImmediateItems mocks base method.
func (m *MockService) GetImmediateItems(ctx context.Context, timelines []string, since time.Time, limit int) ([]core.TimelineItem, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetImmediateItems", ctx, timelines, since, limit)
	ret0, _ := ret[0].([]core.TimelineItem)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetImmediateItems indicates an expected call of GetImmediateItems.
func (mr *MockServiceMockRecorder) GetImmediateItems(ctx, timelines, since, limit interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetImmediateItems", reflect.TypeOf((*MockService)(nil).GetImmediateItems), ctx, timelines, since, limit)
}

// GetImmediateItemsFromSubscription mocks base method.
func (m *MockService) GetImmediateItemsFromSubscription(ctx context.Context, subscription string, since time.Time, limit int) ([]core.TimelineItem, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetImmediateItemsFromSubscription", ctx, subscription, since, limit)
	ret0, _ := ret[0].([]core.TimelineItem)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetImmediateItemsFromSubscription indicates an expected call of GetImmediateItemsFromSubscription.
func (mr *MockServiceMockRecorder) GetImmediateItemsFromSubscription(ctx, subscription, since, limit interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetImmediateItemsFromSubscription", reflect.TypeOf((*MockService)(nil).GetImmediateItemsFromSubscription), ctx, subscription, since, limit)
}

// GetItem mocks base method.
func (m *MockService) GetItem(ctx context.Context, timeline, id string) (core.TimelineItem, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetItem", ctx, timeline, id)
	ret0, _ := ret[0].(core.TimelineItem)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetItem indicates an expected call of GetItem.
func (mr *MockServiceMockRecorder) GetItem(ctx, timeline, id interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetItem", reflect.TypeOf((*MockService)(nil).GetItem), ctx, timeline, id)
}

// GetRecentItems mocks base method.
func (m *MockService) GetRecentItems(ctx context.Context, timelines []string, until time.Time, limit int) ([]core.TimelineItem, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetRecentItems", ctx, timelines, until, limit)
	ret0, _ := ret[0].([]core.TimelineItem)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetRecentItems indicates an expected call of GetRecentItems.
func (mr *MockServiceMockRecorder) GetRecentItems(ctx, timelines, until, limit interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetRecentItems", reflect.TypeOf((*MockService)(nil).GetRecentItems), ctx, timelines, until, limit)
}

// GetRecentItemsFromSubscription mocks base method.
func (m *MockService) GetRecentItemsFromSubscription(ctx context.Context, subscription string, until time.Time, limit int) ([]core.TimelineItem, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetRecentItemsFromSubscription", ctx, subscription, until, limit)
	ret0, _ := ret[0].([]core.TimelineItem)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetRecentItemsFromSubscription indicates an expected call of GetRecentItemsFromSubscription.
func (mr *MockServiceMockRecorder) GetRecentItemsFromSubscription(ctx, subscription, until, limit interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetRecentItemsFromSubscription", reflect.TypeOf((*MockService)(nil).GetRecentItemsFromSubscription), ctx, subscription, until, limit)
}

// GetTimeline mocks base method.
func (m *MockService) GetTimeline(ctx context.Context, key string) (core.Timeline, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetTimeline", ctx, key)
	ret0, _ := ret[0].(core.Timeline)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetTimeline indicates an expected call of GetTimeline.
func (mr *MockServiceMockRecorder) GetTimeline(ctx, key interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetTimeline", reflect.TypeOf((*MockService)(nil).GetTimeline), ctx, key)
}

// HasReadAccess mocks base method.
func (m *MockService) HasReadAccess(ctx context.Context, key, author string) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HasReadAccess", ctx, key, author)
	ret0, _ := ret[0].(bool)
	return ret0
}

// HasReadAccess indicates an expected call of HasReadAccess.
func (mr *MockServiceMockRecorder) HasReadAccess(ctx, key, author interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HasReadAccess", reflect.TypeOf((*MockService)(nil).HasReadAccess), ctx, key, author)
}

// HasWriteAccess mocks base method.
func (m *MockService) HasWriteAccess(ctx context.Context, key, author string) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HasWriteAccess", ctx, key, author)
	ret0, _ := ret[0].(bool)
	return ret0
}

// HasWriteAccess indicates an expected call of HasWriteAccess.
func (mr *MockServiceMockRecorder) HasWriteAccess(ctx, key, author interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HasWriteAccess", reflect.TypeOf((*MockService)(nil).HasWriteAccess), ctx, key, author)
}

// ListTimelineByAuthor mocks base method.
func (m *MockService) ListTimelineByAuthor(ctx context.Context, author string) ([]core.Timeline, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListTimelineByAuthor", ctx, author)
	ret0, _ := ret[0].([]core.Timeline)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListTimelineByAuthor indicates an expected call of ListTimelineByAuthor.
func (mr *MockServiceMockRecorder) ListTimelineByAuthor(ctx, author interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListTimelineByAuthor", reflect.TypeOf((*MockService)(nil).ListTimelineByAuthor), ctx, author)
}

// ListTimelineBySchema mocks base method.
func (m *MockService) ListTimelineBySchema(ctx context.Context, schema string) ([]core.Timeline, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListTimelineBySchema", ctx, schema)
	ret0, _ := ret[0].([]core.Timeline)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListTimelineBySchema indicates an expected call of ListTimelineBySchema.
func (mr *MockServiceMockRecorder) ListTimelineBySchema(ctx, schema interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListTimelineBySchema", reflect.TypeOf((*MockService)(nil).ListTimelineBySchema), ctx, schema)
}

// ListTimelineSubscriptions mocks base method.
func (m *MockService) ListTimelineSubscriptions(ctx context.Context) (map[string]int64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListTimelineSubscriptions", ctx)
	ret0, _ := ret[0].(map[string]int64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListTimelineSubscriptions indicates an expected call of ListTimelineSubscriptions.
func (mr *MockServiceMockRecorder) ListTimelineSubscriptions(ctx interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListTimelineSubscriptions", reflect.TypeOf((*MockService)(nil).ListTimelineSubscriptions), ctx)
}

// NormalizeTimelineID mocks base method.
func (m *MockService) NormalizeTimelineID(ctx context.Context, timeline string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NormalizeTimelineID", ctx, timeline)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NormalizeTimelineID indicates an expected call of NormalizeTimelineID.
func (mr *MockServiceMockRecorder) NormalizeTimelineID(ctx, timeline interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NormalizeTimelineID", reflect.TypeOf((*MockService)(nil).NormalizeTimelineID), ctx, timeline)
}

// PostItem mocks base method.
func (m *MockService) PostItem(ctx context.Context, timeline string, item core.TimelineItem, document, signature string) (core.TimelineItem, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PostItem", ctx, timeline, item, document, signature)
	ret0, _ := ret[0].(core.TimelineItem)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PostItem indicates an expected call of PostItem.
func (mr *MockServiceMockRecorder) PostItem(ctx, timeline, item, document, signature interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PostItem", reflect.TypeOf((*MockService)(nil).PostItem), ctx, timeline, item, document, signature)
}

// PublishEvent mocks base method.
func (m *MockService) PublishEvent(ctx context.Context, event core.Event) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PublishEvent", ctx, event)
	ret0, _ := ret[0].(error)
	return ret0
}

// PublishEvent indicates an expected call of PublishEvent.
func (mr *MockServiceMockRecorder) PublishEvent(ctx, event interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PublishEvent", reflect.TypeOf((*MockService)(nil).PublishEvent), ctx, event)
}

// RemoveItem mocks base method.
func (m *MockService) RemoveItem(ctx context.Context, timeline, id string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "RemoveItem", ctx, timeline, id)
}

// RemoveItem indicates an expected call of RemoveItem.
func (mr *MockServiceMockRecorder) RemoveItem(ctx, timeline, id interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveItem", reflect.TypeOf((*MockService)(nil).RemoveItem), ctx, timeline, id)
}

// UpsertTimeline mocks base method.
func (m *MockService) UpsertTimeline(ctx context.Context, mode core.CommitMode, document, signature string) (core.Timeline, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpsertTimeline", ctx, mode, document, signature)
	ret0, _ := ret[0].(core.Timeline)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// UpsertTimeline indicates an expected call of UpsertTimeline.
func (mr *MockServiceMockRecorder) UpsertTimeline(ctx, mode, document, signature interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpsertTimeline", reflect.TypeOf((*MockService)(nil).UpsertTimeline), ctx, mode, document, signature)
}
