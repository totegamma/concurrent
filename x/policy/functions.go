package policy

import (
	"reflect"
	"strings"

	"github.com/concrnt/concrnt/core"
)

// IsDominant checks if a policy evaluation result is dominant (Always or Never).
// It returns true and the boolean value (true for Always, false for Never) if dominant,
// otherwise returns false, false.
func IsDominant(result core.PolicyEvalResult) (bool, bool) {
	if result == core.PolicyEvalResultAlways {
		return true, true
	} else if result == core.PolicyEvalResultNever {
		return true, false
	} else {
		return false, false
	}
}

// AccumulateOr combines multiple policy evaluation results using OR logic.
// Dominant results (Always, Never) take precedence.
// If conflicting dominant results exist, it defaults.
// Otherwise, Allow takes precedence over Deny, which takes precedence over Default.
// Returns Error if any input result is Error.
func AccumulateOr(results []core.PolicyEvalResult) core.PolicyEvalResult {
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
			return core.PolicyEvalResultError
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
