package concrnt

import (
	"encoding/json"
	"errors"
	"fmt"
)

// DeriveAckMirror derives the canonical acked/unacked mirror document from an
// ack/unack document (CIP-10): the mirror is the original document with only
// its kind flipped (ack → acked, unack → unacked), re-marshalled through the
// canonical Document field set. The derivation is deterministic, so the
// mirror's CDID is stable across retries and repair backfills, and a verifier
// can recompute the expected mirror from the proof-embedded original and
// require byte equality — which covers kind correspondence and every other
// field in one comparison.
func DeriveAckMirror(ackDocument string) (string, error) {
	var doc Document[json.RawMessage]
	if err := json.Unmarshal([]byte(ackDocument), &doc); err != nil {
		return "", errors.Join(errors.New("failed to decode ack document for mirror derivation"), err)
	}

	switch doc.Kind {
	case "ack":
		doc.Kind = "acked"
	case "unack":
		doc.Kind = "unacked"
	default:
		return "", fmt.Errorf("ack mirror can only be derived from ack/unack documents, got kind %q", doc.Kind)
	}

	mirror, err := json.Marshal(doc)
	if err != nil {
		return "", errors.Join(errors.New("failed to encode ack mirror document"), err)
	}
	return string(mirror), nil
}
