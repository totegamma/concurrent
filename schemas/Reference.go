package schemas

import (
	"time"
)

type Reference struct {
	Href      string     `json:"href"`
	Schema    *string    `json:"schema,omitempty"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}
