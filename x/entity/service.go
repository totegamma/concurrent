package entity

import (
)

// Service is entity service
type Service struct {
    repository Repository
}

// NewService is for wire.go
func NewService(repository Repository) Service {
    return Service{ repository }
}


// Create updates stream information
func (s *Service) Create(ccaddr string, meta string) {
    s.repository.Create(&Entity{
        ID: ccaddr,
        Role: "default",
        Meta: meta,
    })
}

// Get returns stream information by ID
func (s *Service) Get(key string) Entity {
    return s.repository.Get(key)
}

// List returns streamList by schema
func (s *Service) List() []SafeEntity {
    entities := s.repository.GetList()
    return entities
}

