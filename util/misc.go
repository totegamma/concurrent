package util

import (
	"encoding/json"
	"fmt"
	"github.com/go-yaml/yaml"
	"github.com/pkg/errors"
	"os"
	"reflect"
)

func JsonPrint(tag string, obj any) {
	b, _ := json.MarshalIndent(obj, "", "  ")
	fmt.Println(tag, string(b))
}

func DeepMerge(dst, src any) error {
	dstPtr := reflect.ValueOf(dst)
	srcPtr := reflect.ValueOf(src)

	if dstPtr.Kind() != reflect.Ptr || srcPtr.Kind() != reflect.Ptr {
		return fmt.Errorf("both arguments must be pointers")
	}

	dstElem := dstPtr.Elem()
	srcElem := srcPtr.Elem()
	if dstElem.Kind() != reflect.Struct || srcElem.Kind() != reflect.Struct {
		return fmt.Errorf("both arguments must be pointers to structs")
	}

	dstType := dstElem.Type()
	for i := range dstElem.NumField() {
		dstField := dstElem.Field(i)
		srcField := srcElem.Field(i)
		structField := dstType.Field(i)

		if !dstField.CanSet() {
			continue
		}

		if isZeroValue(srcField) {
			continue
		}

		switch dstField.Kind() {
		case reflect.Struct:
			if err := DeepMerge(dstField.Addr().Interface(), srcField.Addr().Interface()); err != nil {
				return fmt.Errorf("error merging field %s: %w", structField.Name, err)
			}
		default:
			dstField.Set(srcField)
		}
	}

	return nil
}

func isZeroValue(v reflect.Value) bool {
	return reflect.DeepEqual(v.Interface(), reflect.Zero(v.Type()).Interface())
}

func LoadMultipleYamlFiles[T any](paths []string) (*T, error) {

	out := new(T)

	if len(paths) == 0 {
		return out, nil
	}

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to open configuration file: %s", path)
		}
		defer f.Close()

		var tmp T
		err = yaml.NewDecoder(f).Decode(&tmp)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to load configuration file: %s", path)
		}

		err = DeepMerge(out, &tmp)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to merge configuration file: %s", path)
		}
	}

	return out, nil
}
