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
func (r *Repository) Get(key string) core.Host {
    var host core.Host
    r.db.First(&host, "id = ?", key)
    return host
}

// Upsert updates a stream
func (r *Repository) Upsert(host *core.Host) {
    r.db.Save(&host)
}

// GetList returns list of schemas by schema
func (r *Repository) GetList() []core.Host {
    var hosts []core.Host
    r.db.Find(&hosts)
    return hosts
}

