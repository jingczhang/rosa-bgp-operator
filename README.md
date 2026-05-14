# CUDN BGP Routing Operator

Kubernetes operator for OpenShift that automates L3 direct routing between CUDN Pod networks and external networks via BGP. Replaces the manual in-cluster steps from the [rosa-bgp PoC](https://github.com/msemanrh/rosa-bgp).

The operator is **cloud platform aware but does not depend on it**. Core BGP and CUDN functionality works on any OpenShift cluster with external BGP servers. When platform configuration is provided (e.g. AWS), the operator additionally manages cloud-side networking resources (e.g. AWS VPC Route Server peers, SourceDestCheck) to keep BGP peering and traffic forwarding current for node changes.

## Table of Contents

- [Architecture](#architecture)
- [Cloud Platform Integration](#cloud-platform-integration)
- [Custom Resource Definitions](#custom-resource-definitions)
- [Controller Reconciliation](#controller-reconciliation)
- [Future Enhancements](#future-enhancements)
- [Development](#development)
- [Automated Testing](#automated-testing)

---

## Architecture

The overall solution (e.g. AWS) has two layers. The operator manages the in-cluster layer and, when platform integration is configured, also reconciles cloud-side networking resources.

```
┌──────────────────────────────────────────────────────────────────────┐
│  Cloud Infrastructure (Terraform provisions once)                    │
│                                                                      │
│  VPC / subnets / route server / TGW / etc.                           │
│  Terraform outputs: RS endpoint IPs, RS ASN, local BGP ASN,          │
│                     Route Server endpoint IDs (for platform section) │
└──────────────────────────────────┬───────────────────────────────────┘
                                   │ user copies outputs into CR spec
                                   ▼
┌──────────────────────────────────────────────────────────────────────┐
│  In-Cluster (Operator)                                               │
│                                                                      │
│  CUDNBgpConfig CR (singleton — BGP infra)                            │
│  ├── Patch Network.operator.openshift.io (enable FRR)                │
│  ├── FRRConfiguration per AZ (BGP sessions to local RS endpoints)    │
│  └── [if platform configured] Reconcile cloud networking on          │
│       node changes (Route Server peers, SourceDestCheck, etc.)       │
│                                                                      │
│  CUDNBgpRouting CR (one per application project)                     │
│  ├── ClusterUserDefinedNetwork + Namespace                           │
│  └── Shared RouteAdvertisements (all CUDNs with advertise=true)      │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Cloud Platform Integration

BGP peering between OCP worker nodes and a cloud BGP service (e.g. AWS VPC Route Server) requires configuration on **both sides**:

- **OCP side:** BGP-enabled worker nodes *initiate* BGP sessions toward the cloud BGP service endpoints.
- **Cloud side:** The cloud BGP service must be told to *accept* sessions from each node's IP and ASN.

When a BGP-enabled worker node is replaced (upgrade, spot termination, scaling), two things break at the cloud layer:

1. **Stale BGP peers** — the cloud BGP service still has a peer registered for the old node's IP. The new node cannot establish a session because it has no peer registration.
2. **Forwarding rules revert** — the new node's network interface defaults to dropping forwarded traffic (e.g. AWS `SourceDestCheck=true`), silently breaking all CUDN pod traffic through that node.

Without platform integration, these require manual intervention (e.g. re-running `terraform apply`). With platform integration, the operator creates and reconciles the cloud-side BGP peering and traffic forwarding for node changes.

Kubernetes has out-of-tree cloud controller managers (e.g. [cloud-provider-aws](https://github.com/kubernetes/cloud-provider-aws)) that implement a broad `cloudprovider.Interface`. This operator uses platform API (e.g. `aws-sdk-go-v2`) directly instead because it only needs a narrow slice of platform functionality (Route Server peers + SourceDestCheck). Importing a full cloud controller manager would bring a large dependency graph for minimal benefit. The architectural pattern (interface-based, per-provider package) is the same.

### AWS Platform

When AWS platform integration is configured, the operator performs two additional actions during each `CUDNBgpConfig` reconciliation (Phase 4):

| Action | AWS API calls | Trigger |
|:---|:---|:---|
| Verify credentials | `sts:GetCallerIdentity` | Every reconcile (before any EC2 calls) |
| Reconcile Route Server peers | `DescribeRouteServerPeers`, `CreateRouteServerPeer`, `DeleteRouteServerPeer`, `CreateTags` | BGP-enabled worker node added, removed, or IP changed |
| Disable SourceDestCheck | `DescribeInstances`, `ModifyNetworkInterfaceAttribute` | New BGP-enabled worker node detected |

Route Server peers are created **per-AZ** — each BGP-enabled worker node is peered with its local AZ's Route Server endpoints. Peers are tagged with `managed-by: cudn-bgp-routing-operator/<infrastructureName>` for lifecycle management, where `<infrastructureName>` is read automatically from the OpenShift `Infrastructure/cluster` object (`status.infrastructureName`). This cluster-scoped tag ensures multiple clusters sharing the same VPC Route Server do not interfere with each other's peers. If a peer already exists at a desired IP but was not created by the operator (e.g. created manually or by Terraform), the operator adopts it by adding the `managed-by` tag rather than attempting to create a duplicate.

### Multi-cloud extensibility

Currently only AWS platform integration is implemented but the design allows for additional providers. Each cloud maps to equivalent concepts:

| Concern | AWS | GCP (future) | Azure (future) |
|:---|:---|:---|:---|
| BGP peering | VPC Route Server peers | Cloud Router peers | Azure Route Server peers |
| Forwarding fix | SourceDestCheck=false | canIpForward=true | IP forwarding=enabled |
| Identity | IRSA / Pod Identity | Workload Identity | Workload Identity |

### What the operator replaces (PoC steps 6-7)

| Capability | PoC (current) | Operator (GA) |
|:---|:---|:---|
| Enable FRR + routeAdvertisements | `oc patch Network.operator.openshift.io` (manual) | Controller patches Network CR on reconcile |
| Wait for FRR readiness | `sleep 60`, retry on error | Controller polls FRR namespace and pods, requeues every 10s until ready |
| FRR BGP configuration | Single `FRRConfiguration` for all nodes — cross-AZ sessions fail | One `FRRConfiguration` per AZ, peers only with local RS endpoints |
| Peer liveness detection | `bgp-keepalive` only | BFD is supported (`livenessDetection: bfd`), `bgp-keepalive` as default |
| Namespace + CUDN | `oc apply -f yamls/` (manual) | Controller creates Namespace + ClusterUserDefinedNetwork per CR |
| RouteAdvertisements | `oc apply -f yamls/` (manual) | Controller ensures a single shared RouteAdvertisements (selecting all CUDNs with `advertise: "true"`) |
| Route Server peers | Terraform creates all peers statically | Controller watches BGP-enabled worker nodes, creates per-AZ peers dynamically |
| Source/dest check | Terraform disables on provisioned nodes | Controller disables on each BGP-enabled worker node's primary ENI |

### OLM / OperatorHub Packaging

| Field | Value |
|:---|:---|
| Package name | `cudn-bgp-routing-operator` |
| Default channel | `alpha` (moves to `stable` at GA) |
| Install modes | OwnNamespace, SingleNamespace |
| Target namespace | `openshift-cudn-bgp-routing` |
| Min OCP version | 4.18 (frr-k8s + CUDN support) |
| Categories | Networking |
| Provider | Red Hat |

**Required APIs:** `FRRConfiguration` (frrk8s.metallb.io/v1beta1), `ClusterUserDefinedNetwork` (k8s.ovn.org/v1)
**Owned APIs:** `CUDNBgpConfig`, `CUDNBgpRouting` (networking.openshift.io/v1alpha1)

---

## Custom Resource Definitions

Two CRDs with clear separation of concerns:

- **`CUDNBgpConfig`** (singleton, cluster-scoped, **must be named `cluster`**) — shared BGP infrastructure. Owned by cluster admin. Created once from `terraform output`.
- **`CUDNBgpRouting`** (one per CUDN, cluster-scoped) — declares a single CUDN network. Owned by application teams.

### CUDNBgpConfig (singleton - using PoC configuration)

```yaml
apiVersion: networking.openshift.io/v1alpha1
kind: CUDNBgpConfig
metadata:
  name: cluster
spec:
  routerNodeSelector:
    bgp_router: "true"

  bgp:
    localASN: 65001                  # terraform output rosa_bgp_asn
    livenessDetection: bgp-keepalive # bfd | bgp-keepalive (default)
    availabilityZones:
      - nodeSelector:
          topology.kubernetes.io/zone: us-east-1a
          bgp_router_subnet: "1"
        neighbors:
          - address: 10.0.1.47       # terraform output vpc1-rs1-subnet1-ep1_ip
            remoteASN: 64512         # terraform output vpc1-rs1-asn
          - address: 10.0.1.183      # terraform output vpc1-rs1-subnet1-ep2_ip
            remoteASN: 64512
      - nodeSelector:
          topology.kubernetes.io/zone: us-east-1b
          bgp_router_subnet: "2"
        neighbors:
          - address: 10.0.2.91       # terraform output vpc1-rs1-subnet2-ep1_ip
            remoteASN: 64512
          - address: 10.0.2.204      # terraform output vpc1-rs1-subnet2-ep2_ip
            remoteASN: 64512
      - nodeSelector:
          topology.kubernetes.io/zone: us-east-1c
          bgp_router_subnet: "3"
        neighbors:
          - address: 10.0.3.62       # terraform output vpc1-rs1-subnet3-ep1_ip
            remoteASN: 64512
          - address: 10.0.3.178      # terraform output vpc1-rs1-subnet3-ep2_ip
            remoteASN: 64512

  aws:
    region: us-east-1                # terraform output aws_region
    credentialsSecret:
      name: cudn-bgp-aws-creds      # Secret with AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
      namespace: openshift-cudn-bgp-routing
    routeServerEndpoints:
      - availabilityZone: us-east-1a
        endpointIDs:                 # terraform output vpc1-rs1-subnet1-endpoint_ids
          - rse-0abc1111
          - rse-0abc2222
      - availabilityZone: us-east-1b
        endpointIDs:                 # terraform output vpc1-rs1-subnet2-endpoint_ids
          - rse-0def3333
          - rse-0def4444
      - availabilityZone: us-east-1c
        endpointIDs:                 # terraform output vpc1-rs1-subnet3-endpoint_ids
          - rse-0ghi5555
          - rse-0ghi6666
```

### CUDNBgpRouting (one per application project - using PoC configuration)

```yaml
apiVersion: networking.openshift.io/v1alpha1
kind: CUDNBgpRouting
metadata:
  name: prod
spec:
  network:
    name: prod                       # matches PoC ClusterUserDefinedNetwork "cluster-udn-prod"
    subnet: 10.100.0.0/16
```

### CRD field reference

**CUDNBgpConfig** (cluster admin creates once):

| Field | Required | Description |
|:---|:---|:---|
| `spec.bgp.localASN` | Yes | AS number for the OCP FRR routers. From `terraform output rosa_bgp_asn`. |
| `spec.bgp.livenessDetection` | No | `bfd` or `bgp-keepalive` (default). Applies to all neighbors. BFD detects peer failure in ~1s (300ms interval × 3 multiplier). BGP keepalive detects peer failure in ~90s (default hold time). Each AZ has 2 RS endpoints, so failover on a single peer failure is automatic. |
| `spec.bgp.availabilityZones[]` | Yes | Per-AZ BGP peering groups. One entry per AZ. |
| `spec.bgp.availabilityZones[].nodeSelector` | Yes | Labels selecting BGP-enabled worker nodes in this AZ (e.g. `bgp_router_subnet: "1"`). |
| `spec.bgp.availabilityZones[].neighbors[]` | Yes | BGP neighbor IPs in this AZ's subnet. From `terraform output`. |
| `spec.routerNodeSelector` | Yes | Cluster-wide label selector for all BGP-enabled worker nodes (e.g. `bgp_router: "true"`). |
| `spec.aws.region` | If `spec.aws` set | AWS region where the ROSA cluster and Route Server are deployed. |
| `spec.aws.credentialsSecret.name` | If `spec.aws` set | Name of a Secret containing `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`. |
| `spec.aws.credentialsSecret.namespace` | If `spec.aws` set | Namespace of the credentials Secret. |
| `spec.aws.routeServerEndpoints[]` | If `spec.aws` set | Per-AZ Route Server endpoint IDs for peer lifecycle management. |
| `spec.aws.routeServerEndpoints[].availabilityZone` | If `spec.aws` set | AZ name (must match a `topology.kubernetes.io/zone` value in `spec.bgp.availabilityZones`). |
| `spec.aws.routeServerEndpoints[].endpointIDs` | If `spec.aws` set | Route Server endpoint IDs in this AZ. From `terraform output`. |

#### AWS credentials Secret

The Secret referenced by `spec.aws.credentialsSecret` must be `Opaque` with two data keys:

| Key | Value |
|:---|:---|
| `AWS_ACCESS_KEY_ID` | AWS access key ID |
| `AWS_SECRET_ACCESS_KEY` | AWS secret access key |

**Step 1 — Create an IAM user and policy in AWS:**

```bash
aws iam create-user --user-name cudn-bgp-operator

aws iam put-user-policy --user-name cudn-bgp-operator \
  --policy-name cudn-bgp-operator-policy \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [
      {
        "Effect": "Allow",
        "Action": [
          "sts:GetCallerIdentity",
          "ec2:DescribeRouteServerPeers",
          "ec2:CreateRouteServerPeer",
          "ec2:DeleteRouteServerPeer",
          "ec2:CreateTags",
          "ec2:DescribeInstances",
          "ec2:ModifyNetworkInterfaceAttribute"
        ],
        "Resource": "*"
      }
    ]
  }'

aws iam create-access-key --user-name cudn-bgp-operator
```

The `create-access-key` command outputs `AccessKeyId` and `SecretAccessKey`. Save them — the secret key is only shown once.

**Step 2 — Create the Secret in OCP:**

```bash
oc create secret generic cudn-bgp-aws-creds \
  -n openshift-cudn-bgp-routing \
  --from-literal=AWS_ACCESS_KEY_ID=AKIA... \
  --from-literal=AWS_SECRET_ACCESS_KEY=wJalr...
```

**CUDNBgpRouting** (application teams create per project):

| Field | Required | Description |
|:---|:---|:---|
| `spec.network.name` | Yes | Name for the CUDN and its namespace. |
| `spec.network.subnet` | Yes | CIDR for the CUDN pod network. The operator hardcodes `topology: Layer2`, `role: Primary`, and `ipam.lifecycle: Persistent` on the generated CUDN. |

### Operator-generated resources (from sample inputs above)

The following resources are what the operator produces from the sample `CUDNBgpConfig` and `CUDNBgpRouting` CRs shown above.

#### From CUDNBgpConfig — Network operator patch

```yaml
# Merge-patch applied to Network.operator.openshift.io/cluster
spec:
  additionalRoutingCapabilities:
    providers:
      - FRR
  defaultNetwork:
    ovnKubernetesConfig:
      routeAdvertisements: Enabled
```

#### From CUDNBgpConfig — FRRConfiguration (one per AZ)

```yaml
apiVersion: frrk8s.metallb.io/v1beta1
kind: FRRConfiguration
metadata:
  name: cudn-bgp-az-1
  namespace: openshift-frr-k8s
  labels:
    app.kubernetes.io/managed-by: cudn-bgp-routing-operator
spec:
  nodeSelector:
    matchLabels:
      bgp_router: "true"
      topology.kubernetes.io/zone: us-east-1a
      bgp_router_subnet: "1"
  bgp:
    routers:
      - asn: 65001
        neighbors:
          - address: 10.0.1.47
            asn: 64512
            disableMP: true
            toReceive:
              allowed:
                mode: all
          - address: 10.0.1.183
            asn: 64512
            disableMP: true
            toReceive:
              allowed:
                mode: all
---
apiVersion: frrk8s.metallb.io/v1beta1
kind: FRRConfiguration
metadata:
  name: cudn-bgp-az-2
  namespace: openshift-frr-k8s
  labels:
    app.kubernetes.io/managed-by: cudn-bgp-routing-operator
spec:
  nodeSelector:
    matchLabels:
      bgp_router: "true"
      topology.kubernetes.io/zone: us-east-1b
      bgp_router_subnet: "2"
  bgp:
    routers:
      - asn: 65001
        neighbors:
          - address: 10.0.2.91
            asn: 64512
            disableMP: true
            toReceive:
              allowed:
                mode: all
          - address: 10.0.2.204
            asn: 64512
            disableMP: true
            toReceive:
              allowed:
                mode: all
---
apiVersion: frrk8s.metallb.io/v1beta1
kind: FRRConfiguration
metadata:
  name: cudn-bgp-az-3
  namespace: openshift-frr-k8s
  labels:
    app.kubernetes.io/managed-by: cudn-bgp-routing-operator
spec:
  nodeSelector:
    matchLabels:
      bgp_router: "true"
      topology.kubernetes.io/zone: us-east-1c
      bgp_router_subnet: "3"
  bgp:
    routers:
      - asn: 65001
        neighbors:
          - address: 10.0.3.62
            asn: 64512
            disableMP: true
            toReceive:
              allowed:
                mode: all
          - address: 10.0.3.178
            asn: 64512
            disableMP: true
            toReceive:
              allowed:
                mode: all
```

#### From CUDNBgpRouting — Namespace

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: prod
  labels:
    k8s.ovn.org/primary-user-defined-network: ""
    cluster-udn: prod
    app.kubernetes.io/managed-by: cudn-bgp-routing-operator
```

#### From CUDNBgpRouting — ClusterUserDefinedNetwork

```yaml
apiVersion: k8s.ovn.org/v1
kind: ClusterUserDefinedNetwork
metadata:
  name: cluster-udn-prod
  labels:
    advertise: "true"
    app.kubernetes.io/managed-by: cudn-bgp-routing-operator
spec:
  namespaceSelector:
    matchLabels:
      cluster-udn: prod
  network:
    topology: Layer2
    layer2:
      role: Primary
      ipam:
        lifecycle: Persistent
      subnets:
        - 10.100.0.0/16
```

#### From CUDNBgpRouting — RouteAdvertisements (shared)

```yaml
apiVersion: k8s.ovn.org/v1
kind: RouteAdvertisements
metadata:
  name: cudn-bgp-route-advertisements
  labels:
    app.kubernetes.io/managed-by: cudn-bgp-routing-operator
spec:
  nodeSelector: {}
  frrConfigurationSelector: {}
  networkSelectors:
    - networkSelectionType: ClusterUserDefinedNetworks
      clusterUserDefinedNetworkSelector:
        networkSelector:
          matchLabels:
            advertise: "true"
  advertisements:
    - PodNetwork
```

---

## Controller Reconciliation

Two controllers, one per CRD.

### CUDNBgpConfig controller (singleton)

Reconciles the shared BGP infrastructure.

```
Phase 1: Patch Network Operator
  ├── Patch Network.operator.openshift.io/cluster to enable
  │   additionalRoutingCapabilities: [FRR] and routeAdvertisements: Enabled
  └── Condition: NetworkOperatorPatched
          │
          ▼
Phase 2: Wait for FRR
  ├── Watch for openshift-frr-k8s namespace to exist
  │   and frr-k8s pods to be running
  └── Condition: FRRNamespaceReady
          │
          ▼
Phase 3: Apply FRR Configuration (per AZ)
  ├── For each spec.bgp.availabilityZones[] entry, create/update
  │   a separate FRRConfiguration CR in openshift-frr-k8s:
  │     • nodeSelector: merge routerNodeSelector with AZ's nodeSelector
  │     • BGP router ASN: spec.bgp.localASN
  │     • Neighbors: only the RS endpoints in this AZ's subnet
  │     • Peer liveness detection: spec.bgp.livenessDetection
  │     • disableMP: true, toReceive.allowed.mode: all
  ├── Prune stale FRRConfigurations from previous reconciles
  │   (e.g. if AZ count was reduced)
  └── Condition: FRRConfigurationApplied
          │
          ▼
Phase 4: Reconcile AWS Resources
  ├── List Nodes matching spec.routerNodeSelector
  ├── For each node: read AZ from topology.kubernetes.io/zone label,
  │   private IP from status.addresses, instance ID from spec.providerID
  ├── Route Server peers: for each AZ, ensure peers exist on the
  │   AZ's routeServerEndpointIDs for the local BGP-enabled worker nodes only
  ├── Source/dest check: disable on the primary ENI of each node
  └── Condition: AWSResourcesReconciled
          │
          ▼
     phase: Ready
```

**On deletion:** blocked by finalizer while any `CUDNBgpRouting` CR exists. Once all routing CRs are removed, the finalizer cleans up all FRRConfigurations and deletes all managed AWS Route Server peers. The Network operator patch (`additionalRoutingCapabilities: FRR` and `ovnKubernetesConfig.routeAdvertisements: Enabled`) is intentionally **not** reverted — disabling FRR at the cluster level could disrupt other consumers.

### CUDNBgpRouting controller (per CUDN)

Reconciles individual CUDN networks. Before executing phases, the controller runs two pre-checks in order:

1. **Duplicate network name** — `spec.network.name` must be unique across all `CUDNBgpRouting` CRs. If another CR already claims the same name, the CR is immediately set to `Degraded` with reason `DuplicateNetwork`.
2. **Config readiness** — `CUDNBgpConfig` named `cluster` must exist and be in phase `Ready`. If missing or not yet `Ready`, the routing CR remains in `Pending` phase (conditions are cleared) and requeues every 10 seconds.

```
Phase 1: Create CUDN Resources
  ├── If Namespace exists: adopt it (ensure required labels present)
  │   If not: create Namespace with labels:
  │   k8s.ovn.org/primary-user-defined-network: ""
  │   cluster-udn: <spec.network.name>
  ├── Create ClusterUserDefinedNetwork with:
  │   namespaceSelector matching cluster-udn: <spec.network.name>
  │   subnet from spec.network
  │   topology: Layer2, role: Primary, ipam.lifecycle: Persistent (hardcoded)
  │   label advertise: "true"
  └── Condition: CUDNCreated
          │
          ▼
Phase 2: Ensure Route Advertisements
  ├── Ensure a single shared RouteAdvertisements ("cudn-bgp-route-advertisements") exists
  │   (created on first CUDNBgpRouting reconcile, reused by all)
  │   networkSelector: advertise=true (matches all operator-managed CUDNs)
  ├── advertisements: [PodNetwork]
  └── Condition: RouteAdvertisementsCreated
          │
          ▼
     phase: Ready
```

**On deletion:** delete the owned ClusterUserDefinedNetwork. The shared RouteAdvertisements is deleted only when the last CUDNBgpRouting CR is removed. Namespace is left intact.

### Status phases and error handling

Both CRs follow the same lifecycle: `Pending` → `Configuring` → `Ready`, with `Degraded` on errors.

| Phase | Meaning |
|:---|:---|
| `Pending` | CR accepted but prerequisites not met (e.g. `CUDNBgpConfig` not yet `Ready`) |
| `Configuring` | Reconciliation in progress, phases executing |
| `Ready` | All phases completed successfully |
| `Degraded` | A phase failed — check `status.conditions` for details |

**CUDNBgpConfig conditions:**

| Condition | Degraded Reason | Cause |
|:---|:---|:---|
| `NetworkOperatorPatched` | `PatchFailed` | Failed to patch `Network.operator.openshift.io/cluster` |
| `FRRNamespaceReady` | `CheckFailed` | Error checking FRR readiness (distinct from simply waiting) |
| `FRRConfigurationApplied` | `ApplyFailed` | Failed to create/update one or more FRRConfigurations |
| `AWSResourcesReconciled` | `AWSCredentialsInvalid` | AWS credentials in the referenced Secret are missing or invalid (verified via `sts:GetCallerIdentity`) |
| `AWSResourcesReconciled` | `AWSReconcileFailed` | Failed to reconcile Route Server peers or disable source/dest check (includes insufficient IAM permissions for EC2 API calls) |

> Phase 2 also uses `FRRNamespaceReady=False` with reason `WaitingForFRR` when the FRR namespace or pods are not yet available. This is **not** `Degraded` — the CR stays in `Configuring` and requeues every 10 seconds.

**CUDNBgpRouting conditions:**

| Condition | Degraded Reason | Cause |
|:---|:---|:---|
| `CUDNCreated` | `DuplicateNetwork` | `spec.network.name` already claimed by another CUDNBgpRouting CR |
| `CUDNCreated` | `NamespaceFailed` | Failed to create or adopt the namespace |
| `CUDNCreated` | `CUDNFailed` | Failed to create/update the ClusterUserDefinedNetwork |
| `RouteAdvertisementsCreated` | `RAFailed` | Failed to ensure the shared RouteAdvertisements |

On any `Degraded` state, the controller automatically retries every 30 seconds. In the `Ready` state, both controllers re-reconcile every 5 minutes to self-heal configuration drift (e.g. if a downstream resource such as an FRRConfiguration, ClusterUserDefinedNetwork, or RouteAdvertisements is manually deleted or modified).

Inspect the failing condition for the root cause:

```bash
oc get cudnbgpconfig cluster -o jsonpath='{.status.conditions}' | jq .
oc get cudnbgprouting prod -o jsonpath='{.status.conditions}' | jq .
```

---

## Future Enhancements

### Cloud Pod Identity for credential management

The operator currently authenticates to cloud APIs using static credentials stored in a Kubernetes Secret (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`). This should be replaced with cloud-native pod identity mechanisms that provide short-lived, automatically rotated credentials:

| Cloud | Mechanism | Benefit |
|:---|:---|:---|
| AWS | EKS Pod Identity / IRSA | No long-lived keys; IAM role assumed via ServiceAccount annotation |
| GCP | Workload Identity Federation | GCP IAM credentials injected into pods automatically |
| Azure | Workload Identity | Azure AD tokens bound to Kubernetes ServiceAccounts |

ROSA HCP clusters already have OIDC pre-configured, making IRSA straightforward. EKS Pod Identity is the newer, simpler alternative that avoids OIDC configuration entirely. The migration path would add pod identity support first, then deprecate `spec.aws.credentialsSecret` in a subsequent release.

---

## Development

### Prerequisites

- Go 1.24+
- operator-sdk v1.42+
- `oc` CLI logged into an OCP 4.18+ cluster
- Podman (for image builds only)

### Clone the repo

```bash
git clone https://github.com/jingczhang/rosa-bgp-operator.git
cd rosa-bgp-operator
```

### Deploy and test

The operator can be tested on any OCP 4.18+ cluster with an external BGP router — either on premise or on ROSA HCP with AWS VPC Route Server.

1. `oc login` to the cluster and deploy:

```bash
IMG=$(oc registry info)/openshift-cudn-bgp-routing/operator:dev
make docker-build docker-push CONTAINER_TOOL=podman IMG=$IMG
make deploy IMG=$IMG
```

2. Create the CRs for your environment:

   **For ROSA HCP with AWS VPC Route Server:** provision AWS infrastructure first with [rosa-bgp Terraform](https://github.com/msemanrh/rosa-bgp), then adapt the sample CRs with values from `terraform output`:

   ```bash
   oc apply -f config/samples/networking_v1alpha1_cudnbgpconfig.yaml
   oc apply -f config/samples/networking_v1alpha1_cudnbgprouting.yaml
   ```

   **For on-premise with an external BGP router:** create the CRs with your BGP router's ASN, neighbor addresses, and node selectors. Omit the `spec.aws` section — no cloud integration is needed:

   ```bash
   oc apply -f your-cudnbgpconfig.yaml    # no spec.aws section
   oc apply -f your-cudnbgprouting.yaml
   ```

3. Verify:

```bash
oc get cudnbgpconfig cluster -o yaml   # phase: Ready
oc get cudnbgprouting prod -o yaml     # phase: Ready
oc get frrconfiguration -n openshift-frr-k8s
oc get clusteruserdefinednetwork
oc get routeadvertisements
```

4. Clean up (delete CRs first so finalizers can clean up AWS resources and FRRConfigurations):

```bash
oc delete cudnbgprouting --all
oc delete cudnbgpconfig cluster
make undeploy
```

## Automated Testing

| Target | Scope | Prerequisites |
|:---|:---|:---|
| `make test` | Platform-independent unit tests (controllers + helpers) | None |
| `make test-aws` | AWS platform unit tests (mocked EC2/STS) | None |
| `make test-e2e` | Shared E2E (operator deploys and starts) | Cluster |
| `make test-e2e-aws <profile>` | AWS E2E (full reconciliation lifecycle), profile required | Cluster + AWS credentials |

Provider-specific E2E tests read CR manifests from `test/e2e/manifests/<profile>/` and require a profile name (e.g., `make test-e2e-aws poc`). To test your own ROSA cluster, add a profile directory with your CRs.

For full details see [docs/test-strategy.md](docs/test-strategy.md).
