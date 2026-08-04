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
	"fmt"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
)

// TenantSAGroup names the group that carries ONE ServiceAccount's cluster-scoped
// reads on the kubezoo path.
//
// ⛔ ONE ServiceAccount, not one tenant, and that is the whole design. The
// tenant's own credential already holds cluster-scoped reads through
// ProxiedGroup, and the obvious move -- hand that same group to the tenant's
// ServiceAccounts -- is the thing ImpersonationGroups warns about in so many
// words: it would "quietly turn every workload into a cluster-scoped one". A
// tenant does not trust its own workloads equally either; splitting them across
// ServiceAccounts is how it says so, and a per-tenant group erases exactly that
// distinction.
//
// ⚠️ Under kubezooGroupPrefix, so the front door drops it if it ever arrives on
// an incoming credential rather than being added here.
func TenantSAGroup(tenantID, namespace, name string) string {
	return fmt.Sprintf("%sproxied-sa:%s:%s:%s", kubezooGroupPrefix, tenantID, namespace, name)
}

// TenantSAFromUsername splits an upstream ServiceAccount username into the
// tenant it belongs to and its own namespace and name.
//
// ⚠️ The namespace it returns is the TENANT's, with the prefix taken off --
// `909090-default` becomes `default`. The group is built from what the tenant
// calls things, so that the tenant's own ClusterRoleBinding, which names
// `default`, is what decides.
func TenantSAFromUsername(username string) (tenantID, namespace, name string, ok bool) {
	const prefix = "system:serviceaccount:"
	if !strings.HasPrefix(username, prefix) {
		return "", "", "", false
	}
	rest := strings.TrimPrefix(username, prefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	upstreamNamespace := parts[0]
	// A tenant's namespaces are `<id>-<name>`, and the id is fixed width.
	if len(upstreamNamespace) <= TenantIDLength+len(TenantIDSeparator) {
		return "", "", "", false
	}
	if upstreamNamespace[TenantIDLength:TenantIDLength+len(TenantIDSeparator)] != TenantIDSeparator {
		return "", "", "", false
	}
	tenantID = upstreamNamespace[:TenantIDLength]
	namespace = upstreamNamespace[TenantIDLength+len(TenantIDSeparator):]
	return tenantID, namespace, parts[1], true
}

// ClusterScopedRulesForSA is the intersection a tenant's ServiceAccount may hold
// at cluster scope: what its own role asks for, and nothing the tenant is not
// itself allowed.
//
// ⛔ THE SECURITY KERNEL OF THE SERVICEACCOUNT PATH, and both halves of the
// intersection are load-bearing in different directions:
//
//   - Only what the SA's role ASKS FOR. Handing it the tenant's whole
//     cluster-scoped set would be the per-workload version of the mistake this
//     design exists to avoid -- the tenant said what this ServiceAccount is for
//     by writing that role.
//   - Only what the TENANT MAY HAVE. A tenant can write any ClusterRole it
//     likes; without this half, a rule the tenant itself is not allowed at
//     cluster scope would arrive by way of one of its ServiceAccounts.
//
// ⭐ What makes the result safe to grant at all is not this function: it is that
// kubezoo confines every cluster-scoped read by name prefix, on list
// (FilterUnstructuredList) and on watch (UpstreamObjectBelongsToTenant) alike.
// So the SA sees exactly what the tenant's own credential sees. This function
// only decides how much of that view one ServiceAccount gets.
func ClusterScopedRulesForSA(asked []rbacv1.PolicyRule, allowed []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	var out []rbacv1.PolicyRule
	for i := range asked {
		narrowed := narrowToAllowed(&asked[i], allowed)
		if narrowed != nil {
			out = append(out, *narrowed)
		}
	}
	return out
}

// narrowToAllowed keeps the part of one asked-for rule that some allowed rule
// covers, or nil when none does.
func narrowToAllowed(asked *rbacv1.PolicyRule, allowed []rbacv1.PolicyRule) *rbacv1.PolicyRule {
	// ⚠️ Nothing here tests for NonResourceURLs, and that is deliberate: the
	// kept rule is built field by field below and never copies them, so a path
	// on the apiserver itself -- /healthz, /metrics -- cannot survive whatever
	// else the rule says. An explicit check was written here first and no
	// negative control could make it fail; it was documentation dressed as a
	// control. The property is pinned in the test instead, where it cannot rot
	// into looking load-bearing.
	kept := rbacv1.PolicyRule{}
	for _, group := range asked.APIGroups {
		for _, resource := range asked.Resources {
			for _, verb := range asked.Verbs {
				if !allowedBy(allowed, group, resource, verb) {
					continue
				}
				kept.APIGroups = appendUnique(kept.APIGroups, group)
				kept.Resources = appendUnique(kept.Resources, resource)
				kept.Verbs = appendUnique(kept.Verbs, verb)
			}
		}
	}
	if len(kept.APIGroups) == 0 || len(kept.Resources) == 0 || len(kept.Verbs) == 0 {
		return nil
	}
	return &kept
}

// allowedBy reports whether the allowed set covers one (group, resource, verb).
//
// ⚠️ Wildcards are honoured on the ALLOWED side only. A `*` on the asked side is
// a request for everything and is matched literally, so it is covered only if
// the tenant is allowed `*` -- asking for everything must not quietly become
// "everything the tenant happens to have".
func allowedBy(allowed []rbacv1.PolicyRule, group, resource, verb string) bool {
	for i := range allowed {
		rule := &allowed[i]
		if !covers(rule.APIGroups, group) || !covers(rule.Resources, resource) || !covers(rule.Verbs, verb) {
			continue
		}
		return true
	}
	return false
}

func covers(allowed []string, want string) bool {
	for _, entry := range allowed {
		if entry == rbacv1.APIGroupAll || entry == want {
			return true
		}
	}
	return false
}

func appendUnique(list []string, want string) []string {
	for _, entry := range list {
		if entry == want {
			return list
		}
	}
	return append(list, want)
}
