package cdid

import (
	"testing"
	"time"
)

// IsCDIDChar must accept exactly the characters of the encoding alphabet:
// 0-9 a-z except i, l, o, and x ('x' is reserved as the hash-CDID prefix).
func TestIsCDIDCharMatchesEncodingAlphabet(t *testing.T) {
	inAlphabet := make(map[byte]bool)
	for i := range len(encoding) {
		inAlphabet[encoding[i]] = true
	}

	for c := byte(0); ; c++ {
		if IsCDIDChar(c) != inAlphabet[c] {
			t.Errorf("IsCDIDChar(%q) = %v, want %v", c, IsCDIDChar(c), inAlphabet[c])
		}
		if c == 255 {
			break
		}
	}
}

func TestTimeCDIDRoundTrip(t *testing.T) {
	data := [10]byte{0xff, 1, 2, 3, 4, 5, 6, 7, 8, 0xff}
	at := time.Date(2026, 7, 20, 12, 34, 56, 789000000, time.UTC)
	c := New(data, at)

	s := c.String()
	if IsHashCDID(s) || !IsTimeCDID(s) {
		t.Fatalf("time CDID %q misclassified", s)
	}
	for i := range len(s) {
		if !IsCDIDChar(s[i]) {
			t.Fatalf("time CDID %q contains invalid char %q", s, s[i])
		}
	}

	parsed, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q) returned error: %v", s, err)
	}
	if parsed.String() != s {
		t.Fatalf("round trip mismatch: %q != %q", parsed.String(), s)
	}
	if !parsed.GetTime().Equal(at) {
		t.Fatalf("time mismatch: %v != %v", parsed.GetTime(), at)
	}
}

func TestHashCDIDRoundTrip(t *testing.T) {
	c := MakeHash([]byte("hello concrnt"))

	s := c.String()
	if !IsHashCDID(s) || IsTimeCDID(s) {
		t.Fatalf("hash CDID %q misclassified", s)
	}
	// the body after the 'x' prefix must be alphabet-only
	for i := 1; i < len(s); i++ {
		if !IsCDIDChar(s[i]) {
			t.Fatalf("hash CDID %q contains invalid char %q", s, s[i])
		}
	}

	parsed, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q) returned error: %v", s, err)
	}
	if parsed.String() != s {
		t.Fatalf("round trip mismatch: %q != %q", parsed.String(), s)
	}
}
