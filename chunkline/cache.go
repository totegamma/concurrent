package chunkline

import (
	"encoding/json"
	"fmt"
)

const (
	CacheTTL           int32 = 2 * 24 * 60 * 60
	itrCacheKeyPrefix        = "chunkline:itr:"
	bodyCacheKeyPrefix       = "chunkline:body:"
)

func IteratorCacheKey(uri string, chunkID int64) string {
	return fmt.Sprintf("%s%s:%d", itrCacheKeyPrefix, uri, chunkID)
}

func BodyCacheKey(uri string, chunkID int64) string {
	return fmt.Sprintf("%s%s:%d", bodyCacheKeyPrefix, uri, chunkID)
}

func EncodeBodyCache(items []BodyItem) ([]byte, error) {
	if len(items) == 0 {
		return []byte{}, nil
	}
	body, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	if len(body) < 2 {
		return nil, fmt.Errorf("invalid chunkline body JSON")
	}
	return []byte("," + string(body[1:len(body)-1])), nil
}

func DecodeBodyCache(body []byte) ([]BodyItem, error) {
	if len(body) == 0 {
		return []BodyItem{}, nil
	}
	if body[0] != ',' {
		return nil, fmt.Errorf("invalid chunkline body cache")
	}
	if len(body) == 1 {
		return []BodyItem{}, nil
	}
	// Tolerate a trailing comma that may appear if a prepend was performed
	// onto a previously-empty (",") cache entry.
	end := len(body)
	if body[end-1] == ',' {
		end--
	}
	cacheStr := "[" + string(body[1:end]) + "]"

	var items []BodyItem
	err := json.Unmarshal([]byte(cacheStr), &items)
	return items, err
}
