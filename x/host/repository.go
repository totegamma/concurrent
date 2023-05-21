package host

import (
    "gorm.io/gorm"
)

// Repository is host repository
type Repository struct {
    db *gorm.DB
}

// NewRepository is for wire.go
func NewRepository(db *gorm.DB) Repository {
    return Repository{db: db}
}

// Get returns a host by ID
func (r *Repository) Get(key string) Host {
    var host Host
    r.db.First(&host, "id = ?", key)
    return host
}

// Upsert updates a stream
func (r *Repository) Upsert(host *Host) {
    r.db.Save(&host)
}

// GetList returns list of schemas by schema
func (r *Repository) GetList() []Host {
    var hosts []Host
    r.db.Find(&hosts)
    return hosts
}

