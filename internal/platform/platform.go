package platform

import "context"

type RouterNode struct {
	Name       string
	PrivateIP  string
	AZ         string
	ProviderID string
}

type DiscoveredNeighbor struct {
	Address string
	ASN     int64
}

type DiscoveredEndpoint struct {
	EndpointID string
	AZ         string
	Address    string
}

type DiscoveredRouteServer struct {
	RouteServerID string
	RemoteASN     int64
	Endpoints     []DiscoveredEndpoint
}

type DiscoveryResult struct {
	RouteServers    []DiscoveredRouteServer
	NeighborsByAZ   map[string][]DiscoveredNeighbor
	EndpointsByAZ   map[string][]string
}

type CloudPlatform interface {
	DiscoverEndpoints(ctx context.Context) (*DiscoveryResult, error)
	ReconcileNodes(ctx context.Context, nodes []RouterNode) error
	Cleanup(ctx context.Context) error
}
