#!/bin/zsh
docker rmi harbor.gzky.com/dongxy-projects/rds-cluster-operator:v5 &&  operator-sdk build harbor.gzky.com/dongxy-projects/rds-cluster-operator:v5 &&  docker push harbor.gzky.com/dongxy-projects/rds-cluster-operator:v5 
