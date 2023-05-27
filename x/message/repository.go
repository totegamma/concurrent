package message

import (
	"gorm.io/gorm"
    "github.com/totegamma/concurrent/x/core"
)

// Repository is message repository
type Repository struct {
    db *gorm.DB
}

// NewRepository is used for wire.go
func NewRepository(db *gorm.DB) *Repository {
    return &Repository{db: db}
}

// Create creates new message
func (r *Repository) Create(message *core.Message) string {
    r.db.Create(&message)
    return message.ID
}

// Get returns a message with associaiton data
func (r *Repository) Get(key string) core.Message {
    var message core.Message
    r.db.Preload("Associations").First(&message, "id = ?", key)
    return message
}

// Delete deletes an message
func (r *Repository) Delete(id string) core.Message {
    var deleted core.Message
    r.db.Where("id = $1", id).Delete(&deleted)
    return deleted
}

