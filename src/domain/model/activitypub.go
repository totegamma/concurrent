package model

type Act_Icon struct {
    Type string `json:"type"`
    MediaType string `json:"mediaType"`
    Url string `json:"url"`
}

type Act_Message struct {
    Context string `json:"@context"`
    Type string `json:"type"`
    Id string `json:"id"`
    Name string `json:"name"`
    PreferredUsername string `json:"preferredUsername"`
    Summary string `json:"summary"`
    Inbox string `json:"inbox"`
    Outbox string `json:"outbox"`
    Url string `json:"url"`
    Icon Act_Icon `json:"icon"`
}

