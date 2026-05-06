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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkingv1alpha1 "github.com/openshift/cudn-bgp-routing-operator/api/v1alpha1"
)

func configTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = networkingv1alpha1.AddToScheme(s)

	s.AddKnownTypeWithName(NetworkGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(NetworkGVK.GroupVersion().WithKind("NetworkList"), &unstructured.UnstructuredList{})

	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfiguration"), &unstructured.Unstructured{})
	s.AddKnownTypeWithName(FRRConfigurationGVK.GroupVersion().WithKind("FRRConfigurationList"), &unstructured.UnstructuredList{})
	return s
}

func newTestCUDNBgpConfig() *networkingv1alpha1.CUDNBgpConfig {
	return &networkingv1alpha1.CUDNBgpConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
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
				},
			},
			RouterNodeSelector: map[string]string{"bgp_router": "true"},
		},
	}
}

func TestConfigReconcile_AddsFinalizer(t *testing.T) {
	config := newTestCUDNBgpConfig()
	s := configTestScheme()

	network := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operator.openshift.io/v1",
			"kind":       "Network",
			"metadata":   map[string]interface{}{"name": "cluster"},
			"spec":       map[string]interface{}{},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(config, network).
		WithStatusSubresource(config).
		Build()

	r := &CUDNBgpConfigReconciler{Client: c, Scheme: s}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "cluster"},
	})
	// Will error on FRR not ready, that's OK — we're checking the finalizer
	_ = err

	updated := &networkingv1alpha1.CUDNBgpConfig{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "cluster"}, updated); err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	found := false
	for _, f := range updated.Finalizers {
		if f == ConfigFinalizerName {
			found = true
		}
	}
	if !found {
		t.Error("finalizer not added")
	}
}

func TestConfigReconcile_FullReconcile(t *testing.T) {
	config := newTestCUDNBgpConfig()
	s := configTestScheme()

	network := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operator.openshift.io/v1",
			"kind":       "Network",
			"metadata":   map[string]interface{}{"name": "cluster"},
			"spec":       map[string]interface{}{},
		},
	}

	frrNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: FRRNamespace}}
	frrPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "frr-k8s-pod",
			Namespace: FRRNamespace,
			Labels:    map[string]string{"app": "frr-k8s"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(config, network, frrNS, frrPod).
		WithStatusSubresource(config).
		Build()

	r := &CUDNBgpConfigReconciler{Client: c, Scheme: s}

	// First reconcile adds finalizer
	_, _ = r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "cluster"},
	})

	// Second reconcile does full 3-phase
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "cluster"},
	})
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if result.RequeueAfter != 5*time.Minute {
		t.Errorf("expected 5m resync requeue, got %v", result.RequeueAfter)
	}

	updated := &networkingv1alpha1.CUDNBgpConfig{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "cluster"}, updated); err != nil {
		t.Fatalf("failed to get config: %v", err)
	}
	if updated.Status.Phase != networkingv1alpha1.PhaseReady {
		t.Errorf("expected phase Ready, got %s", updated.Status.Phase)
	}

	if len(updated.Status.Conditions) != 3 {
		t.Errorf("expected 3 conditions, got %d", len(updated.Status.Conditions))
	}

	// Verify FRRConfiguration was created
	frrConfig := &unstructured.Unstructured{}
	frrConfig.SetGroupVersionKind(FRRConfigurationGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Name: "cudn-bgp-az-1", Namespace: FRRNamespace}, frrConfig); err != nil {
		t.Fatalf("FRRConfiguration not created: %v", err)
	}
}

func TestConfigReconcile_WaitingForFRR(t *testing.T) {
	config := newTestCUDNBgpConfig()
	config.Finalizers = []string{ConfigFinalizerName}
	s := configTestScheme()

	network := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operator.openshift.io/v1",
			"kind":       "Network",
			"metadata":   map[string]interface{}{"name": "cluster"},
			"spec":       map[string]interface{}{},
		},
	}

	// No FRR namespace — should requeue without degrading
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(config, network).
		WithStatusSubresource(config).
		Build()

	r := &CUDNBgpConfigReconciler{Client: c, Scheme: s}
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "cluster"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected requeue when FRR not ready")
	}

	updated := &networkingv1alpha1.CUDNBgpConfig{}
	_ = c.Get(context.Background(), types.NamespacedName{Name: "cluster"}, updated)
	if updated.Status.Phase == networkingv1alpha1.PhaseDegraded {
		t.Error("should not be Degraded when simply waiting for FRR")
	}
}

func TestConfigReconcile_DeleteBlockedByRouting(t *testing.T) {
	now := metav1.Now()
	config := newTestCUDNBgpConfig()
	config.Finalizers = []string{ConfigFinalizerName}
	config.DeletionTimestamp = &now

	routing := &networkingv1alpha1.CUDNBgpRouting{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Spec: networkingv1alpha1.CUDNBgpRoutingSpec{
			Network: networkingv1alpha1.NetworkConfig{
				Name: "prod", Subnet: "10.100.0.0/16", Topology: networkingv1alpha1.TopologyLayer2,
			},
		},
	}

	s := configTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(config, routing).
		WithStatusSubresource(config).
		Build()

	r := &CUDNBgpConfigReconciler{Client: c, Scheme: s}
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "cluster"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected requeue when routing CRs still exist")
	}

	updated := &networkingv1alpha1.CUDNBgpConfig{}
	_ = c.Get(context.Background(), types.NamespacedName{Name: "cluster"}, updated)
	found := false
	for _, f := range updated.Finalizers {
		if f == ConfigFinalizerName {
			found = true
		}
	}
	if !found {
		t.Error("finalizer should not be removed while routing CRs exist")
	}
}

func TestConfigReconcile_DeleteSuccessful(t *testing.T) {
	now := metav1.Now()
	config := newTestCUDNBgpConfig()
	config.Finalizers = []string{ConfigFinalizerName}
	config.DeletionTimestamp = &now

	frrObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "frrk8s.metallb.io/v1beta1",
			"kind":       "FRRConfiguration",
			"metadata": map[string]interface{}{
				"name":      "cudn-bgp-az-1",
				"namespace": FRRNamespace,
				"labels":    map[string]interface{}{LabelManagedBy: LabelManagedByVal},
			},
		},
	}

	s := configTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(config, frrObj).
		WithStatusSubresource(config).
		Build()

	r := &CUDNBgpConfigReconciler{Client: c, Scheme: s}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "cluster"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// FRRConfiguration should be cleaned up
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(FRRConfigurationGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Name: "cudn-bgp-az-1", Namespace: FRRNamespace}, obj); err == nil {
		t.Error("FRRConfiguration should be deleted during cleanup")
	}

	// Finalizer should be removed
	updated := &networkingv1alpha1.CUDNBgpConfig{}
	_ = c.Get(context.Background(), types.NamespacedName{Name: "cluster"}, updated)
	for _, f := range updated.Finalizers {
		if f == ConfigFinalizerName {
			t.Error("finalizer should be removed after successful deletion")
		}
	}
}

func TestConfigReconcile_NotFound(t *testing.T) {
	s := configTestScheme()
	c := fake.NewClientBuilder().WithScheme(s).Build()

	r := &CUDNBgpConfigReconciler{Client: c, Scheme: s}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "cluster"},
	})
	if err != nil {
		t.Fatalf("expected no error for not-found, got: %v", err)
	}
}
