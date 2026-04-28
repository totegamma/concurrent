package cdid

import (
	"encoding/base32"
	"golang.org/x/crypto/sha3"
	"time"
)

var (
	encoding = "0123456789abcdefghjkmnpqrstuvwyz"
	encoder  = base32.NewEncoding(encoding).WithPadding(base32.NoPadding)
	decoder  = base32.NewEncoding(encoding).WithPadding(base32.NoPadding)
)

type Kind int

const (
	KindTime Kind = iota
	KindHash
)

type CDID struct {
	kind Kind
	time [6]byte
	data [10]byte

	hash [15]byte
}

func New(data [10]byte, t time.Time) CDID {
	c := CDID{
		kind: KindTime,
		data: data,
	}
	c.setTime(t)
	return c
}

func NewFromHash(data [15]byte) CDID {
	c := CDID{
		kind: KindHash,
		hash: data,
	}
	return c
}

func MakeHash(b []byte) CDID {
	hash := sha3.NewLegacyKeccak256()
	hash.Write(b)
	hashBytes := hash.Sum(nil)

	var data [15]byte
	copy(data[:], hashBytes[:15])
	return NewFromHash(data)
}

func (c *CDID) setTime(t time.Time) {

	if c.kind == KindHash {
		return
	}

	m := uint64(t.Unix())*1e3 + uint64(t.Nanosecond()/int(time.Millisecond))

	c.time[0] = byte(m >> 40)
	c.time[1] = byte(m >> 32)
	c.time[2] = byte(m >> 24)
	c.time[3] = byte(m >> 16)
	c.time[4] = byte(m >> 8)
	c.time[5] = byte(m)
}

func (c CDID) GetTime() time.Time {

	if c.kind == KindHash {
		return time.Time{}
	}

	m := int64(c.time[0])<<40 |
		int64(c.time[1])<<32 |
		int64(c.time[2])<<24 |
		int64(c.time[3])<<16 |
		int64(c.time[4])<<8 |
		int64(c.time[5])

	s := int64(m / 1e3)
	n := int64((m % 1e3) * 1e6)
	return time.Unix(s, n)
}

func (c *CDID) Bytes() []byte {
	if c.kind == KindHash {
		return c.hash[:]
	} else {
		return append(c.time[:], c.data[:]...)
	}
}

func (c CDID) String() string {
	if c.kind == KindHash {
		return "x" + encoder.EncodeToString(c.hash[:])
	} else {
		return encoder.EncodeToString(c.Bytes())
	}
}

func Parse(s string) (CDID, error) {

	if s[0] == 'x' { // hash-based CDID
		b, err := decoder.DecodeString(s[1:])
		if err != nil {
			return CDID{}, err
		}

		if len(b) != 15 {
			return CDID{}, nil
		}

		var c CDID
		c.kind = KindHash
		copy(c.hash[:], b)
		return c, nil
	} else { // time-based CDID
		b, err := decoder.DecodeString(s)
		if err != nil {
			return CDID{}, err
		}

		if len(b) != 16 {
			return CDID{}, nil
		}

		var c CDID
		copy(c.time[:], b[:6])
		copy(c.data[:], b[6:])
		return c, nil
	}
}

func IsHashCDID(s string) bool {
	return len(s) > 0 && s[0] == 'x'
}

func IsTimeCDID(s string) bool {
	return len(s) > 0 && s[0] != 'x'
}

func IsCDIDChar(c byte) bool {
	// 0-9 a-z but no i, l, o, u
	return ((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) && c != 'i' && c != 'l' && c != 'o' && c != 'u'
}
