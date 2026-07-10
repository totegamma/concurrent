package domain

import "time"

type CommitMode int

const (
	CommitModeUnknown CommitMode = iota
	CommitModeExecute
	CommitModeDryRun
	CommitModeLocalOnlyExecute
)

// Commit-time validation bounds on a document's author-signed createdAt.
const (
	// MaxBackdate is how far in the past a non-service-account commit's
	// createdAt may be. Documents older than now-MaxBackdate are rejected, and
	// explicitly deleted keys are tombstoned for exactly this long — so a
	// captured document is always either still tombstoned or already too old to
	// accept, which makes a deletion permanent against replay.
	MaxBackdate = 7 * 24 * time.Hour

	// MaxFutureSkew is how far ahead of server time a committed createdAt may
	// be, tolerating client clock skew. Applied to every committer (a
	// far-future stamp would otherwise dominate all later documents, e.g.
	// freeze entity accept-if-newer).
	MaxFutureSkew = 12 * time.Hour
)
