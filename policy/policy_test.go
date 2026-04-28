package policy

import (
	"github.com/stretchr/testify/assert"
	"testing"
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
