package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/internal/domain"
)

type fakeDeliveryClient struct {
	resolveHost string
	resolveErr  error
	commitErr   error

	resolvedURI  string
	commitCalled bool
	commitHost   string
	commitSD     concrnt.SignedDocument
}

func (f *fakeDeliveryClient) ResolveResourceHost(ctx context.Context, uri string) (string, error) {
	f.resolvedURI = uri
	return f.resolveHost, f.resolveErr
}

func (f *fakeDeliveryClient) Commit(ctx context.Context, resolver string, sd concrnt.SignedDocument) error {
	f.commitCalled = true
	f.commitHost = resolver
	f.commitSD = sd
	return f.commitErr
}

type fakePubSub struct {
	published bool
	channel   string
	event     concrnt.Event
	err       error
}

func (f *fakePubSub) Publish(ctx context.Context, channel string, event concrnt.Event) error {
	f.published = true
	f.channel = channel
	f.event = event
	return f.err
}

func (f *fakePubSub) Subscribe(ctx context.Context, prefixes []string, response chan<- concrnt.Event) error {
	return nil
}

func (f *fakePubSub) SubscribeAll(ctx context.Context, response chan<- concrnt.Event) error {
	return nil
}

type fakeCommitter struct {
	called bool
	ip     string
	sd     concrnt.SignedDocument
	mode   domain.CommitMode
	err    error
}

func (f *fakeCommitter) Commit(ctx context.Context, ip string, sd concrnt.SignedDocument, mode domain.CommitMode) (*concrnt.SignedDocument, error) {
	f.called = true
	f.ip = ip
	f.sd = sd
	f.mode = mode
	return &sd, f.err
}

const testFQDN = "local.example"

func newTestWorker(client DeliveryClient, pubsub PubSub, committer Committer) *DeliveryWorker {
	return NewDeliveryWorker(&domain.Config{FQDN: testFQDN}, client, pubsub, committer, nil)
}

func TestDeliveryWorker_LocalPublish(t *testing.T) {
	client := &fakeDeliveryClient{resolveHost: testFQDN}
	pubsub := &fakePubSub{}
	committer := &fakeCommitter{}
	w := newTestWorker(client, pubsub, committer)

	job := domain.DeliveryJob{
		ResolveURI: "cckv://owner/key",
		Local:      domain.DeliveryLocalPublish,
		Remote:     domain.DeliveryRemoteNone,
		Event:      &concrnt.Event{Type: "deleted", URI: "cckv://owner/key"},
	}

	if err := w.handle(context.Background(), job); err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
	if !pubsub.published {
		t.Fatal("expected Publish to be called")
	}
	if pubsub.channel != job.ResolveURI {
		t.Fatalf("published on channel %q, want %q", pubsub.channel, job.ResolveURI)
	}
	if committer.called || client.commitCalled {
		t.Fatal("did not expect Commit to be called for a local publish job")
	}
}

func TestDeliveryWorker_LocalCommit(t *testing.T) {
	client := &fakeDeliveryClient{resolveHost: testFQDN}
	pubsub := &fakePubSub{}
	committer := &fakeCommitter{}
	w := newTestWorker(client, pubsub, committer)

	payload := concrnt.SignedDocument{Document: "doc"}
	job := domain.DeliveryJob{
		ResolveURI: "cckv://owner/key",
		Local:      domain.DeliveryLocalCommit,
		Remote:     domain.DeliveryRemoteCommit,
		Payload:    payload,
		IP:         "127.0.0.1",
	}

	if err := w.handle(context.Background(), job); err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
	if !committer.called {
		t.Fatal("expected Commit to be called")
	}
	if committer.ip != job.IP {
		t.Fatalf("committer got ip %q, want %q", committer.ip, job.IP)
	}
	if committer.mode != domain.CommitModeExecute {
		t.Fatalf("committer got mode %v, want CommitModeExecute", committer.mode)
	}
	if client.commitCalled {
		t.Fatal("did not expect remote Commit to be called for a local destination")
	}
}

func TestDeliveryWorker_RemoteCommit(t *testing.T) {
	client := &fakeDeliveryClient{resolveHost: "remote.example"}
	pubsub := &fakePubSub{}
	committer := &fakeCommitter{}
	w := newTestWorker(client, pubsub, committer)

	payload := concrnt.SignedDocument{Document: "doc"}
	job := domain.DeliveryJob{
		ResolveURI: "cckv://owner/key",
		Local:      domain.DeliveryLocalPublish,
		Remote:     domain.DeliveryRemoteCommit,
		Payload:    payload,
	}

	if err := w.handle(context.Background(), job); err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
	if !client.commitCalled {
		t.Fatal("expected remote Commit to be called")
	}
	if client.commitHost != "remote.example" {
		t.Fatalf("committed to host %q, want %q", client.commitHost, "remote.example")
	}
	if pubsub.published {
		t.Fatal("did not expect Publish to be called for a remote destination")
	}
}

func TestDeliveryWorker_RemoteNone(t *testing.T) {
	client := &fakeDeliveryClient{resolveHost: "remote.example"}
	pubsub := &fakePubSub{}
	committer := &fakeCommitter{}
	w := newTestWorker(client, pubsub, committer)

	job := domain.DeliveryJob{
		ResolveURI: "cckv://owner/key",
		Local:      domain.DeliveryLocalPublish,
		Remote:     domain.DeliveryRemoteNone,
	}

	if err := w.handle(context.Background(), job); err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
	if client.commitCalled {
		t.Fatal("did not expect Commit to be called when Remote == none")
	}
}

func TestDeliveryWorker_PreResolvedHost(t *testing.T) {
	client := &fakeDeliveryClient{resolveHost: "should-not-be-used"}
	pubsub := &fakePubSub{}
	committer := &fakeCommitter{}
	w := newTestWorker(client, pubsub, committer)

	payload := concrnt.SignedDocument{Document: "doc"}
	job := domain.DeliveryJob{
		Host:    "target.example",
		Remote:  domain.DeliveryRemoteCommit,
		Local:   domain.DeliveryLocalNone,
		Payload: payload,
	}

	if err := w.handle(context.Background(), job); err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
	if client.resolvedURI != "" {
		t.Fatal("did not expect ResolveResourceHost to be called when Host is already known")
	}
	if !client.commitCalled || client.commitHost != "target.example" {
		t.Fatalf("expected Commit to target.example, got called=%v host=%q", client.commitCalled, client.commitHost)
	}
}

func TestDeliveryWorker_ResolveError(t *testing.T) {
	wantErr := errors.New("resolve failed")
	client := &fakeDeliveryClient{resolveErr: wantErr}
	pubsub := &fakePubSub{}
	committer := &fakeCommitter{}
	w := newTestWorker(client, pubsub, committer)

	job := domain.DeliveryJob{
		ResolveURI: "cckv://owner/key",
		Local:      domain.DeliveryLocalPublish,
		Remote:     domain.DeliveryRemoteCommit,
	}

	err := w.handle(context.Background(), job)
	if !errors.Is(err, wantErr) {
		t.Fatalf("handle returned %v, want %v", err, wantErr)
	}
	if pubsub.published || client.commitCalled {
		t.Fatal("did not expect any local/remote action when resolution fails")
	}
}

func TestDeliveryWorker_LocalPublishMissingEvent(t *testing.T) {
	client := &fakeDeliveryClient{resolveHost: testFQDN}
	pubsub := &fakePubSub{}
	committer := &fakeCommitter{}
	w := newTestWorker(client, pubsub, committer)

	job := domain.DeliveryJob{
		ResolveURI: "cckv://owner/key",
		Local:      domain.DeliveryLocalPublish,
		Remote:     domain.DeliveryRemoteNone,
	}

	if err := w.handle(context.Background(), job); err == nil {
		t.Fatal("expected an error for a local publish job with no Event, got nil")
	}
	if pubsub.published {
		t.Fatal("did not expect Publish to be called without an Event")
	}
}
