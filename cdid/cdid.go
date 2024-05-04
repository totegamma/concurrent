package cdid

import (
	"crypto/rand"
	"encoding/base32"
	"time"
)

var (
	encoding = "0123456789abcdefghjkmnpqrstvwxyz"
	encoder  = base32.NewEncoding(encoding).WithPadding(base32.NoPadding)
	decoder  = base32.NewEncoding(encoding).WithPadding(base32.NoPadding)
)

type CDID struct {
	data [10]byte
	time [6]byte
}

func New(data [10]byte, t time.Time) CDID {
	c := CDID{data: data}
	c.SetTime(t)
	return c
}

func NewFromBytes(b []byte) CDID {
	data := [10]byte{}
	copy(data[:], b[:10])
	return NewWithAutoTime(data)
}

func NewWithAutoTime(data [10]byte) CDID {
	c := CDID{data: data}
	c.SetTime(time.Now())
	return c
}

func Make() CDID {
	var data [10]byte
	rand.Read(data[:])
	return NewWithAutoTime(data)
}

func (c *CDID) SetData(data [10]byte) {
	c.data = data
}

func (c *CDID) SetTime(t time.Time) {
	m := uint64(t.Unix())*1e3 + uint64(t.Nanosecond()/int(time.Millisecond))

	c.time[0] = byte(m >> 40)
	c.time[1] = byte(m >> 32)
	c.time[2] = byte(m >> 24)
	c.time[3] = byte(m >> 16)
	c.time[4] = byte(m >> 8)
	c.time[5] = byte(m)
}

func (c CDID) GetTime() time.Time {
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
	return append(c.data[:], c.time[:]...)
}

func (c CDID) String() string {
	return encoder.EncodeToString(c.Bytes())
}

func Parse(s string) (CDID, error) {
	b, err := decoder.DecodeString(s)
	if err != nil {
		return CDID{}, err
	}

	if len(b) != 16 {
		return CDID{}, nil
	}

	var c CDID
	copy(c.data[:], b[:10])
	copy(c.time[:], b[10:])
	return c, nil
}

func IsCDIDChar(c byte) bool {
	// 0-9 a-z but no i, l, o, u
	return ((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) && c != 'i' && c != 'l' && c != 'o' && c != 'u'
}

func IsSeemsCDID(str string, expectPrefix byte) bool {
	if len(str) == 27 {
		if str[0] != expectPrefix {
			return false
		}
		str = str[1:]
	}

	if len(str) != 26 {
		return false
	}

	for i := 0; i < 26; i++ {
		if !IsCDIDChar(str[i]) {
			return false
		}
	}

	return true
}
