/*
Copyright 2021.

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
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TkcElkSpec defines the desired state of TkcElk
type TkcElkSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	EsIpAddress string `json:"esipaddress,omitempty"`
}

type StepStatus struct {
	Step     string `json:"step"`
	Status   string `json:"status"`
	ErrorMsg string `json:"errormsg,omitempty"`
}

// TkcElkStatus defines the observed state of TkcElk
type TkcElkStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	StepStatusDetails []StepStatus `json:"stepstatusdetails,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TkcElk is the Schema for the tkcelks API
type TkcElk struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TkcElkSpec   `json:"spec,omitempty"`
	Status TkcElkStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TkcElkList contains a list of TkcElk
type TkcElkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TkcElk `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TkcElk{}, &TkcElkList{})
}
