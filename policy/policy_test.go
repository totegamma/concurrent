package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolicy(t *testing.T) {

	ctx := RequestContext{
		Params: map[string]any{
			"user": "alice",
			"role": "admin",
		},
	}

	expr := Expr{
		Operator: "Eq",
		Args: []Expr{
			{
				Operator: "Load",
				Const:    "params.role",
			},
			{
				Operator: "Const",
				Const:    "admin",
			},
		},
	}

	result, err := Eval(ctx, expr)
	assert.NoError(t, err)
	assert.Equal(t, true, result.Result)

}

// An errored evaluation set (its referenced policy couldn't be resolved)
// must fold to the entry's declared per-action default conclusion, and
// contribute nothing for actions without a declared default.
func TestEvaluateStackErroredFallsBackToDefaults(t *testing.T) {

	stack := PolicyStack{
		{
			{
				Errored: true,
				Policy: Policy{Defaults: map[string]Conclusion{
					"read":  DENY,
					"write": ALLOW,
				}},
			},
		},
	}

	conclusion, _, err := EvaluateStack(context.Background(), RequestContext{}, stack, "read", "cckv://owner/key")
	assert.NoError(t, err)
	assert.Equal(t, DENY, conclusion)

	conclusion, _, err = EvaluateStack(context.Background(), RequestContext{}, stack, "write", "cckv://owner/key")
	assert.NoError(t, err)
	assert.Equal(t, ALLOW, conclusion)

	conclusion, _, err = EvaluateStack(context.Background(), RequestContext{}, stack, "delete", "cckv://owner/key")
	assert.NoError(t, err)
	assert.Equal(t, UNSET, conclusion)
}
