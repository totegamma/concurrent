package policy

import (
	"testing"

	"github.com/concrnt/concrnt/core"
	"github.com/stretchr/testify/assert"
)

func TestIsDominant(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	dominant, value := IsDominant(core.PolicyEvalResultAlways)
	assert.True(dominant)
	assert.True(value)

	dominant, value = IsDominant(core.PolicyEvalResultNever)
	assert.True(dominant)
	assert.False(value)

	dominant, value = IsDominant(core.PolicyEvalResultAllow)
	assert.False(dominant)

	dominant, value = IsDominant(core.PolicyEvalResultDeny)
	assert.False(dominant)

	dominant, value = IsDominant(core.PolicyEvalResultDefault)
	assert.False(dominant)

	dominant, value = IsDominant(core.PolicyEvalResultError)
	assert.False(dominant)
}

func TestAccumulateOr(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	// Basic cases
	assert.Equal(core.PolicyEvalResultAllow, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAllow}))
	assert.Equal(core.PolicyEvalResultDeny, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultDeny}))
	assert.Equal(core.PolicyEvalResultDefault, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultDefault}))
	assert.Equal(core.PolicyEvalResultAlways, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways}))
	assert.Equal(core.PolicyEvalResultNever, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultNever}))
	assert.Equal(core.PolicyEvalResultError, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultError}))
	assert.Equal(core.PolicyEvalResultDefault, AccumulateOr([]core.PolicyEvalResult{}))

	// Combinations
	assert.Equal(core.PolicyEvalResultAllow, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAllow, core.PolicyEvalResultDefault}))
	assert.Equal(core.PolicyEvalResultDeny, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultDeny, core.PolicyEvalResultDefault}))
	assert.Equal(core.PolicyEvalResultDefault, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAllow, core.PolicyEvalResultDeny}))

	assert.Equal(core.PolicyEvalResultAlways, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways, core.PolicyEvalResultDefault}))
	assert.Equal(core.PolicyEvalResultNever, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultNever, core.PolicyEvalResultDefault}))
	assert.Equal(core.PolicyEvalResultDefault, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways, core.PolicyEvalResultNever}))

	assert.Equal(core.PolicyEvalResultAlways, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways, core.PolicyEvalResultAllow}))
	assert.Equal(core.PolicyEvalResultAlways, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways, core.PolicyEvalResultDeny}))
	assert.Equal(core.PolicyEvalResultNever, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultNever, core.PolicyEvalResultAllow}))
	assert.Equal(core.PolicyEvalResultNever, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultNever, core.PolicyEvalResultDeny}))

	// Error propagation
	assert.Equal(core.PolicyEvalResultError, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAllow, core.PolicyEvalResultError}))
	assert.Equal(core.PolicyEvalResultError, AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways, core.PolicyEvalResultError}))
}

func TestStructToMap(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	type Embedded struct {
		EmbeddedField string `json:"embedded_field"`
		IgnoredField  string
	}

	type SampleStruct struct {
		StringField string `json:"string_field"`
		IntField    int    `json:"int_field,omitempty"`
		BoolField   bool   `json:"bool_field"`
		FloatField  float64
		Embedded
	}

	sample := SampleStruct{
		StringField: "hello",
		IntField:    123,
		BoolField:   true,
		FloatField:  3.14, // Should be ignored
		Embedded: Embedded{
			EmbeddedField: "world",
			IgnoredField:  "ignore me", // Should be ignored
		},
	}

	expectedMap := map[string]any{
		"string_field":   "hello",
		"int_field":      123,
		"bool_field":     true,
		"embedded_field": "world",
	}

	resultMap := structToMap(sample)
	assert.Equal(expectedMap, resultMap)

	// Test with pointer
	resultMapPtr := structToMap(&sample)
	assert.Equal(expectedMap, resultMapPtr)
}

func TestResolveDotNotation(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	testMap := map[string]any{
		"topLevel": "value1",
		"nested": map[string]any{
			"secondLevel": "value2",
			"another": map[string]any{
				"thirdLevel": 123,
			},
		},
		"arrayField": []string{"a", "b"},
	}

	// Simple top-level lookup
	val, ok := resolveDotNotation(testMap, "topLevel")
	assert.True(ok)
	assert.Equal("value1", val)

	// Nested lookup
	val, ok = resolveDotNotation(testMap, "nested.secondLevel")
	assert.True(ok)
	assert.Equal("value2", val)

	// Deeply nested lookup
	val, ok = resolveDotNotation(testMap, "nested.another.thirdLevel")
	assert.True(ok)
	assert.Equal(123, val)

	// Lookup returning a map
	val, ok = resolveDotNotation(testMap, "nested.another")
	assert.True(ok)
	assert.Equal(map[string]any{"thirdLevel": 123}, val)

	// Lookup returning an array
	val, ok = resolveDotNotation(testMap, "arrayField")
	assert.True(ok)
	assert.Equal([]string{"a", "b"}, val)

	// Key not found
	val, ok = resolveDotNotation(testMap, "nonExistent")
	assert.False(ok)
	assert.Nil(val)
	val, ok = resolveDotNotation(testMap, "nested.nonExistent")
	assert.False(ok)
	assert.Nil(val)

	// Intermediate key not a map
	val, ok = resolveDotNotation(testMap, "topLevel.nonExistent")
	assert.False(ok)
	assert.Nil(val)

	// Empty key
	val, ok = resolveDotNotation(testMap, "")
	assert.False(ok)
	assert.Nil(val)

	// Key with only dots
	val, ok = resolveDotNotation(testMap, "..")
	assert.False(ok)
	assert.Nil(val)
}
