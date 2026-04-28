package policy

import (
	"reflect"
	"strings"
)

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

func structToMap(obj any) map[string]any {
	v := reflect.ValueOf(obj)
	out, ok := toAny(v).(map[string]any)
	if ok {
		return out
	}
	// 入力がstruct以外でも安全に map を返す
	return map[string]any{}
}

func toAny(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}

	// interface / pointer の unwrap
	for {
		switch v.Kind() {
		case reflect.Interface:
			if v.IsNil() {
				return nil
			}
			v = v.Elem()
			continue
		case reflect.Pointer:
			if v.IsNil() {
				return nil
			}
			v = v.Elem()
			continue
		}
		break
	}

	switch v.Kind() {
	case reflect.Struct:
		return structValueToMap(v)
	case reflect.Map:
		return mapValueToAny(v)
	case reflect.Slice, reflect.Array:
		return sliceValueToAny(v)
	default:
		// time.Time 等の struct は上で map 化される。
		// 「structは全部map化したくない」などの要件があるならここで除外条件を入れる。
		return v.Interface()
	}
}

func structValueToMap(v reflect.Value) map[string]any {
	t := v.Type()
	out := make(map[string]any, v.NumField())

	for i := 0; i < v.NumField(); i++ {
		sf := t.Field(i)

		// 非公開フィールドはスキップ（PkgPath != "" で判定）
		if sf.PkgPath != "" {
			continue
		}

		// jsonタグの名前を決める
		name, ok := jsonFieldName(sf)
		if !ok {
			continue // json:"-" のケース
		}

		fv := v.Field(i)

		// 埋め込みstruct（anonymous）でタグ名が空の場合はフラットに展開
		if sf.Anonymous && name == "" {
			expanded := toAny(fv)
			if m, ok := expanded.(map[string]any); ok {
				for k, val := range m {
					out[k] = val
				}
				continue
			}
			// 埋め込みだけどstructでない、などはそのまま
			out[sf.Name] = expanded
			continue
		}

		// タグ名が空ならフィールド名
		if name == "" {
			name = sf.Name
		}

		out[name] = toAny(fv)
	}

	return out
}

// jsonFieldName は json タグの「名前部分」を返す。
// - json:"-" -> ok=false
// - json:"foo,omitempty" -> "foo", ok=true
// - json:",omitempty" -> "", ok=true（呼び出し側でフィールド名を採用）
func jsonFieldName(sf reflect.StructField) (name string, ok bool) {
	tag := sf.Tag.Get("json")
	if tag == "" {
		return "", true
	}
	if tag == "-" {
		return "", false
	}
	parts := strings.Split(tag, ",")
	return parts[0], true
}

func sliceValueToAny(v reflect.Value) []any {
	n := v.Len()
	out := make([]any, n)
	for i := 0; i < n; i++ {
		out[i] = toAny(v.Index(i))
	}
	return out
}

func mapValueToAny(v reflect.Value) any {
	// keyがstringなら map[string]any に寄せる
	if v.Type().Key().Kind() == reflect.String {
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			k := iter.Key().String()
			out[k] = toAny(iter.Value())
		}
		return out
	}

	// keyがstring以外は JSON的に扱いづらいので map[any]any にして返す
	out := make(map[any]any, v.Len())
	iter := v.MapRange()
	for iter.Next() {
		out[iter.Key().Interface()] = toAny(iter.Value())
	}
	return out
}
