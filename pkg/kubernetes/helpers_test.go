/*
Copyright 2020 The Kubermatic Kubernetes Platform contributors.

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

package kubernetes

import (
	"strings"
	"testing"

	"github.com/go-test/deep"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateToken(t *testing.T) {
	tokenA := GenerateToken()
	tokenB := GenerateToken()

	if len(tokenA) == 0 {
		t.Error("generated token is empty")
	}

	if tokenA == tokenB {
		t.Errorf("two new tokens should not be identical, but are: '%s'", tokenA)
	}

	if err := ValidateKubernetesToken(tokenA); err != nil {
		t.Errorf("generated token is malformed: %v", err)
	}
}

func makeRef(s string) metav1.OwnerReference {
	parts := strings.SplitN(s, "/", 3)
	name := ""
	if len(parts) >= 3 {
		name = parts[2]
	}

	return metav1.OwnerReference{
		APIVersion: parts[0],
		Kind:       parts[1],
		Name:       name,
	}
}

func makeRefs(s ...string) []metav1.OwnerReference {
	result := []metav1.OwnerReference{}

	for _, i := range s {
		result = append(result, makeRef(i))
	}

	return result
}

func TestRemoveOwnerReferences(t *testing.T) {
	startRefs := makeRefs("core/pod/a", "core/pod/2", "core/configmap/a", "core/configmap/x")

	testcases := []struct {
		name         string
		toRemove     []metav1.OwnerReference
		expectedRefs []metav1.OwnerReference
	}{
		{
			name:         "nop",
			toRemove:     makeRefs(),
			expectedRefs: startRefs,
		},
		{
			name:         "a simple test case",
			toRemove:     makeRefs("core/pod/a", "core/configmap/x"),
			expectedRefs: makeRefs("core/pod/2", "core/configmap/a"),
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			fakeObj := &corev1.Pod{}
			fakeObj.SetOwnerReferences(startRefs)

			RemoveOwnerReferences(fakeObj, testcase.toRemove...)

			if diff := deep.Equal(fakeObj.OwnerReferences, testcase.expectedRefs); diff != nil {
				t.Fatal(diff)
			}
		})
	}
}

func TestRemoveOwnerReferenceKinds(t *testing.T) {
	startRefs := makeRefs("core/pod/a", "core/pod/2", "core/configmap/a", "core/configmap/x")

	testcases := []struct {
		name         string
		toRemove     []metav1.OwnerReference
		expectedRefs []metav1.OwnerReference
	}{
		{
			name:         "nop",
			toRemove:     makeRefs(),
			expectedRefs: startRefs,
		},
		{
			name:         "name should be ignored",
			toRemove:     makeRefs("core/pod/ignoreme"),
			expectedRefs: makeRefs("core/configmap/a", "core/configmap/x"),
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			fakeObj := &corev1.Pod{}
			fakeObj.SetOwnerReferences(startRefs)

			RemoveOwnerReferenceKinds(fakeObj, testcase.toRemove...)

			if diff := deep.Equal(fakeObj.OwnerReferences, testcase.expectedRefs); diff != nil {
				t.Fatal(diff)
			}
		})
	}
}
