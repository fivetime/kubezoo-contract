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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPlacementMatchesThePolicy keeps kubezoo's injection and the Kyverno policy
// injecting the same thing.
//
// ⚠️ Both write a tenant pod's nodeSelector, tolerations and schedulerName, and
// they run one after the other -- kubezoo first, the policy second, so the policy
// wins whenever it is there. Which means a disagreement is INVISIBLE in a healthy
// cluster: everything behaves exactly as the policy says, and kubezoo's copy only
// starts deciding placement on the day the webhook is gone. That is the worst
// possible time to discover the two never agreed.
//
// Nothing about them disagreeing fails to build, fails to deploy, or shows up in
// a lab run. This reads the policy and compares.
func TestPlacementMatchesThePolicy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "policy", "tenant-placement.yaml"))
	if err != nil {
		t.Fatalf("reading the policy this mirrors: %v", err)
	}
	policy := string(raw)

	for _, want := range []struct {
		what string
		text string
	}{
		{"the pool label key", NodePoolLabelKey + ": "},
		{"the scheduler tenants are pinned to", "value: " + TenantSchedulerName},
		{
			"how the pool value is derived",
			"truncate(request.namespace, `" + strconv.Itoa(TenantIDLength) + "`)",
		},
		{
			"how long a pod survives an unreachable node",
			"tolerationSeconds: " + strconv.Itoa(UnreachableTolerationSeconds),
		},
	} {
		if !strings.Contains(policy, want.text) {
			t.Errorf("%s: the policy does not contain %q, so it and the Go constants "+
				"place tenant pods differently -- and the difference only shows up "+
				"once the webhook is gone", want.what, want.text)
		}
	}

	// ⚠️ And the other direction, which is the one that actually rots: the policy
	// naming a DIFFERENT scheduler somewhere while still naming ours elsewhere.
	// Looking only for our own string passes happily beside a contradicting one,
	// and the contradicting one is what a tenant's pods would get.
	for _, line := range strings.Split(policy, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "value: ") {
			continue
		}
		value := strings.TrimPrefix(trimmed, "value: ")
		if strings.HasSuffix(value, "-scheduler") && value != TenantSchedulerName {
			t.Errorf("the policy pins tenants to scheduler %q; Go says %q",
				value, TenantSchedulerName)
		}
	}
}
