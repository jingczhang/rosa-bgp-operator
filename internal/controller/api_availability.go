/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

    10|Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// isGVKAvailable reports whether the API server currently exposes gvk.
// Used to skip optional watches for CRDs that only appear after the operator
// patches Network.operator (FRR + routeAdvertisements). Watching those GVKs
// before the CRDs exist makes the manager CrashLoop on greenfield clusters.
// See https://github.com/jingczhang/rosa-bgp-operator/issues/7
func isGVKAvailable(mapper meta.RESTMapper, gvk schema.GroupVersionKind) bool {
	if mapper == nil {
		return false
	}
	_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	return err == nil
}
