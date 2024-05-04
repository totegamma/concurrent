package core

import ()

// Event is websocket root packet model
type Event struct {
	Timeline  string       `json:"timeline"` // stream full id (ex: <streamID>@<domain>)
	Item      TimelineItem `json:"item,omitempty"`
	Resource  any          `json:"resource,omitempty"`
	Document  string       `json:"document"`
	Signature string       `json:"signature"`
}

type Chunk struct {
	Key   string         `json:"key"`
	Items []TimelineItem `json:"items"`
}

type RequestContext struct {
	Requester Entity
	Document  any
	Self      any
	Params    map[string]any
}

type Policy struct {
	Name       string      `json:"name"`
	Version    string      `json:"version"`
	Statements []Statement `json:"statements"`
}

type Statement struct {
	Actions   []string `json:"actions"`
	Effect    string   `json:"effect"`
	Condition Expr     `json:"condition"`
}

type Expr struct {
	Operator string `json:"op"`
	Args     []Expr `json:"args"`
	Constant any    `json:"const"`
}

type EvalResult struct {
	Operator string       `json:"op"`
	Args     []EvalResult `json:"args"`
	Result   any          `json:"result"`
	Error    string       `json:"error"`
}
