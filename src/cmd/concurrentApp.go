package main

import (
    "net/http"
    "github.com/totegamma/concurrent/x/message"
    "github.com/totegamma/concurrent/x/character"
    "github.com/totegamma/concurrent/x/association"
    "github.com/totegamma/concurrent/x/stream"
)

type ConcurrentApp struct {
    MessageHandler message.MessageHandler;
    CharacterHandler character.CharacterHandler;
    AssociationHandler association.AssociationHandler;
    StreamHandler stream.StreamHandler;
}

func NewConcurrentApp(messageHandler message.MessageHandler, 
                        characterHandler character.CharacterHandler,
                        associationhandler association.AssociationHandler,
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

