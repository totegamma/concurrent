package socket

import ()

type Service interface {
}

// Service is socket service
type service struct {
}

// NewService is for wire.go
func NewService() Service {
	return &service{}
}
