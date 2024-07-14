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
                        "op": "LoadSelf",
                        "const": "domainOwned"
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
			DomainOwned: true,
			Author:      "user1",
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
			DomainOwned: false,
			Author:      "user1",
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
			DomainOwned: false,
			Author:      "user1",
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

	// messageでのチェック
	// リストにあれば許可
}

/*
// 2. timelineに投稿・閲覧できるのは特定のユーザー (timelineレベル想定)
func TestPolicyTimelineLimitAccess(t *testing.T) {

	policy := core.Policy{
		Name:    "StreamLimitAccess",
		Version: "2024-05-01",
		Statements: []core.Statement{
			{
				Actions: []string{"distribute", "GET:/message/*"},
				Condition: core.Expr{
					Operator: "Contains",
					Args: []core.Expr{
						{
							Operator: "LoadParam",
							Constant: "allowlist",
						},
						{
							Operator: "RequesterID",
						},
					},
				},
			},
		},
	}

	context := core.RequestContext{
		Requester: core.Entity{
			ID: "user1",
		},
		Params: map[string]any{
			"allowlist": []any{"user1", "user2"},
		},
	}

	canDistribute, err := s.Test(ctx, policy, context, "distribute")
	assert.NoError(t, err)
	assert.True(t, canDistribute)

	canRead, err := s.Test(ctx, policy, context, "GET:/message/msneb1k006zqtpqsg067jyebxtm")
	assert.NoError(t, err)
	assert.True(t, canRead)
}

// 3. timelineに投稿できるのは特定のschema (timelineレベル想定)
func TestPolicyTimelineLimitMessageSchema(t *testing.T) {

	policy := core.Policy{
		Name:    "StreamLimitMessageSchema",
		Version: "2024-05-01",
		Statements: []core.Statement{
			{
				Actions: []string{"distribute"},
				Condition: core.Expr{
					Operator: "Contains",
					Args: []core.Expr{
						{
							Operator: "LoadParam",
							Constant: "allowlist",
						},
						{
							Operator: "LoadDocument",
							Constant: "schema",
						},
					},
				},
			},
		},
	}

	document := core.MessageDocument[any]{
		DocumentBase: core.DocumentBase[any]{
			Schema: "schema1",
		},
	}

	context := core.RequestContext{
		Params: map[string]any{
			"allowlist": []any{"schema1", "schema2"},
		},
		Document: document,
	}

	canPerform, err := s.Test(ctx, policy, context, "distribute")
	assert.NoError(t, err)
	assert.True(t, canPerform)
}

// 4. このメッセージに対してのアクションは特定のスキーマのみ (メッセージレベル想定)
func TestPolicyMessageLimitAction(t *testing.T) {

	policy := core.Policy{
		Name:    "MessageLimitAction",
		Version: "2024-05-01",
		Statements: []core.Statement{
			{
				Actions: []string{"association"},
				Condition: core.Expr{
					Operator: "Contains",
					Args: []core.Expr{
						{
							Operator: "LoadParam",
							Constant: "allowlist",
						},
						{
							Operator: "LoadDocument",
							Constant: "schema",
						},
					},
				},
			},
		},
	}

	document := core.AssociationDocument[any]{
		DocumentBase: core.DocumentBase[any]{
			Schema: "schema1",
		},
	}

	context := core.RequestContext{
		Params: map[string]any{
			"allowlist": []any{"schema1", "schema2"},
		},
		Document: document,
	}

	canPerform, err := s.Test(ctx, policy, context, "association")
	assert.NoError(t, err)
	assert.True(t, canPerform)
}
*/
