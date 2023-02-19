package handler

import (
	"concurrent/domain/model"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
)

type ActivityPubHandler struct {
}

func NewActivityPubHandler() ActivityPubHandler {
    return ActivityPubHandler{}
}

func (h ActivityPubHandler) Handle(w http.ResponseWriter, r *http.Request) {
    _, id := filepath.Split(r.URL.Path)

    if id != "" {
        fmt.Println(id)
    }

    message := model.Act_Message{
        Context: "https://www.w3.org/ns/activitystreams",
        Type: "Person",
        Id: "https://concurrent.kokopi.me/ap/totegamma",
        Name: "ととがんま",
        PreferredUsername: "totegamma",
        Summary: "きつねエンジニアリング",
        Inbox: "https://concurrent.kokopi.me/ap/inbox",
        Outbox: "https://concurrent.kokopi.me/ap/outbox",
        Url: "https://concurrent.kokopi.me/ap/totegamma",
        Icon: model.Act_Icon{
            Type: "Image",
            MediaType: "image/png",
            Url: "https://s3.gammalab.net/profile/tote-icon.png",
        },
    }

    outputJson, err := json.Marshal(&message)
    if err != nil {
        panic(err)
    }

    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodGet:
            w.Header().Set("content-type", "application/activity+json; charset=utf-8")
            fmt.Print(string(outputJson))
            fmt.Fprint(w, string(outputJson))
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
