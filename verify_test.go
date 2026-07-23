package concrnt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/concrnt/concrnt/cdid"
	"github.com/concrnt/concrnt/schemas"
)

type testRecordValue struct {
	Foo string `json:"foo"`
}

// newTestIdentity generates a fresh secp256k1 key pair and its CCID.
func newTestIdentity(t *testing.T) (ccid string, privKeyHex string) {
	t.Helper()

	priv := make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	privKeyHex = hex.EncodeToString(priv)

	ccid, err := PrivKeyToAddr(privKeyHex, "con")
	if err != nil {
		t.Fatalf("derive ccid: %v", err)
	}
	return ccid, privKeyHex
}

// signDocument marshals doc and produces an ecrecover-proof SignedDocument
// signed with privKeyHex.
func signDocument[T any](t *testing.T, doc Document[T], privKeyHex string) SignedDocument {
	t.Helper()

	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}

	sigBytes, err := SignBytes(docBytes, privKeyHex)
	if err != nil {
		t.Fatalf("sign document: %v", err)
	}
	signature := hex.EncodeToString(sigBytes)

	return SignedDocument{
		Document: string(docBytes),
		Proof: Proof{
			Type:      ProofTypeEcrecover,
			Signature: &signature,
		},
	}
}

// mapResolver is an in-memory DocumentResolver for tests.
type mapResolver map[string]SignedDocument

func (m mapResolver) ResolveSignedDocument(_ context.Context, uri string) (SignedDocument, error) {
	sd, ok := m[uri]
	if !ok {
		return SignedDocument{}, errors.New("not found: " + uri)
	}
	return sd, nil
}

func TestVerifyEcrecoverValid(t *testing.T) {
	ccid, priv := newTestIdentity(t)
	sd := signDocument(t, Document[testRecordValue]{
		Kind:      "record",
		Key:       "cckv://" + ccid + "/example",
		Value:     testRecordValue{Foo: "bar"},
		Author:    ccid,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, priv)

	if err := sd.Verify(context.Background(), nil); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

func TestVerifyEcrecoverTamperedDocument(t *testing.T) {
	ccid, priv := newTestIdentity(t)
	sd := signDocument(t, Document[testRecordValue]{
		Kind:      "record",
		Key:       "cckv://" + ccid + "/example",
		Value:     testRecordValue{Foo: "bar"},
		Author:    ccid,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, priv)

	tampered := strings.Replace(sd.Document, "bar", "evil", 1)
	if tampered == sd.Document {
		t.Fatal("tampering did not change the document")
	}
	sd.Document = tampered

	err := sd.Verify(context.Background(), nil)
	if err == nil {
		t.Fatal("Verify returned nil error for tampered document")
	}
	if !errors.Is(err, ErrSignatureVerificationFailed) {
		t.Fatalf("Verify returned error %v, want ErrSignatureVerificationFailed", err)
	}
}

// newSubkeyProof builds a record signed via a subkey: subkeySD is the
// owner-signed subkey enact document (Author=ownerCCID, Value.CKID=subCCID),
// and the returned SignedDocument is the record itself (Author=ownerCCID)
// signed with the subkey's private key.
func newSubkeyProof(t *testing.T, ownerCCID, subCCID, subPriv, subkeyURI string, subkeySD SignedDocument) SignedDocument {
	t.Helper()

	doc := Document[testRecordValue]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/example",
		Value:     testRecordValue{Foo: "bar"},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}

	sigBytes, err := SignBytes(docBytes, subPriv)
	if err != nil {
		t.Fatalf("sign document with subkey: %v", err)
	}
	signature := hex.EncodeToString(sigBytes)

	return SignedDocument{
		Document: string(docBytes),
		Proof: Proof{
			Type:      ProofTypeSubkey,
			Signature: &signature,
			Key:       &subkeyURI,
		},
	}
}

func TestVerifySubkeyValid(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	subCCID, subPriv := newTestIdentity(t)

	subkeyURI := "cckv://" + ownerCCID + "/subkeys/1"
	subkeySD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       subkeyURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	sd := newSubkeyProof(t, ownerCCID, subCCID, subPriv, subkeyURI, subkeySD)
	resolver := mapResolver{subkeyURI: subkeySD}

	if err := sd.Verify(context.Background(), resolver); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

// CIP-13: only a subkey enact document may authorize a subkey. An owner-signed
// document of any other schema carrying a value.ckid must not pass as a subkey
// authorization.
func TestVerifySubkeyRejectsNonEnactSchema(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	subCCID, subPriv := newTestIdentity(t)

	subkeyURI := "cckv://" + ownerCCID + "/subkeys/1"
	subkeySD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       subkeyURI,
		Schema:    "https://schema.example/some-other-document.json",
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	sd := newSubkeyProof(t, ownerCCID, subCCID, subPriv, subkeyURI, subkeySD)
	resolver := mapResolver{subkeyURI: subkeySD}

	if err := sd.Verify(context.Background(), resolver); err == nil {
		t.Fatal("Verify returned nil error for a non-enact subkey document")
	}
}

func TestVerifySubkeyRejectsAuthorMismatch(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	otherCCID, _ := newTestIdentity(t)
	subCCID, subPriv := newTestIdentity(t)

	subkeyURI := "cckv://" + ownerCCID + "/subkeys/1"
	// subkey enact document authored by a different CCID than the record it
	// is used to sign.
	subkeySD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       subkeyURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    otherCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv) // signature validity of the enact doc itself doesn't matter
	// for this test's assertion, only that Author != record's Author.

	sd := newSubkeyProof(t, ownerCCID, subCCID, subPriv, subkeyURI, subkeySD)
	resolver := mapResolver{subkeyURI: subkeySD}

	err := sd.Verify(context.Background(), resolver)
	if err == nil {
		t.Fatal("Verify returned nil error for subkey author mismatch")
	}
}

// revokedSubkeySD builds an owner-signed revoked-subkey document embedding
// enactSD (CIP-13 §4), placed at the same key as the enact document.
func revokedSubkeySD(t *testing.T, ownerCCID, ownerPriv, subkeyURI string, enactSD SignedDocument, revokedAt time.Time) SignedDocument {
	t.Helper()
	return signDocument(t, Document[SignedDocument]{
		Kind:      "record",
		Key:       subkeyURI,
		Schema:    schemas.RevokedSubkeyURL,
		Value:     SignedDocument{Document: enactSD.Document, Proof: enactSD.Proof},
		Author:    ownerCCID,
		CreatedAt: revokedAt,
	}, ownerPriv)
}

// signDocumentWithSubkey marshals doc and signs it with subPriv as a subkey
// proof pointing at keyURI.
func signDocumentWithSubkey[T any](t *testing.T, doc Document[T], subPriv, keyURI string) SignedDocument {
	t.Helper()

	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	sigBytes, err := SignBytes(docBytes, subPriv)
	if err != nil {
		t.Fatalf("sign document with subkey: %v", err)
	}
	signature := hex.EncodeToString(sigBytes)
	return SignedDocument{
		Document: string(docBytes),
		Proof: Proof{
			Type:      ProofTypeSubkey,
			Signature: &signature,
			Key:       &keyURI,
		},
	}
}

// CIP-13 §6 step 2: an enact document must be master-key (ecrecover-direct)
// signed — a subkey must not be able to enact another subkey, even though the
// chain would otherwise verify within the depth limit.
func TestVerifySubkeyRejectsSubkeySignedEnact(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	subACCID, subAPriv := newTestIdentity(t)
	subBCCID, subBPriv := newTestIdentity(t)

	// subkey A: legitimately enacted with the master key
	keyAURI := "cckv://" + ownerCCID + "/subkeys/a"
	enactASD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       keyAURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subACCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	// subkey B: enacted by subkey A instead of the master key
	keyBURI := "cckv://" + ownerCCID + "/subkeys/b"
	enactBSD := signDocumentWithSubkey(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       keyBURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subBCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2025, 12, 2, 0, 0, 0, 0, time.UTC),
	}, subAPriv, keyAURI)

	sd := newSubkeyProof(t, ownerCCID, subBCCID, subBPriv, keyBURI, enactBSD)
	resolver := mapResolver{keyAURI: enactASD, keyBURI: enactBSD}

	err := sd.Verify(context.Background(), resolver)
	if err == nil {
		t.Fatal("Verify returned nil error for a subkey-signed enact document")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("Verify returned error %q, want a proof-type-not-allowed error", err.Error())
	}
}

// CIP-13 §6 step 3: the enact document embedded in a revoked-subkey document
// must also be master-key signed.
func TestVerifySubkeyRevokedRejectsSubkeySignedEmbeddedEnact(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	subACCID, subAPriv := newTestIdentity(t)
	subBCCID, subBPriv := newTestIdentity(t)

	keyAURI := "cckv://" + ownerCCID + "/subkeys/a"
	enactASD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       keyAURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subACCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	keyBURI := "cckv://" + ownerCCID + "/subkeys/b"
	enactBSD := signDocumentWithSubkey(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       keyBURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subBCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2025, 12, 2, 0, 0, 0, 0, time.UTC),
	}, subAPriv, keyAURI)

	// record signed by subkey B inside what would be its validity period
	sd := newSubkeyProof(t, ownerCCID, subBCCID, subBPriv, keyBURI, enactBSD)

	// owner-signed revocation embedding the subkey-signed enact
	revokedSD := revokedSubkeySD(t, ownerCCID, ownerPriv, keyBURI, enactBSD,
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	resolver := mapResolver{keyAURI: enactASD, keyBURI: revokedSD}

	if err := sd.Verify(context.Background(), resolver); err == nil {
		t.Fatal("Verify returned nil error for a revoked-subkey embedding a subkey-signed enact")
	}
}

func TestVerifyWithProofTypes(t *testing.T) {
	ccid, priv := newTestIdentity(t)
	sd := signDocument(t, Document[testRecordValue]{
		Kind:      "record",
		Key:       "cckv://" + ccid + "/example",
		Value:     testRecordValue{Foo: "bar"},
		Author:    ccid,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, priv)

	if err := sd.VerifyWithProofTypes(context.Background(), nil, []string{ProofTypeEcrecover}); err != nil {
		t.Fatalf("VerifyWithProofTypes returned error for an allowed proof type: %v", err)
	}
	if err := sd.VerifyWithProofTypes(context.Background(), nil, []string{ProofTypeSubkey}); err == nil {
		t.Fatal("VerifyWithProofTypes returned nil error for a disallowed proof type")
	}
	// nil = no restriction
	if err := sd.VerifyWithProofTypes(context.Background(), nil, nil); err != nil {
		t.Fatalf("VerifyWithProofTypes returned error with nil restriction: %v", err)
	}
}

// CIP-13 §4.1: after revocation, a signature made inside the subkey's validity
// period (enact createdAt .. revocation createdAt) must still verify.
func TestVerifySubkeyRevokedSignatureWithinValidityPeriod(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	subCCID, subPriv := newTestIdentity(t)

	subkeyURI := "cckv://" + ownerCCID + "/subkeys/1"
	enactSD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       subkeyURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	// the record itself is created 2026-01-01, inside the validity period
	sd := newSubkeyProof(t, ownerCCID, subCCID, subPriv, subkeyURI, enactSD)

	revokedSD := revokedSubkeySD(t, ownerCCID, ownerPriv, subkeyURI, enactSD,
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	resolver := mapResolver{subkeyURI: revokedSD}

	if err := sd.Verify(context.Background(), resolver); err != nil {
		t.Fatalf("Verify returned error for a signature within the validity period: %v", err)
	}
}

// CIP-13 §4.1: a signature created after the revocation must fail.
func TestVerifySubkeyRevokedSignatureAfterRevocation(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	subCCID, subPriv := newTestIdentity(t)

	subkeyURI := "cckv://" + ownerCCID + "/subkeys/1"
	enactSD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       subkeyURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	// record createdAt 2026-01-01 > revocation 2025-12-15
	sd := newSubkeyProof(t, ownerCCID, subCCID, subPriv, subkeyURI, enactSD)

	revokedSD := revokedSubkeySD(t, ownerCCID, ownerPriv, subkeyURI, enactSD,
		time.Date(2025, 12, 15, 0, 0, 0, 0, time.UTC))
	resolver := mapResolver{subkeyURI: revokedSD}

	if err := sd.Verify(context.Background(), resolver); err == nil {
		t.Fatal("Verify returned nil error for a signature created after revocation")
	}
}

// CIP-13 §6 step 5: a signature that predates the enact document is outside
// the validity period even while the subkey is active.
func TestVerifySubkeySignaturePredatingEnactRejected(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	subCCID, subPriv := newTestIdentity(t)

	subkeyURI := "cckv://" + ownerCCID + "/subkeys/1"
	enactSD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       subkeyURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), // after the record's 2026-01-01
	}, ownerPriv)

	sd := newSubkeyProof(t, ownerCCID, subCCID, subPriv, subkeyURI, enactSD)
	resolver := mapResolver{subkeyURI: enactSD}

	if err := sd.Verify(context.Background(), resolver); err == nil {
		t.Fatal("Verify returned nil error for a signature predating the enact document")
	}
}

// A revoked-subkey document embedding something that is not a valid enact
// document (wrong schema, or tampered) must not authorize anything.
func TestVerifySubkeyRevokedRejectsBadEmbeddedEnact(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	subCCID, subPriv := newTestIdentity(t)

	subkeyURI := "cckv://" + ownerCCID + "/subkeys/1"
	enactSD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       subkeyURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)
	sd := newSubkeyProof(t, ownerCCID, subCCID, subPriv, subkeyURI, enactSD)
	revokedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	// embedded enact has a non-enact schema
	wrongSchemaSD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       subkeyURI,
		Schema:    "https://schema.example/some-other-document.json",
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)
	revokedSD := revokedSubkeySD(t, ownerCCID, ownerPriv, subkeyURI, wrongSchemaSD, revokedAt)
	if err := sd.Verify(context.Background(), mapResolver{subkeyURI: revokedSD}); err == nil {
		t.Fatal("Verify returned nil error for a revoked-subkey embedding a non-enact document")
	}

	// embedded enact is tampered so its own signature no longer verifies
	tamperedSD := enactSD
	tamperedSD.Document = strings.Replace(enactSD.Document, `"kind":"record"`, `"kind":"recorD"`, 1)
	if tamperedSD.Document == enactSD.Document {
		t.Fatal("tampering did not change the embedded enact document")
	}
	revokedSD = revokedSubkeySD(t, ownerCCID, ownerPriv, subkeyURI, tamperedSD, revokedAt)
	if err := sd.Verify(context.Background(), mapResolver{subkeyURI: revokedSD}); err == nil {
		t.Fatal("Verify returned nil error for a revoked-subkey embedding a tampered enact document")
	}
}

func TestVerifyDocumentReferenceValid(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)

	targetURI := "cckv://" + ownerCCID + "/target"
	targetSD := signDocument(t, Document[testRecordValue]{
		Kind:      "record",
		Key:       targetURI,
		Value:     testRecordValue{Foo: "bar"},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/ref",
		Value:     schemas.Reference{Href: targetURI},
		Author:    ownerCCID,
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document: string(refDocBytes),
		Proof:    Proof{Type: ProofTypeDocumentReference, Href: &targetURI},
	}

	resolver := mapResolver{targetURI: targetSD}
	if err := sd.Verify(context.Background(), resolver); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

func TestVerifyDocumentReferenceRejectsAuthorMismatch(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	otherCCID, _ := newTestIdentity(t)

	targetURI := "cckv://" + ownerCCID + "/target"
	targetSD := signDocument(t, Document[testRecordValue]{
		Kind:      "record",
		Key:       targetURI,
		Value:     testRecordValue{Foo: "bar"},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + otherCCID + "/ref",
		Value:     schemas.Reference{Href: targetURI},
		Author:    otherCCID, // does not match targetSD's author
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document: string(refDocBytes),
		Proof:    Proof{Type: ProofTypeDocumentReference, Href: &targetURI},
	}

	resolver := mapResolver{targetURI: targetSD}
	err = sd.Verify(context.Background(), resolver)
	if err == nil {
		t.Fatal("Verify returned nil error for document-reference author mismatch")
	}
}

func TestVerifyPrefersInlineReferencesOverResolver(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)

	targetURI := "cckv://" + ownerCCID + "/target"
	targetSD := signDocument(t, Document[testRecordValue]{
		Kind:      "record",
		Key:       targetURI,
		Value:     testRecordValue{Foo: "bar"},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/ref",
		Value:     schemas.Reference{Href: targetURI},
		Author:    ownerCCID,
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document:   string(refDocBytes),
		Proof:      Proof{Type: ProofTypeDocumentReference, Href: &targetURI},
		References: map[string]SignedDocument{targetURI: targetSD},
	}

	// No resolver passed at all: inline References must be enough.
	if err := sd.Verify(context.Background(), nil); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

// Subkey enact documents must always come from the resolver: an inlined
// (submitter-supplied) copy must not be trusted, otherwise a revoked
// (deleted) subkey enact document could be replayed forever.
func TestVerifySubkeyIgnoresInlineReference(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	subCCID, subPriv := newTestIdentity(t)

	subkeyURI := "cckv://" + ownerCCID + "/subkeys/1"
	subkeySD := signDocument(t, Document[schemas.Subkey]{
		Kind:      "record",
		Key:       subkeyURI,
		Schema:    schemas.SubkeyURL,
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	sd := newSubkeyProof(t, ownerCCID, subCCID, subPriv, subkeyURI, subkeySD)
	sd.References = map[string]SignedDocument{subkeyURI: subkeySD}

	// inline copy present but no resolver: must fail
	if err := sd.Verify(context.Background(), nil); err == nil {
		t.Fatal("Verify returned nil error for subkey proof with no resolver")
	}

	// resolver that doesn't know the enact doc (revoked/deleted): must fail
	if err := sd.Verify(context.Background(), mapResolver{}); err == nil {
		t.Fatal("Verify returned nil error for subkey proof with an empty resolver")
	}

	// resolver serving the authoritative enact doc: must succeed
	if err := sd.Verify(context.Background(), mapResolver{subkeyURI: subkeySD}); err != nil {
		t.Fatalf("Verify returned error for subkey proof with a working resolver: %v", err)
	}
}

// A trusted inline reference still has to verify on its own: a tampered
// inline copy fails its signature check even though no resolver is consulted.
func TestVerifyDocumentReferenceRejectsTamperedInlineCopy(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)

	targetURI := "cckv://" + ownerCCID + "/target"
	targetSD := signDocument(t, Document[testRecordValue]{
		Kind:      "record",
		Key:       targetURI,
		Value:     testRecordValue{Foo: "bar"},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)
	targetSD.Document = strings.Replace(targetSD.Document, "bar", "evil", 1)

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/ref",
		Value:     schemas.Reference{Href: targetURI},
		Author:    ownerCCID,
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document:   string(refDocBytes),
		Proof:      Proof{Type: ProofTypeDocumentReference, Href: &targetURI},
		References: map[string]SignedDocument{targetURI: targetSD},
	}

	if err := sd.Verify(context.Background(), nil); err == nil {
		t.Fatal("Verify returned nil error for a tampered inline reference")
	}
}

// A none-proof inline target is always rejected by Verify — none proofs carry
// no authorship. The migration/import path (where distribution records
// reference none-proof base records) relies on system service accounts
// skipping Verify entirely at the commit layer, not on Verify accepting none.
func TestVerifyDocumentReferenceInlineNoneProofTargetRejected(t *testing.T) {
	ownerCCID, _ := newTestIdentity(t)

	targetURI := "cckv://" + ownerCCID + "/target"
	targetDoc := Document[testRecordValue]{
		Kind:      "record",
		Key:       targetURI,
		Value:     testRecordValue{Foo: "bar"},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	targetDocBytes, err := json.Marshal(targetDoc)
	if err != nil {
		t.Fatalf("marshal target document: %v", err)
	}
	targetSD := SignedDocument{
		Document: string(targetDocBytes),
		Proof:    Proof{Type: ProofTypeNone},
	}

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/ref",
		Value:     schemas.Reference{Href: targetURI},
		Author:    ownerCCID,
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document:   string(refDocBytes),
		Proof:      Proof{Type: ProofTypeDocumentReference, Href: &targetURI},
		References: map[string]SignedDocument{targetURI: targetSD},
	}

	if err := sd.Verify(context.Background(), nil); err == nil {
		t.Fatal("Verify returned nil error for a none-proof inline target")
	}
}

func TestVerifyNoneProofAlwaysRejected(t *testing.T) {
	ccid, _ := newTestIdentity(t)
	doc := Document[testRecordValue]{
		Kind:      "record",
		Key:       "cckv://" + ccid + "/example",
		Value:     testRecordValue{Foo: "bar"},
		Author:    ccid,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	sd := SignedDocument{
		Document: string(docBytes),
		Proof:    Proof{Type: ProofTypeNone},
	}

	if err := sd.Verify(context.Background(), nil); !errors.Is(err, ErrNoneProofNotAllowed) {
		t.Fatalf("Verify returned %v for none proof, want ErrNoneProofNotAllowed", err)
	}
}

func TestVerifyRejectsProofChainTooDeep(t *testing.T) {
	uri := "cckv://con1loop000000000000000000000000000/doc"

	// A self-referencing Reference document: satisfies the document-reference
	// proof's schema/href requirements at every level, so depth exhaustion
	// (not those checks) is what ultimately fails verification.
	doc := Document[schemas.Reference]{
		Kind:   "record",
		Key:    uri,
		Value:  schemas.Reference{Href: uri},
		Author: "con1loop000000000000000000000000000",
		Schema: schemas.ReferenceURL,
	}
	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}

	loopSD := SignedDocument{
		Document: string(docBytes),
		Proof:    Proof{Type: ProofTypeDocumentReference, Href: &uri},
	}
	resolver := mapResolver{uri: loopSD}

	err = loopSD.Verify(context.Background(), resolver)
	if err == nil {
		t.Fatal("Verify returned nil error for a self-referencing proof chain")
	}
	if !strings.Contains(err.Error(), "too deep") {
		t.Fatalf("Verify returned error %q, want it to mention the proof chain being too deep", err.Error())
	}
}

func TestVerifyUnsupportedProofType(t *testing.T) {
	ccid, _ := newTestIdentity(t)
	doc := Document[testRecordValue]{
		Kind:   "record",
		Key:    "cckv://" + ccid + "/example",
		Author: ccid,
	}
	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	sd := SignedDocument{
		Document: string(docBytes),
		Proof:    Proof{Type: "something-unknown"},
	}

	if err := sd.Verify(context.Background(), nil); err == nil {
		t.Fatal("Verify returned nil error for unsupported proof type")
	}
}

// The href must be bound to the target's identity: a correctly-signed but
// *different* document by the same author inlined under References[href] must
// not satisfy the proof.
func TestVerifyDocumentReferenceRejectsSameAuthorOtherDocument(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)

	targetURI := "cckv://" + ownerCCID + "/target"
	otherSD := signDocument(t, Document[testRecordValue]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/other",
		Value:     testRecordValue{Foo: "legit but unrelated"},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/ref",
		Value:     schemas.Reference{Href: targetURI},
		Author:    ownerCCID,
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document:   string(refDocBytes),
		Proof:      Proof{Type: ProofTypeDocumentReference, Href: &targetURI},
		References: map[string]SignedDocument{targetURI: otherSD},
	}

	if err := sd.Verify(context.Background(), nil); err == nil {
		t.Fatal("Verify returned nil error for a same-author document smuggled in under a different href")
	}
}

// A distribution Reference as the commit path produces it for associations:
// keyless target with an associate, href = ccfs://<associate owner>/concrnt/<cdid>.
func TestVerifyDocumentReferenceCCFSValid(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)

	associate := "cckv://" + ownerCCID
	targetSD := signDocument(t, Document[testRecordValue]{
		Kind:      "association",
		Value:     testRecordValue{Foo: "bar"},
		Author:    ownerCCID,
		Associate: &associate,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	hash := GetHash([]byte(targetSD.Document))
	var hash10 [10]byte
	copy(hash10[:], hash[:10])
	documentID := cdid.New(hash10, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).String()
	href := "ccfs://" + ownerCCID + "/concrnt/" + documentID

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/ref",
		Value:     schemas.Reference{Href: href},
		Author:    ownerCCID,
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document:   string(refDocBytes),
		Proof:      Proof{Type: ProofTypeDocumentReference, Href: &href},
		References: map[string]SignedDocument{href: targetSD},
	}

	if err := sd.Verify(context.Background(), nil); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

func TestVerifyDocumentReferenceCCFSRejectsCDIDMismatch(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)

	associate := "cckv://" + ownerCCID
	targetSD := signDocument(t, Document[testRecordValue]{
		Kind:      "association",
		Value:     testRecordValue{Foo: "bar"},
		Author:    ownerCCID,
		Associate: &associate,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	otherSD := signDocument(t, Document[testRecordValue]{
		Kind:      "association",
		Value:     testRecordValue{Foo: "different content"},
		Author:    ownerCCID,
		Associate: &associate,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	// href identifies otherSD, but targetSD is what gets inlined under it
	hash := GetHash([]byte(otherSD.Document))
	var hash10 [10]byte
	copy(hash10[:], hash[:10])
	documentID := cdid.New(hash10, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).String()
	href := "ccfs://" + ownerCCID + "/concrnt/" + documentID

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/ref",
		Value:     schemas.Reference{Href: href},
		Author:    ownerCCID,
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document:   string(refDocBytes),
		Proof:      Proof{Type: ProofTypeDocumentReference, Href: &href},
		References: map[string]SignedDocument{href: targetSD},
	}

	if err := sd.Verify(context.Background(), nil); err == nil {
		t.Fatal("Verify returned nil error for a ccfs href whose cdid does not match the inlined document")
	}
}

func TestVerifyDocumentReferenceCCFSRejectsOwnerMismatch(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)
	otherCCID, _ := newTestIdentity(t)

	associate := "cckv://" + ownerCCID
	targetSD := signDocument(t, Document[testRecordValue]{
		Kind:      "association",
		Value:     testRecordValue{Foo: "bar"},
		Author:    ownerCCID,
		Associate: &associate,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	// correct cdid, but the href claims a different owner than the associate's
	hash := GetHash([]byte(targetSD.Document))
	var hash10 [10]byte
	copy(hash10[:], hash[:10])
	documentID := cdid.New(hash10, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).String()
	href := "ccfs://" + otherCCID + "/concrnt/" + documentID

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/ref",
		Value:     schemas.Reference{Href: href},
		Author:    ownerCCID,
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document:   string(refDocBytes),
		Proof:      Proof{Type: ProofTypeDocumentReference, Href: &href},
		References: map[string]SignedDocument{href: targetSD},
	}

	if err := sd.Verify(context.Background(), nil); err == nil {
		t.Fatal("Verify returned nil error for a ccfs href whose owner does not match the inlined document")
	}
}

// A keyless cckv href is an entity reference: the target must be the owner's
// own (self-authored) document.
func TestVerifyDocumentReferenceEntityHrefValid(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)

	href := "cckv://" + ownerCCID
	targetSD := signDocument(t, Document[testRecordValue]{
		Kind:      "entity",
		Value:     testRecordValue{Foo: "profile"},
		Author:    ownerCCID,
		Schema:    schemas.EntityURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/ref",
		Value:     schemas.Reference{Href: href},
		Author:    ownerCCID,
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document:   string(refDocBytes),
		Proof:      Proof{Type: ProofTypeDocumentReference, Href: &href},
		References: map[string]SignedDocument{href: targetSD},
	}

	if err := sd.Verify(context.Background(), nil); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

// A same-author document that is not an entity document must not stand in for
// an entity reference: the author-match alone would let any self-authored
// record pass under a keyless href.
func TestVerifyDocumentReferenceEntityHrefRejectsNonEntityTarget(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)

	href := "cckv://" + ownerCCID
	targetSD := signDocument(t, Document[testRecordValue]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/some-record",
		Value:     testRecordValue{Foo: "not a profile"},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	refDoc := Document[schemas.Reference]{
		Kind:      "record",
		Key:       "cckv://" + ownerCCID + "/ref",
		Value:     schemas.Reference{Href: href},
		Author:    ownerCCID,
		Schema:    schemas.ReferenceURL,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	refDocBytes, err := json.Marshal(refDoc)
	if err != nil {
		t.Fatalf("marshal reference document: %v", err)
	}
	sd := SignedDocument{
		Document:   string(refDocBytes),
		Proof:      Proof{Type: ProofTypeDocumentReference, Href: &href},
		References: map[string]SignedDocument{href: targetSD},
	}

	if err := sd.Verify(context.Background(), nil); err == nil {
		t.Fatal("Verify returned nil error for an entity href backed by a non-entity document")
	}
}

// http(s) and ccfs blob hrefs cannot be bound to a document identity, so a
// document-reference proof over them must always fail — such references need
// a direct or subkey signature.
func TestVerifyDocumentReferenceRejectsUnbindableHrefs(t *testing.T) {
	ownerCCID, ownerPriv := newTestIdentity(t)

	for _, href := range []string{
		"https://example.com/some-page",
		"ccfs://" + ownerCCID + "/blob/" + strings.Repeat("ab", 32),
	} {
		targetSD := signDocument(t, Document[testRecordValue]{
			Kind:      "record",
			Key:       "cckv://" + ownerCCID + "/target",
			Value:     testRecordValue{Foo: "bar"},
			Author:    ownerCCID,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}, ownerPriv)

		refDoc := Document[schemas.Reference]{
			Kind:      "record",
			Key:       "cckv://" + ownerCCID + "/ref",
			Value:     schemas.Reference{Href: href},
			Author:    ownerCCID,
			Schema:    schemas.ReferenceURL,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		refDocBytes, err := json.Marshal(refDoc)
		if err != nil {
			t.Fatalf("marshal reference document: %v", err)
		}
		href := href
		sd := SignedDocument{
			Document:   string(refDocBytes),
			Proof:      Proof{Type: ProofTypeDocumentReference, Href: &href},
			References: map[string]SignedDocument{href: targetSD},
		}

		if err := sd.Verify(context.Background(), nil); err == nil {
			t.Fatalf("Verify returned nil error for unbindable href %s", href)
		}
	}
}
