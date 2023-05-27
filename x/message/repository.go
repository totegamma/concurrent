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
func (r *Repository) Create(message *core.Message) (string, error) {
    err := r.db.Create(&message).Error
    return message.ID, err
}

// Get returns a message with associaiton data
func (r *Repository) Get(key string) (core.Message, error) {
    var message core.Message
    err := r.db.Preload("Associations").First(&message, "id = ?", key).Error
    return message, err
}

// Delete deletes an message
func (r *Repository) Delete(id string) (core.Message, error) {
    var deleted core.Message
    err := r.db.Where("id = $1", id).Delete(&deleted).Error
    return deleted, err
}

