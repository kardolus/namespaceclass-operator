/*
Copyright 2025.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// NamespaceClassSpec defines the desired state of NamespaceClass
type NamespaceClassSpec struct {
	// Resources is a list of raw Kubernetes resources (e.g. NetworkPolicy, ServiceAccount)
	// that should be created in any namespace using this class.
	Resources []runtime.RawExtension `json:"resources,omitempty"`
}

// NamespaceClassStatus defines the observed state of NamespaceClass
type NamespaceClassStatus struct {
	// TODO INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// NamespaceClass is the Schema for the namespaceclasses API
type NamespaceClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NamespaceClassSpec   `json:"spec,omitempty"`
	Status NamespaceClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NamespaceClassList contains a list of NamespaceClass
type NamespaceClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NamespaceClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NamespaceClass{}, &NamespaceClassList{})
}
