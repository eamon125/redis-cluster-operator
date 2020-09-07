package main

import (
	"fmt"
	"github.com/bitfield/script"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"strconv"
	"strings"
	"time"
)

var Logger *zap.SugaredLogger

var RdsOperator bool

// TODO:移除master pod时移除自身、关联slave；slave pod移除自身即可
// TODO: 平衡slot redis-cli -h %s.%s.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH --cluster rebalance ${THIS_NODE} --cluster-threshold 1 --cluster-use-empty-masters

func main() {
	RdsOperator = false

	CreateEnable := RdsCreateEnable() // 获取RDS_CREATE_ENABLE环境变量
	podName := GetPodName()
	podNameSlice := strings.Split(podName, "-")
	if podNameSlice[len(podNameSlice)-2] != "0" || podNameSlice[len(podNameSlice)-1] != "0" {
		CreateEnable = false
	}

	Logger = NewLogger()
	for {
		time.Sleep(time.Duration(1) * time.Second)

		if !checkRedisAlive() {
			Logger.Warn("等待redis node启动...")
		} else {
			Logger.Info("redis node 存活！")
			// 当$RDS_OPERATOR为false时，执行初始化集群/加入集群操作
			if !RdsOperator {
				if CreateEnable {
					// 初始化集群
					Logger.Info("正在初始化集群...")
					// 判断是否已存在可用redis节点-> svc 1-256搜索可用master，循环每10个一组；找到可用node，直接加入
					// TODO: 更可靠
					index, enable := RdsEnableNode()
					if !enable {
						createCluster()
					} else {
						// FIXME: 有可用node，直接加入???有问题
						fmt.Printf("等待加入可用集群。。。待修复, %v", index)
						RdsJoinMasterNode(index)
					}
				} else {
					// 加入集群操作
					Logger.Info("正在加入集群")
					// 此处分为添加master/slave
					if podNameSlice[len(podNameSlice)-1] != "0" {
						addClusterSlaveNode()
					} else {
						// svc的第一个索引pod为master
						addClusterMasterNode()
					}
				}
			} else {
				//  slave时判断其master是否fail，fail时移除当前slave，重新加入当前svc的master
				if podNameSlice[len(podNameSlice)-1] != "0" {
					fmt.Println("修复slave对应的master")
					fixBindMaster()
				}
			}
		}
	}
}

func fixBindMaster() {
	for {
		// 判断自己是否是slave
		instanceName := GetInstanceName()
		podName := GetPodName()
		podNameSlice := strings.Split(podName, "-")
		if podNameSlice[len(podNameSlice) -1 ] != "0" {
			// 获取master的ID:
			masterPodName := instanceName+"-rds-ss-"+podNameSlice[len(podNameSlice)-2]+"-0"
			//svcName := "rds-svc-"+podNameSlice[2]
			//masterIp := getIpByDns(masterPodName, true, svcName)
			masterId := getCurMasterID(masterPodName)
			// 获取slave对应master的ID: redis-cli -a $RDS_AUTH -h $HOSTNAME cluster nodes 2>/dev/null | grep ${THIS_NODE} |awk -F'[ ]+' '{print $3}'
			masterBindId := getSlaveBindMasterID()
			// 对比
			Logger.Infof("当前SVC的master的ID：[%s]; 当前slave绑定的master的ID：[%s]", masterId, masterBindId)
			if masterId != masterBindId {
				// 修复slave绑定的master
				cmdStr := fmt.Sprintf(`/bin/sh -c '/usr/local/bin/redis-cli  -h $RDS_POD_IP -a $RDS_AUTH cluster replicate %s'`,
					masterId)
				p := script.Exec(cmdStr)
				var exit int = p.ExitStatus()
				if exit != 0 {
					Logger.Warn("Slave对应的Master失败")
				} else {
					Logger.Infof("Slave对应的Master修复成功，当前Master ID: [%s]！", masterId)
				}

				output, _ := p.String()
				fmt.Println(output)
			}
		}
		time.Sleep(time.Duration(3) * time.Second)
	}
}

func getCurMasterID(masterPodName string) string {
	cmdStr := fmt.Sprintf(`/bin/sh -c "echo $(redis-cli -a $RDS_AUTH -h %s.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local cluster nodes 2>/dev/null | grep myself |awk -F'[ ]+' '{print $1}')"`, masterPodName)
	p := script.Exec(cmdStr)
	output, _ := p.String()
	return strings.Trim(output, "\n")
}

func getSlaveBindMasterID() string {
	cmdStr := fmt.Sprintf(`/bin/sh -c "redis-cli -a $RDS_AUTH -h $RDS_POD_IP cluster nodes 2>/dev/null | grep $RDS_POD_IP |awk -F'[ ]+' '{print $4}'"`)
	fmt.Println(cmdStr)
	p := script.Exec(cmdStr)
	output, _ := p.String()
	return strings.Trim(output, "\n")
}

func RdsJoinMasterNode(index int) {
	// TODO: 分配slot
	instanceName := GetInstanceName()
	enablePodName := instanceName + "-rds-ss-" + strconv.Itoa(index) + "-0"
	enableSvcName := instanceName + "-rds-svc-" + strconv.Itoa(index)
	enablePodIP := getIpByDns(enablePodName, false, enableSvcName)

	cmdStr := fmt.Sprintf(`/bin/sh -c "/usr/local/bin/redis-cli -h %s.%s.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH --cluster add-node $RDS_POD_IP:6379 %s:6379"`,
		enablePodName, enableSvcName, enablePodIP)
	p := script.Exec(cmdStr)
	var exit int = p.ExitStatus()
	if exit != 0 {
		Logger.Warnf("新Master加入集群 [%s] 失败！", enablePodIP)
	} else {
		Logger.Info("新Master加入集群/分配slot成功！")
		RdsOperator = true
	}
}

type SearchNode struct {
	index int
	enable bool
}

func checkEnableNode(index int, stopChan chan SearchNode) {
	instanceName := GetInstanceName()
	svcName := instanceName + "-rds-svc-" + strconv.Itoa(index)
	podName := instanceName + "-rds-ss-" + strconv.Itoa(index) + "-0"

	nodeInfo := SearchNode{
		index:index,
		enable:false,
	}
	// cluster nodes 2>/dev/null|wc -l
	cmdStr := fmt.Sprintf(`/bin/sh -c '/usr/local/bin/redis-cli -h %s.%s.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH ping'`, podName, svcName)
	p := script.Exec(cmdStr)
	var exit int = p.ExitStatus()
	//fmt.Sprintf("查看pod %s %s 是否可用", podName, svcName)
	if exit == 0 {
		nodeInfo.enable = true
	}
	stopChan <- nodeInfo
}

func RdsEnableNode() (int, bool) {
	// 返回可用master node的svc索引，和是否可用的boolean值
	var nodeIndex int
	var enable bool
	loop:
		for i:= 0; i <= 26; i++ {
			j := i * 10
			stopChan := make(chan SearchNode)
			count := 0
			var index int
			for k := 1; k <= 10; k++ {
				index = k + j
				if index <= 256 {
					// 判断条件为是否ping通
					go checkEnableNode(index, stopChan)
				}
			}
			loopWait:
				for {
					select {
					case s := <- stopChan:
						if !s.enable {
							count += 1
							if index < 250 {
								if count == 10 {
									break loopWait
								}
							} else {
								if count == 6 {
									break loopWait
								}
							}
						} else {
							nodeIndex = s.index
							enable = s.enable
							break loop
						}
					case <-time.After(time.Duration(10) * time.Second):  // 10秒超时
					fmt.Println("超时")
						break loop
					}
				}
		}
		return nodeIndex, enable
}

// TODO: 替换为流式查看操作信息
func createCluster() {
	// 判断pod名称来判断是否需要初始化集群/等待加入集群
	podName := GetPodName()
	podNameSlice := strings.Split(podName, "-")
	if podNameSlice[len(podNameSlice)-2] == "0" && podNameSlice[len(podNameSlice)-1] == "0" {
		// 执行redis-cli --cluster fix指令创建单节点集群
		p := script.Exec("/bin/sh -c 'echo yes |/usr/local/bin/redis-cli -h $RDS_POD_IP -a $RDS_AUTH --cluster fix $RDS_POD_IP:6379'")
		var exit int = p.ExitStatus()
		if exit != 0 {
			Logger.Warn("创建集群失败！")
		} else {
			Logger.Info("创建集群成功！")
			RdsOperator = true
		}
	} else {
		// TODO: 等待加入集群
		Logger.Info("等待加入集群")
	}
}

func GetPodName() string {
	p := script.Exec("/bin/sh -c '/bin/echo $HOSTNAME'")
	output, _ := p.String()
	return strings.Trim(output, "\n")
}

func GetInstanceName() string {
	p := script.Exec("/bin/sh -c '/bin/echo $RDS_INSTANCE_NAME'")
	output, _ := p.String()
	return strings.Trim(output, "\n")
}

func addClusterSlaveNode() {
	instanceName := GetInstanceName()
	podName := GetPodName()
	podNameSlice := strings.Split(podName, "-")
	masterPodName := instanceName+ "-" + podNameSlice[len(podNameSlice)-4] + "-" + podNameSlice[len(podNameSlice)-3] + "-" + podNameSlice[len(podNameSlice)-2] + "-" + "0"
	masterIP := getIpByDns(masterPodName, true, "")
	Logger.Infof("获取到Master IP: %s", masterIP)
	// 获取master的cluster-master-id
	masterID := getMasterID(masterPodName)
	Logger.Infof("获取到的master id：%s", masterID)
	cmdStr := fmt.Sprintf(`/bin/sh -c '/usr/local/bin/redis-cli -h %s.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH --cluster add-node $RDS_POD_IP:6379 %s:6379 --cluster-slave --cluster-master-id %s'`,
		masterPodName, masterIP, masterID)
	fmt.Println(cmdStr)
	p := script.Exec(cmdStr)
	var exit int = p.ExitStatus()
	if exit != 0 {
		Logger.Warn("新Slave加入集群失败！")
	} else {
		Logger.Info("新Slave加入集群成功！")
		RdsOperator = true
	}
}

func getMasterID(masterHost string) string {
	// /usr/local/bin/redis-cli -h rds-ss-0-1.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH cluster nodes|grep myself,master
	cmdStr := fmt.Sprintf(`/bin/sh -c '/usr/local/bin/redis-cli -h %s.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH cluster nodes 2>/dev/null|grep myself,master'`, masterHost)
	var strList []string
	loop:
		for {
			p := script.Exec(cmdStr)
			output, err := p.String()
			if err == nil {
				str := strings.Trim(output, "\n")
				strList = strings.Fields(strings.TrimSpace(str))
				break loop
			}

		}

	return strList[0]
}

func addClusterMasterNode() {
	// 判断自己的podname尾索引>0，则加入索引0 的集群
	// TODO: 动态获取
	instanceName := GetInstanceName()
	masterPodName := instanceName+"-rds-ss-0-0"
	// 获取svc下第一个node的ip
	masterIP := getIpByDns(masterPodName, false, "")
	Logger.Infof("获取到Master IP: %s", masterIP)
	cmdStr := fmt.Sprintf(`/bin/sh -c '/usr/local/bin/redis-cli -h %s.rds-svc-0.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH --cluster add-node $RDS_POD_IP:6379 %s:6379'`,
			masterPodName, masterIP)
	fmt.Println(cmdStr)
	p := script.Exec(cmdStr)
	var exit int = p.ExitStatus()
	if exit != 0 {
		Logger.Warn("新Master加入集群失败！")
	} else {
		Logger.Info("新Master加入集群成功！")
		RdsOperator = true
	}
}

func RdsCreateEnable() bool {
	p := script.Exec("/bin/sh -c '/bin/echo $RDS_CREATE_ENABLE'")
	output, _ := p.String()
	if strings.Trim(output, "\n") == "true" {
		return true
	}
	return false
}

func getIpByDns(hostname string, isCurNamespace bool, svcName string) string {
	// nslookup hostname.svc.namespace.svc.cluster.local|grep Address:|tail -1|awk  '{print $2}'
	var cmdStr string
	instanceName := GetInstanceName()
	if isCurNamespace {
		cmdStr = fmt.Sprintf(`/bin/sh -c 'nslookup %s.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local|grep Address:|tail -1 |awk "{print $2}"'`, hostname)
	} else if svcName == "" {
		// TODO: 动态获取
		cmdStr = fmt.Sprintf(`/bin/sh -c 'nslookup %s.%s-rds-svc-0.$RDS_NAMESPACE.svc.cluster.local|grep Address:|tail -1 |awk "{print $2}"'`, hostname, instanceName)
	} else {
		cmdStr = fmt.Sprintf(`/bin/sh -c 'nslookup %s.%s.$RDS_NAMESPACE.svc.cluster.local|grep Address:|tail -1 |awk "{print $2}"'`, hostname, svcName)
	}
	//fmt.Println(cmdStr)
	p := script.Exec(cmdStr)
	output, _ := p.String()
	str := strings.Trim(output, "\n")
	strList := strings.Fields(strings.TrimSpace(str))

	return strings.Split(strList[len(strList)-1], ":")[0]
}

func checkRedisAlive() bool {
	p := script.Exec("/bin/sh -c '/usr/local/bin/redis-cli -h $RDS_POD_IP -a $RDS_AUTH ping'")
	var exit int = p.ExitStatus()
	if exit != 0 {
		return false
	}
	return true
}


// 日志器
func LogLevel() map[string]zapcore.Level {
	level := make(map[string]zapcore.Level)
	level["debug"] = zap.DebugLevel
	level["info"] = zap.InfoLevel
	level["warn"] = zap.WarnLevel
	level["error"] = zap.ErrorLevel
	level["dpanic"] = zap.DPanicLevel
	level["panic"] = zap.PanicLevel
	level["fatal"] = zap.FatalLevel
	return level
}

// 初始化日志
func NewLogger() *zap.SugaredLogger {
	logLevelOpt := "DEBUG" // 日志级别
	levelMap := LogLevel()
	logLevel, _ := levelMap[logLevelOpt]
	atomicLevel := zap.NewAtomicLevelAt(logLevel)

	encodingConfig := zapcore.EncoderConfig{
		TimeKey: "Time",
		LevelKey: "Level",
		NameKey: "Log",
		CallerKey: "Celler",
		MessageKey: "Message",
		StacktraceKey: "Stacktrace",
		LineEnding: zapcore.DefaultLineEnding,
		EncodeLevel: zapcore.LowercaseLevelEncoder,
		EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.Format("[2006-01-02 15:04:05]"))
		},
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller: zapcore.FullCallerEncoder,
	}
	var outPath []string
	var errPath []string
	outPath = append(outPath, "stdout")
	errPath = append(outPath, "stderr")

	logCfg := zap.Config{
		Level: atomicLevel,
		Development: true,
		DisableCaller: true,
		DisableStacktrace: true,
		Encoding:"console",
		EncoderConfig: encodingConfig,
		// InitialFields: map[string]interface{}{filedKey: fieldValue},
		OutputPaths: outPath,
		ErrorOutputPaths: errPath,
	}

	logger, _ := logCfg.Build()
	Logger = logger.Sugar()
	return Logger
}

