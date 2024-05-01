package policy

import (
	"github.com/stretchr/testify/assert"
	"testing"

	"github.com/totegamma/concurrent/core"
)

// 1. timelineを作れるのはローカルユーザーか、特定のタグを持つユーザー (Globalレベル想定)
func TestPolicyTimelineCreatorIsLocalUserOrHasTag(t *testing.T) {

	policy := Policy{
		Name:    "StreamCreatorIsLocalUserOrHasTag",
		Version: "2024-05-01",
		Statement: []Statement{
			{
				Action: []string{"timeline"},
				Effect: "allow", // allow: これにマッチしなければ拒否
				Condition: Expr{
					Operator: "OR",
					Args: []Expr{
						{
							Operator: "IsLocalUser",
						},
						{
							Operator: "HasTag",
							Args: []Expr{
								{
									Operator: "CONST",
									Constant: "timeline_creator",
								},
							},
						},
					},
				},
			},
		},
	}

	context := RequestContext{}

	canPerform := Test(policy, context, "timeline")
	assert.True(t, canPerform)
}

// 2. timelineに投稿・閲覧できるのは特定のユーザー (timelineレベル想定)
func TestPolicyTimelineLimitAccess(t *testing.T) {

	policy := Policy{
		Name:    "StreamLimitAccess",
		Version: "2024-05-01",
		Statement: []Statement{
			{
				Action: []string{"distribute", "GET:/message/*"},
				Effect: "allow",
				Condition: Expr{
					Operator: "CONTAINS",
					Args: []Expr{
						{
							Operator: "LOADSTRING",
							Constant: "requester.ID",
						},
						{
							Operator: "LOADSTRINGARR",
							Constant: "parameter.allowlist",
						},
					},
				},
			},
		},
	}

	context := RequestContext{
		Requester: core.Entity{
			ID: "user1",
		},
		Params: map[string]any{
			"allowlist": []string{"user1", "user2"},
		},
	}

	canDistribute := Test(policy, context, "distribute")
	assert.True(t, canDistribute)

	canRead := Test(policy, context, "GET:/message/*")
	assert.True(t, canRead)
}

// 3. timelineに投稿できるのは特定のschema (timelineレベル想定)
func TestPolicyTimelineLimitMessageSchema(t *testing.T) {

	policy := Policy{
		Name:    "StreamLimitMessageSchema",
		Version: "2024-05-01",
		Statement: []Statement{
			{
				Action: []string{"distribute"},
				Effect: "allow",
				Condition: Expr{
					Operator: "CONTAINS",
					Args: []Expr{
						{
							Operator: "LOADSTRING",
							Constant: "resource.schema",
						},
						{
							Operator: "LOADSTRINGARR",
							Constant: "params.allowlist",
						},
					},
				},
			},
		},
	}

	context := RequestContext{
		Params: map[string]any{
			"allowlist": []string{"schema1", "schema2"},
		},
	}

	canPerform := Test(policy, context, "distribute")
	assert.True(t, canPerform)
}

// 4. このメッセージに対してのアクションは特定のスキーマのみ (メッセージレベル想定)
func TestPolicyMessageLimitAction(t *testing.T) {

	policy := Policy{
		Name:    "MessageLimitAction",
		Version: "2024-05-01",
		Statement: []Statement{
			{
				Action: []string{"association"},
				Effect: "allow",
				Condition: Expr{
					Operator: "CONTAINS",
					Args: []Expr{
						{
							Operator: "LOADSTRING",
							Constant: "resource.schema",
						},
						{
							Operator: "LOADSTRINGARR",
							Constant: "params.allowlist",
						},
					},
				},
			},
		},
	}

	context := RequestContext{
		Params: map[string]any{
			"allowlist": []string{"schema1", "schema2"},
		},
	}

	canPerform := Test(policy, context, "GET:/message/123")
	assert.True(t, canPerform)
}
