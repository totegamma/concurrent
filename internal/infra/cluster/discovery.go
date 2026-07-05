package cluster

import (
	"context"
)

// Discovery lists the internal base URLs of all live replicas (including this
// one). Implemented by HTTPElector, which obtains the list from the external
// elector service.
type Discovery interface {
	Peers(ctx context.Context) ([]string, error)
}
