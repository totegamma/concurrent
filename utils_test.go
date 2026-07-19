package concrnt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	testCdid = "t1b2j7ny5s8h6qdcp3wzmvxr4k9g0fae"
	testHex  = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
)

func TestParseCCURI(t *testing.T) {
	hint := "example.com"

	valid := []struct {
		uri  string
		want CCURI
	}{
		{
			uri:  "ccfs://con1owner/concrnt/" + testCdid,
			want: CCURI{Scheme: "ccfs", Owner: "con1owner", Type: CCFSTypeConcrnt, CDID: testCdid},
		},
		{
			uri:  "ccfs://con1owner/blob/" + testHex,
			want: CCURI{Scheme: "ccfs", Owner: "con1owner", Type: CCFSTypeBlob, CDID: testHex},
		},
		{
			uri:  "ccfs://con1owner@example.com/concrnt/" + testCdid,
			want: CCURI{Scheme: "ccfs", Owner: "con1owner", Type: CCFSTypeConcrnt, CDID: testCdid, Hint: &hint},
		},
		{
			uri:  "cckv://con1owner/world.concrnt.p",
			want: CCURI{Scheme: "cckv", Owner: "con1owner", Key: "world.concrnt.p"},
		},
		{
			uri:  "cckv://con1owner@example.com/world.concrnt.p",
			want: CCURI{Scheme: "cckv", Owner: "con1owner", Key: "world.concrnt.p", Hint: &hint},
		},
		{
			uri:  "cckv://con1owner",
			want: CCURI{Scheme: "cckv", Owner: "con1owner"},
		},
	}

	for _, tc := range valid {
		parsed, err := ParseCCURI(tc.uri)
		if assert.NoError(t, err, tc.uri) {
			tc.want.Raw = tc.uri
			assert.Equal(t, tc.want, *parsed, tc.uri)
			assert.Equal(t, tc.uri, parsed.String(), tc.uri)
		}
	}

	invalid := []string{
		"ccfs://con1owner",                     // owner only
		"ccfs://con1owner/concrnt",             // missing hash
		"ccfs://con1owner/blob",                // missing hash
		"ccfs://con1owner/concrnt/",            // empty hash
		"ccfs://con1owner/unknown/" + testCdid, // unknown type
		"ccfs://con1owner/concrnt/a/b",         // too many segments
		"ccfs://con1owner/blob/nothex",         // blob hash not sha256 hex
		"ccfs://con1owner/blob/" + testCdid,    // blob hash wrong length
	}

	for _, uri := range invalid {
		_, err := ParseCCURI(uri)
		assert.Error(t, err, uri)
	}
}

func TestParseCCURILegacyCCFSFallback(t *testing.T) {
	// legacy flat form ccfs://<owner>/<hash> is interpreted as a concrnt object
	parsed, err := ParseCCURI("ccfs://con1owner/" + testCdid)
	if assert.NoError(t, err) {
		assert.Equal(t, CCFSTypeConcrnt, parsed.Type)
		assert.Equal(t, testCdid, parsed.CDID)
		// String() normalizes to the new canonical form
		assert.Equal(t, "ccfs://con1owner/concrnt/"+testCdid, parsed.String())
	}

	hint := "example.com"
	parsed, err = ParseCCURI("ccfs://con1owner@example.com/" + testCdid)
	if assert.NoError(t, err) {
		assert.Equal(t, CCFSTypeConcrnt, parsed.Type)
		assert.Equal(t, testCdid, parsed.CDID)
		assert.Equal(t, &hint, parsed.Hint)
	}
}

func TestComposeCCFSURI(t *testing.T) {
	uri := ComposeCCFSURI("con1owner", CCFSTypeConcrnt, testCdid)
	assert.Equal(t, "ccfs://con1owner/concrnt/"+testCdid, uri)

	parsed, err := ParseCCURI(uri)
	if assert.NoError(t, err) {
		assert.Equal(t, uri, parsed.String())
	}

	blob := ComposeCCFSURI("con1owner", CCFSTypeBlob, testHex)
	assert.Equal(t, "ccfs://con1owner/blob/"+testHex, blob)
}

func TestCCURIString(t *testing.T) {
	hint := "example.com"

	assert.Equal(t,
		"ccfs://con1owner/concrnt/"+testCdid,
		CCURI{Scheme: "ccfs", Owner: "con1owner", Type: CCFSTypeConcrnt, CDID: testCdid}.String(),
	)
	assert.Equal(t,
		"ccfs://con1owner@example.com/blob/"+testHex,
		CCURI{Scheme: "ccfs", Owner: "con1owner", Type: CCFSTypeBlob, CDID: testHex, Hint: &hint}.String(),
	)
	assert.Equal(t,
		"cckv://con1owner",
		CCURI{Scheme: "cckv", Owner: "con1owner"}.String(),
	)
}
