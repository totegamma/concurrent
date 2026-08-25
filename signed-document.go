package concrnt

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/concrnt/concrnt/cdid"
	"github.com/concrnt/concrnt/schemas"
)

// This file defines the CIP-1 signed-document envelope (SignedDocument and
// its Proof) together with the document derivations built on it:
// constructions that take an existing signed document and produce the derived
// document a server records on its behalf — the distribution Reference a
// distribute destination stores (CIP-7 §4.1) and the acked/unacked mirror the
// ackee's server stores (CIP-10 §5.2). Both derivations are deterministic
// given the same inputs, so retries and repair backfills reproduce the same
// document (and CDID) and no-op on the commit dedup. Verification lives in
// verify.go.

const (
	ProofTypeEcrecover         = "concrnt-ecrecover-direct"
	ProofTypeDocumentReference = "document-reference"
	ProofTypeSubkey            = "concrnt-ecrecover-subkey"
	ProofTypeAckReference      = "ack-reference"
	ProofTypeNone              = "none"
)

type Proof struct {
	Type      string  `json:"type"`
	Signature *string `json:"signature,omitempty"`
	Href      *string `json:"href,omitempty"`
	Key       *string `json:"key,omitempty"`

	// ack-reference proofs (CIP-10): the original ack/unack signed document
	// this acked/unacked mirror commit derives from, embedded verbatim so the
	// mirror verifies self-contained (dump replay included).
	Document *string `json:"document,omitempty"`
	Proof    *Proof  `json:"proof,omitempty"`
}

type SignedDocument struct {
	CCKV       *string                   `json:"cckv,omitempty"`
	CCFS       *string                   `json:"ccfs,omitempty"`
	Document   string                    `json:"document"`
	Proof      Proof                     `json:"proof"`
	References map[string]SignedDocument `json:"references,omitempty"`

	// IsPublic is an internal, publish-time annotation (not part of the CCAPI
	// wire format): whether an anonymous requester may read this document
	// (CIP-11 §3.2 baseline). nil means unevaluated, which consumers must
	// treat as not public. It exists only on the redis pubsub Event contract;
	// Event.PublicView strips it at the websocket edge and it is never part of
	// commit responses, query results or federation payloads.
	IsPublic *bool `json:"isPublic,omitempty"`
}

// StripInternalFlags removes the internal IsPublic annotations at every
// nesting level. Called on inbound documents so the spec-external flag never
// echoes back on API responses. The receiver is not mutated.
func (sd SignedDocument) StripInternalFlags() SignedDocument {
	sd.IsPublic = nil
	if len(sd.References) > 0 {
		refs := make(map[string]SignedDocument, len(sd.References))
		for uri, ref := range sd.References {
			refs[uri] = ref.StripInternalFlags()
		}
		sd.References = refs
	}
	return sd
}

// DocumentIDFor derives a document's content+time CDID, the id it is stored
// under. It is time-prefixed and content-hashed, so string comparison orders
// documents by createdAt with a deterministic content tiebreaker.
func DocumentIDFor(document string, createdAt time.Time) string {
	hash := GetHash([]byte(document))
	var hash10 [10]byte
	copy(hash10[:], hash[:10])
	return cdid.New(hash10, createdAt).String()
}

// DistributionReferenceKey derives the key a distribution Reference for href
// is stored under at destination (CIP-7 §4.1): <destination>/<hash-CDID(href)>,
// stable across accept-if-newer overwrites. Creation
// (DeriveDistributionReference) and the delete-side sweep both address
// reference rows through this one rule.
func DistributionReferenceKey(destination string, href string) (string, error) {
	return url.JoinPath(destination, cdid.MakeHash([]byte(href)).String())
}

// DeriveAcked derives the canonical acked/unacked mirror commit for this
// ack/unack signed document (CIP-10 §5.2): the mirror document is the
// original with only its kind flipped (ack → acked, unack → unacked),
// re-marshalled through the canonical Document field set, and the mirror's
// ack-reference proof embeds this signed document verbatim so the mirror
// verifies self-contained (dump replay included). A verifier recomputes this
// derivation from the proof-embedded original and requires byte equality —
// which pins kind correspondence and every other field in one comparison.
func (sd SignedDocument) DeriveAcked() (SignedDocument, error) {
	var doc Document[json.RawMessage]
	if err := json.Unmarshal([]byte(sd.Document), &doc); err != nil {
		return SignedDocument{}, errors.Join(errors.New("failed to decode ack document for mirror derivation"), err)
	}

	switch doc.Kind {
	case "ack":
		doc.Kind = "acked"
	case "unack":
		doc.Kind = "unacked"
	default:
		return SignedDocument{}, fmt.Errorf("ack mirror can only be derived from ack/unack documents, got kind %q", doc.Kind)
	}

	mirror, err := json.Marshal(doc)
	if err != nil {
		return SignedDocument{}, errors.Join(errors.New("failed to encode ack mirror document"), err)
	}

	original := sd.Document
	originalProof := sd.Proof
	return SignedDocument{
		Document: string(mirror),
		Proof: Proof{
			Type:     ProofTypeAckReference,
			Document: &original,
			Proof:    &originalProof,
		},
	}, nil
}

// DeriveDistributionReference derives the auto-generated Reference document
// (CIP-7 §4.1) that lands a copy of this signed document under a distribute
// destination. ref.Href is the URI the reference points back at (the
// document's cckv key, or its ccfs URI for keyless kinds) and doubles as the
// key segment source: the key is <destination>/<hash-CDID(href)>, stable
// across accept-if-newer overwrites. createdAt is the reference's own stamp
// (the server's current time on the live path — retries MUST reuse the first
// derivation, CIP-7 §4.2.1). This signed document is inlined under
// References[href] so the destination verifies without fetching; callers add
// any further references (e.g. the author's entity document) on top.
func (sd SignedDocument) DeriveDistributionReference(destination string, ref schemas.Reference, createdAt time.Time) (SignedDocument, error) {
	if ref.Href == "" {
		return SignedDocument{}, errors.New("distribution reference requires a href")
	}

	var doc Document[json.RawMessage]
	if err := json.Unmarshal([]byte(sd.Document), &doc); err != nil {
		return SignedDocument{}, errors.Join(errors.New("failed to decode document for reference derivation"), err)
	}

	key, err := DistributionReferenceKey(destination, ref.Href)
	if err != nil {
		return SignedDocument{}, errors.Join(fmt.Errorf("failed to derive reference key under %s", destination), err)
	}

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       key,
		Value:     ref,
		Author:    doc.Author,
		Schema:    schemas.ReferenceURL,
		CreatedAt: createdAt,
	}
	refBytes, err := json.Marshal(refDoc)
	if err != nil {
		return SignedDocument{}, errors.Join(errors.New("failed to encode reference document"), err)
	}

	href := ref.Href
	return SignedDocument{
		Document: string(refBytes),
		Proof: Proof{
			Type: ProofTypeDocumentReference,
			Href: &href,
		},
		References: map[string]SignedDocument{
			ref.Href: sd,
		},
	}, nil
}
