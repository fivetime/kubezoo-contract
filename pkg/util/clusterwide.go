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

// Package util ... (this file)
//
// ⛔ THIS LIVES IN CONTRACT BECAUSE TWO REPOSITORIES MUST GIVE THE SAME ANSWER.
// The gateway decides what a tenant may ask for; kubezoo-controller is what
// actually writes the cluster-wide binding, because writing it needs a privilege
// the tenant structurally does not have. If the two ever disagreed, one side
// would believe a rule was refused while the other granted it -- and nothing
// would report a thing.

package util

import (
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
)

// RulesSafeForClusterWideBinding picks out the rules of a tenant's ClusterRole that are safe
// to grant across the whole cluster, and drops everything else.
//
// ⛔ WHY THIS EXISTS. A tenant's ClusterRoleBinding is projected into one
// RoleBinding per namespace, and a RoleBinding can never carry a cluster-scoped
// grant. So an operator that needs a CLUSTER-SCOPED custom resource -- a
// ClusterIssuer, a ClusterPolicy, a ClusterSecretStore -- installs its RBAC
// successfully and then cannot run. cert-manager fails exactly there.
//
// ⭐ WHY A SUBSET IS SAFE AT ALL, and this is the whole argument: a tenant's API
// groups carry the tenant id. `909090-cert-manager.io` can only ever contain
// tenant 909090's objects, because pkg/convert rewrites spec.group on the way in
// and refuses a group that is not the tenant's. So a REAL, ordinary,
// cluster-wide ClusterRoleBinding over such a group reaches nothing belonging to
// anybody else -- not by filtering, but because the objects do not exist.
//
// That is what makes this different in kind from granting `list` on a native
// cluster-scoped resource. There, RBAC has no way to say "only mine"
// (resourceNames does not apply to list or watch), so the confinement would have
// to be enforced on every read path, forever. Here there is nothing to enforce.
//
// ⚠️ Therefore the test below is not a heuristic and must not be relaxed into
// one. A rule survives only when EVERY api group it names is the tenant's own.
// One `""` or one `*` alongside a tenant group, and the whole rule is dropped --
// a rule is a single grant and its groups are alternatives, so keeping it
// because one of the alternatives is safe would grant the unsafe ones too.
func RulesSafeForClusterWideBinding(rules []rbacv1.PolicyRule, tenantID string) []rbacv1.PolicyRule {
	var safe []rbacv1.PolicyRule
	for i := range rules {
		if ruleIsTenantOwned(&rules[i], tenantID) {
			safe = append(safe, *rules[i].DeepCopy())
		}
	}
	return safe
}

// ruleIsTenantOwned reports whether one rule reaches nothing outside the
// tenant's own API groups.
func ruleIsTenantOwned(rule *rbacv1.PolicyRule, tenantID string) bool {
	// ⛔ Non-resource URLs are paths on the API server itself -- /healthz,
	// /metrics, /debug/*. They belong to no API group and no tenant, so nothing
	// here can make them safe.
	if len(rule.NonResourceURLs) > 0 {
		return false
	}
	// A rule that names no group is a rule about the core group, which is
	// shared. Empty is not "all of the tenant's".
	if len(rule.APIGroups) == 0 {
		return false
	}
	for _, group := range rule.APIGroups {
		if !GroupIsTenantOwned(group, tenantID) {
			return false
		}
	}
	return true
}

// GroupIsTenantOwned reports whether an API group name belongs to this tenant.
//
// ⚠️ Prefixed AND separated. Matching on the bare id would make `909090x.io`
// belong to tenant `909090`, and the tenant id is a fixed-width string a tenant
// can choose the rest of a group name after -- so the separator is what stops
// one tenant naming a group that reads as another's.
func GroupIsTenantOwned(group, tenantID string) bool {
	// ⛔ Load-bearing, and found by a negative control that failed to go red.
	// Without it the prefix below becomes a bare "-", so a group named `-foo`
	// reads as owned by a tenant that does not exist. Every caller is supposed to
	// have a tenant in hand, which is exactly why an empty one must be refused
	// here rather than assumed away.
	if tenantID == "" {
		return false
	}
	// ⚠️ `""` (the core group) and `*` are not tested for, and that is not an
	// omission: neither can carry the prefix below, so the prefix already
	// rejects them. An explicit check for them was written here first and no
	// negative control could make it fail -- it was documentation dressed as a
	// control. The property is stated in the test instead, where it cannot rot
	// into looking load-bearing.
	return strings.HasPrefix(group, tenantID+TenantIDSeparator)
}
