package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RdsRebalanceSpec defines the desired state of RdsRebalance
type RdsRebalanceSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	Image string `json:"image"`
	MasterAuth string `json:"masterauth"`
	InstanceName string `json:"instance"`
	// TODO: 根据不同的redis集群实例做rebalance（当前暂未自定义生成svc和ss名称）
}

// RdsRebalanceStatus defines the observed state of RdsRebalance
type RdsRebalanceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RdsRebalance is the Schema for the rdsrebalances API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=rdsrebalances,scope=Namespaced
type RdsRebalance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RdsRebalanceSpec   `json:"spec,omitempty"`
	Status RdsRebalanceStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RdsRebalanceList contains a list of RdsRebalance
type RdsRebalanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RdsRebalance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RdsRebalance{}, &RdsRebalanceList{})
}
