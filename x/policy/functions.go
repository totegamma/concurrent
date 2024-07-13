package policy

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/totegamma/concurrent/core"
)

func Summerize(results []core.PolicyEvalResult) bool {
	result := false

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
		case core.PolicyEvalResultDefault:
			continue
		}
	}

	return result
}

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
