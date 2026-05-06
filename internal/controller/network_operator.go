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
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var networkPatch = map[string]interface{}{
	"spec": map[string]interface{}{
		"additionalRoutingCapabilities": map[string]interface{}{
			"providers": []interface{}{"FRR"},
		},
		"defaultNetwork": map[string]interface{}{
			"ovnKubernetesConfig": map[string]interface{}{
				"routeAdvertisements": "Enabled",
			},
		},
	},
}

func PatchNetworkOperator(ctx context.Context, c client.Client) error {
	patchBytes, err := json.Marshal(networkPatch)
	if err != nil {
		return err
	}

	network := &unstructured.Unstructured{}
	network.SetGroupVersionKind(NetworkGVK)
	network.SetName("cluster")

	return c.Patch(ctx, network, client.RawPatch(types.MergePatchType, patchBytes))
}
