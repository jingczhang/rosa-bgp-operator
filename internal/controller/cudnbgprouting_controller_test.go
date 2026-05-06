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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkingv1alpha1 "github.com/openshift/cudn-bgp-routing-operator/api/v1alpha1"
)

func routingTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = networkingv1alpha1.AddToScheme(s)

	s.AddKnownTypeWithName(CUDNNetworkGVK.GroupVersion().WithKind("ClusterUserDefinedNetwork"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(CUDNNetworkGVK.GroupVersion().WithKind("ClusterUserDefinedNetworkList"), &unstructured.UnstructuredList{})
	s.AddKnownTypeWithName(RouteAdvertisementsGVK.GroupVersion().WithKind("RouteAdvertisements"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(RouteAdvertisementsGVK.GroupVersion().WithKind("RouteAdvertisementsList"), &unstructured.UnstructuredList{})
	return s
}

func newTestCUDNBgpRouting() *networkingv1alpha1.CUDNBgpRouting {
	return &networkingv1alpha1.CUDNBgpRouting{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prod",
		},
		Spec: networkingv1alpha1.CUDNBgpRoutingSpec{
			Network: networkingv1alpha1.NetworkConfig{
				Name:     "prod",
				Subnet:   "10.100.0.0/16",
				Topology: networkingv1alpha1.TopologyLayer2,
			},
		},
	}
}

func newReadyCUDNBgpConfig() *networkingv1alpha1.CUDNBgpConfig {
	return &networkingv1alpha1.CUDNBgpConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
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
		Status: networkingv1alpha1.CUDNBgpConfigStatus{
			Phase: networkingv1alpha1.PhaseReady,
		},
	}
}

func TestRoutingReconcile_PendingWithoutConfig(t *testing.T) {
	routing := newTestCUDNBgpRouting()
	routing.Finalizers = []string{RoutingFinalizerName}
	s := routingTestScheme()

	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routing).
		WithStatusSubresource(routing).
		Build()

	r := &CUDNBgpRoutingReconciler{Client: c, Scheme: s}
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "prod"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected requeue when config not found")
	}

	updated := &networkingv1alpha1.CUDNBgpRouting{}
	_ = c.Get(context.Background(), types.NamespacedName{Name: "prod"}, updated)
	if updated.Status.Phase != networkingv1alpha1.PhasePending {
		t.Errorf("expected Pending, got %s", updated.Status.Phase)
	}
}

func TestRoutingReconcile_PendingWhenConfigNotReady(t *testing.T) {
	routing := newTestCUDNBgpRouting()
	routing.Finalizers = []string{RoutingFinalizerName}
	config := newReadyCUDNBgpConfig()
	config.Status.Phase = networkingv1alpha1.PhaseConfiguring

	s := routingTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routing, config).
		WithStatusSubresource(routing, config).
		Build()

	r := &CUDNBgpRoutingReconciler{Client: c, Scheme: s}
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "prod"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected requeue when config not ready")
	}
}

func TestRoutingReconcile_FullReconcile(t *testing.T) {
	routing := newTestCUDNBgpRouting()
	config := newReadyCUDNBgpConfig()

	s := routingTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routing, config).
		WithStatusSubresource(routing, config).
		Build()

	r := &CUDNBgpRoutingReconciler{Client: c, Scheme: s}

	// First reconcile adds finalizer
	_, _ = r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "prod"},
	})

	// Second reconcile does full 2-phase
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "prod"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if result.RequeueAfter != 5*time.Minute {
		t.Errorf("expected 5m resync requeue, got %v", result.RequeueAfter)
	}

	updated := &networkingv1alpha1.CUDNBgpRouting{}
	_ = c.Get(context.Background(), types.NamespacedName{Name: "prod"}, updated)
	if updated.Status.Phase != networkingv1alpha1.PhaseReady {
		t.Errorf("expected Ready, got %s", updated.Status.Phase)
	}
	if len(updated.Status.Conditions) != 2 {
		t.Errorf("expected 2 conditions, got %d", len(updated.Status.Conditions))
	}

	// Verify Namespace created
	ns := &unstructured.Unstructured{}
	ns.SetAPIVersion("v1")
	ns.SetKind("Namespace")
	if err := c.Get(context.Background(), types.NamespacedName{Name: "prod"}, ns); err != nil {
		t.Fatalf("namespace not created: %v", err)
	}

	// Verify CUDN created
	cudn := &unstructured.Unstructured{}
	cudn.SetGroupVersionKind(CUDNNetworkGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Name: "cluster-udn-prod"}, cudn); err != nil {
		t.Fatalf("CUDN not created: %v", err)
	}

	// Verify RouteAdvertisements created
	ra := &unstructured.Unstructured{}
	ra.SetGroupVersionKind(RouteAdvertisementsGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Name: RouteAdvertisementName}, ra); err != nil {
		t.Fatalf("RouteAdvertisements not created: %v", err)
	}
}

func TestRoutingReconcile_AddsFinalizer(t *testing.T) {
	routing := newTestCUDNBgpRouting()
	config := newReadyCUDNBgpConfig()

	s := routingTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routing, config).
		WithStatusSubresource(routing, config).
		Build()

	r := &CUDNBgpRoutingReconciler{Client: c, Scheme: s}
	_, _ = r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "prod"},
	})

	updated := &networkingv1alpha1.CUDNBgpRouting{}
	_ = c.Get(context.Background(), types.NamespacedName{Name: "prod"}, updated)

	found := false
	for _, f := range updated.Finalizers {
		if f == RoutingFinalizerName {
			found = true
		}
	}
	if !found {
		t.Error("finalizer not added")
	}
}

func TestRoutingReconcile_DeleteLastRemovesRA(t *testing.T) {
	now := metav1.Now()
	routing := newTestCUDNBgpRouting()
	routing.Finalizers = []string{RoutingFinalizerName}
	routing.DeletionTimestamp = &now

	ra := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.ovn.org/v1",
			"kind":       "RouteAdvertisements",
			"metadata": map[string]interface{}{
				"name":   RouteAdvertisementName,
				"labels": map[string]interface{}{LabelManagedBy: LabelManagedByVal},
			},
		},
	}

	s := routingTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routing, ra).
		WithStatusSubresource(routing).
		Build()

	r := &CUDNBgpRoutingReconciler{Client: c, Scheme: s}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "prod"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RA should be deleted since this was the last routing CR
	raCheck := &unstructured.Unstructured{}
	raCheck.SetGroupVersionKind(RouteAdvertisementsGVK)
	err = c.Get(context.Background(), types.NamespacedName{Name: RouteAdvertisementName}, raCheck)
	if err == nil {
		t.Error("RouteAdvertisements should be deleted when last routing CR is removed")
	}
}

func TestRoutingReconcile_DeleteKeepsRAWhenOthersExist(t *testing.T) {
	now := metav1.Now()
	routing := newTestCUDNBgpRouting()
	routing.Finalizers = []string{RoutingFinalizerName}
	routing.DeletionTimestamp = &now

	other := &networkingv1alpha1.CUDNBgpRouting{
		ObjectMeta: metav1.ObjectMeta{Name: "staging"},
		Spec: networkingv1alpha1.CUDNBgpRoutingSpec{
			Network: networkingv1alpha1.NetworkConfig{
				Name: "staging", Subnet: "10.200.0.0/16", Topology: networkingv1alpha1.TopologyLayer2,
			},
		},
	}

	ra := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.ovn.org/v1",
			"kind":       "RouteAdvertisements",
			"metadata": map[string]interface{}{
				"name":   RouteAdvertisementName,
				"labels": map[string]interface{}{LabelManagedBy: LabelManagedByVal},
			},
		},
	}

	s := routingTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routing, other, ra).
		WithStatusSubresource(routing, other).
		Build()

	r := &CUDNBgpRoutingReconciler{Client: c, Scheme: s}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "prod"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RA should still exist since "staging" routing CR remains
	raCheck := &unstructured.Unstructured{}
	raCheck.SetGroupVersionKind(RouteAdvertisementsGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Name: RouteAdvertisementName}, raCheck); err != nil {
		t.Error("RouteAdvertisements should be kept when other routing CRs exist")
	}
}

func TestRoutingReconcile_DuplicateNetworkName(t *testing.T) {
	existing := &networkingv1alpha1.CUDNBgpRouting{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-prod"},
		Spec: networkingv1alpha1.CUDNBgpRoutingSpec{
			Network: networkingv1alpha1.NetworkConfig{
				Name: "prod", Subnet: "10.100.0.0/16", Topology: networkingv1alpha1.TopologyLayer2,
			},
		},
	}
	duplicate := newTestCUDNBgpRouting()
	duplicate.Finalizers = []string{RoutingFinalizerName}
	config := newReadyCUDNBgpConfig()

	s := routingTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(existing, duplicate, config).
		WithStatusSubresource(existing, duplicate, config).
		Build()

	r := &CUDNBgpRoutingReconciler{Client: c, Scheme: s}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "prod"},
	})
	if err == nil {
		t.Fatal("expected error for duplicate network name")
	}

	updated := &networkingv1alpha1.CUDNBgpRouting{}
	_ = c.Get(context.Background(), types.NamespacedName{Name: "prod"}, updated)
	if updated.Status.Phase != networkingv1alpha1.PhaseDegraded {
		t.Errorf("expected Degraded, got %s", updated.Status.Phase)
	}
}

func TestRoutingReconcile_NotFound(t *testing.T) {
	s := routingTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).Build()

	r := &CUDNBgpRoutingReconciler{Client: c, Scheme: s}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "prod"},
	})
	if err != nil {
		t.Fatalf("expected no error for not-found, got: %v", err)
	}
}
