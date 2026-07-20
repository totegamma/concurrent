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

func EvaluateStack(ctx context.Context, req RequestContext, stack PolicyStack, action string, key string) (Conclusion, string, error) {
	ctx, span := tracer.Start(ctx, "Policy.EvaluateStack")
	defer span.End()

	span.SetAttributes(attribute.Int("policy.stack.layers", len(stack)))

	conclusion := UNSET
	reason := ""

	for _, layer := range stack {

		layerConclusion := UNSET
		layerReason := ""
		for _, evalSet := range layer {

			dflt := UNSET
			if evalSet.Policy.Defaults != nil {
				defaults := evalSet.Policy.Defaults
				d, ok := defaults[action]
				if ok {
					dflt = Conclusion(d)
				}
			}

			if evalSet.Errored {
				layerConclusion = layerConclusion.Or(dflt)
				continue
			}

			reqCtx := req
			if evalSet.Params != nil {
				reqCtx.Params = *evalSet.Params
			}

			result, evalReason, err := EvaluatePolicy(ctx, evalSet.Policy, reqCtx, action, key)
			if err != nil {
				span.RecordError(err)
				return UNSET, reason, err
			}
			if evalReason != "" {
				layerReason += evalReason
			}

			layerConclusion = layerConclusion.Or(result)
		}

		reason += "[" + layerReason + "] "

		switch layerConclusion {
		case DENY:
			return DENY, reason, nil
		case ALLOW:
			return ALLOW, reason, nil
		case UNSET:
			continue
		default:
			conclusion = layerConclusion
			continue
		}

	}

	return conclusion, reason, nil
}

func EvaluatePolicy(ctx context.Context, policy Policy, req RequestContext, action string, key string) (Conclusion, string, error) {
	ctx, span := tracer.Start(ctx, "Policy.EvaluatePolicy")
	defer span.End()

	reason := ""

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
			if !matched {
				continue
			}
			statements = append(statements, stmt)
		}
	}

	conclusion := UNSET
	for _, stmt := range statements {
		evalResult, err := Eval(ctx, req, stmt.Condition)
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
			if stmt.Reason != nil {
				reason += *stmt.Reason + "; "
			}
		}

	}

	if conclusion == UNSET {
		def := policy.Defaults[action]
		if def != "" {
			conclusion = def
		}
	}

	span.SetAttributes(attribute.String("policy.conclusion", conclusion.String()))

	return conclusion, reason, nil
}

func Eval(ctx context.Context, rctx RequestContext, expr Expr) (EvalResult, error) {

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
		result, err := Eval(ctx, rctx, arg)
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
		result, err := operatorFunc(ctx, rctx, args)
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
