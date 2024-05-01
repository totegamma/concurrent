package policy

import (
	"fmt"
	"reflect"

	"github.com/totegamma/concurrent/core"
)

type Policy struct {
	Name      string
	Version   string
	Statement []Statement
}

type RequestContext struct {
	Requester core.Entity
	Resource  map[string]any
	Self      map[string]any
	Params    map[string]any
}

type Statement struct {
	Action    []string
	Effect    string
	Condition Expr
}

type Expr struct {
	Operator string
	Args     []Expr
	Constant any
}

func Test(policy Policy, context RequestContext, action string) bool {
	params := map[string]any{
		"requester": context.Requester,
		"resource":  context.Resource,
		"self":      context.Self,
		"params":    context.Params,
	}
	for _, statement := range policy.Statement {
		for _, a := range statement.Action {
			if a == action {
				if statement.Effect == "allow" {
					return Eval(statement.Condition, params).(bool)
				} else {
					return !Eval(statement.Condition, params).(bool)
				}
			}
		}
	}
	return false
}

func Eval(expr Expr, params map[string]any) any {
	switch expr.Operator {
	case "ADD":
		var result int
		for i, arg := range expr.Args {
			eval := Eval(arg, params)
			rhs, ok := eval.(int)
			if !ok {
				fmt.Printf("bad argument type for ADD for arg %d. Expected int but got %s\n", i, reflect.TypeOf(eval).String())
				continue
			}
			result += rhs
		}
		return result
	case "PARAMINT":
		key, ok := expr.Constant.(string)
		if !ok {
			panic("bad argument type for READPARAM")
		}
		val, ok := params[key].(float64)
		if !ok {
			panic("bad argument type for READPARAM")
		}
		return int(val)
	case "PARAMSTRING":
		key, ok := expr.Constant.(string)
		if !ok {
			panic("bad argument type for READPARAM")
		}
		val, ok := params[key].(string)
		if !ok {
			panic("bad argument type for READPARAM")
		}
		return val
	case "PARAMSTRINGARRAY":
		key, ok := expr.Constant.(string)
		if !ok {
			panic("bad argument type for READPARAM")
		}
		val, ok := params[key].([]interface{})
		if !ok {
			panic("bad argument type for READPARAM")
		}
		return val
	case "CONTAINS":
		var result bool
		arg1 := Eval(expr.Args[0], params)
		arg2 := Eval(expr.Args[1], params)
		for _, v := range arg2.([]interface{}) {
			if arg1 == v {
				result = true
				break
			}
		}
		return result
	case "CONST":
		return expr.Constant
	default:
		panic("unknown operator: " + expr.Operator)
	}
}
