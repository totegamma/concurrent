package activitypub

import (
    "log"
    "time"
    "context"
    "encoding/json"

    "github.com/totegamma/concurrent/x/message"
    "github.com/totegamma/concurrent/x/stream"
)

var ctx = context.Background()

// Boot starts agent
func (h *Handler) Boot() {

    ticker10 := time.NewTicker(10 * time.Second)
    workers := make(map[string]context.CancelFunc)

    for {
        <-ticker10.C

        jobs, err := h.repo.GetAllFollows(ctx)
        if err != nil {
            log.Printf("error: %v", err)
        }

        for _, job := range jobs {
            if _, ok := workers[job.ID]; !ok {
                log.Printf("start worker %v", job.ID)
                ctx, cancel := context.WithCancel(context.Background())
                workers[job.ID] = cancel
                person, err := h.repo.GetPersonByID(ctx, job.PublisherUserID)
                if err != nil {
                    log.Printf("error: %v", err)
                }
                home := person.HomeStream
                if home == "" {
                    continue
                }
                pubsub := h.rdb.Subscribe(ctx)
                pubsub.Subscribe(ctx, home)

                go func(ctx context.Context, job ApFollow) {
                    for {
                        select {
                        case <-ctx.Done():
                            log.Printf("worker %v done", job.ID)
                            return
                        default:
                            pubsubMsg, err := pubsub.ReceiveMessage(ctx)
                            if ctx.Err() != nil {
                                continue
                            }
                            if err != nil {
                                log.Printf("error: %v", err)
                            }

                            var streamEvent stream.Event
                            err = json.Unmarshal([]byte(pubsubMsg.Payload), &streamEvent)
                            if err != nil {
                                log.Printf("error: %v", err)
                            }

                            msg, err := h.message.Get(context.TODO(), streamEvent.Body.ID)
                            if err != nil {
                                log.Printf("error: %v", err)
                            }

                            var signedObject message.SignedObject
                            err = json.Unmarshal([]byte(msg.Payload), &signedObject)
                            if err != nil {
                                log.Printf("error: %v", err)
                            }

                            body := signedObject.Body

                            var text string
                            if signedObject.Schema == "https://raw.githubusercontent.com/totegamma/concurrent-schemas/master/messages/note/0.0.1.json" {
                                t, ok := body.(map[string]interface{})["body"].(string)
                                if !ok {
                                    log.Printf("parse error")
                                }
                                text = t
                            }

                            create := Create {
                                Context: []string{"https://www.w3.org/ns/activitystreams"},
                                Type: "Create",
                                ID: "https://" + h.config.Concurrent.FQDN + "/ap/note/" + msg.ID,
                                Actor: "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + job.PublisherUserID,
                                To: []string{"https://www.w3.org/ns/activitystreams#Public"},
                                Object: Note {
                                    Context: "https://www.w3.org/ns/activitystreams",
                                    Type: "Note",
                                    ID: "https://" + h.config.Concurrent.FQDN + "/ap/note/" + msg.ID,
                                    AttributedTo: "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + job.PublisherUserID,
                                    Content: text,
                                    Published: msg.CDate.Format(time.RFC3339),
                                    To: []string{"https://www.w3.org/ns/activitystreams#Public"},
                                },
                            }

                            err = h.PostToInbox(job.SubscriberInbox, create, job.PublisherUserID)
                            if err != nil {
                                log.Printf("error: %v", err)
                            }
                        }
                    }
                } (ctx, job)
            }
        }

        // create job id list
        var jobIDs []string
        for _, job := range jobs {
            jobIDs = append(jobIDs, job.ID)
        }

        for routineID, cancel := range workers {
            if !isInList(routineID, jobIDs) {
                log.Printf("cancel worker %v", routineID)
                cancel()
                delete(workers, routineID)
            }
        }
    }
}

func isInList(server string, list []string) bool {
    for _, s := range list {
        if s == server {
            return true
        }
    }
    return false
}

