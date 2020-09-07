package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/api/resource"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RDSSpec defines the desired state of RDS
type RDSSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	Image           string `json:"image"`
	MasterSize      int32 `json:"masterSize"`
	ClusterReplica  int32 `json:"clusterReplica"`
	RDSConfig       RedisConfig `json:"redisConfig"`
	Storage         *RdsStorage                `json:"storage,omitempty"`
	Resources       corev1.ResourceRequirements `json:"resources,omitempty"`
}

type RedisConfig struct {
	MasterAuth              string `json:"masterauth"`
	RenameCommandConfig     string `json:"rename-command-config"`
	ReplBacklogSize         string `json:"repl-backlog-size"`
	RequirePass             string `json:"requirepass"`
	MaxMemory               string `json:"maxmemory"`
	MaxMemoryPolicy         string `json:"maxmemory-policy"`
	ClusterNodeTimeout      string `json:"cluster-node-timeout"`
	ClientOutputBufferLimit string `json:"client-output-buffer-limit"`
}

type StorageType string

const (
	NfsStorage StorageType  = "nfs"
	EmptyStorage StorageType = "empty"
)

type RdsStorage struct {
	Size        resource.Quantity `json:"size"`
	Type        StorageType       `json:"type"`
	Class       string            `json:"class"`
	DeleteClaim bool			  `json:"delete-claim"`
}

// RDSStatus defines the observed state of RDS
type RDSStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	MasterReplica int32 `json:"masterReplica"`
	SlaveReplica int32 `json:"slaveReplica"`
	// TODO: 已经准备好的master数量、slave数量   1/3
	PodInfoList []PodInfo `json:"podInfoList"`
}

type PodInfo struct {
	UID     types.UID `json:"uid"`
	PodName string `json:"podName"`
	PodIP   string `json:"podIP"`
	Role    string `json:"role"`
	State   corev1.PodPhase `json:"state"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RDS is the Schema for the rds API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=rds,scope=Namespaced
type RDS struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RDSSpec   `json:"spec,omitempty"`
	Status RDSStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RDSList contains a list of RDS
type RDSList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RDS `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RDS{}, &RDSList{})
}
