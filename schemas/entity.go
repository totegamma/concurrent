package schemas

type Entity struct {
	Domain         string  `json:"domain"`
	Alias          *string `json:"alias,omitempty"`
	AliasProofType *string `json:"alias_proof_type,omitempty"`
}
