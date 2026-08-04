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
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
)

func rule(groups, resources, verbs []string) rbacv1.PolicyRule {
	return rbacv1.PolicyRule{APIGroups: groups, Resources: resources, Verbs: verbs}
}

// TestClusterScopedRulesForSA pins how much of a tenant's cluster-scoped view
// one of its ServiceAccounts gets.
//
// ⛔ Both halves of the intersection guard a different mistake:
//
//   - "only what it asked for" stops the per-workload version of the blunder
//     this design exists to avoid. Handing a ServiceAccount the tenant's whole
//     cluster-scoped set would make every workload as powerful as the tenant.
//   - "only what the tenant may have" stops a tenant writing itself a wider
//     ClusterRole and collecting the difference through one of its own
//     ServiceAccounts.
func TestClusterScopedRulesForSA(t *testing.T) {
	// What a tenant is allowed at cluster scope, in miniature.
	allowed := []rbacv1.PolicyRule{
		rule([]string{"admissionregistration.k8s.io"},
			[]string{"validatingwebhookconfigurations", "mutatingwebhookconfigurations"},
			[]string{"get", "list", "watch", "create", "update", "patch", "delete"}),
		rule([]string{"apiextensions.k8s.io"}, []string{"customresourcedefinitions"},
			[]string{"get", "list", "watch"}),
	}

	for _, tc := range []struct {
		name      string
		asked     []rbacv1.PolicyRule
		wantRules int
		wantVerbs []string
	}{
		{
			name: "exactly what cainjector needs",
			asked: []rbacv1.PolicyRule{rule([]string{"admissionregistration.k8s.io"},
				[]string{"validatingwebhookconfigurations"}, []string{"get", "list", "watch"})},
			wantRules: 1,
			wantVerbs: []string{"get", "list", "watch"},
		},
		{
			// ⛔ The tenant may not have secrets at cluster scope, so neither may
			// anything it runs.
			name:      "a resource the tenant is not allowed at cluster scope",
			asked:     []rbacv1.PolicyRule{rule([]string{""}, []string{"secrets"}, []string{"list"})},
			wantRules: 0,
		},
		{
			// ⭐ The half that gets forgotten: the group is allowed, the verb is
			// not. CRDs are readable, not writable, so `delete` must not survive
			// on the strength of `get` being fine.
			name: "an allowed resource with a verb that is not",
			asked: []rbacv1.PolicyRule{rule([]string{"apiextensions.k8s.io"},
				[]string{"customresourcedefinitions"}, []string{"get", "delete"})},
			wantRules: 1,
			wantVerbs: []string{"get"},
		},
		{
			// ⛔ Asking for everything must not collect everything the tenant
			// happens to hold. `*` is matched literally on the asked side.
			name:      "asking for every group",
			asked:     []rbacv1.PolicyRule{rule([]string{"*"}, []string{"*"}, []string{"*"})},
			wantRules: 0,
		},
		{
			name: "a mixed rule keeps only the allowed part",
			asked: []rbacv1.PolicyRule{rule(
				[]string{"admissionregistration.k8s.io", ""},
				[]string{"validatingwebhookconfigurations", "secrets"},
				[]string{"list"})},
			wantRules: 1,
			wantVerbs: []string{"list"},
		},
		{
			// ⛔ /healthz and friends belong to no tenant and nothing can confine
			// them.
			name: "non-resource URLs",
			asked: []rbacv1.PolicyRule{{
				NonResourceURLs: []string{"/metrics"},
				Verbs:           []string{"get"},
			}},
			wantRules: 0,
		},
		{
			// ⛔ RBAC does not allow mixing these, but kubezoo runs no
			// validation, so the pin is that the derived rule cannot carry a
			// non-resource path however the asked-for rule was written.
			name: "a rule mixing resources with a non-resource URL",
			asked: []rbacv1.PolicyRule{{
				APIGroups:       []string{"apiextensions.k8s.io"},
				Resources:       []string{"customresourcedefinitions"},
				Verbs:           []string{"get"},
				NonResourceURLs: []string{"/metrics"},
			}},
			wantRules: 1,
			wantVerbs: []string{"get"},
		},
		{
			name:      "nothing asked for",
			asked:     nil,
			wantRules: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ClusterScopedRulesForSA(tc.asked, allowed)
			if len(got) != tc.wantRules {
				t.Fatalf("kept %d rules, want %d: %+v", len(got), tc.wantRules, got)
			}
			for i := range got {
				if len(got[i].NonResourceURLs) > 0 {
					t.Errorf("a derived rule carries non-resource URLs %v; those belong to no tenant",
						got[i].NonResourceURLs)
				}
			}
			if tc.wantVerbs != nil {
				if len(got[0].Verbs) != len(tc.wantVerbs) {
					t.Fatalf("verbs = %v, want %v", got[0].Verbs, tc.wantVerbs)
				}
				for i, v := range tc.wantVerbs {
					if got[0].Verbs[i] != v {
						t.Errorf("verbs = %v, want %v", got[0].Verbs, tc.wantVerbs)
						break
					}
				}
			}
			// ⚠️ Nothing survives an empty allowed set. That is the state a
			// caller reaches by failing to look the tenant's rules up, and it
			// must fail closed.
			if kept := ClusterScopedRulesForSA(tc.asked, nil); len(kept) != 0 {
				t.Errorf("an empty allowed set kept %d rules: %+v", len(kept), kept)
			}
		})
	}
}

// TestTenantSAGroupIsPerServiceAccount pins that the group names ONE
// ServiceAccount.
//
// ⛔ A group naming only the tenant would be the mistake ImpersonationGroups
// warns about in words: every workload the tenant runs would become as
// cluster-scoped as the tenant itself, and splitting workloads across
// ServiceAccounts -- which is how a tenant says it does not trust them equally
// -- would stop meaning anything.
func TestTenantSAGroupIsPerServiceAccount(t *testing.T) {
	a := TenantSAGroup("909090", "909090-default", "cainjector")
	b := TenantSAGroup("909090", "909090-default", "some-other-op")
	c := TenantSAGroup("909090", "909090-other-ns", "cainjector")
	d := TenantSAGroup("111111", "111111-default", "cainjector")

	// ⭐⭐ EITHER convention lands on the same name, and this is what the
	// normalisation inside TenantSAGroup buys. The bug it fixes was two callers
	// disagreeing about whether the namespace carries the tenant prefix; making
	// them both pass the upstream form fixed that instance, and this makes the
	// whole class impossible -- a caller that passes the tenant's own name for
	// the namespace gets the same group as one that passes upstream's.
	//
	// ⚠️ Without it the first version of this test could not fail: both inputs
	// had been made identical, so deleting the normalisation changed nothing.
	if trimmed, upstream := TenantSAGroup("909090", "default", "cainjector"), a; trimmed != upstream {
		t.Errorf("the group is %q from the tenant's namespace name and %q from upstream's; "+
			"a caller picking the other convention writes a grant nobody asserts", trimmed, upstream)
	}

	// The two callers must land on the same name. kubezoo builds it from a
	// ServiceAccount's username, the controller from a binding's subject; both
	// hold the upstream namespace, and the group is what joins them. A
	// disagreement here does not fail -- the grant is written under one name and
	// asserted under another, and nothing anywhere says so.
	tenant, ns, name, ok := TenantSAFromUsername("system:serviceaccount:909090-default:cainjector")
	if !ok {
		t.Fatal("could not read the username back")
	}
	if fromUsername := TenantSAGroup(tenant, ns, name); fromUsername != a {
		t.Errorf("the group built from a username is %q, and from a subject %q; a grant written "+
			"under one and asserted under the other simply never applies", fromUsername, a)
	}
	for _, other := range []string{b, c, d} {
		if a == other {
			t.Errorf("%q collides with %q; the group does not distinguish the ServiceAccount", a, other)
		}
	}
	// ⚠️ Under kubezoo's own prefix, so the front door drops it if it ever
	// arrives on an incoming credential instead of being added on the way out.
	if got := TenantSAGroup("909090", "909090-default", "cainjector"); got[:len(kubezooGroupPrefix)] != kubezooGroupPrefix {
		t.Errorf("group %q is not under %q, so a mis-issued credential carrying it would be forwarded", got, kubezooGroupPrefix)
	}
}

// TestTenantSAFromUsername pins how an upstream ServiceAccount username is read
// back into the tenant's own terms.
func TestTenantSAFromUsername(t *testing.T) {
	for _, tc := range []struct {
		in                        string
		wantOK                    bool
		tenant, namespace, svcAcc string
	}{
		// ⚠️ The namespace comes back as UPSTREAM has it, prefix included --
		// that is what TenantSAGroup takes, and trimming in two places is the
		// mismatch that made this feature silently grant nothing the first time.
		{in: "system:serviceaccount:909090-default:cainjector", wantOK: true,
			tenant: "909090", namespace: "909090-default", svcAcc: "cainjector"},
		{in: "system:serviceaccount:909090-kube-system:op", wantOK: true,
			tenant: "909090", namespace: "909090-kube-system", svcAcc: "op"},
		// ⛔ A platform ServiceAccount. Its namespace carries no tenant prefix,
		// and reading one out of it would hand a platform component a tenant's
		// group.
		{in: "system:serviceaccount:kube-system:generic-garbage-collector"},
		{in: "system:serviceaccount:default:sa"},
		// Not a ServiceAccount at all.
		{in: "909090-admin"},
		{in: "system:serviceaccount:909090-default"},
		{in: ""},
	} {
		t.Run(tc.in, func(t *testing.T) {
			tenant, ns, name, ok := TenantSAFromUsername(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got %q/%q/%q)", ok, tc.wantOK, tenant, ns, name)
			}
			if !ok {
				return
			}
			if tenant != tc.tenant || ns != tc.namespace || name != tc.svcAcc {
				t.Errorf("got %q/%q/%q, want %q/%q/%q", tenant, ns, name, tc.tenant, tc.namespace, tc.svcAcc)
			}
		})
	}
}
