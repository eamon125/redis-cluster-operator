## 编译operator镜像
```shell
$ operator-sdk build harbor.gzky.com/dongxy-projects/rds-cluster-operator:v5
```

## 编译redis docker镜像
```shell
# 需要把相关脚本编译后打包到镜像中
$ cd docker-library/redis-5.0
$ cd redis-tools && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o rds-tools && cd .. && mv redis-tools/rds-tools . && cd redis-rebalance && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o rds-rebalance && cd .. && mv redis-rebalance/rds-rebalance . && docker build -f Dockerfile -t harbor.gzky.com/dongxy-projects/rds:5.0.8 .
```
## 部署顺序
> yaml方式

### 基础部署
- deploy/service_account.yaml
- deploy/role.yaml
- deploy/role_binding.yaml
- deploy/crds/rds.cluster_rds_crd.yaml
- deploy/crds/rds.cluster_rdsrebalances_crd.yaml
- deploy/crds/rds.cluster_rdsbackups_crd.yaml
- deploy/crds/rds.cluster_rdsrestores_crd.yaml

### 部署operator（参考）
- deploy/operator.yaml

### 部署一个redis cluster实例（持久化仅支持nfs;参考）
- deploy/crds/rds.cluster_v1alpha1_rds_cr.yaml

### 部署一个rebalance redis cluster slot的实例（参考）
- deploy/crds/rds.cluster_v1alpha1_rdsrebalance_cr.yaml

### 部署一个备份 redis cluster slot的实例（单次；当前仅支持备份到对象存储minio;参考）
- deploy/crds/rds.cluster_v1alpha1_rdsbackup_once_cr.yaml

### 部署一个备份 redis cluster slot的实例（cron；当前仅支持备份到对象存储minio;参考）
- deploy/crds/rds.cluster_v1alpha1_rdsbackup_cron_cr.yaml

### 恢复集群（暂不支持原地恢复，需要先恢复数据，再创建新的集群实例）
- deploy/crds/rds.cluster_v1alpha1_rdsrestore_cr.yaml
- deploy/crds/rds.cluster_v1alpha1_rds_cr.yaml
