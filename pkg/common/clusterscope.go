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

package common

import (
	rbacv1 "k8s.io/api/rbac/v1"
	rbacv1helpers "k8s.io/kubernetes/pkg/apis/rbac/v1"
)

// What a tenant may do at cluster scope.
//
// The controller builds the tenant's ClusterRole from this, and the proxy has a
// test that fails when it drifts from the resources actually served. Both need
// it and both need the same answer, which is why it lives here rather than in
// either of them -- splitting the repositories is exactly what would have let
// the two halves drift apart in silence.

// ClusterScopedRules is the cluster-scoped half of what a tenant may do
// upstream: every cluster-scoped resource kubezoo actually serves, and nothing
// else. cmd/kubezoo/app has a test that fails if apigroups.go and this list
// drift apart.
//
// RBAC cannot bound these by name. resourceNames is an exact match with no
// prefix or wildcard form, and a tenant's cluster-scoped objects are
// distinguished from another tenant's only by a "<id>-" prefix that it cannot
// express. So for these resources the rewriting layer remains the only thing
// keeping tenants apart, exactly as before this change. What follows is
// therefore not a security boundary for cluster-scoped resources; it is one for
// the namespaced ones, which are the large majority.
//
// customresourcedefinitions is here even though apigroups.go does not declare
// it: CRDs reach upstream through the CRD handler rather than the generic
// proxy, and tenants do create them.
func ClusterScopedRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		// Namespaces are the tenant's own, created through kubezoo, which
		// prefixes them. status and finalize belong to the namespace controller.
		rbacv1helpers.NewRule("get", "list", "watch", "create", "update", "patch", "delete").
			Groups("").Resources("namespaces").RuleOrDie(),

		// Read-only. Whether tenants should see nodes at all is a separate
		// question -- kubezoo shows them unconditionally today -- but nothing
		// gives a tenant a reason to write one.
		rbacv1helpers.NewRule("get", "list", "watch").
			Groups("").Resources("nodes", "componentstatuses").RuleOrDie(),

		// Prefixed by the convertor, so tenants cannot collide or reach each
		// other's.
		rbacv1helpers.NewRule("get", "list", "watch", "create", "update", "patch",
			"delete", "deletecollection").
			Groups("").Resources("persistentvolumes", "persistentvolumes/status").RuleOrDie(),

		// Confined by pkg/convert's webhook transformer, which forces the
		// namespace selector, the rule scope and the client config.
		rbacv1helpers.NewRule("get", "list", "watch", "create", "update", "patch",
			"delete", "deletecollection").
			Groups("admissionregistration.k8s.io").
			Resources("mutatingwebhookconfigurations", "validatingwebhookconfigurations").RuleOrDie(),

		// The tenant's own CRDs, group-prefixed by the CRD convertor.
		rbacv1helpers.NewRule("get", "list", "watch", "create", "update", "patch",
			"delete", "deletecollection").
			Groups("apiextensions.k8s.io").
			Resources("customresourcedefinitions", "customresourcedefinitions/status").RuleOrDie(),

		// Create-only APIs: they take a request and return an answer, and there
		// is nothing to list or delete.
		rbacv1helpers.NewRule("create").Groups("authentication.k8s.io").
			Resources("tokenreviews").RuleOrDie(),
		rbacv1helpers.NewRule("create").Groups("authorization.k8s.io").
			Resources("selfsubjectaccessreviews", "selfsubjectrulesreviews", "subjectaccessreviews").RuleOrDie(),

		// Cluster-scoped but name-prefixed like any other.
		rbacv1helpers.NewRule("get", "list", "watch", "create", "update", "patch",
			"delete", "deletecollection").
			Groups("networking.k8s.io").Resources("ingressclasses").RuleOrDie(),
		rbacv1helpers.NewRule("get", "list", "watch", "create", "update", "patch",
			"delete", "deletecollection").
			Groups("node.k8s.io").Resources("runtimeclasses").RuleOrDie(),

		// Verbs are spelled out here rather than "*", and the two that are
		// missing are the point.
		//
		// "escalate" and "bind" exist precisely to permit privilege escalation:
		// RBAC normally refuses to let you create a role carrying permissions you
		// do not hold, and those verbs are the documented exemption. Granting
		// "*" grants them, and a tenant could then create a ClusterRole with "*"
		// on "*", bind its own upstream identity to it, and undo every bound in
		// this file. That was verified against a real cluster: with "*" the
		// tenant reached other tenants' secrets and kube-system; with the verbs
		// below it cannot create such a role at all.
		rbacv1helpers.NewRule("get", "list", "watch", "create", "update", "patch",
			"delete", "deletecollection").
			Groups("rbac.authorization.k8s.io").
			Resources("clusterroles", "clusterrolebindings").RuleOrDie(),
	}
}

// NotGrantedToTenants names cluster-scoped resources kubezoo serves that tenants
// are deliberately not authorized for upstream, with the reason.
//
// The grant above used to be derived mechanically from what apigroups.go serves,
// which kept the two in step but inherited every questionable decision in the
// served surface. nodes/proxy is why that is not good enough: it was served, so
// it was granted, and a tenant could then reach the kubelet API on any node --
// verified, listing every pod on the node including kube-system's, and from
// there the logs and a shell in any container of any tenant. It is a proxy, so
// the rewriting layer never sees the path.
func NotGrantedToTenants() map[string][]string {
	return map[string][]string{
		"": {
			// The escape above. Nothing a tenant does needs it.
			"nodes/proxy",
			// Writing node status is the kubelet's job.
			"nodes/status",
			// The namespace controller's, not a tenant's; finalize in particular
			// would let a tenant force a namespace through termination.
			"namespaces/status", "namespaces/finalize",
		},
	}
}
