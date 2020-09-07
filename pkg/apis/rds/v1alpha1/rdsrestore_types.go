package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RdsRestoreSpec defines the desired state of RdsRestore
type RdsRestoreSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	Image string `json:"image"`
	InstancePVCType StorageType `json:"instance_pvc_type"`  // 目前只支持nfs
	Instance string `json:"instance"`
	RestoreStorage *RdsRestoreStorage `json:"restore_storage"`
	Storage         *RdsRestorePvcStorage                `json:"storage,omitempty"`

}

// RdsRestoreStatus defines the observed state of RdsRestore
type RdsRestoreStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

type RestoreType string

type RdsRestoreStorage struct {
	Type RestoreType `json:"type"`
	Endpoint string `json:"endpoint"` // ip:port
	Bucket string `json:"bucket"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
	SSL bool `json:"ssl,omitempty"`
}

type RdsRestorePvcStorage struct {
	Size        resource.Quantity `json:"size"`
	Type        StorageType       `json:"type"`
	Class       string            `json:"class"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RdsRestore is the Schema for the rdsrestores API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=rdsrestores,scope=Namespaced
type RdsRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RdsRestoreSpec   `json:"spec,omitempty"`
	Status RdsRestoreStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RdsRestoreList contains a list of RdsRestore
type RdsRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RdsRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RdsRestore{}, &RdsRestoreList{})
}
