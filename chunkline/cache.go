package chunkline

import (
	"encoding/json"
	"fmt"
)

const (
	CacheTTL           int32 = 60 * 60 * 24 * 2
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
		return []byte(","), nil
	}
	body, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	return []byte("," + string(body[1:len(body)-1])), nil
}

func DecodeBodyCache(body []byte) ([]BodyItem, error) {
	if len(body) == 0 {
		return []BodyItem{}, nil
	}
	cacheStr := string(body)
	if cacheStr == "," {
		return []BodyItem{}, nil
	}
	cacheStr = cacheStr[1:]
	cacheStr = "[" + cacheStr + "]"

	var items []BodyItem
	err := json.Unmarshal([]byte(cacheStr), &items)
	return items, err
}
