package policy

import (
	"github.com/stretchr/testify/assert"
	"testing"

	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/util"
)

var s service

func TestMain(m *testing.M) {

	s = service{
		config: util.Config{},
	}

	m.Run()
}

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
					Operator: "Or",
					Args: []Expr{
						{
							Operator: "IsRequesterLocalUser",
						},
						{
							Operator: "RequesterHasTag",
							Args: []Expr{
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

	context := RequestContext{}

	canPerform, err := s.Test(policy, context, "timeline")
	assert.NoError(t, err)
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
					Operator: "Contains",
					Args: []Expr{
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

	context := RequestContext{
		Requester: core.Entity{
			ID: "user1",
		},
		Params: map[string]any{
			"allowlist": []string{"user1", "user2"},
		},
	}

	canDistribute, err := s.Test(policy, context, "distribute")
	assert.NoError(t, err)
	assert.True(t, canDistribute)

	canRead, err := s.Test(policy, context, "GET:/message/msneb1k006zqtpqsg067jyebxtm")
	assert.NoError(t, err)
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
					Operator: "Contains",
					Args: []Expr{
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

	document := core.CreateMessage[any]{
		DocumentBase: core.DocumentBase[any]{
			Schema: "schema1",
		},
	}

	context := RequestContext{
		Params: map[string]any{
			"allowlist": []string{"schema1", "schema2"},
		},
		Document: document,
	}

	canPerform, err := s.Test(policy, context, "distribute")
	assert.NoError(t, err)
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
					Operator: "Contains",
					Args: []Expr{
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

	document := core.CreateAssociation[any]{
		DocumentBase: core.DocumentBase[any]{
			Schema: "schema1",
		},
	}

	context := RequestContext{
		Params: map[string]any{
			"allowlist": []string{"schema1", "schema2"},
		},
		Document: document,
	}

	canPerform, err := s.Test(policy, context, "association")
	assert.NoError(t, err)
	assert.True(t, canPerform)
}
