package tester

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StressTestConfig struct {
	// Test parameters
	DevboxCount     int           // Number of devboxes
	ConcurrentCount int           // Concurrent count
	CreateInterval  time.Duration // Create interval
	TestTimeout     time.Duration // Test timeout
	CleanupAfter    bool          // Cleanup after test

	// Devbox configuration
	Image        string // Image
	CPU          string // CPU resource
	Memory       string // Memory resource
	StorageLimit string // Storage limit
	Namespace    string // Namespace
}

type StressTestResult struct {
	TotalDevboxes     int           // Total create count
	SuccessfulCreates int           // Successful creates
	FailedCreates     int           // Failed creates
	AverageCreateTime time.Duration // Average create time
	TotalTestTime     time.Duration // Total test time
	MaxQPS            float64       // Max QPS
	ErrorMessages     []string      // Error messages
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

// CommitTriggerMode defines how commit is triggered
type CommitTriggerMode string

const (
	// CommitTriggerByStopped triggers commit by changing state to Stopped
	CommitTriggerByStopped CommitTriggerMode = "stopped"
	// CommitTriggerByShutdown triggers commit by changing state to Shutdown
	CommitTriggerByShutdown CommitTriggerMode = "shutdown"
)

// ReleaseTestConfig configuration for release test
type ReleaseTestConfig struct {
	// Test parameters
	ReleaseCount    int           // Number of releases
	ConcurrentCount int           // Concurrent count
	TestTimeout     time.Duration // Test timeout
	CleanupAfter    bool          // Cleanup after test

	// DevBoxRelease configuration
	BaseDevboxName    string            // Base devbox name (auto-created if empty)
	VersionPattern    string            // Version pattern, e.g. "v1.0.%d"
	StartAfterRelease bool              // Start devbox after release
	CommitTrigger     CommitTriggerMode // Commit trigger method (stop/shutdown)
	Namespace         string            // Namespace

	// Devbox configuration (for creating base devbox)
	Image        string // Image
	CPU          string // CPU resource
	Memory       string // Memory resource
	StorageLimit string // Storage limit
}

// ReleaseTestResult result of release test
type ReleaseTestResult struct {
	TotalReleases      int           // Total release count
	SuccessfulReleases int           // Successful releases
	FailedReleases     int           // Failed releases
	PendingReleases    int           // Pending releases
	AverageReleaseTime time.Duration // Average release time
	TotalTestTime      time.Duration // Total test time
	MaxQPS             float64       // Max QPS
	ErrorMessages      []string      // Error messages
	ImageVerifications []ImageCheck  // Image verification results
}

// ImageCheck result of image consistency check
type ImageCheck struct {
	ReleaseName string // Release name
	SourceImage string // Source image
	TargetImage string // Target image
	DigestMatch bool   // Whether digest matches
	SourceFound bool   // Whether source image exists
	TargetFound bool   // Whether target image exists
	Error       string // Error message
}

// DevboxReleaseTester tester for release
type DevboxReleaseTester struct {
	config     *ReleaseTestConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
}

// LifecycleTestConfig configuration for lifecycle test
type LifecycleTestConfig struct {
	// Test parameters
	DevboxCount     int           // Number of Devboxes to test
	ConcurrentCount int           // Concurrent count
	TestTimeout     time.Duration // Test timeout
	CleanupAfter    bool          // Cleanup after test

	// Data write configuration
	Data1Size  string // First data size
	Data2Size  string // Second data size
	FileCount  int    // Number of files
	VerifyData bool   // Whether to verify data

	// Release configuration
	ReleaseVersionPattern string // Version pattern

	// Devbox configuration
	Image        string // Image
	CPU          string // CPU resource
	Memory       string // Memory resource
	StorageLimit string // Storage limit
	Namespace    string // Namespace
}

// LifecycleTestResult result of lifecycle test
type LifecycleTestResult struct {
	TotalTests      int // Total test count
	SuccessfulTests int // Successful test count
	FailedTests     int // Failed test count

	// Phase statistics
	ResourceCheckPassed  int // Resource check passed count
	StoppedCommitPassed  int // Stopped commit passed count
	ShutdownCommitPassed int // Shutdown commit passed count
	ReleasePassed        int // Release passed count

	// Time statistics
	AverageTestTime time.Duration // Average test time
	TotalTestTime   time.Duration // Total test time

	// Detailed information
	TestDetails   []LifecycleTestDetail // Test details
	ErrorMessages []string              // Error messages
}

// LifecycleTestDetail details of a single test
type LifecycleTestDetail struct {
	DevboxName       string        // Devbox name
	ResourceCheckOK  bool          // Whether resource check passed
	StoppedCommitOK  bool          // Whether stopped commit passed
	Data1VerifyOK    bool          // Whether first data verification passed
	ShutdownCommitOK bool          // Whether shutdown commit passed
	Data2VerifyOK    bool          // Whether second data verification passed
	ReleaseOK        bool          // Whether release passed
	TotalDuration    time.Duration // Total duration
	Error            string        // Error message
}

// DevboxLifecycleTester tester for lifecycle
type DevboxLifecycleTester struct {
	config     *LifecycleTestConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
	helper     *DevboxCommonHelper // Common helper utility
}

// ==================== Delete Test Structures ====================

// DeleteTestConfig configuration for delete test
type DeleteTestConfig struct {
	Namespace       string        // Namespace
	ConcurrentCount int           // Concurrent delete count
	CheckInterval   time.Duration // Resource check interval
	DeleteTimeout   time.Duration // Single delete timeout
	TestTimeout     time.Duration // Total test timeout
}

// DeleteTestResult result of delete test
type DeleteTestResult struct {
	TotalDevboxes      int                 // Total delete count
	SuccessfulDeletes  int                 // Successful delete count
	FailedDeletes      int                 // Failed delete count
	AverageDeleteTime  time.Duration       // Average delete time
	TotalTestTime      time.Duration       // Total test time
	MaxQPS             float64             // Max QPS
	DeleteDetails      []DeleteTestDetail  // Delete details
	ResourceCheckStats ResourceCleanupStat // Resource cleanup statistics
	ErrorMessages      []string            // Error messages
}

// DeleteTestDetail details of a single delete
type DeleteTestDetail struct {
	DevboxName        string        // Devbox name
	DeleteSuccess     bool          // Whether delete succeeded
	DevboxCleaned     bool          // Whether Devbox resource cleaned
	PodCleaned        bool          // Whether Pod cleaned
	ServiceCleaned    bool          // Whether Service cleaned
	SecretCleaned     bool          // Whether Secret cleaned
	LVMCleaned        bool          // Whether LVM cleaned
	DeleteDuration    time.Duration // Delete duration
	CleanupDuration   time.Duration // Resource cleanup duration
	TotalDuration     time.Duration // Total duration
	Error             string        // Error message
	RemainingResource string        // Remaining resource information
}

// ResourceCleanupStat resource cleanup statistics
type ResourceCleanupStat struct {
	DevboxCleaned  int // Devbox cleanup count
	PodCleaned     int // Pod cleanup count
	ServiceCleaned int // Service cleanup count
	SecretCleaned  int // Secret cleanup count
	LVMCleaned     int // LVM cleanup count
}

// DevboxDeleteTester tester for delete
type DevboxDeleteTester struct {
	config     *DeleteTestConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
	helper     *DevboxCommonHelper // Common helper utility
}

// ==================== Concurrent Test Structures ====================

// ConcurrentTestConfig configuration for concurrent test
type ConcurrentTestConfig struct {
	Namespace       string        // Namespace
	Image           string        // Image
	CPU             string        // CPU resource
	Memory          string        // Memory resource
	StorageLimit    string        // Storage limit
	DevboxCount     int           // Number of Devboxes
	ConcurrentCount int           // Concurrent count
	TestTimeout     time.Duration // Test timeout
	WaitTimeout     time.Duration // Wait for Devbox Running timeout
}

// ConcurrentTestResult result of concurrent test
type ConcurrentTestResult struct {
	TotalDevboxes     int                    // Total create count
	SuccessfulCreates int                    // Successful create count
	FailedCreates     int                    // Failed create count
	AverageCreateTime time.Duration          // Average create time
	TotalTestTime     time.Duration          // Total test time
	MaxQPS            float64                // Max QPS
	ErrorMessages     []string               // Error messages
	Details           []ConcurrentTestDetail // Detailed information
}

// ConcurrentTestDetail details of a single concurrent test
type ConcurrentTestDetail struct {
	DevboxName     string        // Devbox name
	CreateSuccess  bool          // Whether create succeeded
	RunningSuccess bool          // Whether running succeeded
	CreateDuration time.Duration // Create duration
	TotalDuration  time.Duration // Total duration (including wait for Running)
	Error          string        // Error message
}

// DevboxConcurrentTester tester for concurrent
type DevboxConcurrentTester struct {
	config     *ConcurrentTestConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
	helper     *DevboxCommonHelper // Common helper utility
}

// ==================== Commit Test Structures ====================

// CommitTestConfig configuration for commit test
type CommitTestConfig struct {
	Namespace       string        // Namespace
	Image           string        // Image
	CPU             string        // CPU resource
	Memory          string        // Memory resource
	StorageLimit    string        // Storage limit
	DevboxCount     int           // Number of Devboxes
	ConcurrentCount int           // Concurrent count
	TargetState     string        // Target state (Stopped/Shutdown)
	DataSize        string        // Data size (e.g. "100M")
	FileCount       int           // Number of files
	VerifyData      bool          // Whether to verify data
	CommitTimeout   time.Duration // Commit timeout
	TestTimeout     time.Duration // Test timeout
}

// CommitTestResult result of commit test
type CommitTestResult struct {
	TotalDevboxes     int                // Total test count
	SuccessfulCommits int                // Successful commit count
	FailedCommits     int                // Failed commit count
	WriteDataTime     time.Duration      // Total write data time
	CommitTime        time.Duration      // Total commit time
	VerifyTime        time.Duration      // Total verify time
	TotalTestTime     time.Duration      // Total test time
	WriteSpeedMBps    float64            // Write speed MB/s
	CommitQPS         float64            // Commit QPS
	ErrorMessages     []string           // Error messages
	Details           []CommitTestDetail // Detailed information
}

// CommitTestDetail details of a single commit test
type CommitTestDetail struct {
	DevboxName     string        // Devbox name
	WriteSuccess   bool          // Whether write succeeded
	CommitSuccess  bool          // Whether commit succeeded
	VerifySuccess  bool          // Whether verify succeeded
	WriteDuration  time.Duration // Write duration
	CommitDuration time.Duration // Commit duration
	VerifyDuration time.Duration // Verify duration
	TotalDuration  time.Duration // Total duration
	Error          string        // Error message
}

// DevboxCommitTester tester for commit
type DevboxCommitTester struct {
	config     *CommitTestConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
	helper     *DevboxCommonHelper // Common helper utility
}

// ==================== Cleanup Structures ====================

// CleanupConfig configuration for cleanup
type CleanupConfig struct {
	Namespace     string // Namespace
	AllNamespaces bool   // Whether to cleanup all namespaces
	Force         bool   // Force delete (remove finalizers)
	DryRun        bool   // Only list resources, don't delete
}

// CleanupResult result of cleanup
type CleanupResult struct {
	TotalResources   int             // Total resource count
	DeletedDevboxes  int             // Deleted Devbox count
	DeletedReleases  int             // Deleted DevBoxRelease count
	FailedDeletes    int             // Failed delete count
	SkippedResources int             // Skipped resource count
	ErrorMessages    []string        // Error messages
	CleanupDetails   []CleanupDetail // Cleanup details
}

// CleanupDetail details of cleanup
type CleanupDetail struct {
	ResourceType string // Resource type (Devbox/DevBoxRelease)
	Namespace    string // Namespace
	Name         string // Resource name
	TestType     string // Test type label
	Deleted      bool   // Whether successfully deleted
	Error        string // Error message
}

// DevboxCleanupTester tester for cleanup
type DevboxCleanupTester struct {
	config     *CleanupConfig
	k8sClient  kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
	restConfig *rest.Config
	helper     *DevboxCommonHelper // Common helper utility
}
