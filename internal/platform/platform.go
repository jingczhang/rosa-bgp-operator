package platform

import "context"

type RouterNode struct {
	Name       string
	PrivateIP  string
	AZ         string
	ProviderID string
}

type CloudPlatform interface {
	ReconcileNodes(ctx context.Context, nodes []RouterNode) error
	Cleanup(ctx context.Context) error
}
