package core

type Policy struct {
	Name      string
	Version   string
	Statement []Statement
	Params    map[string]any
}

type Statement struct {
	Action    []string
	Effect    string
	Condition Expr
}

type Expr struct {
	Operator string
	Args     []Expr
	Constant any
}
