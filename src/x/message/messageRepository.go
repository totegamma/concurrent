package message

import (
    "fmt"
    "gorm.io/gorm"
)

type IMessageRepository interface {
    Create(message Message)
    GetAll() []Message
    GetFollowee() []Message
}

type MessageRepository struct {
    db *gorm.DB
}

func NewMessageRepository(db *gorm.DB) MessageRepository {
    return MessageRepository{db: db}
}

func (r *MessageRepository) Create(message Message) string {
    r.db.Create(&message)
    return message.ID
}

func (r *MessageRepository) Get(key string) Message {
    var message Message
    r.db.First(&message, "id = ?", key)
    fmt.Println(message)
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

