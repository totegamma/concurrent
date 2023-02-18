package main

import (
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

func NewMessageRepository(db *gorm.DB) *MessageRepository {
    return &MessageRepository{db: db}
}

func (r *MessageRepository) Create(message Message) {
    r.db.Create(&message)
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

