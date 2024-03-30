package core

type ResponseBase[T any] struct {
	Status  string `json:"status"`
	Content T      `json:"content"`
	Error   string `json:"error,omitempty"`
}
