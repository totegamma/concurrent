package core

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestTags(t *testing.T) {
	tags1 := ParseTags("key1:value1,key2:value2")
	assert.Equal(t, "value1", tags1.Get("key1"))
	assert.Equal(t, "value2", tags1.Get("key2"))

	tags2 := ParseTags("tag1,tag2:100")
	assert.Equal(t, true, tags2.Has("tag1"))
	assert.Equal(t, true, tags2.Has("tag2"))
	assert.Equal(t, false, tags2.Has("tag3"))
	assert.Equal(t, "100", tags2.Get("tag2"))
	val, ok := tags2.GetAsInt("tag2")
	assert.Equal(t, true, ok)
	assert.Equal(t, 100, val)

	tags3 := NewTags()
	tags3.Add("key1", "value1")
	tags3.Add("key2", "")

	// output order is not guaranteed so we need to check both possibilities
	str := tags3.ToString()
	assert.Contains(t, []string{"key1:value1,key2", "key2,key1:value1"}, str)

	tags3.Remove("key1")
	assert.Equal(t, false, tags3.Has("key1"))
}
