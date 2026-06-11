# Controller Test Plan

Platform-independent tests for the CUDN BGP Routing Operator controllers and helpers.

- [Test Configuration](#test-configuration)
- [Unit Tests](#unit-tests)
- [E2E Tests](#e2e-tests)
- [How to Run](#how-to-run)

---

## Test Configuration

Unit tests use hardcoded values based on the PoC configuration. Both modes (with and without cloud integration) share the same base values; the only difference is whether `spec.aws` is set and the platform is mocked.

**Shared values:**

| Field | Value |
|:---|:---|
| Local BGP ASN | 65001 |
| Remote BGP ASN | 64512 |
| Router node selector | `bgp_router: "true"` |
| Availability Zones | 1 (minimal for unit tests) |
| Neighbor addresses | `10.0.1.47`, `10.0.1.183` |

**Without cloud integration** (`newTestCUDNBgpConfig`): explicit `spec.bgp.availabilityZones` with the above neighbors. No `spec.aws`.

**With cloud integration** (`newTestCUDNBgpConfigWithAWS`): `spec.aws` set (region `us-east-1`, Route Server ID `rs-1`). The `CloudPlatform` is mocked to return a `DiscoveryResult` with 1 Route Server, 1 endpoint (`rse-001` in `us-east-1a`, address `10.0.1.47`, remote ASN `64512`).

---

## Unit Tests

These tests use a fake Kubernetes client and a mocked `CloudPlatform` interface. They never invoke real provider code.

- [Config Controller](#config-controller)
- [Routing Controller](#routing-controller)
- [Helpers](#helpers)

---

### Config Controller

Tests for `CUDNBgpConfigReconciler` covering Phases 1-5 and deletion lifecycle.

#### Reconciliation

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-01 | Full reconcile without cloud (Phases 1-2, 4) | Network CR exists, FRR namespace + pod running, explicit `bgp.availabilityZones` | Network patched, FRRConfigurations created from explicit neighbors, phase=Ready with 3 conditions (NetworkOperatorPatched, FRRNamespaceReady, FRRConfigurationApplied) |
| UT-02 | Full reconcile with cloud (Phases 1-5) | spec.aws set with routeServerIDs, mock platform with discovery | AWSEndpointsDiscovered=True, FRRConfigurations created from discovered neighbors, ReconcileNodes called, AWSResourcesReconciled=True, phase=Ready with 5 conditions |
| UT-03 | Phase 3 credential failure (IRSA) | Mock platform builder returns CredentialError | AWSEndpointsDiscovered=False, reason=AWSCredentialsInvalid, phase=Degraded |
| UT-04 | Phase 3 discovery failure | Mock platform discovery returns error | AWSEndpointsDiscovered=False, reason=AWSDiscoveryFailed, phase=Degraded, requeue 30s |
| UT-05 | Phase 5 failure | Mock platform ReconcileNodes returns error | AWSResourcesReconciled=False, reason=AWSReconcileFailed, phase=Degraded, requeue 30s |
| UT-06 | Node filtering | 5 nodes: 3 complete, 1 missing IP, 1 missing AZ | Only 3 RouterNodes passed to ReconcileNodes |

#### Deletion

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-07 | Delete blocked by routing CRs | CUDNBgpRouting CR exists | Finalizer retained, requeues every 10s |
| UT-08 | Delete successful | spec.aws set, mock platform, FRRConfiguration exists | Cleanup called, FRRConfigurations deleted, finalizer removed |

---

### Routing Controller

Tests for `CUDNBgpRoutingReconciler` covering pre-checks, Phases 1-2, and deletion lifecycle.

#### Pre-checks

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-09 | Duplicate network name | Another CUDNBgpRouting claims same spec.network.name | phase=Degraded, reason=DuplicateNetwork |

#### Reconciliation

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-10 | Full reconcile | Config Ready, labeled namespace pre-created | CUDN + RouteAdvertisements created, phase=Ready with 2 conditions |
| UT-10b | No labeled namespace | Config Ready, no namespace with required labels | phase=Degraded, reason=NamespaceNotReady |

#### Deletion

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-11 | Delete last removes RA | No other CUDNBgpRouting CRs | CUDN deleted, RouteAdvertisements deleted, finalizer removed |
| UT-12 | Delete keeps RA when others exist | Another CUDNBgpRouting CR exists | CUDN deleted, RouteAdvertisements retained |

---

### Helpers

Non-trivial helper logic tested in isolation. Simple CRUD helpers (create, delete) are covered implicitly by the controller tests above.

| ID | Test Case | Verifies |
|:---|:---|:---|
| UT-13 | ValidateNamespaceLabels found | Returns nil when namespace with required labels exists |
| UT-13b | ValidateNamespaceLabels not found | Returns error when no namespace has required labels |
| UT-14 | EnsureFRRConfigurations BFD | BFD profile added when livenessDetection=bfd |
| UT-15 | EnsureFRRConfigurations prunes stale | Stale managed configs deleted when AZ count reduced |
| UT-16 | EnsureFRRConfigurations keeps unmanaged | User-owned FRRConfigurations not pruned |

---

## E2E Tests

Shared E2E tests that validate the operator runs correctly on any OCP 4.18+ cluster. No cloud credentials or CRs required.

No provider-independent E2E tests are currently defined. The suite infrastructure exists at `test/e2e/` for future use.

---

## How to Run

```bash
# Unit tests (no cluster required)
make test

# Shared E2E tests (requires cluster, no CRs or cloud credentials needed)
make test-e2e
```
