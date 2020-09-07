package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RdsbackUpSpec defines the desired state of RdsbackUp
type RdsbackupSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	Image string `json:"image"`
	Instance string `json:"instance"`
	Storage *RdsInstanceStorage   `json:"instance_storage"` // 目前仅支持持久化方式的备份
	BackupStorage *RdsBackupStorage `json:"backup_storage"`
	CronJobString string `json:"cron_job_string,omitempty"`  // 当job_type为cron时需要填写
	JobType string `json:"job_type"`  // cron or once
}

type RdsInstanceStorage struct {
	Type        StorageType       `json:"type"`
	Class       string            `json:"class"`
}

type BackUpType string


type RdsBackupStorage struct {
	Type BackUpType `json:"type"`
	Endpoint string `json:"endpoint"` // ip:port
	Bucket string `json:"bucket"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
	SSL bool `json:"ssl,omitempty"`
}

// RdsbackUpStatus defines the observed state of RdsbackUp
type RdsbackupStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RdsbackUp is the Schema for the rdsbackups API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=rdsbackups,scope=Namespaced
type Rdsbackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RdsbackupSpec   `json:"spec,omitempty"`
	Status RdsbackupStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RdsbackUpList contains a list of RdsbackUp
type RdsbackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Rdsbackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Rdsbackup{}, &RdsbackupList{})
}
