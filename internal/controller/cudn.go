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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	networkingv1alpha1 "github.com/openshift/cudn-bgp-routing-operator/api/v1alpha1"
)

// ValidateNamespaceLabels checks that at least one namespace exists with the required
// labels for the given network name. Users must create and label namespaces themselves.
func ValidateNamespaceLabels(ctx context.Context, c client.Client, networkName string) error {
	nsList := &corev1.NamespaceList{}
	if err := c.List(ctx, nsList, client.MatchingLabels{
		LabelPrimaryUDN: "",
		LabelCUDN:       networkName,
	}); err != nil {
		return err
	}
	if len(nsList.Items) == 0 {
		return fmt.Errorf("no namespace found with labels %s=\"\" and %s=%q; create and label a namespace before applying CUDNBgpRouting",
			LabelPrimaryUDN, LabelCUDN, networkName)
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
