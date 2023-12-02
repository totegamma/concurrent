package core

import (
    "strings"
)

// i.e.
// stream:write:ci8qvhep9dcpltmfq3fg@hub.concurrent.world
// stream:read:ci8qvhep9dcpltmfq3fg@hub.concurrent.world,ci8qvhep9dcpltmfq3fg@hub.concurrent.world // select multiple resources
// kv:write:*;settings:*:* // can be separated by semicolon
// *:*:* // all actions on all resources

type Scopes struct {
    Body map[string]*Scope // key is the resourcetype
}

type Scope struct {
    Action []string
    Resources []string
}


func NewScopes() *Scopes {
    return &Scopes{Body: make(map[string]*Scope)}
}

func ParseScopes(input string) *Scopes {
    scopes := &Scopes{Body: make(map[string]*Scope)}
    split := strings.Split(input, ";")
    for _, scope := range split {
        pair := strings.Split(scope, ":")
        if len(pair) == 3 {
            typ, action, resource := pair[0], pair[1], pair[2]
            if _, ok := scopes.Body[typ]; !ok {
                scopes.Body[typ] = &Scope{Action: []string{}, Resources: []string{}}
            }
            scopes.Body[typ].Action = append(scopes.Body[typ].Action, action)
            scopes.Body[typ].Resources = append(scopes.Body[typ].Resources, resource)
        }
    }
    return scopes
}

// CanPerform returns true if the given action is allowed on the given resource
func (s *Scopes) CanPerform(op string) bool {
    split := strings.Split(op, ":")

    if len(split) != 3 {
        return false
    }

    typ, action, resource := split[0], split[1], split[2]

    scope, ok := s.Body[typ]
    if !ok {
        scope, ok = s.Body["*"]
        if !ok {
            return false
        }
    }
    for i, a := range scope.Action {
        if a == "*" || a == action {
            if scope.Resources[i] == "*" || scope.Resources[i] == resource {
                return true
            }
        }
    }
    return false
}

