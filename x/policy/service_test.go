package policy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/mock/gomock"

	"github.com/concrnt/concrnt/client/mock"
	"github.com/concrnt/concrnt/core"
	"github.com/concrnt/concrnt/internal/testutil"
)

var s core.PolicyService

var globalPolicyJson = `
{
    "statements": {
        "global": {
            "dominant": true,
            "defaultOnTrue": true,
            "condition": {
                "op": "Not",
                "args": [
                    {
                        "op": "Or",
                        "args": [
                            {
                                "op": "RequesterDomainHasTag",
                                "const": "_block"
                            },
                            {
                                "op": "RequesterHasTag",
                                "const": "_block"
                            }
                        ]
                    }
                ]
            }
        },
        "timeline.message.read": {
            "condition": {
                "op": "Or",
                "args": [
                    {
                        "op": "IsCSID",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "owner"
                            }
                        ]
                    },
                    {
                        "op": "Eq",
                        "args": [
                            {
                                "op": "LoadSelf",
                                "const": "author"
                            },
                            {
                                "op": "RequesterID"
                            }
                        ]
                    }
                ]
            }
        }
    }
}
`

var checker *tracetest.InMemoryExporter

func TestMain(m *testing.M) {

	checker = testutil.SetupMockTraceProvider()

	var globalPolicy core.Policy
	err := json.Unmarshal([]byte(globalPolicyJson), &globalPolicy)
	if err != nil {
		panic(err)
	}

	repository := NewRepository(nil)

	ctrl := gomock.NewController(nil)
	client := mock_client.NewMockClient(ctrl)

	s = NewService(
		repository,
		client,
		globalPolicy,
		core.Config{
			FQDN: "local.example.com",
			CSID: "ccs16djx38r2qx8j49fx53ewugl90t3y6ndgye8ykt",
		},
	)

	m.Run()
}

// 0. block判定
func TestGlobalBlock(t *testing.T) {

	rctx0 := core.RequestContext{
		Requester: core.Entity{
			Domain: "local.example.com",
		},
	}

	ctx, id := testutil.SetupTraceCtx()
	result, err := s.TestWithGlobalPolicy(ctx, rctx0, "global")
	test0OK := assert.NoError(t, err)
	test0OK = test0OK && assert.Equal(t, core.PolicyEvalResultDefault, result)

	if !test0OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	rctx1 := core.RequestContext{
		Requester: core.Entity{
			Domain: "local.example.com",
			Tag:    "_block",
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.TestWithGlobalPolicy(ctx, rctx1, "global")
	test1OK := assert.NoError(t, err)
	test1OK = test1OK && assert.Equal(t, core.PolicyEvalResultNever, result)

	if !test1OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	rctx2 := core.RequestContext{
		Requester: core.Entity{
			Domain: "other.example.net",
		},
		RequesterDomain: core.Domain{
			Tag: "_block",
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.TestWithGlobalPolicy(ctx, rctx2, "global")
	test2OK := assert.NoError(t, err)
	test2OK = test2OK && assert.Equal(t, core.PolicyEvalResultNever, result)

	if !test2OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}
}

func TestServiceSummerize(t *testing.T) {
	assert := assert.New(t)

	client := mock_client.NewMockClient(gomock.NewController(t))

	// Setup with some global defaults
	var globalPolicy core.Policy
	json.Unmarshal([]byte(`{"defaults": {"read": true, "write": false}}`), &globalPolicy)
	testService := NewService(nil, client, globalPolicy, core.Config{}).(*service)

	// No results, default true
	assert.True(testService.Summerize([]core.PolicyEvalResult{}, "read", nil))
	// No results, default false
	assert.False(testService.Summerize([]core.PolicyEvalResult{}, "write", nil))
	assert.False(testService.Summerize([]core.PolicyEvalResult{}, "delete", nil)) // No default

	// Basic results
	assert.True(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultAllow}, "write", nil))
	assert.False(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultDeny}, "read", nil))
	assert.True(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultDefault}, "read", nil))
	assert.False(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultDefault}, "write", nil))

	// Dominant results
	assert.True(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultAlways}, "write", nil))
	assert.False(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultNever}, "read", nil))
	assert.True(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultAllow, core.PolicyEvalResultAlways}, "write", nil))
	assert.False(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultDeny, core.PolicyEvalResultNever}, "read", nil))

	// Mixed results
	assert.True(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultDeny, core.PolicyEvalResultAllow}, "write", nil))
	assert.False(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultAllow, core.PolicyEvalResultDeny}, "read", nil))

	// Error results (fall back to default)
	assert.True(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultError}, "read", nil))
	assert.False(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultError}, "write", nil))
	assert.True(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultDeny, core.PolicyEvalResultError}, "read", nil))

	// With override
	override := map[string]bool{"write": true}
	assert.True(testService.Summerize([]core.PolicyEvalResult{}, "write", &override))
	assert.False(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultDeny}, "write", &override))
	assert.True(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultDefault}, "write", &override))
	assert.True(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultError}, "write", &override))
	assert.True(testService.Summerize([]core.PolicyEvalResult{core.PolicyEvalResultDeny, core.PolicyEvalResultError}, "write", &override))
}

func TestServiceAccumulateOr(t *testing.T) {
	assert := assert.New(t)

	client := mock_client.NewMockClient(gomock.NewController(t))

	// Setup with some global defaults
	var globalPolicy core.Policy
	json.Unmarshal([]byte(`{"defaults": {"read": true, "write": false}}`), &globalPolicy)
	testService := NewService(nil, client, globalPolicy, core.Config{}).(*service)

	// Basic cases
	assert.Equal(core.PolicyEvalResultAllow, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAllow}, "any", nil))
	assert.Equal(core.PolicyEvalResultDeny, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultDeny}, "any", nil))
	assert.Equal(core.PolicyEvalResultDefault, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultDefault}, "any", nil))
	assert.Equal(core.PolicyEvalResultAlways, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways}, "any", nil))
	assert.Equal(core.PolicyEvalResultNever, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultNever}, "any", nil))
	assert.Equal(core.PolicyEvalResultDefault, testService.AccumulateOr([]core.PolicyEvalResult{}, "any", nil))

	// Combinations
	assert.Equal(core.PolicyEvalResultAllow, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAllow, core.PolicyEvalResultDefault}, "any", nil))
	assert.Equal(core.PolicyEvalResultDeny, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultDeny, core.PolicyEvalResultDefault}, "any", nil))
	assert.Equal(core.PolicyEvalResultDefault, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAllow, core.PolicyEvalResultDeny}, "any", nil))

	assert.Equal(core.PolicyEvalResultAlways, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways, core.PolicyEvalResultDefault}, "any", nil))
	assert.Equal(core.PolicyEvalResultNever, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultNever, core.PolicyEvalResultDefault}, "any", nil))
	assert.Equal(core.PolicyEvalResultDefault, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways, core.PolicyEvalResultNever}, "any", nil))

	assert.Equal(core.PolicyEvalResultAlways, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways, core.PolicyEvalResultAllow}, "any", nil))
	assert.Equal(core.PolicyEvalResultAlways, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAlways, core.PolicyEvalResultDeny}, "any", nil))
	assert.Equal(core.PolicyEvalResultNever, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultNever, core.PolicyEvalResultAllow}, "any", nil))
	assert.Equal(core.PolicyEvalResultNever, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultNever, core.PolicyEvalResultDeny}, "any", nil))

	// Error propagation (uses default)
	assert.Equal(core.PolicyEvalResultAllow, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultError}, "read", nil))
	assert.Equal(core.PolicyEvalResultDeny, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultError}, "write", nil))
	assert.Equal(core.PolicyEvalResultDeny, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultError}, "delete", nil)) // No default -> false -> Deny

	assert.Equal(core.PolicyEvalResultDefault, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultDeny, core.PolicyEvalResultError}, "read", nil))
	assert.Equal(core.PolicyEvalResultDefault, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultAllow, core.PolicyEvalResultError}, "write", nil))

	// With override
	override := map[string]bool{"write": true}
	assert.Equal(core.PolicyEvalResultAllow, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultError}, "write", &override))
	assert.Equal(core.PolicyEvalResultDefault, testService.AccumulateOr([]core.PolicyEvalResult{core.PolicyEvalResultDeny, core.PolicyEvalResultError}, "write", &override))
}

// 1. timelineを作れるのはローカルユーザーか、特定のタグを持つユーザー
func TestPolicyTimelineCreatorIsLocalUserOrHasTag(t *testing.T) {

	const policyJson = `
    {
        "statements": {
            "timeline": {
                "condition": {
                    "op": "Or",
                    "args": [
                        {
                            "op": "IsRequesterLocalUser"
                        },
                        {
                            "op": "RequesterHasTag",
                            "const": "timeline_creator"
                        }
                    ]
                }
            }
        }
    }`

	var policy core.Policy
	json.Unmarshal([]byte(policyJson), &policy)

	// Local user, no tag (Allow)
	rctx0 := core.RequestContext{
		Requester: core.Entity{
			Domain: "local.example.com",
		},
	}

	ctx, id := testutil.SetupTraceCtx()
	result, err := s.Test(ctx, policy, rctx0, "timeline")

	test0OK := assert.NoError(t, err)
	test0OK = test0OK && assert.Equal(t, core.PolicyEvalResultAllow, result)

	if !test0OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	// Local user, with tag (Allow)
	rctx1 := core.RequestContext{
		Requester: core.Entity{
			Domain: "local.example.com",
			Tag:    "timeline_creator",
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.Test(ctx, policy, rctx1, "timeline")

	test1OK := assert.NoError(t, err)
	test1OK = test1OK && assert.Equal(t, core.PolicyEvalResultAllow, result)

	if !test1OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	// Remote user, no tag (Deny)
	rctx2 := core.RequestContext{
		Requester: core.Entity{
			Domain: "other.example.net",
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.Test(ctx, policy, rctx2, "timeline")

	test2OK := assert.NoError(t, err)
	test2OK = test2OK && assert.Equal(t, core.PolicyEvalResultDeny, result)

	if !test2OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	// Remote user, with tag (Allow)
	rctx3 := core.RequestContext{
		Requester: core.Entity{
			Domain: "other.example.net",
			Tag:    "timeline_creator",
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.Test(ctx, policy, rctx3, "timeline")

	test3OK := assert.NoError(t, err)
	test3OK = test3OK && assert.Equal(t, core.PolicyEvalResultAllow, result)

	if !test3OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}
}

// 2. messageのread (Table Driven)
func TestPolicyMessageReadTable(t *testing.T) {

	timelinePolicyJson := `
    {
        "statements": {
            "timeline.message.read": {
                "condition": {
                    "op": "Or",
                    "args": [
                        {
                            "op": "LoadParam",
                            "const": "isReadPublic"
                        },
                        {
                            "op": "Contains",
                            "args": [
                                {
                                    "op": "LoadParam",
                                    "const": "reader"
                                },
                                {
                                    "op": "RequesterID"
                                }
                            ]
                        }
                    ]
                }
            }
        }
    }`

	messagePolicyJson := `
    {
        "statements": {
            "message.read": {
                "condition": {
                    "op": "Contains",
                    "args": [
                        {
                            "op": "LoadParam",
                            "const": "reader"
                        },
                        {
                            "op": "RequesterID"
                        }
                    ]
                }
            }
        }
    }`

	testCases := []struct {
		name           string
		policyJson     string // Use "" for global policy
		action         string
		requestContext core.RequestContext
		expectedResult core.PolicyEvalResult
	}{
		{
			name:       "Global: Domain Owned (CSID)",
			policyJson: "",
			action:     "timeline.message.read",
			requestContext: core.RequestContext{
				Requester: core.Entity{Domain: "local.example.com"},
				Self:      core.Timeline{Owner: "ccs16djx38r2qx8j49fx53ewugl90t3y6ndgye8ykt", Author: "user1"},
			},
			expectedResult: core.PolicyEvalResultAllow,
		},
		{
			name:       "Global: Author is Requester",
			policyJson: "",
			action:     "timeline.message.read",
			requestContext: core.RequestContext{
				Requester: core.Entity{ID: "user1", Domain: "local.example.com"},
				Self:      core.Timeline{Owner: "user1", Author: "user1"},
			},
			expectedResult: core.PolicyEvalResultAllow,
		},
		{
			name:       "Global: Not Owner/Author",
			policyJson: "",
			action:     "timeline.message.read",
			requestContext: core.RequestContext{
				Requester: core.Entity{ID: "user2", Domain: "local.example.com"},
				Self:      core.Timeline{Owner: "user1", Author: "user1"},
			},
			expectedResult: core.PolicyEvalResultDeny,
		},
		{
			name:       "Timeline: User in reader list",
			policyJson: timelinePolicyJson,
			action:     "timeline.message.read",
			requestContext: core.RequestContext{
				Requester: core.Entity{ID: "user1", Domain: "local.example.com"},
				Self:      core.Timeline{Owner: "user3", Author: "user3"},
				Params:    map[string]any{"isReadPublic": false, "reader": []any{"user1"}},
			},
			expectedResult: core.PolicyEvalResultAllow,
		},
		{
			name:       "Timeline: User not in reader list",
			policyJson: timelinePolicyJson,
			action:     "timeline.message.read",
			requestContext: core.RequestContext{
				Requester: core.Entity{ID: "user2", Domain: "local.example.com"},
				Self:      core.Timeline{Owner: "user3", Author: "user3"},
				Params:    map[string]any{"isReadPublic": false, "reader": []any{"user1"}},
			},
			expectedResult: core.PolicyEvalResultDeny,
		},
		{
			name:       "Timeline: Read is public",
			policyJson: timelinePolicyJson,
			action:     "timeline.message.read",
			requestContext: core.RequestContext{
				Requester: core.Entity{ID: "user2", Domain: "local.example.com"},
				Self:      core.Timeline{Owner: "user3", Author: "user3"},
				Params:    map[string]any{"isReadPublic": true, "reader": []any{"user1"}},
			},
			expectedResult: core.PolicyEvalResultAllow,
		},
		{
			name:       "Message: User in reader list",
			policyJson: messagePolicyJson,
			action:     "message.read",
			requestContext: core.RequestContext{
				Requester: core.Entity{ID: "user1", Domain: "local.example.com"},
				Self:      core.Message{Author: "user3"},
				Params:    map[string]any{"reader": []any{"user1"}},
			},
			expectedResult: core.PolicyEvalResultAllow,
		},
		{
			name:       "Message: User not in reader list",
			policyJson: messagePolicyJson,
			action:     "message.read",
			requestContext: core.RequestContext{
				Requester: core.Entity{ID: "user2", Domain: "local.example.com"},
				Self:      core.Message{Author: "user3"},
				Params:    map[string]any{"reader": []any{"user1"}},
			},
			expectedResult: core.PolicyEvalResultDeny,
		},
	}

	for _, tc := range testCases {
		tc := tc // Capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert := assert.New(t)
			ctx, id := testutil.SetupTraceCtx()

			var result core.PolicyEvalResult
			var err error

			if tc.policyJson == "" {
				result, err = s.TestWithGlobalPolicy(ctx, tc.requestContext, tc.action)
			} else {
				var policy core.Policy
				jsonErr := json.Unmarshal([]byte(tc.policyJson), &policy)
				if jsonErr != nil {
					t.Fatalf("Failed to unmarshal policy JSON for test case '%s': %v", tc.name, jsonErr)
				}
				result, err = s.Test(ctx, policy, tc.requestContext, tc.action)
			}

			testOK := assert.NoError(err)
			testOK = testOK && assert.Equal(tc.expectedResult, result)

			if !testOK {
				testutil.PrintSpans(checker.GetSpans(), id)
			}
		})
	}
}

// 3. Eval Operators
func TestEvalOperators(t *testing.T) {
	assert := assert.New(t)

	serviceImpl, ok := s.(*service)
	if !ok {
		t.Fatal("Could not cast policy service to implementation type")
	}

	type SampleDoc struct {
		FieldA string `json:"field_a"`
		FieldB int    `json:"field_b"`
	}
	type SampleSelf struct {
		SelfField string `json:"self_field"`
	}
	type SampleResource struct {
		ResourceField bool `json:"resource_field"`
	}

	testCases := []struct {
		name        string
		expr        core.Expr
		requestCtx  core.RequestContext
		expectedVal any
		expectError bool
	}{
		// Logical Operators
		{name: "And: true, true", expr: core.Expr{Operator: "And", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "Const", Constant: true}}}, expectedVal: true},
		{name: "And: true, false", expr: core.Expr{Operator: "And", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "Const", Constant: false}}}, expectedVal: false},
		{name: "And: false, true", expr: core.Expr{Operator: "And", Args: []core.Expr{{Operator: "Const", Constant: false}, {Operator: "Const", Constant: true}}}, expectedVal: false},
		{name: "And: short circuit", expr: core.Expr{Operator: "And", Args: []core.Expr{{Operator: "Const", Constant: false}, {Operator: "LoadParam", Constant: "nonexistent"}}}, expectedVal: false},
		{name: "And: wrong type", expr: core.Expr{Operator: "And", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "Const", Constant: "string"}}}, expectError: true},
		{name: "Or: true, false", expr: core.Expr{Operator: "Or", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "Const", Constant: false}}}, expectedVal: true},
		{name: "Or: false, true", expr: core.Expr{Operator: "Or", Args: []core.Expr{{Operator: "Const", Constant: false}, {Operator: "Const", Constant: true}}}, expectedVal: true},
		{name: "Or: false, false", expr: core.Expr{Operator: "Or", Args: []core.Expr{{Operator: "Const", Constant: false}, {Operator: "Const", Constant: false}}}, expectedVal: false},
		{name: "Or: short circuit", expr: core.Expr{Operator: "Or", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "LoadParam", Constant: "nonexistent"}}}, expectedVal: true},
		{name: "Or: wrong type", expr: core.Expr{Operator: "Or", Args: []core.Expr{{Operator: "Const", Constant: false}, {Operator: "Const", Constant: 123}}}, expectError: true},
		{name: "Not: true", expr: core.Expr{Operator: "Not", Args: []core.Expr{{Operator: "Const", Constant: true}}}, expectedVal: false},
		{name: "Not: false", expr: core.Expr{Operator: "Not", Args: []core.Expr{{Operator: "Const", Constant: false}}}, expectedVal: true},
		{name: "Not: wrong type", expr: core.Expr{Operator: "Not", Args: []core.Expr{{Operator: "Const", Constant: "string"}}}, expectError: true},
		{name: "Not: wrong arg count", expr: core.Expr{Operator: "Not", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "Const", Constant: false}}}, expectError: true},

		// Equality
		{name: "Eq: string match", expr: core.Expr{Operator: "Eq", Args: []core.Expr{{Operator: "Const", Constant: "hello"}, {Operator: "Const", Constant: "hello"}}}, expectedVal: true},
		{name: "Eq: string mismatch", expr: core.Expr{Operator: "Eq", Args: []core.Expr{{Operator: "Const", Constant: "hello"}, {Operator: "Const", Constant: "world"}}}, expectedVal: false},
		{name: "Eq: int match", expr: core.Expr{Operator: "Eq", Args: []core.Expr{{Operator: "Const", Constant: 123}, {Operator: "Const", Constant: 123}}}, expectedVal: true},
		{name: "Eq: int mismatch", expr: core.Expr{Operator: "Eq", Args: []core.Expr{{Operator: "Const", Constant: 123}, {Operator: "Const", Constant: 456}}}, expectedVal: false},
		{name: "Eq: bool match", expr: core.Expr{Operator: "Eq", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "Const", Constant: true}}}, expectedVal: true},
		{name: "Eq: type mismatch", expr: core.Expr{Operator: "Eq", Args: []core.Expr{{Operator: "Const", Constant: "1"}, {Operator: "Const", Constant: 1}}}, expectedVal: false},
		{name: "Eq: wrong arg count", expr: core.Expr{Operator: "Eq", Args: []core.Expr{{Operator: "Const", Constant: 1}}}, expectError: true},

		// Constants
		{name: "Const: string", expr: core.Expr{Operator: "Const", Constant: "test"}, expectedVal: "test"},
		{name: "Const: int", expr: core.Expr{Operator: "Const", Constant: 99}, expectedVal: 99},
		{name: "Const: bool", expr: core.Expr{Operator: "Const", Constant: false}, expectedVal: false},

		// Contains
		{name: "Contains: string found", expr: core.Expr{Operator: "Contains", Args: []core.Expr{{Operator: "Const", Constant: []any{"a", "b", "c"}}, {Operator: "Const", Constant: "b"}}}, expectedVal: true},
		{name: "Contains: string not found", expr: core.Expr{Operator: "Contains", Args: []core.Expr{{Operator: "Const", Constant: []any{"a", "b", "c"}}, {Operator: "Const", Constant: "d"}}}, expectedVal: false},
		{name: "Contains: int found", expr: core.Expr{Operator: "Contains", Args: []core.Expr{{Operator: "Const", Constant: []any{1, 2, 3}}, {Operator: "Const", Constant: 2}}}, expectedVal: true},
		{name: "Contains: wrong list type", expr: core.Expr{Operator: "Contains", Args: []core.Expr{{Operator: "Const", Constant: "not a list"}, {Operator: "Const", Constant: "a"}}}, expectError: true},
		{name: "Contains: wrong arg count", expr: core.Expr{Operator: "Contains", Args: []core.Expr{{Operator: "Const", Constant: []any{}}}}, expectError: true},

		// Loaders
		{name: "LoadParam: simple", expr: core.Expr{Operator: "LoadParam", Constant: "user"}, requestCtx: core.RequestContext{Params: map[string]any{"user": "xyz"}}, expectedVal: "xyz"},
		{name: "LoadParam: nested", expr: core.Expr{Operator: "LoadParam", Constant: "data.value"}, requestCtx: core.RequestContext{Params: map[string]any{"data": map[string]any{"value": "nested_string"}}}, expectedVal: "nested_string"},
		{name: "LoadParam: not found", expr: core.Expr{Operator: "LoadParam", Constant: "missing"}, requestCtx: core.RequestContext{Params: map[string]any{"user": "xyz"}}, expectError: true},
		{name: "LoadDocument: simple", expr: core.Expr{Operator: "LoadDocument", Constant: "field_a"}, requestCtx: core.RequestContext{Document: SampleDoc{FieldA: "doc_val", FieldB: 1}}, expectedVal: "doc_val"},
		{name: "LoadDocument: not found", expr: core.Expr{Operator: "LoadDocument", Constant: "field_c"}, requestCtx: core.RequestContext{Document: SampleDoc{FieldA: "doc_val", FieldB: 1}}, expectError: true},
		{name: "LoadSelf: simple", expr: core.Expr{Operator: "LoadSelf", Constant: "self_field"}, requestCtx: core.RequestContext{Self: SampleSelf{SelfField: "self_val"}}, expectedVal: "self_val"},
		{name: "LoadSelf: not found", expr: core.Expr{Operator: "LoadSelf", Constant: "missing"}, requestCtx: core.RequestContext{Self: SampleSelf{SelfField: "self_val"}}, expectError: true},
		{name: "LoadResource: simple", expr: core.Expr{Operator: "LoadResource", Constant: "resource_field"}, requestCtx: core.RequestContext{Resource: SampleResource{ResourceField: true}}, expectedVal: true},
		{name: "LoadResource: not found", expr: core.Expr{Operator: "LoadResource", Constant: "missing"}, requestCtx: core.RequestContext{Resource: SampleResource{ResourceField: true}}, expectError: true},

		// Domain Info
		{name: "DomainFQDN", expr: core.Expr{Operator: "DomainFQDN"}, expectedVal: "local.example.com"},
		{name: "DomainCSID", expr: core.Expr{Operator: "DomainCSID"}, expectedVal: "ccs16djx38r2qx8j49fx53ewugl90t3y6ndgye8ykt"},

		// ID Checks
		{name: "IsCCID: valid", expr: core.Expr{Operator: "IsCCID", Args: []core.Expr{{Operator: "Const", Constant: "con1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}}}, expectedVal: true},
		{name: "IsCCID: invalid", expr: core.Expr{Operator: "IsCCID", Args: []core.Expr{{Operator: "Const", Constant: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}}}, expectedVal: false},
		{name: "IsCSID: valid", expr: core.Expr{Operator: "IsCSID", Args: []core.Expr{{Operator: "Const", Constant: "ccs1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}}}, expectedVal: true},
		{name: "IsCSID: invalid", expr: core.Expr{Operator: "IsCSID", Args: []core.Expr{{Operator: "Const", Constant: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}}}, expectedVal: false},
		{name: "IsCKID: valid", expr: core.Expr{Operator: "IsCKID", Args: []core.Expr{{Operator: "Const", Constant: "cck1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}}}, expectedVal: true},
		{name: "IsCKID: invalid", expr: core.Expr{Operator: "IsCKID", Args: []core.Expr{{Operator: "Const", Constant: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}}}, expectedVal: false},
		{name: "IsCCID: wrong type", expr: core.Expr{Operator: "IsCCID", Args: []core.Expr{{Operator: "Const", Constant: 123}}}, expectError: true},

		// Requester Checks
		{name: "IsRequesterLocalUser: true", expr: core.Expr{Operator: "IsRequesterLocalUser"}, requestCtx: core.RequestContext{Requester: core.Entity{Domain: "local.example.com"}}, expectedVal: true},
		{name: "IsRequesterLocalUser: false", expr: core.Expr{Operator: "IsRequesterLocalUser"}, requestCtx: core.RequestContext{Requester: core.Entity{Domain: "remote.example.com"}}, expectedVal: false},
		{name: "IsRequesterRemoteUser: true", expr: core.Expr{Operator: "IsRequesterRemoteUser"}, requestCtx: core.RequestContext{Requester: core.Entity{Domain: "remote.example.com"}}, expectedVal: true},
		{name: "IsRequesterRemoteUser: false", expr: core.Expr{Operator: "IsRequesterRemoteUser"}, requestCtx: core.RequestContext{Requester: core.Entity{Domain: "local.example.com"}}, expectedVal: false},
		{name: "IsRequesterGuestUser: true", expr: core.Expr{Operator: "IsRequesterGuestUser"}, requestCtx: core.RequestContext{Requester: core.Entity{ID: ""}}, expectedVal: true},
		{name: "IsRequesterGuestUser: false", expr: core.Expr{Operator: "IsRequesterGuestUser"}, requestCtx: core.RequestContext{Requester: core.Entity{ID: "user1"}}, expectedVal: false},
		{name: "RequesterHasTag: true", expr: core.Expr{Operator: "RequesterHasTag", Constant: "admin"}, requestCtx: core.RequestContext{Requester: core.Entity{Tag: "admin,beta"}}, expectedVal: true},
		{name: "RequesterHasTag: false", expr: core.Expr{Operator: "RequesterHasTag", Constant: "root"}, requestCtx: core.RequestContext{Requester: core.Entity{Tag: "admin,beta"}}, expectedVal: false},
		{name: "RequesterID", expr: core.Expr{Operator: "RequesterID"}, requestCtx: core.RequestContext{Requester: core.Entity{ID: "user123"}}, expectedVal: "user123"},
		{name: "RequesterDomainHasTag: true", expr: core.Expr{Operator: "RequesterDomainHasTag", Constant: "blocked"}, requestCtx: core.RequestContext{RequesterDomain: core.Domain{Tag: "blocked,test"}}, expectedVal: true},
		{name: "RequesterDomainHasTag: false", expr: core.Expr{Operator: "RequesterDomainHasTag", Constant: "trusted"}, requestCtx: core.RequestContext{RequesterDomain: core.Domain{Tag: "blocked,test"}}, expectedVal: false},

		// Unknown Operator
		{name: "Unknown Operator", expr: core.Expr{Operator: "DoesNotExist"}, expectError: true},

		// --- Ternary Cond Operator ---
		{name: "Cond: condition true", expr: core.Expr{Operator: "Cond", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "Const", Constant: "result_true"}, {Operator: "Const", Constant: "result_false"}}}, expectedVal: "result_true"},
		{name: "Cond: condition false", expr: core.Expr{Operator: "Cond", Args: []core.Expr{{Operator: "Const", Constant: false}, {Operator: "Const", Constant: "result_true"}, {Operator: "Const", Constant: "result_false"}}}, expectedVal: "result_false"},
		{name: "Cond: condition error", expr: core.Expr{Operator: "Cond", Args: []core.Expr{{Operator: "LoadParam", Constant: "missing"}, {Operator: "Const", Constant: "result_true"}, {Operator: "Const", Constant: "result_false"}}}, expectError: true},
		{name: "Cond: condition not bool", expr: core.Expr{Operator: "Cond", Args: []core.Expr{{Operator: "Const", Constant: "not_bool"}, {Operator: "Const", Constant: "result_true"}, {Operator: "Const", Constant: "result_false"}}}, expectError: true},
		{name: "Cond: true branch error", expr: core.Expr{Operator: "Cond", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "LoadParam", Constant: "missing"}, {Operator: "Const", Constant: "result_false"}}}, expectError: true},
		{name: "Cond: false branch error", expr: core.Expr{Operator: "Cond", Args: []core.Expr{{Operator: "Const", Constant: false}, {Operator: "Const", Constant: "result_true"}, {Operator: "LoadParam", Constant: "missing"}}}, expectError: true},
		{name: "Cond: wrong arg count (2)", expr: core.Expr{Operator: "Cond", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "Const", Constant: "result_true"}}}, expectError: true},
		{name: "Cond: wrong arg count (4)", expr: core.Expr{Operator: "Cond", Args: []core.Expr{{Operator: "Const", Constant: true}, {Operator: "Const", Constant: "result_true"}, {Operator: "Const", Constant: "result_false"}, {Operator: "Const", Constant: "extra"}}}, expectError: true},
	}

	for _, tc := range testCases {
		tc := tc // Capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel() // Restore parallel execution
			// Use assert from outer scope
			// t.Log("Running test case:", tc.name) // Remove logging
			evalResult, err := serviceImpl.eval(t.Context(), tc.expr, tc.requestCtx)

			if tc.expectError {
				assert.Error(err)
				assert.NotEmpty(evalResult.Error) // Check that the error message is populated in the result
			} else {
				assert.NoError(err)
				assert.Empty(evalResult.Error) // Check that the error message is empty
				assert.Equal(tc.expectedVal, evalResult.Result)
			}
		})
	}
}
