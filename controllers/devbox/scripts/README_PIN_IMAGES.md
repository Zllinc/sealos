# Pin Devbox Base Images Script

这个脚本用于批量 pin 当前节点上所有 devbox 的 base image，防止 Kubelet GC 删除镜像。

## 功能

- 自动获取当前节点上所有 devbox
- 只处理当前节点上的 devbox（跳过其他节点）
- 获取每个 devbox 当前 contentID 对应的 base image
- 在 `k8s.io` namespace 中 pin 这些镜像
- 支持 dry-run 模式预览操作

## 依赖要求

- `kubectl` - Kubernetes 命令行工具
- `ctr` - Containerd 命令行工具
- `jq` - JSON 处理工具

安装依赖：

```bash
# Ubuntu/Debian
sudo apt-get install -y kubectl jq

# CentOS/RHEL
sudo yum install -y kubectl jq

# macOS
brew install kubectl jq
```

## 使用方法

### 1. 基本用法

```bash
# 赋予执行权限
chmod +x pin_devbox_images.sh

# 运行脚本（使用 in-cluster config 或 ~/.kube/config）
./pin_devbox_images.sh
```

### 2. 指定节点名

```bash
# 如果自动检测节点名失败，可以手动指定
./pin_devbox_images.sh --node-name=sealos-staging-devbox-worker001
```

### 3. 指定 namespace

```bash
# 只处理特定 namespace 的 devbox
./pin_devbox_images.sh --namespace=devbox-test
```

### 4. Dry-run 模式（预览）

```bash
# 只打印会执行的操作，不实际 pin 镜像
./pin_devbox_images.sh --dry-run
```

### 5. 详细输出

```bash
# 显示所有跳过的 devbox 信息
./pin_devbox_images.sh --verbose
```

### 6. 使用环境变量

```bash
# 通过环境变量配置
export NODE_NAME=sealos-staging-devbox-worker001
export NAMESPACE=devbox-test
export DRY_RUN=true
export VERBOSE=true
./pin_devbox_images.sh
```

### 7. 完整参数列表

```
  -n, --namespace NAMESPACE    Filter devboxes by namespace (default: all namespaces)
  --node-name NODE_NAME        Node name to filter devboxes (default: auto-detect)
  --dry-run                    Dry run mode: only print what would be done
  -v, --verbose                Verbose output
  --containerd-address ADDR    Containerd address (default: unix:///var/run/containerd/containerd.sock)
  --kubeconfig PATH            Path to kubeconfig file (default: use in-cluster config or ~/.kube/config)
  -h, --help                   Show this help message
```

## 节点名检测逻辑

脚本按以下顺序尝试获取当前节点名：

1. `--node-name` 命令行参数
2. `NODE_NAME` 环境变量
3. 系统 hostname

## 筛选逻辑

脚本会跳过以下 devbox：

1. 没有 `contentID` 的 devbox
2. 没有分配节点的 devbox（`status.node` 为空）
3. 节点不是当前节点的 devbox
4. 没有 commit record 的 devbox
5. commit record 中没有 `baseImage` 的 devbox

## 示例输出

```
[INFO] ==========================================
[INFO] Pin Devbox Base Images Script
[INFO] ==========================================
[INFO] Current node name: sealos-staging-devbox-worker001
[INFO] Fetching devboxes...
[INFO] Found 5 devbox(es)
[INFO] Processing devbox devbox-test/test-devbox-1:
[INFO]   ContentID: 95702e7f-f0d2-4f5e-9f22-06dc2e73690a
[INFO]   Node: sealos-staging-devbox-worker001
[INFO]   BaseImage: ghcr.io/labring-actions/devbox/go-1.23.0:13aacd8
[INFO] ✓ Successfully pinned image: ghcr.io/labring-actions/devbox/go-1.23.0:13aacd8
[VERBOSE] SKIP: Devbox devbox-test/test-devbox-2 is on node sealos-staging-devbox-worker002, not current node sealos-staging-devbox-worker001

[INFO] ==========================================
[INFO] Summary:
[INFO]   Successfully processed: 3
[INFO]   Skipped: 2
[INFO]   Errors: 0
[INFO] ==========================================
```

## 验证 Pin 是否成功

### 方法 1：使用 ctr 命令

```bash
# 检查镜像是否被 pin
ctr -n k8s.io images inspect <baseImage> | jq -r '.labels."io.cri-containerd.pinned"'
# 应该输出：pinned
```

### 方法 2：使用 crictl 命令

```bash
# 列出所有镜像（包含 pinned 状态）
crictl images -o json | jq '.images[] | select(.pinned == true)'
```

## 注意事项

1. **权限要求**：
   - 需要能够访问 Kubernetes API（list devboxes）
   - 需要能够访问 containerd socket（pin images）
   - 建议在 DaemonSet 或 Job 中运行，确保有足够权限

2. **镜像必须存在**：
   - 如果镜像在 `k8s.io` namespace 中不存在，pin 操作会失败
   - 镜像需要先被 kubelet 拉取到节点上

3. **节点级别操作**：
   - 每个节点需要独立运行此脚本
   - 镜像 pin 是节点级别的，不会跨节点同步

4. **Dry-run 模式**：
   - 建议先使用 `--dry-run` 预览操作
   - 确认无误后再实际执行

## 在 Kubernetes 中运行

### 作为 DaemonSet 运行（推荐）

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: pin-devbox-images
  namespace: devbox-system
spec:
  selector:
    matchLabels:
      app: pin-devbox-images
  template:
    metadata:
      labels:
        app: pin-devbox-images
    spec:
      hostNetwork: true
      containers:
      - name: pin-images
        image: your-registry/pin-devbox-images:latest
        command: ["/bin/bash", "/scripts/pin_devbox_images.sh"]
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        volumeMounts:
        - name: containerd-sock
          mountPath: /var/run/containerd
        - name: scripts
          mountPath: /scripts
      volumes:
      - name: containerd-sock
        hostPath:
          path: /var/run/containerd
      - name: scripts
        configMap:
          name: pin-devbox-images-script
          defaultMode: 0755
```

### 作为 Job 运行（一次性执行）

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: pin-devbox-images-once
  namespace: devbox-system
spec:
  template:
    spec:
      hostNetwork: true
      nodeSelector:
        kubernetes.io/hostname: sealos-staging-devbox-worker001
      containers:
      - name: pin-images
        image: your-registry/pin-devbox-images:latest
        command: ["/bin/bash", "/scripts/pin_devbox_images.sh", "--node-name", "sealos-staging-devbox-worker001"]
        volumeMounts:
        - name: containerd-sock
          mountPath: /var/run/containerd
        - name: scripts
          mountPath: /scripts
      volumes:
      - name: containerd-sock
        hostPath:
          path: /var/run/containerd
      - name: scripts
        configMap:
          name: pin-devbox-images-script
          defaultMode: 0755
      restartPolicy: Never
```

### 创建 ConfigMap（包含脚本）

```bash
# 将脚本内容创建为 ConfigMap
kubectl create configmap pin-devbox-images-script \
  --from-file=pin_devbox_images.sh=./pin_devbox_images.sh \
  -n devbox-system
```

## 故障排查

### 错误：command not found: kubectl/ctr/jq

```
[ERROR] Missing required dependencies: kubectl ctr jq
```

**解决方案**：
- 安装缺失的依赖工具
- 确保工具在 PATH 中

### 错误：image not found

```
[ERROR] Image ghcr.io/xxx not found in namespace k8s.io
```

**解决方案**：
- 确认镜像已被 kubelet 拉取到节点
- 检查镜像名称是否正确
- 等待 Pod 启动后再运行脚本

### 错误：Failed to list devboxes

```
[ERROR] Failed to list devboxes. Check kubectl permissions and connectivity.
```

**解决方案**：
- 检查 kubeconfig 路径是否正确
- 确认有足够的 RBAC 权限
- 如果在 Pod 中运行，确认 ServiceAccount 有权限

### 错误：Failed to pin image

```
[ERROR] Failed to pin image: ghcr.io/xxx
```

**解决方案**：
- 确认 containerd socket 路径正确（默认：`/var/run/containerd/containerd.sock`）
- 检查是否有权限访问 socket
- 确认 containerd 服务正在运行
- 检查镜像是否存在于 `k8s.io` namespace

## 快速测试

```bash
# 1. 检查依赖
which kubectl ctr jq

# 2. 测试连接
kubectl get devboxes --all-namespaces
ctr -n k8s.io images ls | head -5

# 3. Dry-run 测试
./pin_devbox_images.sh --dry-run --verbose

# 4. 实际执行
./pin_devbox_images.sh
```

