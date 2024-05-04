package policy

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/totegamma/concurrent/core"
)

func debugPrint(comment string, v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(comment, string(b))
}

func structToMap(obj any) map[string]any {
	result := make(map[string]any)
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)

		if field.Anonymous {
			embedded := structToMap(v.Field(i).Interface())
			for k, v := range embedded {
				result[k] = v
			}
			continue
		}

		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag == "" {
			continue
		}

		result[tag] = v.Field(i).Interface()
	}
	return result
}

func resolveDotNotation(obj map[string]any, key string) (any, bool) {
	keys := strings.Split(key, ".")
	current := obj
	for i, k := range keys {
		if i == len(keys)-1 {
			value, ok := current[k]
			return value, ok
		} else {
			next, ok := current[k].(map[string]any)
			if !ok {
				return nil, false
			}
			current = next
		}
	}
	return nil, false
}

func isActionMatch(action string, statementAction string) bool {
	split := strings.Split(statementAction, "*")
	if len(split) == 0 {
		return statementAction == action
	}
	statementAction = "^" + strings.Join(split, ".*") + "$"
	match, err := regexp.MatchString(statementAction, action)
	if err != nil {
		return false
	}
	return match
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
			Result:   domain == s.config.Concurrent.FQDN,
		}, nil

	case "IsRequesterRemoteUser":
		domain := requestCtx.Requester.Domain
		return core.EvalResult{
			Operator: "IsRequesterRemoteUser",
			Result:   domain != s.config.Concurrent.FQDN,
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
