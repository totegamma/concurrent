package application

import (
    "concurrent/presentation/handler"
    "net/http"
)

type ConcurrentApp struct {
    messageHandler handler.MessageHandler;
    characterHandler handler.CharacterHandler;
    associationHandler handler.AssociationHandler;
}

func NewConcurrentApp(messageHandler handler.MessageHandler, 
                        characterHandler handler.CharacterHandler,
                        associationhandler handler.AssociationHandler) ConcurrentApp {
    return ConcurrentApp{
        messageHandler: messageHandler,
        characterHandler: characterHandler,
        associationHandler: associationhandler,
    }
}

func (app *ConcurrentApp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    switch r.URL.Path {
    case "/messages":
        app.messageHandler.Handle(w, r)
    case "/characters":
        app.characterHandler.Handle(w, r)
    case "/associations":
        app.associationHandler.Handle(w, r)
    }
}

