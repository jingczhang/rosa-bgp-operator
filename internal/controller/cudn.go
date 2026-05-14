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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	networkingv1alpha1 "github.com/openshift/cudn-bgp-routing-operator/api/v1alpha1"
)

func EnsureNamespace(ctx context.Context, c client.Client, name string) error {
	ns := &corev1.Namespace{}
	err := c.Get(ctx, types.NamespacedName{Name: name}, ns)
	if apierrors.IsNotFound(err) {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					LabelPrimaryUDN: "",
					LabelCUDN:       name,
					LabelManagedBy:  LabelManagedByVal,
				},
			},
		}
		return c.Create(ctx, ns)
	}
	if err != nil {
		return err
	}

	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	changed := false
	for k, v := range map[string]string{
		LabelPrimaryUDN: "",
		LabelCUDN:       name,
		LabelManagedBy:  LabelManagedByVal,
	} {
		if ns.Labels[k] != v {
			ns.Labels[k] = v
			changed = true
		}
	}
	if changed {
		return c.Update(ctx, ns)
	}
	return nil
}

func EnsureCUDN(ctx context.Context, c client.Client, routing *networkingv1alpha1.CUDNBgpRouting) error {
	name := CUDNNamePrefix + routing.Spec.Network.Name

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.ovn.org/v1",
			"kind":       "ClusterUserDefinedNetwork",
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]interface{}{
					LabelAdvertise: "true",
					LabelManagedBy: LabelManagedByVal,
				},
			},
			"spec": map[string]interface{}{
				"namespaceSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						LabelCUDN: routing.Spec.Network.Name,
					},
				},
				"network": map[string]interface{}{
					"topology": "Layer2",
					"layer2": map[string]interface{}{
						"role": "Primary",
						"ipam": map[string]interface{}{
							"lifecycle": "Persistent",
						},
						"subnets": []interface{}{routing.Spec.Network.Subnet},
					},
				},
			},
		},
	}

	return createOrUpdate(ctx, c, obj)
}

func DeleteCUDN(ctx context.Context, c client.Client, networkName string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(CUDNNetworkGVK)
	obj.SetName(CUDNNamePrefix + networkName)

	if err := c.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
