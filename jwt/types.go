package jwt

// Header is jwt header type
type Header struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
	KeyID     string `json:"kid,omitempty"`
}

// Claims is jwt payload type
type Claims struct {
	Issuer         string `json:"iss,omitempty"` // 発行者
	Subject        string `json:"sub,omitempty"` // 用途
	Audience       string `json:"aud,omitempty"` // 想定利用者
	ExpirationTime string `json:"exp,omitempty"` // 失効時刻
	IssuedAt       string `json:"iat,omitempty"` // 発行時刻
	JWTID          string `json:"jti,omitempty"` // JWT ID
}
