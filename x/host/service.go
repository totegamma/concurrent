package host

import (
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
func (s *Service) Upsert(host *Host) {
    s.repository.Upsert(host)
}

// Get returns stream information by ID
func (s *Service) Get(key string) Host {
    return s.repository.Get(key)
}

// List returns streamList by schema
func (s *Service) List() []Host {
    hosts := s.repository.GetList()
    return hosts
}

