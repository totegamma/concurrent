package model

type MessagesResponse struct {
    Messages []Message `json:"messages"`
}

type CharactersResponse struct {
    Characters []Character `json:"characters"`
}

