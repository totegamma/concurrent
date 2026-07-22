package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/concrnt/concrnt"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/policy"
)

// A disallowed api must be rejected before any resolution or network I/O:
// with a nil client, reaching the client would panic.
func TestConcrntCallAllowlist(t *testing.T) {

	s := &PolicyService{client: nil}

	_, err := s.ConcrntCall(context.Background(), "con1alice", "net.concrnt.core.commit", map[string]string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

// CIP-12 §5.3: a virtual parent (distribution destination) forms its own
// layer inserted immediately before the layer that declares it — the
// declaring resource stays last — instead of being merged into an ancestor
// layer. An unreachable parent yields an errored evaluation set in that slot.
func TestResolvePolicyStackVirtualParentPlacement(t *testing.T) {
	s := NewPolicyService(policy.Policy{}, GlobalParameters{}, client.New("example.com"))

	unreachable := ":::not-a-uri"
	stack := []concrnt.Policy{
		{Source: "cckv://con1owner/root"},
		{Source: "cckv://con1owner/root/child", VirtualParents: &[]string{unreachable}},
	}

	result, err := s.resolvePolicyStack(context.Background(), stack)
	assert.NoError(t, err)
	// [root, virtual parent (errored), declaring resource]
	if assert.Len(t, result, 3) {
		assert.Len(t, result[0], 0)
		if assert.Len(t, result[1], 1) {
			assert.True(t, result[1][0].Errored)
		}
		assert.Len(t, result[2], 0)
	}
}
