package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("policy")

type service struct {
	repository Repository
	config     core.Config
}

func NewService(repository Repository, config core.Config) core.PolicyService {
	return &service{
		repository,
		config,
	}
}

func (s service) TestWithPolicyURL(ctx context.Context, url string, context core.RequestContext, action string) (bool, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.TestWithPolicyURL")
	defer span.End()

	policy, err := s.repository.Get(ctx, url)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return false, err
	}

	return s.Test(ctx, policy, context, action)
}

func (s service) Test(ctx context.Context, policy core.Policy, context core.RequestContext, action string) (bool, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.Test")
	defer span.End()

	span.SetAttributes(attribute.String("action", action))

	for _, statement := range policy.Statements {
		_, span := tracer.Start(ctx, "Policy.Service.Test.Statement")
		for _, a := range statement.Actions {
			span.SetAttributes(attribute.StringSlice("actions", statement.Actions))
			if isActionMatch(action, a) {
				span.SetAttributes(attribute.Bool("examined", true))

				result, err := s.eval(statement.Condition, context)
				resultJson, _ := json.MarshalIndent(result, "", "  ")
				span.SetAttributes(attribute.String("result", string(resultJson)))
				if err != nil {
					span.SetStatus(codes.Error, err.Error())
					span.End()
					return false, err
				}

				result_bool, ok := result.Result.(bool)
				if !ok {
					err := fmt.Errorf("bad argument type for Policy. Expected bool but got %s\n", reflect.TypeOf(result).String())
					span.SetStatus(codes.Error, err.Error())
					span.End()
					return false, err
				}

				if statement.Effect == "allow" {
					span.End()
					return result_bool, nil
				} else {
					span.End()
					return !result_bool, nil
				}
			}
		}
		span.SetAttributes(attribute.Bool("examined", false))
		span.End()
	}
	return false, nil
}

func (s service) eval(expr core.Expr, requestCtx core.RequestContext) (core.EvalResult, error) {

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("recovered from: %v\n", r)
			fmt.Printf("while evaluating: %v\n", expr.Operator)
			debugPrint("expr", expr)
			debugPrint("requestCtx", requestCtx)
		}
	}()

	switch expr.Operator {
	case "And":
		args := make([]core.EvalResult, 0)
		for _, arg := range expr.Args {
			eval, err := s.eval(arg, requestCtx)
			if err != nil {
				return core.EvalResult{
					Operator: "And",
					Args:     args,
					Error:    err.Error(),
				}, err
			}
			args = append(args, eval)
			rhs, ok := eval.Result.(bool)

			if !ok {
				err := fmt.Errorf("bad argument type for AND. Expected bool but got %s\n", reflect.TypeOf(eval.Result))
				return core.EvalResult{
					Operator: "And",
					Args:     args,
					Error:    err.Error(),
				}, err
			}

			if !rhs {
				return core.EvalResult{
					Operator: "And",
					Args:     args,
					Result:   false,
				}, nil
			}
		}
		return core.EvalResult{
			Operator: "And",
			Args:     args,
			Result:   true,
		}, nil

	case "Or":
		args := make([]core.EvalResult, 0)
		for _, arg := range expr.Args {
			eval, err := s.eval(arg, requestCtx)
			if err != nil {
				return core.EvalResult{
					Operator: "Or",
					Args:     args,
					Error:    err.Error(),
				}, err
			}
			rhs, ok := eval.Result.(bool)
			if !ok {
				err := fmt.Errorf("bad argument type for OR. Expected bool but got %s\n", reflect.TypeOf(eval.Result))
				return core.EvalResult{
					Operator: "Or",
					Args:     args,
					Error:    err.Error(),
				}, err
			}

			if rhs {
				return core.EvalResult{
					Operator: "Or",
					Args:     args,
					Result:   true,
				}, nil
			}
		}
		return core.EvalResult{
			Operator: "Or",
			Args:     args,
			Result:   false,
		}, nil

	case "Const":
		return core.EvalResult{
			Operator: "Const",
			Result:   expr.Constant,
		}, nil

	case "Contains":
		if len(expr.Args) != 2 {
			err := fmt.Errorf("bad argument length for CONTAINS. Expected 2 but got %d\n", len(expr.Args))
			return core.EvalResult{
				Operator: "Contains",
				Error:    err.Error(),
			}, err
		}

		arg0_raw, err := s.eval(expr.Args[0], requestCtx)
		if err != nil {
			return core.EvalResult{
				Operator: "Contains",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		arg0, ok := arg0_raw.Result.([]any)
		if !ok {
			err := fmt.Errorf("bad argument type for CONTAINS. Expected []any but got %s\n", reflect.TypeOf(arg0_raw.Result))
			return core.EvalResult{
				Operator: "Contains",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		arg1_raw, err := s.eval(expr.Args[1], requestCtx)
		if err != nil {
			return core.EvalResult{
				Operator: "Contains",
				Args:     []core.EvalResult{arg0_raw, arg1_raw},
				Error:    err.Error(),
			}, err
		}

		arg1, ok := arg1_raw.Result.(any)
		if !ok {
			err := fmt.Errorf("bad argument type for CONTAINS. Expected any but got %s\n", reflect.TypeOf(arg1_raw.Result))
			return core.EvalResult{
				Operator: "Contains",
				Args:     []core.EvalResult{arg0_raw, arg1_raw},
				Error:    err.Error(),
			}, err
		}

		return core.EvalResult{
			Operator: "Contains",
			Args:     []core.EvalResult{arg0_raw, arg1_raw},
			Result:   slices.Contains(arg0, arg1),
		}, nil

	case "LoadParam":
		key, ok := expr.Constant.(string)
		if !ok {
			err := fmt.Errorf("bad argument type for LoadParam. Expected string but got %s\n", reflect.TypeOf(expr.Constant))
			return core.EvalResult{
				Operator: "LoadParam",
				Error:    err.Error(),
			}, err
		}

		value, ok := resolveDotNotation(requestCtx.Params, key)
		if !ok {
			err := fmt.Errorf("key not found: %s\n", key)
			return core.EvalResult{
				Operator: "LoadParam",
				Error:    err.Error(),
			}, err
		}

		return core.EvalResult{
			Operator: "LoadParam",
			Result:   value,
		}, nil

	case "LoadDocument":
		key, ok := expr.Constant.(string)
		if !ok {
			err := fmt.Errorf("bad argument type for LoadDocument. Expected string but got %s\n", reflect.TypeOf(expr.Constant))
			return core.EvalResult{
				Operator: "LoadDocument",
				Error:    err.Error(),
			}, err
		}

		mappedDocument := structToMap(requestCtx.Document)
		value, ok := resolveDotNotation(mappedDocument, key)
		if !ok {
			err := fmt.Errorf("key not found: %s\n", key)
			return core.EvalResult{
				Operator: "LoadDocument",
				Error:    err.Error(),
			}, err
		}

		return core.EvalResult{
			Operator: "LoadDocument",
			Result:   value,
		}, nil

	case "IsRequesterLocalUser":
		domain := requestCtx.Requester.Domain
		return core.EvalResult{
			Operator: "IsRequesterLocalUser",
			Result:   domain == s.config.FQDN,
		}, nil

	case "IsRequesterRemoteUser":
		domain := requestCtx.Requester.Domain
		return core.EvalResult{
			Operator: "IsRequesterRemoteUser",
			Result:   domain != s.config.FQDN,
		}, nil

	case "IsRequesterGuestUser":
		return core.EvalResult{
			Operator: "IsRequesterGuestUser",
			Result:   requestCtx.Requester.ID == "",
		}, nil

	case "RequesterHasTag":
		target, ok := expr.Constant.(string)
		if !ok {
			err := fmt.Errorf("bad argument type for RequesterHasTag. Expected string but got %s\n", reflect.TypeOf(expr.Constant))
			return core.EvalResult{
				Operator: "RequesterHasTag",
				Error:    err.Error(),
			}, err
		}

		tags := core.ParseTags(requestCtx.Requester.Tag)
		return core.EvalResult{
			Operator: "RequesterHasTag",
			Result:   tags.Has(target),
		}, nil

	case "RequesterID":
		return core.EvalResult{
			Operator: "RequesterID",
			Result:   requestCtx.Requester.ID,
		}, nil

	default:
		err := fmt.Errorf("unknown operator: %s\n", expr.Operator)
		return core.EvalResult{
			Operator: expr.Operator,
			Error:    err.Error(),
		}, err
	}
}
