package policy

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/concrnt/concrnt"
)

type Operator func(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error)

var operators = make(map[string]Operator)

func init() {
	operators["And"] = opAnd
	operators["Or"] = opOr
	operators["Not"] = opNot
	operators["Eq"] = opEq
	operators["Contains"] = opContains
	operators["CCUriOwner"] = opCCUriOwner
	operators["Load"] = opLoad
	operators["ConcrntCall"] = opConcrntCall
	operators["IsNotEmpty"] = opIsNotEmpty
}

func opAnd(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error) {

	for i, arg := range args {
		evaluated, ok := arg.(bool)
		if !ok {
			err := fmt.Errorf("bad argument type for AND at index %d. Expected bool but got %s\n", i, reflect.TypeOf(arg))
			return EvalResult{
				Operator: "And",
				Error:    err.Error(),
			}, err
		}

		if !evaluated {
			return EvalResult{
				Operator: "And",
				Result:   false,
			}, nil
		}
	}

	return EvalResult{
		Operator: "And",
		Result:   true,
	}, nil
}

func opOr(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error) {
	for i, arg := range args {
		evaluated, ok := arg.(bool)
		if !ok {
			err := fmt.Errorf("bad argument type for OR at index %d. Expected bool but got %s\n", i, reflect.TypeOf(arg))
			return EvalResult{
				Operator: "Or",
				Error:    err.Error(),
			}, err
		}

		if evaluated {
			return EvalResult{
				Operator: "Or",
				Result:   true,
			}, nil
		}
	}

	return EvalResult{
		Operator: "Or",
		Result:   false,
	}, nil
}

func opNot(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error) {
	if len(args) != 1 {
		err := fmt.Errorf("bad argument length for NOT. Expected 1 but got %d\n", len(args))
		return EvalResult{
			Operator: "Not",
			Error:    err.Error(),
		}, err
	}

	evaluated, ok := args[0].(bool)
	if !ok {
		err := fmt.Errorf("bad argument type for NOT. Expected bool but got %s: %v\n", reflect.TypeOf(args[0]), args[0])
		return EvalResult{
			Operator: "Not",
			Error:    err.Error(),
		}, err
	}

	return EvalResult{
		Operator: "Not",
		Result:   !evaluated,
	}, nil
}

func opEq(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error) {
	if len(args) != 2 {
		err := fmt.Errorf("bad argument length for EQ. Expected 2 but got %d\n", len(args))
		return EvalResult{
			Operator: "Eq",
			Error:    err.Error(),
		}, err
	}

	return EvalResult{
		Operator: "Eq",
		Result:   args[0] == args[1],
	}, nil
}

func opContains(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error) {
	if len(args) != 2 {
		err := fmt.Errorf("bad argument length for CONTAINS. Expected 2 but got %d\n", len(args))
		return EvalResult{
			Operator: "Contains",
			Error:    err.Error(),
		}, err
	}

	arg0, ok := args[0].([]any)
	if !ok {
		err := fmt.Errorf("bad argument type for CONTAINS. Expected []any but got %s: %v\n", reflect.TypeOf(args[0]), args[0])
		return EvalResult{
			Operator: "Contains",
			Error:    err.Error(),
		}, err
	}

	arg1 := args[1]

	return EvalResult{
		Operator: "Contains",
		Result:   slices.Contains(arg0, arg1),
	}, nil

}

/*
func opParseCCURI(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error) {
	if len(args) != 1 {
		err := fmt.Errorf("bad argument length for ParseCCURI. Expected 1 but got %d\n", len(args))
		return EvalResult{
			Operator: "ParseCCURI",
			Error:    err.Error(),
		}, err
	}

	arg0, ok := args[0].(string)
	if !ok {
		err := fmt.Errorf("bad argument type for ParseCCURI. Expected string but got %s: %v\n", reflect.TypeOf(args[0]), args[0])
		return EvalResult{
			Operator: "ParseCCURI",
			Error:    err.Error(),
		}, err
	}

	result, err := concrnt.ParseCCURI(arg0)
	if err != nil {
		return EvalResult{
			Operator: "ParseCCURI",
			Error:    err.Error(),
		}, err
	}

	return EvalResult{
		Operator: "ParseCCURI",
		Result:   result,
	}, nil
}
*/

func opCCUriOwner(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error) {
	if len(args) != 1 {
		err := fmt.Errorf("bad argument length for CCIriOwner. Expected 1 but got %d\n", len(args))
		return EvalResult{
			Operator: "CCUriOwner",
			Error:    err.Error(),
		}, err
	}

	arg0, ok := args[0].(string)
	if !ok {
		err := fmt.Errorf("bad argument type for CCIriOwner. Expected string but got %s: %v\n", reflect.TypeOf(args[0]), args[0])
		return EvalResult{
			Operator: "CCUriOwner",
			Error:    err.Error(),
		}, err
	}

	parsed, err := concrnt.ParseCCURI(arg0)
	if err != nil {
		return EvalResult{
			Operator: "CCUriOwner",
			Error:    err.Error(),
		}, err
	}

	result := parsed.Owner

	return EvalResult{
		Operator: "CCUriOwner",
		Result:   result,
	}, nil
}

func opLoad(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error) {
	if len(args) != 1 {
		err := fmt.Errorf("bad argument length for Load. Expected 1 but got %d\n", len(args))
		return EvalResult{
			Operator: "Load",
			Error:    err.Error(),
		}, err
	}

	key, ok := args[0].(string)
	if !ok {
		err := fmt.Errorf("bad argument type for Load. Expected string but got %s\n", reflect.TypeOf(args[0]))
		return EvalResult{
			Operator: "Load",
			Error:    err.Error(),
		}, err
	}

	mappedCtx := structToMap(rctx)
	value, ok := resolveDotNotation(mappedCtx, key)
	if !ok {
		err := fmt.Errorf("key not found: %s", key)
		concrnt.JsonPrint("mappedCtx", mappedCtx)
		return EvalResult{
			Operator: "Load",
			Error:    err.Error(),
		}, err
	}

	return EvalResult{
		Operator: "Load",
		Result:   value,
	}, nil
}

func opConcrntCall(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error) {
	if rctx.Caller == nil {
		err := fmt.Errorf("no concrnt caller available in this evaluation context\n")
		return EvalResult{
			Operator: "ConcrntCall",
			Error:    err.Error(),
		}, err
	}

	if len(args) < 2 || (len(args)-2)%2 != 0 {
		err := fmt.Errorf("bad argument length for ConcrntCall. Expected resolver, api and key/value pairs but got %d args\n", len(args))
		return EvalResult{
			Operator: "ConcrntCall",
			Error:    err.Error(),
		}, err
	}

	resolver, ok := args[0].(string)
	if !ok {
		err := fmt.Errorf("bad argument type for ConcrntCall resolver. Expected string but got %s: %v\n", reflect.TypeOf(args[0]), args[0])
		return EvalResult{
			Operator: "ConcrntCall",
			Error:    err.Error(),
		}, err
	}

	api, ok := args[1].(string)
	if !ok {
		err := fmt.Errorf("bad argument type for ConcrntCall api. Expected string but got %s: %v\n", reflect.TypeOf(args[1]), args[1])
		return EvalResult{
			Operator: "ConcrntCall",
			Error:    err.Error(),
		}, err
	}

	params := make(map[string]string)
	for i := 2; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			err := fmt.Errorf("bad argument type for ConcrntCall param key at index %d. Expected string but got %s: %v\n", i, reflect.TypeOf(args[i]), args[i])
			return EvalResult{
				Operator: "ConcrntCall",
				Error:    err.Error(),
			}, err
		}
		value, ok := args[i+1].(string)
		if !ok {
			err := fmt.Errorf("bad argument type for ConcrntCall param value at index %d. Expected string but got %s: %v\n", i+1, reflect.TypeOf(args[i+1]), args[i+1])
			return EvalResult{
				Operator: "ConcrntCall",
				Error:    err.Error(),
			}, err
		}
		params[key] = value
	}

	result, err := rctx.Caller.ConcrntCall(ctx, resolver, api, params)
	if err != nil {
		return EvalResult{
			Operator: "ConcrntCall",
			Error:    err.Error(),
		}, err
	}

	return EvalResult{
		Operator: "ConcrntCall",
		Result:   result,
	}, nil
}

func opIsNotEmpty(ctx context.Context, rctx RequestContext, args []any) (EvalResult, error) {
	if len(args) != 1 {
		err := fmt.Errorf("bad argument length for IsNotEmpty. Expected 1 but got %d\n", len(args))
		return EvalResult{
			Operator: "IsNotEmpty",
			Error:    err.Error(),
		}, err
	}

	if args[0] == nil {
		return EvalResult{
			Operator: "IsNotEmpty",
			Result:   false,
		}, nil
	}

	switch arg := args[0].(type) {
	case []any:
		return EvalResult{
			Operator: "IsNotEmpty",
			Result:   len(arg) > 0,
		}, nil
	case string:
		return EvalResult{
			Operator: "IsNotEmpty",
			Result:   arg != "",
		}, nil
	case map[string]any:
		return EvalResult{
			Operator: "IsNotEmpty",
			Result:   len(arg) > 0,
		}, nil
	default:
		err := fmt.Errorf("bad argument type for IsNotEmpty. Expected slice, string or map but got %s: %v\n", reflect.TypeOf(args[0]), args[0])
		return EvalResult{
			Operator: "IsNotEmpty",
			Error:    err.Error(),
		}, err
	}
}
