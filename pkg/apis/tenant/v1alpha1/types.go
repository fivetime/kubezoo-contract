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

package v1alpha1

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource/resourcestrategy"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	// `spec` is the specification of the desired behavior of a flow-schema.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Spec TenantSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	// `status` is the current status of a flow-schema.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Status TenantStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// TenantList is a list of Tenant objects.
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	// `metadata` is the standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// `items` is a list of tenant
	// +listType=atomic
	Items []Tenant `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// TenantSpec describes how the proxy-rule's specification looks like.
type TenantSpec struct {
	ID    int32       `json:"id" protobuf:"varint,1,name=id"`
	Quota TenantQuota `json:"quota" protobuf:"bytes,2,name=quota"`
	// Suspension stops a tenant from operating without touching what it is
	// running. Absent means the tenant is operating normally.
	// +optional
	Suspension *TenantSuspension `json:"suspension,omitempty" protobuf:"bytes,3,opt,name=suspension"`
	// CredentialValidity is how long a credential issued to this tenant is good
	// for. Absent means the platform's default.
	//
	// ⭐ The tenant's choice, within a ceiling the platform sets. How often it
	// wants to come back for a new credential is a question about the tenant's
	// own operations -- how its CI is wired, how many people hold a kubeconfig,
	// what its own policy says -- and the platform is not in a position to
	// answer it. What the platform does get to say is "no longer than this".
	//
	// ⚠️ It is not only a convenience. A client certificate cannot be revoked, so
	// this number is the entire bound on how long a credential keeps working
	// after the platform would rather it stopped -- which makes a shorter
	// validity the difference between "cannot cut a tenant off" and "cannot cut
	// a tenant off for longer than this".
	// +optional
	CredentialValidity *metav1.Duration `json:"credentialValidity,omitempty" protobuf:"bytes,4,opt,name=credentialValidity"`
	// Capacity is how much of the shared cluster this tenant may take. Absent
	// fields mean the platform's default.
	// +optional
	Capacity *TenantCapacity `json:"capacity,omitempty" protobuf:"bytes,5,opt,name=capacity"`
}

// TenantCapacity caps the cluster-scoped objects one tenant may own.
//
// ⛔ ON THE OBJECT, NOT ON A FLAG, and the difference is operational rather than
// stylistic. Capacity is per tenant, so a flag makes raising ONE tenant's limit a
// restart of the gateway -- which is a single-replica StatefulSet, so that
// restart is every tenant's API interrupted and every tenant's operator watches
// broken, to change one number for one of them. The same reasoning is already
// written down for published storage classes, which moved from flags to labels
// for exactly this.
//
// ⚠️ Set by the PLATFORM, not by the tenant. A tenant cannot write Tenant
// objects at all -- pkg/filters/platformapi.go refuses the whole group to any
// identity carrying a tenant id -- so this is safe to keep beside the fields a
// tenant does choose, like CredentialValidity.
//
// ⭐ These are ceilings on AMPLIFIERS, not billing. A cross-namespace list reads
// each of the tenant's namespaces in turn; one ClusterRoleBinding becomes one
// RoleBinding in every namespace it owns; every CRD it creates enters the
// upstream cluster's discovery document and OpenAPI, which every client of that
// cluster downloads. The cost of each lands on the other tenants.
type TenantCapacity struct {
	// MaxNamespaces caps how many namespaces the tenant may own. Zero means no
	// limit; absent means the platform default.
	// +optional
	MaxNamespaces *int32 `json:"maxNamespaces,omitempty" protobuf:"varint,1,opt,name=maxNamespaces"`
	// MaxClusterRoleBindings caps how many the tenant may own. ⚠️ Multiplies with
	// MaxNamespaces: each binding is projected into every namespace.
	// +optional
	MaxClusterRoleBindings *int32 `json:"maxClusterRoleBindings,omitempty" protobuf:"varint,2,opt,name=maxClusterRoleBindings"`
	// MaxCRDs caps how many CustomResourceDefinitions the tenant may own.
	// ⭐ The heaviest of the three: namespaces and bindings cost per request,
	// a CRD costs continuously whether the tenant uses it again or not.
	// +optional
	MaxCRDs *int32 `json:"maxCRDs,omitempty" protobuf:"varint,3,opt,name=maxCRDs"`
}

// TenantSuspensionMode selects how far a suspension goes.
type TenantSuspensionMode string

const (
	// SuspensionReadOnly leaves the tenant able to see its objects but not to
	// change them. It is the billing case: the point is to prompt payment
	// without manufacturing an incident, and a tenant that cannot see its own
	// objects will reasonably conclude they are gone.
	SuspensionReadOnly TenantSuspensionMode = "ReadOnly"
	// SuspensionFrozen stops the tenant operating at all, while its workloads
	// keep running exactly as they are. It is the investigation case: the
	// tenant must not touch anything, and the evidence must not move.
	//
	// Frozen in the sense an account is frozen: reversible, and what was there
	// is still there. It is deliberately not called revoked -- nothing is
	// revoked. The tenant's certificate still authenticates; what is withdrawn
	// is its authorization, and lifting the suspension gives it straight back.
	//
	// It is the tenant's ability to act that is frozen, not its workloads.
	// Those keep running, which is the point in both the billing and the
	// investigation case. Kubernetes uses "suspend" for the opposite -- a
	// suspended Job stops -- so this is worth being explicit about.
	SuspensionFrozen TenantSuspensionMode = "Frozen"
)

// TenantSuspension describes a suspension in force.
type TenantSuspension struct {
	// Mode is how far the suspension goes.
	Mode TenantSuspensionMode `json:"mode" protobuf:"bytes,1,name=mode"`
	// Reason is shown to the tenant on every refused request, so that it reads
	// as a decision rather than as a malfunction.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,2,opt,name=reason"`
}

type TenantQuota struct {
	// hard is the set of desired hard limits for each named resource.
	// More info: https://kubernetes.io/docs/concepts/policy/resource-quotas/
	// +optional
	Hard corev1.ResourceList `json:"hard,omitempty" protobuf:"bytes,1,rep,name=hard,casttype=ResourceList,castkey=ResourceName"`
}

// TenantStatus represents the current state of a rule.
type TenantStatus struct {
	// Current state of tenant.
	Online bool `json:"online,omitempty" protobuf:"bytes,1,name=online"`
}

var _ resource.Object = &Tenant{}
var _ resourcestrategy.Validater = &Tenant{}

// GetObjectMeta returns the object metadata of tenant.
func (t *Tenant) GetObjectMeta() *metav1.ObjectMeta {
	return &t.ObjectMeta
}

// NamespaceScoped returns whether the tenant is namespace scoped or not.
func (t *Tenant) NamespaceScoped() bool {
	return false
}

// New returns a tenant object.
func (t *Tenant) New() runtime.Object {
	return &Tenant{}
}

// NewList returns a list of tenant objects.
func (t *Tenant) NewList() runtime.Object {
	return &TenantList{}
}

// GetGroupVersionResource returns the group version resource of tenant.
func (t *Tenant) GetGroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "tenant.kubezoo.io",
		Version:  "v1alpha1",
		Resource: "tenants",
	}
}

// IsStorageVersion returns whether tenant is storage version or not.
func (t *Tenant) IsStorageVersion() bool {
	return false
}

// Validate does some validation.
func (t *Tenant) Validate(ctx context.Context) field.ErrorList {
	return nil
}

var _ resource.ObjectList = &TenantList{}

// GetListMeta returns list metadata.
func (in *TenantList) GetListMeta() *metav1.ListMeta {
	return &in.ListMeta
}

// SubResourceName returns subresource name.
func (in TenantStatus) SubResourceName() string {
	return "status"
}

// Xuchen implements ObjectWithStatusSubResource interface.
var _ resource.ObjectWithStatusSubResource = &Tenant{}

// GetStatus returns the sub resource of status.
func (in *Tenant) GetStatus() resource.StatusSubResource {
	return in.Status
}

// XuchenStatus{} implements StatusSubResource interface.
var _ resource.StatusSubResource = &TenantStatus{}

// CopyTo the status.
func (in TenantStatus) CopyTo(parent resource.ObjectWithStatusSubResource) {
	parent.(*Tenant).Status = in
}
