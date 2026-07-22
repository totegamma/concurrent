package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolicy(t *testing.T) {

	rctx := RequestContext{
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

	result, err := Eval(context.Background(), rctx, expr)
	assert.NoError(t, err)
	assert.Equal(t, true, result.Result)

}

// fakeCaller records the arguments of the last ConcrntCall and returns the
// configured result/err.
type fakeCaller struct {
	result any
	err    error

	gotResolver string
	gotAPI      string
	gotParams   map[string]string
}

func (f *fakeCaller) ConcrntCall(ctx context.Context, resolver string, api string, params map[string]string) (any, error) {
	f.gotResolver = resolver
	f.gotAPI = api
	f.gotParams = params
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestConcrntCallOperator(t *testing.T) {

	caller := &fakeCaller{
		result: []any{map[string]any{"document": `{"kind":"ack"}`}},
	}

	rctx := RequestContext{
		Requester: map[string]any{"ccid": "con1bob"},
		Caller:    caller,
	}

	expr := Expr{
		Operator: "IsNotEmpty",
		Args: []Expr{
			{
				Operator: "ConcrntCall",
				Args: []Expr{
					{Operator: "Const", Const: "con1alice"},
					{Operator: "Const", Const: "net.concrnt.core.acknowledges"},
					{Operator: "Const", Const: "from"},
					{Operator: "Const", Const: "con1alice"},
					{Operator: "Const", Const: "to"},
					{Operator: "Load", Const: "requester.ccid"},
				},
			},
		},
	}

	result, err := Eval(context.Background(), rctx, expr)
	assert.NoError(t, err)
	assert.Equal(t, true, result.Result)

	assert.Equal(t, "con1alice", caller.gotResolver)
	assert.Equal(t, "net.concrnt.core.acknowledges", caller.gotAPI)
	assert.Equal(t, map[string]string{"from": "con1alice", "to": "con1bob"}, caller.gotParams)
}

func TestConcrntCallWithoutCaller(t *testing.T) {

	expr := Expr{
		Operator: "ConcrntCall",
		Args: []Expr{
			{Operator: "Const", Const: "con1alice"},
			{Operator: "Const", Const: "net.concrnt.core.acknowledges"},
		},
	}

	_, err := Eval(context.Background(), RequestContext{}, expr)
	assert.Error(t, err)
}

func TestConcrntCallBadArgs(t *testing.T) {

	rctx := RequestContext{Caller: &fakeCaller{}}

	// too few args
	_, err := Eval(context.Background(), rctx, Expr{
		Operator: "ConcrntCall",
		Args: []Expr{
			{Operator: "Const", Const: "con1alice"},
		},
	})
	assert.Error(t, err)

	// odd number of param args
	_, err = Eval(context.Background(), rctx, Expr{
		Operator: "ConcrntCall",
		Args: []Expr{
			{Operator: "Const", Const: "con1alice"},
			{Operator: "Const", Const: "net.concrnt.core.acknowledges"},
			{Operator: "Const", Const: "from"},
		},
	})
	assert.Error(t, err)

	// non-string api
	_, err = Eval(context.Background(), rctx, Expr{
		Operator: "ConcrntCall",
		Args: []Expr{
			{Operator: "Const", Const: "con1alice"},
			{Operator: "Const", Const: true},
		},
	})
	assert.Error(t, err)
}

func TestIsNotEmpty(t *testing.T) {

	cases := []struct {
		name     string
		arg      any
		expected any
		isError  bool
	}{
		{name: "nil", arg: nil, expected: false},
		{name: "empty slice", arg: []any{}, expected: false},
		{name: "non-empty slice", arg: []any{"x"}, expected: true},
		{name: "empty string", arg: "", expected: false},
		{name: "non-empty string", arg: "x", expected: true},
		{name: "empty map", arg: map[string]any{}, expected: false},
		{name: "non-empty map", arg: map[string]any{"k": "v"}, expected: true},
		{name: "unsupported type", arg: 42, isError: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := Eval(context.Background(), RequestContext{}, Expr{
				Operator: "IsNotEmpty",
				Args: []Expr{
					{Operator: "Const", Const: c.arg},
				},
			})
			if c.isError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, c.expected, result.Result)
		})
	}
}

// A statement whose ConcrntCall condition fails (network error, disallowed
// api, ...) must contribute nothing, so the policy's own defaults apply —
// fail-closed for a restriction policy declaring ng defaults.
func TestEvaluateStackConcrntCallErrorFallsToDefault(t *testing.T) {

	restrictPolicy := Policy{
		Statements: []Statement{
			{
				Action: "record:read",
				Key:    "*",
				Emit:   OK,
				Condition: Expr{
					Operator: "IsNotEmpty",
					Args: []Expr{
						{
							Operator: "ConcrntCall",
							Args: []Expr{
								{Operator: "Const", Const: "con1alice"},
								{Operator: "Const", Const: "net.concrnt.core.acknowledges"},
								{Operator: "Const", Const: "from"},
								{Operator: "Const", Const: "con1alice"},
								{Operator: "Const", Const: "to"},
								{Operator: "Const", Const: "con1bob"},
							},
						},
					},
				},
			},
		},
		Defaults: map[string]Conclusion{
			"record:read": NG,
		},
	}

	stack := PolicyStack{
		{
			{Policy: restrictPolicy},
		},
	}

	// caller errors -> statement contributes nothing -> defaults: ng
	rctx := RequestContext{Caller: &fakeCaller{err: assert.AnError}}
	conclusion, _, err := EvaluateStack(context.Background(), rctx, stack, "record:read", "cckv://con1alice/key")
	assert.NoError(t, err)
	assert.Equal(t, NG, conclusion)

	// caller returns an ack -> statement emits ok
	rctx = RequestContext{Caller: &fakeCaller{result: []any{map[string]any{"document": `{"kind":"ack"}`}}}}
	conclusion, _, err = EvaluateStack(context.Background(), rctx, stack, "record:read", "cckv://con1alice/key")
	assert.NoError(t, err)
	assert.Equal(t, OK, conclusion)

	// caller returns no acks -> condition false -> defaults: ng
	rctx = RequestContext{Caller: &fakeCaller{result: []any{}}}
	conclusion, _, err = EvaluateStack(context.Background(), rctx, stack, "record:read", "cckv://con1alice/key")
	assert.NoError(t, err)
	assert.Equal(t, NG, conclusion)
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

// A statement's reason must survive into EvaluateStack's aggregated reason
// string, so denials can be explained to the caller.
func TestEvaluateStackAggregatesReasons(t *testing.T) {

	reason := "blocked by moderation policy"
	denyPolicy := Policy{
		Statements: []Statement{
			{
				Action:    "record:read",
				Key:       "*",
				Emit:      DENY,
				Reason:    &reason,
				Condition: Expr{Operator: "Const", Const: true},
			},
		},
	}

	stack := PolicyStack{
		{
			{Policy: denyPolicy},
		},
	}

	conclusion, gotReason, err := EvaluateStack(context.Background(), RequestContext{}, stack, "record:read", "cckv://owner/key")
	assert.NoError(t, err)
	assert.Equal(t, DENY, conclusion)
	assert.Contains(t, gotReason, reason)
}

// CIP-12 §5.3/§6.3: the stack evaluates global → root → … → self. Strong
// conclusions (allow/deny) are first-wins, so the outer (global-side) layer
// wins; weak conclusions (ok/ng) are last-wins, so the layer closest to the
// resource itself wins.
func TestEvaluateStackOrderSemantics(t *testing.T) {
	alwaysTrue := Expr{Operator: "IsNotEmpty", Args: []Expr{{Operator: "Const", Const: "x"}}}
	emit := func(c Conclusion) Policy {
		return Policy{Statements: []Statement{{Action: "record:read", Key: "*", Emit: c, Condition: alwaysTrue}}}
	}

	t.Run("weak ng on the ancestor loses to weak ok on self", func(t *testing.T) {
		stack := PolicyStack{
			{{Policy: emit(NG)}}, // ancestor (outer)
			{{Policy: emit(OK)}}, // the resource itself (last)
		}
		conclusion, _, err := EvaluateStack(context.Background(), RequestContext{}, stack, "record:read", "cckv://owner/key")
		assert.NoError(t, err)
		assert.Equal(t, OK, conclusion)
	})

	t.Run("strong deny on the outer layer beats allow on self", func(t *testing.T) {
		stack := PolicyStack{
			{{Policy: emit(DENY)}},  // global (first)
			{{Policy: emit(ALLOW)}}, // the resource itself
		}
		conclusion, _, err := EvaluateStack(context.Background(), RequestContext{}, stack, "record:read", "cckv://owner/key")
		assert.NoError(t, err)
		assert.Equal(t, DENY, conclusion)
	})
}
