package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A disallowed api must be rejected before any resolution or network I/O:
// with a nil client, reaching the client would panic.
func TestConcrntCallAllowlist(t *testing.T) {

	s := &PolicyService{client: nil}

	_, err := s.ConcrntCall(context.Background(), "con1alice", "net.concrnt.core.commit", map[string]string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}
