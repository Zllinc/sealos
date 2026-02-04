# Kubernetes Ingress 最小实践完整指南

> 从零到公网可访问的完整实践手册，包含所有遇到的问题和解决方案

**作者**: cunzili
**日期**: 2025-01-15
**适用环境**: 本地学习、测试、生产参考

---

## 📋 目录

- [第一部分：核心概念](#第一部分核心概念)
  - [1.1 Ingress 工作原理](#11-ingress-工作原理)
  - [1.2 DNS 解析流程](#12-dns-解析流程)
  - [1.3 核心组件关系](#13-核心组件关系)
  - [1.4 为什么需要 Ingress + Service](#14-为什么需要-ingress--service)
- [第二部分：深入理解](#第二部分深入理解)
  - [2.1 LoadBalancer 工作原理](#21-loadbalancer-工作原理)
  - [2.2 Webhook 的作用](#22-webhook-的作用)
  - [2.3 云环境 vs 本地环境](#23-云环境-vs-本地环境)
  - [2.4 公网 IP vs 私网 IP](#24-公网-ip-vs-私网-ip)
- [第三部分：实践步骤](#第三部分实践步骤)
  - [3.1 环境准备](#31-环境准备)
  - [3.2 部署应用](#32-部署应用)
  - [3.3 创建 Service](#33-创建-service)
  - [3.4 安装 Ingress Controller](#34-安装-ingress-controller)
  - [3.5 创建 Ingress](#35-创建-ingress)
  - [3.6 配置访问](#36-配置访问)
- [第四部分：问题与解决](#第四部分问题与解决)
  - [4.1 Service Endpoints 为空](#41-service-endpoints-为空)
  - [4.2 Pod 无法调度（污点问题）](#42-pod-无法调度污点问题)
  - [4.3 Secret 未找到](#43-secret-未找到)
  - [4.4 Ingress ADDRESS 为空](#44-ingress-address-为空)
  - [4.5 集群外无法访问](#45-集群外无法访问)
- [第五部分：生产环境配置](#第五部分生产环境配置)
  - [5.1 方案对比](#51-方案对比)
  - [5.2 云厂商 LoadBalancer](#52-云厂商-loadbalancer)
  - [5.3 完整配置示例](#53-完整配置示例)
- [第六部分：故障排查](#第六部分故障排查)
  - [6.1 常用诊断命令](#61-常用诊断命令)
  - [6.2 问题定位流程](#62-问题定位流程)
  - [6.3 日志查看技巧](#63-日志查看技巧)

---

## 第一部分：核心概念

### 1.1 Ingress 工作原理

#### 完整请求流程

```
用户浏览器
    │ 1. 输入: http://myapp.local
    ▼
┌──────────────────────────────────────────┐
│ DNS 解析                                  │
│ myapp.local → 192.168.12.200             │
└──────────────────────────────────────────┘
    │ 2. TCP 连接
    ▼
┌──────────────────────────────────────────┐
│ Ingress Controller                       │
│ (Nginx/Traefik/APISIX)                   │
│                                          │
│ 接收请求: Host: myapp.local              │
└──────────────────────────────────────────┘
    │ 3. 匹配 Ingress 规则
    ▼
┌──────────────────────────────────────────┐
│ Ingress 资源配置                         │
│ host: myapp.local                        │
│ path: /                                  │
│ backend: myapp-service:80                │
└──────────────────────────────────────────┘
    │ 4. 路由到 Service
    ▼
┌──────────────────────────────────────────┐
│ Service: myapp-service                   │
│ ClusterIP: 20.98.94.88                   │
│ Port: 80 → TargetPort: 80                │
└──────────────────────────────────────────┘
    │ 5. 负载均衡
    ▼
┌──────────────────────────────────────────┐
│ Endpoints (Pod IPs)                      │
│ - 10.0.0.203:80                          │
│ - 10.0.0.239:80                          │
│ - 10.0.0.44:80                           │
└──────────────────────────────────────────┘
    │ 6. 选中一个 Pod
    ▼
┌──────────────────────────────────────────┐
│ Pod: 10.0.0.203:80                       │
│ 处理请求 → 返回 nginx 欢迎页面            │
└──────────────────────────────────────────┘
```

#### 关键点

| 组件 | 作用 | 可访问性 |
|------|------|----------|
| **Ingress Controller** | 接收外部流量，根据 Host/Path 路由 | 需要公网 IP 或 NodePort |
| **Ingress** | 路由规则配置 | 仅是配置文件，不直接处理流量 |
| **Service** | 服务发现、负载均衡 | 集群内虚拟 IP |
| **Pod** | 运行实际应用 | 集群内 Pod IP |

### 1.2 DNS 解析流程

#### DNS 记录类型

```bash
# A 记录（直接指向 IP）
myapp.local A 192.168.12.200

# CNAME 记录（指向另一个域名）
app.example.com CNAME myapp.sealos.io

# 优先级: A 记录优先于 CNAME
```

#### DNS 查询过程

```
1. 浏览器缓存
   ↓ (未找到)
2. 操作系统缓存 (/etc/hosts)
   ↓ (未找到)
3. 本地 DNS 服务器
   ↓
4. 根服务器 (.)
   ↓
5. 顶级域名服务器 (.local)
   ↓
6. 权威 DNS 服务器
   ↓
返回 IP: 192.168.12.200
```

### 1.3 核心组件关系

```
Pod (有标签: app=myapp)
    ↓ labels
Service (selector: app=myapp)
    ↓ name 引用
Ingress (service.name: myapp-service)
    ↓ host 匹配
DNS (myapp.local → LoadBalancer IP)
    ↓ HTTP 请求
Ingress Controller (处理流量)
```

**关键点**：
- Service 通过 **标签** 选择 Pod
- Ingress 通过 **名称** 引用 Service
- Ingress 不通过标签，而是直接使用 Service 名称

### 1.4 为什么需要 Ingress + Service

| 方案 | 成本 | 功能 | 适用场景 |
|------|------|------|----------|
| **Ingress + Service** | 低（1 个公网 IP） | 丰富（基于域名/路径路由） | 多个 HTTP 服务（推荐） |
| **多个 LoadBalancer** | 高（N 个公网 IP） | 简单 | 单个非 HTTP 服务 |

**成本对比示例**：
```
方案 A: 每个 Service 一个 LoadBalancer
- app Service: $20/月
- api Service: $20/月
- blog Service: $20/月
总计: $60/月

方案 B: Ingress + Service
- Ingress Controller: $20/月
- 所有 Service: ClusterIP（免费）
总计: $20/月（节省 67%）
```

---

## 第二部分：深入理解

### 2.1 LoadBalancer 工作原理

#### 云环境

```
你创建 LoadBalancer Service
    ↓
Kubernetes API Server
    ↓
Cloud Controller Manager 检测到类型
    ↓
调用云厂商 API (CreateLoadBalancer)
    ↓
云厂商创建负载均衡器
    ↓
分配公网 IP: 1.2.3.4
    ↓
自动配置后端（K8s 节点）
```

#### 本地/裸机环境（使用 MetalLB）

```
你创建 LoadBalancer Service
    ↓
MetalLB Controller 监听到变化
    ↓
从 IPAddressPool 分配 IP: 192.168.12.200
    ↓
MetalLB Speaker 使用 ARP/NDP 广播
    ↓
告诉网络: "192.168.12.200 在节点 X 上"
    ↓
外部流量可以访问
```

#### 关键区别

| 特性 | 云环境 | 裸机 + MetalLB |
|------|--------|--------------|
| **IP 分配** | 云厂商自动 | MetalLB 从配置的池分配 |
| **网络广播** | 云厂商处理 | MetalLB Speaker 使用 ARP/BGP |
| **成本** | $15-30/月 | 免费（但需要服务器） |
| **限制** | 无 | L2 模式仅限同网段 |

### 2.2 Webhook 的作用

#### Webhook 在 API Server 和 Ingress Controller 之间

```
用户创建 Ingress
    │
    ▼ API Server
    │
    ├─→ Mutating Webhook (修改)
    │   - 自动添加注解
    │
    ├─→ Validating Webhook (验证) ← Sealos Webhook
    │   - CNAME 验证
    │   - 所有权验证
    │   - ICP 备案验证（可选）
    │   ✅ 通过 → 继续
    │   ❌ 拒绝 → 返回错误
    │
    ▼ etcd (持久化)
    │
    ▼ Informer (通知)
    │
    ▼ Ingress Controller
    - 更新配置
    - 重载 Nginx
```

#### Sealos Webhook 的验证逻辑

1. **身份验证**：是否是用户 ServiceAccount？
2. **CNAME 验证**：域名是否指向系统域名？
3. **所有权验证**：域名是否被其他 NS 占用？
4. **ICP 验证**：域名是否已备案（中国）？

### 2.3 云环境 vs 本地环境

#### 类型对比

| 类型 | 示例 | LoadBalancer | 维护成本 |
|------|------|-------------|----------|
| **托管 K8s** | AWS EKS、阿里云 ACK | ✅ 云厂商提供 | 低 |
| **云 VM 自建** | 阿里云 ECS + kubeadm | ❌ 需要 MetalLB | 中 |
| **裸机/本地** | PVE、VMware | ❌ 需要 MetalLB | 高 |

#### 你的环境

```
PVE 上的 Kubernetes = 裸机环境

特点:
- 完全控制
- 免费
- 适合学习
- 需要 MetalLB 实现 LoadBalancer
- 需要 NodePort 或反向代理暴露服务
```

### 2.4 公网 IP vs 私网 IP

#### 私网 IP（无法直接从互联网访问）

```
范围:
- 10.0.0.0    - 10.255.255.255    (10.0.0.0/8)
- 172.16.0.0  - 172.31.255.255    (172.16.0.0/12)
- 192.168.0.0 - 192.168.255.255   (192.168.0.0/16)

特点:
- 仅在局域网内有效
- 全球可以重复使用
- 无法直接从互联网访问

示例:
- 家里路由器: 192.168.1.1
- 公司路由器: 192.168.1.1
- 它们可以相同，因为不在同一网络
```

#### 公网 IP（全球可访问）

```
范围: 除了私网 IP 段之外的所有 IP

特点:
- 全球唯一分配
- 可以从全球任何地方访问
- 需要向 ISP 申请
- 通常需要付费

示例:
- 8.8.8.8 (Google DNS)
- 1.1.1.1 (Cloudflare DNS)
- 47.96.123.45 (阿里云 ECS 公网 IP)
```

#### NAT（网络地址转换）

```
家庭网络:
电脑 A: 192.168.1.100 ─┐
电脑 B: 192.168.1.101 ─┼─→ 路由器: 192.168.1.1
电脑 C: 192.168.1.102 ─┘        ↓
                         公网 IP: 1.2.3.4
                               ↓
                          互联网

作用:
1. 多个私网 IP 共享一个公网 IP
2. 隐藏内网结构
3. 节省公网 IP 资源
```

---

## 第三部分：实践步骤

### 3.1 环境准备

#### 检查环境

```bash
# 1. 检查 Kubernetes 集群
kubectl cluster-info

# 预期输出:
# Kubernetes control plane is running at ...
# CoreDNS is running at ...

# 2. 检查节点状态
kubectl get nodes

# 预期输出:
# NAME     STATUS   ROLES           AGE   VERSION
# master   Ready    control-plane   10d   v1.28.x
# worker   Ready    <none>          10d   v1.28.x

# 3. 检查是否有污点
kubectl describe nodes | grep Taints

# 如果有污点，记录下来（后面需要）
```

#### 准备镜像

```bash
# 使用国内可访问的镜像
# 推荐: registry.cn-hangzhou.aliyuncs.com/google_containers/nginx:1.25

# 或手动拉取
crictl pull registry.cn-hangzhou.aliyuncs.com/google_containers/nginx:1.25
```

### 3.2 部署应用

#### 创建 Deployment

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  labels:
    app: myapp
spec:
  replicas: 2
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
      - name: myapp
        # 使用国内镜像
        image: registry.cn-hangzhou.aliyuncs.com/google_containers/nginx:1.25
        ports:
        - containerPort: 80
          name: http  # 重要：给端口命名
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 200m
            memory: 256Mi
        livenessProbe:
          httpGet:
            path: /
            port: http
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /
            port: http
          initialDelaySeconds: 10
          periodSeconds: 5
```

```bash
# 部署
kubectl apply -f deployment.yaml

# 验证
kubectl get deployment myapp
kubectl get pods -l app=myapp

# 预期输出:
# NAME                     READY   STATUS    RESTARTS   AGE
# myapp-xxx-xxx            1/1     Running   0          1m

# 进入 Pod 测试
kubectl exec -it <pod-name> -- /bin/sh

# 在 Pod 内:
# wget -O- -q http://localhost/
# exit
```

### 3.3 创建 Service

#### 问题 1: Endpoints 为空

**症状**：
```bash
kubectl get endpoints myapp-service
# NAME            ENDPOINTS   AGE
# myapp-service   <none>      1m
```

**原因**: Service 的 targetPort 使用了端口名称，但 Pod 没有给端口命名。

**解决**：
```yaml
# service.yaml（正确配置）
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
spec:
  selector:
    app: myapp
  ports:
  - name: http
    port: 80
    targetPort: http  # 使用端口名称（Pod 中定义的）
  type: ClusterIP
```

**或者直接使用端口号**（更简单）:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
spec:
  selector:
    app: myapp
  ports:
  - name: http
    port: 80
    targetPort: 80  # 直接使用端口号
  type: ClusterIP
```

```bash
# 应用配置
kubectl apply -f service.yaml

# 验证
kubectl get svc myapp-service
kubectl get endpoints myapp-service

# 预期输出:
# NAME            ENDPOINTS                          AGE
# myapp-service   10.0.0.203:80,10.0.0.239:80        1m
#                            ↑ 必须有 IP！

# 测试 Service
kubectl run test --image=busybox:1.28 --rm -it --restart=Never -- \
  wget -O- -q http://myapp-service/
```

### 3.4 安装 Ingress Controller

#### 问题 2: 节点有污点，Pod 无法调度

**症状**：
```bash
kubectl get pods -n ingress-nginx
# NAME                                        READY   STATUS    RESTARTS   AGE
# ingress-nginx-controller-xxx                0/1     Pending   0          5m

kubectl describe pod -n ingress-nginx ingress-nginx-controller-xxx
# Warning  FailedScheduling  0/2 nodes are available: 2 node(s) had untolerated taint {devbox.sealos.io/node: }.
```

**解决**：给 Deployment 添加容忍

```bash
kubectl patch deployment ingress-nginx-controller -n ingress-nginx --type=json -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/tolerations",
    "value": [
      {
        "key": "devbox.sealos.io/node",
        "operator": "Exists",
        "effect": "NoSchedule"
      }
    ]
  }
]'
```

#### 安装 Ingress Controller

```bash
# 方式 1: 使用官方 YAML
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.9.4/deploy/static/provider/cloud/deploy.yaml

# 等待 Pod 就绪
kubectl get pods -n ingress-nginx -w

# 预期输出:
# NAME                                       READY   STATUS    RESTARTS   AGE
# ingress-nginx-controller-xxx               1/1     Running   0          2m
```

#### 问题 3: admission-webhook Secret 未找到

**症状**：
```bash
kubectl get pods -n ingress-nginx
# NAME                                        READY   STATUS              RESTARTS   AGE
# ingress-nginx-admission-create-xxx          0/1     Pending             0          5m
# ingress-nginx-admission-patch-xxx           0/1     Pending             0          5m
# ingress-nginx-controller-xxx                0/1     ContainerCreating   0          2m

kubectl describe pod -n ingress-nginx ingress-nginx-controller-xxx
# Warning  FailedMount  secret "ingress-nginx-admission" not found
```

**原因**：init Job 无法调度（没有容忍污点），导致 Secret 未创建。

**解决**：给 Job 也添加容忍

```bash
# 方式 1: 使用 patch
kubectl patch job ingress-nginx-admission-create -n ingress-nginx --type=json -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/tolerations",
    "value": [
      {
        "key": "devbox.sealos.io/node",
        "operator": "Exists",
        "effect": "NoSchedule"
      }
    ]
  }
]'

kubectl patch job ingress-nginx-admission-patch -n ingress-nginx --type=json -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/tolerations",
    "value": [
      {
        "key": "devbox.sealos.io/node",
        "operator": "Exists",
        "effect": "NoSchedule"
      }
    ]
  }
]'

# 方式 2: 下载 YAML 修改后重新部署
wget https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.9.4/deploy/static/provider/cloud/deploy.yaml

# 编辑文件，在 Deployment 和 Job 的 spec.template.spec 下添加:
# tolerations:
# - key: devbox.sealos.io/node
#   operator: Exists
#   effect: NoSchedule

# 重新部署
kubectl delete -f deploy.yaml
kubectl apply -f deploy.yaml

# 等待 Job 完成
kubectl get job -n ingress-nginx -w

# 预期输出:
# NAME                             COMPLETIONS   DURATION   AGE
# ingress-nginx-admission-create   1/1           30s        2m
# ingress-nginx-admission-patch    1/1           25s        2m
```

### 3.5 创建 Ingress

```yaml
# ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
spec:
  ingressClassName: nginx
  rules:
  - host: myapp.local
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: myapp-service
            port:
              number: 80
```

```bash
# 应用配置
kubectl apply -f ingress.yaml

# 验证
kubectl get ingress myapp-ingress

# 预期输出:
# NAME            CLASS   HOSTS         ADDRESS   PORTS   AGE
# myapp-ingress   nginx   myapp.local             80      1m
#                                         ↑
#                           现在是空的（正常）
```

### 3.6 配置访问

#### 方案 1: 使用 MetalLB（本实验）

```bash
# 1. 安装 MetalLB
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/main/config/manifests/metallb-native.yaml

# 2. 配置 IP 地址池
cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: ingress-ips
  namespace: metallb-system
spec:
  addresses:
  - 192.168.12.200-192.168.12.250
EOF

# 3. 配置 L2 广播
cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: ingress-l2
  namespace: metallb-system
spec:
  ipAddressPools:
  - ingress-ips
EOF

# 4. 修改 Service 为 LoadBalancer
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"LoadBalancer"}}'

# 5. 等待 IP 分配
sleep 10
kubectl get svc -n ingress-nginx ingress-nginx-controller

# 预期输出:
# NAME                       TYPE           EXTERNAL-IP      PORT(S)
# ingress-nginx-controller   LoadBalancer   192.168.12.200   80:xxxxx/TCP
#                                            ↑
#                                 MetalLB 分配的 IP

# 6. 查看更新后的 Ingress
kubectl get ingress myapp-ingress

# 预期输出:
# NAME            CLASS   HOSTS         ADDRESS          PORTS   AGE
# myapp-ingress   nginx   myapp.local   192.168.12.200   80      5m
#                                            ↑
#                                  现在有 ADDRESS 了！

# 7. 配置本地 DNS
LB_IP=192.168.12.200
echo "$LB_IP myapp.local" | sudo tee -a /etc/hosts

# 8. 测试访问
curl http://myapp.local/

# 预期输出: HTML 内容（nginx 欢迎页面）
```

#### 方案 2: 使用 NodePort（替代方案）

```bash
# 1. 修改 Service 为 NodePort
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"NodePort"}}'

# 2. 查看 NodePort
kubectl get svc -n ingress-nginx ingress-nginx-controller

# 输出示例:
# NAME                       TYPE       PORT(S)
# ingress-nginx-controller   NodePort   80:32768/TCP,443:32345/TCP
#                                        ↑
#                                  HTTP 的 NodePort

# 3. 获取节点 IP
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')

# 4. 配置 DNS
echo "$NODE_IP myapp.local" | sudo tee -a /etc/hosts

# 5. 测试访问（带端口）
curl http://myapp.local:32768/
```

---

## 第四部分：问题与解决

### 4.1 Service Endpoints 为空

#### 症状

```bash
kubectl get endpoints myapp-service
# NAME            ENDPOINTS   AGE
# myapp-service   <none>      10m
```

#### 原因

Service 的 selector 与 Pod 的 labels 不匹配，或端口配置错误。

#### 排查步骤

```bash
# 1. 检查 Pod labels
kubectl get pods --show-labels

# 2. 检查 Service selector
kubectl get svc myapp-service -o jsonpath='{.spec.selector}'

# 3. 验证匹配
kubectl get pods -l app=myapp

# 4. 检查端口配置
kubectl get svc myapp-service -o yaml
kubectl get deployment myapp -o yaml | grep -A 5 "ports:"
```

#### 常见错误

**错误 1**: 标签大小写不匹配
```yaml
# Deployment
metadata:
  labels:
    App: myapp    # 大写

# Service
selector:
  app: myapp    # 小写
```

**错误 2**: 端口名称不匹配
```yaml
# Service
targetPort: http  # 使用端口名称

# Pod
ports:
- containerPort: 80  # 没有定义 name
```

#### 解决方案

```bash
# 方案 1: 修复标签（重新创建）
kubectl delete deployment myapp
kubectl delete service myapp-service

# 使用正确的配置重新创建（确保 selector 和 labels 完全一致）

# 方案 2: 修复 Service（直接修改端口）
kubectl patch svc myapp-service -p '{"spec":{"ports":[{"name":"http","port":80,"targetPort":80}]}}'

# 方案 3: 给 Pod 添加标签
kubectl label pod <pod-name> app=myapp --overwrite
```

### 4.2 Pod 无法调度（污点问题）

#### 症状

```bash
kubectl get pods
# NAME                     READY   STATUS    RESTARTS   AGE
# ingress-nginx-xxx        0/1     Pending   0          5m

kubectl describe pod ingress-nginx-xxx
# Warning  FailedScheduling  0/2 nodes are available: 2 node(s) had untolerated taint {devbox.sealos.io/node: }.
```

#### 原因

节点有污点，Pod 没有对应的容忍。

#### 查看污点

```bash
kubectl describe nodes | grep Taints

# 输出示例:
# Taints: devbox.sealos.io/node:NoSchedule
```

#### 解决方案

```bash
# 方法 1: 使用 patch 添加容忍
kubectl patch deployment <deployment-name> --type=json -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/tolerations",
    "value": [
      {
        "key": "devbox.sealos.io/node",
        "operator": "Exists",
        "effect": "NoSchedule"
      }
    ]
  }
]'

# 方法 2: 编辑 YAML 添加
kubectl edit deployment <deployment-name>

# 添加:
# spec:
#   template:
#     spec:
#       tolerations:
#       - key: devbox.sealos.io/node
#         operator: Exists
#         effect: NoSchedule

# 方法 3: 容忍所有污点（不推荐生产环境）
kubectl patch deployment <deployment-name> -p '{"spec":{"template":{"spec":{"tolerations":[{"operator":"Exists"}]}}}'
```

### 4.3 Secret 未找到

#### 症状

```bash
kubectl get pods -n ingress-nginx
# NAME                                        READY   STATUS              RESTARTS   AGE
# ingress-nginx-controller-xxx                0/1     ContainerCreating   0          5m

kubectl describe pod ingress-nginx-controller-xxx
# Warning  FailedMount  secret "ingress-nginx-admission" not found
```

#### 原因链

```
1. admission-create Job 无法调度（没有容忍污点）
   ↓
2. Job 无法运行，Secret 未创建
   ↓
3. Controller Pod 无法挂载 Secret
   ↓
4. Pod 卡在 ContainerCreating 状态
```

#### 解决方案

```bash
# 1. 给 Job 添加容忍
kubectl patch job ingress-nginx-admission-create -n ingress-nginx --type=json -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/tolerations",
    "value": [{
      "key": "devbox.sealos.io/node",
      "operator": "Exists",
      "effect": "NoSchedule"
    }]
  }
]'

kubectl patch job ingress-nginx-admission-patch -n ingress-nginx --type=json -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/tolerations",
    "value": [{
      "key": "devbox.sealos.io/node",
      "operator": "Exists",
      "effect": "NoSchedule"
    }]
  }
]'

# 2. 等待 Job 完成
kubectl wait --for=condition=complete job/ingress-nginx-admission-create -n ingress-nginx --timeout=60s
kubectl wait --for=condition=complete job/ingress-nginx-admission-patch -n ingress-nginx --timeout=60s

# 3. 验证 Secret 创建
kubectl get secret -n ingress-nginx ingress-nginx-admission

# 4. Pod 会自动重启并正常运行
kubectl get pods -n ingress-nginx
```

### 4.4 Ingress ADDRESS 为空

#### 症状

```bash
kubectl get ingress myapp-ingress
# NAME            CLASS   HOSTS         ADDRESS   PORTS   AGE
# myapp-ingress   nginx   myapp.local             80      10m
#                                         ↑
#                                   ADDRESS 字段为空
```

#### 原因

**这是正常的！** 在裸机/本地环境中，如果不使用 MetalLB 或云厂商 LoadBalancer，ADDRESS 字段会是空的。

#### ADDRESS 字段填充条件

| Service 类型 | ADDRESS 字段 | 环境 |
|-------------|-------------|------|
| **LoadBalancer**（云） | 公网 IP | AWS/阿里云/Azure |
| **LoadBalancer**（MetalLB） | 分配的 IP | 本地/裸机 |
| **NodePort** | 节点 IP 或空 | 本地/裸机 |
| **ClusterIP** | 空 | 本地/裸机 |

#### 解决方案

**方案 1: 使用 MetalLB（推荐）**

见 3.6 节的配置步骤。

**方案 2: 忽略 ADDRESS 字段，直接使用节点 IP**

```bash
# 获取节点 IP
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')

# 获取 NodePort（如果是 NodePort 类型）
NODE_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[0].nodePort}')

# 配置 DNS
echo "$NODE_IP myapp.local" | sudo tee -a /etc/hosts

# 访问（带端口）
curl http://myapp.local:$NODE_PORT/
```

**方案 3: 手动设置 ADDRESS（不推荐，仅显示）**

```bash
kubectl edit ingress myapp-ingress

# 添加:
status:
  loadBalancer:
    ingress:
    - ip: 192.168.12.214
```

⚠️ **注意**: Ingress Controller 会定期覆盖这个值，不推荐这样做。

### 4.5 集群外无法访问

#### 症状

```bash
# 集群内访问正常
kubectl run test --image=busybox:1.28 --rm -it --restart=Never -- \
  wget -O- -q http://myapp-service/
# ✓ 成功

# 集群外访问失败
# 在另一台机器上配置了 /etc/hosts
echo "192.168.12.200 myapp.local" >> /etc/hosts
curl http://myapp.local/
# ✗ 失败: Connection timed out
```

#### 原因

**MetalLB L2 模式的限制**: ARP 广播无法跨越网段。

#### 诊断

```bash
# 1. 检查是否在同一网段
# 在 K8s 节点上
ip addr show | grep "192.168.12"

# 在你的电脑上
ip addr show | grep "192.168"

# 如果前三位不同（如 192.168.12.x vs 192.168.20.x），就不在同一网段

# 2. Ping 测试
ping 192.168.12.200  # LoadBalancer IP

# 3. 检查节点是否绑定了 IP
# 在 K8s 节点上执行
ip addr show | grep 192.168.12.200

# 预期输出:
# inet 192.168.12.200/32 scope global ens192
```

#### 解决方案

**情况 1: 同一网段但仍无法访问**

```bash
# 检查防火墙
sudo iptables -L -n | grep -E "80|443"

# 开放端口
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# 或
sudo iptables -I INPUT -p tcp --dport 80 -j ACCEPT
sudo iptables -I INPUT -p tcp --dport 443 -j ACCEPT

# 重启 MetalLB Speaker
kubectl delete pod -n metallb-system -l app.kubernetes.io/component=speaker
```

**情况 2: 不同网段**

**方案 A: 改用 NodePort**（最简单）

```bash
# 修改 Service 为 NodePort
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"NodePort"}}'

# 获取节点 IP 和 NodePort
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
NODE_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[0].nodePort}')

# 配置 DNS（使用节点 IP）
echo "$NODE_IP myapp.local" | sudo tee -a /etc/hosts

# 访问（带端口）
http://myapp.local:$NODE_PORT/
```

**方案 B: 使用反向代理**

在 K8s 节点上配置 Nginx：

```bash
# 安装 Nginx
sudo apt update && sudo apt install nginx -y

# 配置反向代理
cat <<EOF | sudo tee /etc/nginx/sites-available/k8s-ingress
upstream k8s_ingress {
    server 192.168.12.200:80;  # LoadBalancer IP
}

server {
    listen 80;
    server_name myapp.local;

    location / {
        proxy_pass http://k8s_ingress;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF

# 启用配置
sudo ln -s /etc/nginx/sites-available/k8s-ingress /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx

# 配置 DNS
echo "192.168.12.214 myapp.local" | sudo tee -a /etc/hosts

# 访问
curl http://myapp.local/
```

---

## 第五部分：生产环境配置

### 5.1 方案对比

| 方案 | 成本/月 | 复杂度 | 高可用 | 适用场景 |
|------|---------|--------|--------|----------|
| **云厂商 LoadBalancer** | $15-30 | 低 | 高 | 生产环境（推荐） |
| **NodePort + 反向代理** | $5-10 + 带宽 | 中 | 低 | 小型应用 |
| **MetalLB + BGP** | 服务器成本 | 高 | 高 | 企业/IDC |
| **CDN + 源站** | 免费-$50 | 中 | 高 | 全球加速 |

### 5.2 云厂商 LoadBalancer

#### 架构图

```
用户
  │ DNS: www.example.com
  ▼
云厂商负载均衡器
  │ 公网 IP: 1.2.3.4
  │ - DDoS 防护
  │ - SSL 卸载
  │ - 全球加速
  ▼
Kubernetes 集群
  │
  ├─→ Ingress Controller
  ├─→ Service
  └─→ Pod
```

#### 配置示例（AWS EKS）

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  namespace: ingress-nginx
  annotations:
    # AWS NLB
    service.beta.kubernetes.io/aws-load-balancer-type: nlb
    # AWS ALB
    # service.beta.kubernetes.io/aws-load-balancer-type: alb
    # 跨可用区
    service.beta.kubernetes.io/aws-load-balancer-scheme: internet-facing
spec:
  type: LoadBalancer
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - port: 80
    targetPort: 80
  - port: 443
    targetPort: 443
```

```bash
# AWS 自动分配公网 IP
kubectl get svc ingress-nginx

# 输出:
# NAME            TYPE           EXTERNAL-IP   PORT(S)
# ingress-nginx   LoadBalancer   1.2.3.4       80:xxxxx/TCP
#                                     ↑
#                          AWS 自动分配的公网 IP
```

#### 配置示例（阿里云 ACK）

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  namespace: ingress-nginx
  annotations:
    # 阿里云 SLB
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-spec: slb.s3.small
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-charge-type: paybytraffic
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-protocol-port: "http:80,https:443"
spec:
  type: LoadBalancer
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - port: 80
    targetPort: 80
  - port: 443
    targetPort: 443
```

### 5.3 完整配置示例

#### 步骤 1: 购买资源

```bash
# 1. 购买域名
# 例如: example.com
# 成本: 约 ¥50-100/年

# 2. 购买云服务器
# 例如: 阿里云 ECS
# 配置: 2核4G，5Mbps 带宽
# 成本: 约 ¥100-200/月

# 3. 部署 Kubernetes
# 使用 sealos/kubeadm/rancher
# 或直接使用托管 K8s（EKS/ACK）
```

#### 步骤 2: 部署应用

```bash
# 1. 创建 Namespace
kubectl create namespace production

# 2. 部署应用
kubectl create deployment myapp \
  --image=nginx:1.25 \
  --replicas=3 \
  -n production

# 3. 创建 Service
kubectl expose deployment myapp \
  --port=80 \
  --target-port=80 \
  -n production

# 4. 验证
kubectl get pods,svc -n production
```

#### 步骤 3: 安装 Ingress Controller

```bash
# 安装
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.9.4/deploy/static/provider/cloud/deploy.yaml

# 等待就绪
kubectl wait --for=condition=available \
  deployment/ingress-nginx-controller \
  -n ingress-nginx \
  --timeout=300s
```

#### 步骤 4: 创建 LoadBalancer

```bash
# 修改 Service 类型
kubectl patch svc ingress-nginx-controller \
  -n ingress-nginx \
  -p '{"spec":{"type":"LoadBalancer"}}'

# 获取公网 IP
kubectl get svc ingress-nginx-controller -n ingress-nginx

# 输出:
# NAME            TYPE           EXTERNAL-IP   PORT(S)
# ingress-nginx   LoadBalancer   47.96.123.45  80:xxxxx/TCP
#                                     ↑
#                         云厂商分配的公网 IP
```

#### 步骤 5: 配置 DNS

```bash
# 在域名服务商控制台（阿里云DNS/Cloudflare）

# 添加记录:
@ A 47.96.123.45         # example.com → 47.96.123.45
www A 47.96.123.45       # www.example.com → 47.96.123.45
api A 47.96.123.45       # api.example.com → 47.96.123.45

# 等待 DNS 生效（5分钟 - 48小时）
dig www.example.com
```

#### 步骤 6: 创建 Ingress

```bash
kubectl create ingress myapp-ingress \
  -n production \
  --rule="www.example.com/*=myapp:80"
```

#### 步骤 7: 配置 SSL（HTTPS）

```bash
# 1. 安装 cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# 2. 创建 ClusterIssuer
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - http01:
        ingress:
          class: nginx
EOF

# 3. 在 Ingress 中启用 TLS
kubectl annotate ingress myapp-ingress \
  -n production \
  cert-manager.io/cluster-issuer=letsencrypt-prod

# 4. 等待证书签发
kubectl get certificate -n production
```

#### 步骤 8: 验证

```bash
# 测试 HTTPS 访问
curl https://www.example.com/

# 浏览器访问
https://www.example.com/

# 检查证书
curl -vI https://www.example.com/ 2>&1 | grep "subject issuer"
```

---

## 第六部分：故障排查

### 6.1 常用诊断命令

```bash
# 查看所有资源状态
kubectl get all
kubectl get ingress
kubectl get endpoints

# 查看事件
kubectl get events --sort-by='.lastTimestamp'

# 查看日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx --tail=50 -f

# 进入 Pod 调试
kubectl exec -it <pod-name> -- /bin/sh

# 端口转发测试
kubectl port-forward svc/<service-name> 8080:80

# DNS 测试
kubectl run test --image=busybox:1.28 --rm -it --restart=Never -- nslookup myapp.local
```

### 6.2 问题定位流程

```
问题: 无法访问应用
    │
    ▼
1. 检查 Pod 是否运行
kubectl get pods -l app=myapp
    │
    ├─→ Pod 不正常 → 查看 Pod 日志、describe pod
    │
    ▼ Pod 正常
2. 检查 Endpoints
kubectl get endpoints myapp-service
    │
    ├─→ Endpoints 为空 → 检查 labels 和 selector
    │
    ▼ Endpoints 正常
3. 检查 Service 连通性
kubectl run test --rm -it -- wget -O- http://myapp-service/
    │
    ├─→ 无法访问 → 检查端口配置
    │
    ▼ Service 正常
4. 检查 Ingress
kubectl get ingress
kubectl describe ingress myapp-ingress
    │
    ├─→ ADDRESS 为空 → 需要配置 LoadBalancer/NodePort
    │
    ▼ Ingress 正常
5. 检查 Ingress Controller
kubectl get pods -n ingress-nginx
    │
    ├─→ Controller 不正常 → 查看日志
    │
    ▼ Controller 正常
6. 检查 DNS
ping myapp.local
nslookup myapp.local
    │
    ├─→ DNS 无法解析 → 检查 /etc/hosts
    │
    ▼ DNS 正常
7. 检查防火墙
sudo iptables -L -n
ping <LoadBalancer IP>
```

### 6.3 日志查看技巧

#### Ingress Controller 日志

```bash
# 查看最近 100 行日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx --tail=100

# 实时查看日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx -f

# 查看特定请求的日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx | grep "myapp.local"
```

#### Pod 日志

```bash
# 查看当前容器日志
kubectl logs <pod-name>

# 查看之前的容器日志（如果重启过）
kubectl logs <pod-name> --previous

# 查看所有容器的日志（多容器 Pod）
kubectl logs <pod-name> --all-containers
```

#### 事件日志

```bash
# 查看所有事件
kubectl get events

# 只查看错误事件
kubectl get events --field-selector type=Warning

# 查看特定 Pod 的事件
kubectl get events --field-selector involvedObject.name=<pod-name>

# 按时间排序
kubectl get events --sort-by='.lastTimestamp'
```

---

## 附录

### A. 完整配置文件

#### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  labels:
    app: myapp
spec:
  replicas: 2
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      # 如果节点有污点，添加容忍
      tolerations:
      - key: devbox.sealos.io/node
        operator: Exists
        effect: NoSchedule
      containers:
      - name: myapp
        image: registry.cn-hangzhou.aliyuncs.com/google_containers/nginx:1.25
        ports:
        - containerPort: 80
          name: http
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 200m
            memory: 256Mi
        livenessProbe:
          httpGet:
            path: /
            port: http
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /
            port: http
          initialDelaySeconds: 10
          periodSeconds: 5
```

#### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
spec:
  selector:
    app: myapp
  ports:
  - name: http
    port: 80
    targetPort: 80  # 直接使用端口号
  type: ClusterIP
```

#### Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  ingressClassName: nginx
  rules:
  - host: myapp.local
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: myapp-service
            port:
              number: 80
```

### B. 快速验证脚本

```bash
#!/bin/bash

echo "=== Ingress 完整验证 ==="

# 1. 检查 Pod
echo -e "\n1. Pod 状态"
kubectl get pods -l app=myapp

# 2. 检查 Service
echo -e "\n2. Service 状态"
kubectl get svc myapp-service
kubectl get endpoints myapp-service

# 3. 检查 Ingress Controller
echo -e "\n3. Ingress Controller"
kubectl get pods -n ingress-nginx

# 4. 检查 Ingress
echo -e "\n4. Ingress 状态"
kubectl get ingress myapp-ingress

# 5. 测试访问
echo -e "\n5. 测试访问"
kubectl run test --image=busybox:1.28 --rm -it --restart=Never -- \
  wget -O- -q http://myapp-service/ 2>/dev/null | head -5

echo -e "\n=== 验证完成 ==="
```

### C. 问题总结

| 问题 | 症状 | 原因 | 解决 |
|------|------|------|------|
| **Endpoints 为空** | `kubectl get endpoints` 显示 `<none>` | labels/selector 不匹配或端口配置错误 | 确保标签一致，端口名称匹配 |
| **Pod Pending** | Pod 一直处于 Pending 状态 | 节点有污点，Pod 没有容忍 | 添加 tolerations |
| **Secret 未找到** | Controller 无法挂载 Secret | init Job 无法调度 | 给 Job 也添加容忍 |
| **ADDRESS 为空** | Ingress 的 ADDRESS 字段为空 | 没有配置 LoadBalancer | 使用 MetalLB 或 NodePort |
| **集群外无法访问** | 同网段可访问，不同网段无法 | MetalLB L2 模式限制 | 使用 NodePort 或反向代理 |

### D. 参考资料

- [Kubernetes 官方文档](https://kubernetes.io/docs/)
- [NGINX Ingress Controller](https://kubernetes.github.io/ingress-nginx/)
- [MetalLB 文档](https://metallb.universe.tf/)
- [Sealos 官方文档](https://sealos.io/docs/)

---

**文档版本**: v1.0
**最后更新**: 2025-01-15
**作者**: cunzili

**学习路径建议**:
1. 本地环境学习（MetalLB）✅ 你在这里
2. 云 VM 实践（NodePort + 反向代理）
3. 云托管 K8s（EKS/ACK）→ 生产环境
4. 高级配置（CDN/WAF/监控）

祝你学习顺利！🚀
