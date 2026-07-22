package domain

import "time"

type CommitMode int

const (
	CommitModeUnknown CommitMode = iota
	CommitModeExecute
	CommitModeDryRun
	CommitModeLocalOnlyExecute
	// CommitModeCacheRemoteEntity is the internal re-entry GetEntity uses to
	// cache a fetched remote entity document. It behaves like Execute but
	// skips the CIP-3 §3.1 authority check, which would otherwise reject
	// every foreign-domain entity commit with 421 and break remote
	// resolution. Never accepted from the HTTP surface.
	CommitModeCacheRemoteEntity
)

// Commit-time validation bounds on a document's author-signed createdAt.
const (
	// MaxBackdate is how far in the past a non-service-account commit's
	// createdAt may be. Documents older than now-MaxBackdate are rejected, and
	// explicitly deleted (or overwritten) documents have their ccfs URI
	// (content id) tombstoned for exactly this long — so a captured document
	// is always either still tombstoned or already too old to accept, which
	// makes a deletion permanent against replay while leaving the cckv key
	// itself reusable for fresh documents.
	MaxBackdate = 7 * 24 * time.Hour

	// MaxFutureSkew is how far ahead of server time a committed createdAt may
	// be, tolerating client clock skew. Applied to every committer (a
	// far-future stamp would otherwise dominate all later documents, e.g.
	// freeze entity accept-if-newer).
	MaxFutureSkew = 12 * time.Hour
)

// Protocol size limits enforced at commit time.
const (
	// MaxDocumentSize bounds the UTF-8 serialization of a document (the signed
	// string itself, CIP-1 §4.1). Applied to every committer including system
	// service accounts: nothing larger is ever written to the DB.
	MaxDocumentSize = 32768

	// MaxAssociationVariantSize bounds associationVariant in bytes (CIP-9).
	MaxAssociationVariantSize = 512

	// MaxRecordKeySize bounds the key component of a record's cckv URI in
	// bytes (CIP-0 §7: 1..1024 bytes).
	MaxRecordKeySize = 1024
)
