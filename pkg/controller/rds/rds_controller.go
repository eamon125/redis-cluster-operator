package rds

import (
	"context"
	"fmt"
	"github.com/chenhg5/collection"
	"github.com/dongxiaoyi/rds-operator/pkg/utils"
	"k8s.io/client-go/util/retry"
	"strings"
	"time"

	rdsv1alpha1 "github.com/dongxiaoyi/rds-operator/pkg/apis/rds/v1alpha1"
	"github.com/dongxiaoyi/rds-operator/pkg/resources/services"
	"github.com/dongxiaoyi/rds-operator/pkg/resources/statefulset"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_rds")
var reconcileTime int
/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new RDS Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileRDS{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("rds-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource RDS
	err = c.Watch(&source.Kind{Type: &rdsv1alpha1.RDS{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner RDS
	// 使我们的RDS可以管理StatefulSet，否则StatefulSet不受我们的资源类型控制
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &rdsv1alpha1.RDS{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileRDS implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileRDS{}

// ReconcileRDS reconciles a RDS object
type ReconcileRDS struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

//var MASTER_BASE_SIZE int = 0

// Reconcile reads that state of the cluster for a RDS object and makes changes based on the state read
// and what is in the RDS.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileRDS) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling RDS")

	Logger := utils.NewLogger()

	// Fetch the RDS instance
	instance := &rdsv1alpha1.RDS{}
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

	if instance.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	// 资源的控制逻辑

	// 1.1 获取已经存在的statefulSet
	ss := &appsv1.StatefulSetList{}  // 空的ss列表对象
	labelsSS := labels.Set{  // label的Set对象，获取statefulset需要的过滤label
		"app": instance.Name+"-rds-cluster",
	}
	err = r.client.List(context.TODO(), ss, client.ListOption(&client.ListOptions{  // 根据labelsSS（过滤条件）去获取ss的具体信息
		Namespace:request.Namespace,
		LabelSelector: labels.SelectorFromSet(labelsSS),
	}))
	if err != nil {
		Logger.Warn("获取已经存在的StatefulSet失败！")
		Logger.Error(err)
		return reconcile.Result{}, err
	}
	existingSSName := make([]string, 0)  // 获取到正在运行的SatatefulSet的名称列表
	for _, s := range ss.Items {
		if s.DeletionTimestamp != nil {
			continue
		}
		name := s.Name
		existingSSName = append(existingSSName, name)
	}
	Logger.Infof("获取到已经存在的StatefulSet为：%v", existingSSName)
	specSSName := make([]string, 0)  // 期望的ss名称列表
	for i := 0; i < int(instance.Spec.MasterSize); i++ {
		name := fmt.Sprintf("%s-rds-ss-%v", instance.Name, i)
		specSSName = append(specSSName, name)
	}

	// 获取已经存在的svc
	existingSVC := &corev1.ServiceList{}
	labelsSVC := labels.Set{
		"svc": instance.Name+"-rds-headlessService",
	}
	err = r.client.List(context.TODO(), existingSVC, client.ListOption(&client.ListOptions{  // 根据labelsSS（过滤条件）去获取ss的具体信息
		Namespace:request.Namespace,
		LabelSelector: labels.SelectorFromSet(labelsSVC),
	}))
	if err != nil {
		Logger.Warn("获取已经存在的headlessService失败！")
		Logger.Error(err)
		return reconcile.Result{}, err
	}
	existingSVCName := make([]string, 0)  // 获取到正在运行的SatatefulSet的名称列表
	for _, svc := range existingSVC.Items {
		if svc.DeletionTimestamp != nil {
			continue
		}
		name := svc.Name
		existingSVCName = append(existingSVCName, name)
	}

	createEnv := false
	if len(existingSSName) == 0 {
		// 当检测到不存在已经运行的ss时，判断为首次初始化，则rds-ss-0的pod: rds-ss-0-0 应该执行redis-cli --cluster fix操作
		createEnv = true
	}

	// 比对期望的ss（masterSize）和运行中的ss：比对名称
	for i, existingSS := range existingSSName {
		if !collection.Collect(specSSName).Contains(existingSS) {
			// 如果已经运行的ss不在期望的ss名称列表中，delete: headlessService+statefulSet
			// (1) 删除svc
			deleteSVCName := fmt.Sprintf("%s-rds-svc-%v", instance.Name, i)
			if collection.Collect(existingSVCName).Contains(deleteSVCName) {
				// 如果需要删除的svc名称在已经运行的svc名称列表内，就删除
				for _, svc := range existingSVC.Items {
					if deleteSVCName == svc.Name {
						err = r.client.Delete(context.TODO(), &svc)
						if err != nil {
							Logger.Warnf("删除已经存在的headlessService [%v] 失败！", deleteSVCName)
							Logger.Error(err)
							//return reconcile.Result{}, err
						}
					}
				}
			}

			// (2) 删除ss
			// TODO: 删除成功，从exstingSS中删除
			deleteSS := ss.Items[i]
			err = r.client.Delete(context.TODO(), &deleteSS)
			if err != nil {
				Logger.Warnf("删除已经存在的StatefulSet [%v] 失败！", deleteSS)
				Logger.Error(err)
				//return reconcile.Result{}, err
			}
		}
	}

	/*
	// 初始化MASTER_BASE_SIZE：(1) MASTER_BASE_SIZE 数量首次获取到master_size 存放到内存中。（2）operator被干掉恢复后，根据当前的svc数量确定 MASTER_BASE_SIZE
	if MASTER_BASE_SIZE == 0 {
		if sn := len(existingSSName); sn != 0 {
			MASTER_BASE_SIZE = sn
		} else {
			MASTER_BASE_SIZE = int(instance.Spec.MasterSize)
		}
	}
	*/

	// 已经存在的ss字符串,用于传给初始化cluster脚本
	existingSSNameStr := ""
	for i, ss := range existingSSName {
		existingSSNameStr = strings.Join(existingSSName, ss)
		if i < len(existingSSName) -1 {
			existingSSNameStr = strings.Join(existingSSName, ",")
		}
	}
	if existingSSNameStr == "" {
		existingSSNameStr = "None"
	}
	Logger.Infof("已经存在的StatefulSet为：%v", existingSSNameStr)

	if instance.Spec.Storage == nil {
		Logger.Info("未配置持久化存储类型，使用默认类型：EmptyDir")
	}

	switch instance.Spec.Storage.Type {
	case rdsv1alpha1.EmptyStorage:
		Logger.Info("配置为非持久化存储类型：EmptyDir")
	case rdsv1alpha1.NfsStorage:
		// 创建ss之前先创建PVC
		getClaim := &corev1.PersistentVolumeClaim{}
		err = r.client.Get(context.TODO(), types.NamespacedName{
			Namespace: instance.Namespace,
			Name:      instance.Name,
		}, getClaim)
		// PVC不存在才会创建
		if err != nil {
			if errors.IsNotFound(err) {
				mode := corev1.PersistentVolumeFilesystem
				pvc := corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:   instance.Name + "-"+"rds-nfs-pvc",
						Namespace: instance.Namespace,
						Labels: map[string]string{
							"rds-cluster": instance.Name + "-rds-pvc",
						},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
						Resources: corev1.ResourceRequirements{
							Requests:corev1.ResourceList{
								corev1.ResourceStorage: instance.Spec.Storage.Size,
							},
						},
						StorageClassName: &instance.Spec.Storage.Class,
						VolumeMode: &mode,
					},
				}
				// 是否pvc与实例保持级联操作
				if instance.Spec.Storage.DeleteClaim {
					pvc.OwnerReferences = []metav1.OwnerReference{
						{
							APIVersion: rdsv1alpha1.SchemeGroupVersion.String(),
							Name:       instance.Name,
							UID:        instance.UID,
							Kind:       "RDS",
						},
					}
				}
				err = r.client.Create(context.TODO(), &pvc)
				if err != nil {
					Logger.Warnf("创建PVC [%v] 失败！", instance.Name + "-"+"rds-nfs-pvc")
					Logger.Error(err)
					// TODO: 判断初始集群与恢复集群
					//return reconcile.Result{}, err
				}
			}
		}
	default:
		Logger.Info("配置为非持久化存储类型：EmptyDir")
	}

	for i, specSS := range specSSName {
		if !collection.Collect(existingSSName).Contains(specSS) {
			// 如果我们期望的ss不在运行中的ss列表中，我们就创建：svc+ss
			// 创建svc
			// TODO: 判断svc是否存在，存在则不创建
			newSvc := services.NewSVC(instance, i)
			err = r.client.Create(context.TODO(), newSvc)
			if err != nil {
				Logger.Warnf("创建headlessSService [%s-rds-svc-%v] 失败！", instance.Name, i)
				Logger.Error(err)
				//return reconcile.Result{}, err
			}
			// 创建SS
			var newSS *appsv1.StatefulSet
			if i == 0 {
				newSS = statefulset.NewSS(instance, i, createEnv, existingSSNameStr)
			} else {
				newSS = statefulset.NewSS(instance, i, false, existingSSNameStr)
			}

			err = r.client.Create(context.TODO(), newSS)
			if err != nil {
				Logger.Warnf("创建SS [%v] 失败！", specSS)
				Logger.Error(err)
				// 如果ss创建失败，把svc删除
				errDeleteSvc := r.client.Delete(context.TODO(), newSvc)
				if errDeleteSvc != nil {
					Logger.Warnf("删除SVC [%v] 失败！", specSS)
					Logger.Error(errDeleteSvc)
				}
				return reconcile.Result{}, err
			}
			// 把已经创建的ss放到existingSS
			ss.Items = append(ss.Items, *newSS)
			// TODO: 更严格的创建顺序的标准
			time.Sleep(time.Duration(3) * time.Second)
		}
	}

	// 1.2 获取已经存在的Pod
	podList := &corev1.PodList{}
	labelsPod := labels.Set{
		"app": instance.Name+"-rds-cluster",
		"instance": instance.Name+"-rds-server",
	}
	err = r.client.List(context.TODO(), podList, client.ListOption(&client.ListOptions{  // 根据labelsSS（过滤条件）去获取ss的具体信息
		Namespace:request.Namespace,
		LabelSelector: labels.SelectorFromSet(labelsPod),
	}))
	if err != nil {
		Logger.Warn("获取已经存在的Pod列表失败！")
		Logger.Error(err)
		return reconcile.Result{}, err
	}

	var podInfoList []rdsv1alpha1.PodInfo
	var MatserSize int32 = 0
	var SlaveSize int32 = 0

	// 2. 更新instance的Status
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		podNameSplit := strings.Split(pod.Name, "-")
		roleNum := podNameSplit[len(podNameSplit)-1]
		role := "Slave"
		if roleNum == "0" {
			role = "Master"
			MatserSize += 1
		} else {
			SlaveSize += 1
		}

		podInfo := rdsv1alpha1.PodInfo{
			UID: pod.UID,
			PodName: pod.Name,
			PodIP: pod.Status.PodIP,
			Role: role,
			State: pod.Status.Phase,
		}
		podInfoList = append(podInfoList, podInfo)
	}
	Status := rdsv1alpha1.RDSStatus{
		MasterReplica:MatserSize,
		SlaveReplica:SlaveSize,
		PodInfoList:podInfoList,
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		instance.Status = Status
		return r.client.Status().Update(context.TODO(), instance)
	})
	if err != nil {
		Logger.Warn("更新cr的status失败！")
		Logger.Error(err.Error())
	}

	// 3. 同步Spec的字段和ss的Spec字段
	//
	for _, s := range ss.Items {
		if &s.CreationTimestamp != nil || s.DeletionTimestamp != nil {
			// 更新镜像地址
			s.Spec.Template.Spec.Containers[0].Image = instance.Spec.Image
			// 更新replica数量: 和slave数量相关（slaveSize+1）
			updateReplica := instance.Spec.ClusterReplica + 1
			s.Spec.Replicas = &updateReplica
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				return r.client.Update(context.TODO(), &s)
			})
			if err != nil {
				Logger.Warn("同步Spec的字段和ss的Spec字段失败！")
				Logger.Error(err.Error())
			}
		}
	}
	Logger.Info("本次循环结束！")

	return reconcile.Result{RequeueAfter: time.Duration(reconcileTime) * time.Second}, nil
}
