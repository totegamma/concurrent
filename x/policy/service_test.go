package policy

import (
	"context"
	"github.com/stretchr/testify/assert"
	"testing"

	"github.com/totegamma/concurrent/core"
)

var s service
var ctx = context.Background()

func TestMain(m *testing.M) {

	s = service{
		config: core.Config{},
	}

	m.Run()
}

// 1. timelineを作れるのはローカルユーザーか、特定のタグを持つユーザー (Globalレベル想定)
func TestPolicyTimelineCreatorIsLocalUserOrHasTag(t *testing.T) {

	policy := core.Policy{
		Name:    "StreamCreatorIsLocalUserOrHasTag",
		Version: "2024-05-01",
		Statements: []core.Statement{
			{
				Actions: []string{"timeline"},
				Effect:  "allow", // allow: これにマッチしなければ拒否
				Condition: core.Expr{
					Operator: "Or",
					Args: []core.Expr{
						{
							Operator: "IsRequesterLocalUser",
						},
						{
							Operator: "RequesterHasTag",
							Args: []core.Expr{
								{
									Operator: "Const",
									Constant: "timeline_creator",
								},
							},
						},
					},
				},
			},
		},
	}

	context := core.RequestContext{}

	canPerform, err := s.Test(ctx, policy, context, "timeline")
	assert.NoError(t, err)
	assert.True(t, canPerform)
}

// 2. timelineに投稿・閲覧できるのは特定のユーザー (timelineレベル想定)
func TestPolicyTimelineLimitAccess(t *testing.T) {

	policy := core.Policy{
		Name:    "StreamLimitAccess",
		Version: "2024-05-01",
		Statements: []core.Statement{
			{
				Actions: []string{"distribute", "GET:/message/*"},
				Effect:  "allow",
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
				Effect:  "allow",
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
				Effect:  "allow",
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
