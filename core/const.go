package core

const (
	RequesterTypeCtxKey    = "cc-requesterType"
	RequesterContextCtxKey = "cc-requesterContext"
	RequesterKeychainKey   = "cc-requesterKeychain"
	RequesterPassportKey   = "cc-requesterPassport"
	CaptchaVerifiedKey     = "cc-captchaVerified"
)

const (
	RequesterTypeHeader     = "cc-requester-type"
	RequesterContextHeader  = "cc-requester-context"
	RequesterKeychainHeader = "cc-requester-keychain"
	RequesterPassportHeader = "passport"
	CaptchaVerifiedHeader   = "cc-captcha-verified"
)

const (
	RequestPathCtxKey = "cc-request-path"
)

type CommitMode int

const (
	CommitModeUnknown CommitMode = iota
	CommitModeExecute
	CommitModeDryRun
	CommitModeLocalOnlyExec
)

const (
	Unknown = iota
	LocalUser
	RemoteUser
	RemoteDomain
)

func RequesterTypeString(t int) string {
	switch t {
	case LocalUser:
		return "LocalUser"
	case RemoteUser:
		return "RemoteUser"
	case RemoteDomain:
		return "RemoteDomain"
	case Unknown:
		return "Unknown"
	default:
		return "Error"
	}
}
