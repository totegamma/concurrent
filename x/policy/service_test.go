package policy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/internal/testutil"
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

	s = NewService(
		repository,
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

	// ローカルユーザー かつ タグ無し (成功)
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

	// ローカルユーザー かつ タグあり (成功)
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

	// ローカルユーザーでない かつ タグ無し (失敗)
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

	// ローカルユーザーでない かつ タグあり (成功)
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

// 2. messageのread
func TestPolicyMessageRead(t *testing.T) {
	// globalでの処理
	// domainOwnedのとき: 誰でも読める
	// domainOwnedでないとき: timelineのcreatorに限る

	rctx0 := core.RequestContext{
		Requester: core.Entity{
			Domain: "local.example.com",
		},
		Self: core.Timeline{
			Owner:  "ccs16djx38r2qx8j49fx53ewugl90t3y6ndgye8ykt",
			Author: "user1",
		},
	}

	ctx, id := testutil.SetupTraceCtx()
	result, err := s.TestWithGlobalPolicy(ctx, rctx0, "timeline.message.read")
	test0OK := assert.NoError(t, err)
	test0OK = test0OK && assert.Equal(t, core.PolicyEvalResultAllow, result)

	if !test0OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	rctx1 := core.RequestContext{
		Requester: core.Entity{
			ID:     "user1",
			Domain: "local.example.com",
		},
		Self: core.Timeline{
			Owner:  "user1",
			Author: "user1",
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.TestWithGlobalPolicy(ctx, rctx1, "timeline.message.read")
	test1OK := assert.NoError(t, err)
	test1OK = test1OK && assert.Equal(t, core.PolicyEvalResultAllow, result)

	if !test1OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	rctx2 := core.RequestContext{
		Requester: core.Entity{
			ID:     "user2",
			Domain: "local.example.com",
		},
		Self: core.Timeline{
			Owner:  "user1",
			Author: "user1",
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.TestWithGlobalPolicy(ctx, rctx2, "timeline.message.read")
	test2OK := assert.NoError(t, err)
	test2OK = test2OK && assert.Equal(t, core.PolicyEvalResultDeny, result)

	if !test2OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	// timelineでのチェック
	// リストにあれば許可

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

	var timelinePolicy core.Policy
	err = json.Unmarshal([]byte(timelinePolicyJson), &timelinePolicy)
	if err != nil {
		panic(err)
	}

	rctx3 := core.RequestContext{
		Requester: core.Entity{
			ID:     "user1",
			Domain: "local.example.com",
		},
		Self: core.Timeline{
			Owner:  "user3",
			Author: "user3",
		},
		Params: map[string]any{
			"isWritePublic": false,
			"isReadPublic":  false,
			"reader":        []any{"user1"},
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.Test(ctx, timelinePolicy, rctx3, "timeline.message.read")
	test3OK := assert.NoError(t, err)
	test3OK = test3OK && assert.Equal(t, core.PolicyEvalResultAllow, result)

	if !test3OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	rctx4 := core.RequestContext{
		Requester: core.Entity{
			ID:     "user2",
			Domain: "local.example.com",
		},
		Self: core.Timeline{
			Owner:  "user3",
			Author: "user3",
		},
		Params: map[string]any{
			"isWritePublic": false,
			"isReadPublic":  false,
			"reader":        []any{"user1"},
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.Test(ctx, timelinePolicy, rctx4, "timeline.message.read")
	test4OK := assert.NoError(t, err)
	test4OK = test4OK && assert.Equal(t, core.PolicyEvalResultDeny, result)

	if !test4OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	// messageでのチェック
	// リストにあれば許可

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

	var messagePolicy core.Policy
	err = json.Unmarshal([]byte(messagePolicyJson), &messagePolicy)
	if err != nil {
		panic(err)
	}

	rctx5 := core.RequestContext{
		Requester: core.Entity{
			ID:     "user1",
			Domain: "local.example.com",
		},
		Self: core.Message{
			Author: "user3",
		},
		Params: map[string]any{
			"reader": []any{"user1"},
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.Test(ctx, messagePolicy, rctx5, "message.read")
	test5OK := assert.NoError(t, err)
	test5OK = test5OK && assert.Equal(t, core.PolicyEvalResultAllow, result)

	if !test5OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}

	rctx6 := core.RequestContext{
		Requester: core.Entity{
			ID:     "user2",
			Domain: "local.example.com",
		},
		Self: core.Message{
			Author: "user3",
		},
		Params: map[string]any{
			"reader": []any{"user1"},
		},
	}

	ctx, id = testutil.SetupTraceCtx()
	result, err = s.Test(ctx, messagePolicy, rctx6, "message.read")
	test6OK := assert.NoError(t, err)
	test6OK = test6OK && assert.Equal(t, core.PolicyEvalResultDeny, result)

	if !test6OK {
		testutil.PrintSpans(checker.GetSpans(), id)
	}
}
