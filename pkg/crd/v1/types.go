package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ConsulResourcePlural = "consuls"

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Consul struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              ConsulSpec `json:"spec"`
}

type ConsulSpec struct {
	Size uint `json:"size"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ConsulList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Consul `json:"items"`
}
