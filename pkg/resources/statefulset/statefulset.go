package statefulset

import (
	"fmt"
	rdsv1alpha1 "github.com/dongxiaoyi/rds-operator/pkg/apis/rds/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func NewSS(instance *rdsv1alpha1.RDS, index int, createEnv bool, existingSSNameStr string) *appsv1.StatefulSet {
	createEnable := "false"  // 是否需要create redis cluster
	if createEnv {
		createEnable = "true"
	}

	name := fmt.Sprintf("%s-rds-ss-%v", instance.Name, index)
	svcName := fmt.Sprintf("%s-rds-svc-%v", instance.Name, index)
	replicas := instance.Spec.ClusterReplica + 1
	// 只有当持久化数据目录不存在时才会创建；配置文件会重新生成/重定向
	configCmd := fmt.Sprintf(`
if [ ! -d /data/${HOSTNAME} ]; then mkdir /data/${HOSTNAME};else rm -f /data/$HOSTNAME/redis.pid;fi; echo bind $RDS_POD_IP > /data/$HOSTNAME/redis-cluster.conf && echo -e "protected-mode yes
port 6379
tcp-backlog 32768
timeout 0
tcp-keepalive 300
daemonize no
supervised no
pidfile /data/$HOSTNAME/redis.pid
loglevel notice
databases 16
always-show-logo yes
save 900 1
save 300 10
save 60 3000
stop-writes-on-bgsave-error yes
rdbcompression yes
rdbchecksum yes
dbfilename redis_dump_6379.rdb
dir /data/$HOSTNAME
masterauth '%s'
slave-serve-stale-data yes
rename-command KEYS     ''
rename-command FLUSHALL ''
rename-command FLUSHDB  ''
rename-command CONFIG   '%s'
slave-read-only yes
repl-diskless-sync no
repl-diskless-sync-delay 5
repl-ping-slave-period 10
repl-timeout 60
repl-disable-tcp-nodelay no
repl-backlog-size %s
repl-backlog-ttl 3600
slave-priority 100
min-slaves-to-write 0
min-slaves-max-lag 10
requirepass '%s'
maxclients 10000
maxmemory %s
maxmemory-policy %s
lazyfree-lazy-eviction no
lazyfree-lazy-expire no
lazyfree-lazy-server-del no
slave-lazy-flush no
appendonly no
appendfilename 'appendonly.aof'
appendfsync everysec
no-appendfsync-on-rewrite no
auto-aof-rewrite-percentage 100
auto-aof-rewrite-min-size 128mb
aof-load-truncated yes
aof-use-rdb-preamble no
lua-time-limit 5000
cluster-enabled yes
cluster-config-file redis-cluster-nodes-6379.conf
cluster-node-timeout %s
cluster-slave-validity-factor 10
cluster-migration-barrier 1
cluster-require-full-coverage yes
slowlog-log-slower-than 10000
slowlog-max-len 128
latency-monitor-threshold 0
notify-keyspace-events ''
hash-max-ziplist-entries 512
hash-max-ziplist-value 2048
list-max-ziplist-size -2
list-compress-depth 0
set-max-intset-entries 512
zset-max-ziplist-entries 512
zset-max-ziplist-value 512
hll-sparse-max-bytes 3000
activerehashing yes
client-output-buffer-limit %s
client-output-buffer-limit pubsub 32mb 8mb 60
hz 10
aof-rewrite-incremental-fsync yes" >> /data/$HOSTNAME/redis-cluster.conf`,
		instance.Spec.RDSConfig.MasterAuth,
		instance.Spec.RDSConfig.RenameCommandConfig,
		instance.Spec.RDSConfig.ReplBacklogSize,
		instance.Spec.RDSConfig.RequirePass,
		instance.Spec.RDSConfig.MaxMemory,
		instance.Spec.RDSConfig.MaxMemoryPolicy,
		instance.Spec.RDSConfig.ClusterNodeTimeout,
		instance.Spec.RDSConfig.ClientOutputBufferLimit,
	)

	// (1) 缩容的时候slavedel-node失败（master是单线程的，并发操作可能在reshard处夯住，导致slave移除失败）
	// 解决以上问题方案 - > master等待自己没有slave后再行 - > 迁移 - > 移除
	// 缩容时无需删除自己的工作目录(进程丢失后容器会结束，无法再进行删除工作目录操作，可在创建目录前先执行删除操作)

	lifyCycleCmd := fmt.Sprintf(`SLOT_COUNT=$(/usr/local/bin/redis-cli  -a $RDS_AUTH --cluster info ${RDS_POD_IP}:6379 2>/dev/null | grep "${RDS_POD_IP}" | awk -F'[|]' '{print $2}'|awk -F'[ ]+' '{print $2}'); THIS_ID=$(/usr/local/bin/redis-cli -h $HOSTNAME.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH cluster nodes 2>/dev/null|grep myself|head -1|awk -F'[ ]+' '{print $1}');MOVE_TO=$(/usr/local/bin/redis-cli -h $HOSTNAME.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH cluster nodes 2>/dev/null|grep master|grep -v myself,master|head -1|awk -F'[ ]+' '{print $1}');MOVE_NODE=$(/usr/local/bin/redis-cli -h $HOSTNAME.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH cluster nodes 2>/dev/null|grep master|grep -v myself,master|head -1|awk -F'[ ]+' '{print $2}'|awk -F@ '{print $1}'|awk -F: '{print$1}');FROM_NODE=$(/usr/local/bin/redis-cli -h $HOSTNAME.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH cluster nodes 2>/dev/null|grep myself,master|awk -F'[ ]+' '{print $1}'); if [[ $(echo $HOSTNAME|awk -F'[-]+' '{print $NF}') == 0 ]];then if [[ ${SLOT_COUNT} == 0 ]]; then redis-cli -a ${MOVE_NODE} --cluster del-node ${RDS_POD_IP}:6379 ${THIS_ID}; else while true; do if [[ $(redis-cli -h example-rds-ss-0-0 -a $RDS_AUTH cluster nodes 2> /dev/null|grep "$THIS_ID"|grep -v myself| wc -l) == 0 ]];then redis-cli -h $HOSTNAME -a $RDS_AUTH --cluster reshard ${RDS_POD_IP}:6379 --cluster-from ${FROM_NODE} --cluster-to ${MOVE_TO} --cluster-slots ${SLOT_COUNT}  --cluster-yes --cluster-timeout 5000 --cluster-pipeline 100 ;sleep 1; redis-cli -a $RDS_AUTH -h ${MOVE_NODE} --cluster del-node ${RDS_POD_IP}:6379 ${THIS_ID};fi;done;fi; else redis-cli -a $RDS_AUTH -h ${MOVE_NODE} --cluster del-node ${RDS_POD_IP}:6379 ${THIS_ID}; fi`)
	// FIXME: 初始化节点可用性判定条件修复??目前有问题
	readnessProbeCmd := fmt.Sprintf(`SLOT_COUNT=$(/usr/local/bin/redis-cli  -a $RDS_AUTH --cluster info ${RDS_POD_IP}:6379 2>/dev/null | grep "${RDS_POD_IP}" | awk -F'[|]' '{print $2}'|awk -F'[ ]+' '{print $2}'); RDS_INDEX=$(echo $HOSTNAME|awk -F'[-]' '{print $NF}'); RDS_SLAVE_COUNT=$(/usr/local/bin/redis-cli  -a $RDS_AUTH -h $HOSTNAME cluster nodes 2>/dev/null|grep myself,slave|wc -l);MASTER_COUNT=$(/usr/local/bin/redis-cli -h $HOSTNAME.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH cluster nodes 2>/dev/null|wc -l);  if [[ $RDS_INDEX == 0 ]]; then if [[ ${SLOT_COUNT} == 0 ]]; then if [[ $HOSTNAME == $RDS_INSTANCE_NAME-rds-ss-0-0 ]]; then if [[ $(/usr/local/bin/redis-cli  -a $RDS_AUTH --cluster info ${RDS_POD_IP}:6379 2>/dev/null | grep $RDS_POD_IP | grep slots | awk -F'[ ]+' '{print $7}') == 0 ]]; then echo success master; else echo success master;fi; else if [[ ${MASTER_COUNT} == 1 ]]; then exit 1; else echo success master;fi;fi; else echo success master;fi ; else if [[ $RDS_SLAVE_COUNT == 0 ]] ; then exit 1; else echo success slave; fi; fi`)

	volume := redisDataVolume(instance)

	var terminationGracePeriodSeconds int64 = 100
	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:"RDS",
			APIVersion:"rds.cluster/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app": instance.Name+"-rds-cluster",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(instance, schema.GroupVersionKind{
					Group:   rdsv1alpha1.SchemeGroupVersion.Group,
					Version: rdsv1alpha1.SchemeGroupVersion.Version,
					Kind:    "RDS",
				}),
			},
		},
		Spec:appsv1.StatefulSetSpec{
			Replicas: &replicas,
			ServiceName: svcName,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": instance.Name+"-rds-cluster",
					"instance": instance.Name+"-rds-server",
					"ss": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta:metav1.ObjectMeta{
					Name: "rds",
					Namespace:instance.Namespace,
					Labels: map[string]string{
						"app": instance.Name+"-rds-cluster",
						"instance": instance.Name+"-rds-server",
						"ss": name,
					},
				},
				Spec:corev1.PodSpec{
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					InitContainers:[]corev1.Container{
						corev1.Container{
							Name:"rds-init",
							Image:instance.Spec.Image,
							Env: []corev1.EnvVar{
								corev1.EnvVar{
									Name: "RDS_POD_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath:"status.podIP",
										},
									},
								},
							},
							Command:[]string{
								"/bin/sh",
								"-c",
								configCmd,
							},
							VolumeMounts:[]corev1.VolumeMount{
								corev1.VolumeMount{
									Name: "datadir",
									MountPath: "/data",
								},
							},
						},
					},
					Containers:[]corev1.Container{
						corev1.Container{
							Name:  "rds-server",
							Image: instance.Spec.Image,
							ImagePullPolicy: corev1.PullAlways,
							Command: []string{
								"/bin/sh",
								"-c",
								"redis-server /data/$HOSTNAME/redis-cluster.conf",
							},
							Lifecycle: &corev1.Lifecycle{
								PreStop: &corev1.Handler{
									Exec: &corev1.ExecAction{
										Command: []string{
											"/bin/sh",
											"-c",
											lifyCycleCmd,
										},
									},
								},
							},
							Env: []corev1.EnvVar{
								corev1.EnvVar{
									Name: "RDS_POD_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath:"status.podIP",
										},
									},
								},
								corev1.EnvVar{
									Name: "RDS_AUTH",  // rds 密码
									Value: instance.Spec.RDSConfig.MasterAuth,
								},
								corev1.EnvVar{
									Name: "RDS_CREATE_ENABLE", // rds是否需要create集群
									Value: createEnable,
								},
								corev1.EnvVar{
									Name: "RDS_EXISTING_SS",  // 已经存在的可用的ss，不存在值为None
									Value: existingSSNameStr,
								},
								corev1.EnvVar{
									Name: "RDS_SVC_NAME",
									Value: svcName,
								},
								corev1.EnvVar{
									Name: "RDS_NAMESPACE",
									Value: instance.Namespace,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								corev1.VolumeMount{
									Name:      "datadir",
									MountPath: "/data",
								},
							},
							Ports: []corev1.ContainerPort{
								corev1.ContainerPort{
									Name:          "client",
									ContainerPort: 6379,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: instance.Spec.Resources,
							LivenessProbe: &corev1.Probe{
								PeriodSeconds: 5,
								Handler:corev1.Handler{
									Exec: &corev1.ExecAction{
										Command: []string{
											"/bin/sh",
											"-c",
											"/usr/local/bin/redis-cli -h $RDS_POD_IP -a $RDS_AUTH ping",
										},
									},
								},
							},
							// TODO: 判断自己成功加入到了集群:
							//  (1) master: 可用slot不为0？ 或者master数量不为1？
							//  (2) slave: 显示为slave角色

							ReadinessProbe: &corev1.Probe{
								PeriodSeconds: 5,
								Handler:corev1.Handler{
									Exec: &corev1.ExecAction{
										Command: []string{
											"/bin/sh",
											"-c",
											readnessProbeCmd,
										},
									},
								},
							},
						},

						corev1.Container{
							Name:  "rds-tools",
							Image: instance.Spec.Image,
							ImagePullPolicy: corev1.PullAlways,
							Command: []string{
								"/bin/sh",
								"-c",
								"rds-tools",
							},
							Env: []corev1.EnvVar{
								corev1.EnvVar{
									Name: "RDS_POD_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath:"status.podIP",
										},
									},
								},
								corev1.EnvVar{
									Name: "RDS_AUTH",  // rds 密码
									Value: instance.Spec.RDSConfig.MasterAuth,
								},
								corev1.EnvVar{
									Name: "RDS_CREATE_ENABLE", // rds是否需要create集群
									Value: createEnable,
								},
								corev1.EnvVar{
									Name: "RDS_EXISTING_SS",  // 已经存在的可用的ss，不存在值为None
									Value: existingSSNameStr,
								},
								corev1.EnvVar{
									Name: "RDS_SVC_NAME",
									Value: svcName,
								},
								corev1.EnvVar{
									Name: "RDS_NAMESPACE",
									Value: instance.Namespace,
								},
								corev1.EnvVar{
									Name: "RDS_INSTANCE_NAME",
									Value: instance.Name,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						*volume,
					},
				},
			},
		},
	}
}

func emptyVolume() *corev1.Volume {
	return &corev1.Volume{
		Name: "datadir",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

func nfsVolume(instance *rdsv1alpha1.RDS) *corev1.Volume {
	return &corev1.Volume{
		Name:"datadir",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: instance.Name + "-"+"rds-nfs-pvc",
			},
		},
	}
}

// 数据持久化卷
func redisDataVolume(instance *rdsv1alpha1.RDS) *corev1.Volume {
	// This will find the volumed desired by the user. If no volume defined
	// an EmptyDir will be used by default
	if instance.Spec.Storage == nil {
		return emptyVolume()
	}

	switch instance.Spec.Storage.Type {
	case rdsv1alpha1.EmptyStorage:
		return emptyVolume()
	case rdsv1alpha1.NfsStorage:
		return nfsVolume(instance)
	default:
		return emptyVolume()
	}
}
