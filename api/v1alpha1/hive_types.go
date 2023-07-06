/*
Copyright 2023.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// HiveSpec defines the desired state of Hive
type HiveSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Size defines the number of Hive instances
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3
	// +kubebuilder:validation:ExclusiveMaximum=false
	Size              int32          `json:"size"`
	BaseImageVersion  string         `json:"baseImageVersion,omitempty"`
	HiveMetastoreUris string         `json:"hiveMetastoreUris"`
	Service           ServiceSpec    `json:"service,omitempty"`
	Deployment        DeploymentSpec `json:"deployment"`
	DbConnection      DbConnection   `json:"dbConnection"`
}

// HiveStatus defines the observed state of Hive
type HiveStatus struct {
	// Represents the observations of a Hive's current state.
	// Hive.status.conditions.type are: "Available", "Progressing", and "Degraded"
	// Hive.status.conditions.status are one of True, False, Unknown.
	// Hive.status.conditions.reason the value should be a CamelCase string and producers of specific
	// condition types may define expected values and meanings for this field, and whether the values
	// are considered a guaranteed API.
	// Hive.status.conditions.Message is a human readable message indicating details about the transition.
	// For further information see: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

type DeploymentSpec struct {
	EnvVar    EnvVar                      `json:"envVar"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

type EnvVar struct {
	//HIVE_DB_EXTERNAL string `json:"HIVE_DB_EXTERNAL,omitempty"`
	HIVE_DB_NAME string `json:"HIVE_DB_NAME"`
}

type Resources struct {
	MemoryRequest string `json:"memoryRequest,omitempty"`
	CpuRequest    string `json:"cpuRequest,omitempty"`
	MemoryLimit   string `json:"memoryLimit,omitempty"`
	Cpulimit      string `json:"cpulimit,omitempty"`
}

type ServiceSpec struct {
	ThriftPort     int32 `json:"thriftPort,omitempty"`
	HiveServerPort int32 `json:"hiveServerPort,omitempty"`
}

type DbConnection struct {
	//DbType   string `json:"dbType,omitempty"`
	UserName string `json:"userName"`
	PassWord string `json:"passWord"`
	Url      string `json:"url"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Hive is the Schema for the hives API
type Hive struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HiveSpec   `json:"spec,omitempty"`
	Status HiveStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// HiveList contains a list of Hive
type HiveList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Hive `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Hive{}, &HiveList{})
}
