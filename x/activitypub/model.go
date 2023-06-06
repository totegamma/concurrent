package activitypub

// Entity is a db model of an ActivityPub entity.
type Entity struct {
    ID string `json:"id" gorm:"type:char(42)"`
    Name string `json:"name" gorm:"type:text"`
    Summary string `json:"summary" gorm:"type:text"`
    ProfileURL string `json:"profile_url" gorm:"type:text"`
    IconURL string `json:"icon_url" gorm:"type:text"`
}

// TableName returns the table name of the model.
func (Entity) TableName() string {
    return "ap_entities"
}

// WebFinger is a struct for a WebFinger response.
type WebFinger struct {
    Subject string `json:"subject"`
    Links []WebFingerLink `json:"links"`
}

// WebFingerLink is a struct for the links field of a WebFinger response.
type WebFingerLink struct {
    Rel string `json:"rel"`
    Type string `json:"type"`
    Href string `json:"href"`
}

// ActivityPub is a struct for an ActivityPub actor.
type ActivityPub struct {
    Context string `json:"@context"`
    Type string `json:"type"`
    ID string `json:"id"`
    Inbox string `json:"inbox"`
    Outbox string `json:"outbox"`
    Followers string `json:"followers"`
    Following string `json:"following"`
    Liked string `json:"liked"`
    PreferredUsername string `json:"preferredUsername"`
    Name string `json:"name"`
    Summary string `json:"summary"`
    URL string `json:"url"`
    Icon Icon `json:"icon"`
}

// Icon is a struct for the icon field of an actor.
type Icon struct {
    Type string `json:"type"`
    MediaType string `json:"mediaType"`
    URL string `json:"url"`
}

// Message is a struct for the actor object.
type Message struct {
    Context string `json:"@context"`
    Type string `json:"type"`
    ID string `json:"id"`
    Name string `json:"name"`
    PreferredUsername string `json:"preferredUsername"`
    Summary string `json:"summary"`
    Inbox string `json:"inbox"`
    Outbox string `json:"outbox"`
    URL string `json:"url"`
    Icon Icon `json:"icon"`
}

// Activity is a struct for an ActivityPub activity.
type Activity struct {
    Context string `json:"@context"`
    Type string `json:"type"`
    ID string `json:"id"`
    Actor string `json:"actor"`
    To []string `json:"to"`
    CC []string `json:"cc"`
    Object string `json:"object"`
    Published string `json:"published"`
    Summary string `json:"summary"`
    Content string `json:"content"`
}

// Object is a struct for an ActivityPub object.
type Object struct {
    Context string `json:"@context"`
    Type string `json:"type"`
    ID string `json:"id"`
    Name string `json:"name"`
    Content string `json:"content"`
    URL string `json:"url"`
    Attachment []Attachment `json:"attachment"`
}

// Attachment is a struct for an ActivityPub attachment.
type Attachment struct {
    Type string `json:"type"`
    MediaType string `json:"mediaType"`
    URL string `json:"url"`
}

