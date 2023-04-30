package activitypub

import (
	"github.com/totegamma/concurrent/x/character"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
)

type ActivityPubHandler struct {
    service character.CharacterService
}

func NewActivityPubHandler(service character.CharacterService) ActivityPubHandler {
    return ActivityPubHandler{service: service}
}

type Profile struct {
    Username string `json:"username"`
    Avatar string `json:"avatar"`
    Description string `json:"description"`
}

func (h ActivityPubHandler) Handle(w http.ResponseWriter, r *http.Request) {
    _, id := filepath.Split(r.URL.Path)

    if id != "" {
        fmt.Println(id)
    }

    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet:
            entityArr := h.service.GetCharacters(id, profileSchema)

            if len(entityArr) > 0 {
                entity := entityArr[0]

                profileJson := entity.Payload
                var profile Profile
                json.Unmarshal([]byte(profileJson), &profile)

                message := Act_Message{
                    Context: "https://www.w3.org/ns/activitystreams",
                    Type: "Person",
                    Id: "https://concurrent.kokopi.me/ap/" + id,
                    Name: profile.Username,
                    PreferredUsername: profile.Username,
                    Summary: profile.Description,
                    Inbox: "https://concurrent.kokopi.me/ap/inbox",
                    Outbox: "https://concurrent.kokopi.me/ap/outbox",
                    Url: "https://concurrent.kokopi.me/ap/" + id,
                    Icon: Act_Icon{
                        Type: "Image",
                        MediaType: "image/png",
                        Url: profile.Avatar,
                    },
                }

                outputJson, err := json.Marshal(&message)
                if err != nil {
                    panic(err)
                }

                w.Header().Set("content-type", "application/activity+json; charset=utf-8")
                fmt.Print(string(outputJson))
                fmt.Fprint(w, string(outputJson))
            }

            return
        case http.MethodPost:
            return
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}
