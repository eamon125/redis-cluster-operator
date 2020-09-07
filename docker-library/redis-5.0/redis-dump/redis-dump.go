package main

import (
	"fmt"
	"github.com/bitfield/script"
	"github.com/minio/minio-go"
	"github.com/robfig/cron"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io/ioutil"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	Logger *zap.SugaredLogger
	cstSh, _ = time.LoadLocation("Asia/Shanghai")
)

func main() {
	Logger = NewLogger()
	fmt.Println("---------Redis cluster集群备份任务-------")

	envCronJobString := GetStringByEnvVars("RDS_CRON_STRING")
	envBackupStorageType := GetStringByEnvVars("RDS_BACKUP_STORAGE_TYPE")
	envBackupStorageEndpoint := GetStringByEnvVars("RDS_BACKUP_STORAGE_ENDPOINT")
	envBackupStorageBucket := GetStringByEnvVars("RDS_BACKUP_STORAGE_BUCKET")
	envBackupStorageAccessKey := GetStringByEnvVars("RDS_BACKUP_STORAGE_ACCESSKEY")
	envBackupStorageSecretKey := GetStringByEnvVars("RDS_BACKUP_STORAGE_SECRETKEY")
	envBackupStorageSSL := GetStringByEnvVars("RDS_BACKUP_STORAGE_SSL")
	envBackupJobType := GetStringByEnvVars("RDS_BACKUP_JOB_TYPE")

	switch envBackupStorageType {
	case "minio":
		// 配置Minio备份任务
		backupJob(envBackupJobType, envCronJobString, envBackupStorageEndpoint, envBackupStorageBucket, envBackupStorageAccessKey, envBackupStorageSecretKey, envBackupStorageSSL)
	default:
		// 配置Minio备份任务
		backupJob(envBackupJobType, envCronJobString, envBackupStorageEndpoint, envBackupStorageBucket, envBackupStorageAccessKey, envBackupStorageSecretKey, envBackupStorageSSL)
	}
}

func GetStringByEnvVars(envVar string) string {
	cmdStr := fmt.Sprintf(`/bin/sh -c "/bin/echo $%s"`, envVar)
	p := script.Exec(cmdStr)
	output, _ := p.String()
	return strings.Trim(output, "\n")
}

func backupJob(backupJobType, cronJobString, endpoint, bucket, accessKey, secretKey, envBackupStorageSSL string) {

	Logger.Infof("redis cluster备份任务：备份到 [%s], bucket [%s], cron [%s]", endpoint, bucket, cronJobString)
	if backupJobType == "cron" {
		c := cron.New()
		c.AddFunc(cronJobString, func() {
			backupToMinio(endpoint, bucket, accessKey, secretKey, envBackupStorageSSL)
		})
		go c.Start()
		defer c.Stop()

		select {
		}
	} else if backupJobType == "once" {
		// 执行一次任务
		backupToMinio(endpoint, bucket, accessKey, secretKey, envBackupStorageSSL)
	} else {
		Logger.Error("任务类型必须为: [once] 或者 [cron]")
	}
	fmt.Println("---------Redis cluster集群备份任务完毕-------")
}

func backupToMinio(endpoint, bucketPrename, accessKey, secretKey, backupStorageSSL string) {
	// 创建minio连接
	useSSL, err := strconv.ParseBool(backupStorageSSL)
	if err != nil {
		Logger.Error(err)
	}
	minioClient, err := minio.New(endpoint, accessKey, secretKey, useSSL)
	if err != nil {
		Logger.Error(err)
	}

	// TODO: 遍历所有备份文件后在bucket下创建'命名空间-实例名称-日期（精确到到年月日时分）'的目录，将备份文件存储进去
	// 获取所有的目录名称
	serviceDir := make([]string, 0)
	dirs, _ := ioutil.ReadDir("/data")
	for _, dir := range dirs {
		// 正则匹配目录名称带"rds-ss"的目录
		if dir.IsDir() {
			match, _ := regexp.Match(".*(rds-ss).*", []byte(dir.Name()))
			if match {
				serviceDir = append(serviceDir, dir.Name())
			}
		}
	}
	// 获取当前时间字符串
	currentTimeStr := time.Now().In(cstSh).Format("2006-01-02-15-04-05")
	// 创建备份bucket
	bucketName := bucketPrename + "-" + currentTimeStr
	err = minioClient.MakeBucket(bucketName, "rds-backup-job")
	if err != nil {
		// Check to see if we already own this bucket (which happens if you run this twice)
		exists, errBucketExists := minioClient.BucketExists(bucketName)
		if errBucketExists == nil && exists {
			Logger.Warnf("We already own %s\n", bucketName)
		} else {
			Logger.Error(err)
		}
	} else {
		Logger.Infof("Successfully created %s\n", bucketName)
	}

	backupFiles := make(map[string]string)
	// 遍历目录中的rdb文件
	for _, dir := range serviceDir {
		files, _ := ioutil.ReadDir("/data/"+dir)
		for _, f := range files {
			if !f.IsDir() {
				m, _ := regexp.Match(".*(rdb)", []byte(f.Name()))
				if m {
					hostnameSplit := strings.Split(dir, "-")
					hostnameLen := len(hostnameSplit)
					hostNameLast := hostnameSplit[hostnameLen-4] + "-" + hostnameSplit[hostnameLen-3] + "-" + hostnameSplit[hostnameLen-2] + "-" + hostnameSplit[hostnameLen-1]
					backupFiles[hostNameLast+"-"+f.Name()] =  "/data/"+ dir + "/" + f.Name()
				}
			}
		}
	}

	for k, v := range backupFiles {
		objectName := k
		filePath := v
		contentType := "application/octet-stream"

		// Upload the zip file with FPutObject
		n, err := minioClient.FPutObject(bucketName, objectName, filePath, minio.PutObjectOptions{ContentType:contentType})
		if err != nil {
			Logger.Error(err)
		} else {
			Logger.Infof("Successfully uploaded file %s to object %s of size %d\n", filePath, objectName, n)
		}
	}
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
