package domain

import "time"

type NotificationSubscription struct {
	VendorID     string    `json:"vendorID"`
	Owner        string    `json:"owner"`
	Schemas      []string  `json:"schemas"`
	Prefixes     []string  `json:"prefixes"`
	Subscription string    `json:"subscription"`
	CDate        time.Time `json:"cdate"`
	MDate        time.Time `json:"mdate"`
}
