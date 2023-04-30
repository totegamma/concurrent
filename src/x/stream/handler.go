package stream

import (
    "net/http"
)

type StreamHandler struct {
    service StreamService
}

func NewStreamHandler(service StreamService) StreamHandler {
    return StreamHandler{service: service}
}

func (h StreamHandler) Handle(w http.ResponseWriter, r *http.Request) {
    h.service.PostRedis()
}
