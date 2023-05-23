// Package agent runs some scheduled tasks
package agent

import (
    "log"
    "time"
    "github.com/totegamma/concurrent/x/host"
    "github.com/totegamma/concurrent/x/entity"
)

// Agent is...
type Agent struct {
    hostService* host.Service
    entityService* entity.Service
}

func NewAgent(host *host.Service, entity *entity.Service) *Agent {
    return &Agent{hostService: host, entityService: entity}
}


func (a *Agent) collectUsers() {
    hosts := a.hostService.List()
    for _, host := range hosts {
        log.Printf("collect users for %v\n", host)
        a.entityService.PullRemoteEntities(host)
    }
}


// Boot starts agent
func (a *Agent) Boot() {
    log.Printf("agent start!")
    ticker := time.NewTicker(60 * time.Second)
    go func() {
        for range ticker.C {
            a.collectUsers()
        }
    }()
}



