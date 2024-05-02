package policy

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/totegamma/concurrent/core"
)

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

func (s service) eval(expr core.Expr, requestCtx core.RequestContext) (any, error) {
	switch expr.Operator {

	case "And":
		for _, arg := range expr.Args {
			eval, err := s.eval(arg, requestCtx)
			if err != nil {
				return nil, err
			}
			rhs, ok := eval.(bool)
			if !ok {
				return nil, fmt.Errorf("bad argument type for AND. Expected bool but got %s\n", reflect.TypeOf(eval).String())
			}

			if !rhs {
				return false, nil
			}
		}
		return true, nil

	case "Or":
		for _, arg := range expr.Args {
			eval, err := s.eval(arg, requestCtx)
			if err != nil {
				return nil, err
			}
			rhs, ok := eval.(bool)
			if !ok {
				return nil, fmt.Errorf("bad argument type for OR. Expected bool but got %s\n", reflect.TypeOf(eval).String())
			}

			if rhs {
				return true, nil
			}
		}
		return false, nil

	case "Const":
		return expr.Constant, nil

	case "Contains":
		if len(expr.Args) != 2 {
			return nil, fmt.Errorf("bad argument length for CONTAINS. Expected 2 but got %d\n", len(expr.Args))
		}

		arg0_raw, err := s.eval(expr.Args[0], requestCtx)
		if err != nil {
			return nil, err
		}

		arg0, ok := arg0_raw.([]string)
		if !ok {
			return nil, fmt.Errorf("bad argument type for CONTAINS. Expected []string but got %s\n", reflect.TypeOf(arg0_raw).String())
		}

		arg1_raw, err := s.eval(expr.Args[1], requestCtx)
		if err != nil {
			return nil, err
		}

		arg1, ok := arg1_raw.(string)
		if !ok {
			return nil, fmt.Errorf("bad argument type for CONTAINS. Expected string but got %s\n", reflect.TypeOf(arg1_raw).String())
		}

		return slices.Contains(arg0, arg1), nil

	case "LoadParam":
		key, ok := expr.Constant.(string)
		if !ok {
			return nil, fmt.Errorf("bad argument type for LoadParam. Expected string but got %s\n", reflect.TypeOf(expr.Constant).String())
		}

		value, ok := resolveDotNotation(requestCtx.Params, key)
		if !ok {
			return nil, fmt.Errorf("key not found: %s\n", key)
		}

		return value, nil

	case "LoadDocument":
		key, ok := expr.Constant.(string)
		if !ok {
			return nil, fmt.Errorf("bad argument type for LoadDocument. Expected string but got %s\n", reflect.TypeOf(expr.Constant).String())
		}

		mappedDocument := structToMap(requestCtx.Document)
		value, ok := resolveDotNotation(mappedDocument, key)
		if !ok {
			return nil, fmt.Errorf("key not found: %s\n", key)
		}

		return value, nil

	case "IsRequesterLocalUser":
		domain := requestCtx.Requester.Domain
		return domain == s.config.Concurrent.FQDN, nil

	case "IsRequesterRemoteUser":
		domain := requestCtx.Requester.Domain
		return domain != s.config.Concurrent.FQDN, nil

	case "IsRequesterGuestUser":
		return requestCtx.Requester.ID == "", nil

	case "RequesterHasTag":
		target, ok := expr.Constant.(string)
		if !ok {
			return nil, fmt.Errorf("bad argument type for RequesterHasTag. Expected string but got %s\n", reflect.TypeOf(expr.Constant).String())
		}

		tags := core.ParseTags(requestCtx.Requester.Tag)
		return tags.Has(target), nil

	case "RequesterID":
		return requestCtx.Requester.ID, nil

	default:
		return nil, fmt.Errorf("unknown operator: %s\n", expr.Operator)
	}
}
