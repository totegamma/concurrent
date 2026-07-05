package concrnt

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/concrnt/concrnt/schemas"
)

// DocumentResolver fetches a signed document by its cckv/ccfs URI, so that
// Verify can follow proof references (subkey / document-reference) that
// aren't already inlined in SignedDocument.References. *client.Client
// satisfies this interface structurally.
type DocumentResolver interface {
	ResolveSignedDocument(ctx context.Context, uri string) (SignedDocument, error)
}

// VerifyOpts controls SignedDocument.Verify behavior.
type VerifyOpts struct {
	// AllowNoneProof allows proof.type == "none" to verify successfully.
	// Callers are responsible for any authorization checks (e.g. restricting
	// none proofs to trusted system service accounts) before setting this.
	AllowNoneProof bool

	// IgnoreReferences makes Verify fetch referenced documents (subkey /
	// document-reference proofs) via the resolver even when a copy is
	// inlined in SignedDocument.References. Inlined copies are supplied by
	// whoever submitted the document, so trusting them would let a revoked
	// (deleted) subkey enact document be replayed forever; authoritative
	// paths such as committing must set this.
	IgnoreReferences bool
}

// ErrSignatureVerificationFailed indicates a signed document's proof did not
// verify.
var ErrSignatureVerificationFailed = errors.New("signature verification failed")

// ErrNoneProofNotAllowed indicates a none-proof document was verified without
// VerifyOpts.AllowNoneProof.
var ErrNoneProofNotAllowed = errors.New("none proof type is not allowed")

// ErrUnsupportedProofType indicates a proof type Verify doesn't know how to
// check.
var ErrUnsupportedProofType = errors.New("unsupported or unverifiable proof type")

// maxVerifyDepth bounds how many linked documents (subkey / document-reference
// proofs) Verify will follow, to protect against malicious, arbitrarily deep
// proof chains.
const maxVerifyDepth = 4

// Verify verifies a signed document's proof. For proofs that reference
// another document (subkey, document-reference), the referenced document is
// looked up (via References if present and not disabled by
// VerifyOpts.IgnoreReferences, otherwise via resolver) and recursively
// verified.
func (sd *SignedDocument) Verify(ctx context.Context, resolver DocumentResolver, opts *VerifyOpts) error {
	return sd.verify(ctx, resolver, opts, maxVerifyDepth)
}

func (sd *SignedDocument) resolve(ctx context.Context, resolver DocumentResolver, opts *VerifyOpts, uri string) (SignedDocument, error) {
	if opts == nil || !opts.IgnoreReferences {
		if ref, ok := sd.References[uri]; ok {
			return ref, nil
		}
	}
	if resolver == nil {
		return SignedDocument{}, fmt.Errorf("no resolver available to fetch referenced document %s", uri)
	}
	return resolver.ResolveSignedDocument(ctx, uri)
}

func (sd *SignedDocument) verify(ctx context.Context, resolver DocumentResolver, opts *VerifyOpts, depth int) error {
	if depth <= 0 {
		return errors.New("proof chain is too deep")
	}

	var doc Document[any]
	err := json.Unmarshal([]byte(sd.Document), &doc)
	if err != nil {
		return errors.Join(errors.New("failed to decode document for signature verification"), err)
	}

	switch sd.Proof.Type {
	case ProofTypeEcrecover:
		if sd.Proof.Signature == nil {
			return errors.New("signature is required for ecrecover proof")
		}
		signatureBytes, err := hex.DecodeString(*sd.Proof.Signature)
		if err != nil {
			return errors.Join(errors.New("invalid signature format"), err)
		}
		err = VerifySignature([]byte(sd.Document), signatureBytes, doc.Author)
		if err != nil {
			return errors.Join(ErrSignatureVerificationFailed, err)
		}
		return nil

	case ProofTypeSubkey:
		if sd.Proof.Signature == nil {
			return errors.New("signature is required for subkey proof")
		}
		if sd.Proof.Key == nil {
			return errors.New("key is required for subkey proof")
		}

		subKeySD, err := sd.resolve(ctx, resolver, opts, *sd.Proof.Key)
		if err != nil {
			return errors.Join(fmt.Errorf("failed to fetch subkey document %s", *sd.Proof.Key), err)
		}

		err = subKeySD.verify(ctx, resolver, opts, depth-1)
		if err != nil {
			return errors.Join(errors.New("subkey document failed verification"), err)
		}

		var subKeyDoc Document[schemas.Subkey]
		err = json.Unmarshal([]byte(subKeySD.Document), &subKeyDoc)
		if err != nil {
			return errors.Join(errors.New("failed to decode subkey document"), err)
		}

		if subKeyDoc.Author != doc.Author {
			return errors.New("subkey document author does not match signed document author")
		}

		signatureBytes, err := hex.DecodeString(*sd.Proof.Signature)
		if err != nil {
			return errors.Join(errors.New("invalid signature format"), err)
		}

		err = VerifySignature([]byte(sd.Document), signatureBytes, subKeyDoc.Value.CKID)
		if err != nil {
			return errors.Join(ErrSignatureVerificationFailed, err)
		}
		return nil

	case ProofTypeDocumentReference:
		// CIP-4/CIP-6: a document-reference proof is only valid on the
		// auto-generated Reference document (schema reference.json) whose
		// href points back at the memberOf-referenced document.
		if sd.Proof.Href == nil {
			return errors.New("href is required for document-reference proof")
		}
		if doc.Schema != schemas.ReferenceURL {
			return errors.New("document-reference proof is only allowed for reference documents")
		}

		var refDoc Document[schemas.Reference]
		err = json.Unmarshal([]byte(sd.Document), &refDoc)
		if err != nil {
			return errors.Join(errors.New("invalid reference document"), err)
		}
		if refDoc.Value.Href != *sd.Proof.Href {
			return errors.New("proof href does not match reference document href")
		}

		targetSD, err := sd.resolve(ctx, resolver, opts, *sd.Proof.Href)
		if err != nil {
			return errors.Join(fmt.Errorf("failed to fetch referenced document %s", *sd.Proof.Href), err)
		}

		err = targetSD.verify(ctx, resolver, opts, depth-1)
		if err != nil {
			return errors.Join(errors.New("referenced document failed verification"), err)
		}

		var targetDoc Document[any]
		err = json.Unmarshal([]byte(targetSD.Document), &targetDoc)
		if err != nil {
			return errors.Join(errors.New("failed to decode referenced document"), err)
		}

		if targetDoc.Author != doc.Author {
			return errors.New("referenced document author does not match signed document author")
		}
		return nil

	case ProofTypeNone:
		if opts == nil || !opts.AllowNoneProof {
			return ErrNoneProofNotAllowed
		}
		return nil

	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedProofType, sd.Proof.Type)
	}
}
