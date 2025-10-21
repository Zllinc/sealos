# DevBoxRelease 发版测试指南

## 功能概述

DevBoxRelease 测试模块用于验证 Devbox 发版功能的正确性和性能。测试覆盖以下方面：

### 核心功能
- **状态转换验证**：监控 DevBoxRelease 从 Pending → Success/Failed 的完整生命周期
- **镜像一致性检查**：验证源镜像和目标镜像的 digest 是否一致
- **并发发版测试**：测试系统在高并发下的发版能力
- **自动化验证**：自动验证发版后的 Devbox 状态

### 测试场景
1. **基础发版测试**：顺序创建多个发版，验证基本功能
2. **并发发版测试**：并发创建多个发版，测试系统并发处理能力

## 快速开始

### 前置条件

1. **环境要求**：
   - Kubernetes 集群已配置
   - DevBox CRD 和控制器已安装
   - kubectl 已正确配置

2. **工具要求**（用于镜像验证）：
   - `crane` (推荐) 或
   - `skopeo` 或
   - `crictl`

### 构建工具

```bash
cd controllers/devbox/test/stress
make build
```

## 使用示例

### 1. 基础发版测试（最小配置）

创建 5 个发版，顺序执行：

```bash
./dtest release --count 5
```

这将：
- 自动创建一个基础 Devbox
- 创建 5 个版本的 DevBoxRelease（v1.0.0 到 v1.0.4）
- 等待每个发版完成
- 验证镜像一致性
- 输出测试结果

### 2. 指定基础 Devbox

如果你已经有一个准备好的 Devbox：

```bash
./dtest release --count 5 --base-devbox my-existing-devbox
```

**注意**：
- 基础 Devbox 必须处于非 Running/Paused 状态
- Devbox 必须有有效的 CommitRecord

### 3. 并发发版测试

测试并发发版能力：

```bash
./dtest release --count 20 --concurrent 5
```

或者：

```bash
./dtest release --count 20 --mode concurrent --concurrent 5
```

这将：
- 使用 5 个并发 goroutine
- 同时创建 20 个发版
- 计算发版 QPS 和成功率

### 4. 自定义版本号模式

使用自定义版本号格式：

```bash
./dtest release --count 10 --version-pattern "v2.0.%d"
```

生成的版本号：v2.0.0, v2.0.1, v2.0.2, ...

### 5. 发版后自动启动 Devbox

测试 `StartDevboxAfterRelease` 功能：

```bash
./dtest release --count 3 --start-after-release
```

这将验证：
- 发版完成后，Devbox.Spec.State 是否被设置为 Running
- 控制器是否正确处理了状态更新

### 6. 自定义资源配置

指定 Devbox 的资源配置：

```bash
./dtest release \
  --count 5 \
  --cpu 1000m \
  --memory 2Gi \
  --storage 2Gi \
  --namespace my-test-ns
```

### 7. 完整测试（带清理）

执行完整测试并自动清理资源：

```bash
./dtest release \
  --count 10 \
  --concurrent 3 \
  --timeout 30m \
  --cleanup
```

### 8. 自定义镜像

使用特定镜像创建 Devbox：

```bash
./dtest release \
  --count 5 \
  --image ghcr.io/labring-actions/devbox/ubuntu:22.04
```

## 测试结果解读

### 输出示例

```
===========================================================
                    发版测试结果
===========================================================
总发版数量: 10
成功发版: 9
失败发版: 1
待处理发版: 0
成功率: 90.00%

-----------------------------------------------------------
性能指标:
  平均发版时间: 15.3s
  总测试时间: 2m35s
  最大 QPS: 0.35

-----------------------------------------------------------
镜像一致性验证:
  验证总数: 9
  验证通过: 8
  验证失败: 1
  验证通过率: 88.89%

  失败的验证详情:
    - release-test-devbox-1234567890-release-5:
      Digest 不匹配
      错误: digest 不匹配: 源=sha256:abc..., 目标=sha256:def...

-----------------------------------------------------------
错误信息:
  1. 创建 release-test-devbox-1234567890-release-3 失败: context deadline exceeded
===========================================================
```

### 关键指标说明

1. **成功率**：
   - 理想值：> 95%
   - 如果低于 90%，检查控制器日志和集群资源

2. **平均发版时间**：
   - 取决于镜像大小和网络速度
   - 通常在 10-30 秒之间

3. **镜像一致性验证**：
   - 必须 100% 通过
   - 如果失败，说明 re-tag 功能有问题

4. **最大 QPS**：
   - 顺序测试：约 0.1-0.5
   - 并发测试：取决于并发级别，通常 1-5

## 常见问题排查

### 1. 创建 Devbox 失败

**问题**：测试一开始就报错，无法创建基础 Devbox

**排查步骤**：
```bash
# 检查命名空间是否存在
kubectl get namespace devbox-test

# 检查资源配额
kubectl describe quota -n devbox-test

# 检查 StorageClass
kubectl get storageclass
```

### 2. 发版一直处于 Pending 状态

**问题**：DevBoxRelease 创建成功但一直不变为 Success

**排查步骤**：
```bash
# 查看 DevBoxRelease 状态
kubectl -n devbox-test get devboxrelease

# 查看控制器日志
kubectl -n devbox-system logs -l app=devbox-controller --tail=100

# 检查 Devbox 状态
kubectl -n devbox-test get devbox
```

**可能原因**：
- Devbox 处于 Running/Paused 状态（控制器会跳过）
- 源镜像不存在（会重试）
- 控制器未运行或有错误

### 3. 镜像一致性验证失败

**问题**：报错 "无法获取镜像 digest"

**解决方案**：

安装 crane：
```bash
# macOS
brew install crane

# Linux
curl -sL "https://github.com/google/go-containerregistry/releases/download/v0.19.0/go-containerregistry_Linux_x86_64.tar.gz" | tar -xz crane
sudo mv crane /usr/local/bin/
```

或安装 skopeo：
```bash
# Ubuntu/Debian
sudo apt-get install skopeo

# RHEL/CentOS
sudo yum install skopeo

# macOS
brew install skopeo
```

### 4. 并发测试失败率高

**问题**：并发测试时成功率明显降低

**排查步骤**：
```bash
# 检查 API Server 负载
kubectl top nodes

# 检查控制器资源使用
kubectl -n devbox-system top pods

# 查看 etcd 性能指标
kubectl -n kube-system exec -it etcd-xxx -- etcdctl endpoint status --write-out=table
```

**优化建议**：
- 降低并发级别
- 增加超时时间
- 检查集群资源是否充足

### 5. Devbox 启动验证失败

**问题**：使用 `--start-after-release` 但验证失败

**排查步骤**：
```bash
# 检查 Devbox 状态
kubectl -n devbox-test get devbox -o yaml

# 查看 Devbox 事件
kubectl -n devbox-test describe devbox xxx

# 查看 Pod 状态
kubectl -n devbox-test get pods
```

**可能原因**：
- 控制器更新 Devbox 失败
- 存在并发冲突
- 网络问题导致更新延迟

## 高级用法

### 1. 压力测试

测试系统在大量发版下的表现：

```bash
./dtest release \
  --count 100 \
  --concurrent 20 \
  --timeout 1h \
  --cleanup
```

### 2. 持续监控

配合监控脚本使用：

```bash
# 终端 1：运行测试
./dtest release --count 50 --concurrent 10

# 终端 2：监控资源
./monitor.sh 600 5
```

### 3. 多命名空间测试

在不同命名空间并行测试：

```bash
# 终端 1
./dtest release --count 10 -n test-ns-1

# 终端 2
./dtest release --count 10 -n test-ns-2

# 终端 3
./dtest release --count 10 -n test-ns-3
```

### 4. 自动化测试脚本

创建测试脚本 `run_release_tests.sh`：

```bash
#!/bin/bash

echo "开始 DevBoxRelease 自动化测试"

# 基础功能测试
echo "=== 测试 1: 基础功能 ==="
./dtest release --count 5 --cleanup

# 并发测试
echo "=== 测试 2: 并发测试 ==="
./dtest release --count 20 --concurrent 5 --cleanup

# 启动验证测试
echo "=== 测试 3: 启动验证 ==="
./dtest release --count 3 --start-after-release --cleanup

echo "所有测试完成"
```

## 最佳实践

### 测试前

1. **确认集群状态**：确保集群健康，资源充足
2. **清理旧资源**：删除之前测试留下的资源
3. **备份重要数据**：如果在生产环境测试，先备份

### 测试中

1. **逐步增加负载**：从小规模开始，逐步增加
2. **监控集群状态**：实时查看资源使用情况
3. **记录异常情况**：保存日志和错误信息

### 测试后

1. **分析测试结果**：关注成功率、性能指标
2. **验证镜像**：确保镜像一致性 100% 通过
3. **清理测试资源**：使用 `--cleanup` 或手动清理

## 集成到 CI/CD

### GitHub Actions 示例

```yaml
name: DevBoxRelease Test

on:
  pull_request:
    paths:
      - 'controllers/devbox/**'

jobs:
  release-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Setup K8s cluster
        uses: helm/kind-action@v1
        
      - name: Build test tool
        run: |
          cd controllers/devbox/test/stress
          make build
          
      - name: Run release test
        run: |
          ./dtest release --count 10 --timeout 20m --cleanup
```

## 参考资料

- [DevBox 文档](../../README.md)
- [DevBoxRelease API 定义](../../api/v1alpha2/devboxrelease_types.go)
- [发版控制器实现](../../internal/controller/devboxrelease_controller.go)
- [压测工具总体说明](./README.md)

## 贡献

欢迎提交 Issue 和 Pull Request 来改进测试工具！

如有问题，请在 GitHub 上创建 Issue 或联系维护者。

