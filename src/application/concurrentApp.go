package application

import (
    "net/http"
    "concurrent/presentation/handler"
    "concurrent/x/stream"
)

type ConcurrentApp struct {
    MessageHandler handler.MessageHandler;
    CharacterHandler handler.CharacterHandler;
    AssociationHandler handler.AssociationHandler;
    StreamHandler stream.StreamHandler;
}

func NewConcurrentApp(messageHandler handler.MessageHandler, 
                        characterHandler handler.CharacterHandler,
                        associationhandler handler.AssociationHandler,
                        streamhandler stream.StreamHandler,
                    ) ConcurrentApp {
    return ConcurrentApp{
        MessageHandler: messageHandler,
        CharacterHandler: characterHandler,
        AssociationHandler: associationhandler,
        StreamHandler: streamhandler,
    }
}

func (app *ConcurrentApp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    switch r.URL.Path {
    case "/messages":
        app.MessageHandler.Handle(w, r)
    case "/characters":
        app.CharacterHandler.Handle(w, r)
    case "/associations":
        app.AssociationHandler.Handle(w, r)
    case "/stream":
        app.StreamHandler.Handle(w, r)
    }
}

