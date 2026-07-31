/*
Copyright 2022 The KubeZoo Authors.

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

package util

import (
	"context"
	"testing"

	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestFieldSelectorIsTranslated guards a whole class of silent emptiness.
//
// ⚠️ Only involvedObject.namespace used to be rewritten. A selector that names
// something kubezoo prefixes and is not rewritten does not fail -- it matches
// nothing upstream and the request succeeds, so the client is told the world is
// empty rather than told it asked the wrong question. The single-object informer
// on a cluster-scoped resource, which is the standard client-go pattern for
// watching one CRD or webhook configuration, watched an empty world forever.
func TestFieldSelectorIsTranslated(t *testing.T) {
	const tenantID = "111111"
	crdKind := schema.GroupVersionKind{
		Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition",
	}
	podKind := schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
	pvKind := schema.GroupVersionKind{Version: "v1", Kind: "PersistentVolume"}

	cases := []struct {
		name     string
		selector string
		scope    ListOptionScope
		want     string
	}{
		{
			name:     "metadata.name on a cluster-scoped resource",
			selector: "metadata.name=my-volume",
			scope:    ListOptionScope{NamespaceScoped: false, Kind: pvKind},
			want:     "metadata.name=111111-my-volume",
		},
		{
			name:     "metadata.name on a CRD, whose prefix sits in the middle",
			selector: "metadata.name=widgets.example.com",
			scope:    ListOptionScope{NamespaceScoped: false, Kind: crdKind},
			want:     "metadata.name=widgets.111111-example.com",
		},
		{
			name:     "metadata.name on a namespaced resource is left alone",
			selector: "metadata.name=my-pod",
			scope:    ListOptionScope{NamespaceScoped: true, Kind: podKind},
			want:     "metadata.name=my-pod",
		},
		{
			name:     "metadata.namespace, which the fan-out forwards into each namespace",
			selector: "metadata.namespace=default",
			scope:    ListOptionScope{NamespaceScoped: true, Kind: podKind},
			want:     "metadata.namespace=111111-default",
		},
		{
			name:     "metadata.namespace already in upstream terms",
			selector: "metadata.namespace=111111-default",
			scope:    ListOptionScope{NamespaceScoped: true, Kind: podKind},
			want:     "metadata.namespace=111111-default",
		},
		{
			name:     "involvedObject.namespace, which was the only one handled",
			selector: "involvedObject.namespace=default",
			scope:    ListOptionScope{NamespaceScoped: true, Kind: podKind},
			want:     "involvedObject.namespace=111111-default",
		},
		{
			name:     "a field kubezoo does not rewrite",
			selector: "status.phase=Running",
			scope:    ListOptionScope{NamespaceScoped: true, Kind: podKind},
			want:     "status.phase=Running",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selector, err := fields.ParseSelector(tc.selector)
			if err != nil {
				t.Fatalf("parsing %q: %v", tc.selector, err)
			}
			out, err := ConvertInternalListOptions(context.Background(),
				&metainternalversion.ListOptions{FieldSelector: selector}, tenantID, tc.scope)
			if err != nil {
				t.Fatalf("converting: %v", err)
			}
			if out.FieldSelector != tc.want {
				t.Errorf("field selector = %q, want %q", out.FieldSelector, tc.want)
			}
		})
	}
}
