package handler

import (
    "fmt"
    "net/http"
    "encoding/json"
    "concurrent/domain/model"
)

type WebfingerHandler struct {
}

func NewWebFingerHandler() WebfingerHandler {
    return WebfingerHandler{}
}

func (h WebfingerHandler) Handle(w http.ResponseWriter, r *http.Request) {

    switch r.Method {
        case http.MethodGet:
            webfinger := model.WebFinger {
                Subject: "acct:totegamma@concurrent.kokopi.me",
                Links: []model.WebFinger_Link{
                    {
                        Rel: "self",
                        Type: "application/activity+json",
                        Href: "https://concurrent.kokopi.me/ap/totegamma",
                    },
                },
            }

            outputJson, err := json.Marshal(&webfinger)
            if err != nil {
                panic(err)
            }

            fmt.Fprint(w, string(outputJson))
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

/*
[thotgamma@20:30].../concurrent/src$ curl -s -H "Accept: application/jrd+json, application/json" 'http://misskey.gammalab.intra/.well-known/webfinger?resource=acct:@totegamma' | jq
{
  "subject": "acct:totegamma@misskey-dev.house.gammalab.net",
  "links": [
    {
      "rel": "self",
      "type": "application/activity+json",
      "href": "http://misskey-dev.house.gammalab.net/users/9az9ueizu3"
    },
    {
      "rel": "http://webfinger.net/rel/profile-page",
      "type": "text/html",
      "href": "http://misskey-dev.house.gammalab.net/@totegamma"
    },
    {
      "rel": "http://ostatus.org/schema/1.0/subscribe",
      "template": "http://misskey-dev.house.gammalab.net/authorize-follow?acct={uri}"
    }
  ]
}
[thotgamma@20:30].../concurrent/src$
*/

