// Package host is...
package host

// Profile is portable information of host
type Profile struct {
	ID     string `json:"fqdn" gorm:"type:text"`
	CCAddr string `json:"ccaddr" gorm:"type:char(42)"`
	Pubkey string `json:"pubkey" gorm:"type:text"`
}
