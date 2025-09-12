# Devbox 压测工具 - Cobra 版本使用指南

## **🎯 概述**

这是基于 Cobra 重构的 Devbox 压测工具，提供了更好的命令行体验和模块化架构。

## **🚀 快速开始**

### **安装和构建**

```bash
# 进入压测工具目录
cd controllers/devbox/test/stress

# 构建工具
make build

# 检查工具是否正常
make test
```

### **基本用法**

```bash
# 显示帮助信息
./devbox-stress --help

# 显示子命令帮助
./devbox-stress scale --help
./devbox-stress concurrent --help
./devbox-stress monitor --help
./devbox-stress cleanup --help
```

## **📋 命令详解**

### **1. 规模测试 (scale)**

测试集群创建大量 devbox 的能力。

```bash
# 基本用法
./devbox-stress scale --count 100

# 自定义配置
./devbox-stress scale \
  --count 200 \
  --interval 2s \
  --timeout 45m \
  --namespace test-namespace \
  --image ubuntu:22.04 \
  --cpu 200m \
  --memory 256Mi \
  --storage 2Gi

# 无清理模式
./devbox-stress scale --count 50 --cleanup=false
```

**参数说明**：
- `--count, -c`: devbox 数量（必需）
- `--interval, -i`: 创建间隔（默认: 1s）
- `--timeout, -t`: 测试超时时间（默认: 30m）
- `--cleanup`: 测试后是否清理（默认: true）

### **2. 并发测试 (concurrent)**

测试集群并发创建 devbox 的能力。

```bash
# 基本用法
./devbox-stress concurrent --count 50 --concurrent 10

# 高并发测试
./devbox-stress concurrent \
  --count 100 \
  --concurrent 25 \
  --timeout 20m \
  --namespace concurrent-test

# 自定义资源配置
./devbox-stress concurrent \
  --count 200 \
  --concurrent 50 \
  --cpu 500m \
  --memory 512Mi
```

**参数说明**：
- `--count, -c`: devbox 数量（必需）
- `--concurrent`: 并发级别（默认: 10）
- `--timeout, -t`: 测试超时时间（默认: 20m）
- `--cleanup`: 测试后是否清理（默认: true）

### **3. 资源监控 (monitor)**

持续监控集群资源状态。

```bash
# 基本用法
./devbox-stress monitor --duration 30m

# 自定义监控间隔
./devbox-stress monitor \
  --duration 1h \
  --interval 60s \
  --namespace production

# 短期监控
./devbox-stress monitor --duration 10m --interval 15s
```

**参数说明**：
- `--duration, -d`: 监控持续时间（必需）
- `--interval, -i`: 监控间隔（默认: 30s）

### **4. 资源清理 (cleanup)**

清理测试创建的 devbox 资源。

```bash
# 清理指定命名空间
./devbox-stress cleanup --namespace test-namespace

# 清理所有命名空间
./devbox-stress cleanup --all-namespaces

# 强制清理（不询问确认）
./devbox-stress cleanup --force

# 查看要清理的资源
./devbox-stress cleanup --namespace test --force=false
```

**参数说明**：
- `--all-namespaces`: 清理所有命名空间
- `--force, -f`: 强制删除，不询问确认

## **⚙️ 全局参数**

所有命令都支持以下全局参数：

```bash
./devbox-stress <command> \
  --kubeconfig /path/to/kubeconfig \
  --namespace my-namespace \
  --image ubuntu:22.04 \
  --cpu 200m \
  --memory 256Mi \
  --storage 2Gi \
  --verbose
```

**全局参数说明**：
- `--kubeconfig`: kubeconfig 文件路径
- `--namespace, -n`: Kubernetes 命名空间（默认: default）
- `--image`: devbox 镜像（默认: ubuntu:22.04）
- `--cpu`: CPU 资源（默认: 100m）
- `--memory`: 内存资源（默认: 128Mi）
- `--storage`: 存储限制（默认: 1Gi）
- `--verbose, -v`: 显示详细日志
- `--config`: 配置文件路径

## **📁 配置文件**

可以使用配置文件来设置默认参数：

```bash
# 复制示例配置文件
cp .devbox-stress.yaml.example .devbox-stress.yaml

# 编辑配置文件
vim .devbox-stress.yaml

# 使用配置文件
./devbox-stress scale --count 100
```

配置文件示例：
```yaml
namespace: "devbox-stress-test"
image: "ubuntu:22.04"
cpu: "200m"
memory: "256Mi"
storage: "2Gi"

scale:
  count: 100
  interval: "2s"
  timeout: "45m"
  cleanup: true

concurrent:
  count: 50
  concurrent: 15
  timeout: "25m"
```

## **🛠️ Makefile 快捷命令**

使用 Makefile 提供的快捷命令：

```bash
# 基础测试
make scale              # 规模测试 (100 个 devbox)
make concurrent         # 并发测试 (50 个 devbox, 10 并发)
make monitor           # 资源监控 (30分钟)

# 快速测试
make quick-scale       # 快速规模测试 (10, 20, 50)
make quick-concurrent  # 快速并发测试 (5, 10 并发)

# 自定义参数
make scale NAMESPACE=test CPU=200m MEMORY=256Mi
make concurrent NAMESPACE=prod STORAGE=2Gi

# 维护命令
make cleanup           # 清理测试环境
make status           # 查看集群状态
```

## **📊 输出示例**

### **规模测试输出**

```
开始规模测试...
配置: 数量=100, 间隔=1s, 超时=30m0s
资源: CPU=100m, Memory=128Mi, Storage=1Gi
镜像: ubuntu:22.04
命名空间: default

==================================================
               规模测试结果
==================================================
总 Devbox 数量: 100
成功创建: 95
创建失败: 5
成功率: 95.00%
平均创建时间: 2.5s
总测试时间: 4m30s
==================================================
```

### **并发测试输出**

```
开始并发测试...
配置: 数量=50, 并发=10, 超时=20m0s
资源: CPU=100m, Memory=128Mi, Storage=1Gi
镜像: ubuntu:22.04
命名空间: default

==================================================
               并发测试结果
==================================================
总 Devbox 数量: 50
成功创建: 48
创建失败: 2
成功率: 96.00%
平均创建时间: 1.8s
总测试时间: 1m45s
最大 QPS: 27.43
==================================================
```

## **🐛 故障排查**

### **常见问题**

#### **1. 命令未找到**
```bash
# 确保已构建
make build

# 检查二进制文件
ls -la devbox-stress
```

#### **2. 权限错误**
```bash
# 检查 kubeconfig
kubectl cluster-info

# 检查权限
kubectl auth can-i create devboxes
```

#### **3. 资源不足**
```bash
# 检查节点资源
kubectl top nodes

# 检查命名空间配额
kubectl describe quota -n your-namespace
```

### **调试模式**

```bash
# 启用详细日志
./devbox-stress scale --count 10 --verbose

# 检查具体错误
./devbox-stress scale --count 5 2>&1 | tee debug.log
```

## **🔧 开发和扩展**

### **添加新命令**

1. 在 `cmd/` 目录下创建新的命令文件
2. 实现命令逻辑
3. 在 `cmd/root.go` 中注册命令

### **扩展测试功能**

1. 在 `pkg/tester/` 中添加新的测试方法
2. 在相应的命令文件中调用新方法

### **自定义配置**

1. 修改 `cmd/root.go` 中的配置结构
2. 更新配置文件示例

## **📈 性能建议**

### **规模测试**
- 建议从小规模开始 (10-50)
- 根据集群性能调整 `--interval`
- 监控集群资源使用情况

### **并发测试**
- 并发数不要超过节点数的 2-3 倍
- 注意 API Server 的 QPS 限制
- 监控 etcd 性能

### **监控测试**
- 长时间监控时适当增加 `--interval`
- 结合其他测试一起使用
- 保存监控数据用于分析

## **🎉 总结**

Cobra 版本的压测工具提供了：

- ✅ **更好的命令行体验**：清晰的子命令和参数
- ✅ **模块化架构**：易于扩展和维护
- ✅ **配置文件支持**：方便批量测试
- ✅ **详细的帮助信息**：每个命令都有完整的帮助
- ✅ **灵活的参数配置**：支持全局和命令特定参数

使用这个工具可以全面测试 Devbox 的性能和稳定性！
