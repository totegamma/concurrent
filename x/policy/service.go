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
	"github.com/totegamma/concurrent/internal/testutil"
)

var tracer = otel.Tracer("policy")

type service struct {
	repository Repository
	global     core.Policy
	config     core.Config
}

func NewService(repository Repository, globalPolicy core.Policy, config core.Config) core.PolicyService {
	return &service{
		repository,
		globalPolicy,
		config,
	}
}

func (s service) Summerize(results []core.PolicyEvalResult, action string, override *map[string]bool) bool {
	_, span := tracer.Start(context.Background(), "Policy.Service.Summerize")
	defer span.End()

	var defaultResult bool = false
	var ok bool = false
	if override != nil {
		defaultResult, ok = (*override)[action]
	}
	if !ok {
		defaultResult, ok = s.global.Defaults[action]
		if !ok {
			defaultResult = false
		}
	}

	span.SetAttributes(attribute.Bool("defaultResult", defaultResult))

	var result bool = defaultResult

	for _, r := range results {
		switch r {
		case core.PolicyEvalResultAlways:
			return true
		case core.PolicyEvalResultNever:
			return false
		case core.PolicyEvalResultAllow:
			result = true
		case core.PolicyEvalResultDeny:
			result = false
		case core.PolicyEvalResultError:
			result = defaultResult
		case core.PolicyEvalResultDefault:
			continue
		}
	}

	return result
}

func (s service) AccumulateOr(results []core.PolicyEvalResult, action string, override *map[string]bool) core.PolicyEvalResult {
	_, span := tracer.Start(context.Background(), "Policy.Service.AccumulateOr")
	defer span.End()

	var defaultResult bool = false
	var ok bool = false
	if override != nil {
		defaultResult, ok = (*override)[action]
	}
	if !ok {
		defaultResult, ok = s.global.Defaults[action]
		if !ok {
			defaultResult = false
		}
	}

	span.SetAttributes(attribute.Bool("defaultResult", defaultResult))

	var hasAlways bool
	var hasNever bool
	var hasAllow bool
	var hasDeny bool

	for _, r := range results {
		if r == core.PolicyEvalResultAlways {
			hasAlways = true
		} else if r == core.PolicyEvalResultNever {
			hasNever = true
		} else if r == core.PolicyEvalResultAllow {
			hasAllow = true
		} else if r == core.PolicyEvalResultDeny {
			hasDeny = true
		} else if r == core.PolicyEvalResultError {
			if defaultResult {
				hasAllow = true
			} else {
				hasDeny = true
			}
		}
	}

	if hasAlways && hasNever {
		return core.PolicyEvalResultDefault
	} else if hasAlways {
		return core.PolicyEvalResultAlways
	} else if hasNever {
		return core.PolicyEvalResultNever
	}

	if hasAllow && hasDeny {
		return core.PolicyEvalResultDefault
	} else if hasAllow {
		return core.PolicyEvalResultAllow
	} else if hasDeny {
		return core.PolicyEvalResultDeny
	}

	return core.PolicyEvalResultDefault
}

func (s service) TestWithGlobalPolicy(ctx context.Context, context core.RequestContext, action string) (core.PolicyEvalResult, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.TestWithGlobalPolicy")
	defer span.End()

	return s.test(ctx, s.global, context, action)
}

func (s service) TestWithPolicyURL(ctx context.Context, url string, context core.RequestContext, action string) (core.PolicyEvalResult, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.TestWithPolicyURL")
	defer span.End()

	var policy core.Policy
	if url != "" {
		var err error
		policy, err = s.repository.Get(ctx, url)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return core.PolicyEvalResultDefault, err
		}
	}

	return s.Test(ctx, policy, context, action)
}

func (s service) Test(ctx context.Context, policy core.Policy, context core.RequestContext, action string) (core.PolicyEvalResult, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.Test")
	defer span.End()

	globalResult, err := s.test(ctx, s.global, context, action)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return core.PolicyEvalResultDefault, err
	}

	if globalResult == core.PolicyEvalResultAlways || globalResult == core.PolicyEvalResultNever {
		return globalResult, nil
	}

	if len(policy.Statements) == 0 {
		return globalResult, nil
	}

	localResult, err := s.test(ctx, policy, context, action)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return core.PolicyEvalResultDefault, err
	}

	if localResult == core.PolicyEvalResultDefault {
		return globalResult, nil
	}

	return localResult, nil
}

func (s service) test(ctx context.Context, policy core.Policy, context core.RequestContext, action string) (core.PolicyEvalResult, error) {
	ctx, span := tracer.Start(ctx, "Policy.Service.test")
	defer span.End()

	span.SetAttributes(attribute.String("action", action))

	statement, ok := policy.Statements[action]
	if !ok {
		span.SetAttributes(attribute.String("debug", "no rule"))
		return core.PolicyEvalResultDefault, nil
	}

	result, err := s.eval(statement.Condition, context)
	resultJson, _ := json.MarshalIndent(result, "", "  ")
	span.SetAttributes(attribute.String("result", string(resultJson)))
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return core.PolicyEvalResultDefault, err
	}

	result_bool, ok := result.Result.(bool)
	if !ok {
		err := fmt.Errorf("bad argument type for Policy. Expected bool but got %s\n", reflect.TypeOf(result).String())
		span.SetStatus(codes.Error, err.Error())
		return core.PolicyEvalResultDefault, err
	}

	if statement.DefaultOnTrue && result_bool {
		return core.PolicyEvalResultDefault, nil
	} else if statement.DefaultOnFalse && !result_bool {
		return core.PolicyEvalResultDefault, nil
	} else if statement.Dominant && result_bool {
		return core.PolicyEvalResultAlways, nil
	} else if statement.Dominant && !result_bool {
		return core.PolicyEvalResultNever, nil
	} else if result_bool {
		return core.PolicyEvalResultAllow, nil
	} else {
		return core.PolicyEvalResultDeny, nil
	}
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
			args = append(args, eval)
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
	case "Not":
		if len(expr.Args) != 1 {
			err := fmt.Errorf("bad argument length for NOT. Expected 1 but got %d\n", len(expr.Args))
			return core.EvalResult{
				Operator: "Not",
				Error:    err.Error(),
			}, err
		}

		arg0_raw, err := s.eval(expr.Args[0], requestCtx)
		if err != nil {
			return core.EvalResult{
				Operator: "Not",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		arg0, ok := arg0_raw.Result.(bool)
		if !ok {
			err := fmt.Errorf("bad argument type for NOT. Expected bool but got %s\n", reflect.TypeOf(arg0_raw.Result))
			return core.EvalResult{
				Operator: "Not",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		return core.EvalResult{
			Operator: "Not",
			Args:     []core.EvalResult{arg0_raw},
			Result:   !arg0,
		}, nil

	case "Eq":
		if len(expr.Args) != 2 {
			err := fmt.Errorf("bad argument length for EQ. Expected 2 but got %d\n", len(expr.Args))
			return core.EvalResult{
				Operator: "Eq",
				Error:    err.Error(),
			}, err
		}

		arg0_raw, err := s.eval(expr.Args[0], requestCtx)
		if err != nil {
			return core.EvalResult{
				Operator: "Eq",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		arg1_raw, err := s.eval(expr.Args[1], requestCtx)
		if err != nil {
			return core.EvalResult{
				Operator: "Eq",
				Args:     []core.EvalResult{arg0_raw, arg1_raw},
				Error:    err.Error(),
			}, err
		}

		return core.EvalResult{
			Operator: "Eq",
			Args:     []core.EvalResult{arg0_raw, arg1_raw},
			Result:   arg0_raw.Result == arg1_raw.Result,
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
			testutil.PrintJson(requestCtx)
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

	case "LoadSelf":
		key, ok := expr.Constant.(string)
		if !ok {
			err := fmt.Errorf("bad argument type for LoadSelf. Expected string but got %s\n", reflect.TypeOf(expr.Constant))
			return core.EvalResult{
				Operator: "LoadSelf",
				Error:    err.Error(),
			}, err
		}

		mappedSelf := structToMap(requestCtx.Self)
		value, ok := resolveDotNotation(mappedSelf, key)
		if !ok {
			err := fmt.Errorf("key not found: %s\n", key)
			return core.EvalResult{
				Operator: "LoadSelf",
				Error:    err.Error(),
			}, err
		}

		return core.EvalResult{
			Operator: "LoadSelf",
			Result:   value,
		}, nil

	case "LoadResource":
		key, ok := expr.Constant.(string)
		if !ok {
			err := fmt.Errorf("bad argument type for LoadResource. Expected string but got %s\n", reflect.TypeOf(expr.Constant))
			return core.EvalResult{
				Operator: "LoadResource",
				Error:    err.Error(),
			}, err
		}

		mappedResource := structToMap(requestCtx.Resource)
		value, ok := resolveDotNotation(mappedResource, key)
		if !ok {
			err := fmt.Errorf("key not found: %s\n", key)
			return core.EvalResult{
				Operator: "LoadResource",
				Error:    err.Error(),
			}, err
		}

		return core.EvalResult{
			Operator: "LoadResource",
			Result:   value,
		}, nil

	case "DomainFQDN":
		return core.EvalResult{
			Operator: "DomainFQDN",
			Result:   s.config.FQDN,
		}, nil

	case "DomainCSID":
		return core.EvalResult{
			Operator: "DomainCSID",
			Result:   s.config.CSID,
		}, nil

	case "IsCCID":
		if len(expr.Args) != 1 {
			err := fmt.Errorf("bad argument length for IsCCID. Expected 1 but got %d\n", len(expr.Args))
			return core.EvalResult{
				Operator: "IsCCID",
				Error:    err.Error(),
			}, err
		}

		arg0_raw, err := s.eval(expr.Args[0], requestCtx)
		if err != nil {
			return core.EvalResult{
				Operator: "IsCCID",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		arg0, ok := arg0_raw.Result.(string)
		if !ok {
			err := fmt.Errorf("bad argument type for IsCCID. Expected string but got %s\n", reflect.TypeOf(arg0_raw.Result))
			return core.EvalResult{
				Operator: "IsCCID",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		return core.EvalResult{
			Operator: "IsCCID",
			Args:     []core.EvalResult{arg0_raw},
			Result:   core.IsCCID(arg0),
		}, nil

	case "IsCSID":
		if len(expr.Args) != 1 {
			err := fmt.Errorf("bad argument length for IsCSID. Expected 1 but got %d\n", len(expr.Args))
			return core.EvalResult{
				Operator: "IsCSID",
				Error:    err.Error(),
			}, err
		}

		arg0_raw, err := s.eval(expr.Args[0], requestCtx)
		if err != nil {
			return core.EvalResult{
				Operator: "IsCSID",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		arg0, ok := arg0_raw.Result.(string)
		if !ok {
			err := fmt.Errorf("bad argument type for IsCSID. Expected string but got %s\n", reflect.TypeOf(arg0_raw.Result))
			return core.EvalResult{
				Operator: "IsCSID",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		return core.EvalResult{
			Operator: "IsCSID",
			Args:     []core.EvalResult{arg0_raw},
			Result:   core.IsCSID(arg0),
		}, nil

	case "IsCKID":
		if len(expr.Args) != 1 {
			err := fmt.Errorf("bad argument length for IsCKID. Expected 1 but got %d\n", len(expr.Args))
			return core.EvalResult{
				Operator: "IsCKID",
				Error:    err.Error(),
			}, err
		}

		arg0_raw, err := s.eval(expr.Args[0], requestCtx)
		if err != nil {
			return core.EvalResult{
				Operator: "IsCKID",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		arg0, ok := arg0_raw.Result.(string)
		if !ok {
			err := fmt.Errorf("bad argument type for IsCKID. Expected string but got %s\n", reflect.TypeOf(arg0_raw.Result))
			return core.EvalResult{
				Operator: "IsCKID",
				Args:     []core.EvalResult{arg0_raw},
				Error:    err.Error(),
			}, err
		}

		return core.EvalResult{
			Operator: "IsCKID",
			Args:     []core.EvalResult{arg0_raw},
			Result:   core.IsCKID(arg0),
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

	case "RequesterDomainHasTag":
		target, ok := expr.Constant.(string)
		if !ok {
			err := fmt.Errorf("bad argument type for RequesterDomainHasTag. Expected string but got %s\n", reflect.TypeOf(expr.Constant))
			return core.EvalResult{
				Operator: "RequesterDomainHasTag",
				Error:    err.Error(),
			}, err
		}

		tags := core.ParseTags(requestCtx.RequesterDomain.Tag)
		return core.EvalResult{
			Operator: "RequesterDomainHasTag",
			Result:   tags.Has(target),
		}, nil

	default:
		err := fmt.Errorf("unknown operator: %s\n", expr.Operator)
		return core.EvalResult{
			Operator: expr.Operator,
			Error:    err.Error(),
		}, err
	}
}
