package association

import (
    "net/http"
    "encoding/json"
    "fmt"
    "bytes"
    "io"
)

type AssociationHandler struct {
    service AssociationService
}

func NewAssociationHandler(service AssociationService) AssociationHandler {
    return AssociationHandler{service: service}
}

func (h AssociationHandler) Handle(w http.ResponseWriter, r *http.Request) {

    w.Header().Set("Access-Control-Allow-Headers", "*")
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set( "Access-Control-Allow-Methods","GET, POST, PUT, DELETE, OPTIONS" )
    switch r.Method {
        case http.MethodPost:
            body := r.Body
            defer body.Close()

            buf := new(bytes.Buffer)
            io.Copy(buf, body)

            var association Association
            json.Unmarshal(buf.Bytes(), &association)

            h.service.PostAssociation(association)
        case http.MethodOptions:
            return
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
            fmt.Fprint(w, "Method not allowed.")
    }
}

