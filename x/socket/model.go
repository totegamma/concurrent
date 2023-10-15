package socket

type Request struct {
	Type string `json:"type"`
	Channels []string `json:"channels"`
}

type StreamEvent struct {
	Stream string `json:"stream"`
	Type   string `json:"type"`
	Action string `json:"action"`
}
