package host

import (
    "gorm.io/gorm"
    "github.com/totegamma/concurrent/x/core"
)

// Repository is host repository
type Repository struct {
    db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

// Get returns a host by ID
func (r *Repository) Get(key string) (core.Host, error) {
    var host core.Host
    err := r.db.First(&host, "id = ?", key).Error
    return host, err
}

// Upsert updates a stream
func (r *Repository) Upsert(host *core.Host) error {
    return r.db.Save(&host).Error
}

// GetList returns list of schemas by schema
func (r *Repository) GetList() ([]core.Host, error) {
    var hosts []core.Host
    err := r.db.Find(&hosts).Error
    return hosts, err
}

