package job

import (
	"time"
)

type Job struct {
	Type      string    `json:"type"`
	Payload   string    `json:"payload"`
	Scheduled time.Time `json:"scheduled"`
}
