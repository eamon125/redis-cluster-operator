package services

import (
	"fmt"
	rdsv1alpha1 "github.com/dongxiaoyi/rds-operator/pkg/apis/rds/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func NewSVC(instance *rdsv1alpha1.RDS, index int) *corev1.Service {
	name := fmt.Sprintf("%s-rds-svc-%v", instance.Name, index)
	ssName := fmt.Sprintf("%s-rds-ss-%v", instance.Name, index)
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:"RDS",
			APIVersion:"rds.cluster/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"svc": name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(instance, schema.GroupVersionKind{
					Group:   rdsv1alpha1.SchemeGroupVersion.Group,
					Version: rdsv1alpha1.SchemeGroupVersion.Version,
					Kind:    "RDS",
				}),
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				corev1.ServicePort{
					Name:"client",
					Port: 6379,
				},
				corev1.ServicePort{
					Name:"gossip",
					Port: 16379,
				},
			},
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				"app": instance.Name + "-rds-cluster",
				"instance": instance.Name + "-rds-server",
				"ss": ssName,
			},
		},
	}
}
