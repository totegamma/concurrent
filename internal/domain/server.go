package domain

import (
	"github.com/totegamma/concrnt-playground"
	"github.com/totegamma/concrnt-playground/impl/tags"
)

// Server represents a remote Concrnt node descriptor.
type Server struct {
	TagString string                   `json:"tag,omitempty"`
	WellKnown concrnt.WellKnownConcrnt `json:"wellKnown"`
}

func (s *Server) Domain() string {
	return s.WellKnown.Domain
}

func (s *Server) CSID() string {
	return s.WellKnown.CSID
}

func (s *Server) Layer() string {
	return s.WellKnown.Layer
}

func (s *Server) Version() string {
	return s.WellKnown.Version
}

func (s *Server) Tag() tags.Tags {
	return tags.Parse(s.TagString)
}
