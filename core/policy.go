package core

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
	Action    []string `json:"action"`
	Effect    string   `json:"effect"`
	Condition Expr     `json:"condition"`
}

type Expr struct {
	Operator string `json:"op"`
	Args     []Expr `json:"args"`
	Constant any    `json:"constant"`
}
