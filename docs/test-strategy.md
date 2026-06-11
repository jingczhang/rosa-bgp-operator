# Test Strategy

Test strategy for the CUDN BGP Routing Operator. The operator has two layers of functionality — core BGP/CUDN reconciliation (platform-independent) and cloud platform integration (per-provider). This document describes the shared test structure that all platform-specific test plans follow.

- [Coverage by layer and location](#coverage-by-layer-and-location)
- [Test Layers](#test-layers)
- [Platform Interface](#platform-interface)
- [Test Plans](#test-plans)
- [OpenShift CI Pipeline](#openshift-ci-pipeline)

## Coverage by layer and location

```
                      Platform-independent              Provider-specific (AWS/GCP/Azure)
                   ┌──────────────────────────────┐  ┌──────────────────────────────────────┐
                   │                              │  │                                      │
  Unit tests       │  internal/controller/*_test  │  │  internal/platform/<provider>/*_test │
                   │  • Config controller         │  │  • IRSA credential verification      │
                   │    Phases 1-5 (mocked        │  │  • Provider ID → instance ID + AZ    │
                   │    CloudPlatform)            │  │  • Endpoint discovery (mocked API)   │
                   │                              │  │  • Peer reconciliation (mocked API)  │
                   │  • Helpers (NS, CUDN, FRR,   │  │  • Forwarding fix (mocked API)       │
                   │    RouteAdvertisements)      │  │                                      │
                   │  • Routing controller        │  │                                      │
                   │                              │  │                                      │
  E2E tests        │  test/e2e/ (shared tests)    │  │  test/e2e/<provider>/ tests          │
                   │  • (none currently defined)  │  │  • Full stack reconcile on cluster   │
                   │                              │  │  • Node-to-peer consistency          │
                   │                              │  │  • Cloud drift recovery              │
                   │                              │  │  • Deletion cleanup (cloud resources)│
                   │                              │  │  Skip when IRSA not configured       │
                   │                              │  │                                      │
                   └──────────────────────────────┘  └──────────────────────────────────────┘
```

## Test Layers

Two layers, ordered by infrastructure cost and feedback speed:

| Layer | What it validates | Speed | Infrastructure |
|:---|:---|:---|:---|
| Unit | Reconciliation logic, error handling, edge cases | ~30s | None (fake client + mocks) |
| E2E | Full operator lifecycle on a real cluster with cloud infrastructure | ~10min | Cluster + cloud infra (Terraform) |

### Unit test structure

```
internal/
  controller/
    cudnbgpconfig_controller_test.go   ← config controller (Phases 1-5, mocked CloudPlatform)
    cudnbgprouting_controller_test.go  ← routing controller
    helpers_test.go                    ← helpers (NS, CUDN, FRR, RouteAdvertisements)
  platform/
    aws/
      aws_test.go                      ← AWS platform (mocked EC2 client)
```

### E2E test structure

```
test/e2e/
  e2e_suite_test.go
  e2e_test.go                          ← shared (no tests currently defined)
  aws/
    aws_e2e_suite_test.go
    aws_e2e_test.go                    ← AWS E2E (requires IRSA configured)
  manifests/
    poc/                               ← PoC profile (CUDNBgpConfig + CUDNBgpRouting)
      cudnbgpconfig.yaml
      cudnbgprouting.yaml
```

E2E tests read CR manifests from a profile directory under `test/e2e/manifests/<profile>/`. The test framework:

1. Deploys the operator
2. Applies CRs from the selected profile directory
3. Discovers expected state from the operator's `status.aws.routeServers` (auto-discovered endpoints) + listing cluster nodes
4. Asserts relative outcomes (e.g., "peers per endpoint == router nodes in that AZ", "status.aws contains discovered endpoints for all Route Server IDs")

Provider-independent E2E tests ignore `spec.aws` (or any provider section) in the CRs. Provider-specific tests use the full CR.

### Make targets

| Target | What it runs | Credentials needed |
|:---|:---|:---|
| `make test` | Platform-independent unit tests (`internal/controller/`) | No |
| `make test-aws` | AWS unit tests, mocked (`internal/platform/aws/`) | No |
| `make test-e2e` | Shared E2E (operator starts) | No (just cluster) |
| `make test-e2e-aws <profile>` | AWS E2E tests (`test/e2e/aws/`), profile required | Yes (cluster + IRSA configured) |

## Platform Interface

The `CloudPlatform` interface (`internal/platform/platform.go`) defines three methods:

```go
type CloudPlatform interface {
    DiscoverEndpoints(ctx context.Context) (*DiscoveryResult, error)
    ReconcileNodes(ctx context.Context, nodes []RouterNode) error
    Cleanup(ctx context.Context) error
}
```

`DiscoverEndpoints` returns the discovered Route Server endpoints, their BGP neighbor addresses, AZs, and remote ASN. This data drives FRR configuration generation (Phase 4) and is written to CR status. Every cloud provider implements this interface. Each provider's test plan maps to the same set of concerns:

| Test category | Interface concept | AWS | GCP (future) | Azure (future) |
|:---|:---|:---|:---|:---|
| Platform initialization | `New()` constructor | IRSA (default credential chain) + `sts:GetCallerIdentity` validation | Workload Identity | Workload Identity |
| Provider ID → instance ID + AZ | `RouterNode.ProviderID` | `aws:///zone/instance` | `gce:///project/zone/instance` | `azure:///...` |
| Endpoint discovery | `DiscoverEndpoints` | DescribeRouteServers + DescribeRouteServerEndpoints + DescribeSubnets | Cloud Router interface listing | Azure Route Server IP config |
| Peer reconciliation | `ReconcileNodes` — peering | VPC Route Server peers | Cloud Router peers | Azure Route Server peers |
| Forwarding fix | `ReconcileNodes` — forwarding | SourceDestCheck=false | canIpForward=true | IP forwarding=enabled |

## Test Plans

| Scope | Test plan | Status |
|:---|:---|:---|
| Platform-independent (controllers + helpers) | [docs/controller-test-plan.md](controller-test-plan.md) | Active |
| AWS | [docs/aws-integration-test-plan.md](aws-integration-test-plan.md) | Active |
| GCP | `docs/gcp-integration-test-plan.md` | Future |
| Azure | `docs/azure-integration-test-plan.md` | Future |

When adding a new provider, clone the AWS test plan and replace:

1. **Unit tests** — swap AWS API mocks for the new provider's API mocks. Peer reconciliation, forwarding fix, and provider ID parsing test cases map 1:1. Controller tests (mocked `CloudPlatform`) do not need to be duplicated — they are provider-agnostic.
2. **E2E tests** — same test categories (initial deployment, node lifecycle, self-healing, deletion). Swap AWS-specific verifications (Route Server peers, SourceDestCheck) for provider equivalents.

## OpenShift CI Pipeline

When the project moves to an OpenShift CI-managed repository, the following pipeline structure can be used:

| Layer | ci-operator type | Trigger |
|:---|:---|:---|
| Unit | Container test | Every PR (presubmit) |
| E2E | Container test + cluster + cloud credentials | On demand (`/test e2e-aws <profile>`) |

The E2E job requires IRSA configured for the operator's ServiceAccount and a profile name specifying which CRs to apply.
