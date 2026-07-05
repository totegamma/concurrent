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
}

// ErrSignatureVerificationFailed indicates a signed document's proof did not
// verify.
var ErrSignatureVerificationFailed = errors.New("signature verification failed")

// maxVerifyDepth bounds how many linked documents (subkey / document-reference
// proofs) Verify will follow, to protect against malicious, arbitrarily deep
// proof chains.
const maxVerifyDepth = 4

// Verify verifies a signed document's proof. For proofs that reference
// another document (subkey, document-reference), the referenced document is
// looked up (via References if present, otherwise via resolver) and
// recursively verified.
func (sd *SignedDocument) Verify(ctx context.Context, resolver DocumentResolver, opts *VerifyOpts) error {
	return sd.verify(ctx, resolver, opts, maxVerifyDepth)
}

func (sd *SignedDocument) resolve(ctx context.Context, resolver DocumentResolver, uri string) (SignedDocument, error) {
	if ref, ok := sd.References[uri]; ok {
		return ref, nil
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

		subKeySD, err := sd.resolve(ctx, resolver, *sd.Proof.Key)
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
		if sd.Proof.Href == nil {
			return errors.New("href is required for document-reference proof")
		}

		targetSD, err := sd.resolve(ctx, resolver, *sd.Proof.Href)
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
			return errors.New("none proof type is not allowed")
		}
		return nil

	default:
		return fmt.Errorf("unsupported or unverifiable proof type: %s", sd.Proof.Type)
	}
}
