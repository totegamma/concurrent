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

// Out-of-range times must clamp to the 48-bit millisecond range instead of
// wrapping: a zero time.Time (negative unix time, e.g. an unset createdAt)
// must sort before every real time, not into the far future.
func TestTimeCDIDClampsOutOfRangeTimes(t *testing.T) {
	data := [10]byte{0xff, 1, 2, 3, 4, 5, 6, 7, 8, 0xff}
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	zero := New(data, time.Time{})
	if got := zero.GetTime(); !got.Equal(time.UnixMilli(0)) {
		t.Fatalf("zero time clamped to %v, want unix epoch", got)
	}
	if zero.String() >= New(data, now).String() {
		t.Fatalf("zero-time CDID %q does not sort before now %q", zero.String(), New(data, now).String())
	}

	farFuture := New(data, time.Date(12000, 1, 1, 0, 0, 0, 0, time.UTC))
	if got := farFuture.GetTime(); !got.Equal(time.UnixMilli(1<<48 - 1)) {
		t.Fatalf("far-future time clamped to %v, want 48-bit max", got)
	}
	if farFuture.String() <= New(data, now).String() {
		t.Fatalf("far-future CDID %q does not sort after now %q", farFuture.String(), New(data, now).String())
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
