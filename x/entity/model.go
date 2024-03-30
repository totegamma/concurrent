// Package entity handles concurrent object Entity
package entity

import (
	"github.com/totegamma/concurrent/x/core"
)

type affiliationOption struct {
	Info       string `json:"info"`
	Invitation string `json:"invitation"`
}

type entityResponse struct {
	Status  string      `json:"status"`
	Content core.Entity `json:"content"`
}

/*
type EntityDocResponse struct {
    core.EntityAffiliation

    Document string `json:"_document"`
    Signature string `json:"_signature"`
    Extension *ExtensionDocResponse `json:"_extension,omitempty"`
    CDate time.Time `json:"_cdate"`
}

type ExtensionDocResponse struct {
    core.EntityExtension

    Document string `json:"_document"`
    Signature string `json:"_signature"`
}
*/
