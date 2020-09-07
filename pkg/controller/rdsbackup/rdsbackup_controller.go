package rdsbackup

import (
	"context"
	rdsv1alpha1 "github.com/dongxiaoyi/rds-operator/pkg/apis/rds/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strconv"
)

var log = logf.Log.WithName("controller_rdsbackup")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new RdsBackUp Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileRdsBackup{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("rdsbackup-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource RdsBackUp
	err = c.Watch(&source.Kind{Type: &rdsv1alpha1.Rdsbackup{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner RdsBackUp
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &rdsv1alpha1.Rdsbackup{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileRdsBackUp implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileRdsBackup{}

// ReconcileRdsBackUp reconciles a RdsBackUp object
type ReconcileRdsBackup struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a RdsBackUp object and makes changes based on the state read
// and what is in the RdsBackUp.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileRdsBackup) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling RdsBackup")

	// Fetch the RdsBackUp instance
	instance := &rdsv1alpha1.Rdsbackup{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Define a new Pod object
	pod := newPodForCR(instance)

	// Set RdsBackUp instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, pod, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Pod already exists
	found := &corev1.Pod{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		err = r.client.Create(context.TODO(), pod)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Pod created successfully - don't requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	// Pod already exists - don't requeue
	reqLogger.Info("Skip reconcile: Pod already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)
	return reconcile.Result{}, nil
}

// newPodForCR returns a busybox pod with the same name/namespace as the cr
func newPodForCR(cr *rdsv1alpha1.Rdsbackup) *corev1.Pod {
	labels := map[string]string{
		"app": cr.Name,
	}

	volume := redisDataVolume(cr)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-pod-rds-backup",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				*volume,
			},
			RestartPolicy: corev1.RestartPolicyOnFailure,
			Containers: []corev1.Container{
				{
					Name:    "redis-backup",
					Image:   cr.Spec.Image,
					ImagePullPolicy: corev1.PullAlways,
					Command: []string{
						"/bin/sh",
						"-c",
						"rds-dump",
					},
					VolumeMounts: []corev1.VolumeMount{
						corev1.VolumeMount{
							Name:      "datadir",
							MountPath: "/data",
						},
					},
					Env: []corev1.EnvVar{
						corev1.EnvVar{
							Name: "RDS_NAMESPACE",
							Value: cr.Namespace,
						},
						corev1.EnvVar{
							Name: "RDS_CRON_STRING",
							Value: cr.Spec.CronJobString,
						},
						corev1.EnvVar{
							Name: "RDS_BACKUP_STORAGE_TYPE",
							Value: string(cr.Spec.BackupStorage.Type),
						},
						corev1.EnvVar{
							Name: "RDS_BACKUP_STORAGE_ENDPOINT",
							Value: string(cr.Spec.BackupStorage.Endpoint),
						},
						corev1.EnvVar{
							Name: "RDS_BACKUP_STORAGE_BUCKET",
							Value: string(cr.Spec.BackupStorage.Bucket),
						},
						corev1.EnvVar{
							Name: "RDS_BACKUP_STORAGE_ACCESSKEY",
							Value: string(cr.Spec.BackupStorage.AccessKey),
						},
						corev1.EnvVar{
							Name: "RDS_BACKUP_STORAGE_SECRETKEY",
							Value: string(cr.Spec.BackupStorage.SecretKey),
						},
						corev1.EnvVar{
							Name: "RDS_BACKUP_STORAGE_SSL",
							Value: strconv.FormatBool(cr.Spec.BackupStorage.SSL),
						},
						corev1.EnvVar{
							Name: "RDS_BACKUP_JOB_TYPE",
							Value: cr.Spec.JobType,
						},
					},
				},
			},
		},
	}
}

func nfsVolume(instance *rdsv1alpha1.Rdsbackup) *corev1.Volume {
	return &corev1.Volume{
		Name:"datadir",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: instance.Spec.Instance + "-"+"rds-nfs-pvc",
			},
		},
	}
}

// 当前仅支持redis的cr持久化存储为nfs的数据备份
func redisDataVolume(instance *rdsv1alpha1.Rdsbackup) *corev1.Volume {
	// This will find the volumed desired by the user. If no volume defined
	// an EmptyDir will be used by default
	switch instance.Spec.Storage.Type {
	case rdsv1alpha1.NfsStorage:
		return nfsVolume(instance)
	default:
		return nfsVolume(instance)
	}
}