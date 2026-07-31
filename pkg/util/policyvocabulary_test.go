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
	"regexp"
	"strings"
	"testing"
)

// The policies in config/policy work out which tenant a request belongs to, in
// CEL and in JMESPath, and this package works it out in Go. Three expressions of
// one rule, and nothing makes them agree.
//
// They did disagree. Every policy took the text before the first dash while
// GetTenantIDFromNamespace takes the first TenantIDLength bytes -- and
// ValidateTenantName deliberately allows a dash inside a tenant id, so for a
// tenant called ab-123 the two answers differed. The hostname policy then
// computed the same subdomain for ab-123 and ab-456, reopening exactly the
// cross-tenant theft it exists to prevent; the placement policy injected a pool
// that no node carries, leaving those pods unschedulable with no admission
// error; and the namespace policy handed pods a name that gets prefixed twice.
//
// None of that fails to parse, and no Go code reads these files, so nothing was
// going to notice. This does.
func TestPoliciesDeriveTheTenantTheSameWayGoDoes(t *testing.T) {
	dir := filepath.Join("..", "..", "config", "policy")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the policies: %v", err)
	}

	// What each language has to say to mean "the tenant id of this namespace",
	// given that the id is the first TenantIDLength bytes.
	wantCEL := "request.namespace.substring(0, " + itoa(TenantIDLength) + ")"
	wantJMESPath := "truncate(request.namespace, `" + itoa(TenantIDLength) + "`)"

	// Ways of saying "up to the first dash", which is the wrong rule however it
	// is spelled.
	//
	// ⚠️ These describe the RULE, not the three strings that were removed when it
	// was first fixed. The first version of this test was a transcription of
	// those three, including the CEL method form with double quotes -- and the
	// one policy that still had the bug wrote it with single quotes, which is the
	// house style everywhere else in these files. The test passed, the bug sat
	// there, and the backstop below was satisfied by the files that had been
	// fixed. An enumeration of past mistakes cannot catch the next one.
	firstDash := []*regexp.Regexp{
		// JMESPath: split(<anything>, '-') / split(<anything>, "-")
		regexp.MustCompile(`split\([^)]*,\s*['"]-['"]\)`),
		// CEL: <anything>.split('-') / .split("-"), on a namespace or a username
		regexp.MustCompile(`\.split\(['"]-['"]\)`),
		// Regex form: strip everything up to the first dash
		regexp.MustCompile(`\^\[\^-\]\+-`),
	}

	derives := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}
		text := string(body)

		for _, wrong := range firstDash {
			// Comments explain why the wrong rule is wrong, so only look at what
			// the policy actually evaluates.
			if wrong.MatchString(stripComments(text)) {
				t.Errorf("%s derives the tenant from the text before the first dash. "+
					"A tenant id may contain a dash -- see ValidateTenantName -- so that is a "+
					"different tenant from the one Go resolves. Use %q (CEL) or %q (JMESPath).",
					entry.Name(), wantCEL, wantJMESPath)
			}
		}
		if strings.Contains(text, wantCEL) || strings.Contains(text, wantJMESPath) {
			derives++
		}
	}

	// If the spellings above stop appearing, either the policies changed shape or
	// this test is checking nothing. Both are worth failing for.
	if derives == 0 {
		t.Errorf("no policy derives the tenant id the way this package does; either the "+
			"policies now spell it differently, in which case update %q and %q here, or "+
			"this test has quietly stopped covering anything", wantCEL, wantJMESPath)
	}
}

// stripComments removes YAML comments so that a comment explaining a mistake is
// not mistaken for the mistake.
func stripComments(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
