/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	networkingv1alpha1 "github.com/openshift/cudn-bgp-routing-operator/api/v1alpha1"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = networkingv1alpha1.AddToScheme(s)
	return s
}

// --- EnsureNamespace tests ---

func TestEnsureNamespace_Creates(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	ctx := context.Background()

	if err := EnsureNamespace(ctx, c, "prod"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ns := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: "prod"}, ns); err != nil {
		t.Fatalf("namespace not created: %v", err)
	}
	if ns.Labels[LabelPrimaryUDN] != "" {
		t.Errorf("expected empty primary UDN label, got %q", ns.Labels[LabelPrimaryUDN])
	}
	if ns.Labels[LabelCUDN] != "prod" {
		t.Errorf("expected CUDN label 'prod', got %q", ns.Labels[LabelCUDN])
	}
	if ns.Labels[LabelManagedBy] != LabelManagedByVal {
		t.Errorf("expected managed-by label, got %q", ns.Labels[LabelManagedBy])
	}
}

func TestEnsureNamespace_AdoptsExisting(t *testing.T) {
	existing := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "prod",
			Labels: map[string]string{"existing": "label"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(existing).Build()
	ctx := context.Background()

	if err := EnsureNamespace(ctx, c, "prod"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ns := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: "prod"}, ns); err != nil {
		t.Fatalf("namespace not found: %v", err)
	}
	if ns.Labels["existing"] != "label" {
		t.Error("existing label was removed")
	}
	if ns.Labels[LabelCUDN] != "prod" {
		t.Error("CUDN label not added during adoption")
	}
}

func TestEnsureNamespace_NoUpdateIfLabelsMatch(t *testing.T) {
	existing := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prod",
			Labels: map[string]string{
				LabelPrimaryUDN: "",
				LabelCUDN:       "prod",
				LabelManagedBy:  LabelManagedByVal,
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(existing).Build()
	ctx := context.Background()

	if err := EnsureNamespace(ctx, c, "prod"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- EnsureCUDN tests ---

func TestEnsureCUDN_CreatesLayer2(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(CUDNNetworkGVK.GroupVersion().WithKind("ClusterUserDefinedNetwork"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(CUDNNetworkGVK.GroupVersion().WithKind("ClusterUserDefinedNetworkList"), &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(s).Build()
	ctx := context.Background()

	routing := &networkingv1alpha1.CUDNBgpRouting{
		Spec: networkingv1alpha1.CUDNBgpRoutingSpec{
			Network: networkingv1alpha1.NetworkConfig{
				Name:     "prod",
				Subnet:   "10.100.0.0/16",
				Topology: networkingv1alpha1.TopologyLayer2,
			},
		},
	}

	if err := EnsureCUDN(ctx, c, routing); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(CUDNNetworkGVK)
	if err := c.Get(ctx, types.NamespacedName{Name: "cluster-udn-prod"}, obj); err != nil {
		t.Fatalf("CUDN not created: %v", err)
	}

	labels := obj.GetLabels()
	if labels[LabelAdvertise] != "true" {
		t.Errorf("expected advertise label, got %q", labels[LabelAdvertise])
	}

	network, _, _ := unstructured.NestedMap(obj.Object, "spec", "network", "layer2")
	if network["role"] != "Primary" {
		t.Errorf("expected role Primary, got %v", network["role"])
	}

	ipam, _, _ := unstructured.NestedMap(obj.Object, "spec", "network", "layer2", "ipam")
	if ipam["lifecycle"] != "Persistent" {
		t.Errorf("expected ipam lifecycle Persistent, got %v", ipam["lifecycle"])
	}
}

func TestEnsureCUDN_CreatesLayer3(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(CUDNNetworkGVK.GroupVersion().WithKind("ClusterUserDefinedNetwork"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(CUDNNetworkGVK.GroupVersion().WithKind("ClusterUserDefinedNetworkList"), &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(s).Build()
	ctx := context.Background()

	routing := &networkingv1alpha1.CUDNBgpRouting{
		Spec: networkingv1alpha1.CUDNBgpRoutingSpec{
			Network: networkingv1alpha1.NetworkConfig{
				Name:     "staging",
				Subnet:   "10.200.0.0/16",
				Topology: networkingv1alpha1.TopologyLayer3,
			},
		},
	}

	if err := EnsureCUDN(ctx, c, routing); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(CUDNNetworkGVK)
	if err := c.Get(ctx, types.NamespacedName{Name: "cluster-udn-staging"}, obj); err != nil {
		t.Fatalf("CUDN not created: %v", err)
	}

	topology, _, _ := unstructured.NestedString(obj.Object, "spec", "network", "topology")
	if topology != "Layer3" {
		t.Errorf("expected topology Layer3, got %s", topology)
	}

	network, _, _ := unstructured.NestedMap(obj.Object, "spec", "network", "layer3")
	if network == nil {
		t.Fatal("expected layer3 key in network spec")
	}
	if network["role"] != "Primary" {
		t.Errorf("expected role Primary, got %v", network["role"])
	}
}

func TestEnsureCUDN_UpdatesExisting(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(CUDNNetworkGVK.GroupVersion().WithKind("ClusterUserDefinedNetwork"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(CUDNNetworkGVK.GroupVersion().WithKind("ClusterUserDefinedNetworkList"), &unstructured.UnstructuredList{})

	existing := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.ovn.org/v1",
			"kind":       "ClusterUserDefinedNetwork",
			"metadata": map[string]interface{}{
				"name":   "cluster-udn-prod",
				"labels": map[string]interface{}{LabelAdvertise: "true", LabelManagedBy: LabelManagedByVal},
			},
			"spec": map[string]interface{}{
				"network": map[string]interface{}{"topology": "Layer2"},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(existing).Build()
	ctx := context.Background()

	routing := &networkingv1alpha1.CUDNBgpRouting{
		Spec: networkingv1alpha1.CUDNBgpRoutingSpec{
			Network: networkingv1alpha1.NetworkConfig{
				Name:     "prod",
				Subnet:   "10.100.0.0/16",
				Topology: networkingv1alpha1.TopologyLayer2,
			},
		},
	}

	if err := EnsureCUDN(ctx, c, routing); err != nil {
		t.Fatalf("unexpected error on update: %v", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(CUDNNetworkGVK)
	if err := c.Get(ctx, types.NamespacedName{Name: "cluster-udn-prod"}, obj); err != nil {
		t.Fatalf("CUDN not found after update: %v", err)
	}

	subnets, _, _ := unstructured.NestedSlice(obj.Object, "spec", "network", "layer2", "subnets")
	if len(subnets) != 1 || subnets[0] != "10.100.0.0/16" {
		t.Errorf("expected subnet [10.100.0.0/16], got %v", subnets)
	}
}

// --- EnsureRouteAdvertisements tests ---

func TestEnsureRouteAdvertisements_Creates(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(RouteAdvertisementsGVK.GroupVersion().WithKind("RouteAdvertisements"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(RouteAdvertisementsGVK.GroupVersion().WithKind("RouteAdvertisementsList"), &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(s).Build()
	ctx := context.Background()

	if err := EnsureRouteAdvertisements(ctx, c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(RouteAdvertisementsGVK)
	if err := c.Get(ctx, types.NamespacedName{Name: RouteAdvertisementName}, obj); err != nil {
		t.Fatalf("RouteAdvertisements not created: %v", err)
	}

	ads, _, _ := unstructured.NestedStringSlice(obj.Object, "spec", "advertisements")
	if len(ads) != 1 || ads[0] != "PodNetwork" {
		t.Errorf("expected [PodNetwork], got %v", ads)
	}
}

func TestEnsureRouteAdvertisements_Idempotent(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(RouteAdvertisementsGVK.GroupVersion().WithKind("RouteAdvertisements"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(RouteAdvertisementsGVK.GroupVersion().WithKind("RouteAdvertisementsList"), &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(s).Build()
	ctx := context.Background()

	if err := EnsureRouteAdvertisements(ctx, c); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if err := EnsureRouteAdvertisements(ctx, c); err != nil {
		t.Fatalf("second call failed (not idempotent): %v", err)
	}
}

func TestDeleteRouteAdvertisements(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(RouteAdvertisementsGVK.GroupVersion().WithKind("RouteAdvertisements"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(RouteAdvertisementsGVK.GroupVersion().WithKind("RouteAdvertisementsList"), &unstructured.UnstructuredList{})

	c := fake.NewClientBuilder().WithScheme(s).Build()
	ctx := context.Background()

	_ = EnsureRouteAdvertisements(ctx, c)
	if err := DeleteRouteAdvertisements(ctx, c); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Delete again should not error (idempotent)
	if err := DeleteRouteAdvertisements(ctx, c); err != nil {
		t.Fatalf("delete of non-existent failed: %v", err)
	}
}

// --- IsFRRReady tests ---

func TestIsFRRReady_NoNamespace(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	ctx := context.Background()

	ready, err := IsFRRReady(ctx, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected not ready when namespace doesn't exist")
	}
}

func TestIsFRRReady_NoPods(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: FRRNamespace}}
	c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(ns).Build()
	ctx := context.Background()

	ready, err := IsFRRReady(ctx, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected not ready when no pods exist")
	}
}

func TestIsFRRReady_PodRunning(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: FRRNamespace}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "frr-k8s-abc",
			Namespace: FRRNamespace,
			Labels:    map[string]string{"app": "frr-k8s"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(ns, pod).Build()
	ctx := context.Background()

	ready, err := IsFRRReady(ctx, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Error("expected ready when pod is running")
	}
}

func TestIsFRRReady_PodPending(t *testing.T) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: FRRNamespace}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "frr-k8s-abc",
			Namespace: FRRNamespace,
			Labels:    map[string]string{"app": "frr-k8s"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(ns, pod).Build()
	ctx := context.Background()

	ready, err := IsFRRReady(ctx, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready {
		t.Error("expected not ready when pod is pending")
	}
}

// --- EnsureFRRConfigurations tests ---

func TestEnsureFRRConfigurations_CreatesPerAZ(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfiguration"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfigurationList"), &unstructured.UnstructuredList{})

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: FRRNamespace}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(ns).Build()
	ctx := context.Background()

	config := &networkingv1alpha1.CUDNBgpConfig{
		Spec: networkingv1alpha1.CUDNBgpConfigSpec{
			BGP: networkingv1alpha1.BGPConfig{
				LocalASN:          65001,
				LivenessDetection: networkingv1alpha1.LivenessDetectionBGPKeepalive,
				AvailabilityZones: []networkingv1alpha1.AvailabilityZone{
					{
						NodeSelector: map[string]string{"bgp_router_subnet": "1"},
						Neighbors: []networkingv1alpha1.BGPNeighbor{
							{Address: "10.0.1.47", RemoteASN: 64512},
							{Address: "10.0.1.183", RemoteASN: 64512},
						},
					},
					{
						NodeSelector: map[string]string{"bgp_router_subnet": "2"},
						Neighbors: []networkingv1alpha1.BGPNeighbor{
							{Address: "10.0.2.91", RemoteASN: 64512},
						},
					},
				},
			},
			RouterNodeSelector: map[string]string{"bgp_router": "true"},
		},
	}

	if err := EnsureFRRConfigurations(ctx, c, config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, tc := range []struct {
		name   string
		subnet string
	}{
		{"cudn-bgp-az-1", "1"},
		{"cudn-bgp-az-2", "2"},
	} {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(FRRConfigurationGVK)
		if err := c.Get(ctx, types.NamespacedName{Name: tc.name, Namespace: FRRNamespace}, obj); err != nil {
			t.Errorf("FRRConfiguration %s not created: %v", tc.name, err)
			continue
		}

		labels, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "nodeSelector", "matchLabels")
		if labels["bgp_router"] != "true" {
			t.Errorf("%s: missing bgp_router label", tc.name)
		}
		if labels["bgp_router_subnet"] != tc.subnet {
			t.Errorf("%s: expected subnet %s, got %s", tc.name, tc.subnet, labels["bgp_router_subnet"])
		}
	}
}

func TestEnsureFRRConfigurations_BFDProfile(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfiguration"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfigurationList"), &unstructured.UnstructuredList{})

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: FRRNamespace}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(ns).Build()
	ctx := context.Background()

	config := &networkingv1alpha1.CUDNBgpConfig{
		Spec: networkingv1alpha1.CUDNBgpConfigSpec{
			BGP: networkingv1alpha1.BGPConfig{
				LocalASN:          65001,
				LivenessDetection: networkingv1alpha1.LivenessDetectionBFD,
				AvailabilityZones: []networkingv1alpha1.AvailabilityZone{
					{
						NodeSelector: map[string]string{"bgp_router_subnet": "1"},
						Neighbors: []networkingv1alpha1.BGPNeighbor{
							{Address: "10.0.1.47", RemoteASN: 64512},
						},
					},
				},
			},
			RouterNodeSelector: map[string]string{"bgp_router": "true"},
		},
	}

	if err := EnsureFRRConfigurations(ctx, c, config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(FRRConfigurationGVK)
	if err := c.Get(ctx, types.NamespacedName{Name: "cudn-bgp-az-1", Namespace: FRRNamespace}, obj); err != nil {
		t.Fatalf("FRRConfiguration not created: %v", err)
	}

	bfdProfiles, _, _ := unstructured.NestedSlice(obj.Object, "spec", "bgp", "bfdProfiles")
	if len(bfdProfiles) == 0 {
		t.Fatal("expected bfdProfiles when livenessDetection is bfd")
	}

	profile := bfdProfiles[0].(map[string]interface{})
	if profile["name"] != "default" {
		t.Errorf("expected bfd profile name 'default', got %v", profile["name"])
	}
}

func TestDeleteFRRConfigurations(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfiguration"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfigurationList"), &unstructured.UnstructuredList{})

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: FRRNamespace}}

	existing := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "frrk8s.metallb.io/v1beta1",
			"kind":       "FRRConfiguration",
			"metadata": map[string]interface{}{
				"name":      "cudn-bgp-az-1",
				"namespace": FRRNamespace,
				"labels": map[string]interface{}{
					LabelManagedBy: LabelManagedByVal,
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(ns, existing).Build()
	ctx := context.Background()

	if err := DeleteFRRConfigurations(ctx, c); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(FRRConfigurationGVK)
	_ = c.List(ctx, list)
	if len(list.Items) != 0 {
		t.Errorf("expected 0 FRRConfigurations after delete, got %d", len(list.Items))
	}
}

// --- EnsureFRRConfigurations pruning tests ---

func TestEnsureFRRConfigurations_PrunesStale(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfiguration"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfigurationList"), &unstructured.UnstructuredList{})

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: FRRNamespace}}

	stale := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "frrk8s.metallb.io/v1beta1",
			"kind":       "FRRConfiguration",
			"metadata": map[string]interface{}{
				"name":      "cudn-bgp-az-3",
				"namespace": FRRNamespace,
				"labels":    map[string]interface{}{LabelManagedBy: LabelManagedByVal},
			},
			"spec": map[string]interface{}{},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(ns, stale).Build()
	ctx := context.Background()

	config := &networkingv1alpha1.CUDNBgpConfig{
		Spec: networkingv1alpha1.CUDNBgpConfigSpec{
			BGP: networkingv1alpha1.BGPConfig{
				LocalASN:          65001,
				LivenessDetection: networkingv1alpha1.LivenessDetectionBGPKeepalive,
				AvailabilityZones: []networkingv1alpha1.AvailabilityZone{
					{
						NodeSelector: map[string]string{"bgp_router_subnet": "1"},
						Neighbors:    []networkingv1alpha1.BGPNeighbor{{Address: "10.0.1.47", RemoteASN: 64512}},
					},
				},
			},
			RouterNodeSelector: map[string]string{"bgp_router": "true"},
		},
	}

	if err := EnsureFRRConfigurations(ctx, c, config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// az-1 should exist
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(FRRConfigurationGVK)
	if err := c.Get(ctx, types.NamespacedName{Name: "cudn-bgp-az-1", Namespace: FRRNamespace}, obj); err != nil {
		t.Error("cudn-bgp-az-1 should exist")
	}

	// stale az-3 should be pruned
	staleObj := &unstructured.Unstructured{}
	staleObj.SetGroupVersionKind(FRRConfigurationGVK)
	err := c.Get(ctx, types.NamespacedName{Name: "cudn-bgp-az-3", Namespace: FRRNamespace}, staleObj)
	if err == nil {
		t.Error("cudn-bgp-az-3 should have been pruned")
	}
}

func TestEnsureFRRConfigurations_KeepsUnmanagedResources(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfiguration"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfigurationList"), &unstructured.UnstructuredList{})

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: FRRNamespace}}

	userOwned := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "frrk8s.metallb.io/v1beta1",
			"kind":       "FRRConfiguration",
			"metadata": map[string]interface{}{
				"name":      "user-custom-frr",
				"namespace": FRRNamespace,
				"labels":    map[string]interface{}{"owner": "user"},
			},
			"spec": map[string]interface{}{},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(ns, userOwned).Build()
	ctx := context.Background()

	config := &networkingv1alpha1.CUDNBgpConfig{
		Spec: networkingv1alpha1.CUDNBgpConfigSpec{
			BGP: networkingv1alpha1.BGPConfig{
				LocalASN:          65001,
				LivenessDetection: networkingv1alpha1.LivenessDetectionBGPKeepalive,
				AvailabilityZones: []networkingv1alpha1.AvailabilityZone{
					{
						NodeSelector: map[string]string{"bgp_router_subnet": "1"},
						Neighbors:    []networkingv1alpha1.BGPNeighbor{{Address: "10.0.1.47", RemoteASN: 64512}},
					},
				},
			},
			RouterNodeSelector: map[string]string{"bgp_router": "true"},
		},
	}

	if err := EnsureFRRConfigurations(ctx, c, config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// User-owned FRRConfiguration must not be deleted
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(FRRConfigurationGVK)
	if err := c.Get(ctx, types.NamespacedName{Name: "user-custom-frr", Namespace: FRRNamespace}, obj); err != nil {
		t.Error("user-owned FRRConfiguration should not be pruned")
	}
}

// --- DeleteCUDN tests ---

func TestDeleteCUDN(t *testing.T) {
	s := testScheme()
	s.AddKnownTypeWithName(CUDNNetworkGVK.GroupVersion().WithKind("ClusterUserDefinedNetwork"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(CUDNNetworkGVK.GroupVersion().WithKind("ClusterUserDefinedNetworkList"), &unstructured.UnstructuredList{})

	existing := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.ovn.org/v1",
			"kind":       "ClusterUserDefinedNetwork",
			"metadata": map[string]interface{}{
				"name": "cluster-udn-prod",
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(existing).Build()
	ctx := context.Background()

	if err := DeleteCUDN(ctx, c, "prod"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Delete non-existent should not error
	if err := DeleteCUDN(ctx, c, "prod"); err != nil {
		t.Fatalf("delete of non-existent failed: %v", err)
	}
}

// --- mergeLabels tests ---

func TestMergeLabels(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	overlay := map[string]string{"b": "override", "c": "3"}

	merged := mergeLabels(base, overlay)

	if merged["a"] != "1" {
		t.Error("base key 'a' missing")
	}
	if merged["b"] != "override" {
		t.Error("overlay should override base")
	}
	if merged["c"] != "3" {
		t.Error("overlay key 'c' missing")
	}
	if len(merged) != 3 {
		t.Errorf("expected 3 keys, got %d", len(merged))
	}
}
