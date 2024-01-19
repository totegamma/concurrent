package core

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestScopes(t *testing.T) {
	scope1 := ParseScopes("stream:write:ci8qvhep9dcpltmfq3fg@hub.concurrent.world")
	if assert.Contains(t, scope1.Body, "stream") {
		assert.Equal(t, scope1.Body["stream"].Action[0], "write")
		assert.Equal(t, scope1.Body["stream"].Resources[0], "ci8qvhep9dcpltmfq3fg@hub.concurrent.world")
		assert.Equal(t, scope1.CanPerform("stream:write:ci8qvhep9dcpltmfq3fg@hub.concurrent.world"), true)
		assert.Equal(t, scope1.CanPerform("stream:read:ci8qvhep9dcpltmfq3fg@hub.concurrent.world"), false)
	}

	scope2 := ParseScopes("*:*:*")
	assert.Equal(t, scope2.CanPerform("stream:write:ci8qvhep9dcpltmfq3fg@hub.concurrent.world"), true)
}
