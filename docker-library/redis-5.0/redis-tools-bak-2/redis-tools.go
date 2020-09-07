package main

import (
	"fmt"
	"context"
	"github.com/bitfield/script"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"strconv"
	"strings"
	"time"
)

var Logger *zap.SugaredLogger

var RdsOperator bool

// TODO: 暂不考虑节点异常情况下节点恢复：对持久化数据做判断
// TODO: 考虑缩容后再扩容的情景：只要index为0的节点有slot即是已执行过create，只需要将节点操作join到集群
func main() {
	RdsOperator = true

	podName := GetPodName()
	podNameSlice := strings.Split(podName, "-")
	SvcIndex, _ := strconv.Atoi(podNameSlice[2])
	podIndex, _ := strconv.Atoi(podNameSlice[3])
	masterBaseSize, _ := strconv.Atoi(GetRdsMasterBaseSize())

	Logger = NewLogger()

	for {
		time.Sleep(time.Duration(1) * time.Second)

		if !checkRedisAlive() {
			Logger.Warn("等待当前节点redis进程启动...")
		} else {

			Logger.Info("当前节点redis进程已启动")
			if RdsOperator {
				// 节点角色根据svc分为两种：init节点与join节点
				// TODO：判断是否非首次创建，非首次需要节点恢复
				// 如果节点角色为init节点：（1）svc名称在 < RDS_MASTER_BASE_SIZE环境变量 的情况下，且为首次创建
				if SvcIndex < masterBaseSize {
					// index为0的节点的master，等待其他节点可用后，执行create cluster操作：需要判断条件（其他的master均无slot；master数量可以对得上，对不上的情况下肯定是做过缩容了，只需要join即可）
					if podIndex == 0 {
						// ping其他index节点（每个master包含一个从节点），全部ping通后执行create
						// TODO: 不适用于集群节点恢复，集群恢复时会重新执行创建集群操作
						createCluster(masterBaseSize)
					} else if podIndex == 1 {
						// 本节点是第一个slave的，如果本svc的master有slot，即加入本svc的master；本svc的master没有slot的情况跳过（可能是需要create）
						// xxx 本slave有master的话，是否是本svc的master，不是的话重新加入
						Logger.Info("当前节点为第一个slave，首次只需被动创建加入集群，忽略本节点操作！")
					} else {
						// 从节点大于1的加入本svc所有的master（因为初始化集群的时候每个master至少需要1个slave节点）
						addSlave(SvcIndex)
					}
				} else {
					// 如果节点角色为join节点：（1）svc名称在 < RDS_MASTER_BASE_SIZE环境变量 的情况下，且非首次创建（2）svc名称在 > RDS_MASTER_BASE_SIZE环境变量
					// 随机选择init节点的index节点做加入操作，直到可以加入为止
					if podIndex > 0 {
						// 只要是从节点都是加入本svc所有的master
						addSlave(SvcIndex)
					} else {
						// master节点需要通过index为0的节点加入
						addMaster()
					}
				}
			} else {
				Logger.Info("已执行过集群化操作，监听节点状态...")
				// （1）master意外退出后，当前slave节点挂载master修复
				if podIndex > 0 {
					fixBindMaster()
				}
			}
		}
	}
}

func getCurMasterID(masterPodName string) string {
	cmdStr := fmt.Sprintf(`/bin/sh -c "echo $(redis-cli -a $RDS_AUTH -h %s.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local cluster nodes 2>/dev/null | grep myself |awk -F'[ ]+' '{print $1}')"`, masterPodName)
	p := script.Exec(cmdStr)
	output, _ := p.String()
	return strings.Trim(output, "\n")
}


func addSlave(SvcIndex int) {
	masterPodName := "rds-ss-"+strconv.Itoa(SvcIndex)+"-0"
	masterSvcname := "rds-svc-" + strconv.Itoa(SvcIndex)
	masterId := getCurMasterID(masterPodName)
	masterIP := getIpByDns(masterPodName, true, masterSvcname)
	cmdStr := fmt.Sprintf(`/bin/sh -c '/usr/local/bin/redis-cli -h %s.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH --cluster add-node $RDS_POD_IP:6379 %s:6379 --cluster-slave --cluster-master-id %s'`,
		masterPodName, masterIP, masterId)
	p := script.Exec(cmdStr)
	var exit int = p.ExitStatus()
	if exit != 0 {
		Logger.Warn("新Slave加入集群失败！")
	} else {
		Logger.Info("新Slave加入集群成功！")
		RdsOperator = true
	}
}

func addMaster() {
	masterPodName := "rds-ss-0-0"
	// 获取svc下第一个node的ip
	masterIP := getIpByDns(masterPodName, false, "")
	Logger.Infof("获取到Master IP: %s", masterIP)
	cmdStr := fmt.Sprintf(`/bin/sh -c '/usr/local/bin/redis-cli -h %s.rds-svc-0.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH --cluster add-node $RDS_POD_IP:6379 %s:6379'`,
		masterPodName, masterIP)

	p := script.Exec(cmdStr)
	var exit int = p.ExitStatus()
	if exit != 0 {
		Logger.Warn("新Master加入集群失败！")
	} else {
		Logger.Info("新Master加入集群成功！")
		RdsOperator = true
	}
}

func createCluster(index int) {
	var countPingSuccess int
	done := make(chan bool)
	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < index; i++ {
		for j := 0; j <= 2; j++ {
			go pingRedisInitNode(ctx, done, i, j)
		}
	}

	loop:
		for {
			select {
			case d := <- done:
				if d {
					countPingSuccess += 1
				}
				fmt.Println(countPingSuccess, index)
				if countPingSuccess == index {
					cancel()
					break loop
				}
			}
		}
	Logger.Info("所有初始化节点准备就绪，准备创建redis集群...")
	// redis-cli --cluster create 127.0.0.1:7000 127.0.0.1:7001 127.0.0.1:7002 127.0.0.1:7003 127.0.0.1:7004 127.0.0.1:7005 --cluster-replicas 1
	cmdStr := "/bin/sh -c '/usr/local/bin/redis-cli --cluster create "
	port := "6379"
	for i := 0; i < index; i++ {
		for j := 0; j <= 2; j++ {
			podName := "rds-ss-"+strconv.Itoa(i)+strconv.Itoa(j)
			svcName := "rds-svc-"+strconv.Itoa(i)
			ip := getIpByDns(podName, false, svcName)
			cmdStr += ip+":"+port + " "
		}
	}
	cmdStr += "--cluster-replicas 1"
	p := script.Exec(cmdStr)
	var exit int = p.ExitStatus()
	if exit != 0 {
		Logger.Error("集群创建失败！")
	}
	Logger.Info("集群创建成功！")
	// 下一次循环时不再走这里
	RdsOperator = false
}

func fixBindMaster() {
	for {
		// 判断自己是否是slave
		podName := GetPodName()
		podNameSlice := strings.Split(podName, "-")
		if podNameSlice[len(podNameSlice) -1 ] != "0" {
			// 获取master的ID:
			masterPodName := "rds-ss-"+podNameSlice[2]+"-0"
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
				fmt.Println(cmdStr)
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

func getSlaveBindMasterID() string {
	cmdStr := fmt.Sprintf(`/bin/sh -c "redis-cli -a $RDS_AUTH -h $RDS_POD_IP cluster nodes 2>/dev/null | grep $RDS_POD_IP |awk -F'[ ]+' '{print $4}'"`)
	fmt.Println(cmdStr)
	p := script.Exec(cmdStr)
	output, _ := p.String()
	return strings.Trim(output, "\n")
}

func pingRedisInitNode(ctx context.Context, done chan bool, indexSvc, indexPod int) {
	for {
		select {
		case <- ctx.Done():
			return
		default:
			podName := "rds-ss-"+strconv.Itoa(indexSvc)+strconv.Itoa(indexPod)
			svcName := "rds-svc-"+strconv.Itoa(indexSvc)
			done <- pingRedis(podName, svcName)

		}
	}
}


func GetPodName() string {
	p := script.Exec("/bin/sh -c '/bin/echo $HOSTNAME'")
	output, _ := p.String()
	return strings.Trim(output, "\n")
}

func GetRdsMasterBaseSize() string {
	p := script.Exec("/bin/sh -c '/bin/echo $RDS_MASTER_BASE_SIZE'")
	output, _ := p.String()
	return strings.Trim(output, "\n")
}

func getIpByDns(hostname string, isCurNamespace bool, svcName string) string {
	// nslookup hostname.svc.namespace.svc.cluster.local|grep Address:|tail -1|awk  '{print $2}'
	var cmdStr string
	if isCurNamespace {
		cmdStr = fmt.Sprintf(`/bin/sh -c 'nslookup %s.$RDS_SVC_NAME.$RDS_NAMESPACE.svc.cluster.local|grep Address:|tail -1 |awk "{print $2}"'`, hostname)
	} else if svcName == "" {
		// TODO: 动态获取
		cmdStr = fmt.Sprintf(`/bin/sh -c 'nslookup %s.rds-svc-0.$RDS_NAMESPACE.svc.cluster.local|grep Address:|tail -1 |awk "{print $2}"'`, hostname)
	} else {
		cmdStr = fmt.Sprintf(`/bin/sh -c 'nslookup %s.%s.$RDS_NAMESPACE.svc.cluster.local|grep Address:|tail -1 |awk "{print $2}"'`, hostname, svcName)
	}
	p := script.Exec(cmdStr)
	output, _ := p.String()
	str := strings.Trim(output, "\n")
	strList := strings.Fields(strings.TrimSpace(str))
	fmt.Println(strings.Split(strList[len(strList)-1], ":")[0])
	fmt.Println(cmdStr)
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

func pingRedis(podName, svcName string) bool {
	cmdStr := fmt.Sprintf(`/bin/sh -c '/usr/local/bin/redis-cli -h %s.%s.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH ping'`, podName, svcName)
	p := script.Exec(cmdStr)
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

