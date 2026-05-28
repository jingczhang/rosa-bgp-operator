package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/openshift/cudn-bgp-routing-operator/internal/platform"
)

// --- Mocks ---

type mockSTS struct {
	err error
}

func (m *mockSTS) GetCallerIdentity(_ context.Context, _ *sts.GetCallerIdentityInput, _ ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &sts.GetCallerIdentityOutput{}, nil
}

type mockEC2 struct {
	describeRSFunc     func(*ec2.DescribeRouteServersInput) (*ec2.DescribeRouteServersOutput, error)
	describeRSEFunc    func(*ec2.DescribeRouteServerEndpointsInput) (*ec2.DescribeRouteServerEndpointsOutput, error)
	describeSubFunc    func(*ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error)
	describePeersFunc  func(*ec2.DescribeRouteServerPeersInput) (*ec2.DescribeRouteServerPeersOutput, error)
	createPeerFunc     func(*ec2.CreateRouteServerPeerInput) (*ec2.CreateRouteServerPeerOutput, error)
	deletePeerFunc     func(*ec2.DeleteRouteServerPeerInput) (*ec2.DeleteRouteServerPeerOutput, error)
	createTagsFunc     func(*ec2.CreateTagsInput) (*ec2.CreateTagsOutput, error)
	describeInstFunc   func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	modifyENIFunc      func(*ec2.ModifyNetworkInterfaceAttributeInput) (*ec2.ModifyNetworkInterfaceAttributeOutput, error)

	createPeerCalls  []*ec2.CreateRouteServerPeerInput
	deletePeerCalls  []*ec2.DeleteRouteServerPeerInput
	createTagsCalls  []*ec2.CreateTagsInput
	modifyENICalls   []*ec2.ModifyNetworkInterfaceAttributeInput
}

func (m *mockEC2) DescribeRouteServers(_ context.Context, input *ec2.DescribeRouteServersInput, _ ...func(*ec2.Options)) (*ec2.DescribeRouteServersOutput, error) {
	if m.describeRSFunc != nil {
		return m.describeRSFunc(input)
	}
	return &ec2.DescribeRouteServersOutput{}, nil
}

func (m *mockEC2) DescribeRouteServerEndpoints(_ context.Context, input *ec2.DescribeRouteServerEndpointsInput, _ ...func(*ec2.Options)) (*ec2.DescribeRouteServerEndpointsOutput, error) {
	if m.describeRSEFunc != nil {
		return m.describeRSEFunc(input)
	}
	return &ec2.DescribeRouteServerEndpointsOutput{}, nil
}

func (m *mockEC2) DescribeSubnets(_ context.Context, input *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	if m.describeSubFunc != nil {
		return m.describeSubFunc(input)
	}
	return &ec2.DescribeSubnetsOutput{}, nil
}

func (m *mockEC2) DescribeRouteServerPeers(_ context.Context, input *ec2.DescribeRouteServerPeersInput, _ ...func(*ec2.Options)) (*ec2.DescribeRouteServerPeersOutput, error) {
	if m.describePeersFunc != nil {
		return m.describePeersFunc(input)
	}
	return &ec2.DescribeRouteServerPeersOutput{}, nil
}

func (m *mockEC2) CreateRouteServerPeer(_ context.Context, input *ec2.CreateRouteServerPeerInput, _ ...func(*ec2.Options)) (*ec2.CreateRouteServerPeerOutput, error) {
	m.createPeerCalls = append(m.createPeerCalls, input)
	if m.createPeerFunc != nil {
		return m.createPeerFunc(input)
	}
	return &ec2.CreateRouteServerPeerOutput{}, nil
}

func (m *mockEC2) DeleteRouteServerPeer(_ context.Context, input *ec2.DeleteRouteServerPeerInput, _ ...func(*ec2.Options)) (*ec2.DeleteRouteServerPeerOutput, error) {
	m.deletePeerCalls = append(m.deletePeerCalls, input)
	if m.deletePeerFunc != nil {
		return m.deletePeerFunc(input)
	}
	return &ec2.DeleteRouteServerPeerOutput{}, nil
}

func (m *mockEC2) CreateTags(_ context.Context, input *ec2.CreateTagsInput, _ ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error) {
	m.createTagsCalls = append(m.createTagsCalls, input)
	if m.createTagsFunc != nil {
		return m.createTagsFunc(input)
	}
	return &ec2.CreateTagsOutput{}, nil
}

func (m *mockEC2) DescribeInstances(_ context.Context, input *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if m.describeInstFunc != nil {
		return m.describeInstFunc(input)
	}
	return &ec2.DescribeInstancesOutput{}, nil
}

func (m *mockEC2) ModifyNetworkInterfaceAttribute(_ context.Context, input *ec2.ModifyNetworkInterfaceAttributeInput, _ ...func(*ec2.Options)) (*ec2.ModifyNetworkInterfaceAttributeOutput, error) {
	m.modifyENICalls = append(m.modifyENICalls, input)
	if m.modifyENIFunc != nil {
		return m.modifyENIFunc(input)
	}
	return &ec2.ModifyNetworkInterfaceAttributeOutput{}, nil
}

// --- UT-AWS-01 to UT-AWS-02: Credential Validation ---

func TestNew_ValidCredentials(t *testing.T) {
	p, err := newPlatform(context.Background(), Config{
		AccessKeyID:    "AKID",
		SecretAccessKey: "secret",
		Region:         "us-east-1",
		RouteServerIDs: []string{"rs-1"},
		LocalASN:       65001,
	}, &mockEC2{}, &mockSTS{})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if p == nil {
		t.Fatal("expected platform to be non-nil")
	}
}

func TestNew_InvalidCredentials(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		secret string
		sts    *mockSTS
	}{
		{"empty key", "", "secret", &mockSTS{}},
		{"empty secret", "AKID", "", &mockSTS{}},
		{"STS auth failure", "AKID", "secret", &mockSTS{err: errors.New("InvalidClientTokenId")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newPlatform(context.Background(), Config{
				AccessKeyID:    tc.key,
				SecretAccessKey: tc.secret,
				Region:         "us-east-1",
			}, &mockEC2{}, tc.sts)

			var credErr *CredentialError
			if !errors.As(err, &credErr) {
				t.Fatalf("expected CredentialError, got: %v", err)
			}
		})
	}
}

// --- UT-AWS-03 to UT-AWS-04: Provider ID Parsing ---

func TestParseProviderID_Valid(t *testing.T) {
	instanceID, az, err := ParseProviderID("aws:///us-east-1a/i-0abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if instanceID != "i-0abc123" {
		t.Errorf("instanceID = %q, want %q", instanceID, "i-0abc123")
	}
	if az != "us-east-1a" {
		t.Errorf("az = %q, want %q", az, "us-east-1a")
	}
}

func TestParseProviderID_Invalid(t *testing.T) {
	cases := []string{
		"gce:///zone/instance",
		"aws:///us-east-1a/",
		"aws:////i-0abc123",
		"aws://something",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, _, err := ParseProviderID(input)
			if err == nil {
				t.Errorf("expected error for %q", input)
			}
		})
	}
}

// --- UT-AWS-09 to UT-AWS-13: Route Server Peer Reconciliation ---

func newTestPlatform(mock *mockEC2) *Platform {
	return &Platform{
		ec2Client:      mock,
		region:         "us-east-1",
		routeServerIDs: []string{"rs-1"},
		endpointsByAZ: map[string][]string{
			"us-east-1a": {"ep-a1", "ep-a2"},
			"us-east-1b": {"ep-b1", "ep-b2"},
			"us-east-1c": {"ep-c1", "ep-c2"},
		},
		localASN:          65001,
		livenessDetection: "",
		clusterID:         "test-cluster",
	}
}

func TestReconcilePeers_MultiAZCreate(t *testing.T) {
	mock := &mockEC2{
		describePeersFunc: func(_ *ec2.DescribeRouteServerPeersInput) (*ec2.DescribeRouteServerPeersOutput, error) {
			return &ec2.DescribeRouteServerPeersOutput{}, nil
		},
	}
	p := newTestPlatform(mock)

	nodes := []platform.RouterNode{
		{Name: "node-a", PrivateIP: "10.0.1.10", AZ: "us-east-1a", ProviderID: "aws:///us-east-1a/i-a"},
		{Name: "node-b", PrivateIP: "10.0.2.10", AZ: "us-east-1b", ProviderID: "aws:///us-east-1b/i-b"},
		{Name: "node-c", PrivateIP: "10.0.3.10", AZ: "us-east-1c", ProviderID: "aws:///us-east-1c/i-c"},
	}

	if err := p.reconcileRouteServerPeers(context.Background(), nodes); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 AZs × 2 endpoints × 1 node = 6 create calls
	if len(mock.createPeerCalls) != 6 {
		t.Fatalf("expected 6 create calls, got %d", len(mock.createPeerCalls))
	}

	for _, call := range mock.createPeerCalls {
		if call.BgpOptions == nil || aws.ToInt64(call.BgpOptions.PeerAsn) != 65001 {
			t.Error("expected ASN 65001 in create call")
		}
		if len(call.TagSpecifications) == 0 {
			t.Error("expected tag specifications in create call")
		} else {
			found := false
			for _, tag := range call.TagSpecifications[0].Tags {
				if aws.ToString(tag.Key) == "managed-by" && aws.ToString(tag.Value) == "cudn-bgp-routing-operator/test-cluster" {
					found = true
				}
			}
			if !found {
				t.Error("expected managed-by tag in create call")
			}
		}
	}
}

func TestReconcilePeers_AdoptPreExistingUntagged(t *testing.T) {
	mock := &mockEC2{
		describePeersFunc: func(input *ec2.DescribeRouteServerPeersInput) (*ec2.DescribeRouteServerPeersOutput, error) {
			hasTagFilter := false
			for _, f := range input.Filters {
				if aws.ToString(f.Name) == "tag:managed-by" {
					hasTagFilter = true
				}
			}
			if hasTagFilter {
				// listManagedPeers: no managed peers
				return &ec2.DescribeRouteServerPeersOutput{}, nil
			}
			// listAllPeers: return untagged peer with desired IP
			return &ec2.DescribeRouteServerPeersOutput{
				RouteServerPeers: []ec2types.RouteServerPeer{
					{PeerAddress: aws.String("10.0.1.10"), RouteServerPeerId: aws.String("peer-existing")},
				},
			}, nil
		},
	}
	p := &Platform{
		ec2Client:     mock,
		endpointsByAZ: map[string][]string{"us-east-1a": {"ep-a1"}},
		localASN:      65001,
		clusterID:     "test-cluster",
	}

	nodes := []platform.RouterNode{
		{Name: "node-a", PrivateIP: "10.0.1.10", AZ: "us-east-1a", ProviderID: "aws:///us-east-1a/i-a"},
	}

	if err := p.reconcileRouteServerPeers(context.Background(), nodes); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.createPeerCalls) != 0 {
		t.Errorf("expected no create calls, got %d", len(mock.createPeerCalls))
	}
	if len(mock.createTagsCalls) != 1 {
		t.Fatalf("expected 1 CreateTags call, got %d", len(mock.createTagsCalls))
	}
	if mock.createTagsCalls[0].Resources[0] != "peer-existing" {
		t.Errorf("expected tag on peer-existing, got %v", mock.createTagsCalls[0].Resources)
	}
}

func TestReconcilePeers_DeleteStalePeer(t *testing.T) {
	mock := &mockEC2{
		describePeersFunc: func(input *ec2.DescribeRouteServerPeersInput) (*ec2.DescribeRouteServerPeersOutput, error) {
			hasTagFilter := false
			for _, f := range input.Filters {
				if aws.ToString(f.Name) == "tag:managed-by" {
					hasTagFilter = true
				}
			}
			if hasTagFilter {
				return &ec2.DescribeRouteServerPeersOutput{
					RouteServerPeers: []ec2types.RouteServerPeer{
						{PeerAddress: aws.String("10.0.1.99"), RouteServerPeerId: aws.String("peer-stale")},
					},
				}, nil
			}
			return &ec2.DescribeRouteServerPeersOutput{
				RouteServerPeers: []ec2types.RouteServerPeer{
					{PeerAddress: aws.String("10.0.1.99"), RouteServerPeerId: aws.String("peer-stale")},
				},
			}, nil
		},
	}
	p := &Platform{
		ec2Client:     mock,
		endpointsByAZ: map[string][]string{"us-east-1a": {"ep-a1"}},
		localASN:      65001,
		clusterID:     "test-cluster",
	}

	// No desired nodes in this AZ — the managed peer is stale
	if err := p.reconcileRouteServerPeers(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.deletePeerCalls) != 1 {
		t.Fatalf("expected 1 delete call, got %d", len(mock.deletePeerCalls))
	}
	if aws.ToString(mock.deletePeerCalls[0].RouteServerPeerId) != "peer-stale" {
		t.Errorf("expected delete of peer-stale, got %s", aws.ToString(mock.deletePeerCalls[0].RouteServerPeerId))
	}
	if len(mock.createPeerCalls) != 0 {
		t.Errorf("expected no create calls, got %d", len(mock.createPeerCalls))
	}
}

func TestReconcilePeers_BFDLivenessDetection(t *testing.T) {
	mock := &mockEC2{
		describePeersFunc: func(_ *ec2.DescribeRouteServerPeersInput) (*ec2.DescribeRouteServerPeersOutput, error) {
			return &ec2.DescribeRouteServerPeersOutput{}, nil
		},
	}
	p := &Platform{
		ec2Client:         mock,
		endpointsByAZ:     map[string][]string{"us-east-1a": {"ep-a1"}},
		localASN:          65001,
		livenessDetection: "bfd",
		clusterID:         "test-cluster",
	}

	nodes := []platform.RouterNode{
		{Name: "node-a", PrivateIP: "10.0.1.10", AZ: "us-east-1a", ProviderID: "aws:///us-east-1a/i-a"},
	}

	if err := p.reconcileRouteServerPeers(context.Background(), nodes); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.createPeerCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(mock.createPeerCalls))
	}
	bgpOpts := mock.createPeerCalls[0].BgpOptions
	if bgpOpts == nil || bgpOpts.PeerLivenessDetection != ec2types.RouteServerPeerLivenessMode("bfd") {
		t.Error("expected BFD peer liveness mode in create call")
	}
}

func TestCleanup_DeletesAllManagedPeers(t *testing.T) {
	mock := &mockEC2{
		describePeersFunc: func(input *ec2.DescribeRouteServerPeersInput) (*ec2.DescribeRouteServerPeersOutput, error) {
			// Return 1 peer per endpoint to simulate 6 total across 3 AZs × 2 endpoints
			return &ec2.DescribeRouteServerPeersOutput{
				RouteServerPeers: []ec2types.RouteServerPeer{
					{PeerAddress: aws.String("10.0.0.1"), RouteServerPeerId: aws.String("peer-" + input.Filters[0].Values[0])},
				},
			}, nil
		},
	}
	p := newTestPlatform(mock)

	if err := p.Cleanup(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.deletePeerCalls) != 6 {
		t.Errorf("expected 6 delete calls (3 AZs × 2 endpoints), got %d", len(mock.deletePeerCalls))
	}
}

// --- UT-AWS-14 to UT-AWS-16: SourceDestCheck ---

func TestDisableSourceDestCheck_EnabledToDisabled(t *testing.T) {
	mock := &mockEC2{
		describeInstFunc: func(_ *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{
				Reservations: []ec2types.Reservation{{
					Instances: []ec2types.Instance{{
						NetworkInterfaces: []ec2types.InstanceNetworkInterface{{
							NetworkInterfaceId: aws.String("eni-primary"),
							SourceDestCheck:    aws.Bool(true),
							Attachment:         &ec2types.InstanceNetworkInterfaceAttachment{DeviceIndex: aws.Int32(0)},
						}},
					}},
				}},
			}, nil
		},
	}
	p := &Platform{ec2Client: mock}

	nodes := []platform.RouterNode{
		{Name: "node-a", PrivateIP: "10.0.1.10", AZ: "us-east-1a", ProviderID: "aws:///us-east-1a/i-a"},
	}
	if err := p.disableSourceDestCheck(context.Background(), nodes); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.modifyENICalls) != 1 {
		t.Fatalf("expected 1 modify call, got %d", len(mock.modifyENICalls))
	}
	if aws.ToString(mock.modifyENICalls[0].NetworkInterfaceId) != "eni-primary" {
		t.Errorf("expected eni-primary, got %s", aws.ToString(mock.modifyENICalls[0].NetworkInterfaceId))
	}
	if aws.ToBool(mock.modifyENICalls[0].SourceDestCheck.Value) != false {
		t.Error("expected SourceDestCheck set to false")
	}
}

func TestDisableSourceDestCheck_AlreadyDisabled(t *testing.T) {
	mock := &mockEC2{
		describeInstFunc: func(_ *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{
				Reservations: []ec2types.Reservation{{
					Instances: []ec2types.Instance{{
						NetworkInterfaces: []ec2types.InstanceNetworkInterface{{
							NetworkInterfaceId: aws.String("eni-primary"),
							SourceDestCheck:    aws.Bool(false),
							Attachment:         &ec2types.InstanceNetworkInterfaceAttachment{DeviceIndex: aws.Int32(0)},
						}},
					}},
				}},
			}, nil
		},
	}
	p := &Platform{ec2Client: mock}

	nodes := []platform.RouterNode{
		{Name: "node-a", PrivateIP: "10.0.1.10", AZ: "us-east-1a", ProviderID: "aws:///us-east-1a/i-a"},
	}
	if err := p.disableSourceDestCheck(context.Background(), nodes); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.modifyENICalls) != 0 {
		t.Errorf("expected no modify calls when already disabled, got %d", len(mock.modifyENICalls))
	}
}

func TestDisableSourceDestCheck_NoPrimaryENI(t *testing.T) {
	mock := &mockEC2{
		describeInstFunc: func(_ *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{
				Reservations: []ec2types.Reservation{{
					Instances: []ec2types.Instance{{
						NetworkInterfaces: []ec2types.InstanceNetworkInterface{{
							NetworkInterfaceId: aws.String("eni-secondary"),
							SourceDestCheck:    aws.Bool(true),
							Attachment:         &ec2types.InstanceNetworkInterfaceAttachment{DeviceIndex: aws.Int32(1)},
						}},
					}},
				}},
			}, nil
		},
	}
	p := &Platform{ec2Client: mock}

	nodes := []platform.RouterNode{
		{Name: "node-a", PrivateIP: "10.0.1.10", AZ: "us-east-1a", ProviderID: "aws:///us-east-1a/i-a"},
	}
	err := p.disableSourceDestCheck(context.Background(), nodes)
	if err == nil {
		t.Fatal("expected error when no primary ENI found")
	}
}

// --- UT-AWS-05 to UT-AWS-08: Endpoint Discovery ---

func newDiscoveryMock() *mockEC2 {
	return &mockEC2{
		describeRSFunc: func(input *ec2.DescribeRouteServersInput) (*ec2.DescribeRouteServersOutput, error) {
			return &ec2.DescribeRouteServersOutput{
				RouteServers: []ec2types.RouteServer{
					{RouteServerId: aws.String(input.RouteServerIds[0]), AmazonSideAsn: aws.Int64(64512)},
				},
			}, nil
		},
		describeRSEFunc: func(_ *ec2.DescribeRouteServerEndpointsInput) (*ec2.DescribeRouteServerEndpointsOutput, error) {
			return &ec2.DescribeRouteServerEndpointsOutput{
				RouteServerEndpoints: []ec2types.RouteServerEndpoint{
					{RouteServerEndpointId: aws.String("rse-a1"), SubnetId: aws.String("subnet-a"), EniAddress: aws.String("10.0.1.47")},
					{RouteServerEndpointId: aws.String("rse-a2"), SubnetId: aws.String("subnet-a"), EniAddress: aws.String("10.0.1.183")},
					{RouteServerEndpointId: aws.String("rse-b1"), SubnetId: aws.String("subnet-b"), EniAddress: aws.String("10.0.2.91")},
					{RouteServerEndpointId: aws.String("rse-b2"), SubnetId: aws.String("subnet-b"), EniAddress: aws.String("10.0.2.145")},
					{RouteServerEndpointId: aws.String("rse-c1"), SubnetId: aws.String("subnet-c"), EniAddress: aws.String("10.0.3.62")},
					{RouteServerEndpointId: aws.String("rse-c2"), SubnetId: aws.String("subnet-c"), EniAddress: aws.String("10.0.3.118")},
				},
			}, nil
		},
		describeSubFunc: func(_ *ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error) {
			return &ec2.DescribeSubnetsOutput{
				Subnets: []ec2types.Subnet{
					{SubnetId: aws.String("subnet-a"), AvailabilityZone: aws.String("us-east-1a")},
					{SubnetId: aws.String("subnet-b"), AvailabilityZone: aws.String("us-east-1b")},
					{SubnetId: aws.String("subnet-c"), AvailabilityZone: aws.String("us-east-1c")},
				},
			}, nil
		},
	}
}

func TestDiscoverEndpoints_Success(t *testing.T) {
	mock := newDiscoveryMock()
	p := &Platform{
		ec2Client:      mock,
		routeServerIDs: []string{"rs-1"},
	}

	result, err := p.DiscoverEndpoints(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.RouteServers) != 1 {
		t.Fatalf("expected 1 route server, got %d", len(result.RouteServers))
	}
	if result.RouteServers[0].RemoteASN != 64512 {
		t.Errorf("expected ASN 64512, got %d", result.RouteServers[0].RemoteASN)
	}
	if len(result.RouteServers[0].Endpoints) != 6 {
		t.Fatalf("expected 6 endpoints, got %d", len(result.RouteServers[0].Endpoints))
	}
	if len(result.NeighborsByAZ) != 3 {
		t.Fatalf("expected 3 AZs in neighbors, got %d", len(result.NeighborsByAZ))
	}
	for _, az := range []string{"us-east-1a", "us-east-1b", "us-east-1c"} {
		if len(result.NeighborsByAZ[az]) != 2 {
			t.Errorf("expected 2 neighbors in %s, got %d", az, len(result.NeighborsByAZ[az]))
		}
	}
	if len(p.endpointsByAZ) != 3 {
		t.Errorf("expected endpointsByAZ populated with 3 AZs, got %d", len(p.endpointsByAZ))
	}
}

func TestDiscoverEndpoints_RouteServerNotFound(t *testing.T) {
	mock := &mockEC2{
		describeRSFunc: func(_ *ec2.DescribeRouteServersInput) (*ec2.DescribeRouteServersOutput, error) {
			return &ec2.DescribeRouteServersOutput{}, nil
		},
	}
	p := &Platform{
		ec2Client:      mock,
		routeServerIDs: []string{"rs-nonexistent"},
	}

	_, err := p.DiscoverEndpoints(context.Background())
	if err == nil {
		t.Fatal("expected error for non-existent route server")
	}
}

func TestDiscoverEndpoints_MultipleRouteServers(t *testing.T) {
	mock := &mockEC2{
		describeRSFunc: func(input *ec2.DescribeRouteServersInput) (*ec2.DescribeRouteServersOutput, error) {
			asn := int64(64512)
			if input.RouteServerIds[0] == "rs-2" {
				asn = 64513
			}
			return &ec2.DescribeRouteServersOutput{
				RouteServers: []ec2types.RouteServer{
					{RouteServerId: aws.String(input.RouteServerIds[0]), AmazonSideAsn: aws.Int64(asn)},
				},
			}, nil
		},
		describeRSEFunc: func(input *ec2.DescribeRouteServerEndpointsInput) (*ec2.DescribeRouteServerEndpointsOutput, error) {
			rsID := ""
			for _, f := range input.Filters {
				if aws.ToString(f.Name) == "route-server-id" {
					rsID = f.Values[0]
				}
			}
			if rsID == "rs-1" {
				return &ec2.DescribeRouteServerEndpointsOutput{
					RouteServerEndpoints: []ec2types.RouteServerEndpoint{
						{RouteServerEndpointId: aws.String("rse-1a"), SubnetId: aws.String("subnet-a"), EniAddress: aws.String("10.0.1.10")},
					},
				}, nil
			}
			return &ec2.DescribeRouteServerEndpointsOutput{
				RouteServerEndpoints: []ec2types.RouteServerEndpoint{
					{RouteServerEndpointId: aws.String("rse-2a"), SubnetId: aws.String("subnet-a"), EniAddress: aws.String("10.0.1.20")},
				},
			}, nil
		},
		describeSubFunc: func(_ *ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error) {
			return &ec2.DescribeSubnetsOutput{
				Subnets: []ec2types.Subnet{
					{SubnetId: aws.String("subnet-a"), AvailabilityZone: aws.String("us-east-1a")},
				},
			}, nil
		},
	}

	p := &Platform{
		ec2Client:      mock,
		routeServerIDs: []string{"rs-1", "rs-2"},
	}

	result, err := p.DiscoverEndpoints(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.RouteServers) != 2 {
		t.Fatalf("expected 2 route servers, got %d", len(result.RouteServers))
	}
	if len(result.NeighborsByAZ["us-east-1a"]) != 2 {
		t.Errorf("expected 2 neighbors in us-east-1a (from 2 RS), got %d", len(result.NeighborsByAZ["us-east-1a"]))
	}
}

func TestDiscoverEndpoints_APIFailure(t *testing.T) {
	mock := &mockEC2{
		describeRSFunc: func(_ *ec2.DescribeRouteServersInput) (*ec2.DescribeRouteServersOutput, error) {
			return nil, errors.New("ec2 API failure")
		},
	}
	p := &Platform{
		ec2Client:      mock,
		routeServerIDs: []string{"rs-1"},
	}

	_, err := p.DiscoverEndpoints(context.Background())
	if err == nil {
		t.Fatal("expected error on API failure")
	}
}
