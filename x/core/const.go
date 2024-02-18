package core

const (
	RequesterTypeCtxKey    = "cc-requesterType"
	RequesterIdCtxKey      = "cc-requesterId"
	RequesterTagCtxKey     = "cc-requesterTag"
	RequesterDomainCtxKey  = "cc-requesterDomain"
	RequesterKeyDepathKey  = "cc-requesterKeyDepath"
	RequesterDomainTagsKey = "cc-requesterDomainTags"
	RequesterRemoteTagsKey = "cc-requesterRemoteTags"
)

const (
	RequesterTypeHeader       = "cc-requester-type"
	RequesterIdHeader         = "cc-requester-ccid"
	RequesterTagHeader        = "cc-requester-tag"
	RequesterDomainHeader     = "cc-requester-domain"
	RequesterKeyDepathHeader  = "cc-requester-key-depath"
	RequesterDomainTagsHeader = "cc-requester-domain-tags"
	RequesterRemoteTagsHeader = "cc-requester-remote-tags"
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
