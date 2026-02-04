# Kubernetes Ingress 核心概念

> 理解 Ingress、Service、DNS 和 LoadBalancer 的工作原理

---

## 目录

- [1. Ingress 工作原理](#1-ingress-工作原理)
- [2. DNS 解析流程](#2-dns-解析流程)
- [3. 核心组件关系](#3-核心组件关系)
- [4. Service 如何找到 Pod](#4-service-如何找到-pod)
- [5. 为什么需要 Ingress + Service](#5-为什么需要-ingress--service)
- [6. LoadBalancer 工作原理](#6-loadbalancer-工作原理)
- [7. Webhook 的作用](#7-webhook-的作用)
- [8. 云环境 vs 本地环境](#8-云环境-vs-本地环境)
- [9. 公网 IP vs 私网 IP](#9-公网-ip-vs-私网-ip)

---

## 1. Ingress 工作原理

### 1.1 什么是 Ingress？

**Ingress** 是 Kubernetes 的一个资源对象，用于管理外部访问集群内服务的 HTTP/HTTPS 路由规则。

**简单类比**:
```
Kubernetes 集群 = 一栋大楼
Service = 各个房间（办公室、会议室等）
Ingress = 大楼的前台/接待处

任务：
- 根据访客要求，指引到正确的房间
- 根据访问时间（路径），安排到不同的区域
```

### 1.2 完整请求流程

```
用户: http://myapp.local
    │ 1. DNS 解析
    ▼
DNS: myapp.local → 192.168.12.200 (Ingress Controller IP)
    │ 2. 发送 HTTP 请求
    ▼
┌──────────────────────────────────────────────┐
│ Ingress Controller (Nginx/Traefik)          │
│                                              │
│ 接收请求: Host: myapp.local                │
│                                              │
│ 匹配规则:                                   │
│   host: myapp.local → 找到对应 Ingress    │
│   path: / → 路由到 backend                 │
└──────────────────────────────────────────────┘
    │ 3. 转发到 Service
    ▼
┌──────────────────────────────────────────────┐
│ Service: myapp-service                      │
│ ClusterIP: 20.98.94.88                      │
│                                              │
│ 负载均衡到后端 Pod                           │
└──────────────────────────────────────────────┘
    │ 4. 分发流量
    ▼
┌──────────────────────────────────────────────┐
│ Endpoints: [10.0.0.203, 10.0.0.239]        │
│                                              │
│ 选中: 10.0.0.203 (轮询)                      │
└──────────────────────────────────────────────┘
    │ 5. 处理请求
    ▼
┌──────────────────────────────────────────────┐
│ Pod: 10.0.0.203                             │
│                                              │
│ 处理 HTTP 请求 → 返回响应                   │
└──────────────────────────────────────────────┘
```

### 1.3 关键组件

| 组件 | 作用 | 类型 |
|------|------|------|
| **Ingress** | 路由规则配置 | 资源对象（YAML） |
| **Ingress Controller** | 实际处理流量的组件 | 软件（Nginx/Traefik） |
| **Service** | 服务发现、负载均衡 | 抽象层 |
| **Pod** | 运行应用容器 | 实体 |

**关键点**:
- Ingress 只是**配置**，不处理流量
- Ingress Controller 处理实际流量
- 一个 Ingress Controller 可以处理多个 Ingress 规则

---

## 2. DNS 解析流程

### 2.1 DNS 记录类型

```bash
# A 记录（直接指向 IP）
myapp.local A 192.168.12.200

# CNAME 记录（域名别名）
app.example.com CNAME myapp.sealos.io

# 解析链:
# app.example.com → myapp.sealos.io → 192.168.12.200
```

### 2.2 完整 DNS 查询过程

```
用户浏览器输入: myapp.local
    │
    ▼ 1. 浏览器缓存检查
    ├─ 有记录 → 直接使用
    └─ 无记录 → 继续
    │
    ▼ 2. 操作系统缓存检查
    ├─ /etc/hosts 有记录 → 使用
    └─ 无记录 → 继续
    │
    ▼ 3. 查询本地 DNS 服务器
    │
    ▼ 4. 递归查询
    Root Server → .local TLD Server → Authoritative DNS
    │
    ▼ 5. 返回结果
    返回 IP: 192.168.12.200
```

### 2.3 本地 DNS 配置

```bash
# 方法 1: 修改 /etc/hosts（测试用）
echo "192.168.12.200 myapp.local" | sudo tee -a /etc/hosts

# 验证
ping myapp.local
```

---

## 3. 核心组件关系

### 3.1 组件依赖关系

```
Pod (有标签: app=myapp)
    ↓ selector
Service (selector: app=myapp)
    ↓ name 引用
Ingress (service.name: myapp-service)
    ↓ host 匹配
DNS (myapp.local → LoadBalancer IP)
```

### 3.2 标签与选择器

**Service 选择 Pod**:
```yaml
# Pod 定义
metadata:
  labels:
    app: myapp    # Pod 的标签

# Service 定义
spec:
  selector:
    app: myapp    # 必须与 Pod 标签一致
```

**Ingress 引用 Service**:
```yaml
# Ingress 定义
spec:
  rules:
  - host: myapp.local
    http:
      paths:
      - backend:
          service:
            name: myapp-service    # 直接使用 Service 名称
            port:
              number: 80
```

### 3.3 关键点

✅ **Service 通过标签选择 Pod**
✅ **Ingress 通过名称引用 Service**
❌ **Ingress 不通过标签选择 Service**

---

## 4. Service 如何找到 Pod

### 4.1 自动发现机制

```
1. 创建 Pod
   ↓
2. Endpoint Controller 监听 Pod 事件
   ↓
3. 根据 Service 的 selector 匹配 Pod labels
   ↓
4. 自动创建/更新 Endpoints 对象
   ↓
5. Service 通过 Endpoints 获取 Pod IP 列表
```

### 4.2 Endpoints 对象

```yaml
apiVersion: v1
kind: Endpoints
metadata:
  name: myapp-service
subsets:
- addresses:
  - ip: 10.0.0.203    # Pod 1
    nodeName: node-1
    targetRef:
      kind: Pod
      name: myapp-pod-1
  - ip: 10.0.0.239    # Pod 2
    nodeName: node-1
    targetRef:
      kind: Pod
      name: myapp-pod-2
  ports:
  - port: 80
```

**自动化特性**:
- Pod 创建 → 自动添加到 Endpoints
- Pod 删除 → 自动从 Endpoints 移除
- Pod 不健康 → 自动从 Endpoints 移除
- Pod IP 变化 → 自动更新 Endpoints

---

## 5. 为什么需要 Ingress + Service

### 5.1 问题：如果不用 Ingress

```
场景: 3 个 HTTP 服务需要对外访问

方案 A: 每个 Service 一个 LoadBalancer
┌────────────────────────────────────┐
│ Service A: LoadBalancer            │
│   公网 IP: 1.2.3.4                  │
│   成本: $20/月                      │
├────────────────────────────────────┤
│ Service B: LoadBalancer            │
│   公网 IP: 1.2.3.5                  │
│   成本: $20/月                      │
├────────────────────────────────────┤
│ Service C: LoadBalancer            │
│   公网 IP: 1.2.3.6                  │
│   成本: $20/月                      │
└────────────────────────────────────┘
总成本: $60/月
```

### 5.2 方案：使用 Ingress + Service

```
┌────────────────────────────────────┐
│ Ingress Controller                │
│   公网 IP: 1.2.3.4                  │
│   成本: $20/月                      │
│                                      │
│   ┌──────────────────────────────┐  │
│   │ myapp.com  → Service A      │  │
│   │ api.com     → Service B      │  │
│   │ blog.com    → Service C      │  │
│   └──────────────────────────────┘  │
└────────────────────────────────────┘
总成本: $20/月（节省 67%）
```

### 5.3 对比总结

| 特性 | 多个 LoadBalancer | Ingress + Service |
|------|-------------------|-------------------|
| **公网 IP** | N 个（每个服务一个） | 1 个（共用） |
| **成本** | N × $20/月 | $20/月 |
| **路由能力** | 4 层（IP:端口） | 7 层（域名/路径） |
| **SSL 证书** | N 个（每个配置） | 1 个（统一配置） |
| **适用场景** | 非 HTTP 服务 | HTTP/HTTPS 服务 |

---

## 6. LoadBalancer 工作原理

### 6.1 云环境

```
你创建: LoadBalancer Service
    ↓
Kubernetes Cloud Controller Manager
    ↓
调用云厂商 API (CreateLoadBalancer)
    ↓
云厂商创建负载均衡器
    ↓
分配公网 IP: 1.2.3.4
    ↓
自动配置后端服务器（K8s 节点）
```

**配置示例**:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  namespace: ingress-nginx
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
spec:
  type: LoadBalancer  # ← 关键
  selector:
    app: ingress-nginx
  ports:
  - port: 80
    targetPort: 80
```

### 6.2 本地/裸机环境（使用 MetalLB）

```
你创建: LoadBalancer Service
    ↓
MetalLB Controller 监听
    ↓
从 IPAddressPool 分配 IP: 192.168.12.200
    ↓
MetalLB Speaker 使用 ARP 广播
    ↓
告诉网络: "192.168.12.200 在节点 X"
```

**配置示例**:
```yaml
# IPAddressPool
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: ingress-ips
  namespace: metallb-system
spec:
  addresses:
  - 192.168.12.200-192.168.12.250

---
# L2Advertisement
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: ingress-l2
  namespace: metallb-system
spec:
  ipAddressPools:
  - ingress-ips
```

### 6.3 关键区别

| 特性 | 云环境 | MetalLB |
|------|--------|---------|
| **IP 分配** | 云厂商自动 | MetalLB 从配置池分配 |
| **网络广播** | 云厂商处理 | Speaker 使用 ARP/BGP |
| **成本** | $15-30/月 | 免费 |
| **限制** | 无 | L2 模式仅限同网段 |

---

## 7. Webhook 的作用

### 7.1 Webhook 的位置

```
用户创建 Ingress
    │
    ▼ API Server
    │
    ├─→ Mutating Webhook（修改）
    │   - 自动添加注解
    │
    ├─→ Validating Webhook（验证）← Sealos Webhook
    │   - CNAME 验证
    │   - 所有权验证
    │   - ICP 备案验证
    │   ✅ 通过 → 继续
    │   ❌ 拒绝 → 返回错误
    │
    ▼ etcd（持久化）
    │
    ▼ Informer（通知）
    │
    ▼ Ingress Controller
    - 更新配置
    - 重载 Nginx
```

### 7.2 Sealos Webhook 验证逻辑

```go
// 1. 身份验证
if !isUserServiceAccount(request.UserInfo.Username) {
    return nil  // 非用户 SA，跳过
}

if !isUserNamespace(i.Namespace) {
    return nil  // 非 NS，跳过
}

// 2. CNAME 验证
cname, _ := net.LookupCNAME(rule.Host)
if !strings.HasSuffix(cname, "sealos.io") {
    return error  // CNAME 不指向系统域名
}

// 3. 所有权验证
existingIngress := cache.List(host)
for _, ingress := range existingIngress {
    if ingress.Namespace != i.Namespace {
        return error  // 域名已被占用
    }
}

// 4. ICP 备案验证（可选）
if icpEnabled {
    icp := queryICP(rule.Host)
    if !icp.isLicensed {
        return error  // 未备案
    }
}

return success
```

---

## 8. 云环境 vs 本地环境

### 8.1 环境类型

| 类型 | 示例 | LoadBalancer | 维护 |
|------|------|-------------|------|
| **托管 K8s** | AWS EKS、阿里云 ACK | ✅ 云厂商提供 | 低 |
| **云 VM 自建** | 阿里云 ECS + kubeadm | ❌ 需要 MetalLB | 中 |
| **裸机/本地** | PVE + kubespray | ❌ 需要 MetalLB | 高 |

### 8.2 你的环境

```
PVE 上的 Kubernetes = 裸机环境

特点:
✅ 完全控制
✅ 免费
✅ 适合学习
❌ 需要 MetalLB 实现 LoadBalancer
❌ 需要 NodePort 或反向代理暴露服务
```

---

## 9. 公网 IP vs 私网 IP

### 9.1 私网 IP（无法直接从互联网访问）

```
范围:
- 10.0.0.0    - 10.255.255.255    (10.0.0.0/8)
- 172.16.0.0  - 172.31.255.255    (172.16.0.0/12)
- 192.168.0.0 - 192.168.255.255   (192.168.0.0/16)

特点:
❌ 无法直接从互联网访问
✅ 只在局域网内有效
✅ 全球可以重复使用

示例:
- 你家里: 192.168.1.1
- 你公司: 192.168.1.1
- 它们可以相同（不在同一网络）
```

### 9.2 公网 IP（全球可访问）

```
范围: 除了私网 IP 段之外的所有 IP

特点:
✅ 可以从全球任何地方访问
✅ 全球唯一分配
❌ 需要向 ISP 申请
❌ 通常需要付费

示例:
- 8.8.8.8 (Google DNS)
- 1.1.1.1 (Cloudflare DNS)
- 47.96.123.45 (阿里云 ECS)
```

### 9.3 NAT（网络地址转换）

```
家庭网络:
多台设备: 192.168.1.100-192.168.1.200
    ↓
路由器: 192.168.1.1
    ↓ NAT
公网 IP: 1.2.3.4
    ↓
互联网

作用:
1. 多个私网 IP 共享一个公网 IP
2. 隐藏内网结构
3. 节省公网 IP 资源
```

---

## 总结

### 关键要点

1. **Ingress** = 路由规则配置，不处理流量
2. **Ingress Controller** = 实际处理流量的组件
3. **Service** = 服务发现和负载均衡
4. **Service 通过标签选择 Pod**，**Ingress 通过名称引用 Service**
5. **公网 IP 全球可访问**，**私网 IP 仅局域网可访问**

### 学习建议

```
理解原理 → 阅读本部分（01-concepts.md）
    ↓
快速实践 → 跟随快速开始（02-quick-start.md）
    ↓
深入学习 → 查看完整指南（03-complete-guide.md）
    ↓
解决问题 → 参考故障排查（04-troubleshooting.md）
```

---

**下一步**: [02-quick-start.md](./02-quick-start.md) 🚀
