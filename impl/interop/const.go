package interop

const (
	RequesterCtxKey          = "cc-requester"
	RequesterTagCtxKey       = "cc-requester-tag"
	CaptchaVerifiedCtxKey    = "cc-captcha-verified"
	ServiceAccountTypeCtxKey = "cc-service-account-type"
)

const (
	RequesterHeader          = "cc-requester"
	RequesterTagHeader       = "cc-requester-tag"
	CaptchaVerifiedHeader    = "cc-captcha-verified"
	ServiceAccountTypeHeader = "cc-service-account-type"
)

// EventChannelPrefix namespaces concrnt realtime events on the shared Redis
// pubsub. Only the Redis channel name carries it — Event.Source and every
// application-level prefix stay raw resource URIs.
//
// Events on this channel are the server-internal view: they carry every
// document, including ones an anonymous requester may not read. Each
// document's "isPublic" field (concrnt.SignedDocument.IsPublic, set at
// publish time) says whether it passed the anonymous read baseline
// (CIP-11 §3.2). Modules consuming this channel may use all documents
// internally, but before re-serving an event to unauthenticated clients they
// must apply concrnt.Event.PublicView (or equivalently drop every document
// whose isPublic is not true) — isPublic itself is not part of the CCAPI
// wire format and must not leave the server.
const EventChannelPrefix = "cc-event:"
