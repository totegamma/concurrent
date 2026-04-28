package utils

import (
	"bytes"
	"encoding/json"
	"sort"
)

type OrderedKV[T any] struct {
	Value T
	Order int64
}

type OrderedKVMap[T any] map[string]OrderedKV[T]

func (om OrderedKVMap[T]) MarshalJSON() ([]byte, error) {
	type pair struct {
		key   string
		value T
		order int64
	}
	pairs := make([]pair, 0, len(om))
	for k, v := range om {
		pairs = append(pairs, pair{
			key:   k,
			value: v.Value,
			order: v.Order,
		})
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].order < pairs[j].order
	})

	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			buf.WriteByte(',')
		}

		keyBytes, err := json.Marshal(p.key)
		if err != nil {
			return nil, err
		}
		buf.Write(keyBytes)
		buf.WriteByte(':')

		valueBytes, err := json.Marshal(p.value)
		if err != nil {
			return nil, err
		}
		buf.Write(valueBytes)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
