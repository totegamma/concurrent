package domain

import (
	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/impl/tags"
)

// Entity represents the core user/server identity without persistence concerns.
type Entity struct {
	ID        string  `json:"ccid"`
	Alias     *string `json:"alias,omitempty"`
	Domain    string  `json:"domain"`
	TagString string  `json:"tag,omitempty"`
}

func (e *Entity) Tag() tags.Tags {
	return tags.Parse(e.TagString)
}

func (e *Entity) CCKV() string {
	return concrnt.ComposeCCURI("cckv", e.ID, "")
}

// EntityMeta is auxiliary metadata associated with an Entity.
type EntityMeta struct {
	ID      string  `json:"ccid"`
	Inviter *string `json:"inviter,omitempty"`
	Info    string  `json:"info"`
}
