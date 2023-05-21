package activitypub

import (
	"github.com/totegamma/concurrent/x/character"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const profileSchema = "https://raw.githubusercontent.com/totegamma/concurrent-schemas/master/characters/profile/v1.json"
const domain = "concurrent.kokopi.me"

type WebfingerHandler struct {
    service character.CharacterService
}

func NewWebfingerHandler(service character.CharacterService) WebfingerHandler {
    return WebfingerHandler{service: service}
}

func (h WebfingerHandler) Handle(w http.ResponseWriter, r *http.Request) {

    switch r.Method {
        case http.MethodGet:
            resource := strings.Split(r.URL.Query().Get("resource"), ":")

            if resource[0] != "acct" {
                fmt.Println("webfinger request is not acct")
                return
            }

            subject := resource[1]
            fmt.Printf("acct fetched: %s\n", subject)

            hits := h.service.GetCharacters(subject, profileSchema)

            if len(hits) > 0 {
                webfinger := WebFinger {
                    Subject: "acct:" + subject + "@" + domain,
                    Links: []WebFinger_Link{
                        {
                            Rel: "self",
                            Type: "application/activity+json",
                            Href: "https://" + domain + "/ap/" + subject,
                        },
                    },
                }

                outputJson, err := json.Marshal(&webfinger)
                if err != nil {
                    panic(err)
                }

                fmt.Fprint(w, string(outputJson))
            }
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

