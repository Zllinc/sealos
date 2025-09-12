package tester

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StressTestConfig struct {
	// 测试参数
	DevboxCount     int           // devbox 数量
	ConcurrentCount int           // 并发数量
	CreateInterval  time.Duration // 创建间隔
	TestTimeout     time.Duration // 测试超时
	CleanupAfter    bool          // 测试后清理

	// Devbox 配置
	Image        string // 镜像
	CPU          string // CPU 资源
	Memory       string // 内存资源
	StorageLimit string // 存储限制
	Namespace    string // 命名空间
}

type StressTestResult struct {
	TotalDevboxes     int           // 总创建数量
	SuccessfulCreates int           // 成功创建数量
	FailedCreates     int           // 失败创建数量
	AverageCreateTime time.Duration // 平均创建时间
	TotalTestTime     time.Duration // 总测试时间
	MaxQPS            float64       // 最大 QPS
	ErrorMessages     []string      // 错误信息
}

type DevboxStressTester struct {
	config     *StressTestConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
}

type DevboxResource struct {
	Name      string
	Namespace string
}
