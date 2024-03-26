package store

type commitRequest struct {
	Document  string `json:"document"`
	Signature string `json:"signature"`
	Option    string `json:"option"`
}
