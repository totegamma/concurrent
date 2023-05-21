package stream

import (
    "time"
    "github.com/lib/pq"
)

// Stream is one of a base object of concurrent
type Stream struct {
    ID string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Author string `json:"author" gorm:"type:char(42)"`
    Maintainer pq.StringArray `json:"maintainer" gorm:"type:char(42)[];default:'{}'"`
    Writer pq.StringArray `json:"writer" gorm:"type:char(42)[];default:'{}'"`
    Reader pq.StringArray `json:"reader" gorm:"type:char(42)[];default:'{}'"`
    Schema string `json:"schema" gorm:"type:varchar(256)"`
    Meta string `json:"meta" gorm:"type:json;default:'{}'"`
    Signature string `json:"signature" gorm:"type:char(130)"`
    CDate time.Time `json:"cdate" gorm:"type:timestamp with time zone;not null;default:clock_timestamp()"`
}

type postQuery struct {
    Stream string `json:"stream"`
    ID string `json:"id"`
}

