// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package object

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestCreateObjMetadata(t *testing.T) {
	tests := map[string]struct {
		namespace string
		name      string
		gk        schema.GroupKind
		expected  string
		isError   bool
	}{
		"Empty name is an error": {
			namespace: "test-namespace ",
			name:      "",
			gk: schema.GroupKind{
				Group: "apps",
				Kind:  "ReplicaSet",
			},
			expected: "",
			isError:  true,
		},
		"Empty GroupKind is an error": {
			namespace: "test-namespace",
			name:      "test-name",
			gk:        schema.GroupKind{},
			expected:  "",
			isError:   true,
		},
		"Colon is allowed in the name for RBAC resources": {
			namespace: "test-namespace",
			name:      "system::kube-scheduler",
			gk: schema.GroupKind{
				Group: rbacv1.GroupName,
				Kind:  "Role",
			},
			expected: "test-namespace_system____kube-scheduler_rbac.authorization.k8s.io_Role",
			isError:  false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			inv, err := CreateObjMetadata(tc.namespace, tc.name, tc.gk)
			if !tc.isError {
				if err != nil {
					t.Errorf("Error creating ObjMetadata when it should have worked.")
				} else if tc.expected != inv.String() {
					t.Errorf("Expected inventory\n(%s) != created inventory\n(%s)\n", tc.expected, inv.String())
				}
				// Parsing back the just created inventory string to ObjMetadata,
				// so that tests will catch any change to CreateObjMetadata that
				// would break ParseObjMetadata.
				expectedObjMetadata := &ObjMetadata{
					Namespace: tc.namespace,
					Name:      tc.name,
					GroupKind: tc.gk,
				}
				actual, err := ParseObjMetadata(inv.String())
				if err != nil {
					t.Errorf("Error parsing back ObjMetadata, when it should have worked.")
				} else if !expectedObjMetadata.Equals(&actual) {
					t.Errorf("Expected inventory (%s) != parsed inventory (%s)\n", expectedObjMetadata, actual)
				}
			}
			if tc.isError && err == nil {
				t.Errorf("Should have returned an error in CreateObjMetadata()")
			}
		})
	}
}

func TestObjMetadataEquals(t *testing.T) {
	testCases := map[string]struct {
		objMeta1     *ObjMetadata
		objMeta2     *ObjMetadata
		expectEquals bool
	}{
		"parameter is nil": {
			objMeta1: &ObjMetadata{
				GroupKind: schema.GroupKind{
					Group: "apps",
					Kind:  "Deployment",
				},
				Name:      "dep",
				Namespace: "default",
			},
			objMeta2:     nil,
			expectEquals: false,
		},
		"different groupKind": {
			objMeta1: &ObjMetadata{
				GroupKind: schema.GroupKind{
					Group: "apps",
					Kind:  "StatefulSet",
				},
				Name:      "dep",
				Namespace: "default",
			},
			objMeta2: &ObjMetadata{
				GroupKind: schema.GroupKind{
					Group: "apps",
					Kind:  "Deployment",
				},
				Name:      "dep",
				Namespace: "default",
			},
			expectEquals: false,
		},
		"both are missing groupKind": {
			objMeta1: &ObjMetadata{
				Name:      "dep",
				Namespace: "default",
			},
			objMeta2: &ObjMetadata{
				Name:      "dep",
				Namespace: "default",
			},
			expectEquals: true,
		},
		"they are equal": {
			objMeta1: &ObjMetadata{
				GroupKind: schema.GroupKind{
					Group: "apps",
					Kind:  "Deployment",
				},
				Name:      "dep",
				Namespace: "default",
			},
			objMeta2: &ObjMetadata{
				GroupKind: schema.GroupKind{
					Group: "apps",
					Kind:  "Deployment",
				},
				Name:      "dep",
				Namespace: "default",
			},
			expectEquals: true,
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			equal := tc.objMeta1.Equals(tc.objMeta2)

			if tc.expectEquals && !equal {
				t.Error("Expected objMetas to be equal, but they weren't")
			}

			if !tc.expectEquals && equal {
				t.Error("Expected objMetas not to be equal, but they were")
			}
		})
	}
}

func TestParseObjMetadata(t *testing.T) {
	tests := map[string]struct {
		invStr    string
		inventory *ObjMetadata
		isError   bool
	}{
		"Simple inventory string parse with empty namespace": {
			invStr: "_test-name_apps_ReplicaSet",
			inventory: &ObjMetadata{
				Name: "test-name",
				GroupKind: schema.GroupKind{
					Group: "apps",
					Kind:  "ReplicaSet",
				},
			},
			isError: false,
		},
		"Basic inventory string parse": {
			invStr: "test-namespace_test-name_apps_Deployment",
			inventory: &ObjMetadata{
				Namespace: "test-namespace",
				Name:      "test-name",
				GroupKind: schema.GroupKind{
					Group: "apps",
					Kind:  "Deployment",
				},
			},
			isError: false,
		},
		"RBAC resources can have colon (double underscore) in their name": {
			invStr: "test-namespace_kubeadm__nodes-kubeadm-config_rbac.authorization.k8s.io_Role",
			inventory: &ObjMetadata{
				Namespace: "test-namespace",
				Name:      "kubeadm:nodes-kubeadm-config",
				GroupKind: schema.GroupKind{
					Group: rbacv1.GroupName,
					Kind:  "Role",
				},
			},
			isError: false,
		},
		"RBAC resources can have double colon (double underscore) in their name": {
			invStr: "test-namespace_system____leader-locking-kube-scheduler_rbac.authorization.k8s.io_Role",
			inventory: &ObjMetadata{
				Namespace: "test-namespace",
				Name:      "system::leader-locking-kube-scheduler",
				GroupKind: schema.GroupKind{
					Group: rbacv1.GroupName,
					Kind:  "Role",
				},
			},
			isError: false,
		},
		"Test double underscore (colon) at beginning of name": {
			invStr: "test-namespace___leader-locking-kube-scheduler_rbac.authorization.k8s.io_ClusterRole",
			inventory: &ObjMetadata{
				Namespace: "test-namespace",
				Name:      ":leader-locking-kube-scheduler",
				GroupKind: schema.GroupKind{
					Group: rbacv1.GroupName,
					Kind:  "ClusterRole",
				},
			},
			isError: false,
		},
		"Test double underscore (colon) at end of name": {
			invStr: "test-namespace_leader-locking-kube-scheduler___rbac.authorization.k8s.io_RoleBinding",
			inventory: &ObjMetadata{
				Namespace: "test-namespace",
				Name:      "leader-locking-kube-scheduler:",
				GroupKind: schema.GroupKind{
					Group: rbacv1.GroupName,
					Kind:  "RoleBinding",
				},
			},
			isError: false,
		},
		"Not enough fields -- error": {
			invStr:    "_test-name_apps",
			inventory: &ObjMetadata{},
			isError:   true,
		},
		"Only one field (no separators) -- error": {
			invStr:    "test-namespacetest-nametest-grouptest-kind",
			inventory: &ObjMetadata{},
			isError:   true,
		},
		"Too many fields": {
			invStr:    "test-namespace_test-name_apps_foo_Deployment",
			inventory: &ObjMetadata{},
			isError:   true,
		},
	}

	for tn, tc := range tests {
		t.Run(tn, func(t *testing.T) {
			actual, err := ParseObjMetadata(tc.invStr)
			if !tc.isError {
				if err != nil {
					t.Errorf("Error parsing inventory when it should have worked: %s", err)
				} else if !tc.inventory.Equals(&actual) {
					t.Errorf("Expected inventory (%s) != parsed inventory (%s)\n", tc.inventory, actual)
				}
			}
			if tc.isError && err == nil {
				t.Errorf("Should have returned an error in ParseObjMetadata()")
			}
		})
	}
}

var objMeta1 = ObjMetadata{
	GroupKind: schema.GroupKind{
		Group: "apps",
		Kind:  "Deployment",
	},
	Name:      "dep",
	Namespace: "default",
}

var objMeta2 = ObjMetadata{
	GroupKind: schema.GroupKind{
		Group: "apps",
		Kind:  "StatefulSet",
	},
	Name:      "dep",
	Namespace: "default",
}

var objMeta3 = ObjMetadata{
	GroupKind: schema.GroupKind{
		Group: "",
		Kind:  "Pod",
	},
	Name:      "pod-a",
	Namespace: "default",
}

var objMeta4 = ObjMetadata{
	GroupKind: schema.GroupKind{
		Group: "",
		Kind:  "Pod",
	},
	Name:      "pod-b",
	Namespace: "default",
}

func TestHash(t *testing.T) {
	tests := map[string]struct {
		objs     []ObjMetadata
		expected string
	}{
		"No objects gives valid hash": {
			objs:     []ObjMetadata{},
			expected: "811c9dc5",
		},
		"Single object gives valid hash": {
			objs:     []ObjMetadata{objMeta1},
			expected: "3715cd95",
		},
		"Multiple objects gives valid hash": {
			objs:     []ObjMetadata{objMeta1, objMeta2, objMeta3},
			expected: "d69d726a",
		},
		"Different ordering gives same hash": {
			objs:     []ObjMetadata{objMeta2, objMeta3, objMeta1},
			expected: "d69d726a",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			actual, err := Hash(tc.objs)
			if err != nil {
				t.Fatalf("Received unexpected error: %s", err)
			}
			if tc.expected != actual {
				t.Errorf("expected hash string (%s), got (%s)", tc.expected, actual)
			}
		})
	}
}

func TestSetDiff(t *testing.T) {
	testCases := map[string]struct {
		setA     []ObjMetadata
		setB     []ObjMetadata
		expected []ObjMetadata
	}{
		"Empty sets results in empty diff": {
			setA:     []ObjMetadata{},
			setB:     []ObjMetadata{},
			expected: []ObjMetadata{},
		},
		"Empty subtraction set results in same set": {
			setA:     []ObjMetadata{objMeta1, objMeta3},
			setB:     []ObjMetadata{},
			expected: []ObjMetadata{objMeta1, objMeta3},
		},
		"Empty initial set results in empty diff": {
			setA:     []ObjMetadata{},
			setB:     []ObjMetadata{objMeta1, objMeta3},
			expected: []ObjMetadata{},
		},
		"Sets equal results in empty diff": {
			setA:     []ObjMetadata{objMeta2, objMeta1},
			setB:     []ObjMetadata{objMeta1, objMeta2},
			expected: []ObjMetadata{},
		},
		"Basic diff": {
			setA:     []ObjMetadata{objMeta2, objMeta1},
			setB:     []ObjMetadata{objMeta1},
			expected: []ObjMetadata{objMeta2},
		},
		"Subtract non-elements results in no change": {
			setA:     []ObjMetadata{objMeta1},
			setB:     []ObjMetadata{objMeta3, objMeta4},
			expected: []ObjMetadata{objMeta1},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			actual := SetDiff(tc.setA, tc.setB)
			if !SetEquals(tc.expected, actual) {
				t.Errorf("SetDiff expected set (%s), got (%s)", tc.expected, actual)
			}
		})
	}
}

func TestUnion(t *testing.T) {
	testCases := map[string]struct {
		setA     []ObjMetadata
		setB     []ObjMetadata
		expected []ObjMetadata
	}{
		"Empty sets results in empty union": {
			setA:     []ObjMetadata{},
			setB:     []ObjMetadata{},
			expected: []ObjMetadata{},
		},
		"Empty second set results in same set": {
			setA:     []ObjMetadata{objMeta1, objMeta3},
			setB:     []ObjMetadata{},
			expected: []ObjMetadata{objMeta1, objMeta3},
		},
		"Empty initial set results in empty diff": {
			setA:     []ObjMetadata{},
			setB:     []ObjMetadata{objMeta1, objMeta3},
			expected: []ObjMetadata{objMeta1, objMeta3},
		},
		"Same sets in different order results in same set": {
			setA:     []ObjMetadata{objMeta2, objMeta1},
			setB:     []ObjMetadata{objMeta1, objMeta2},
			expected: []ObjMetadata{objMeta1, objMeta2},
		},
		"One item overlap": {
			setA:     []ObjMetadata{objMeta2, objMeta1},
			setB:     []ObjMetadata{objMeta1, objMeta3},
			expected: []ObjMetadata{objMeta1, objMeta2, objMeta3},
		},
		"Disjoint sets results in larger set": {
			setA:     []ObjMetadata{objMeta1, objMeta2},
			setB:     []ObjMetadata{objMeta3, objMeta4},
			expected: []ObjMetadata{objMeta1, objMeta2, objMeta3, objMeta4},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			actual := Union(tc.setA, tc.setB)
			if !SetEquals(tc.expected, actual) {
				t.Errorf("SetDiff expected set (%s), got (%s)", tc.expected, actual)
			}
		})
	}
}

func TestSetEquals(t *testing.T) {
	testCases := map[string]struct {
		setA    []ObjMetadata
		setB    []ObjMetadata
		isEqual bool
	}{
		"Empty sets results in empty union": {
			setA:    []ObjMetadata{},
			setB:    []ObjMetadata{},
			isEqual: true,
		},
		"Empty second set results in same set": {
			setA:    []ObjMetadata{objMeta1, objMeta3},
			setB:    []ObjMetadata{},
			isEqual: false,
		},
		"Empty initial set results in empty diff": {
			setA:    []ObjMetadata{},
			setB:    []ObjMetadata{objMeta1, objMeta3},
			isEqual: false,
		},
		"Different ordering are equal sets": {
			setA:    []ObjMetadata{objMeta2, objMeta1},
			setB:    []ObjMetadata{objMeta1, objMeta2},
			isEqual: true,
		},
		"One item overlap": {
			setA:    []ObjMetadata{objMeta2, objMeta1},
			setB:    []ObjMetadata{objMeta1, objMeta3},
			isEqual: false,
		},
		"Disjoint sets results in larger set": {
			setA:    []ObjMetadata{objMeta1, objMeta2},
			setB:    []ObjMetadata{objMeta3, objMeta4},
			isEqual: false,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			actual := SetEquals(tc.setA, tc.setB)
			if tc.isEqual != actual {
				t.Errorf("SetEqual expected (%t), got (%t)", tc.isEqual, actual)
			}
		})
	}
}
