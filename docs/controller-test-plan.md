# Controller Test Plan

Platform-independent tests for the CUDN BGP Routing Operator controllers and helpers.

- [Test Configuration](#test-configuration)
- [Unit Tests](#unit-tests)
- [E2E Tests](#e2e-tests)
- [How to Run](#how-to-run)

---

## Test Configuration

Unit tests use hardcoded values based on the PoC configuration:

| Field | Value |
|:---|:---|
| Cluster | ROSA HCP on AWS (OCP 4.18+) |
| Operator namespace | openshift-cudn-bgp-routing |
| Local BGP ASN | 65001 |
| Availability Zones | 1 (minimal for unit tests) |
| Router node selector | `bgp_router: "true"` |

---

## Unit Tests

These tests use a fake Kubernetes client and a mocked `CloudPlatform` interface. They never invoke real provider code.

- [Config Controller](#config-controller)
- [Routing Controller](#routing-controller)
- [Helpers](#helpers)

---

## Config Controller

Tests for `CUDNBgpConfigReconciler` covering Phases 1-4 and deletion lifecycle.

### Reconciliation

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-01 | Full reconcile (Phases 1-3) | Network CR exists, FRR namespace + pod running | Network patched, FRRConfigurations created, phase=Ready with 3 conditions (NetworkOperatorPatched, FRRNamespaceReady, FRRConfigurationApplied) |
| UT-02 | Phase 4 success (mocked CloudPlatform) | spec.aws set, mock platform, 1 router node | ReconcileNodes called, AWSResourcesReconciled=True, phase=Ready with 4 conditions (NetworkOperatorPatched, FRRNamespaceReady, FRRConfigurationApplied, AWSResourcesReconciled) |
| UT-03 | Phase 4 credential failure | Mock platform builder returns CredentialError | AWSResourcesReconciled=False, reason=AWSCredentialsInvalid, phase=Degraded |
| UT-04 | Phase 4 failure | Mock platform returns error | AWSResourcesReconciled=False, reason=AWSReconcileFailed, phase=Degraded, requeue 30s |
| UT-05 | Node filtering | 5 nodes: 3 complete, 1 missing IP, 1 missing AZ | Only 3 RouterNodes passed to ReconcileNodes |

### Deletion

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-06 | Delete blocked by routing CRs | CUDNBgpRouting CR exists | Finalizer retained, requeues every 10s |
| UT-07 | Delete successful | spec.aws set, mock platform, FRRConfiguration exists | Cleanup called, FRRConfigurations deleted, finalizer removed |

---

## Routing Controller

Tests for `CUDNBgpRoutingReconciler` covering pre-checks, Phases 1-2, and deletion lifecycle.

### Pre-checks

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-08 | Duplicate network name | Another CUDNBgpRouting claims same spec.network.name | phase=Degraded, reason=DuplicateNetwork |

### Reconciliation

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-09 | Full reconcile | Config Ready | Namespace + CUDN + RouteAdvertisements created, phase=Ready with 2 conditions |

### Deletion

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-10 | Delete last removes RA | No other CUDNBgpRouting CRs | CUDN deleted, RouteAdvertisements deleted, finalizer removed |
| UT-11 | Delete keeps RA when others exist | Another CUDNBgpRouting CR exists | CUDN deleted, RouteAdvertisements retained |

---

## Helpers

Non-trivial helper logic tested in isolation. Simple CRUD helpers (create, delete) are covered implicitly by the controller tests above.

| ID | Test Case | Verifies |
|:---|:---|:---|
| UT-12 | EnsureNamespace adopts existing | Adds required labels without removing existing ones |
| UT-13 | EnsureFRRConfigurations BFD | BFD profile added when livenessDetection=bfd |
| UT-14 | EnsureFRRConfigurations prunes stale | Stale managed configs deleted when AZ count reduced |
| UT-15 | EnsureFRRConfigurations keeps unmanaged | User-owned FRRConfigurations not pruned |

---

## E2E Tests

Shared E2E tests that validate the operator runs correctly on any OCP 4.18+ cluster. No cloud credentials or CRs required.

| ID | Test Case | Action | Verification |
|:---|:---|:---|:---|
| E2E-01 | Operator pod starts | Deploy operator via `make deploy` | Pod Running in `openshift-cudn-bgp-routing` namespace |

---

## How to Run

```bash
# Unit tests (no cluster required)
make test

# Shared E2E tests (requires cluster, no CRs or cloud credentials needed)
make test-e2e
```
