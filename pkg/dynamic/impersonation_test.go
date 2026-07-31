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

package dynamic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
	restclient "k8s.io/client-go/rest"
)

// TestEveryVerbImpersonates pins the invariant the whole authorization design
// rests on: kubezoo authorizes with AlwaysAllow and defers to upstream RBAC, and
// upstream can only apply the tenant's RBAC to a request that says who the
// tenant is. One client, one certificate, nine verbs -- a verb that forgets the
// header is authorized as kubezoo, which holds impersonate on everything.
//
// ⚠️ Patch did forget it, and nothing caught it because the per-verb tests all
// assert on the body and never on the headers. This test exists to be the thing
// that catches the next one, so it enumerates the verbs rather than testing one.
func TestEveryVerbImpersonates(t *testing.T) {
	const (
		tenantID = "111111"
		userName = "111111-admin"
	)

	gvr := schema.GroupVersionResource{Group: "gtest", Version: "vtest", Resource: "rtest"}
	obj := getObject("gtest/vtest", "rtest", "name")

	verbs := []struct {
		name string
		call func(ctx context.Context, c ResourceInterface) error
	}{
		{"Get", func(ctx context.Context, c ResourceInterface) error {
			_, err := c.Get(ctx, "name", metav1.GetOptions{})
			return err
		}},
		{"List", func(ctx context.Context, c ResourceInterface) error {
			_, err := c.List(ctx, metav1.ListOptions{})
			return err
		}},
		{"Watch", func(ctx context.Context, c ResourceInterface) error {
			w, err := c.Watch(ctx, metav1.ListOptions{})
			if w != nil {
				w.Stop()
			}
			return err
		}},
		{"Create", func(ctx context.Context, c ResourceInterface) error {
			_, err := c.Create(ctx, obj, metav1.CreateOptions{})
			return err
		}},
		{"Update", func(ctx context.Context, c ResourceInterface) error {
			_, _, err := c.Update(ctx, obj, metav1.UpdateOptions{})
			return err
		}},
		{"UpdateStatus", func(ctx context.Context, c ResourceInterface) error {
			_, err := c.UpdateStatus(ctx, obj, metav1.UpdateOptions{})
			return err
		}},
		{"Delete", func(ctx context.Context, c ResourceInterface) error {
			_, _, err := c.Delete(ctx, "name", metav1.DeleteOptions{})
			return err
		}},
		{"DeleteCollection", func(ctx context.Context, c ResourceInterface) error {
			_, err := c.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
			return err
		}},
		{"Patch", func(ctx context.Context, c ResourceInterface) error {
			_, _, err := c.Patch(ctx, "name", types.ApplyPatchType,
				[]byte(`{"apiVersion":"gtest/vtest","kind":"rtest"}`), metav1.PatchOptions{})
			return err
		}},
	}

	for _, verb := range verbs {
		t.Run(verb.name, func(t *testing.T) {
			var gotUser string
			var gotGroups []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				gotUser = req.Header.Get("Impersonate-User")
				gotGroups = req.Header.Values("Impersonate-Group")
				w.Header().Set("Content-Type", runtimeJSONContentType)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(getJSON("gtest/vtest", "rtest", "name"))
			}))
			defer srv.Close()

			cl, err := NewForConfig(&restclient.Config{Host: srv.URL})
			if err != nil {
				t.Fatal(err)
			}

			ctx := request.WithUser(context.Background(), &user.DefaultInfo{
				Name:   userName,
				Groups: []string{"system:authenticated"},
				Extra:  map[string][]string{"tenant": {tenantID}},
			})
			// List and Watch decode a list; the others decode an object. Only
			// the headers matter here, so a decode failure is not the assertion.
			_ = verb.call(ctx, cl.Resource(gvr).Namespace("111111-default"))

			if gotUser != userName {
				t.Errorf("%s did not impersonate the tenant: Impersonate-User = %q, want %q",
					verb.name, gotUser, userName)
			}
			if !containsString(gotGroups, "kubezoo:proxied:"+tenantID) {
				t.Errorf("%s did not carry the tenant's proxied group: Impersonate-Group = %v",
					verb.name, gotGroups)
			}
		})
	}
}

const runtimeJSONContentType = "application/json"

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

var _ = unstructured.Unstructured{}
