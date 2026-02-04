# SSH 连接功能模块

## 概述

Devbox 提供 SSH 远程连接能力，允许用户通过 SSH 协议连接到运行中的 Devbox 容器进行开发工作。

## 架构设计

```
┌─────────────────────────────────────────────────────────────────┐
│                        用户终端                                   │
│                           │                                      │
│                    ssh -i key -p nodePort                        │
│                           │                                      │
│                           ▼                                      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                   Kubernetes 集群                          │   │
│  │                                                           │   │
│  │   ┌─────────────┐      ┌─────────────┐                   │   │
│  │   │  NodePort   │──────│   Devbox    │                   │   │
│  │   │  Service    │      │    Pod      │                   │   │
│  │   │ :nodePort   │      │  :22(SSH)   │                   │   │
│  │   └─────────────┘      └──────┬──────┘                   │   │
│  │                               │                           │   │
│  │                        ┌──────▼──────┐                   │   │
│  │                        │   Secret    │                   │   │
│  │                        │ (SSH Keys) │                   │   │
│  │                        └─────────────┘                   │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## 核心组件

### 1. SSH 密钥管理

#### 1.1 密钥生成

Controller 在创建 Devbox 时自动生成 Ed25519 SSH 密钥对：

**代码位置**: `internal/controller/helper/devbox.go`

```go
func GenerateSSHKeyPair() ([]byte, []byte, error) {
    pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
    // ...
    return sshPublicKey, privateKey, nil
}
```

#### 1.2 Secret 结构

密钥存储在与 Devbox 同名的 Secret 中：

| Key | 说明 |
|-----|------|
| `SEALOS_DEVBOX_PUBLIC_KEY` | SSH 公钥 |
| `SEALOS_DEVBOX_PRIVATE_KEY` | SSH 私钥 |
| `SEALOS_DEVBOX_AUTHORIZED_KEYS` | 授权公钥（用于 SSH 认证） |
| `SEALOS_DEVBOX_JWT_SECRET` | JWT 密钥（32位随机字符串） |
| `SEALOS_DEVBOX_ENV_PROFILE` | 环境变量脚本 |

**代码位置**: `internal/controller/devbox_controller.go` - `syncSecret()`

```go
secret := &corev1.Secret{
    ObjectMeta: objectMeta,
    Data: map[string][]byte{
        "SEALOS_DEVBOX_JWT_SECRET":      []byte(rand.String(32)),
        "SEALOS_DEVBOX_PUBLIC_KEY":      publicKey,
        "SEALOS_DEVBOX_PRIVATE_KEY":     privateKey,
        "SEALOS_DEVBOX_AUTHORIZED_KEYS": publicKey,
    },
}
```

### 2. Pod SSH 配置

#### 2.1 Volume 挂载

SSH 密钥通过 Secret Volume 挂载到 Pod 容器内：

**代码位置**: `internal/controller/helper/devbox.go`

```go
func GenerateSSHVolumeMounts() []corev1.VolumeMount {
    return []corev1.VolumeMount{
        {
            Name:      "devbox-ssh-keys",
            MountPath: "/usr/start/.ssh/authorized_keys",
            SubPath:   "authorized_keys",
            ReadOnly:  true,
        },
        {
            Name:      "devbox-ssh-keys",
            MountPath: "/usr/start/.ssh/id.pub",
            SubPath:   "id.pub",
            ReadOnly:  true,
        },
    }
}
```

#### 2.2 默认端口配置

Devbox 默认配置 SSH 端口（22）：

**代码位置**: `api/v1alpha2/devbox_types.go`

```go
// +kubebuilder:default={{name:"devbox-ssh-port",containerPort:22,protocol:TCP}}
Ports []corev1.ContainerPort `json:"ports,omitempty"`
```

### 3. 网络类型

Devbox 支持三种网络类型：

| 类型 | 说明 | 状态 |
|------|------|------|
| `NodePort` | 通过 NodePort Service 暴露 SSH 端口 | ✅ 主要使用 |
| `Tailnet` | 通过 Tailscale 网络 | ⚠️ 已废弃 |
| `SSHGate` | SSH 网关模式 | 🔄 开发中 |

**代码位置**: `api/v1alpha2/devbox_types.go`

```go
type NetworkType string

const (
    NetworkTypeNodePort NetworkType = "NodePort"
    NetworkTypeTailnet  NetworkType = "Tailnet"
    NetworkTypeSSHGate  NetworkType = "SSHGate"
)
```

### 4. NodePort 网络模式

#### 4.1 Service 创建

当网络类型为 `NodePort` 时，Controller 创建 NodePort Service：

**代码位置**: `internal/controller/devbox_controller.go` - `syncNodeport()`

```go
service := &corev1.Service{
    ObjectMeta: metav1.ObjectMeta{
        Name:      devbox.Name + "-svc",
        Namespace: devbox.Namespace,
        Labels:    recLabels,
    },
}
// ...
service.Spec.Type = corev1.ServiceTypeNodePort
```

#### 4.2 状态更新

NodePort 值存储在 Devbox Status 中：

```go
latestDevbox.Status.Network.NodePort = nodePort
```

### 5. UniqueID 机制

每个 Devbox 生成唯一标识符，用于 Headless Service 命名：

**代码位置**: `internal/controller/utils/rwords/rwords.go`

```go
// 格式: "{word1}-{word2}-{4chars}"
// 示例: "abandon-ability-abcd"
func GenerateRandomWords() string {
    return fmt.Sprintf("%s-%s-%s", 
        wordsList[Intn(listLength)], 
        wordsList[Intn(listLength)], 
        String(4))
}
```

UniqueID 存储在 `devbox.Status.Network.UniqueID`。

## 使用方式

### 获取连接信息

```bash
# 1. 获取私钥
kubectl get secret {devbox-name} -n {namespace} \
  -o jsonpath='{.data.SEALOS_DEVBOX_PRIVATE_KEY}' | base64 -d > /tmp/devbox_key
chmod 600 /tmp/devbox_key

# 2. 获取 NodePort
kubectl get devbox {devbox-name} -n {namespace} \
  -o jsonpath='{.status.network.nodePort}'

# 3. 获取节点 IP
kubectl get nodes -o wide
```

### SSH 连接

```bash
# 连接命令
ssh -i /tmp/devbox_key -p {nodePort} devbox@{node-ip}

# 示例
ssh -i /tmp/devbox_key -p 30022 devbox@192.168.1.100
```

### 连接参数说明

| 参数 | 说明 |
|------|------|
| `-i /tmp/devbox_key` | 指定私钥文件 |
| `-p {nodePort}` | NodePort 端口号 |
| `devbox` | 默认用户名 |
| `{node-ip}` | 集群节点 IP 地址 |

## 相关资源

### Kubernetes 资源

| 资源类型 | 名称格式 | 说明 |
|----------|----------|------|
| Secret | `{devbox-name}` | 存储 SSH 密钥 |
| Service (NodePort) | `{devbox-name}-svc` | 暴露 SSH 端口 |
| Service (Headless) | `{uniqueID}` | 内部服务发现 |
| Pod | `{devbox-name}` | Devbox 容器 |

### 代码文件索引

| 文件 | 职责 |
|------|------|
| `internal/controller/devbox_controller.go` | 主控制器，包含 Secret/Service/Pod 同步 |
| `internal/controller/helper/devbox.go` | 辅助函数：密钥生成、Volume 配置 |
| `api/v1alpha2/devbox_types.go` | API 类型定义 |
| `internal/controller/utils/rwords/rwords.go` | UniqueID 生成 |

## 状态流转

```
┌──────────────────────────────────────────────────────────────┐
│                     Devbox 创建流程                           │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│   1. syncSecret()                                            │
│      └── 生成 SSH 密钥对                                      │
│      └── 创建 Secret 资源                                     │
│                                                              │
│   2. syncNetwork()                                           │
│      ├── syncCommon()                                        │
│      │   └── 生成 UniqueID                                   │
│      │   └── 创建 Headless Service                           │
│      └── syncNodeport()                                      │
│          └── 创建 NodePort Service                           │
│          └── 更新 Status.Network.NodePort                    │
│                                                              │
│   3. syncPod()                                               │
│      └── 生成 Pod（挂载 SSH Volume）                          │
│      └── 创建 Pod 资源                                        │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

## 注意事项

1. **私钥安全**：私钥应妥善保管，不要泄露
2. **端口冲突**：NodePort 范围为 30000-32767，由 Kubernetes 自动分配
3. **网络策略**：确保节点防火墙允许 NodePort 端口访问
4. **用户权限**：默认用户为 `devbox`，工作目录为 `/home/devbox/project`

## 更新日志

| 日期 | 版本 | 更新内容 |
|------|------|----------|
| 2025-12-11 | v1.0 | 初始版本 |

