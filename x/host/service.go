package host

import (
    "github.com/totegamma/concurrent/x/core"
)

// Service is stream service
type Service struct {
    repository *Repository
}

// NewService is for wire.go
func NewService(repository *Repository) *Service {
    return &Service{ repository }
}


// Upsert updates stream information
func (s *Service) Upsert(host *core.Host) error {
    return s.repository.Upsert(host)
}

// Get returns stream information by ID
func (s *Service) Get(key string) (core.Host, error) {
    return s.repository.Get(key)
}

// List returns streamList by schema
func (s *Service) List() ([]core.Host, error) {
    return s.repository.GetList()
}

