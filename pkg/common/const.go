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

const (
	TenantNamespaceLabelKey = "kubezoo.io/tenant"

	// TenantFrozenLabelKey marks a frozen tenant's namespaces so that a policy
	// in the upstream API server can refuse the tenant's remaining credentials.
	//
	// Withdrawing the RoleBindings kubezoo issued is not enough on its own: a
	// tenant that bound its own ServiceAccount keeps that binding, and its pods
	// reach upstream directly without passing through kubezoo at all. Measured
	// -- a frozen tenant's pod still listed and created objects. The label is
	// how the front door tells upstream which namespaces are frozen, since
	// upstream has no view of the Tenant object.
	TenantFrozenLabelKey = "kubezoo.io/frozen"

	TenantQuotaNamePrefix = "kubezoo-tenant-quota"

	// PodSecurityEnforceLabelKey and PodSecurityEnforceVersionLabelKey are
	// Kubernetes' own keys; PodSecurityLevel and PodSecurityVersion are the
	// values kubezoo pins on every tenant namespace.
	//
	// ⭐ Pinned in Go, not only by the Kyverno policy that also pins them, and
	// the duplication is the point. Pod Security Admission runs INSIDE the
	// apiserver -- no webhook, no single point -- so once a namespace carries
	// this label the host-escape fields are refused even with every webhook in
	// the cluster gone. But the label was put there by a Kyverno mutate, which
	// means the second layer was installed by the first: a namespace created
	// while Kyverno's webhook was not registered got no label and no enforcement
	// at all. failurePolicy: Fail does not help -- it covers a webhook that
	// fails, not one that was never registered, and the latter is the failure
	// that actually happened (see docs/operations-cn.md in kubezoo-gateway).
	//
	// ⚠️ Kept honest by TestPolicyPinsTheSamePodSecurityLevel, which reads the
	// policy YAML and compares. Two expressions of one rule only stay one rule
	// if something checks.
	PodSecurityEnforceLabelKey        = "pod-security.kubernetes.io/enforce"
	PodSecurityEnforceVersionLabelKey = "pod-security.kubernetes.io/enforce-version"
	PodSecurityLevel                  = "restricted"
	PodSecurityVersion                = "latest"

	// ProjectedClusterRoleBindingLabelKey marks the RoleBindings that carry a
	// tenant's ClusterRoleBinding.
	//
	// A tenant's cluster is its namespaces, so its cluster-wide binding is one
	// RoleBinding in each of them. The label is how those are told apart from
	// the tenant's own RoleBindings, which share the namespace and must not
	// appear when it lists ClusterRoleBindings -- nor the other way round.
	ProjectedClusterRoleBindingLabelKey = "kubezoo.io/clusterrolebinding"

	// StorageClassPublishedLabelKey and IngressClassPublishedLabelKey mark the
	// platform's own class objects as offered to tenants.
	//
	// The key is qualified by the resource rather than being one shared
	// kubezoo.io/published, which is the shape Kubernetes itself uses for
	// markers on these objects -- storageclass.kubernetes.io/is-default-class,
	// ingressclass.kubernetes.io/is-default-class. It reads as what it is in a
	// selector, and adding runtimeclass or volumesnapshotclass later needs no new
	// vocabulary. The other kubezoo.io/* labels are a different thing: they are
	// stamped on a TENANT's objects, while these are stamped by the platform on
	// its own.
	//
	// A label rather than an annotation, unlike the Kubernetes markers above,
	// because these drive an informer's selector and a selector cannot match an
	// annotation.
	//
	// ⚠️ Only the platform can set these: a tenant has no write verb on
	// storageclasses at all, and an IngressClass a tenant writes is name-prefixed
	// into its own space. That is what makes it safe to key behaviour on them --
	// see the rule in config/policy/README.md, which exists because a tenant once
	// set pod-security enforce: privileged on its own namespace.
	StorageClassPublishedLabelKey = "storageclass.kubezoo.io/published"
	IngressClassPublishedLabelKey = "ingressclass.kubezoo.io/published"
	// VolumeAttributesClassPublishedLabelKey marks a VolumeAttributesClass a
	// tenant may name in spec.volumeAttributesClassName.
	//
	// ⭐ Publishing NONE is the useful default here, unlike the other two. A
	// VolumeAttributesClass carries the CSI driver's IOPS and throughput
	// parameters, so naming one is asking for a performance tier -- something a
	// platform sells rather than something a tenant picks freely. With no class
	// labelled, no tenant can set the field at all, and a platform that does
	// offer tiers labels exactly those.
	//
	// ⚠️ Unlike storageClassName this field is MUTABLE after the claim is bound,
	// so the refusal cannot be create-only. See
	// tenantProxy.refuseUnpublishedVolumeAttributesClass.
	VolumeAttributesClassPublishedLabelKey = "volumeattributesclass.kubezoo.io/published"

	// DeviceClassPublishedLabelKey marks a DeviceClass a tenant may name in a
	// ResourceClaim's spec.devices.requests[].deviceClassName.
	//
	// ⭐ The fourth of these, and the same shape for the same reason: a
	// cluster-scoped object the PLATFORM owns, referenced by NAME from inside a
	// tenant's object. Storage classes, ingress classes and volume attributes
	// classes each arrived as "the field is passed through untranslated, so a
	// tenant naming the platform's own gets it" -- this one is being wired at
	// the moment the API group is opened rather than after.
	//
	// ⚠️ Publishing NOTHING by default, like VolumeAttributesClass. A DeviceClass
	// selects HARDWARE -- which GPUs, which accelerators -- so it is a tier a
	// platform sells rather than a choice a tenant makes freely, and there is no
	// existing pass-through behaviour to stay compatible with.
	DeviceClassPublishedLabelKey = "deviceclass.kubezoo.io/published"

	// PublishedTrue means tenants may see the class and use it for new objects.
	PublishedTrue = "true"
	// PublishedDeprecated means tenants may still SEE the class -- so that an
	// existing reference is explicable and the tenant can read that it is on the
	// way out -- while new references are refused.
	//
	// The two states are separate from "no label at all" on purpose. Removing a
	// label has to stay a safe, reversible act: an operator tidying up, or fixing
	// a typo, must not silently stop some tenant's StatefulSet from scaling.
	// Retiring a class is a different intention and says so.
	PublishedDeprecated = "deprecated"
)

// ReservedClusterRoleNames are the upstream names kubezoo keeps for itself, per
// tenant, in the RBAC group.
//
// They collide with what a tenant produces: names of cluster-scoped objects get
// the tenant prefix, so a tenant creating a ClusterRole called cluster-admin
// addresses <tid>-cluster-admin -- which is the very role the controller creates
// and binds cluster-wide to that tenant. Measured: with escalate granted, a
// tenant overwrote it with star-on-star and reached kube-system's and another
// tenant's secrets. Without escalate it can still delete it, which is self-harm
// the controller repairs, but the collision is what turns any future privilege
// on this path into an escape.
func ReservedClusterRoleNames(tenantID string) []string {
	return []string{
		tenantID + "-cluster-admin",
		tenantID + "-admin",
	}
}

// IsReservedClusterName reports whether an upstream name is one kubezoo manages
// for this tenant and the tenant must not write.
func IsReservedClusterName(tenantID, upstreamName string) bool {
	for _, reserved := range ReservedClusterRoleNames(tenantID) {
		if upstreamName == reserved {
			return true
		}
	}
	return false
}
