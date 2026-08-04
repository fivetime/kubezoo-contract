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

// TestClusterScopedRules pins the one judgement this whole feature rests on:
// which of a tenant's rules may be granted across the entire cluster.
//
// ⭐ The safe set is exactly the rules whose every API group carries the
// tenant's id. Such a group can only ever contain that tenant's objects --
// pkg/convert rewrites spec.group on the way in and refuses any other -- so an
// ordinary cluster-wide binding over it reaches nothing of anybody else's. Not
// by filtering: the objects do not exist.
//
// ⛔ Everything else is dropped, and the cases below are the ways "everything
// else" disguises itself.
func TestRulesSafeForClusterWideBinding(t *testing.T) {
	const tenant = "909090"
	rule := func(groups []string, resources ...string) rbacv1.PolicyRule {
		return rbacv1.PolicyRule{
			APIGroups: groups,
			Resources: resources,
			Verbs:     []string{"get", "list", "watch"},
		}
	}

	for _, tc := range []struct {
		name string
		in   []rbacv1.PolicyRule
		want int
	}{
		{
			name: "the tenant's own group",
			in:   []rbacv1.PolicyRule{rule([]string{"909090-cert-manager.io"}, "clusterissuers")},
			want: 1,
		},
		{
			// ⭐ Safe despite the wildcard: the GROUP confines it, so "every
			// resource" means every resource of the tenant's own kind.
			name: "wildcard resources inside the tenant's group",
			in:   []rbacv1.PolicyRule{rule([]string{"909090-cert-manager.io"}, "*")},
			want: 1,
		},
		{
			name: "the core group",
			in:   []rbacv1.PolicyRule{rule([]string{""}, "secrets")},
			want: 0,
		},
		{
			name: "every group",
			in:   []rbacv1.PolicyRule{rule([]string{"*"}, "*")},
			want: 0,
		},
		{
			name: "a platform group",
			in:   []rbacv1.PolicyRule{rule([]string{"apiextensions.k8s.io"}, "customresourcedefinitions")},
			want: 0,
		},
		{
			name: "another tenant's group",
			in:   []rbacv1.PolicyRule{rule([]string{"111111-cert-manager.io"}, "clusterissuers")},
			want: 0,
		},
		{
			// ⛔ The one that matters most. A rule's groups are ALTERNATIVES, so
			// keeping the rule because one of them is safe would grant the other
			// one too -- here, secrets across the whole cluster.
			name: "the tenant's group ALONGSIDE the core group",
			in:   []rbacv1.PolicyRule{rule([]string{"909090-cert-manager.io", ""}, "secrets")},
			want: 0,
		},
		{
			name: "the tenant's group alongside a wildcard",
			in:   []rbacv1.PolicyRule{rule([]string{"909090-cert-manager.io", "*"}, "*")},
			want: 0,
		},
		{
			// ⛔ Prefix without the separator. The tenant id is fixed width and a
			// tenant chooses the rest of its group names, so without this a
			// tenant could name a group that reads as another tenant's -- or,
			// here, a group that merely starts with the same digits.
			name: "a group that starts with the id but is not the tenant's",
			in:   []rbacv1.PolicyRule{rule([]string{"909090x.io"}, "widgets")},
			want: 0,
		},
		{
			name: "no groups named at all",
			in:   []rbacv1.PolicyRule{{Resources: []string{"secrets"}, Verbs: []string{"get"}}},
			want: 0,
		},
		{
			// ⛔ /healthz, /metrics, /debug/*. They belong to no API group and to
			// no tenant, so no group test can make them safe.
			name: "non-resource URLs",
			in: []rbacv1.PolicyRule{{
				APIGroups:       []string{"909090-cert-manager.io"},
				NonResourceURLs: []string{"/metrics"},
				Verbs:           []string{"get"},
			}},
			want: 0,
		},
		{
			name: "a mixed role keeps only the safe rules",
			in: []rbacv1.PolicyRule{
				rule([]string{"909090-cert-manager.io"}, "clusterissuers"),
				rule([]string{""}, "secrets"),
				rule([]string{"909090-acme.cert-manager.io"}, "challenges"),
				rule([]string{"apiextensions.k8s.io"}, "customresourcedefinitions"),
			},
			want: 2,
		},
		{
			// ⛔ The tenant id is what the prefix is built from, so an empty one
			// turns the test into a bare "-" and makes `-foo` look owned. Found
			// by a negative control: deleting the explicit ""/`*` check changed
			// nothing, which said the real risk was elsewhere.
			name: "a group that would match an empty tenant id",
			in:   []rbacv1.PolicyRule{rule([]string{"-cert-manager.io"}, "clusterissuers")},
			want: 0,
		},
		{
			name: "an empty role yields nothing",
			in:   nil,
			want: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RulesSafeForClusterWideBinding(tc.in, tenant)
			// ⚠️ Nothing is ever owned by a tenant that is not there. Checked on
			// every case rather than once, because "no tenant" is the state a
			// caller reaches by forgetting to look one up.
			if kept := RulesSafeForClusterWideBinding(tc.in, ""); len(kept) != 0 {
				t.Errorf("an empty tenant id kept %d rules: %+v", len(kept), kept)
			}
			if len(got) != tc.want {
				t.Fatalf("kept %d rules, want %d: %+v", len(got), tc.want, got)
			}
			// Whatever survived must survive the test again -- a filter that is
			// not idempotent is a filter that depends on how often it runs.
			if len(RulesSafeForClusterWideBinding(got, tenant)) != len(got) {
				t.Errorf("filtering the result again changed it, so the rule is not a property of the rule")
			}
		})
	}
}

// TestRulesSafeForClusterWideBindingDoesNotAliasItsInput pins that the derived role cannot
// be changed by whatever later edits the tenant's own role object.
//
// ⚠️ The rules come from an object read from a cache in some call paths. Handing
// out the same backing arrays would let an edit of the tenant's ClusterRole
// reach into a grant that has already been made and widened nothing at the time
// it was checked.
func TestRulesSafeForClusterWideBindingDoesNotAliasItsInput(t *testing.T) {
	in := []rbacv1.PolicyRule{{
		APIGroups: []string{"909090-cert-manager.io"},
		Resources: []string{"clusterissuers"},
		Verbs:     []string{"get"},
	}}
	got := RulesSafeForClusterWideBinding(in, "909090")
	if len(got) != 1 {
		t.Fatalf("expected the rule to survive, got %d", len(got))
	}
	in[0].APIGroups[0] = "*"
	in[0].Verbs[0] = "*"
	if got[0].APIGroups[0] != "909090-cert-manager.io" || got[0].Verbs[0] != "get" {
		t.Errorf("the derived rule followed an edit of the input: %+v", got[0])
	}
}
