# Kubernetes Ingress 与域名/IP 工作原理详解

> 深入理解用户如何通过域名访问到 Kubernetes 集群中的服务

---

## 目录

- [1. 基础概念](#1-基础概念)
- [2. 完整的请求流程](#2-完整的请求流程)
- [3. DNS 解析详解](#3-dns-解析详解)
- [4. Ingress Controller 工作原理](#4-ingress-controller-工作原理)
- [5. Service 负载均衡](#5-service-负载均衡)
- [6. 实际配置示例](#6-实际配置示例)
- [7. 常见问题](#7-常见问题)

---

## 1. 基础概念

### 1.1 核心组件关系

```
用户域名 (example.com)
        │
        ▼ DNS 解析
    Ingress IP
        │
        ▼ HTTP 请求
┌─────────────────────────────────┐
│  Ingress Controller             │
│  (Nginx/Traefik/HAProxy/APISIX) │
│  - 监听 Ingress 资源变化         │
│  - 动态生成配置                  │
│  - 反向代理流量                  │
└─────────────────────────────────┘
        │
        ▼ 根据 Ingress 规则路由
┌─────────────────────────────────┐
│  Kubernetes Service             │
│  - ClusterIP (虚拟 IP)          │
│  - 服务发现                     │
│  - 负载均衡                     │
└─────────────────────────────────┘
        │
        ▼ 通过 Endpoints 分发
┌─────────────────────────────────┐
│  Pods (实际应用)                │
│  - Pod1: 10.244.1.5:8080       │
│  - Pod2: 10.244.1.6:8080       │
│  - Pod3: 10.244.2.7:8080       │
└─────────────────────────────────┘
```

### 1.2 各层职责

| 层级 | 组件 | 职责 | 示例 |
|------|------|------|------|
| DNS 层 | DNS 服务器 | 域名 → IP 解析 | `example.com` → `1.2.3.4` |
| 接入层 | Ingress Controller | 接收外部流量，根据 Host/Path 路由 | Nginx, Traefik |
| 服务层 | Service | 服务发现，负载均衡 | ClusterIP Service |
| 应用层 | Pod | 运行实际应用容器 | nginx, tomcat |

---

## 2. 完整的请求流程

### 2.1 流程图

```
用户浏览器
    │
    │ 1. 输入 URL: http://app.example.com
    ▼
┌──────────────────────────────────────────┐
│ DNS 解析                                  │
│ app.example.com → Ingress Controller IP  │
│ (1.2.3.4)                                │
└──────────────────────────────────────────┘
    │
    │ 2. 发送 HTTP 请求
    ▼
┌──────────────────────────────────────────┐
│ Ingress Controller                       │
│ (Nginx/Traefik 等)                      │
│                                          │
│ 接收请求: Host: app.example.com          │
│ Path: /api/users                         │
└──────────────────────────────────────────┘
    │
    │ 3. 匹配 Ingress 规则
    ▼
┌──────────────────────────────────────────┐
│ Ingress 资源配置                         │
│                                          │
│ apiVersion: networking.k8s.io/v1         │
│ kind: Ingress                           │
│ metadata:                               │
│   name: myapp-ingress                   │
│ spec:                                   │
│   rules:                                │
│   - host: app.example.com               │
│     http:                               │
│       paths:                            │
│       - path: /api                      │
│         pathType: Prefix                │
│         backend:                        │
│           service:                      │
│             name: myapp-service         │
│             port:                       │
│               number: 8080              │
└──────────────────────────────────────────┘
    │
    │ 4. 路由到 Service
    ▼
┌──────────────────────────────────────────┐
│ Service: myapp-service                   │
│ Type: ClusterIP                          │
│ ClusterIP: 10.96.100.50                  │
│ Port: 8080 → TargetPort: 8080           │
└──────────────────────────────────────────┘
    │
    │ 5. 通过 Endpoints 分发
    ▼
┌──────────────────────────────────────────┐
│ Endpoints: myapp-service                 │
│ - 10.244.1.5:8080                       │
│ - 10.244.1.6:8080                       │
│ - 10.244.2.7:8080                       │
└──────────────────────────────────────────┘
    │
    │ 6. 负载均衡到 Pod
    ▼
┌──────────────────────────────────────────┐
│ Pod 1 (10.244.1.5:8080)                 │
│ 处理请求，返回响应                       │
└──────────────────────────────────────────┘
    │
    │ 7. 响应原路返回
    ▼
用户看到页面
```

### 2.2 详细步骤说明

#### 步骤 1: DNS 解析

**用户操作**: 在浏览器输入 `http://app.example.com`

**DNS 查询过程**:
```bash
# 1. 检查浏览器缓存
# 2. 检查操作系统缓存
# 3. 查询本地 DNS 服务器
# 4. 查询根服务器
# 5. 查询 .com 域服务器
# 6. 查询 example.com 的权威 DNS
# 7. 返回 A 记录: app.example.com → 1.2.3.4
```

**DNS 记录类型**:
```
# A 记录（直接指向 IP）
app.example.com A 1.2.3.4

# CNAME 记录（指向另一个域名）
app.example.com CNAME ingress.sealos.io
ingress.sealos.io A 1.2.3.4

# 多 A 记录（负载均衡）
app.example.com A 1.2.3.4
app.example.com A 1.2.3.5
app.example.com A 1.2.3.6
```

#### 步骤 2: 建立 TCP 连接

```bash
# 用户浏览器与 Ingress Controller 建立 TCP 连接
TCP 三次握手: SYN → SYN-ACK → ACK
目标: 1.2.3.4:443 (HTTPS) 或 1.2.3.4:80 (HTTP)
```

#### 步骤 3: 发送 HTTP 请求

```http
GET /api/users HTTP/1.1
Host: app.example.com
User-Agent: Mozilla/5.0
Accept: application/json
```

#### 步骤 4: Ingress Controller 处理

**Nginx Ingress Controller 示例**:

```nginx
# Ingress Controller 自动生成的 Nginx 配置
server {
    server_name app.example.com;

    location /api {
        # 转发到 Service
        proxy_pass http://myapp-service.default.svc.cluster.local:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

**匹配逻辑**:
1. **Host 匹配**: 查找 `host: app.example.com` 的 Ingress 规则
2. **Path 匹配**: 在匹配的 Host 下，查找路径 `/api`
3. **Backend 获取**: 找到对应的 Service 和端口

#### 步骤 5: Service 转发

```yaml
# Service 资源
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
spec:
  clusterIP: 10.96.100.50  # 虚拟 IP
  ports:
  - port: 8080             # Service 端口
    targetPort: 8080       # Pod 端口
  selector:
    app: myapp             # Pod 标签选择器
```

**Service 工作原理**:
- `ClusterIP` 是一个虚拟 IP，不存在于任何网络接口
- kube-proxy 监听 Service 变化，配置 iptables/IPVS 规则
- 当访问 `10.96.100.50:8080` 时，自动负载均衡到后端 Pod

#### 步骤 6: Endpoint 分发

```yaml
# Endpoints 自动创建（由 Controller 管理）
apiVersion: v1
kind: Endpoints
metadata:
  name: myapp-service
subsets:
- addresses:
  - ip: 10.244.1.5  # Pod 1 IP
    nodeName: node-1
    targetRef:
      kind: Pod
      name: myapp-pod-1
  - ip: 10.244.1.6  # Pod 2 IP
    nodeName: node-1
    targetRef:
      kind: Pod
      name: myapp-pod-2
  - ip: 10.244.2.7  # Pod 3 IP
    nodeName: node-2
    targetRef:
      kind: Pod
      name: myapp-pod-3
  ports:
  - port: 8080
```

**Endpoint 控制器**:
- 监听 Pod 变化（创建/删除/IP 变更）
- 根据 Service 的 selector 匹配 Pod
- 自动更新 Endpoints 列表

#### 步骤 7: Pod 处理请求

```bash
# Pod 收到请求
10.244.1.5:8080 - GET /api/users HTTP/1.1
Host: app.example.com
X-Real-IP: 1.2.3.4
X-Forwarded-For: 1.2.3.4
X-Forwarded-Proto: http

# Pod 处理请求
# 应用逻辑处理 → 数据库查询 → 返回结果
```

---

## 3. DNS 解析详解

### 3.1 DNS 配置方式

#### 方式 1: A 记录（直接指向）

```
# DNS 配置
app.example.com A 1.2.3.4

# 适用场景
- Ingress Controller 使用固定 IP（LoadBalancer 类型）
- 云厂商提供的负载均衡器 IP
```

**示例**:
```bash
# 阿里云 SLB
app.example.com A 47.96.123.45

# AWS ELB
app.example.com A elb123.us-west-2.elb.amazonaws.com
```

#### 方式 2: CNAME 记录（域名别名）

```
# DNS 配置
app.example.com CNAME ingress.sealos.io
ingress.sealos.io A 1.2.3.4

# 适用场景
- 使用云域名（如 AWS ELB 域名）
- 使用 CDN 加速
- 多层 DNS 委托
```

**示例**:
```bash
# AWS ELB（推荐使用 CNAME）
app.example.com CNAME my-elb-123.us-west-2.elb.amazonaws.com

# Sealos 场景
app.example.com CNAME myapp.sealos.io
myapp.sealos.io A 1.2.3.4
```

#### 方式 3: 泛域名（通配符）

```
# DNS 配置
*.example.com A 1.2.3.4

# 适用场景
- 支持动态子域名
- 多租户场景
- 无需为每个子域名单独配置
```

**示例**:
```bash
# 泛域名解析
user1.example.com → 1.2.3.4
user2.example.com → 1.2.3.4
app.example.com → 1.2.3.4
# 所有 *.example.com 都指向同一个 Ingress Controller
```

### 3.2 DNS 查询流程

```
用户请求: http://app.example.com
    │
    ▼
┌────────────────────────────────────┐
│ 1. 检查浏览器缓存                  │
│    - 有记录？直接返回               │
│    - 无记录？继续                  │
└────────────────────────────────────┘
    │
    ▼
┌────────────────────────────────────┐
│ 2. 检查操作系统缓存                │
│    - /etc/hosts                    │
│    - DNS Cache (Windows/macOS)     │
└────────────────────────────────────┘
    │
    ▼
┌────────────────────────────────────┐
│ 3. 查询本地 DNS 服务器             │
│    - 通常是路由器或 ISP 提供        │
└────────────────────────────────────┘
    │
    ▼
┌────────────────────────────────────┐
│ 4. 递归查询过程                    │
│    Root (.)                        │
│      │                             │
│      ▼                             │
│    TLD Server (.com)               │
│      │                             │
│      ▼                             │
│    Authoritative NS                │
│    (example.com 的权威 DNS)        │
└────────────────────────────────────┘
    │
    ▼
返回 IP: 1.2.3.4
```

### 3.3 DNS 配置示例

#### 场景 1: 传统 DNS 服务商

```
# 阿里云 DNS 配置
主机记录    记录类型    记录值
@           A           1.2.3.4
*           A           1.2.3.4
app         A           1.2.3.4
api         CNAME       app.example.com
```

#### 场景 2: 云厂商负载均衡

```bash
# AWS Route 53
app.example.com A
Alias: Yes
Alias Target: dualstack.my-elb-123.us-west-2.elb.amazonaws.com

# 阿里云 DNS
app.example.com CNAME
my-elb-123.cn-hangzhou.elb.aliyuncs.com
```

#### 场景 3: Sealos 平台

```bash
# 用户的自定义域名
app.userdomain.com CNAME myapp.sealos.io

# Sealos 提供的子域名
myapp.sealos.io A 1.2.3.4

# DNS 查询链
app.userdomain.com → myapp.sealos.io → 1.2.3.4
```

---

## 4. Ingress Controller 工作原理

### 4.1 Ingress Controller 架构

```
┌──────────────────────────────────────────────────────────┐
│ Ingress Controller Pod                                   │
│                                                          │
│ ┌────────────────────────────────────────────────────┐ │
│ │ Watch Loop (监听 Kubernetes API)                    │ │
│ │ - Ingress 变化                                      │ │
│ │ - Service 变化                                      │ │
│ │ - Secret 变化（TLS 证书）                           │ │
│ │ - Endpoint 变化                                     │ │
│ └────────────────────────────────────────────────────┘ │
│                          │                              │
│                          ▼                              │
│ ┌────────────────────────────────────────────────────┐ │
│ │ Config Generator                                    │ │
│ │ - 根据 Ingress 资源生成配置                         │ │
│ │ - Nginx: nginx.conf                                 │ │
│ │ - Traefik: traefik.yml                              │ │
│ │ - APISIX: etcd 路由配置                             │ │
│ └────────────────────────────────────────────────────┘ │
│                          │                              │
│                          ▼                              │
│ ┌────────────────────────────────────────────────────┐ │
│ │ Reload/Apply                                        │ │
│ │ - Nginx: nginx -s reload                           │ │
│ │ - Traefik: 动态配置，无需重启                      │ │
│ │ - APISIX: HTTP API 动态更新                        │ │
│ └────────────────────────────────────────────────────┘ │
│                          │                              │
│                          ▼                              │
│ ┌────────────────────────────────────────────────────┐ │
│ │ Proxy Engine (反向代理)                            │ │
│ │ - Nginx                                            │ │
│ │ - Envoy                                            │ │
│ │ - HAProxy                                          │ │
│ └────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
```

### 4.2 主流 Ingress Controller 对比

| 特性 | Nginx Ingress | Traefik | APISIX | HAProxy Ingress |
|------|--------------|---------|--------|-----------------|
| 配置方式 | 模板生成 | 动态配置 | 动态配置 | 模板生成 |
| 性能 | 高 | 中 | 极高 | 高 |
| 功能丰富度 | 高 | 高 | 极高 | 中 |
| 学习曲线 | 中 | 低 | 中 | 高 |
| 热更新 | 支持 | 原生支持 | 原生支持 | 支持 |
| WebSocket | 支持 | 支持 | 支持 | 支持 |
| gRPC | 支持 | 支持 | 支持 | 支持 |
| 监控集成 | Prometheus | Prometheus | Prometheus | Prometheus |

### 4.3 Nginx Ingress Controller 配置生成

#### 输入: Ingress 资源

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  namespace: default
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  rules:
  - host: app.example.com
    http:
      paths:
      - path: /api
        pathType: Prefix
        backend:
          service:
            name: myapp-service
            port:
              number: 8080
```

#### 输出: Nginx 配置

```nginx
# Ingress Controller 自动生成
upstream myapp-service-default-8080 {
    # 通过 Endpoints 动态获取
    server 10.244.1.5:8080 max_fails=0 fail_timeout=0;
    server 10.244.1.6:8080 max_fails=0 fail_timeout=0;
    server 10.244.2.7:8080 max_fails=0 fail_timeout=0;

    # 负载均衡配置
    keepalive 32;
    keepalive_requests 100;
    keepalive_timeout 60s;
}

server {
    server_name app.example.com;
    listen 80;

    # 访问日志
    access_log /var/log/nginx/app.example.com-access.log;

    location /api {
        # 代理到 Service
        proxy_pass http://myapp-service-default-8080;

        # 请求头设置
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 超时配置
        proxy_connect_timeout 5s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;

        # 注解配置的重写规则
        rewrite ^/api/(.*) /$1 break;
    }
}
```

### 4.4 Ingress 规则匹配

#### 匹配优先级

```
1. Host 精确匹配 (exact)
   app.example.com

2. Host 通配符匹配 (wildcard)
   *.example.com

3. Host 正则匹配 (regex)
   ~^www\d+.example.com$

4. Path 精确匹配 (Exact)
   /api/v1/users

5. Path 前缀匹配 (Prefix)
   /api

6. ImplementationSpecific（由具体实现决定）
```

#### 示例: 多规则匹配

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: multi-rule-ingress
spec:
  rules:
  # 规则 1: 前端应用
  - host: app.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: frontend-service
            port:
              number: 80

  # 规则 2: API 服务
  - host: api.example.com
    http:
      paths:
      - path: /v1
        pathType: Prefix
        backend:
          service:
            name: api-v1-service
            port:
              number: 8080
      - path: /v2
        pathType: Prefix
        backend:
          service:
            name: api-v2-service
            port:
              number: 8080

  # 规则 3: 通配符域名
  - host: "*.tenant.example.com"
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: tenant-service
            port:
              number: 80
```

**请求路由示例**:
```
http://app.example.com/           → frontend-service
http://app.example.com/about      → frontend-service
http://api.example.com/v1/users   → api-v1-service
http://api.example.com/v2/posts   → api-v2-service
http://user1.tenant.example.com/  → tenant-service
```

---

## 5. Service 负载均衡

### 5.1 Service 类型

| 类型 | 场景 | 访问方式 | 示例 |
|------|------|----------|------|
| ClusterIP | 集群内部访问 | 虚拟 IP | 数据库连接 |
| NodePort | 外部访问（节点 IP） | 节点 IP + 端口 | 测试环境 |
| LoadBalancer | 云厂商负载均衡 | 公网 IP | 生产环境 |
| ExternalName | 外部服务别名 | CNAME | 第三方 API |

### 5.2 ClusterIP 工作原理

```bash
# Service 创建
kubectl expose deployment myapp --port=8080 --target-port=80

# kube-proxy 监听 Service 变化
# 自动配置 iptables/IPVS 规则

# 查看 iptables 规则
iptables -t nat -L KUBE-SERVICES | grep myapp

# 示例输出
# KUBE-SVC-XXX  tcp  --  anywhere  10.96.100.50  tcp dpt:8080
```

#### iptables 模式

```
请求: 10.96.100.50:8080
    │
    ▼ iptables 规则匹配
┌────────────────────────────────────┐
│ KUBE-SERVICES 链                  │
│ 10.96.100.50:8080 →               │
│   KUBE-SVC-XXX                    │
└────────────────────────────────────┘
    │
    ▼
┌────────────────────────────────────┐
│ KUBE-SVC-XXX 链（负载均衡规则）   │
│                                    │
│ 0.0.0.0/0 → KUBE-SEP-YYY  (33%)   │
│ 0.0.0.0/0 → KUBE-SEP-ZZZ  (33%)   │
│ 0.0.0.0/0 → KUBE-SEP-WWW  (33%)   │
└────────────────────────────────────┘
    │
    ▼ 随机选择
┌────────────────────────────────────┐
│ KUBE-SEP-YYY (Endpoint 1)         │
│ DNAT 到 10.244.1.5:8080           │
└────────────────────────────────────┘
    │
    ▼
Pod: 10.244.1.5:8080
```

#### IPVS 模式（性能更好）

```bash
# 启用 IPVS
kube-proxy --proxy-mode=ipvs

# 查看 IPVS 规则
ipvsadm -Ln

# 示例输出
# TCP  10.96.100.50:8080 rr
#   -> 10.244.1.5:8080          Masq    1      0          0
#   -> 10.244.1.6:8080          Masq    1      0          0
#   -> 10.244.2.7:8080          Masq    1      0          0
```

**IPVS 优势**:
- 性能更好（内核级负载均衡）
- 支持更多负载均衡算法（rr, lc, dh, sh 等）
- 连接保持更稳定

### 5.3 Endpoint 工作流程

```
┌────────────────────────────────────────────────────────┐
│ 1. 用户创建 Deployment                                  │
│    kubectl create deployment myapp --image=nginx       │
└────────────────────────────────────────────────────────┘
    │
    ▼
┌────────────────────────────────────────────────────────┐
│ 2. Deployment 创建 3 个 Pod 副本                       │
│    - myapp-pod-1 (10.244.1.5)                         │
│    - myapp-pod-2 (10.244.1.6)                         │
│    - myapp-pod-3 (10.244.2.7)                         │
└────────────────────────────────────────────────────────┘
    │
    ▼
┌────────────────────────────────────────────────────────┐
│ 3. 用户创建 Service                                    │
│    selector: app=myapp                                │
│    port: 8080                                         │
└────────────────────────────────────────────────────────┘
    │
    ▼
┌────────────────────────────────────────────────────────┐
│ 4. Endpoint Controller 自动工作                       │
│    - 监听 Pod 变化（标签匹配 selector）               │
│    - 创建/更新 Endpoints 资源                         │
└────────────────────────────────────────────────────────┘
    │
    ▼
┌────────────────────────────────────────────────────────┐
│ 5. Endpoints 自动创建                                 │
│    - 10.244.1.5:8080                                  │
│    - 10.244.1.6:8080                                  │
│    - 10.244.2.7:8080                                  │
└────────────────────────────────────────────────────────┘
    │
    ▼
┌────────────────────────────────────────────────────────┐
│ 6. kube-proxy 更新负载均衡规则                        │
│    - iptables/IPVS                                     │
│    - 将流量分发到 3 个 Pod                            │
└────────────────────────────────────────────────────────┘
```

**Endpoint 变化自动处理**:
- Pod 创建 → 自动添加到 Endpoints
- Pod 删除 → 自动从 Endpoints 移除
- Pod 不健康（ReadinessProbe 失败）→ 自动从 Endpoints 移除
- Pod IP 变化 → 自动更新 Endpoints

---

## 6. 实际配置示例

### 6.1 完整示例：部署一个 Web 应用

#### 步骤 1: 部署应用

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  labels:
    app: myapp
spec:
  replicas: 3
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
        image: nginx:latest
        ports:
        - containerPort: 80
        readinessProbe:
          httpGet:
            path: /
            port: 80
          initialDelaySeconds: 5
          periodSeconds: 10
        livenessProbe:
          httpGet:
            path: /
            port: 80
          initialDelaySeconds: 15
          periodSeconds: 20
```

```bash
kubectl apply -f deployment.yaml
```

#### 步骤 2: 创建 Service

```yaml
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
spec:
  selector:
    app: myapp
  ports:
  - port: 80        # Service 端口
    targetPort: 80  # Pod 端口
  type: ClusterIP   # 集群内部访问
```

```bash
kubectl apply -f service.yaml

# 查看 Service
kubectl get svc myapp-service
# NAME            TYPE        CLUSTER-IP      PORT(S)   AGE
# myapp-service   ClusterIP   10.96.100.50    80/TCP    1m

# 查看 Endpoints
kubectl get endpoints myapp-service
# NAME            ENDPOINTS                          AGE
# myapp-service   10.244.1.5:80,10.244.1.6:80,...  1m
```

#### 步骤 3: 创建 Ingress

```yaml
# ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  rules:
  - host: app.example.com
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
kubectl apply -f ingress.yaml

# 查看 Ingress
kubectl get ingress myapp-ingress
# NAME            CLASS    HOSTS              ADDRESS        PORTS   AGE
# myapp-ingress   <none>   app.example.com    1.2.3.4        80      1m
```

#### 步骤 4: 配置 DNS

```
# 在 DNS 服务商处配置 A 记录
app.example.com A 1.2.3.4

# 或配置 CNAME 记录
app.example.com CNAME ingress.sealos.io
```

#### 步骤 5: 验证

```bash
# 本地测试（修改 /etc/hosts）
echo "1.2.3.4 app.example.com" >> /etc/hosts

# 访问测试
curl http://app.example.com/

# 查看 Ingress 日志
kubectl logs -n ingress-nginx <ingress-controller-pod>

# 查看 Pod 日志
kubectl logs -l app=myapp
```

### 6.2 HTTPS 配置示例

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress-tls
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  tls:
  - hosts:
    - app.example.com
    secretName: app-tls-cert  # TLS 证书
  rules:
  - host: app.example.com
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

### 6.3 多服务路由示例

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: multi-service-ingress
spec:
  rules:
  - host: example.com
    http:
      paths:
      # 前端 → root path
      - path: /
        pathType: Prefix
        backend:
          service:
            name: frontend-service
            port:
              number: 80

      # API → /api
      - path: /api
        pathType: Prefix
        backend:
          service:
            name: api-service
            port:
              number: 8080

      # 静态文件 → /static
      - path: /static
        pathType: Prefix
        backend:
          service:
            name: static-service
            port:
              number: 80
```

**请求路由**:
```
http://example.com/          → frontend-service
http://example.com/about     → frontend-service
http://example.com/api/users → api-service
http://example.com/static/img/logo.png → static-service
```

---

## 7. 常见问题

### 7.1 为什么配置了 Ingress 还是无法访问？

**排查步骤**:

```bash
# 1. 检查 Ingress 是否创建
kubectl get ingress

# 2. 检查 Ingress Controller 是否运行
kubectl get pods -n ingress-nginx

# 3. 检查 DNS 解析
dig app.example.com

# 4. 检查 Service 是否存在
kubectl get svc

# 5. 检查 Endpoints 是否有 Pod
kubectl get endpoints

# 6. 检查 Pod 是否运行
kubectl get pods -l app=myapp

# 7. 测试 Service 连通性
kubectl run test --image=busybox --rm -it -- wget -O- myapp-service:80

# 8. 查看 Ingress Controller 日志
kubectl logs -n ingress-nginx <ingress-pod>
```

### 7.2 502 Bad Gateway 是什么原因？

**常见原因**:

1. **Pod 未就绪**
```bash
# 检查 Pod 状态
kubectl get pods
kubectl describe pod <pod-name>

# 检查 ReadinessProbe
kubectl get pod <pod-name> -o yaml | grep -A 5 readinessProbe
```

2. **Service 端口错误**
```bash
# 检查 Service 配置
kubectl get svc myapp-service -o yaml

# 确认 port 和 targetPort
# port: Service 端口（Ingress 访问的端口）
# targetPort: Pod 端口（容器监听的端口）
```

3. **Endpoint 为空**
```bash
# 检查 Endpoints
kubectl get endpoints myapp-service

# 如果为空，说明没有匹配的 Pod
# 检查 Pod 标签是否匹配 Service selector
kubectl get pods --show-labels
```

### 7.3 如何实现灰度发布？

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: canary-ingress
  annotations:
    nginx.ingress.kubernetes.io/canary: "true"
    nginx.ingress.kubernetes.io/canary-weight: "10"  # 10% 流量到新版本
spec:
  rules:
  - host: app.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: myapp-v2-service  # 新版本
            port:
              number: 80
```

### 7.4 如何实现 WebSocket 支持？

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: websocket-ingress
  annotations:
    nginx.ingress.kubernetes.io/proxy-set-headers: "default/nginx-config"
spec:
  rules:
  - host: ws.example.com
    http:
      paths:
      - path: /ws
        pathType: Prefix
        backend:
          service:
            name: websocket-service
            port:
              number: 8080
```

**ConfigMap 配置**:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nginx-config
  namespace: default
data:
  X-Forwarded-Proto: "https"
  Upgrade: "websocket"
  Connection: "upgrade"
```

### 7.5 Ingress 和 LoadBalancer 类型 Service 的区别？

| 特性 | Ingress | LoadBalancer Service |
|------|---------|---------------------|
| 资源消耗 | 一个 LoadBalancer | 每个服务一个 LoadBalancer |
| 成本 | 低（一个公网 IP） | 高（多个公网 IP） |
| 功能 | HTTP/HTTPS 路由 | 4 层负载均衡 |
| 协议支持 | HTTP/HTTPS/gRPC/WebSocket | TCP/UDP |
| 灵活性 | 高（基于 Host/Path 路由） | 低（基于 IP:端口） |

**推荐**:
- 多个 HTTP/HTTPS 服务 → 使用 Ingress
- 单个非 HTTP 服务（如数据库）→ 使用 LoadBalancer
- 需要公开 TCP/UDP 服务 → 使用 LoadBalancer

---

## 8. 总结

### 8.1 关键要点

1. **DNS 解析**: 将域名解析到 Ingress Controller 的 IP
2. **Ingress Controller**: 根据规则（Host/Path）路由流量到 Service
3. **Service**: 提供稳定的虚拟 IP 和负载均衡
4. **Endpoints**: 自动跟踪 Pod IP 变化
5. **Pod**: 实际运行应用容器

### 8.2 数据流向

```
用户
  ↓ DNS 解析
Ingress Controller IP
  ↓ HTTP 请求（Host: app.example.com）
Ingress Controller
  ↓ 匹配 Ingress 规则
Service (ClusterIP)
  ↓ 负载均衡
Endpoints (Pod IPs)
  ↓ 请求转发
Pod
  ↓ 处理请求
返回响应
```

### 8.3 最佳实践

1. **使用 ReadinessProbe**: 确保 Pod 就绪后才加入 Endpoints
2. **配置健康检查**: Ingress Controller 定期检查后端健康状态
3. **设置超时**: 合理配置 proxy_connect_timeout、proxy_read_timeout
4. **启用监控**: 使用 Prometheus 监控 Ingress 和 Service
5. **使用 TLS**: 在生产环境启用 HTTPS
6. **配置资源限制**: 为 Ingress Controller 设置合理的 CPU/内存限制
7. **高可用部署**: Ingress Controller 使用多副本部署

---

## 附录

### A. 常用命令速查

```bash
# Ingress 管理
kubectl get ingress
kubectl describe ingress <name>
kubectl delete ingress <name>

# Service 管理
kubectl get svc
kubectl describe svc <name>
kubectl get endpoints <name>

# 测试连通性
kubectl run test --image=busybox --rm -it -- wget -O- http://myapp-service:80
kubectl port-forward svc/myapp-service 8080:80

# 查看 Ingress Controller 日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx

# 修改 /etc/hosts 测试
echo "1.2.3.4 app.example.com" | sudo tee -a /etc/hosts
```

### B. 相关资源

- [Kubernetes Ingress 官方文档](https://kubernetes.io/docs/concepts/services-networking/ingeess/)
- [Nginx Ingress Controller](https://kubernetes.github.io/ingress-nginx/)
- [Traefik Ingress Controller](https://doc.traefik.io/traefik/providers/kubernetes-ingress/)
- [Apache APISIX Ingress](https://apisix.apache.org/docs/ingress-controller/)

---

**作者**: cunzili
**日期**: 2025-01-15
**版本**: v1.0
