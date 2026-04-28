package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func SummerizeConclusion(conclusions []Conclusion, defaultAllow bool) bool {
	result := UNSET
	for _, c := range conclusions {
		switch c {
		case ALLOW:
			return true
		case DENY:
			return false
		default:
			result = result.Or(c)
		}
	}
	if result == UNSET {
		return defaultAllow
	}
	return result == ALLOW
}

func EvaluateStack(ctx context.Context, req RequestContext, stack PolicyStack, action string, key string) (Conclusion, error) {
	ctx, span := tracer.Start(ctx, "Policy.EvaluateStack")
	defer span.End()

	span.SetAttributes(attribute.Int("policy.stack.layers", len(stack)))

	conclusion := UNSET

	for _, layer := range stack {

		layerConclusion := UNSET
		for _, evalSet := range layer {

			if evalSet.Errored {
				if evalSet.Defaults == nil {
					continue
				}
				defaults := *evalSet.Defaults
				result, ok := defaults[action]
				if ok {
					layerConclusion = layerConclusion.Or(Conclusion(result))
				}
				continue
			}

			reqCtx := req
			if evalSet.Params != nil {
				reqCtx.Params = *evalSet.Params
			}

			result, err := EvaluatePolicy(ctx, evalSet.Policy, reqCtx, action, key)
			if err != nil {
				span.RecordError(err)
				return UNSET, err
			}

			layerConclusion = layerConclusion.Or(result)
		}

		switch layerConclusion {
		case DENY:
			return DENY, nil
		case ALLOW:
			return ALLOW, nil
		default:
			conclusion = conclusion.Or(layerConclusion)
		}

	}

	return conclusion, nil
}

func EvaluatePolicy(ctx context.Context, policy Policy, req RequestContext, action string, key string) (Conclusion, error) {
	ctx, span := tracer.Start(ctx, "Policy.EvaluatePolicy")
	defer span.End()

	statements := make([]Statement, 0)
	for _, stmt := range policy.Statements {
		if stmt.Action == action {
			//regexKey := strings.ReplaceAll(stmt.Key, "*", ".*")
			regexKey := "^" + strings.ReplaceAll(regexp.QuoteMeta(stmt.Key), "\\*", ".*") + "$"
			matched, err := regexp.MatchString(regexKey, key)
			if err != nil {
				span.RecordError(err)
				continue
			}
			fmt.Println("matching key", key, "against statement key", stmt.Key, "regex", regexKey, "matched", matched)
			if !matched {
				continue
			}
			statements = append(statements, stmt)
		}
	}

	conclusion := UNSET
	for _, stmt := range statements {
		evalResult, err := Eval(req, stmt.Condition)
		if err != nil {
			span.RecordError(err)
			continue
		}

		resultJson, _ := json.MarshalIndent(evalResult, "", "  ")

		span.AddEvent("Evaluated Condition", trace.WithAttributes(
			attribute.String("action", action),
			attribute.String("condition", string(resultJson)),
		))

		if evalResult.Result == true {
			conclusion = conclusion.Or(stmt.Emit)
		}

	}

	if conclusion == UNSET {
		def := policy.Defaults[action]
		if def != "" {
			conclusion = def
		}
	}

	span.SetAttributes(attribute.String("policy.conclusion", conclusion.String()))

	return conclusion, nil
}

func Eval(ctx RequestContext, expr Expr) (EvalResult, error) {

	if expr.Operator == "Const" {
		return EvalResult{
			Operator: "Const",
			Result:   expr.Const,
		}, nil
	}

	// syntax sugar: if Const is set, prepend a Const arg
	if expr.Const != nil {
		expr.Args = append([]Expr{
			{
				Operator: "Const",
				Const:    expr.Const,
			},
		}, expr.Args...)
	}

	args := make([]any, 0, len(expr.Args))
	evalResults := make([]EvalResult, 0, len(expr.Args))
	for _, arg := range expr.Args {
		result, err := Eval(ctx, arg)
		if err != nil {
			return EvalResult{
				Operator: expr.Operator,
				Error:    err.Error(),
			}, err
		}
		args = append(args, result.Result)
		evalResults = append(evalResults, result)
	}

	if operatorFunc, exists := operators[expr.Operator]; exists {
		result, err := operatorFunc(ctx, args)
		result.Args = evalResults
		return result, err
	}

	err := fmt.Errorf("unknown operator: %s\n", expr.Operator)
	return EvalResult{
		Operator: expr.Operator,
		Error:    err.Error(),
		Args:     evalResults,
	}, err
}
