package model

type WebFinger_Link struct {
    Rel string `json:"rel"`
    Type string `json:"type"`
    Href string `json:"href"`
}

type WebFinger struct {
    Subject string `json:"subject"`
    Links []WebFinger_Link `json:"links"`
}

