package model

type MessagesResponse struct {
    Messages []Message `json:"messages"`
}

type MessageResponse struct {
    Message Message `json:"message"`
}


type CharactersResponse struct {
    Characters []Character `json:"characters"`
}

