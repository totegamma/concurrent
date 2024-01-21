package core

import (
	"strconv"
	"strings"
)

type Tags struct {
	Body map[string]string
}

func NewTags() *Tags {
	return &Tags{Body: make(map[string]string)}
}

func ParseTags(input string) *Tags {
	tags := &Tags{Body: make(map[string]string)}
	split := strings.Split(input, ",")
	for _, tag := range split {
		pair := strings.Split(tag, ":")
		if len(pair) == 1 {
			tags.Body[pair[0]] = ""
		} else if len(pair) == 2 {
			tags.Body[pair[0]] = pair[1]
		}
	}
	return tags
}

func (t *Tags) Add(key, value string) {
	t.Body[key] = value
}

func (t *Tags) Has(key string) bool {
	_, ok := t.Body[key]
	return ok
}

func (t *Tags) Remove(key string) {
	delete(t.Body, key)
}

func (t *Tags) ToString() string {
	result := ""
	for key, value := range t.Body {
		if result != "" {
			result += ","
		}
		result += key
		if value != "" {
			result += ":" + value
		}
	}
	return result
}

func (t *Tags) Get(key string) string {
	return t.Body[key]
}

func (t *Tags) GetAsInt(key string) (int, bool) {
	value, ok := t.Body[key]
	if !ok {
		return 0, false
	}
	result, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return result, true
}
