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
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsGVKAvailable_NilMapper(t *testing.T) {
	if isGVKAvailable(nil, FRRConfigurationGVK) {
		t.Fatal("expected false for nil mapper")
	}
}

func TestIsGVKAvailable_PresentAndMissing(t *testing.T) {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		FRRConfigurationGVK.GroupVersion(),
	})
	mapper.Add(FRRConfigurationGVK, meta.RESTScopeNamespace)

	if !isGVKAvailable(mapper, FRRConfigurationGVK) {
		t.Fatal("expected FRRConfiguration to be available")
	}
	if isGVKAvailable(mapper, RouteAdvertisementsGVK) {
		t.Fatal("expected RouteAdvertisements to be unavailable")
	}
}
