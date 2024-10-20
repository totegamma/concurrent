//go:generate go run go.uber.org/mock/mockgen -source=interfaces.go -destination=mock/services.go
package core

import (
	"context"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"io"
	"time"
)

type AckService interface {
	Ack(ctx context.Context, mode CommitMode, document, signature string) (Ack, error)
	GetAcker(ctx context.Context, key string) ([]Ack, error)
	GetAcking(ctx context.Context, key string) ([]Ack, error)
}

type AgentService interface {
	Boot()
}

type AssociationService interface {
	Create(ctx context.Context, mode CommitMode, document, signature string) (Association, []string, error)
	Delete(ctx context.Context, mode CommitMode, document, signature string) (Association, []string, error)

	Clean(ctx context.Context, ccid string) error
	Get(ctx context.Context, id string) (Association, error)
	GetOwn(ctx context.Context, author string) ([]Association, error)
	GetByTarget(ctx context.Context, targetID string) ([]Association, error)
	GetCountsBySchema(ctx context.Context, messageID string) (map[string]int64, error)
	GetBySchema(ctx context.Context, messageID string, schema string) ([]Association, error)
	GetCountsBySchemaAndVariant(ctx context.Context, messageID string, schema string) (map[string]int64, error)
	GetBySchemaAndVariant(ctx context.Context, messageID string, schema string, variant string) ([]Association, error)
	GetOwnByTarget(ctx context.Context, targetID, author string) ([]Association, error)
	Count(ctx context.Context) (int64, error)
}

type AuthService interface {
	IssuePassport(ctx context.Context, requester string, key []Key) (string, error)
	IdentifyIdentity(next echo.HandlerFunc) echo.HandlerFunc
	RateLimiter(configMap RateLimitConfigMap) echo.MiddlewareFunc
}

type DomainService interface {
	Upsert(ctx context.Context, host Domain) (Domain, error)
	Get(ctx context.Context, key string) (Domain, error)
	GetByFQDN(ctx context.Context, key string) (Domain, error)
	GetByCCID(ctx context.Context, key string) (Domain, error)
	ForceFetch(ctx context.Context, fqdn string) (Domain, error)
	List(ctx context.Context) ([]Domain, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, host Domain) error
	UpdateScrapeTime(ctx context.Context, id string, scrapeTime time.Time) error
}

type EntityService interface {
	Affiliation(ctx context.Context, mode CommitMode, document, signature, meta string) (Entity, error)
	Tombstone(ctx context.Context, mode CommitMode, document, signature string) (Entity, error)

	Clean(ctx context.Context, ccid string) error
	Get(ctx context.Context, ccid string) (Entity, error)
	GetWithHint(ctx context.Context, ccid, hint string) (Entity, error)
	GetMeta(ctx context.Context, ccid string) (EntityMeta, error)
	GetByAlias(ctx context.Context, alias string) (Entity, error)
	List(ctx context.Context) ([]Entity, error)
	UpdateScore(ctx context.Context, id string, score int) error
	UpdateTag(ctx context.Context, id, tag string) error
	IsUserExists(ctx context.Context, user string) bool
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int64, error)
	PullEntityFromRemote(ctx context.Context, id, domain string) (Entity, error)
}

type KeyService interface {
	Enact(ctx context.Context, mode CommitMode, payload, signature string) (Key, error)
	Revoke(ctx context.Context, mode CommitMode, payload, signature string) (Key, error)
	Clean(ctx context.Context, ccid string) error
	ResolveSubkey(ctx context.Context, keyID string) (string, error)
	GetKeyResolution(ctx context.Context, keyID string) ([]Key, error)
	GetRemoteKeyResolution(ctx context.Context, remote string, keyID string) ([]Key, error)
	GetAllKeys(ctx context.Context, owner string) ([]Key, error)
}

type MessageService interface {
	GetAsGuest(ctx context.Context, id string) (Message, error)
	GetAsUser(ctx context.Context, id string, requester Entity) (Message, error)
	GetWithOwnAssociations(ctx context.Context, id string, requester string) (Message, error)
	Clean(ctx context.Context, ccid string) error
	Create(ctx context.Context, mode CommitMode, document string, signature string) (Message, []string, error)
	Delete(ctx context.Context, mode CommitMode, document, signature string) (Message, []string, error)
	Count(ctx context.Context) (int64, error)
}

type PolicyService interface {
	Test(ctx context.Context, policy Policy, context RequestContext, action string) (PolicyEvalResult, error)
	TestWithPolicyURL(ctx context.Context, url string, context RequestContext, action string) (PolicyEvalResult, error)
	TestWithGlobalPolicy(ctx context.Context, context RequestContext, action string) (PolicyEvalResult, error)
	Summerize(results []PolicyEvalResult, action string, overrides *map[string]bool) bool
	AccumulateOr(results []PolicyEvalResult, action string, override *map[string]bool) PolicyEvalResult
}

type ProfileService interface {
	Upsert(ctx context.Context, mode CommitMode, document, signature string) (Profile, error)
	Delete(ctx context.Context, mode CommitMode, document string) (Profile, error)

	Clean(ctx context.Context, ccid string) error
	Count(ctx context.Context) (int64, error)
	Get(ctx context.Context, id string) (Profile, error)
	GetBySemanticID(ctx context.Context, semanticID, owner string) (Profile, error)
	GetByAuthorAndSchema(ctx context.Context, owner string, schema string) ([]Profile, error)
	GetByAuthor(ctx context.Context, owner string) ([]Profile, error)
	GetBySchema(ctx context.Context, schema string) ([]Profile, error)
}

type SchemaService interface {
	UrlToID(ctx context.Context, url string) (uint, error)
	IDToUrl(ctx context.Context, id uint) (string, error)
}

type SemanticIDService interface {
	Name(ctx context.Context, id, owner, target, document, signature string) (SemanticID, error)
	Lookup(ctx context.Context, id, owner string) (string, error)
	Delete(ctx context.Context, id, owner string) error
	Clean(ctx context.Context, ccid string) error
}

type SocketManager interface {
	Subscribe(conn *websocket.Conn, timelines []string)
	Unsubscribe(conn *websocket.Conn)
	GetAllRemoteSubs() []string
}

type StoreService interface {
	Commit(ctx context.Context, mode CommitMode, document, signature, option string, keys []Key, IP string) (any, error)
	Restore(ctx context.Context, archive io.Reader, from, IP string) ([]BatchResult, error)
	ValidateDocument(ctx context.Context, document, signature string, keys []Key) error
	CleanUserAllData(ctx context.Context, target string) error
	SyncCommitFile(ctx context.Context, owner string) (SyncStatus, error)
	SyncStatus(ctx context.Context, owner string) (SyncStatus, error)
}

type SubscriptionService interface {
	UpsertSubscription(ctx context.Context, mode CommitMode, document, signature string) (Subscription, error)
	Subscribe(ctx context.Context, mode CommitMode, document string, signature string) (SubscriptionItem, error)
	Unsubscribe(ctx context.Context, mode CommitMode, document string) (SubscriptionItem, error)
	DeleteSubscription(ctx context.Context, mode CommitMode, document string) (Subscription, error)
	Clean(ctx context.Context, ccid string) error

	GetSubscription(ctx context.Context, id string) (Subscription, error)
	GetOwnSubscriptions(ctx context.Context, owner string) ([]Subscription, error)
}

type TimelineService interface {
	UpsertTimeline(ctx context.Context, mode CommitMode, document, signature string) (Timeline, error)
	DeleteTimeline(ctx context.Context, mode CommitMode, document string) (Timeline, error)
	Event(ctx context.Context, mode CommitMode, document, signature string) (Event, error)

	Clean(ctx context.Context, ccid string) error

	LookupChunkItr(ctx context.Context, timeliens []string, epoch string) (map[string]string, error)
	LoadChunkBody(ctx context.Context, query map[string]string) (map[string]Chunk, error)

	GetRecentItems(ctx context.Context, timelines []string, until time.Time, limit int) ([]TimelineItem, error)
	GetRecentItemsFromSubscription(ctx context.Context, subscription string, until time.Time, limit int) ([]TimelineItem, error)
	GetImmediateItems(ctx context.Context, timelines []string, since time.Time, limit int) ([]TimelineItem, error)
	GetImmediateItemsFromSubscription(ctx context.Context, subscription string, since time.Time, limit int) ([]TimelineItem, error)
	GetItem(ctx context.Context, timeline string, id string) (TimelineItem, error)
	PostItem(ctx context.Context, timeline string, item TimelineItem, document, signature string) (TimelineItem, error)
	Retract(ctx context.Context, mode CommitMode, document, signature string) (TimelineItem, []string, error)
	RemoveItemsByResourceID(ctx context.Context, resourceID string) error

	PublishEvent(ctx context.Context, event Event) error

	GetTimeline(ctx context.Context, key string) (Timeline, error)
	GetTimelineAutoDomain(ctx context.Context, timelineID string) (Timeline, error)

	ListTimelineBySchema(ctx context.Context, schema string) ([]Timeline, error)
	ListTimelineByAuthor(ctx context.Context, author string) ([]Timeline, error)

	GetChunks(ctx context.Context, timelines []string, epoch string) (map[string]Chunk, error)

	ListTimelineSubscriptions(ctx context.Context) (map[string]int64, error)
	Count(ctx context.Context) (int64, error)
	NormalizeTimelineID(ctx context.Context, timeline string) (string, error)
	GetOwners(ctx context.Context, timelines []string) ([]string, error)

	Query(ctx context.Context, timelineID, schema, owner, author string, until time.Time, limit int) ([]TimelineItem, error)

	ListLocalRecentlyRemovedItems(ctx context.Context, timelines []string) (map[string][]string, error)

	Realtime(ctx context.Context, request <-chan []string, response chan<- Event)

	UpdateMetrics()
}

type JobService interface {
	List(ctx context.Context, requester string) ([]Job, error)
	Create(ctx context.Context, requester, typ, payload string, scheduled time.Time) (Job, error)
	Dequeue(ctx context.Context) (*Job, error)
	Complete(ctx context.Context, id, status, result string) (Job, error)
	Cancel(ctx context.Context, id string) (Job, error)
}
