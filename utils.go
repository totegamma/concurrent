package concrnt

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/totegamma/concrnt-playground/cdid"
	"github.com/yosida95/uritemplate/v3"
)

func JsonPrint(tag string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("%s: error marshaling: %v\n", tag, err)
		return
	}
	fmt.Printf("%s: %s\n", tag, string(b))
}

type CCURI struct {
	Scheme string  `json:"schema"`
	Owner  string  `json:"owner"`
	Key    string  `json:"key"`
	CDID   string  `json:"cdid"`
	Hint   *string `json:"hint,omitempty"`
}

func ParseCCURI(escaped string) (*CCURI, error) {

	uriString, err := url.QueryUnescape(escaped)
	if err != nil {
		return nil, fmt.Errorf("invalid uri encoding")
	}
	uri, err := url.Parse(uriString)
	if err != nil {
		return nil, fmt.Errorf("invalid uri")
	}

	path := uri.Path
	key := strings.TrimPrefix(path, "/")

	var owner string
	var hint *string = nil
	if uri.User != nil {
		if uri.User.Username() != "" {
			owner = uri.User.Username()
			hint = &uri.Host
		} else {
			owner = "@" + uri.Host
		}
	} else {
		owner = uri.Host
	}

	owner, err = url.QueryUnescape(owner)
	if err != nil {
		return nil, fmt.Errorf("invalid owner encoding")
	}

	switch uri.Scheme {
	case "cckv":
		return &CCURI{
			Scheme: "cckv",
			Owner:  owner,
			Key:    key,
			CDID:   "",
			Hint:   hint,
		}, nil
	case "ccfs":
		return &CCURI{
			Scheme: "ccfs",
			Owner:  owner,
			Key:    "",
			CDID:   key,
			Hint:   hint,
		}, nil
	case "http", "https":
		return &CCURI{
			Scheme: "http",
		}, nil
	case "":
		return &CCURI{
			Scheme: "cckv",
			Owner:  uriString,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported uri scheme: %s", uri.Scheme)
	}
}

func ComposeCCURI(scheme, owner, key string) string {
	u := &url.URL{
		Scheme: scheme,
		Host:   owner,
		Path:   key,
	}
	return u.String()
}

func hasChar(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

func IsCCID(keyID string) bool {
	return len(keyID) == 42 && keyID[:3] == "con" && !hasChar(keyID, '.')
}

func IsCSID(keyID string) bool {
	return len(keyID) == 42 && keyID[:3] == "ccs" && !hasChar(keyID, '.')
}

func IsCKID(keyID string) bool {
	return len(keyID) == 42 && keyID[:3] == "cck" && !hasChar(keyID, '.')
}

func RenderURITemplate(template string, args map[string]string) (string, error) {

	t, err := uritemplate.New(template)
	if err != nil {
		return "", err
	}

	vars := uritemplate.Values{}
	for key, value := range args {
		vars.Set(key, uritemplate.String(value))
	}

	return t.Expand(vars)

}

func ToLegacyDocument(sd *SignedDocument) (*LegacyDocument, error) {

	var doc Document[any]
	err := json.Unmarshal([]byte(sd.Document), &doc)
	if err != nil {
		return nil, err
	}

	hash := GetHash([]byte(sd.Document))
	hash10 := [10]byte{}
	copy(hash10[:], hash[:10])
	documentID := cdid.New(hash10, doc.CreatedAt).String()

	parsed, err := ParseCCURI(doc.Key)
	if err != nil {
		return nil, fmt.Errorf("invalid document key: %v", err)
	}

	return &LegacyDocument{
		ID:        documentID,
		Author:    doc.Author,
		Owner:     &parsed.Owner,
		Schema:    doc.Schema,
		Document:  sd.Document,
		Signature: *sd.Proof.Signature,
		CDate:     doc.CreatedAt,
	}, nil
}
