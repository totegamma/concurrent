// Package cluster provides the coordination primitives used to run multiple
// replicas of concrnt: leader election (which replica runs the singleton
// workers) and peer discovery (which replicas exist, for aggregating realtime
// subscription demand). Outside Kubernetes, AlwaysLeader/NoPeers reproduce the
// single-instance behavior.
package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Elector decides which replica runs the singleton workers.
type Elector interface {
	// Run blocks. onLead is called when this replica becomes the leader; the
	// ctx passed to onLead is cancelled when leadership is lost.
	Run(ctx context.Context, onLead func(ctx context.Context))
	IsLeader() bool
	// LeaderURL returns the internal base URL of the current leader.
	LeaderURL() (string, bool)
}

// AlwaysLeader is the standalone (non-clustered) elector: this replica is the
// one and only leader.
type AlwaysLeader struct{}

func (AlwaysLeader) Run(ctx context.Context, onLead func(ctx context.Context)) {
	onLead(ctx)
	<-ctx.Done()
}

func (AlwaysLeader) IsLeader() bool { return true }

func (AlwaysLeader) LeaderURL() (string, bool) { return "", false }

const (
	leaseName     = "concrnt-leader"
	leaseDuration = 15 * time.Second
	renewDeadline = 10 * time.Second
	retryPeriod   = 2 * time.Second
)

// KubeElector elects a leader via a coordination.k8s.io Lease. The lease
// holder identity carries the pod IP so peers can derive LeaderURL without
// extra API calls.
type KubeElector struct {
	clientset    *kubernetes.Clientset
	namespace    string
	identity     string
	internalPort int

	elector atomic.Pointer[leaderelection.LeaderElector]
}

func NewKubeElector(internalPort int) (*KubeElector, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return nil, fmt.Errorf("POD_NAMESPACE is not set and namespace file is unreadable: %w", err)
		}
		namespace = strings.TrimSpace(string(data))
	}

	podName := os.Getenv("POD_NAME")
	if podName == "" {
		podName, _ = os.Hostname()
	}

	podIP := os.Getenv("POD_IP")
	if podIP == "" {
		return nil, fmt.Errorf("POD_IP is not set (expose it via the downward API)")
	}

	return &KubeElector{
		clientset:    clientset,
		namespace:    namespace,
		identity:     podName + "|" + podIP,
		internalPort: internalPort,
	}, nil
}

func (e *KubeElector) Run(ctx context.Context, onLead func(ctx context.Context)) {
	for ctx.Err() == nil {
		lock := &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      leaseName,
				Namespace: e.namespace,
			},
			Client: e.clientset.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: e.identity,
			},
		}

		elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
			Lock:            lock,
			LeaseDuration:   leaseDuration,
			RenewDeadline:   renewDeadline,
			RetryPeriod:     retryPeriod,
			ReleaseOnCancel: true,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(leadCtx context.Context) {
					slog.Info("acquired cluster leadership", slog.String("identity", e.identity))
					onLead(leadCtx)
				},
				OnStoppedLeading: func() {
					slog.Info("lost cluster leadership", slog.String("identity", e.identity))
				},
				OnNewLeader: func(identity string) {
					slog.Info("cluster leader observed", slog.String("leader", identity))
				},
			},
		})
		if err != nil {
			slog.Error("failed to create leader elector", slog.String("error", err.Error()))
			return
		}

		e.elector.Store(elector)
		// returns on ctx cancellation or when leadership is lost; loop to re-candidate
		elector.Run(ctx)
	}
}

func (e *KubeElector) IsLeader() bool {
	elector := e.elector.Load()
	if elector == nil {
		return false
	}
	return elector.IsLeader()
}

func (e *KubeElector) LeaderURL() (string, bool) {
	elector := e.elector.Load()
	if elector == nil {
		return "", false
	}
	leader := elector.GetLeader()
	if leader == "" {
		return "", false
	}
	_, ip, found := strings.Cut(leader, "|")
	if !found || ip == "" {
		return "", false
	}
	return "http://" + net.JoinHostPort(ip, strconv.Itoa(e.internalPort)), true
}
