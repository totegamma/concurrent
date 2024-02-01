package domain

// Profile is portable information of host
type Profile struct {
	ID     string `json:"fqdn" gorm:"type:text"`
	CCID   string `json:"ccid" gorm:"type:char(42)"`
	Pubkey string `json:"pubkey" gorm:"type:text"`
}

type ProfileResponse struct {
	Status  string  `json:"status"`
	Content Profile `json:"content"`
}
