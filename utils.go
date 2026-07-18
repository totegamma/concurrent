package concrnt

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/concrnt/concrnt/cdid"
	"github.com/yosida95/uritemplate/v3"
)

func JsonPrint(tag string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		slog.Error("failed to marshal json for debug print", slog.String("tag", tag), slog.String("error", err.Error()))
		return
	}
	fmt.Printf("=== %s ===\n%s\n", tag, string(b))
}

const (
	CCFSTypeConcrnt = "concrnt"
	CCFSTypeBlob    = "blob"
)

type CCURI struct {
	Scheme string  `json:"schema"`
	Owner  string  `json:"owner"`
	Key    string  `json:"key"`
	Type   string  `json:"type,omitempty"` // ccfs only: CCFSTypeConcrnt or CCFSTypeBlob
	CDID   string  `json:"cdid"`           // ccfs only: bare cdid (type=concrnt) or bare sha256 hex (type=blob)
	Hint   *string `json:"hint,omitempty"`
	Raw    string  `json:"raw"`
}

func (c CCURI) String() string {
	result := c.Raw
	switch c.Scheme {
	case "cckv":
		if c.Hint != nil {
			result = fmt.Sprintf("cckv://%s@%s/%s", c.Owner, *c.Hint, c.Key)
		} else {
			result = fmt.Sprintf("cckv://%s/%s", c.Owner, c.Key)
		}
		if c.Key == "" {
			result = strings.TrimSuffix(result, "/")
		}
	case "ccfs":
		if c.Hint != nil {
			result = fmt.Sprintf("ccfs://%s@%s/%s/%s", c.Owner, *c.Hint, c.Type, c.CDID)
		} else {
			result = fmt.Sprintf("ccfs://%s/%s/%s", c.Owner, c.Type, c.CDID)
		}
	}
	return result
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
			Raw:    uriString,
		}, nil
	case "ccfs":
		parts := strings.Split(key, "/")
		var ccfsType, hash string
		switch {
		case len(parts) == 1 && parts[0] != "" && parts[0] != CCFSTypeConcrnt && parts[0] != CCFSTypeBlob:
			// legacy flat form ccfs://<owner>/<hash>: interpret as a concrnt object
			ccfsType = CCFSTypeConcrnt
			hash = parts[0]
		case len(parts) == 2 && parts[0] != "" && parts[1] != "":
			ccfsType = parts[0]
			hash = parts[1]
			if ccfsType != CCFSTypeConcrnt && ccfsType != CCFSTypeBlob {
				return nil, fmt.Errorf("invalid ccfs type: %s", ccfsType)
			}
			if ccfsType == CCFSTypeBlob && !isSha256Hex(hash) {
				return nil, fmt.Errorf("invalid ccfs blob hash: expected 64 hex characters")
			}
		default:
			return nil, fmt.Errorf("invalid ccfs uri: expected ccfs://<owner>/<type>/<hash>")
		}
		return &CCURI{
			Scheme: "ccfs",
			Owner:  owner,
			Key:    "",
			Type:   ccfsType,
			CDID:   hash,
			Hint:   hint,
			Raw:    uriString,
		}, nil
	case "http", "https":
		return &CCURI{
			Scheme: "http",
			Raw:    uriString,
		}, nil
	case "":
		return &CCURI{
			Scheme: "cckv",
			Owner:  path,
			Raw:    uriString,
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

func ComposeCCFSURI(owner, ccfsType, hash string) string {
	return ComposeCCURI("ccfs", owner, ccfsType+"/"+hash)
}

func isSha256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
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
