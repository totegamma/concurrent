package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

const (
	chunkLength = 600
)

func Time2Chunk(t time.Time) string {
	// chunk by 10 minutes
	return fmt.Sprintf("%d", (t.Unix()/chunkLength)*chunkLength)
}

func NextChunk(chunk string) string {
	i, _ := strconv.ParseInt(chunk, 10, 64)
	return fmt.Sprintf("%d", i+chunkLength)
}

func PrevChunk(chunk string) string {
	i, _ := strconv.ParseInt(chunk, 10, 64)
	return fmt.Sprintf("%d", i-chunkLength)
}

func Chunk2RecentTime(chunk string) time.Time {
	i, _ := strconv.ParseInt(chunk, 10, 64)
	return time.Unix(i+chunkLength, 0)
}

func Chunk2ImmediateTime(chunk string) time.Time {
	i, _ := strconv.ParseInt(chunk, 10, 64)
	return time.Unix(i, 0)
}

func EpochTime(epoch string) time.Time {
	i, _ := strconv.ParseInt(epoch, 10, 64)
	return time.Unix(i, 0)
}

func TypedIDToType(id string) string {
	if len(id) != 27 {
		return ""
	}
	prefix := id[0]
	switch prefix {
	case 'a':
		return "association"
	case 'm':
		return "message"
	default:
		return ""
	}
}

func hasChar(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

func IsCKID(keyID string) bool {
	return len(keyID) == 42 && keyID[:3] == "cck" && !hasChar(keyID, '.')
}

func IsCCID(keyID string) bool {
	return len(keyID) == 42 && keyID[:3] == "con" && !hasChar(keyID, '.')
}

func IsCSID(keyID string) bool {
	return len(keyID) == 42 && keyID[:3] == "ccs" && !hasChar(keyID, '.')
}

func JsonPrint(tag string, obj interface{}) {
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
