package application

import (
    "net/http"
    "concurrent/presentation/handler"
    "concurrent/x/stream"
)

type ConcurrentApp struct {
    messageHandler handler.MessageHandler;
    characterHandler handler.CharacterHandler;
    associationHandler handler.AssociationHandler;
    streamHandler stream.StreamHandler;
}

func NewConcurrentApp(messageHandler handler.MessageHandler, 
                        characterHandler handler.CharacterHandler,
                        associationhandler handler.AssociationHandler,
                        streamhandler stream.StreamHandler,
                    ) ConcurrentApp {
    return ConcurrentApp{
        messageHandler: messageHandler,
        characterHandler: characterHandler,
        associationHandler: associationhandler,
        streamHandler: streamhandler,
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
    case "/stream":
        app.streamHandler.Handle(w, r)
    }
}

