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
	"os"
	"strings"
	"testing"
)

// policyPath is the Kyverno policy that pins the same labels these constants do.
const policyPath = "../../config/policy/tenant-pod-security.yaml"

// TestPolicyPinsTheSamePodSecurityLevel is the check that makes "two expressions
// of one rule" true rather than aspirational.
//
// ⚠️ The Go code and the policy both stamp Pod Security Admission labels on a
// tenant namespace, and they have to agree. Nothing about them disagreeing fails
// to compile, fails to deploy, or shows up in a lab run: the namespace simply
// carries whichever value won, and the weaker one is the one nobody notices.
// This reads the YAML and compares.
func TestPolicyPinsTheSamePodSecurityLevel(t *testing.T) {
	raw, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("reading the policy this constant mirrors: %v", err)
	}
	policy := string(raw)

	for _, want := range []struct {
		what  string
		key   string
		value string
	}{
		{"the enforced level", PodSecurityEnforceLabelKey, PodSecurityLevel},
		{"the version it is enforced against", PodSecurityEnforceVersionLabelKey, PodSecurityVersion},
	} {
		// The policy writes them as a strategic-merge patch, one "key: value"
		// per line under metadata.labels.
		pair := want.key + ": " + want.value
		if !strings.Contains(policy, pair) {
			t.Errorf("%s: the policy does not pin %q, so it and the Go constants disagree "+
				"and a tenant namespace gets whichever wrote last", want.what, pair)
		}
	}

	// ⚠️ And the other direction, which is the one that actually rots: the
	// policy being changed to a weaker level while the constants stay. Looking
	// only for our own value would pass happily with `enforce: privileged`
	// sitting right beside it -- which is not hypothetical, a tenant setting
	// exactly that on its own namespace is why config/policy/README.md exists.
	for _, weaker := range []string{"privileged", "baseline"} {
		if strings.Contains(policy, PodSecurityEnforceLabelKey+": "+weaker) {
			t.Errorf("the policy pins %s: %s somewhere; the Go constants say %q, "+
				"and a namespace ends up with whichever wrote last",
				PodSecurityEnforceLabelKey, weaker, PodSecurityLevel)
		}
	}
}
