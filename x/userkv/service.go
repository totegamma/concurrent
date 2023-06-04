package userkv

// Service is userkv service
type Service struct {
    repository *Repository
}

// NewService is for wire.go
func NewService(repository *Repository) *Service {
    return &Service{repository: repository}
}

// Get returns a userkv by ID
func (s *Service) Get(userID string, key string) (string, error) {
    return s.repository.Get(userID + ":" + key)
}

// Upsert updates a userkv
func (s *Service) Upsert(userID string, key string, value string) error {
    return s.repository.Upsert(userID + ":" + key, value)
}

