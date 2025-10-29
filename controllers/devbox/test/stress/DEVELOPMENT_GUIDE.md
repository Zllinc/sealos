# Devbox Stress Test Development Guide

## 📖 Overview

This document provides a comprehensive guide for developing and extending the Devbox stress test framework. It covers architecture, coding standards, development workflow, and best practices.

## 🏗️ Architecture

### Directory Structure

```
controllers/devbox/test/stress/
├── cmd/                           # CLI commands (Cobra)
│   ├── root.go                   # Root command and global flags
│   ├── concurrent.go             # Concurrent creation test
│   ├── commit.go                 # Commit functionality test
│   ├── release.go                # Release test
│   ├── lifecycle.go              # Full lifecycle test
│   ├── delete.go                 # Delete test
│   ├── cleanup.go                # Resource cleanup
│   ├── edge.go                   # Edge case tests
│   ├── scale.go                  # Scale test (legacy)
│   ├── monitor.go                # Resource monitoring (legacy)
│   ├── smallfile.go              # Small file test (legacy)
│   ├── lvm.go                    # LVM operations
│   └── release_cleanup.go        # Release cleanup
│
├── pkg/tester/                    # Test implementations
│   ├── common.go                 # ⭐ Common helper utilities (CRITICAL)
│   ├── types.go                  # ⭐ Type definitions (CRITICAL)
│   ├── concurrent_tester.go      # Concurrent test implementation
│   ├── commit_tester.go          # Commit test implementation
│   ├── cleanup_tester.go         # Cleanup implementation
│   ├── delete_tester.go          # Delete test implementation
│   ├── lifecycle_tester.go       # Lifecycle test implementation
│   ├── release_tester.go         # Release test implementation
│   ├── tester.go                 # Legacy stress tester (being phased out)
│   │
│   └── edge/                     # Edge case tests
│       ├── types.go              # Edge test type definitions
│       ├── state_edge_tester.go  # State toggle test
│       ├── unexpected_delete_tester.go # Unexpected delete test
│       └── crash_recovery_tester.go    # Crash recovery test
│
├── main.go                        # Entry point
├── Makefile                       # Build and test targets
├── README.md                      # User documentation
└── DEVELOPMENT_GUIDE.md           # This file
```

### Module Responsibilities

| Module | Responsibility | File Count |
|--------|---------------|-----------|
| **cmd/** | CLI interface, user input parsing, result formatting | 13 files |
| **pkg/tester/** | Test logic implementation, Kubernetes interactions | 9 files |
| **pkg/tester/edge/** | Edge case and boundary condition tests | 4 files |

---

## 🎯 Core Design Patterns

### 1. Tester Pattern

All testers follow a consistent pattern:

```go
// Step 1: Define configuration and result types in types.go
type XxxTestConfig struct {
    Namespace       string
    DevboxCount     int
    ConcurrentCount int
    // ... other configs
}

type XxxTestResult struct {
    TotalTests      int
    SuccessfulTests int
    FailedTests     int
    // ... statistics
}

// Step 2: Define tester struct
type DevboxXxxTester struct {
    config     *XxxTestConfig
    k8sClient  kubernetes.Interface
    ctrlClient client.Client
    scheme     *runtime.Scheme
    restConfig *rest.Config
    helper     *DevboxCommonHelper  // ⭐ MUST use common helper
}

// Step 3: Implement constructor
func NewDevboxXxxTester(config *XxxTestConfig) (*DevboxXxxTester, error) {
    // Standard client creation (copy from existing tester)
    // ...
    helper := NewDevboxCommonHelper(k8sClient, ctrlClient, restConfig)
    return &DevboxXxxTester{...}
}

// Step 4: Implement main test method
func (t *DevboxXxxTester) RunXxxTest(ctx context.Context) (*XxxTestResult, error) {
    // Test logic
}

// Step 5: Implement cleanup
func (t *DevboxXxxTester) Cleanup(ctx context.Context) error {
    // Cleanup logic
}
```

### 2. Common Helper Pattern ⭐ CRITICAL

**DO NOT** duplicate code! **ALWAYS** use `DevboxCommonHelper` for common operations.

Available methods in `common.go`:

```go
// ==================== Devbox Creation ====================
helper.GenerateDevbox(spec DevboxCreateSpec) *Devbox
helper.CreateDevbox(ctx, spec DevboxCreateSpec) error

// ==================== Resource Checks ====================
helper.IsPodRunning(ctx, devbox) bool
helper.IsServiceCreated(ctx, devbox) bool
helper.IsSecretCreated(ctx, devbox) bool
helper.IsLVMCreated(ctx, devbox) bool

// ==================== Resource Retrieval ====================
helper.GetDevboxPods(ctx, devbox) ([]Pod, error)
helper.GetDevboxServices(ctx, devbox) ([]Service, error)
helper.GetDevboxSecrets(ctx, devbox) ([]Secret, error)

// ==================== Wait Methods ====================
helper.WaitForDevboxState(ctx, namespace, name, targetState, timeout) error
helper.WaitForDevboxRunningWithResources(ctx, namespace, name, timeout) (*Devbox, error)
helper.WaitForCommitComplete(ctx, namespace, name, targetState, timeout) error
helper.WaitForReleaseComplete(ctx, namespace, name, timeout) (Phase, error)

// ==================== Data Operations ====================
helper.WriteTestDataToDevbox(ctx, devbox, directory, dataSize, fileCount) error
helper.VerifyTestDataInDevbox(ctx, devbox, directory) error

// ==================== Command Execution ====================
helper.ExecCommandInPod(ctx, namespace, podName, containerName, cmd) error
```

### 3. Concurrency Control Pattern

Use semaphore for concurrent operations:

```go
var wg sync.WaitGroup
var mu sync.Mutex
semaphore := make(chan struct{}, config.ConcurrentCount)

for i := 0; i < count; i++ {
    wg.Add(1)
    go func(index int) {
        defer wg.Done()
        semaphore <- struct{}{}        // Acquire
        defer func() { <-semaphore }() // Release
        
        // Your test logic here
        
        mu.Lock()
        // Update shared result
        mu.Unlock()
    }(i)
}

wg.Wait()
```

---

## 📝 Development Workflow

### Step-by-Step Guide to Add a New Test

#### Example: Adding a "NetworkTest"

**Step 1: Define Types** (`pkg/tester/types.go`)

```go
// ==================== Network Test Structures ====================

// NetworkTestConfig configuration for network test
type NetworkTestConfig struct {
    Namespace       string        // Namespace
    DevboxCount     int           // Number of Devboxes
    ConcurrentCount int           // Concurrent count
    // ... your specific configs
}

// NetworkTestResult result of network test
type NetworkTestResult struct {
    TotalTests      int           // Total test count
    SuccessfulTests int           // Successful test count
    FailedTests     int           // Failed test count
    // ... statistics
}

// DevboxNetworkTester tester for network
type DevboxNetworkTester struct {
    config     *NetworkTestConfig
    k8sClient  kubernetes.Interface
    ctrlClient client.Client
    scheme     *runtime.Scheme
    restConfig *rest.Config
    helper     *DevboxCommonHelper // ⭐ MUST include
}
```

**Step 2: Create Tester File** (`pkg/tester/network_tester.go`)

```go
package tester

import (
    // Standard imports (copy from concurrent_tester.go)
)

// NewDevboxNetworkTester creates a new network tester
func NewDevboxNetworkTester(config *NetworkTestConfig) (*DevboxNetworkTester, error) {
    // ⭐ COPY this entire function from any existing tester
    // It's standardized Kubernetes client creation
    
    helper := NewDevboxCommonHelper(k8sClient, ctrlClient, restConfig)
    
    return &DevboxNetworkTester{
        config:     config,
        k8sClient:  k8sClient,
        ctrlClient: ctrlClient,
        scheme:     scheme,
        restConfig: restConfig,
        helper:     helper,
    }, nil
}

// RunNetworkTest runs the network test
func (t *DevboxNetworkTester) RunNetworkTest(ctx context.Context) (*NetworkTestResult, error) {
    log.Printf("========== start network test ==========")
    
    // Your test logic here
    // ⭐ REUSE helper methods as much as possible
    
    return result, nil
}

// Cleanup cleans up test resources
func (t *DevboxNetworkTester) Cleanup(ctx context.Context) error {
    log.Printf("cleanup network test resources...")
    
    devboxList := &devboxv1alpha2.DevboxList{}
    if err := t.ctrlClient.List(ctx, devboxList, client.InNamespace(t.config.Namespace)); err != nil {
        return fmt.Errorf("list devbox failed: %w", err)
    }
    
    deletedCount := 0
    for _, devbox := range devboxList.Items {
        if t.isNetworkTestDevbox(devbox) {
            if err := t.ctrlClient.Delete(ctx, &devbox); err != nil {
                log.Printf("delete devbox %s failed: %v", devbox.Name, err)
            } else {
                log.Printf("devbox %s deleted", devbox.Name)
                deletedCount++
            }
        }
    }
    
    log.Printf("cleanup completed, deleted %d devboxes", deletedCount)
    return nil
}

func (t *DevboxNetworkTester) isNetworkTestDevbox(devbox devboxv1alpha2.Devbox) bool {
    if devbox.Labels != nil {
        if testType, ok := devbox.Labels["test-type"]; ok && testType == "network" {
            return true
        }
    }
    return false
}
```

**Step 3: Create CLI Command** (`cmd/network.go`)

```go
package cmd

import (
    "context"
    "fmt"
    "strings"
    "time"
    
    "github.com/labring/sealos/controllers/devbox/test/stress/pkg/tester"
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
)

var networkCmd = &cobra.Command{
    Use:   "network",
    Short: "run network test",
    Long: `test description here.

examples:
  devbox-stress network --count 10
`,
    RunE: runNetworkTest,
}

var (
    networkCount      int
    networkConcurrent int
    // ... other flags
)

func init() {
    rootCmd.AddCommand(networkCmd)
    
    networkCmd.Flags().IntVarP(&networkCount, "count", "c", 10, "number of devboxes")
    networkCmd.Flags().IntVar(&networkConcurrent, "concurrent", 1, "concurrent count")
    // ... other flags
}

func runNetworkTest(cmd *cobra.Command, args []string) error {
    config := &tester.NetworkTestConfig{
        Namespace:       viper.GetString("namespace"),
        DevboxCount:     networkCount,
        ConcurrentCount: networkConcurrent,
        // ...
    }
    
    networkTester, err := tester.NewDevboxNetworkTester(config)
    if err != nil {
        return fmt.Errorf("failed to create tester: %w", err)
    }
    
    ctx := context.Background()
    result, err := networkTester.RunNetworkTest(ctx)
    if err != nil {
        return fmt.Errorf("test failed: %w", err)
    }
    
    printNetworkResult(result)
    return nil
}

func printNetworkResult(result *tester.NetworkTestResult) {
    fmt.Println("\n" + strings.Repeat("=", 60))
    fmt.Println("network test summary")
    fmt.Println(strings.Repeat("=", 60))
    // ... print results
}
```

**Step 4: Update Root Command** (`cmd/root.go`)

```go
Long: `Devbox stress test tool provides multiple test scenarios:

- network: network test  // ⭐ ADD THIS LINE

examples:
  devbox-stress network --count 10  // ⭐ ADD THIS LINE
`,
```

**Step 5: Test and Verify**

```bash
# Compile
go build -o /tmp/devbox-stress-test

# Check help
/tmp/devbox-stress-test network --help

# Run test
/tmp/devbox-stress-test network --count 5
```

---

## 📏 Coding Standards

### Naming Conventions

#### Files
- **Tester files**: `{feature}_tester.go` (e.g., `commit_tester.go`)
- **Command files**: `{command}.go` (e.g., `commit.go`)
- **Edge tests**: `pkg/tester/edge/{feature}_edge_tester.go`

#### Types
- **Config**: `{Feature}TestConfig` (e.g., `CommitTestConfig`)
- **Result**: `{Feature}TestResult` (e.g., `CommitTestResult`)
- **Detail**: `{Feature}TestDetail` (e.g., `CommitTestDetail`)
- **Tester**: `Devbox{Feature}Tester` (e.g., `DevboxCommitTester`)

#### Methods
- **Constructor**: `NewDevbox{Feature}Tester(config) (*Devbox{Feature}Tester, error)`
- **Main test**: `Run{Feature}Test(ctx) (*{Feature}TestResult, error)`
- **Cleanup**: `Cleanup(ctx) error`

#### Labels
All test Devboxes MUST have:
```go
Labels: map[string]string{
    "stress-test": "true",
    "test-type":   "{feature}",  // e.g., "concurrent", "lifecycle"
}
```

### Comment Standards

**Function Comments** (English):
```go
// NewDevboxCommitTester creates a new commit tester
func NewDevboxCommitTester(config *CommitTestConfig) (*DevboxCommitTester, error)

// RunCommitTest runs the commit test
func (t *DevboxCommitTester) RunCommitTest(ctx context.Context) (*CommitTestResult, error)
```

**Struct Comments** (English):
```go
// CommitTestConfig configuration for commit test
type CommitTestConfig struct {
    Namespace string // Namespace
    DevboxCount int  // Number of Devboxes
}
```

**Log Messages** (Chinese - user-facing):
```go
log.Printf("开始 Commit 测试...")
log.Printf("总测试数: %d", result.TotalTests)
```

**Rationale**: Code comments are for developers (international), log messages are for users (Chinese).

### Error Handling

**Always wrap errors** with context:
```go
// ✅ Good
if err := t.helper.CreateDevbox(ctx, spec); err != nil {
    return fmt.Errorf("failed to create Devbox: %w", err)
}

// ❌ Bad
if err := t.helper.CreateDevbox(ctx, spec); err != nil {
    return err
}
```

### Concurrency Safety

**Always use mutex** when updating shared data:
```go
var mu sync.Mutex
var result Result

go func() {
    // ... test logic
    
    mu.Lock()
    result.SuccessfulTests++
    result.Details = append(result.Details, detail)
    mu.Unlock()
}()
```

---

## 🔧 Common Helper Utility

### Philosophy

> "Write once, use everywhere"

The `DevboxCommonHelper` is the **cornerstone** of code reuse. Before implementing any Devbox/Pod/Service/Secret operation, **CHECK IF IT EXISTS** in `common.go`.

### When to Add to common.go

✅ **ADD if**:
- Used by 2+ testers
- Generic operation (not test-specific)
- Interacts with Kubernetes resources
- Wait/poll operations

❌ **DON'T ADD if**:
- Test-specific logic
- Used by only 1 tester
- Result formatting/printing

### How to Add a New Helper Method

**Example**: Adding a method to check if PVC exists

```go
// In common.go

// IsPVCCreated checks if the PVC is created
func (h *DevboxCommonHelper) IsPVCCreated(ctx context.Context, devbox devboxv1alpha2.Devbox) bool {
    pvcList, err := h.k8sClient.CoreV1().PersistentVolumeClaims(devbox.Namespace).List(ctx, metav1.ListOptions{
        LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", devbox.Name),
    })
    return err == nil && len(pvcList.Items) > 0
}
```

Then use it in any tester:
```go
if !t.helper.IsPVCCreated(ctx, devbox) {
    return fmt.Errorf("PVC not created")
}
```

---

## 📚 Reference Implementations

### Best Reference Files by Use Case

| Need | Reference File | Key Methods |
|------|---------------|-------------|
| **Basic test structure** | `concurrent_tester.go` | Simple, clean, good starting point |
| **Data write/verify** | `lifecycle_tester.go` | Multiple data operations |
| **Commit operations** | `commit_tester.go` | State change and commit |
| **Resource checks** | `delete_tester.go` | Comprehensive resource checking |
| **Concurrent patterns** | `release_tester.go` | Complex concurrent logic |
| **Edge cases** | `edge/state_edge_tester.go` | Edge test patterns |

### Test Flow Templates

#### Template 1: Simple Create-Verify Test

```go
func (t *DevboxXxxTester) RunXxxTest(ctx context.Context) (*XxxTestResult, error) {
    result := &XxxTestResult{}
    
    // Step 1: Create Devbox
    spec := tester.DevboxCreateSpec{
        Name:      "test-devbox",
        Namespace: t.config.Namespace,
        Labels:    map[string]string{"test-type": "xxx"},
        Image:     t.config.Image,
        // ... other fields from config
    }
    if err := t.helper.CreateDevbox(ctx, spec); err != nil {
        return nil, err
    }
    
    // Step 2: Wait for ready
    devbox, err := t.helper.WaitForDevboxRunningWithResources(ctx, namespace, name, 5*time.Minute)
    if err != nil {
        return nil, err
    }
    
    // Step 3: Perform test operations
    // ...
    
    // Step 4: Verify results
    // ...
    
    return result, nil
}
```

#### Template 2: Data Persistence Test

**Reference**: `lifecycle_tester.go`, `edge/crash_recovery_tester.go`

```go
// Step 1: Create and wait ready
devbox, err := t.helper.WaitForDevboxRunningWithResources(ctx, namespace, name, 5*time.Minute)

// Step 2: Write data
dataDir := "test_data"
if err := t.helper.WriteTestDataToDevbox(ctx, *devbox, dataDir, "100M", 5); err != nil {
    return fmt.Errorf("write data failed: %w", err)
}

// Step 3: Perform operation (commit, crash, etc.)
// ...

// Step 4: Wait for ready again
devbox, err = t.helper.WaitForDevboxRunningWithResources(ctx, namespace, name, 5*time.Minute)

// Step 5: Verify data
if err := t.helper.VerifyTestDataInDevbox(ctx, *devbox, dataDir); err != nil {
    return fmt.Errorf("data verification failed: %w", err)
}
```

#### Template 3: Commit Test

**Reference**: `commit_tester.go`, `release_tester.go`

```go
// Step 1: Wait for Running
devbox, err := t.helper.WaitForDevboxRunningWithResources(ctx, namespace, name, 5*time.Minute)

// Step 2: Write data
t.helper.WriteTestDataToDevbox(ctx, *devbox, "data", "100M", 5)

// Step 3: Trigger commit by changing state
devbox.Spec.State = devboxv1alpha2.DevboxStateStopped
if err := t.ctrlClient.Update(ctx, devbox); err != nil {
    return err
}

// Step 4: Wait for commit complete
if err := t.helper.WaitForDevboxState(ctx, namespace, name, DevboxStateStopped, 10*time.Minute); err != nil {
    return err
}
```

#### Template 4: Resource Check Loop

**Reference**: `delete_tester.go`

```go
deadline := time.Now().Add(timeout)
ticker := time.NewTicker(2 * time.Second)
defer ticker.Stop()

for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-ticker.C:
        if time.Now().After(deadline) {
            return fmt.Errorf("timeout")
        }
        
        // Check condition
        if t.helper.IsPodRunning(ctx, devbox) &&
           t.helper.IsServiceCreated(ctx, devbox) &&
           t.helper.IsSecretCreated(ctx, devbox) {
            return nil // All ready
        }
    }
}
```

---

## 🎨 Code Style Guidelines

### Logging Best Practices

```go
// Phase logging
log.Printf("========== start xxx test ==========")
log.Printf("configuration:")
log.Printf("  namespace: %s", config.Namespace)

// Progress logging
log.Printf("[%d/%d] processing devbox: %s", i+1, total, name)

// Status logging
log.Printf("✓ operation successful")
log.Printf("✗ operation failed: %v", err)

// Summary logging
log.Printf("\n========== test completed ==========")
log.Printf("total: %d, successful: %d, failed: %d", total, success, failed)
```

### Result Printing Format

```go
func printXxxResult(result *tester.XxxTestResult, verbose bool) {
    fmt.Println("\n" + strings.Repeat("=", 60))
    fmt.Println("test name summary")
    fmt.Println(strings.Repeat("=", 60))
    
    // Statistics
    fmt.Printf("total: %d\n", result.TotalTests)
    fmt.Printf("successful: %d (%.1f%%)\n", result.SuccessfulTests,
        float64(result.SuccessfulTests)/float64(result.TotalTests)*100)
    
    // Time stats
    fmt.Printf("\ntime statistics:\n")
    fmt.Printf("  total time: %v\n", result.TotalTestTime)
    
    // Errors
    if len(result.ErrorMessages) > 0 {
        fmt.Printf("\nerror messages:\n")
        for i, msg := range result.ErrorMessages {
            fmt.Printf("  [%d] %s\n", i+1, msg)
        }
    }
    
    // Detailed output (optional)
    if verbose {
        fmt.Printf("\ndetailed results:\n")
        // ...
    }
    
    fmt.Println("\n" + strings.Repeat("=", 60))
    
    if result.FailedTests == 0 {
        fmt.Println("✓ all tests passed!")
    } else {
        fmt.Printf("⚠ some tests failed (%d/%d)\n", result.FailedTests, result.TotalTests)
    }
}
```

---

## 🔍 Testing Best Practices

### Wait Strategies

#### Wait for State Change
```go
// Use helper.WaitForDevboxState for simple state wait
err := t.helper.WaitForDevboxState(ctx, namespace, name, targetState, timeout)
```

#### Wait for Running with Resources
```go
// Use helper.WaitForDevboxRunningWithResources when you need all resources ready
// This includes: Pod Running + Service + Secret + LVM
devbox, err := t.helper.WaitForDevboxRunningWithResources(ctx, namespace, name, timeout)
```

**IMPORTANT**: When writing data to container, **ALWAYS** use `WaitForDevboxRunningWithResources`, not just `WaitForDevboxState`, to ensure containers are ready.

### Creating Devboxes

**ALWAYS** use `DevboxCreateSpec`:
```go
spec := tester.DevboxCreateSpec{
    Name:         name,
    Namespace:    namespace,
    Labels:       map[string]string{"stress-test": "true", "test-type": "your-test"},
    Image:        config.Image,
    CPU:          config.CPU,
    Memory:       config.Memory,
    StorageLimit: config.StorageLimit,
}

err := t.helper.CreateDevbox(ctx, spec)
```

### Data Operations

**Write data**:
```go
// directory: relative to /home/devbox/project/
// dataSize: e.g., "100M"
// fileCount: number of files to create
err := t.helper.WriteTestDataToDevbox(ctx, devbox, "test_data", "100M", 5)
```

**Verify data**:
```go
err := t.helper.VerifyTestDataInDevbox(ctx, devbox, "test_data")
```

---

## 📦 Structure Definitions

### Configuration Struct Pattern

```go
type XxxTestConfig struct {
    // Basic configuration (ALWAYS include these)
    Namespace string // Namespace
    Image     string // Image
    CPU       string // CPU resource
    Memory    string // Memory resource
    Storage   string // Storage limit (use "Storage", not "StorageLimit")
    
    // Test scale
    DevboxCount     int // Number of Devboxes
    ConcurrentCount int // Concurrent count
    
    // Feature-specific configs
    // ... your specific fields
    
    // Timeout configuration (ALWAYS include these)
    TestTimeout time.Duration // Total test timeout
}
```

### Result Struct Pattern

```go
type XxxTestResult struct {
    // Basic statistics (ALWAYS include these)
    TotalTests      int           // Total test count
    SuccessfulTests int           // Successful test count
    FailedTests     int           // Failed test count
    
    // Time statistics
    TotalTestTime   time.Duration // Total test time
    AverageTestTime time.Duration // Average test time
    
    // Detailed information
    Details       []XxxTestDetail // Test details
    ErrorMessages []string        // Error messages
}
```

### Detail Struct Pattern

```go
type XxxTestDetail struct {
    DevboxName    string        // Devbox name (ALWAYS first field)
    
    // Operation success flags
    OperationSuccess bool          // Whether operation succeeded
    
    // Time information
    TotalDuration time.Duration // Total duration
    
    // Error information
    Error         string        // Error message (ALWAYS last field)
}
```

---

## 🧪 Test Implementation Checklist

When implementing a new test, verify:

- [ ] Types defined in `types.go` (or `edge/types.go` for edge tests)
- [ ] Config struct follows naming convention
- [ ] Result struct includes basic statistics
- [ ] Tester struct includes `helper *DevboxCommonHelper`
- [ ] Constructor creates helper: `NewDevboxCommonHelper(...)`
- [ ] Main test method returns `(*XxxTestResult, error)`
- [ ] Cleanup method implemented
- [ ] CLI command created in `cmd/`
- [ ] Command added to root command help text
- [ ] All exported functions have English comments
- [ ] Mutex used for concurrent shared data access
- [ ] Devbox labels include `"stress-test": "true"` and `"test-type": "xxx"`
- [ ] Compile successful: `go build -o /tmp/devbox-stress-test`
- [ ] Help text verified: `/tmp/devbox-stress-test xxx --help`

---

## 🔄 Migration from Legacy tester.go

### Current Status

**Migrated** (independent testers):
- ✅ `concurrent_tester.go` - Concurrent creation test
- ✅ `commit_tester.go` - Commit functionality test
- ✅ `cleanup_tester.go` - Resource cleanup
- ✅ `delete_tester.go` - Delete test
- ✅ `lifecycle_tester.go` - Full lifecycle test
- ✅ `release_tester.go` - Release test
- ✅ `edge/state_edge_tester.go` - State toggle test
- ✅ `edge/unexpected_delete_tester.go` - Unexpected delete test
- ✅ `edge/crash_recovery_tester.go` - Crash recovery test

**Still in tester.go** (to be migrated):
- 🔄 `RunScaleTest` - Scale test
- 🔄 `RunMonitor` - Resource monitoring
- 🔄 `RunSmallFileCommitTest` - Small file commit test

### Migration Steps

1. **Extract types** from method signatures to `types.go`
2. **Create new tester file** following naming convention
3. **Copy client creation code** from any existing tester (standardized)
4. **Refactor test logic** to use `helper` methods
5. **Create CLI command** in `cmd/`
6. **Update root.go** help text
7. **Test compilation** and functionality
8. **Update this guide** with new tester info

---

## 📖 Example: Full Development Process

### Scenario: Adding a "Resource Quota Test"

This test verifies Devbox behavior when namespace resource quotas are reached.

#### Step 1: Design

**Test Flow**:
1. Create namespace with resource quota
2. Create Devboxes until quota is hit
3. Verify proper error handling
4. Verify existing Devboxes still work

**Files to create**:
- `pkg/tester/quota_tester.go`
- `cmd/quota.go`

**Types to define** (in `types.go`):
- `QuotaTestConfig`
- `QuotaTestResult`
- `QuotaTestDetail`
- `DevboxQuotaTester`

#### Step 2: Implementation

**Define types** (`pkg/tester/types.go`):
```go
// ==================== Quota Test Structures ====================

type QuotaTestConfig struct {
    Namespace       string
    Image           string
    CPU             string
    Memory          string
    Storage         string
    QuotaCPU        string        // Namespace CPU quota
    QuotaMemory     string        // Namespace memory quota
    MaxDevboxCount  int           // Expected max Devboxes within quota
    TestTimeout     time.Duration
}

type QuotaTestResult struct {
    TotalAttempts      int
    SuccessfulCreates  int
    QuotaReachedAt     int           // Which attempt hit quota
    ExistingDevboxOK   bool          // Existing Devboxes still functional
    TotalTestTime      time.Duration
    ErrorMessages      []string
}

type DevboxQuotaTester struct {
    config     *QuotaTestConfig
    k8sClient  kubernetes.Interface
    ctrlClient client.Client
    scheme     *runtime.Scheme
    restConfig *rest.Config
    helper     *DevboxCommonHelper
}
```

**Implement tester** (`pkg/tester/quota_tester.go`):
```go
package tester

// Copy constructor from concurrent_tester.go
func NewDevboxQuotaTester(config *QuotaTestConfig) (*DevboxQuotaTester, error) {
    // ... standard client creation
    helper := NewDevboxCommonHelper(k8sClient, ctrlClient, restConfig)
    return &DevboxQuotaTester{...}
}

func (t *DevboxQuotaTester) RunQuotaTest(ctx context.Context) (*QuotaTestResult, error) {
    // 1. Create resource quota
    // 2. Create Devboxes until quota hit
    // 3. Verify error is quota-related
    // 4. Test existing Devboxes
    // 5. Return result
}

func (t *DevboxQuotaTester) Cleanup(ctx context.Context) error {
    // Cleanup logic
}
```

**Create CLI command** (`cmd/quota.go`):
```go
// Copy structure from concurrent.go
var quotaCmd = &cobra.Command{
    Use:   "quota",
    Short: "test resource quota handling",
    RunE:  runQuotaTest,
}

func runQuotaTest(cmd *cobra.Command, args []string) error {
    config := &tester.QuotaTestConfig{...}
    quotaTester, err := tester.NewDevboxQuotaTester(config)
    // ... run test and print results
}
```

**Update root.go**:
```go
Long: `...
- quota: test resource quota handling
...

examples:
  devbox-stress quota --quota-cpu 10 --quota-memory 20Gi
`,
```

#### Step 3: Testing

```bash
# Compile
go build -o /tmp/devbox-stress-test

# Verify help
/tmp/devbox-stress-test quota --help

# Run test
/tmp/devbox-stress-test quota --quota-cpu 10 --quota-memory 20Gi

# Verify cleanup
/tmp/devbox-stress-test cleanup -n test-namespace
```

#### Step 4: Documentation

Update this guide with:
- New tester in "Reference Implementations"
- Any new helper methods added to common.go
- Update "Current Status" section

---

## 🚀 Quick Start for Contributors

### Adding a Simple Test (30 minutes)

1. **Copy an existing tester** as template:
   ```bash
   cp pkg/tester/concurrent_tester.go pkg/tester/your_tester.go
   cp cmd/concurrent.go cmd/your_command.go
   ```

2. **Search and replace** names:
   ```bash
   # In your_tester.go
   Concurrent -> YourFeature
   concurrent -> yourfeature
   
   # In your_command.go
   concurrent -> yourfeature
   ```

3. **Modify test logic** in `RunYourFeatureTest()`

4. **Update types** in `types.go`

5. **Update root.go** help text

6. **Compile and test**:
   ```bash
   go build -o /tmp/devbox-stress-test
   /tmp/devbox-stress-test yourfeature --help
   ```

### Adding an Edge Test (45 minutes)

1. **Add types** in `pkg/tester/edge/types.go`

2. **Create tester** in `pkg/tester/edge/your_edge_tester.go`
   - Copy from `state_edge_tester.go` as template

3. **Add subcommand** in `cmd/edge.go`
   - Add to `edgeCmd.Long` description
   - Add to `init()`: `edgeCmd.AddCommand(yourCmd)`
   - Define `yourCmd` with flags
   - Implement `runYourTest()` function
   - Implement `printYourResult()` function

4. **Test**

---

## 📊 File Size Guidelines

Target file sizes:

| File Type | Target Size | Max Size | Notes |
|-----------|------------|----------|-------|
| Tester implementation | 200-500 lines | 1000 lines | Split if larger |
| CLI command | 100-300 lines | 500 lines | Keep focused |
| Types definition | N/A | N/A | Can be large |
| Common helper | 400-600 lines | 800 lines | Widely used methods |

**If a tester exceeds 1000 lines**, consider:
- Splitting into multiple testers
- Moving generic logic to common.go
- Creating sub-modules

---

## 🛠️ Common Development Tasks

### Adding a New Helper Method

**File**: `pkg/tester/common.go`

```go
// 1. Add method to DevboxCommonHelper
func (h *DevboxCommonHelper) YourNewMethod(ctx context.Context, ...) error {
    // Implementation
}

// 2. Use in testers
func (t *DevboxXxxTester) someMethod(ctx context.Context) error {
    return t.helper.YourNewMethod(ctx, ...)
}
```

### Adding a New Wait Method

```go
func (h *DevboxCommonHelper) WaitForYourCondition(ctx context.Context, ...) error {
    deadline := time.Now().Add(timeout)
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            if time.Now().After(deadline) {
                return fmt.Errorf("timeout waiting for condition")
            }
            
            // Check your condition
            if conditionMet {
                return nil
            }
        }
    }
}
```

### Adding Concurrent Support to Existing Test

```go
// Pattern from concurrent_tester.go and lifecycle_tester.go

func (t *DevboxXxxTester) RunXxxTest(ctx context.Context) (*XxxTestResult, error) {
    // Decide sequential or concurrent based on config
    if t.config.ConcurrentCount > 1 {
        return t.runConcurrentTest(ctx)
    } else {
        return t.runSequentialTest(ctx)
    }
}

func (t *DevboxXxxTester) runSequentialTest(ctx context.Context) *XxxTestResult {
    result := &XxxTestResult{}
    
    for i := 0; i < t.config.DevboxCount; i++ {
        detail := t.testSingleDevbox(ctx, fmt.Sprintf("devbox-%d", i))
        result.Details = append(result.Details, detail)
        // ... update statistics
    }
    
    return result
}

func (t *DevboxXxxTester) runConcurrentTest(ctx context.Context) *XxxTestResult {
    result := &XxxTestResult{}
    var mu sync.Mutex
    var wg sync.WaitGroup
    semaphore := make(chan struct{}, t.config.ConcurrentCount)
    
    for i := 0; i < t.config.DevboxCount; i++ {
        wg.Add(1)
        go func(index int) {
            defer wg.Done()
            semaphore <- struct{}{}
            defer func() { <-semaphore }()
            
            detail := t.testSingleDevbox(ctx, fmt.Sprintf("devbox-%d", index))
            
            mu.Lock()
            result.Details = append(result.Details, detail)
            // ... update statistics
            mu.Unlock()
        }(i)
    }
    
    wg.Wait()
    return result
}
```

---

## 🐛 Common Pitfalls and Solutions

### Pitfall 1: Container Not Ready Error

❌ **Problem**:
```go
// Only wait for state
helper.WaitForDevboxState(ctx, namespace, name, Running, timeout)
// Immediately write data
helper.WriteTestDataToDevbox(ctx, devbox, ...) // ❌ FAILS: container not found
```

✅ **Solution**:
```go
// Wait for Running WITH resources (including container ready)
devbox, err := helper.WaitForDevboxRunningWithResources(ctx, namespace, name, timeout)
// Now safe to write data
helper.WriteTestDataToDevbox(ctx, *devbox, ...)
```

### Pitfall 2: Race Condition in Concurrent Tests

❌ **Problem**:
```go
var result Result

go func() {
    result.SuccessfulTests++  // ❌ Race condition!
}()
```

✅ **Solution**:
```go
var result Result
var mu sync.Mutex

go func() {
    mu.Lock()
    result.SuccessfulTests++
    mu.Unlock()
}()
```

### Pitfall 3: Code Duplication

❌ **Problem**:
```go
// In your_tester.go
func (t *YourTester) isPodRunning(ctx, devbox) bool {
    // Duplicate implementation
}
```

✅ **Solution**:
```go
// Use helper
if !t.helper.IsPodRunning(ctx, devbox) {
    return false
}
```

### Pitfall 4: Incorrect Devbox State After Commit

❌ **Problem**:
```go
// Trigger commit
devbox.Spec.State = Stopped
t.ctrlClient.Update(ctx, devbox)

// Immediately check
if devbox.Status.State == Stopped {  // ❌ Status not updated yet!
}
```

✅ **Solution**:
```go
// Trigger commit
devbox.Spec.State = Stopped
t.ctrlClient.Update(ctx, devbox)

// Wait for state change
helper.WaitForDevboxState(ctx, namespace, name, Stopped, timeout)
```

---

## 📚 Advanced Topics

### Custom Wait Conditions

If existing wait methods don't meet your needs:

```go
func (t *DevboxXxxTester) waitForCustomCondition(ctx context.Context, devbox Devbox) error {
    deadline := time.Now().Add(5 * time.Minute)
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            if time.Now().After(deadline) {
                return fmt.Errorf("timeout")
            }
            
            // Your custom check logic
            // ...
            
            if conditionMet {
                return nil
            }
        }
    }
}
```

### Executing Commands in Container

```go
// Single command
cmd := []string{"sh", "-c", "echo hello"}
err := t.helper.ExecCommandInPod(ctx, namespace, podName, containerName, cmd)

// Complex shell script
cmd := []string{
    "bash", "-c",
    fmt.Sprintf(`
        mkdir -p /path/to/dir
        cd /path/to/dir
        for i in $(seq 1 10); do
            echo "Processing $i"
            # Your commands
        done
    `),
}
err := t.helper.ExecCommandInPod(ctx, namespace, podName, containerName, cmd)
```

### Handling Multiple States

```go
// Use switch for clean state handling
targetState := t.getTargetState()

switch targetState {
case devboxv1alpha2.DevboxStateRunning:
    // Handle Running
case devboxv1alpha2.DevboxStateStopped:
    // Handle Stopped
case devboxv1alpha2.DevboxStateShutdown:
    // Handle Shutdown
default:
    return fmt.Errorf("unsupported state: %s", targetState)
}
```

---

## 🔧 Troubleshooting

### Build Errors

**Error**: `undefined: XxxTestConfig`
- **Solution**: Add type definition to `types.go`

**Error**: `cannot use xxx (type string) as type DevboxState`
- **Solution**: Cast: `devboxv1alpha2.DevboxState(xxx)`

**Error**: `method has pointer receiver but called without pointer`
- **Solution**: Use `&devbox` instead of `devbox`

### Runtime Errors

**Error**: `container not found`
- **Solution**: Use `WaitForDevboxRunningWithResources` before executing commands

**Error**: `no pod found`
- **Solution**: Check if Pod exists before operations; handle Stopped/Shutdown states

**Error**: `timeout waiting for state`
- **Solution**: Increase timeout or check if state transition is blocked (e.g., during commit)

---

## 📈 Code Metrics

### Current Statistics

| Category | Count | Total Lines |
|----------|-------|-------------|
| **Independent Testers** | 9 | ~4,600 lines |
| **Edge Testers** | 3 | ~1,600 lines |
| **Common Helper** | 1 | ~450 lines |
| **CLI Commands** | 13 | ~2,500 lines |
| **Total** | **26 files** | **~9,150 lines** |

### Code Reuse Metrics

- **Common helper methods**: 20+
- **Estimated code saved**: 2,000+ lines
- **Code reuse rate**: ~60%

---

## 🎓 Learning Path for New Contributors

### Beginner (1-2 hours)

1. Read `README.md` for user perspective
2. Run existing tests: `devbox-stress concurrent --count 5`
3. Read `concurrent_tester.go` (simplest tester)
4. Understand `common.go` helper methods

### Intermediate (3-4 hours)

1. Read `lifecycle_tester.go` (complex flow)
2. Read `commit_tester.go` (state management)
3. Understand concurrency patterns
4. Try modifying a test parameter

### Advanced (5+ hours)

1. Read `edge/` tests (boundary conditions)
2. Add a new helper method to `common.go`
3. Implement a new simple test
4. Implement a new edge test

---

## 🔗 Related Documentation

- **User Guide**: `README.md` - How to use the tests
- **Cobra Usage**: `COBRA_USAGE.md` - CLI framework guide
- **Test Scripts**: `test.sh`, `run_stress_tests.sh` - Automation scripts

---

## 💡 Best Practices Summary

### DOs ✅

- ✅ Use `DevboxCommonHelper` for all common operations
- ✅ Follow naming conventions strictly
- ✅ Add English comments for code, Chinese for logs
- ✅ Use mutex for concurrent access to shared data
- ✅ Always wait for resources ready before operations
- ✅ Wrap errors with context
- ✅ Include cleanup method in every tester
- ✅ Add labels to test Devboxes: `stress-test: true`, `test-type: xxx`

### DON'Ts ❌

- ❌ Duplicate code (use helper instead)
- ❌ Skip resource readiness checks
- ❌ Access shared data without mutex in goroutines
- ❌ Return raw errors (always wrap with context)
- ❌ Create Devboxes without proper labels
- ❌ Exceed 1000 lines per tester file
- ❌ Mix test logic with result printing

---

## 📞 Getting Help

### Code References

For specific scenarios, refer to:

| Scenario | Reference File | Line/Function |
|----------|---------------|---------------|
| Create Devbox | `common.go` | `CreateDevbox()` |
| Wait for ready | `common.go` | `WaitForDevboxRunningWithResources()` |
| Write data | `common.go` | `WriteTestDataToDevbox()` |
| Trigger commit | `commit_tester.go` | `triggerCommitConcurrently()` |
| Handle concurrency | `concurrent_tester.go` | `RunConcurrentTest()` |
| Edge case patterns | `edge/state_edge_tester.go` | Full file |

### Common Questions

**Q: Where should I add my new test?**
- Functional test → `pkg/tester/{feature}_tester.go`
- Edge/boundary test → `pkg/tester/edge/{feature}_edge_tester.go`

**Q: Do I need to create a new helper method?**
- Check if similar operation exists in `common.go`
- If used by 2+ testers → add to `common.go`
- If test-specific → keep in tester file

**Q: How to handle different Devbox states?**
- Reference `edge/unexpected_delete_tester.go` for state-aware logic
- Use `switch` statement for multiple states

**Q: My test needs a new type of resource check?**
- Add to `common.go` following pattern of `IsPodRunning()`, `IsServiceCreated()`, etc.

---

## 🔄 Version History

- **v1.0** - Initial modular architecture with common helper
- **v1.1** - Added edge case tests (toggle, unexpected-delete, crash)
- **v1.2** - Internationalized code comments (English)
- **v1.3** - Current version

---

## 🎯 Contribution Guidelines

1. **Before coding**: Check if similar test exists
2. **During coding**: Follow patterns in this guide
3. **Before PR**: 
   - Compile successfully
   - Test manually
   - Update this guide if adding new patterns
   - Add English comments to exported functions

---

**Happy Testing! 🎉**

For questions or suggestions, please open an issue or contact the maintainers.

