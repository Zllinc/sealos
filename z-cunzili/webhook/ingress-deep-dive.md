# Ingress 深度解析：常见问题详解

> 解答 Ingress 工作原理中的核心疑问

---

## 问题 1: DNS 解析出的 IP 是什么？一台机器只能部署一个 Ingress Controller 吗？

### 1.1 这个 IP 是什么？

DNS 解析出的 IP **不是** Ingress Controller Pod 所在机器的 IP，而是：

#### 场景 1: LoadBalancer 类型（生产环境常用）

```yaml
# Ingress Controller 的 Service 配置
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  namespace: ingress-nginx
spec:
  type: LoadBalancer  # ← 关键
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - port: 80
    targetPort: 80
  - port: 443
    targetPort: 443
```

**工作原理**:

```
┌─────────────────────────────────────────────────────────────┐
│ 云厂商 (AWS/阿里云/Azure)                                   │
│                                                             │
│  创建负载均衡器 (ALB/SLB/ELB)                               │
│  分配公网 IP: 1.2.3.4                                       │
│                                                             │
│  ┌────────────────────────────────────────────────────┐   │
│  │ 负载均衡器 (1.2.3.4)                               │   │
│  │   │                                                │   │
│  │   ├─→ Node 1 (192.168.1.10:30080)                 │   │
│  │   ├─→ Node 2 (192.168.1.11:30080)                 │   │
│  │   └─→ Node 3 (192.168.1.12:30080)                 │   │
│  └────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────────┐
│ Kubernetes 集群节点                                         │
│                                                             │
│  Node 1: 192.168.1.10                                      │
│    └─ Ingress Controller Pod 1                             │
│                                                             │
│  Node 2: 192.168.1.11                                      │
│    └─ Ingress Controller Pod 2                             │
│                                                             │
│  Node 3: 192.168.1.12                                      │
│    └─ Ingress Controller Pod 3                             │
└─────────────────────────────────────────────────────────────┘
```

**DNS 配置**:
```
# DNS 记录
app.example.com A 1.2.3.4
#              ↑
#         负载均衡器的 IP（不是 Pod IP，不是节点 IP）
```

**请求流程**:
```
用户请求: app.example.com
    │
    ▼ DNS 解析
1.2.3.4 (负载均衡器 IP)
    │
    ▼ 负载均衡
随机分发到:
    ├─→ Node 1 (192.168.1.10:30080)
    ├─→ Node 2 (192.168.1.11:30080)
    └─→ Node 3 (192.168.1.12:30080)
    │
    ▼ 节点端口转发
Ingress Controller Pod (80/443)
    │
    ▼ 处理请求
路由到后端 Service
```

#### 场景 2: NodePort 类型（测试/开发环境）

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  namespace: ingress-nginx
spec:
  type: NodePort  # ← 关键
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - port: 80
    targetPort: 80
    nodePort: 30080  # ← 节点端口 (30000-32767)
  - port: 443
    targetPort: 443
    nodePort: 30443
```

**工作原理**:

```
DNS 记录:
app.example.com A 192.168.1.10  # 任意节点 IP

或者使用轮询:
app.example.com A 192.168.1.10
app.example.com A 192.168.1.11
app.example.com A 192.168.1.12
```

**请求流程**:
```
用户请求: app.example.com
    │
    ▼ DNS 解析
192.168.1.10 (节点 1 IP)
    │
    ▼ HTTP 请求
Node 1:30080 (NodePort)
    │
    ▼ 转发到
Ingress Controller Pod (可能在任意节点)
    │
    ▼ 处理请求
```

#### 场景 3: HostNetwork 类型（特殊场景）

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: ingress-nginx
  namespace: ingress-nginx
spec:
  template:
    spec:
      hostNetwork: true  # ← 使用宿主机网络
      containers:
      - name: controller
        ports:
        - containerPort: 80
          hostPort: 80    # ← 绑定到宿主机 80 端口
        - containerPort: 443
          hostPort: 443
```

**工作原理**:

```
DNS 记录:
app.example.com A 192.168.1.10  # 节点 IP
app.example.com A 192.168.1.11
app.example.com A 192.168.1.12

请求直接到节点 80/443 端口
Ingress Controller 直接监听在宿主机上
```

### 1.2 一台机器只能部署一个 Ingress Controller 吗？

**答案：不是！** 一台机器可以部署多个 Ingress Controller：

#### 方案 1: 多副本（高可用）

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ingress-nginx
spec:
  replicas: 3  # ← 3 个副本
  template:
    spec:
      containers:
      - name: controller
        resources:
          requests:
            cpu: 500m
            memory: 512Mi
          limits:
            cpu: 1000m
            memory: 1Gi
```

**节点分布**:
```
Node 1 (192.168.1.10)
  └─ Ingress Controller Pod 1 (副本 1)

Node 2 (192.168.1.11)
  └─ Ingress Controller Pod 2 (副本 2)

Node 3 (192.168.1.12)
  └─ Ingress Controller Pod 3 (副本 3)

所有 Pod 共享一个 LoadBalancer IP: 1.2.3.4
```

**为什么需要多副本？**
- 高可用：一个 Pod 挂了，其他继续工作
- 负载分担：分散流量压力
- 零宕机：滚动更新时不中断服务

#### 方案 2: 多个 Ingress Controller（不同类型）

```bash
# 部署 Nginx Ingress Controller
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.8.1/deploy/static/provider/cloud/deploy.yaml

# 部署 Traefik Ingress Controller
kubectl apply -f https://raw.githubusercontent.com/traefik/traefik/v2.9/docs/content/content/static/traefik-k8s.yaml

# 部署 APISIX Ingress Controller
kubectl apply -f https://raw.githubusercontent.com/apache/apisix-ingress-controller/docs/en/latest/tutorials/docs-api-v1.yaml
```

**不同 Ingress Controller 使用不同的 IngressClass**:

```yaml
# Nginx Ingress Controller 配置
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  name: nginx
spec:
  controller: k8s.io/ingress-nginx

---
# Traefik Ingress Controller 配置
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  name: traefik
spec:
  controller: traefik.io/ingress-controller

---
# APISIX Ingress Controller 配置
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  name: apisix
spec:
  controller: apisix.org/ingress-controller
```

**使用不同的 IngressClass**:

```yaml
# 使用 Nginx 处理的 Ingress
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nginx-ingress
spec:
  ingressClassName: nginx  # ← 指定使用 nginx
  rules:
  - host: nginx.example.com
    http:
      paths:
      - backend:
          service:
            name: nginx-service
            port:
              number: 80

---
# 使用 Traefik 处理的 Ingress
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: traefik-ingress
spec:
  ingressClassName: traefik  # ← 指定使用 traefik
  rules:
  - host: traefik.example.com
    http:
      paths:
      - backend:
          service:
            name: traefik-service
            port:
              number: 80
```

**架构**:
```
                            ┌─→ Nginx Ingress Controller
LoadBalancer IP: 1.2.3.4 ──┼─→ Traefik Ingress Controller
                            └─→ APISIX Ingress Controller

每个 Ingress Controller 处理不同类型的流量
```

### 1.3 Ingress 资源本身有 IP 吗？

**答案：没有！** Ingress 资源只是**规则配置**，不是实际运行的组件。

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
spec:
  rules:
  - host: app.example.com
    http:
      paths:
      - backend:
          service:
            name: myapp-service
            port:
              number: 80
```

**Ingress 资源的本质**:
- 只是一个配置对象（类似路由表）
- 告诉 Ingress Controller 如何路由流量
- 不分配 IP，不监听端口
- 由 Ingress Controller 读取并应用

**类比**:
```
Ingress 资源 = 路由配置表
Ingress Controller = 路由器硬件

路由配置表没有 IP，路由器才有 IP
```

**查看 Ingress 时的 ADDRESS 字段**:
```bash
$ kubectl get ingress

NAME            CLASS    HOSTS              ADDRESS        PORTS   AGE
myapp-ingress   <none>   app.example.com    1.2.3.4        80      1d
                                              ↑
                                       这个 IP 是 Ingress
                                       Controller 的 Service IP
```

---

## 问题 2: Ingress 怎么找到对应的 Service？

### 2.1 不是通过标签，而是通过名称引用

**Ingress 资源配置**:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
spec:
  rules:
  - host: app.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: myapp-service  # ← Service 名称
            port:
              number: 8080       # ← Service 端口
```

**Service 资源**:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp-service  # ← 这个名称
  namespace: default    # ← 同一个 namespace
spec:
  selector:
    app: myapp          # ← 这是选择 Pod 的标签
  ports:
  - port: 8080
    targetPort: 8080
```

### 2.2 Ingress Controller 如何查找 Service

#### 步骤 1: 监听 Ingress 资源

```go
// Ingress Controller 伪代码
func (c *Controller) Run() {
    // 监听 Ingress 变化
    ingressWatcher := c.client.CoreV1().Ingresses("").Watch(context.Background())

    for event := range ingressWatcher.ResultChan() {
        ingress := event.Object.(*networking.Ingress)

        // 处理 Ingress 变化
        c.syncIngress(ingress)
    }
}

func (c *Controller) syncIngress(ingress *networking.Ingress) {
    for _, rule := range ingress.Spec.Rules {
        for _, path := range rule.HTTP.Paths {
            serviceName := path.Backend.Service.Name
            servicePort := path.Backend.Service.Port.Number

            // 通过名称查找 Service
            service, err := c.getService(ingress.Namespace, serviceName)
            if err != nil {
                log.Error("service not found: %s", serviceName)
                continue
            }

            // 获取 Service 的 ClusterIP 和 Endpoints
            clusterIP := service.Spec.ClusterIP
            endpoints := c.getEndpoints(service)

            // 生成配置
            c.updateConfig(rule.Host, path.Path, clusterIP, servicePort, endpoints)
        }
    }
}
```

#### 步骤 2: 查询 Kubernetes API

```bash
# Ingress Controller 通过 API 查询 Service
GET /api/v1/namespaces/{namespace}/services/{service-name}

# 示例：查询 myapp-service
GET /api/v1/namespaces/default/services/myapp-service

# 响应
{
  "kind": "Service",
  "metadata": {
    "name": "myapp-service",
    "namespace": "default",
    "uid": "12345678-1234-1234-1234-123456789abc"
  },
  "spec": {
    "clusterIP": "10.96.100.50",
    "ports": [{"port": 8080, "targetPort": 8080}],
    "selector": {"app": "myapp"}
  }
}
```

#### 步骤 3: 查询 Endpoints

```bash
# 查询 Service 的 Endpoints
GET /api/v1/namespaces/{namespace}/endpoints/{service-name}

# 示例
GET /api/v1/namespaces/default/endpoints/myapp-service

# 响应
{
  "kind": "Endpoints",
  "metadata": {
    "name": "myapp-service"
  },
  "subsets": [
    {
      "addresses": [
        {"ip": "10.244.1.5", "nodeName": "node-1"},
        {"ip": "10.244.1.6", "nodeName": "node-1"},
        {"ip": "10.244.2.7", "nodeName": "node-2"}
      ],
      "ports": [{"port": 8080}]
    }
  ]
}
```

### 2.3 完整的查找流程

```
┌─────────────────────────────────────────────────────────────┐
│ 1. 用户创建 Ingress                                          │
│    backend.service.name: myapp-service                      │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 2. Ingress Controller 监听到变化                             │
│    - Informer 通知 Ingress 创建事件                         │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 3. 解析 Ingress 配置                                         │
│    - 提取 backend.service.name: "myapp-service"            │
│    - 提取 backend.service.port.number: 8080                │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 4. 通过 Kubernetes API 查询 Service                          │
│    GET /api/v1/namespaces/default/services/myapp-service   │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 5. 获取 Service 信息                                         │
│    - ClusterIP: 10.96.100.50                                │
│    - Port: 8080                                             │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 6. 查询 Endpoints                                            │
│    GET /api/v1/namespaces/default/endpoints/myapp-service  │
│    - 10.244.1.5:8080                                        │
│    - 10.244.1.6:8080                                        │
│    - 10.244.2.7:8080                                        │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 7. 生成 Nginx 配置                                           │
│    upstream myapp-service-default-8080 {                    │
│      server 10.244.1.5:8080;                               │
│      server 10.244.1.6:8080;                               │
│      server 10.244.2.7:8080;                               │
│    }                                                        │
│                                                             │
│    server {                                                 │
│      server_name app.example.com;                          │
│      location / {                                          │
│        proxy_pass http://myapp-service-default-8080;      │
│      }                                                      │
│    }                                                        │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 8. 重载 Nginx 配置                                           │
│    nginx -s reload                                         │
└─────────────────────────────────────────────────────────────┘
```

### 2.4 标签的作用

**标签用于 Service 选择 Pod，而不是 Ingress 选择 Service！**

```yaml
# Pod 有标签
apiVersion: v1
kind: Pod
metadata:
  name: myapp-pod
  labels:
    app: myapp      # ← Pod 的标签
spec:
  containers:
  - name: myapp
    image: nginx

---
# Service 通过标签选择 Pod
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
spec:
  selector:
    app: myapp      # ← 选择有这个标签的 Pod
  ports:
  - port: 8080
    targetPort: 80

---
# Ingress 通过名称引用 Service（不使用标签！）
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
spec:
  rules:
  - host: app.example.com
    http:
      paths:
      - backend:
          service:
            name: myapp-service  # ← 直接使用名称
            port:
              number: 8080
```

**关系图**:
```
Pod (有标签)
    │
    │ labels: app=myapp
    ▼
Service (通过 selector 找 Pod)
    │
    │ name: myapp-service
    ▼
Ingress (通过 name 引用 Service)
```

---

## 问题 3: 必须使用 Ingress + Service 组合吗？

### 3.1 让应用外部可访问的 4 种方式

#### 方式 1: Ingress + Service（推荐用于 HTTP/HTTPS）

**适用场景**:
- 多个 HTTP/HTTPS 服务
- 需要基于域名/路径路由
- 希望节省成本（多个服务共享一个公网 IP）

**优势**:
- ✅ 成本低：一个公网 IP 对应多个服务
- ✅ 功能强：支持 TLS 终止、路由、重写等
- ✅ 易管理：集中管理所有 HTTP 入口流量

**劣势**:
- ❌ 只支持 HTTP/HTTPS/WebSocket/gRPC（L7 协议）
- ❌ 配置相对复杂

**示例**:
```
一个公网 IP: 1.2.3.4
    ├─→ app.example.com → App Service
    ├─→ api.example.com → API Service
    ├─→ blog.example.com → Blog Service
    └─→ admin.example.com → Admin Service
```

#### 方式 2: LoadBalancer Service（适用于单个服务）

**适用场景**:
- 单个非 HTTP 服务（如数据库、游戏服务器）
- 需要独立公网 IP
- 简单部署

**优势**:
- ✅ 配置简单
- ✅ 支持 TCP/UDP 任意协议
- ✅ 云厂商提供 DDoS 防护

**劣势**:
- ❌ 成本高：每个服务一个公网 IP 和负载均衡器
- ❌ 不支持基于域名/路径的路由

**示例**:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: mysql-lb
spec:
  type: LoadBalancer  # ← 直接创建负载均衡器
  selector:
    app: mysql
  ports:
  - port: 3306
    targetPort: 3306

# 分配公网 IP: 5.6.7.8
# DNS: mysql.example.com A 5.6.7.8
```

**成本对比**:
```
LoadBalancer 方式:
- App Service: $20/月
- API Service: $20/月
- Blog Service: $20/月
- Admin Service: $20/月
总计: $80/月

Ingress 方式:
- Ingress Controller: $20/月
总计: $20/月（节省 75%）
```

#### 方式 3: NodePort Service（测试/开发环境）

**适用场景**:
- 测试环境
- 开发环境
- 临时访问
- 不想创建负载均衡器

**优势**:
- ✅ 免费（无需云厂商负载均衡器）
- ✅ 配置简单

**劣势**:
- ❌ 端口号不友好（30000-32767）
- ❌ 需要知道节点 IP
- ❌ 不适合生产环境

**示例**:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp-nodeport
spec:
  type: NodePort
  selector:
    app: myapp
  ports:
  - port: 80
    targetPort: 80
    nodePort: 30080  # ← 节点端口

# 访问方式: http://节点IP:30080
```

#### 方式 4: hostNetwork（特殊场景）

**适用场景**:
- 系统级网络服务
- 需要直接使用宿主机网络
- 监控代理、网络插件

**示例**:
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: myapp
spec:
  template:
    spec:
      hostNetwork: true  # ← 使用宿主机网络
      containers:
      - name: myapp
        ports:
        - containerPort: 80
          hostPort: 80  # ← 绑定到宿主机 80 端口
```

### 3.2 对比总结

| 方式 | 协议支持 | 成本 | 复杂度 | 使用场景 |
|------|----------|------|--------|----------|
| **Ingress + Service** | HTTP/HTTPS/WebSocket/gRPC | 低（1 个 LB） | 中 | 多个 HTTP 服务（推荐） |
| **LoadBalancer** | TCP/UDP 任意协议 | 高（N 个 LB） | 低 | 单个非 HTTP 服务 |
| **NodePort** | TCP/UDP 任意协议 | 免费 | 低 | 测试/开发 |
| **hostNetwork** | 任意协议 | 免费 | 高 | 系统服务 |

### 3.3 是否必须 Service？

**Ingress 必须配合 Service 使用！**

**原因**:
1. **Service 提供稳定的访问入口**
   - Pod IP 会变（重启、重新调度）
   - Service ClusterIP 不变
   - Ingress 通过 Service 名称引用，不依赖 Pod IP

2. **Service 提供负载均衡**
   - 多个 Pod 副本
   - Service 自动分发流量
   - Ingress 不需要知道 Pod IP

3. **Service 提供服务发现**
   - Endpoints 自动跟踪 Pod
   - Pod 增删自动更新
   - Ingress Controller 动态获取后端

**错误示例**（Ingress 直接访问 Pod IP）:
```yaml
# ❌ 不支持！Ingress 不能直接配置 Pod IP
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: bad-ingress
spec:
  rules:
  - host: app.example.com
    http:
      paths:
      - backend:
          service:
            name: ""  # ← 必须指定 Service
          # 不支持直接配置 Pod IP
```

**正确示例**（通过 Service）:
```yaml
# ✅ 正确！必须通过 Service
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: good-ingress
spec:
  rules:
  - host: app.example.com
    http:
      paths:
      - backend:
          service:
            name: myapp-service  # ← 必须指定 Service 名称
            port:
              number: 80
```

### 3.4 特殊情况：ExternalName Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: external-api
spec:
  type: ExternalName
  externalName: api.external.com  # ← 外部域名

# Ingress 可以使用这个 Service
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: external-ingress
spec:
  rules:
  - host: external.example.com
    http:
      paths:
      - backend:
          service:
            name: external-api  # ← 引用 ExternalName Service
            port:
              number: 443
```

**用途**: 代理外部服务到集群内部域名

---

## 问题 4: Ingress Controller 如何匹配规则？需要遍历所有 Ingress 吗？

### 4.1 Ingress Controller 的匹配机制

**答案：不需要遍历！** 使用高效的数据结构（哈希表/字典）进行查找。

#### 4.1.1 数据结构设计

```go
// Ingress Controller 内部数据结构（简化版）
type IngressRouter struct {
    // 第一层：Host → Path 路由表
    HostRoutes map[string]*PathRouter

    // 第二层：通配符 Host 路由表
    WildcardRoutes map[string]*PathRouter

    // 第三层：正则 Host 路由表
    RegexRoutes []RegexRoute
}

type PathRouter struct {
    // 精确匹配
    ExactPaths map[string]*Backend

    // 前缀匹配
    PrefixPaths *PathTrie  // 路径树（前缀树）

    // 正则匹配
    RegexPaths []RegexPath
}

type Backend struct {
    ServiceName  string
    ServicePort  int32
    Endpoints    []string  // Pod IP 列表
}

type PathTrie struct {
    path     string
    backend  *Backend
    children map[string]*PathTrie
}
```

#### 4.1.2 匹配流程

```
用户请求: app.example.com/api/users
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 步骤 1: Host 匹配（O(1) 哈希查找）                          │
│                                                             │
│ router.HostRoutes["app.example.com"]                       │
│                                                             │
│ 时间复杂度: O(1)                                           │
└─────────────────────────────────────────────────────────────┘
    │
    ▼ 找到
┌─────────────────────────────────────────────────────────────┐
│ 步骤 2: Path 匹配                                           │
│                                                             │
│ PathRouter:                                                │
│   ExactPaths: {"/health": Backend1}                        │
│   PrefixPaths: PathTrie                                    │
│     ├── "/" → Backend2                                     │
│     │   ├── "api" → Backend3                               │
│     │   │   ├── "users" → Backend4                         │
│     │   │   └── "products" → Backend5                      │
│     │   └── "static" → Backend6                            │
│                                                             │
│ 匹配 "/api/users":                                         │
│   "/" → "api" → "users" → Backend4                        │
│                                                             │
│ 时间复杂度: O(path_length)                                 │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 步骤 3: 获取 Backend                                        │
│                                                             │
│ Backend4:                                                  │
│   ServiceName: "api-service"                               │
│   ServicePort: 8080                                        │
│   Endpoints:                                               │
│     - 10.244.1.5:8080                                      │
│     - 10.244.1.6:8080                                      │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 步骤 4: 负载均衡到 Pod                                      │
│                                                             │
│ 选择: 10.244.1.5:8080 (轮询)                                │
│                                                             │
│ 总时间复杂度: O(1) + O(path_length) ≈ O(1)                │
└─────────────────────────────────────────────────────────────┘
```

### 4.2 通配符匹配

```go
// 通配符 Host 路由表
type WildcardRouter struct {
    // "*.example.com" → PathRouter
    wildcardRoutes map[string]*PathRouter
}

// 匹配逻辑
func (r *WildcardRouter) Match(host string) *PathRouter {
    // 1. 先尝试精确匹配
    if router, ok := r.HostRoutes[host]; ok {
        return router
    }

    // 2. 尝试通配符匹配
    // "app.example.com" → 查找 "*.example.com"
    parts := strings.Split(host, ".")
    if len(parts) >= 2 {
        wildcard := "*." + strings.Join(parts[1:], ".")
        if router, ok := r.WildcardRoutes[wildcard]; ok {
            return router
        }
    }

    // 3. 尝试正则匹配（较少使用）
    for _, regexRoute := range r.RegexRoutes {
        if regexRoute.Pattern.MatchString(host) {
            return regexRoute.PathRouter
        }
    }

    return nil
}
```

**示例**:
```
Ingress 规则:
1. host: app.example.com
2. host: "*.example.com"
3. host: "*.test.com"

请求匹配:
- app.example.com → 规则 1（精确匹配优先）
- api.example.com → 规则 2（通配符）
- foo.test.com → 规则 3（通配符）
- other.com → 404（无匹配）
```

### 4.3 Path 前缀树（Trie）

```
Path Trie 结构:

"/"
├─ "api" (Backend: api-service)
│  ├─ "users" (Backend: users-service)
│  ├─ "products" (Backend: products-service)
│  └─ "orders" (Backend: orders-service)
├─ "static" (Backend: static-service)
│  ├─ "css" (Backend: static-service)
│  └─ "js" (Backend: static-service)
└─ "health" (Backend: health-service)

请求匹配:
/api/users     → api → users     → users-service
/api/products  → api → products  → products-service
/static/css    → static → css    → static-service
/health        → health          → health-service
```

**为什么使用 Trie？**
- 快速前缀匹配：O(path_length)
- 节省内存：公共前缀只存储一次
- 支持最长前缀匹配

### 4.4 实际性能

**Nginx Ingress Controller 性能**:

```
配置: 1000 个 Ingress 规则

查找时间:
- Host 匹配: < 1 微秒（哈希查找）
- Path 匹配: < 5 微秒（Trie 查找）
- 总查找时间: < 10 微秒

QPS: 单实例 10,000+ QPS
延迟: P99 < 1ms
```

**对比**:

| 方式 | 时间复杂度 | 1000 条规则 | 10000 条规则 |
|------|-----------|-------------|--------------|
| 遍历（线性查找） | O(n) | 50 微秒 | 500 微秒 |
| 哈希 + Trie | O(1) | < 10 微秒 | < 10 微秒 |

### 4.5 Ingress Controller 的缓存机制

```go
type IngressCache struct {
    // 配置缓存
    ConfigCache *lru.Cache

    // Endpoints 缓存
    EndpointsCache map[string]*Endpoints

    // SSL 证书缓存
    SSLCache map[string]*tls.Certificate
}

// 定期更新
func (c *IngressCache) Run() {
    // 每 1 秒同步一次配置
    ticker := time.NewTicker(1 * time.Second)

    for range ticker.C {
        c.syncIngresses()
        c.syncEndpoints()
        c.syncSecrets()
    }
}
```

**缓存更新**:
- Ingress 变化 → 立即更新（Informer 通知）
- Endpoints 变化 → 立即更新（Informer 通知）
- SSL 证书变化 → 立即更新
- 全量同步 → 每 1 分钟（兜底）

### 4.6 热更新机制

**Nginx Ingress Controller**:
```bash
# 检测到配置变化
1. 生成新配置到 /tmp/nginx-new.conf
2. 测试配置: nginx -t -c /tmp/nginx-new.conf
3. 重载配置: nginx -s reload
4. 优雅重启旧 Worker 进程
```

**Traefik Ingress Controller**:
```bash
# 动态配置，无需重启
1. 监听 Kubernetes API
2. 更新内存中的路由表
3. 动态路由表立即生效
```

**APISIX Ingress Controller**:
```bash
# 通过 etcd 动态更新
1. 监听 Kubernetes API
2. 写入 etcd
3. APISIX 从 etcd 读取配置
4. 动态更新路由表
```

---

## 总结

### 核心要点回顾

#### 1. DNS 解析的 IP
- ✅ 是 LoadBalancer 的 IP（云厂商负载均衡器）
- ✅ 或节点 IP（NodePort/hostNetwork）
- ❌ 不是 Pod IP
- ❌ 不是容器运行时的机器 IP
- ✅ 一个 IP 可以被多个 Ingress Controller 副本共享

#### 2. Ingress 找到 Service
- ✅ 通过**名称引用**，不是标签
- ✅ Ingress Controller 通过 Kubernetes API 查询 Service
- ✅ Service 通过**标签**选择 Pod
- ✅ 标签用于 Service → Pod，不用于 Ingress → Service

#### 3. 是否必须 Ingress + Service
- ✅ HTTP/HTTPS 服务：推荐 Ingress + Service
- ✅ 非 HTTP 服务：使用 LoadBalancer/NodePort
- ✅ Ingress 必须配合 Service 使用
- ✅ Service 提供稳定入口和负载均衡

#### 4. Ingress Controller 匹配规则
- ✅ 不遍历，使用哈希表和 Trie 树
- ✅ Host 匹配：O(1) 哈希查找
- ✅ Path 匹配：O(path_length) Trie 查找
- ✅ 总体时间复杂度：O(1)

### 架构关系图

```
┌─────────────────────────────────────────────────────────────┐
│ 外部世界                                                     │
│                                                              │
│  DNS: app.example.com → 1.2.3.4 (LoadBalancer IP)         │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ LoadBalancer (云厂商负载均衡器)                              │
│  IP: 1.2.3.4                                               │
│  分发流量到节点:                                            │
│    ├─ Node 1: 192.168.1.10                                │
│    ├─ Node 2: 192.168.1.11                                │
│    └─ Node 3: 192.168.1.12                                │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Ingress Controller (多副本)                                  │
│  Pod 1 (Node 1): 监听 80/443                               │
│  Pod 2 (Node 2): 监听 80/443                               │
│  Pod 3 (Node 3): 监听 80/443                               │
│                                                             │
│  内部路由表（哈希 + Trie）:                                 │
│    "app.example.com" → PathRouter                          │
│      "/" → frontend-service                                │
│      "/api" → api-service                                  │
└─────────────────────────────────────────────────────────────┘
    │
    ▼ 通过名称查找
┌─────────────────────────────────────────────────────────────┐
│ Service (ClusterIP: 虚拟 IP)                                │
│  frontend-service: 10.96.100.50                            │
│    ↓ selector: app=frontend                                │
│    ↓ Endpoints: [10.244.1.5, 10.244.1.6]                  │
│                                                             │
│  api-service: 10.96.100.51                                 │
│    ↓ selector: app=api                                     │
│    ↓ Endpoints: [10.244.2.7, 10.244.2.8]                  │
└─────────────────────────────────────────────────────────────┘
    │
    ▼ 通过标签选择
┌─────────────────────────────────────────────────────────────┐
│ Pod (实际运行应用)                                          │
│  Pod 1: 10.244.1.5 (app=frontend)                         │
│  Pod 2: 10.244.1.6 (app=frontend)                         │
│  Pod 3: 10.244.2.7 (app=api)                              │
│  Pod 4: 10.244.2.8 (app=api)                              │
└─────────────────────────────────────────────────────────────┘
```

---

**作者**: cunzili
**日期**: 2025-01-15
**版本**: v1.0
**相关文档**: [ingress-dns-workflow.md](./ingress-dns-workflow.md)
