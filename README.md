# CUDN BGP Routing Operator

Kubernetes operator for OpenShift (ROSA HCP) that automates L3 direct routing between CUDN Pod networks and AWS VPC networks via BGP. Replaces the manual in-cluster steps from the [rosa-bgp PoC](https://github.com/msemanrh/rosa-bgp).

## Table of Contents

- [Architecture](#architecture)
- [Custom Resource Definitions](#custom-resource-definitions)
- [Controller Reconciliation](#controller-reconciliation)
- [Future Enhancements](#future-enhancements)
- [Development](#development)

---

## Architecture

The overall solution has two layers. This operator replaces the manual in-cluster layer only.

```
┌──────────────────────────────────────────────────────────────────────┐
│  AWS Infrastructure (Terraform)                                      │
│                                                                      │
│  VPC1 (ROSA) ─── TGW ─── VPC2 (External)                             │
│  ├── 3 private + 3 public subnets per VPC                            │
│  ├── ROSA HCP cluster + machine pools (bgp_router=true)              │
│  ├── VPC Route Server + 6 endpoints (2 per AZ)                       │
│  ├── Route Server peers (one per router node)                        │
│  ├── Disable src/dst check on bgp_router nodes                       │
│  └── NAT GW, IGW, TGW attachments, route propagation                 │
│                                                                      │
│  Terraform outputs: RS endpoint IPs, RS ASN, local BGP ASN           │
└──────────────────────────────────┬───────────────────────────────────┘
                                   │ user copies IPs into CR spec
                                   ▼
┌──────────────────────────────────────────────────────────────────────┐
│  In-Cluster (Operator)                                               │
│                                                                      │
│  CUDNBgpConfig CR (singleton — BGP infra)                            │
│  ├── Patch Network.operator.openshift.io (enable FRR)                │
│  └── FRRConfiguration per AZ (BGP sessions to local RS endpoints)    │
│                                                                      │
│  CUDNBgpRouting CR (one per application project)                     │
│  ├── ClusterUserDefinedNetwork + Namespace                           │
│  └── Shared RouteAdvertisements (all CUDNs with advertise=true)      │
└──────────────────────────────────────────────────────────────────────┘
```

### What the operator replaces (PoC steps 6-7)

| Capability | PoC (current) | Operator (GA) |
|:---|:---|:---|
| Enable FRR + routeAdvertisements | `oc patch Network.operator.openshift.io` (manual) | Controller patches Network CR on reconcile |
| Wait for FRR readiness | `sleep 60`, retry on error | Controller watches `openshift-frr-k8s` namespace |
| FRR BGP configuration | Single `FRRConfiguration` for all nodes — cross-AZ sessions fail | One `FRRConfiguration` per AZ, peers only with local RS endpoints |
| Peer liveness detection | `bgp-keepalive` only | BFD is supported (`livenessDetection: bfd`), `bgp-keepalive` as default |
| Namespace + CUDN | `oc apply -f yamls/` (manual) | Controller creates Namespace + ClusterUserDefinedNetwork per CR |
| RouteAdvertisements | `oc apply -f yamls/` (manual) | Controller ensures a single shared RouteAdvertisements (selecting all CUDNs with `advertise: "true"`) |

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

- **`CUDNBgpConfig`** (singleton, cluster-scoped) — shared BGP infrastructure. Owned by cluster admin. Created once from `terraform output`.
- **`CUDNBgpRouting`** (one per CUDN, cluster-scoped) — declares a single CUDN network. Owned by application teams.

### CUDNBgpConfig (singleton)

```yaml
apiVersion: networking.openshift.io/v1alpha1
kind: CUDNBgpConfig
metadata:
  name: cluster
spec:
  bgp:
    localASN: 65001                  # terraform output rosa_bgp_asn
    livenessDetection: bgp-keepalive # bfd | bgp-keepalive (default)
    availabilityZones:
      - nodeSelector:
          bgp_router_subnet: "1"
        neighbors:
          - address: 10.0.1.47       # terraform output vpc1-rs1-subnet1-ep1_ip
            remoteASN: 64512         # terraform output vpc1-rs1-asn
          - address: 10.0.1.183      # terraform output vpc1-rs1-subnet1-ep2_ip
            remoteASN: 64512
      - nodeSelector:
          bgp_router_subnet: "2"
        neighbors:
          - address: 10.0.2.91       # terraform output vpc1-rs1-subnet2-ep1_ip
            remoteASN: 64512
          - address: 10.0.2.204      # terraform output vpc1-rs1-subnet2-ep2_ip
            remoteASN: 64512
      - nodeSelector:
          bgp_router_subnet: "3"
        neighbors:
          - address: 10.0.3.62       # terraform output vpc1-rs1-subnet3-ep1_ip
            remoteASN: 64512
          - address: 10.0.3.178      # terraform output vpc1-rs1-subnet3-ep2_ip
            remoteASN: 64512

  routerNodeSelector:
    bgp_router: "true"
```

### CUDNBgpRouting (one per application project)

```yaml
apiVersion: networking.openshift.io/v1alpha1
kind: CUDNBgpRouting
metadata:
  name: prod
spec:
  network:
    name: prod                       # matches PoC ClusterUserDefinedNetwork "cluster-udn-prod"
    subnet: 10.100.0.0/16
    topology: Layer2
```

### CRD field reference

**CUDNBgpConfig** (cluster admin creates once):

| Field | Required | Description |
|:---|:---|:---|
| `spec.bgp.localASN` | Yes | AS number for the OCP FRR routers. From `terraform output rosa_bgp_asn` (default 65001). |
| `spec.bgp.livenessDetection` | No | `bfd` or `bgp-keepalive` (default). Applies to all neighbors. |
| `spec.bgp.availabilityZones[]` | Yes | Per-AZ BGP peering groups. One entry per AZ. |
| `spec.bgp.availabilityZones[].nodeSelector` | Yes | Labels selecting router nodes in this AZ (e.g. `bgp_router_subnet: "1"`). |
| `spec.bgp.availabilityZones[].neighbors[]` | Yes | RS endpoint IPs in this AZ's subnet. From `terraform output`. |
| `spec.routerNodeSelector` | Yes | Cluster-wide label selector for all router nodes (e.g. `bgp_router: "true"`). |

**CUDNBgpRouting** (application teams create per project):

| Field | Required | Description |
|:---|:---|:---|
| `spec.network.name` | Yes | Name for the CUDN and its namespace. |
| `spec.network.subnet` | Yes | CIDR for the CUDN pod network. The operator hardcodes `role: Primary` and `ipam.lifecycle: Persistent` on the generated CUDN (PodNetwork advertisements require a primary CUDN). |
| `spec.network.topology` | Yes | OVN topology: `Layer2` or `Layer3`. |

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
spec:
  nodeSelector:
    matchLabels:
      bgp_router: "true"
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
spec:
  nodeSelector:
    matchLabels:
      bgp_router: "true"
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
spec:
  nodeSelector:
    matchLabels:
      bgp_router: "true"
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
```

#### From CUDNBgpRouting — ClusterUserDefinedNetwork

```yaml
apiVersion: k8s.ovn.org/v1
kind: ClusterUserDefinedNetwork
metadata:
  name: cluster-udn-prod
  labels:
    advertise: "true"
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
  name: default
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
  │     • peer_liveness_detection: spec.bgp.livenessDetection
  │     • disableMP: true, toReceive.allowed.mode: all
  ├── Prune stale FRRConfigurations from previous reconciles
  │   (e.g. if AZ count was reduced)
  └── Condition: FRRConfigurationApplied
          │
          ▼
     phase: Ready
```

**On deletion:** blocked by finalizer while any `CUDNBgpRouting` CR exists. Once all routing CRs are removed, the finalizer cleans up all FRRConfigurations.

### CUDNBgpRouting controller (per CUDN)

Reconciles individual CUDN networks. Requires `CUDNBgpConfig` to be `Ready`.

```
Phase 1: Create CUDN Resources
  ├── If Namespace exists: adopt it (ensure required labels present)
  │   If not: create Namespace with labels:
  │   k8s.ovn.org/primary-user-defined-network: ""
  │   cluster-udn: <spec.network.name>
  ├── Create ClusterUserDefinedNetwork with:
  │   namespaceSelector matching cluster-udn: <spec.network.name>
  │   subnet, topology from spec.network
  │   role: Primary, ipam.lifecycle: Persistent (hardcoded)
  │   label advertise: "true"
  └── Condition: CUDNCreated
          │
          ▼
Phase 2: Ensure Route Advertisements
  ├── Ensure a single shared RouteAdvertisements ("default") exists
  │   (created on first CUDNBgpRouting reconcile, reused by all)
  │   networkSelector: advertise=true (matches all operator-managed CUDNs)
  ├── advertisements: [PodNetwork]
  └── Condition: RouteAdvertisementsCreated
          │
          ▼
     phase: Ready
```

**On deletion:** clean up owned CUDN in reverse order. The shared RouteAdvertisements is deleted only when the last CUDNBgpRouting CR is removed. Namespace is left intact.

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

> Phase 2 also uses `FRRNamespaceReady=False` with reason `WaitingForFRR` when the FRR namespace or pods are not yet available. This is **not** `Degraded` — the CR stays in `Configuring` and requeues every 10 seconds.

**CUDNBgpRouting conditions:**

| Condition | Degraded Reason | Cause |
|:---|:---|:---|
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

### BGP session health observability

The operator currently configures BFD or BGP keepalive on `FRRConfiguration` CRs but does not monitor BGP session state at runtime. Peer liveness detection and failover are handled entirely by the FRR daemon:

- **BFD**: detects peer failure in ~1s (300ms interval × 3 multiplier)
- **BGP keepalive**: detects peer failure in ~90s (default hold time)
- **Failover**: automatic — each AZ has 2 RS endpoints, so a single peer failure is transparent

A future enhancement could watch `FRRNodeState` CRs (published by frr-k8s with per-node BGP session status) and surface aggregate peer health in `CUDNBgpConfig` status conditions. This would give users a single pane of glass for BGP health via `oc get cudnbgpconfig cluster` without needing to inspect individual FRR pods.

---

## Development

### Prerequisites

- Go 1.23+
- operator-sdk v1.38+
- `oc` CLI logged into an OCP 4.18+ cluster
- Podman (for image builds only)

### Clone the repo

```bash
git clone https://github.com/jingczhang/rosa-bgp-operator.git
cd rosa-bgp-operator
```

### Deploy and test

The operator can be tested on any OCP 4.18+ cluster with an external BGP router — either on premise or on ROSA HCP with AWS VPC Route Server.

**For ROSA HCP:** provision AWS infrastructure first with [rosa-bgp Terraform](https://github.com/msemanrh/rosa-bgp).

1. `oc login` to the cluster and deploy:

```bash
IMG=$(oc registry info)/rosa-bgp-operator-system/operator:dev
make docker-build CONTAINER_TOOL=podman IMG=$IMG
podman push $IMG
make deploy IMG=$IMG
```

2. Adapt the sample CRs to fit your configuration and apply them:

```bash
oc apply -f config/samples/networking_v1alpha1_cudnbgpconfig.yaml
oc apply -f config/samples/networking_v1alpha1_cudnbgprouting.yaml
```

3. Verify:

```bash
oc get cudnbgpconfig cluster -o yaml   # phase: Ready
oc get cudnbgprouting prod -o yaml     # phase: Ready
oc get frrconfiguration -n openshift-frr-k8s
oc get clusteruserdefinednetwork
oc get routeadvertisements
```

4. Clean up:

```bash
make undeploy
```
