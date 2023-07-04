package activitypub

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/stream"
)

// Boot starts agent
func (h *Handler) Boot() {

	ticker10 := time.NewTicker(10 * time.Second)
	workers := make(map[string]context.CancelFunc)

	for {
		<-ticker10.C
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

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
				entity, err := h.repo.GetEntityByID(ctx, job.PublisherUserID)
				if err != nil {
					log.Printf("error: %v", err)
				}
				ownerID := entity.CCAddr
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
								continue
							}

							var streamEvent stream.Event
							err = json.Unmarshal([]byte(pubsubMsg.Payload), &streamEvent)
							if err != nil {
								log.Printf("error: %v", err)
								continue
							}

							if streamEvent.Body.Author != ownerID {
								continue
							}

							msg, err := h.message.Get(ctx, streamEvent.Body.ID)
							if err != nil {
								log.Printf("error: %v", err)
								continue
							}

							var signedObject message.SignedObject
							err = json.Unmarshal([]byte(msg.Payload), &signedObject)
							if err != nil {
								log.Printf("error: %v", err)
								continue
							}

							body := signedObject.Body

							var text string
							if signedObject.Schema == "https://raw.githubusercontent.com/totegamma/concurrent-schemas/master/messages/note/0.0.1.json" {
								t, ok := body.(map[string]interface{})["body"].(string)
								if !ok {
									log.Printf("parse error")
									continue
								}
								text = t
							} else {
								continue
							}

							create := Create{
								Context: []string{"https://www.w3.org/ns/activitystreams"},
								Type:    "Create",
								ID:      "https://" + h.config.Concurrent.FQDN + "/ap/note/" + msg.ID,
								Actor:   "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + job.PublisherUserID,
								To:      []string{"https://www.w3.org/ns/activitystreams#Public"},
								Object: Note{
									Context:      "https://www.w3.org/ns/activitystreams",
									Type:         "Note",
									ID:           "https://" + h.config.Concurrent.FQDN + "/ap/note/" + msg.ID,
									AttributedTo: "https://" + h.config.Concurrent.FQDN + "/ap/acct/" + job.PublisherUserID,
									Content:      text,
									Published:    msg.CDate.Format(time.RFC3339),
									To:           []string{"https://www.w3.org/ns/activitystreams#Public"},
								},
							}

							err = h.PostToInbox(ctx, job.SubscriberInbox, create, job.PublisherUserID)
							if err != nil {
								log.Printf("error: %v", err)
								continue
							}
						}
					}
				}(ctx, job)
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
