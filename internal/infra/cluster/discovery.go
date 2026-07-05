package cluster

import (
	"context"
	"net"
	"strconv"
)

// Discovery lists the internal base URLs of all live replicas (including this
// one).
type Discovery interface {
	Peers(ctx context.Context) ([]string, error)
}

// DNSDiscovery resolves a Kubernetes headless service: each A/AAAA record is
// one replica.
type DNSDiscovery struct {
	host string
	port int
}

func NewDNSDiscovery(host string, port int) *DNSDiscovery {
	return &DNSDiscovery{host: host, port: port}
}

func (d *DNSDiscovery) Peers(ctx context.Context) ([]string, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, d.host)
	if err != nil {
		return nil, err
	}

	peers := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		peers = append(peers, "http://"+net.JoinHostPort(addr.IP.String(), strconv.Itoa(d.port)))
	}
	return peers, nil
}
