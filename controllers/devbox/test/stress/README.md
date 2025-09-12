# Devbox 压测工具

这是一个专门为 Devbox 设计的压力测试工具，用于评估集群在不同负载下的性能表现。

## 功能特性

- **规模测试**: 创建大量 devbox，观察集群资源负载
- **并发测试**: 测试集群同时创建 devbox 的能力
- **容量测试**: 寻找集群能容纳的 devbox 数量上限
- **压力测试**: 长时间持续负载测试
- **资源监控**: 实时监控 etcd、containerd、系统资源等
- **自动化报告**: 生成详细的测试报告和分析

## 工具组成

### 核心文件

- `devbox_stress_test.go`: Go 语言编写的压测核心工具
- `run_stress_tests.sh`: 自动化测试执行脚本
- `monitor.sh`: 系统资源监控脚本
- `Makefile`: 简化构建和执行的 Make 配置

### 测试场景

1. **规模测试 (Scale Test)**
   - 创建 100, 300, 500, 1000 个 devbox
   - 观察集群资源使用情况
   - 分析 etcd 存储压力和 containerd 响应

2. **并发测试 (Concurrent Test)**
   - 5, 10, 20, 50 个并发级别
   - 测试集群瞬时创建能力
   - 计算最大 QPS

3. **容量测试 (Capacity Test)**
   - 逐步增加 devbox 数量
   - 寻找集群容量上限
   - 识别系统瓶颈

4. **压力测试 (Stress Test)**
   - 1小时持续负载
   - 模拟生产环境使用模式
   - 测试系统稳定性

## 快速开始

### 前置条件

1. **环境要求**:
   - Go 1.19+ 
   - kubectl 已配置
   - 有效的 Kubernetes 集群连接
   - Devbox CRD 已安装

2. **权限要求**:
   - 集群管理员权限
   - 能够创建命名空间和资源

### 安装和使用

```bash
# 1. 进入压测工具目录
cd controllers/devbox/test/stress

# 2. 检查环境
make check

# 3. 构建工具
make build

# 4. 快速测试 (小规模)
make quick-scale

# 5. 执行完整压测
make all
```

### 常用命令

```bash
# 显示帮助
make help

# 只执行规模测试
make scale

# 只执行并发测试  
make concurrent

# 自定义资源配置
make scale CPU=200m MEMORY=256Mi NAMESPACE=my-test

# 查看测试状态
make status

# 清理测试环境
make cleanup

# 生成测试报告
make report
```

## 详细配置

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| NAMESPACE | devbox-stress-test | 测试命名空间 |
| IMAGE | ubuntu:22.04 | Devbox 使用的镜像 |
| CPU | 100m | CPU 资源限制 |
| MEMORY | 128Mi | 内存资源限制 |
| STORAGE | 1Gi | 存储资源限制 |
| KUBECONFIG | ~/.kube/config | Kubernetes 配置文件 |

### 测试参数

```bash
# 直接使用压测工具
./devbox-stress-test \
  -kubeconfig=/path/to/kubeconfig \
  -namespace=test-namespace \
  -test-type=scale \
  -devbox-count=100 \
  -concurrent=10 \
  -create-interval=2s \
  -timeout=30m \
  -image=ubuntu:22.04 \
  -cpu=100m \
  -memory=128Mi \
  -storage=1Gi \
  -cleanup=true
```

## 监控指标

### 系统资源

- **节点资源使用率**: CPU、内存、存储
- **Pod 状态分布**: Running、Pending、Failed
- **网络负载**: 网络 I/O 和连接数

### Kubernetes 组件

- **etcd 性能**: 存储大小、请求延迟、磁盘性能
- **API Server**: 请求延迟、QPS、错误率
- **Controller Manager**: 控制器延迟、工作队列深度

### Devbox 特定

- **创建成功率**: 成功/失败比例
- **创建时间**: 平均、最大、最小创建时间
- **状态转换**: Pending → Running 的时间
- **资源分配**: 实际 vs 请求的资源

### 存储系统

- **LVM 状态**: Thin Pool 使用率、逻辑卷数量
- **磁盘 I/O**: 读写 IOPS、延迟
- **容器存储**: Overlay 文件系统性能

## 测试结果分析

### 输出文件

测试完成后，会生成以下文件：

```
stress_test_results/
├── etcd.log              # etcd 性能指标
├── containerd.log        # containerd 状态
├── system.log           # 系统资源使用
├── devbox.log           # devbox 状态统计
├── storage.log          # 存储状态
├── events.log           # Kubernetes 事件
└── summary.md           # 汇总报告
```

### 关键指标解读

1. **成功率指标**
   ```
   总 Devbox 数量: 1000
   成功创建: 950
   创建失败: 50
   成功率: 95.00%
   ```

2. **性能指标**
   ```
   平均创建时间: 2.5s
   最大 QPS: 45.2
   总测试时间: 25m30s
   ```

3. **资源使用**
   ```
   CPU 使用率: 65%
   内存使用率: 78%
   存储使用率: 45%
   ```

### 瓶颈识别

1. **etcd 瓶颈**: 存储大小快速增长、写入延迟高
2. **API Server 瓶颈**: 请求延迟高、5xx 错误增多
3. **节点资源瓶颈**: CPU/内存使用率接近 100%
4. **存储瓶颈**: 磁盘 I/O 饱和、Thin Pool 空间不足
5. **网络瓶颈**: 网络延迟高、丢包率增加

## 性能调优建议

### 基于测试结果的优化

1. **控制器优化**
   ```yaml
   # 调整控制器并发数
   MaxConcurrentReconciles: 5  # 降低并发避免冲突
   ```

2. **资源配额优化**
   ```yaml
   # 设置合理的资源请求
   resources:
     requests:
       cpu: 50m      # 降低 CPU 请求
       memory: 64Mi  # 降低内存请求
   ```

3. **存储优化**
   ```bash
   # 扩展 LVM Thin Pool
   lvextend -L +10G /dev/vg/thin-pool
   
   # 配置自动扩展
   echo "activation/thin_pool_autoextend_threshold = 80" >> /etc/lvm/lvm.conf
   ```

4. **etcd 优化**
   ```yaml
   # etcd 性能调优
   --quota-backend-bytes=8589934592  # 8GB
   --max-request-bytes=10485760      # 10MB
   ```

### 集群扩容建议

基于测试结果，提供扩容建议：

- **水平扩容**: 增加工作节点数量
- **垂直扩容**: 增加单节点资源
- **存储扩容**: 增加存储容量和 IOPS
- **网络优化**: 升级网络带宽

## 故障排查

### 常见问题

1. **创建失败率高**
   ```bash
   # 检查资源配额
   kubectl describe quota -n devbox-stress-test
   
   # 检查节点资源
   kubectl top nodes
   
   # 查看事件
   kubectl get events --sort-by='.lastTimestamp'
   ```

2. **创建时间过长**
   ```bash
   # 检查镜像拉取时间
   kubectl describe pod <pod-name>
   
   # 检查存储分配
   kubectl get pv,pvc
   ```

3. **系统负载过高**
   ```bash
   # 检查系统负载
   kubectl top nodes
   
   # 检查容器运行时
   crictl stats
   ```

### 调试命令

```bash
# 查看压测工具日志
./devbox-stress-test -help

# 手动监控资源
./monitor.sh 300 10

# 检查特定 devbox
kubectl describe devbox <devbox-name>

# 查看控制器日志
kubectl logs -n devbox-system deployment/devbox-controller-manager
```

## 最佳实践

### 测试前准备

1. **集群状态检查**: 确保集群处于健康状态
2. **资源预留**: 为测试预留足够的资源
3. **监控准备**: 启用必要的监控系统
4. **备份配置**: 备份重要配置和数据

### 测试执行

1. **逐步测试**: 从小规模开始，逐步增加负载
2. **监控观察**: 实时监控各项指标
3. **及时停止**: 发现异常及时停止测试
4. **数据记录**: 详细记录测试过程和结果

### 测试后处理

1. **清理资源**: 及时清理测试创建的资源
2. **数据分析**: 仔细分析测试结果
3. **报告生成**: 生成详细的测试报告
4. **优化建议**: 基于结果提出优化建议

## 扩展功能

### 自定义测试场景

可以通过修改 `devbox_stress_test.go` 来添加自定义测试场景：

```go
// 添加新的测试方法
func (t *DevboxStressTester) RunCustomTest(ctx context.Context) (*StressTestResult, error) {
    // 自定义测试逻辑
}
```

### 集成 CI/CD

可以将压测工具集成到 CI/CD 流水线中：

```yaml
# GitHub Actions 示例
- name: Run Devbox Stress Test
  run: |
    cd controllers/devbox/test/stress
    make quick-scale
```

### 监控告警

配置 Prometheus 告警规则：

```yaml
groups:
- name: devbox-stress-test
  rules:
  - alert: DevboxCreateFailureRate
    expr: devbox_create_failure_rate > 0.1
    labels:
      severity: warning
    annotations:
      summary: "Devbox 创建失败率过高"
```

## 贡献指南

欢迎贡献代码和建议：

1. Fork 项目
2. 创建特性分支
3. 提交更改
4. 发起 Pull Request

## 许可证

本项目采用 Apache 2.0 许可证。

---

如有问题或建议，请创建 Issue 或联系维护者。
