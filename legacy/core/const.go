package core

const (
	RequesterTypeCtxKey      = "cc-requesterType"
	RequesterIdCtxKey        = "cc-requesterId"
	RequesterTagCtxKey       = "cc-requesterTag"
	RequesterDomainCtxKey    = "cc-requesterDomain"
	RequesterDomainTagsKey   = "cc-requesterDomainTags"
	RequesterKeychainKey     = "cc-requesterKeychain"
	RequesterPassportKey     = "cc-requesterPassport"
	RequesterIsRegisteredKey = "cc-requesterIsRegistered"
	CaptchaVerifiedKey       = "cc-captchaVerified"
)

const (
	RequesterTypeHeader         = "cc-requester-type"
	RequesterIdHeader           = "cc-requester-ccid"
	RequesterTagHeader          = "cc-requester-tag"
	RequesterDomainHeader       = "cc-requester-domain"
	RequesterDomainTagsHeader   = "cc-requester-domain-tags"
	RequesterKeychainHeader     = "cc-requester-keychain"
	RequesterPassportHeader     = "passport"
	RequesterIsRegisteredHeader = "cc-requester-is-registered"
	CaptchaVerifiedHeader       = "cc-captcha-verified"
)

type CommitMode int

const (
	CommitModeUnknown CommitMode = iota
	CommitModeExecute
	CommitModeDryRun
	CommitModeLocalOnlyExec
)

type PolicyEvalResult int

const (
	PolicyEvalResultDefault PolicyEvalResult = iota
	PolicyEvalResultNever
	PolicyEvalResultDeny
	PolicyEvalResultAllow
	PolicyEvalResultAlways
	PolicyEvalResultError
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
