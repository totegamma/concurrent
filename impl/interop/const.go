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
const EventChannelPrefix = "cc-event:"
