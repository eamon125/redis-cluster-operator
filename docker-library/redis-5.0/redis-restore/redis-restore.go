package main

import (
	"fmt"
	"github.com/bitfield/script"
	"github.com/minio/minio-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	Logger *zap.SugaredLogger
)

func main() {
	Logger = NewLogger()
	fmt.Println("---------Redis cluster集群恢复任务-------")

	envInstanceName := GetStringByEnvVars("RDS_INSTANCE_NAME")
	envRestoreStorageType := GetStringByEnvVars("RDS_RESTORE_STORAGE_TYPE")
	envRestoreStorageEndpoint := GetStringByEnvVars("RDS_RESTORE_STORAGE_ENDPOINT")
	envRestoreStorageBucket := GetStringByEnvVars("RDS_RESTORE_STORAGE_BUCKET")
	envRestoreStorageAccessKey := GetStringByEnvVars("RDS_RESTORE_STORAGE_ACCESSKEY")
	envRestoreStorageSecretKey := GetStringByEnvVars("RDS_RESTORE_STORAGE_SECRETKEY")
	envRestoreStorageSSL := GetStringByEnvVars("RDS_RESTORE_STORAGE_SSL")

	restoreJob(envInstanceName, envRestoreStorageType, envRestoreStorageEndpoint, envRestoreStorageBucket, envRestoreStorageAccessKey, envRestoreStorageSecretKey, envRestoreStorageSSL)

	fmt.Println("---------Redis cluster集群恢复任务操作完毕，等待集群重启-------")
}

func GetStringByEnvVars(envVar string) string {
	cmdStr := fmt.Sprintf(`/bin/sh -c "/bin/echo $%s"`, envVar)
	p := script.Exec(cmdStr)
	output, _ := p.String()
	return strings.Trim(output, "\n")
}

func restoreJob(instance, storageType, endpoint, bucket, accessKey, secretKey, ssl string) {
	switch storageType {
	case "minio":
		restoreFromMinio(instance, endpoint, bucket, accessKey, secretKey, ssl)
	default:
		restoreFromMinio(instance, endpoint, bucket, accessKey, secretKey, ssl)
	}
}

func restoreFromMinio(instance, endpoint, bucket, accessKey, secretKey, ssl string) {
	Logger.Infof("redis cluster恢复任务：从MinIO [%s] 的bucket [%s]恢复数据", endpoint, bucket)
	useSSL, err := strconv.ParseBool(ssl)
	if err != nil {
		Logger.Error(err)
	}
	minioClient, err := minio.New(endpoint, accessKey, secretKey, useSSL)
	if err != nil {
		Logger.Error(err)
	}

	// 获取备份数据列表
	doneCh := make(chan struct{})
	defer close(doneCh)
	isRecursive := false

	objectCh := minioClient.ListObjects(bucket, "", isRecursive, doneCh)
	// TODO: -> 操作异步
	for object := range objectCh {
		if object.Err != nil {
			Logger.Error(object.Err)
		} else {
			// 根据文件名称判断应该恢复的目录名称
			match, _ := regexp.Match(".*(rds-ss).*(rdb)", []byte(object.Key))
			if match {
				// 恢复到文件
				fileNameList := strings.Split(object.Key, "-")
				if fileNameList[3] == "0" {
					restoreDirPath := "/data/" + instance + "-" + fileNameList[0] + "-" + fileNameList[1] + "-" + fileNameList[2] + "-" + fileNameList[3]
					// 目录不存在则创建
					isDirExists, _ := PathExists(restoreDirPath)
					if !isDirExists {
						// 不存在创建目录
						Logger.Infof("创建目录[%s]", restoreDirPath)
						err := os.Mkdir(restoreDirPath, os.ModePerm)
						if err != nil {
							Logger.Errorf("目录[%s]创建失败", restoreDirPath)
							return
						} else {
							Logger.Infof("创建目录[%s]成功.", restoreDirPath)
						}
					}

					restoreFilePath := restoreDirPath + "/redis_dump_6379.rdb"
					isExists, _ := PathExists(restoreFilePath)
					if isExists {
						// 已经存在则删除
						Logger.Warnf("文件[%s]已存在，正在移除", restoreFilePath)
						err := os.Remove(restoreFilePath)
						if err != nil {
							Logger.Errorf("文件[%s]移除出错.", restoreFilePath)
							return
						} else {
							Logger.Warnf("文件[%s]已移除.", restoreFilePath)
						}
					}
					err := minioClient.FGetObject(bucket, object.Key, restoreFilePath, minio.GetObjectOptions{})
					if err != nil {
						Logger.Error(err)
					} else {
						Logger.Infof("拉取备份文件[%s]到地址[%s]", object.Key, restoreFilePath)
					}
				} else {
					Logger.Warnf("[%s]从节点无需恢复", object.Key)
				}

			} else {
				Logger.Warnf("object [%s] 不符合备份规则！", object.Key)
			}
		}
	}

}

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
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

