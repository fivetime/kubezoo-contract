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

const (
	// NodePoolLabelKey is the label and taint key that ties a node to one
	// tenant's pool. Its value is the tenant id.
	//
	// ⚠️ Every tenant's pool must carry a DIFFERENT value, and that is
	// load-bearing rather than tidy. A tenant can bind a pod to a node directly,
	// going around the scheduler, and on that path the taint is not consulted at
	// all -- kubelet checks nodeSelector and ignores NoSchedule. Measured: with
	// per-tenant labels a cross-tenant binding is refused by kubelet with
	// NodeAffinity failed and no container starts; with a label shared between
	// pools the same binding put the pod Running in another tenant's pool.
	NodePoolLabelKey = "kubezoo.io/pool"

	// TenantSchedulerName is the scheduler every tenant pod is pinned to. A
	// tenant naming another one would be asking for a scheduler the platform
	// runs, with whatever placement rules that scheduler applies.
	TenantSchedulerName = "default-scheduler"

	// UnreachableTolerationSeconds is how long a tenant pod stays on a node that
	// has gone not-ready or unreachable. Kubernetes' own default admission
	// injects 300 too; it is repeated here because the placement rules replace
	// the toleration list wholesale, and dropping these would evict every tenant
	// pod the instant a node blipped.
	UnreachableTolerationSeconds = 300
)

// NodePoolFor returns the pool a tenant's workloads belong on.
//
// ⭐ Defined here, once, because three things have to agree on it: the Kyverno
// policy that injects it, kubezoo's own injection, and any operator labelling
// nodes by hand. Two of those are code and one is YAML, so nothing about them
// disagreeing fails to build -- TestPlacementMatchesThePolicy reads the policy
// and compares.
//
// ⚠️ The policy spells this truncate(request.namespace, 6), which is the tenant
// id only because an upstream namespace is <tenant id>-<name> and the id is a
// fixed TenantIDLength bytes. See TestPoliciesDeriveTheTenantTheSameWayGoDoes
// for what went wrong when the two spellings drifted.
func NodePoolFor(tenantID string) string { return tenantID }
