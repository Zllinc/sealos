package edge

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
)

// ==================== 状态转换边界测试相关结构 ====================

// StateToggleTestConfig 状态切换测试配置
type StateToggleTestConfig struct {
	// 基础配置
	Namespace string // 命名空间
	Image     string // 镜像
	CPU       string // CPU 资源
	Memory    string // 内存资源
	Storage   string // 存储限制

	// 测试规模
	DevboxCount     int // Devbox 数量
	ConcurrentCount int // 并发数量

	// 切换配置
	ToggleCycles   int             // 开关机循环次数
	WaitAfterState time.Duration   // 每次状态切换后等待时间
	StateMode      StateToggleMode // 切换模式（Stopped/Shutdown）

	// 数据配置
	DataSize  string // 数据大小（如 "100M"）
	FileCount int    // 文件数量

	// 超时配置
	StateTimeout time.Duration // 状态切换超时
	TestTimeout  time.Duration // 总测试超时
}

// StateToggleMode 状态切换模式
type StateToggleMode string

const (
	StateToggleStopped  StateToggleMode = "stopped"  // Running ↔ Stopped
	StateToggleShutdown StateToggleMode = "shutdown" // Running ↔ Shutdown
)

// StateToggleTestResult 状态切换测试结果
type StateToggleTestResult struct {
	TotalDevboxes   int                     // 总测试数量
	SuccessfulTests int                     // 成功测试数量
	FailedTests     int                     // 失败测试数量
	TotalTestTime   time.Duration           // 总测试时间
	AverageTestTime time.Duration           // 平均测试时间
	ToggleDetails   []StateToggleTestDetail // 详细信息
	ErrorMessages   []string                // 错误信息
}

// StateToggleTestDetail 单个状态切换测试详情
type StateToggleTestDetail struct {
	DevboxName        string          // Devbox 名称
	ToggleCycles      int             // 实际执行的循环次数
	DataWriteSuccess  bool            // 数据写入是否成功
	DataVerifySuccess bool            // 数据验证是否成功
	ResourceCheckOK   bool            // 资源检查是否通过
	TotalDuration     time.Duration   // 总耗时
	ToggleDurations   []time.Duration // 每次切换耗时
	Error             string          // 错误信息
	MissingResources  string          // 缺失的资源
}

// StateEdgeTester tester for state edge cases
type StateEdgeTester struct {
	config     *StateToggleTestConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
	helper     *tester.DevboxCommonHelper // Common helper utility
}

// ==================== Unexpected Delete Test Structures ====================

// UnexpectedDeleteTestConfig configuration for unexpected delete test
type UnexpectedDeleteTestConfig struct {
	// Basic configuration
	Namespace string // Namespace
	Image     string // Image
	CPU       string // CPU resource
	Memory    string // Memory resource
	Storage   string // Storage limit

	// Test scale
	DevboxCount     int    // Number of Devboxes
	ConcurrentCount int    // Concurrent count
	DevboxState     string // Target Devbox state (Running/Stopped/Shutdown)

	// Timeout configuration
	RecoveryTimeout time.Duration // Resource recovery timeout
	TestTimeout     time.Duration // Total test timeout
}

// UnexpectedDeleteTestResult result of unexpected delete test
type UnexpectedDeleteTestResult struct {
	TotalTests      int                          // Total test count
	SuccessfulTests int                          // Successful test count
	FailedTests     int                          // Failed test count
	TotalTestTime   time.Duration                // Total test time
	AverageTestTime time.Duration                // Average test time
	Details         []UnexpectedDeleteTestDetail // Test details
	ErrorMessages   []string                     // Error messages
}

// UnexpectedDeleteTestDetail details of a single unexpected delete test
type UnexpectedDeleteTestDetail struct {
	DevboxName       string        // Devbox name
	State            string        // Devbox state
	PodDeleteOK      bool          // Whether Pod delete test passed
	SecretDeleteOK   bool          // Whether Secret delete test passed
	ServiceDeleteOK  bool          // Whether Service delete test passed
	PodRecovered     bool          // Whether Pod recovered (or not exists as expected)
	SecretRecovered  bool          // Whether Secret recovered
	ServiceRecovered bool          // Whether Service recovered (or not exists as expected)
	TotalDuration    time.Duration // Total duration
	Error            string        // Error message
}

// UnexpectedDeleteTester tester for unexpected delete scenarios
type UnexpectedDeleteTester struct {
	config     *UnexpectedDeleteTestConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
	helper     *tester.DevboxCommonHelper // Common helper utility
}

// ==================== Crash Recovery Test Structures ====================

// CrashRecoveryTestConfig configuration for crash recovery test
type CrashRecoveryTestConfig struct {
	// Basic configuration
	Namespace string // Namespace
	Image     string // Image
	CPU       string // CPU resource
	Memory    string // Memory resource
	Storage   string // Storage limit

	// Test scale
	DevboxCount     int // Number of Devboxes
	ConcurrentCount int // Concurrent count

	// Crash configuration
	CrashCycles    int           // Number of crash cycles
	WaitAfterCrash time.Duration // Wait time after crash

	// Data configuration
	DataSize  string // Data size
	FileCount int    // Number of files

	// Timeout configuration
	RecoveryTimeout time.Duration // Single recovery timeout
	TestTimeout     time.Duration // Total test timeout
}

// CrashRecoveryTestResult result of crash recovery test
type CrashRecoveryTestResult struct {
	TotalTests      int                       // Total test count
	SuccessfulTests int                       // Successful test count
	FailedTests     int                       // Failed test count
	TotalTestTime   time.Duration             // Total test time
	AverageTestTime time.Duration             // Average test time
	Details         []CrashRecoveryTestDetail // Test details
	ErrorMessages   []string                  // Error messages
}

// CrashRecoveryTestDetail details of a single crash recovery test
type CrashRecoveryTestDetail struct {
	DevboxName        string              // Devbox name
	CrashCycles       int                 // Actual crash cycles
	DataWriteSuccess  bool                // Whether data write succeeded
	DataVerifySuccess bool                // Whether data verification succeeded
	CrashRecoveries   []CrashRecoveryInfo // Each crash recovery info
	TotalDuration     time.Duration       // Total duration
	Error             string              // Error message
}

// CrashRecoveryInfo information of a single crash recovery cycle
type CrashRecoveryInfo struct {
	CycleNumber      int           // Cycle number
	CrashTime        time.Time     // Crash time
	RecoveryTime     time.Time     // Recovery time
	RecoveryDuration time.Duration // Recovery duration
	PodRecreated     bool          // Whether Pod was recreated by controller
	Recovered        bool          // Whether recovered successfully
	Error            string        // Error message
}

// CrashRecoveryTester tester for crash recovery scenarios
type CrashRecoveryTester struct {
	config     *CrashRecoveryTestConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
	helper     *tester.DevboxCommonHelper // Common helper utility
}
