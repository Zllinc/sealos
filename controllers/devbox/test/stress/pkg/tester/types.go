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

// ReleaseTestConfig 发版测试配置
type ReleaseTestConfig struct {
	// 测试参数
	ReleaseCount    int           // 发版数量
	ConcurrentCount int           // 并发数量
	TestTimeout     time.Duration // 测试超时
	CleanupAfter    bool          // 测试后清理

	// DevBoxRelease 配置
	BaseDevboxName    string // 基础 devbox 名称（如果为空，会自动创建）
	VersionPattern    string // 版本号模式，例如 "v1.0.%d"
	StartAfterRelease bool   // 发版后是否启动 devbox
	Namespace         string // 命名空间

	// Devbox 配置（用于创建基础 devbox）
	Image        string // 镜像
	CPU          string // CPU 资源
	Memory       string // 内存资源
	StorageLimit string // 存储限制
}

// ReleaseTestResult 发版测试结果
type ReleaseTestResult struct {
	TotalReleases      int           // 总发版数量
	SuccessfulReleases int           // 成功发版数量
	FailedReleases     int           // 失败发版数量
	PendingReleases    int           // 待处理发版数量
	AverageReleaseTime time.Duration // 平均发版时间
	TotalTestTime      time.Duration // 总测试时间
	MaxQPS             float64       // 最大 QPS
	ErrorMessages      []string      // 错误信息
	ImageVerifications []ImageCheck  // 镜像验证结果
}

// ImageCheck 镜像一致性检查结果
type ImageCheck struct {
	ReleaseName string // 发版名称
	SourceImage string // 源镜像
	TargetImage string // 目标镜像
	DigestMatch bool   // digest 是否一致
	SourceFound bool   // 源镜像是否存在
	TargetFound bool   // 目标镜像是否存在
	Error       string // 错误信息
}

// DevboxReleaseTester 发版测试器
type DevboxReleaseTester struct {
	config     *ReleaseTestConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
}
