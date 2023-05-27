// Package agent runs some scheduled tasks
package agent

import (
    "log"
    "time"
    "github.com/totegamma/concurrent/x/host"
    "github.com/totegamma/concurrent/x/entity"
    "github.com/totegamma/concurrent/x/socket"
)

// Agent is...
type Agent struct {
    socket* socket.Service
    host* host.Service
    entity* entity.Service
}

func NewAgent(socket *socket.Service, host *host.Service, entity *entity.Service) *Agent {
    return &Agent{socket, host, entity}
}


func (a *Agent) collectUsers() {
    hosts := a.host.List()
    for _, host := range hosts {
        log.Printf("collect users for %v\n", host)
        a.entity.PullRemoteEntities(host)
    }
}


// Boot starts agent
func (a *Agent) Boot() {
    log.Printf("agent start!")
    ticker := time.NewTicker(60 * time.Second)
    go func() {
        for range ticker.C {
            a.collectUsers()
            a.socket.UpdateConnections()
        }
    }()
}



