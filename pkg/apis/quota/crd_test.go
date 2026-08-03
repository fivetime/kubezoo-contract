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

package quota

import "testing"

// TestCRDDecodes is what lets NewClusterResourceQuotaCRD panic honestly.
//
// ⚠️ The embedded document is a compile-time constant, so a decode failure means
// a broken build rather than bad input -- but "cannot happen" is only true while
// something checks. Without this the first discovery would be a panic in
// cmd/clusterresourcequota at startup, in a cluster, where the consequence is
// that quota.kubezoo.io is never served and no tenant quota is enforced.
func TestCRDDecodes(t *testing.T) {
	crd, err := decodeClusterResourceQuotaCRD()
	if err != nil {
		t.Fatalf("the embedded CRD does not decode: %v", err)
	}

	// ⭐ The four fields the rest of the system actually depends on. The tenant
	// controller asks discovery for exactly this group and plural, and the
	// reconciler treats the type as cluster-scoped; a CRD that decodes but names
	// something else would leave the quota path silently skipped, which is the
	// failure this whole area has already had once.
	if got := crd.Spec.Group; got != "quota.kubezoo.io" {
		t.Errorf("group is %q; the tenant controller's discovery check asks for "+
			"quota.kubezoo.io and would find nothing", got)
	}
	if got := crd.Spec.Names.Plural; got != "clusterresourcequotas" {
		t.Errorf("plural is %q; discovery matches on the plural", got)
	}
	if got := string(crd.Spec.Scope); got != "Cluster" {
		t.Errorf("scope is %q, want Cluster -- a namespaced quota cannot span a "+
			"tenant's namespaces, which is the entire point of it", got)
	}
	if len(crd.Spec.Versions) == 0 {
		t.Fatal("no versions")
	}
	if got := crd.Spec.Versions[0].Name; got != "v1alpha1" {
		t.Errorf("version is %q, want v1alpha1", got)
	}
	if crd.Spec.Versions[0].Schema == nil || crd.Spec.Versions[0].Schema.OpenAPIV3Schema == nil {
		t.Error("no schema: a v1 CRD without a structural schema is rejected by the apiserver")
	}
	if crd.Spec.Versions[0].Subresources == nil || crd.Spec.Versions[0].Subresources.Status == nil {
		t.Error("no status subresource: the reconciler writes status separately")
	}
}
