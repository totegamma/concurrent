package socket

type ChannelRequest struct {
	Channels []string `json:"channels"`
}

type StreamEvent struct {
	Stream string `json:"stream"`
	Type   string `json:"type"`
	Action string `json:"action"`
}
