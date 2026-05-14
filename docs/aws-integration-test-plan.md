# AWS Test Plan

Automated test plan for the CUDN BGP Routing Operator's AWS platform integration.

- [Test Configuration](#test-configuration)
- [Unit Tests (Mocked EC2)](#unit-tests-mocked-ec2)
- [E2E Tests (ROSA HCP Cluster)](#e2e-tests-rosa-hcp-cluster)
- [How to Run](#how-to-run)

## Test Configuration

Based on the rosa-bgp PoC configuration:

| Field | Value |
|:---|:---|
| Region | us-east-1 |
| Availability Zones | us-east-1a, us-east-1b, us-east-1c |
| Local BGP ASN | 65001 |
| Remote BGP ASN | 64512 (Route Server ASN) |
| Route Server endpoints | 2 per AZ (6 total) |
| Liveness detection | bfd |
| BGP router nodes | 1 per AZ (3 total), labeled `bgp_router: "true"` |
| CUDN network | name=`prod`, CIDR from Terraform outputs |

Unit tests hardcode these values with mocked EC2/STS clients — no credentials or cluster required.

E2E tests read CR manifests from a profile directory (`test/e2e/manifests/<profile>/`). The `poc` profile ships with the above configuration. The test framework derives expected state from the applied CRs and cluster nodes — no topology is hardcoded in the test code.

| Component | How discovered |
|:---|:---|
| Region, endpoints, ASN | From `CUDNBgpConfig` CR in the profile |
| Router nodes + AZs | Listed from cluster using CR's nodeSelector |
| AWS credentials | From the Secret referenced by the CR |
| Infrastructure | Provisioned externally (e.g., [rosa-bgp Terraform](https://github.com/msemanrh/rosa-bgp)) |

---

## Unit Tests (Mocked EC2)

Test the AWS platform package in isolation using a mocked EC2 client interface. No AWS credentials or cluster required.

### Credential Validation

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-AWS-01 | Valid credentials | Mock STS returns success | Platform created successfully |
| UT-AWS-02 | Invalid credentials | Empty key, empty secret, or STS auth error | CredentialError returned |

### Provider ID → Instance ID + AZ

| ID | Test Case | Input | Expected Result |
|:---|:---|:---|:---|
| UT-AWS-03 | Valid provider ID | `aws:///us-east-1a/i-0abc123` | instanceID=`i-0abc123`, az=`us-east-1a` |
| UT-AWS-04 | Invalid provider IDs (defensive test only) | `gce:///zone/instance`, `aws:///us-east-1a/`, `aws:////i-0abc123`, `aws://something` | Error for each |

### Route Server Peer Reconciliation

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-AWS-05 | Multi-AZ create | Empty peer list, nodes across 3 AZs | Peers created only on correct AZ endpoints, correct ASN, tagged `managed-by: cudn-bgp-routing-operator/<infrastructureName>` |
| UT-AWS-06 | Adopt pre-existing untagged peer | Untagged peer exists with same IP as a desired node | No create call; peer adopted via CreateTags with `managed-by: cudn-bgp-routing-operator/<infrastructureName>` |
| UT-AWS-07 | Delete stale peer | 1 managed peer, no desired nodes | Peer deleted, no create calls |
| UT-AWS-08 | BFD liveness detection | livenessDetection=bfd | CreateRouteServerPeer includes BFD peer liveness mode |
| UT-AWS-09 | Cleanup deletes all managed peers | 6 managed peers across 3 AZs | All 6 deleted |

### SourceDestCheck

| ID | Test Case | Setup | Expected Result |
|:---|:---|:---|:---|
| UT-AWS-10 | Disable on node with check enabled | SourceDestCheck=true on primary ENI | ModifyNetworkInterfaceAttribute called with false |
| UT-AWS-11 | No-op when already disabled | SourceDestCheck=false on primary ENI | No modify call |
| UT-AWS-12 | No primary ENI found (defensive test only) | Instance with no device index 0 | Error returned |

---

## E2E Tests (ROSA HCP Cluster)

Full end-to-end tests running the operator on a ROSA HCP cluster with VPC Route Server infrastructure. Validates the complete reconciliation loop from CR creation through AWS resource state.

**Prerequisites:** ROSA HCP cluster provisioned by rosa-bgp Terraform, AWS credentials secret created.

### Initial Deployment

| ID | Test Case | Action | Verification |
|:---|:---|:---|:---|
| E2E-AWS-01 | Full stack reconcile | Deploy operator, apply CUDNBgpConfig and CUDNBgpRouting CRs | Operator Running; config phase=Ready; 3 FRRConfigurations created; Route Server peers exist per AZ; SourceDestCheck=false on router nodes; routing phase=Ready with namespace + CUDN + RouteAdvertisements; FRR pods show established BGP sessions |

### Node Lifecycle

| ID | Test Case | Action | Verification |
|:---|:---|:---|:---|
| E2E-AWS-02 | Node-to-peer consistency | Record current router nodes and managed peers | Every router node IP has a corresponding Route Server peer; SourceDestCheck=false on all router nodes |

### Self-Healing and Drift Recovery

| ID | Test Case | Action | Verification |
|:---|:---|:---|:---|
| E2E-AWS-03 | Route Server peer manually deleted | Delete a managed peer via AWS CLI | Operator recreates it within 5 minutes |
| E2E-AWS-04 | SourceDestCheck manually re-enabled | Enable SourceDestCheck on a router node ENI | Operator disables it within 5 minutes |

### Deletion and Cleanup

| ID | Test Case | Action | Verification |
|:---|:---|:---|:---|
| E2E-AWS-05 | Full cleanup lifecycle | Delete config (blocked by routing CR → requeues) → delete routing CR → delete config CR | AWS peers deleted, FRRConfigurations deleted, finalizer removed; Network patch intentionally left in place |

---

## How to Run

### Unit Tests

No credentials or cluster required.

```bash
# All unit tests (controllers + AWS platform package)
make test && make test-aws

# AWS platform package only
make test-aws
```

### E2E Tests

Full operator lifecycle on a ROSA HCP cluster with VPC Route Server infrastructure.

```bash
# Prerequisites:
# - oc login to ROSA HCP cluster
# - AWS credentials secret created in openshift-cudn-bgp-routing namespace
# - Infrastructure provisioned (Terraform or equivalent)

# Profile is mandatory — specifies which CRs to apply
make test-e2e-aws poc
make test-e2e-aws my-cluster
```

Profiles are directories under `test/e2e/manifests/` containing `cudnbgpconfig.yaml` and `cudnbgprouting.yaml`. To test your own ROSA cluster, create a profile directory with your CRs and run `make test-e2e-aws <profile-name>`.
