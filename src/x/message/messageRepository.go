package message

import (
    "gorm.io/gorm"
    "github.com/totegamma/concurrent/x/association"
)

type IMessageRepository interface {
    Create(message Message)
    GetAll() []Message
    GetFollowee() []Message
    Delete() Message
}

type MessageRepository struct {
    db *gorm.DB
}

func NewMessageRepository(db *gorm.DB) MessageRepository {
    return MessageRepository{db: db}
}

func (r *MessageRepository) Create(message *Message) string {
    r.db.Create(&message)
    return message.ID
}

func (r *MessageRepository) Get(key string) Message {
    var message Message
    var associations []association.Association
    r.db.First(&message, "id = ?", key)
    r.db.Table("associations").
        Select("associations.*").
        Joins("JOIN messages ON messages.id = associations.target").
        Where("messages.id = ? AND associations.id = ANY(messages.associations)", message.ID).
        Find(&associations)
    message.AssociationsData = associations
    return message
}

func (r *MessageRepository) GetAll() []Message {
    var messages []Message
    r.db.Find(&messages)
    return messages
}

func (r *MessageRepository) GetFollowee(followeeList []string) []Message {
    var messages []Message
    r.db.Where("author = ANY($1)", followeeList).Find(&messages)
    return messages
}

func (r *MessageRepository) Delete(id string) Message {
    var deleted Message
    r.db.Where("id = $1", id).Delete(&deleted)
    return deleted
}

