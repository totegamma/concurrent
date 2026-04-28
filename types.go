package concrnt

import (
	"time"
)

const (
	ProofTypeEcrecover         = "concrnt-ecrecover-direct"
	ProofTypeDocumentReference = "document-reference"
	ProofTypeSubkey            = "concrnt-ecrecover-subkey"
	ProofTypeNone              = "none"
)

type SoftwareInfo struct {
	Version      string `json:"version"`
	BuildMachine string `json:"buildMachine"`
	BuildTime    string `json:"buildTime"`
	GoVersion    string `json:"goVersion"`
}

type WellKnownConcrnt struct {
	Version      string            `json:"version"`
	Domain       string            `json:"domain"`
	CSID         string            `json:"csid"`
	Layer        string            `json:"layer"`
	Dimension    string            `json:"dimension"` // for backwards compatibility
	Endpoints    map[string]string `json:"endpoints"`
	SoftwareInfo SoftwareInfo      `json:"softwareInfo"`
	Meta         map[string]any    `json:"meta,omitempty"`
}

type PolicyEntry struct {
	URL      *string            `json:"url"`
	Params   *map[string]any    `json:"params,omitempty"`
	Defaults *map[string]string `json:"defaults,omitempty"`
}

type Policy struct {
	Entries        []PolicyEntry `json:"entries"`
	VirtualParents *[]string     `json:"virtualParents,omitempty"`

	Source string `json:"source,omitempty"` // internal use
}

type Document[T any] struct {
	// CIP-1
	Key   string `json:"key"`
	Value T      `json:"value"`

	Author string `json:"author"`

	Schema string `json:"schema,omitempty"`

	CreatedAt time.Time `json:"createdAt"`

	// CIP-5
	Distributes *[]string `json:"distributes,omitempty"`

	// CIP-6
	Associate          *string `json:"associate,omitempty"`
	AssociationVariant *string `json:"associationVariant,omitempty"`

	// CIP-8
	Policy *Policy `json:"policy,omitempty"`
}

type LegacyDocument struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Owner     *string   `json:"owner,omitempty"`
	Schema    string    `json:"schema,omitempty"`
	Document  string    `json:"document"`
	Signature string    `json:"signature"`
	CDate     time.Time `json:"cdate"`
}

type SchemaDeleteType string

type Proof struct {
	Type      string  `json:"type"`
	Signature *string `json:"signature,omitempty"`
	Href      *string `json:"href,omitempty"`
	Key       *string `json:"key,omitempty"`
}

type SignedDocument struct {
	CCKV       *string                   `json:"cckv,omitempty"`
	CCFS       *string                   `json:"ccfs,omitempty"`
	Document   string                    `json:"document"`
	Proof      Proof                     `json:"proof"`
	References map[string]SignedDocument `json:"references,omitempty"`
}

type RegisterRequest[T any] struct {
	SignedDocument
	Meta        T       `json:"meta,omitempty"`
	InviteToken *string `json:"inviteToken,omitempty"`
}

type Event struct {
	Type       string                    `json:"type"`
	Source     string                    `json:"source"`
	URI        string                    `json:"uri"`
	References map[string]SignedDocument `json:"documents,omitempty"`
}

type RealtimeRequest struct {
	Type     string   `json:"type"`
	Prefixes []string `json:"prefixes"`
}

type AbuseReport struct {
	TargetURI string `json:"target" gorm:"type:text"`
	Body      string `json:"body" gorm:"type:text"`
}
