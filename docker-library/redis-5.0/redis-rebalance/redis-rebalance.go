package main

import (
	"fmt"
	"github.com/bitfield/script"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os/exec"
	"strings"
	"time"
)


var Logger *zap.SugaredLogger

type Result struct {
	Done bool
	Msg string
}

func main() {
	Logger = NewLogger()
	// 分配完slot，进程结束
	// 指定rds-ss-0-0分配slot
	// 获取rds-ss-0-0的ip
	Logger.Info("开始rebalance slot, 请稍后...")
	done := make(chan Result)
	go rebalance(done)
	loop:
		for {
			select {
			case isDone := <- done:
				if !isDone.Done {
					Logger.Errorf("Redis 集群 slot rebalance 失败: %s", isDone.Msg)
				} else {
					Logger.Info("Redis 集群 slot rebalance 完毕.")
				}
				break loop
			default:
				time.Sleep(500 * time.Millisecond)
			}
		}
	for {
		time.Sleep(1 * time.Second)
		Logger.Info("操作已完毕，如需重复操作，请删除pod或重新部署rebalance的cr.")
	}
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

func GetInstanceName() string {
	p := script.Exec("/bin/sh -c '/bin/echo $RDS_INSTANCE_NAME'")
	output, _ := p.String()
	return strings.Trim(output, "\n")
}

func rebalance(done chan Result) {
	instanceName := GetInstanceName()
	r := Result{Done:true, Msg:"success"}

	IP := getIpByDns(instanceName+ "-rds-ss-0-0", false, instanceName+ "-rds-svc-0")
	cmdStr := fmt.Sprintf(`/bin/sh -c 'redis-cli -h $RDS_INSTANCE_NAME-rds-ss-0-0.%s-rds-svc-0.$RDS_NAMESPACE.svc.cluster.local -a $RDS_AUTH --cluster rebalance %s:6379 --cluster-threshold 1 --cluster-use-empty-masters'`, instanceName, IP)
	cmd := exec.Command("/bin/ash", "-c", cmdStr)
	stdout, err := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err != nil {
		r.Done = false
		r.Msg = err.Error()
	}
	if err := cmd.Start(); err != nil {
		r.Done = false
		r.Msg = err.Error()
	}
	for {
		tmp := make([]byte, 1024)
		_, err5 := stdout.Read(tmp)
		fmt.Print(string(tmp))
		if err5 != nil {
			break
		}
	}
	err = cmd.Wait()
	if err != nil {
		r.Done = false
		r.Msg = err.Error()
	}

	/*p := script.Exec(cmdStr)
	var exit int = p.ExitStatus()
	output, err := p.String()

	if exit != 0 {
		r.Done = false
		r.Msg = err.Error()

	} else {
		r.Done = true
		r.Msg = output
	}
	*/
	done <- r
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

