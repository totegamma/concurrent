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

	if err := sd.Verify(context.Background(), nil, nil); err != nil {
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

	err := sd.Verify(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("Verify returned nil error for tampered document")
	}
	if !errors.Is(err, ErrSignatureVerificationFailed) {
		t.Fatalf("Verify returned error %v, want ErrSignatureVerificationFailed", err)
	}
}

// newSubkeyProof builds a record signed via a subkey: subkeySD is the
// owner-signed subkey-enact document (Author=ownerCCID, Value.CKID=subCCID),
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
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    ownerCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv)

	sd := newSubkeyProof(t, ownerCCID, subCCID, subPriv, subkeyURI, subkeySD)
	resolver := mapResolver{subkeyURI: subkeySD}

	if err := sd.Verify(context.Background(), resolver, nil); err != nil {
		t.Fatalf("Verify returned error: %v", err)
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
		Value:     schemas.Subkey{CKID: subCCID},
		Author:    otherCCID,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, ownerPriv) // signature validity of the enact doc itself doesn't matter
	// for this test's assertion, only that Author != record's Author.

	sd := newSubkeyProof(t, ownerCCID, subCCID, subPriv, subkeyURI, subkeySD)
	resolver := mapResolver{subkeyURI: subkeySD}

	err := sd.Verify(context.Background(), resolver, nil)
	if err == nil {
		t.Fatal("Verify returned nil error for subkey author mismatch")
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
	if err := sd.Verify(context.Background(), resolver, nil); err != nil {
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
	err = sd.Verify(context.Background(), resolver, nil)
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
	if err := sd.Verify(context.Background(), nil, nil); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

func TestVerifyNoneProofRejectedByDefault(t *testing.T) {
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

	if err := sd.Verify(context.Background(), nil, nil); err == nil {
		t.Fatal("Verify returned nil error for none proof without AllowNoneProof")
	}
	if err := sd.Verify(context.Background(), nil, &VerifyOpts{AllowNoneProof: false}); err == nil {
		t.Fatal("Verify returned nil error for none proof with AllowNoneProof=false")
	}
}

func TestVerifyNoneProofAllowedWhenOptedIn(t *testing.T) {
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

	if err := sd.Verify(context.Background(), nil, &VerifyOpts{AllowNoneProof: true}); err != nil {
		t.Fatalf("Verify returned error for allowed none proof: %v", err)
	}
}

func TestVerifyRejectsProofChainTooDeep(t *testing.T) {
	uri := "cckv://con1loop000000000000000000000000000/doc"

	doc := Document[testRecordValue]{
		Kind:   "record",
		Key:    uri,
		Value:  testRecordValue{Foo: "bar"},
		Author: "con1loop000000000000000000000000000",
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

	err = loopSD.Verify(context.Background(), resolver, nil)
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

	if err := sd.Verify(context.Background(), nil, nil); err == nil {
		t.Fatal("Verify returned nil error for unsupported proof type")
	}
}
