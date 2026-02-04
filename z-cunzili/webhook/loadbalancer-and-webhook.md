# LoadBalancer 和 Webhook 工作原理详解

> 深入理解 LoadBalancer Service 和 Admission Webhook 的真实工作方式

---

## 问题 1: LoadBalancer Service 是如何配置的？IP 是什么？

### 1.1 核心概念澄清

**LoadBalancer Service 不是在集群外新开一台机器！**

LoadBalancer Service 是 Kubernetes 的一种**资源类型**，当你创建这种类型的 Service 时：

```
┌─────────────────────────────────────────────────────────────┐
│ Kubernetes 集群                                              │
│                                                              │
│  你执行: kubectl apply -f service.yaml                      │
│         ↓                                                    │
│  Kubernetes API Server 收到请求                             │
│         ↓                                                    │
│  Cloud Controller Manager 检测到 LoadBalancer 类型          │
│         ↓                                                    │
│  调用云厂商 API (AWS/阿里云/Azure)                          │
│         ↓                                                    │
│  云厂商自动创建负载均衡器                                   │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 云厂商基础设施（Kubernetes 集群外）                           │
│                                                              │
│  ┌────────────────────────────────────────────────────┐   │
│  │ 负载均衡器 (AWS ELB/阿里云 SLB/Azure LB)           │   │
│  │ 公网 IP: 1.2.3.4                                   │   │
│  │                                                     │   │
│  │ 后端服务器列表:                                     │   │
│  │   - Node 1: 192.168.1.10:30080                    │   │
│  │   - Node 2: 192.168.1.11:30080                    │   │
│  │   - Node 3: 192.168.1.12:30080                    │   │
│  └────────────────────────────────────────────────────┘   │
│                              │                              │
│                              ▼ 分发流量                     │
│                     Kubernetes 集群节点                      │
└─────────────────────────────────────────────────────────────┘
```

### 1.2 完整的创建流程

#### 步骤 1: 创建 LoadBalancer Service

```yaml
# lb-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  namespace: ingress-nginx
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-type: "nlb"  # AWS 注解
spec:
  type: LoadBalancer  # ← 关键：LoadBalancer 类型
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - name: http
    port: 80
    targetPort: 80
  - name: https
    port: 443
    targetPort: 443
```

#### 步骤 2: 应用配置

```bash
kubectl apply -f lb-service.yaml
```

#### 步骤 3: Kubernetes 自动化流程

```bash
# 时间线

T+0s: 你执行 kubectl apply
     ↓
T+1s: Kubernetes API Server 创建 Service 对象
     状态: Pending (无外部 IP)
     ↓
T+5s: Cloud Controller Manager 检测到 LoadBalancer 类型
     ↓
T+10s: 调用云厂商 API 创建负载均衡器
      - AWS: CreateLoadBalancer API
      - 阿里云: CreateLoadBalancer API
      - Azure: Create LoadBalancer API
     ↓
T+30s: 云厂商创建负载均衡器完成
      - 分配公网 IP: 1.2.3.4
      - 配置健康检查
      - 配置后端服务器列表
     ↓
T+35s: Cloud Controller Manager 更新 Service 状态
      - status.loadBalancer.ingress[0].ip = 1.2.3.4
     ↓
T+40s: 你可以查看到外部 IP
```

#### 步骤 4: 查看状态

```bash
# 创建中
$ kubectl get svc ingress-nginx -n ingress-nginx
NAME            TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)        AGE
ingress-nginx   LoadBalancer   10.96.100.50    <pending>     80:30080/TCP   10s
                                                        ↑
                                                     等待分配

# 创建完成
$ kubectl get svc ingress-nginx -n ingress-nginx
NAME            TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)        AGE
ingress-nginx   LoadBalancer   10.96.100.50    1.2.3.4       80:30080/TCP   2m
                                                        ↑
                                                    公网 IP 已分配

# 详细信息
$ kubectl describe svc ingress-nginx -n ingress-nginx

Name:                     ingress-nginx
Namespace:                ingress-nginx
Labels:                   app.kubernetes.io/name=ingress-nginx
Type:                     LoadBalancer
IP Family Policy:         SingleStack
IP Families:              IPv4
IP:                       10.96.100.50  # ClusterIP（虚拟 IP）
IPs:                      10.96.100.50
LoadBalancer Ingress:     1.2.3.4        # 公网 IP
Port:                     http  80/TCP
TargetPort:               80/TCP
NodePort:                 http  30080/TCP
Endpoints:                10.244.1.5:80,10.244.1.6:80,10.244.1.7:80

Events:
  Type     Reason                   Age   From                Message
  ----     ------                   ----  ----                -------
  Normal   EnsuringLoadBalancer     2m    service-controller  Ensuring load balancer
  Normal   EnsuredLoadBalancer      1m    service-controller  Ensured load balancer
```

### 1.3 不同云厂商的实现

#### AWS (Elastic Load Balancer)

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  annotations:
    # 使用 Network Load Balancer (NLB)
    service.beta.kubernetes.io/aws-load-balancer-type: nlb
    # 指定 NLB 的跨可用区配置
    service.beta.kubernetes.io/aws-load-balancer-scheme: internet-facing
    # 指定子网
    service.beta.kubernetes.io/aws-load-balancer-subnets: subnet-123,subnet-456
spec:
  type: LoadBalancer
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - port: 80
    targetPort: 80
```

**AWS 创建的资源**:
```
AWS 账户下自动创建:
- ELB/NLB: my-ingress-abc123 (AWS 自动命名)
- DNS 名: my-ingress-abc123.elb.us-west-2.amazonaws.com
- 公网 IP: 自动分配（可能多个）

$ kubectl get svc ingress-nginx
NAME            TYPE           CLUSTER-IP      EXTERNAL-IP                                                              PORT(S)
ingress-nginx   LoadBalancer   10.96.100.50    abc123.us-west-2.elb.amazonaws.com                                          80:30080/TCP
                                         ↑
                                  AWS ELB 的 DNS 名（推荐使用）
```

#### 阿里云 (Server Load Balancer)

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  annotations:
    # 指定 SLB 的规格
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-spec: slb.s3.small
    # 指定带宽
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-bandwidth: "10"
    # 指定负载均衡器类型
    service.beta.kubernetes.io/alibaba-cloud-loadbalancer-charge-type: paybytraffic
spec:
  type: LoadBalancer
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - port: 80
    targetPort: 80
```

**阿里云创建的资源**:
```
阿里云账户下自动创建:
- SLB 实例: lb-abc123 (阿里云自动命名)
- 公网 IP: 47.96.123.45
- 监听规则: HTTP :80 → 节点 :30080

$ kubectl get svc ingress-nginx
NAME            TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)
ingress-nginx   LoadBalancer   10.96.100.50    47.96.123.45   80:30080/TCP
                                         ↑
                                  阿里云 SLB 的公网 IP
```

#### Azure (Load Balancer)

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  annotations:
    service.beta.kubernetes.io/azure-load-balancer-internal: "false"  # 公网 LB
spec:
  type: LoadBalancer
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - port: 80
    targetPort: 80
```

### 1.4 LoadBalancer IP 的本质

**这个 IP 是云厂商分配的公网 IP**

```
┌─────────────────────────────────────────────────────────────┐
│ 云厂商控制台                                                 │
│                                                              │
│ 负载均衡器实例:                                              │
│   - 实例 ID: lb-abc123456                                   │
│   - 实例名称: k8s-ingress-nginx-abc123                       │
│   - 公网 IP: 1.2.3.4                                         │
│   - 状态: 运行中                                             │
│   - 监听端口: HTTP :80                                       │
│   - 后端服务器:                                              │
│       ├─ 192.168.1.10:30080 (健康)                          │
│       ├─ 192.168.1.11:30080 (健康)                          │
│       └─ 192.168.1.12:30080 (健康)                          │
│                                                              │
│ 成本: $20/月 (按规格和使用量)                                │
└─────────────────────────────────────────────────────────────┘
```

**这个 IP 的特点**:
- ✅ 是真实的公网 IP，可以从互联网访问
- ✅ 由云厂商分配和管理
- ✅ 需要付费（通常 $15-30/月）
- ✅ 云厂商负责维护负载均衡器的高可用
- ❌ 不是 Kubernetes 集群内的 IP
- ❌ 不是 Pod 的 IP
- ❌ 不是节点的 IP

### 1.5 负载均衡器如何找到节点？

**自动发现！** Kubernetes Cloud Controller Manager 自动配置：

```go
// Cloud Controller Manager 伪代码
func (cm *CloudControllerManager) syncLoadBalancer(service *v1.Service) {
    // 1. 获取所有节点
    nodes, err := cm.client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})

    // 2. 提取节点的 IP 和 NodePort
    var backendServers []BackendServer
    for _, node := range nodes.Items {
        nodeIP := getNodeIP(node)  // 192.168.1.10
        nodePort := getNodePort(service, 80)  // 30080

        backendServers = append(backendServers, BackendServer{
            IP:   nodeIP,
            Port: nodePort,
        })
    }

    // 3. 调用云厂商 API 更新负载均衡器的后端服务器列表
    cloudAPI.UpdateLoadBalancerBackends(loadBalancerID, backendServers)
}
```

**实际配置**（以 AWS 为例）:
```json
{
  "LoadBalancerName": "k8s-ingress-nginx-abc123",
  "Listeners": [
    {
      "Protocol": "TCP",
      "LoadBalancerPort": 80,
      "InstanceProtocol": "TCP",
      "InstancePort": 30080,
      "Instances": [
        {"InstanceId": "i-12345"},  // Node 1
        {"InstanceId": "i-67890"},  // Node 2
        {"InstanceId": "i-abcde"}   // Node 3
      ]
    }
  ]
}
```

### 1.6 完整的请求路径

```
用户浏览器
    │
    │ 1. 输入: http://app.example.com
    ▼
┌──────────────────────────────────────────┐
│ DNS 解析                                  │
│ app.example.com → 1.2.3.4                │
└──────────────────────────────────────────┘
    │
    │ 2. TCP 连接: 1.2.3.4:80
    ▼
┌──────────────────────────────────────────┐
│ 云厂商负载均衡器 (ELB/SLB)               │
│ 公网 IP: 1.2.3.4                         │
│                                          │
│ 3. 负载均衡算法: 轮询/最小连接/源IP哈希  │
└──────────────────────────────────────────┘
    │
    ├──────────────┬──────────────┬──────────────┐
    ▼              ▼              ▼              ▼
Node 1         Node 2         Node 3         (可能更多)
192.168.1.10    192.168.1.11    192.168.1.12
:30080         :30080         :30080
    │              │              │
    ▼              ▼              ▼
Ingress        Ingress        Ingress
Controller     Controller     Controller
Pod (可能      Pod (可能      Pod (可能
在任意节点)    在任意节点)    在任意节点)
    │              │              │
    └──────────────┴──────────────┴──────────────┐
                                                   │
                                                   ▼
                                        所有 Pod 共享配置
                                        (相同的 Ingress 规则)
```

### 1.7 本地开发环境（无云厂商）

**问题**: 本地 Kubernetes（minikube/kind/k3s）没有 LoadBalancer 功能

**解决方案 1: MetalLB**

```bash
# 安装 MetalLB
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/main/config/manifests/metallb-native.yaml

# 配置 IP 地址池
cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: example
  namespace: metallb-system
spec:
  addresses:
  - 192.168.1.200-192.168.1.250
EOF

# 创建 LoadBalancer Service
kubectl apply -f lb-service.yaml

# MetalLB 分配 IP
$ kubectl get svc ingress-nginx
NAME            TYPE           CLUSTER-IP      EXTERNAL-IP     PORT(S)
ingress-nginx   LoadBalancer   10.96.100.50    192.168.1.200   80:30080/TCP
                                                        ↑
                                               MetalLB 分配的 IP
```

**解决方案 2: NodePort + DNS**

```bash
# 使用 NodePort 类型
kubectl patch svc ingress-nginx -p '{"spec":{"type":"NodePort"}}'

# 配置 DNS 轮询
app.example.com A 192.168.1.10  # Node 1
app.example.com A 192.168.1.11  # Node 2
app.example.com A 192.168.1.12  # Node 3
```

**解决方案 3: kubectl port-forward（仅测试）**

```bash
# 端口转发到本地
kubectl port-forward -n ingress-nginx svc/ingress-nginx 8080:80

# 访问
curl http://localhost:8080
```

---

## 问题 2: Webhook 是在 API Server 和 Ingress Controller 之间进行判断吗？

### 2.1 正确的理解！

**是的！你的理解完全正确！**

```
用户请求流程：

用户 (kubectl/客户端)
    │
    │ 1. kubectl apply -f ingress.yaml
    ▼
┌──────────────────────────────────────────┐
│ Kubernetes API Server                    │
│                                          │
│ 收到创建 Ingress 资源的请求              │
└──────────────────────────────────────────┘
    │
    │ 2. 是否有匹配的 ValidatingWebhook？
    ▼
┌──────────────────────────────────────────┐
│ Admission Webhook (Sealos Webhook)      │
│                                          │
│ 验证:                                    │
│   - CNAME 是否指向系统域名?              │
│   - 域名是否被其他 NS 占用?              │
│   - ICP 备案是否完成?（可选）            │
│                                          │
│ 结果:                                    │
│   ✅ 通过 → 继续创建                     │
│   ❌ 拒绝 → 返回错误，停止创建           │
└──────────────────────────────────────────┘
    │
    │ 3. 验证通过
    ▼
┌──────────────────────────────────────────┐
│ etcd (Kubernetes 数据库)                 │
│                                          │
│ 持久化 Ingress 对象                      │
└──────────────────────────────────────────┘
    │
    │ 4. Informer 通知
    ▼
┌──────────────────────────────────────────┐
│ Ingress Controller                       │
│                                          │
│ 监听到 Ingress 创建事件                  │
│   → 更新内部路由表                       │
│   → 重新生成 Nginx 配置                  │
│   → 重载 Nginx                           │
└──────────────────────────────────────────┘
```

### 2.2 详细的时序图

```
时间  用户/组件                  操作
─────────────────────────────────────────────────────────────
T0    User                     kubectl apply -f ingress.yaml
                                │
                                ▼
T1    API Server               收到请求，解析为 Create Ingress 操作
                                │
                                ▼
T2    Admission Controller      检查是否有匹配的 Webhook
                                │
                                ├─→ 查找 ValidatingWebhookConfiguration
                                │   ✓ 找到: vingress.sealos.io
                                │
                                ▼
T3    API Server               发送 AdmissionReview 请求
                                │   到 Sealos Webhook 服务
                                │   (POST /validate-networking-k8s-io-v1-ingress)
                                │
                                ▼
T4    Sealos Webhook           收到请求，解析:
                                │   - UserInfo: system:serviceaccount:ns-user1:default
                                │   - Ingress: host: app.example.com
                                │
                                ▼
T5    Sealos Webhook           执行验证逻辑:
                                │   1. isUserServiceAccount()? → Yes
                                │   2. isUserNamespace()? → Yes
                                │   3. checkCname(app.example.com)
                                │      - DNS 查询: app.example.com CNAME myapp.sealos.io
                                │      - 检查: myapp.sealos.io 以 sealos.io 结尾 → Yes
                                │   4. checkOwner(app.example.com)
                                │      - 查询: 是否有其他 NS 使用此域名?
                                │      - 结果: 无 → Yes
                                │   5. checkIcp(app.example.com)
                                │      - ICP enabled? → No (跳过)
                                │
                                ▼
T6    Sealos Webhook           验证通过！返回:
                                │   {
                                │     "apiVersion": "admission.k8s.io/v1",
                                │     "kind": "AdmissionReview",
                                │     "response": {
                                │       "uid": "...",
                                │       "allowed": true
                                │     }
                                │   }
                                │
                                ▼
T7    API Server               收到 Webhook 响应，allowed = true
                                │   → 继续创建 Ingress 对象
                                │
                                ▼
T8    etcd                     持久化 Ingress 对象
                                │
                                ▼
T9    API Server               返回成功给用户
                                │   $ kubectl apply -f ingress.yaml
                                │   ingress.networking.k8s.io/myapp created
                                │
                                ▼
T10   Informer                 通知所有监听者
                                │   - Ingress Controller 的 Informer
                                │
                                ▼
T11   Ingress Controller        收到事件: Added ingress/myapp
                                │   → 解析 Ingress 规则
                                │   → 生成 Nginx 配置
                                │   → nginx -s reload
                                │
                                ▼
T12   Ingress Controller        Nginx 重载完成
                                │   → 现在可以处理 app.example.com 的流量
```

### 2.3 如果验证失败会怎样？

```
时间  用户/组件                  操作
─────────────────────────────────────────────────────────────
T0    User                     kubectl apply -f ingress.yaml
                                │
                                ▼
T1-T4  (同上)                   到达 Sealos Webhook
                                │
                                ▼
T5    Sealos Webhook           执行验证逻辑:
                                │   1. checkCname(app.example.com)
                                │      - DNS 查询: app.example.com CNAME other.com
                                │      - 检查: other.com 不以 sealos.io 结尾 → No
                                │
                                ▼
T6    Sealos Webhook           验证失败！返回:
                                │   {
                                │     "apiVersion": "admission.k8s.io/v1",
                                │     "kind": "AdmissionReview",
                                │     "response": {
                                │       "uid": "...",
                                │       "allowed": false,
                                │       "result": {
                                │         "message": "40300: can not verify ingress host app.example.com, cname is not end with any domains in sealos.io"
                                │       }
                                │     }
                                │   }
                                │
                                ▼
T7    API Server               收到 Webhook 响应，allowed = false
                                │   → 停止创建 Ingress 对象
                                │   → 返回错误给用户
                                │
                                ▼
T8    User                     收到错误:
                                │   Error from server (InternalError):
                                │   admission webhook "vingress.sealos.io" denied the request:
                                │   40300: can not verify ingress host app.example.com,
                                │   cname is not end with any domains in sealos.io
                                │
                                ✗ Ingress 未创建
                                ✗ Ingress Controller 不会收到通知
                                ✗ etcd 中不会保存此对象
```

### 2.4 Webhook 的配置

```yaml
# ValidatingWebhookConfiguration (注册 Webhook)
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: ingress-validator
webhooks:
- name: vingress.sealos.io
  rules:
  - operations: ["CREATE", "UPDATE"]  # 拦截创建和更新操作
    apiGroups: ["networking.k8s.io"]
    apiVersions: ["v1"]
    resources: ["ingresses"]           # 拦截 Ingress 资源
  failurePolicy: Ignore                # Webhook 不可用时是否忽略
  sideEffects: None
  admissionReviewVersions: ["v1"]
  clientConfig:
    service:                          # Webhook 服务的地址
      namespace: sealos-system
      name: webhook-service
      path: /validate-networking-k8s-io-v1-ingress
      port: 443
```

**工作原理**:
```go
// API Server 伪代码
func (s *APIServer) Admit(attrs admission.Attributes) error {
    // 1. 查找匹配的 ValidatingWebhook
    webhooks := s.webhookResolver.ResolveWebhooks(attrs)

    // 2. 调用每个 Webhook
    for _, webhook := range webhooks {
        // 构造 AdmissionReview 请求
        review := admissionreview.AdmissionReview{
            TypeMeta: metav1.TypeMeta{
                Kind:       "AdmissionReview",
                APIVersion: "admission.k8s.io/v1",
            },
            Request: &admissionreview.AdmissionRequest{
                UID:                attrs.GetUID(),
                Kind:               attrs.GetKind(),
                Resource:           attrs.GetResource(),
                SubResource:        attrs.GetSubResource(),
                RequestKind:        attrs.GetKind(),
                RequestResource:    attrs.GetResource(),
                Name:               attrs.GetName(),
                Namespace:          attrs.GetNamespace(),
                Operation:          admission.Operation(attrs.GetOperation()),
                UserInfo:           attrs.GetUserInfo(),
                Object:             runtime.RawExtension{Object: attrs.GetObject()},
                OldObject:          runtime.RawExtension{Object: attrs.GetOldObject()},
                DryRun:             attrs.IsDryRun(),
                Options:            runtime.RawExtension{Object: attrs.GetOptions()},
            },
        }

        // 发送请求到 Webhook 服务
        response := s.callWebhook(webhook, review)

        // 检查响应
        if !response.Response.Allowed {
            // 拒绝请求
            return fmt.Errorf(response.Response.Result.Message)
        }
    }

    return nil  // 所有 Webhook 都通过
}
```

### 2.5 Webhook 和 Ingress Controller 的职责划分

| 阶段 | 组件 | 职责 | 时机 |
|------|------|------|------|
| **验证阶段** | Admission Webhook | 验证请求是否合法 | 创建/更新 Ingress 资源时（秒级） |
| **运行阶段** | Ingress Controller | 根据合法的 Ingress 配置路由流量 | 处理实际的用户请求（毫秒级） |

**类比**:
```
Admission Webhook = 机场安检
  - 检查行李是否合规
  - 检查证件是否有效
  - 不通过就不能登机

Ingress Controller = 航空调度系统
  - 根据航班计划调度飞机
  - 引导飞机到正确的登机口
  - 处理实际飞行任务
```

### 2.6 Mutating Webhook vs Validating Webhook

Sealos 使用了两种 Webhook：

#### Validating Webhook（验证）
```
职责: 检查并拒绝不合法的请求
时机: 在对象持久化到 etcd 之前
结果: ✅ Allowed 或 ❌ Denied
示例: Sealos 的域名验证
```

#### Mutating Webhook（修改）
```
职责: 修改对象内容
时机: 在对象持久化到 etcd 之前（Validating 之前）
结果: 修改后的对象
示例: Sealos 自动添加注解
```

**执行顺序**:
```
用户请求
    ↓
Mutating Webhook (修改)
    ↓
Validating Webhook (验证)
    ↓
持久化到 etcd
    ↓
Ingress Controller 应用配置
```

### 2.7 Sealos Webhook 的完整工作流

```
┌─────────────────────────────────────────────────────────────┐
│ 用户创建 Ingress                                             │
│ kubectl apply -f ingress.yaml                              │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Mutating Webhook (优先执行)                                 │
│                                                              │
│ - 检查: 是否在用户 NS?                                      │
│ - 检查: 是否使用系统域名?                                   │
│ - 操作: 自动添加注解                                        │
│   sealos.io/namespace: ns-user1                             │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Validating Webhook                                           │
│                                                              │
│ 1. 身份验证                                                 │
│    isUserServiceAccount()? → Yes                           │
│    isUserNamespace()? → Yes                                │
│                                                              │
│ 2. CNAME 验证                                               │
│    DNS 查询 → myapp.sealos.io                               │
│    检查后缀 → ✅ 通过                                        │
│                                                              │
│ 3. 所有权验证                                               │
│    查询其他 NS → ✅ 无冲突                                   │
│                                                              │
│ 4. ICP 验证（可选）                                         │
│    查询备案信息 → ✅ 已备案                                   │
│                                                              │
│ 结果: ✅ Allowed                                            │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 持久化到 etcd                                               │
│                                                              │
│ Ingress 对象已保存                                          │
│ metadata.annotations:                                        │
│   sealos.io/namespace: ns-user1                             │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Ingress Controller Informer                                 │
│                                                              │
│ 监听到 Ingress 创建事件                                     │
│                                                              │
│ 操作:                                                        │
│ 1. 解析 Ingress 规则                                        │
│ 2. 查询 Service 信息                                        │
│ 3. 查询 Endpoints 信息                                       │
│ 4. 生成 Nginx 配置                                          │
│ 5. 重载 Nginx                                               │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ 用户请求到达                                                 │
│                                                              │
│ http://app.example.com                                      │
│                                                              │
│ → Ingress Controller 匹配规则                                │
│ → 转发到 Service                                            │
│ → 负载均衡到 Pod                                            │
│ → 返回响应                                                  │
└─────────────────────────────────────────────────────────────┘
```

---

## 总结

### 问题 1 总结

**LoadBalancer Service 不是在集群外新开机器**：
- ✅ 是 Kubernetes 的 Service 类型
- ✅ 触发云厂商自动创建负载均衡器
- ✅ 负载均衡器的 IP 是云厂商分配的公网 IP
- ✅ 自动配置后端服务器列表（Kubernetes 节点）
- ✅ 由云厂商 Cloud Controller Manager 管理

**配置方式**:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
spec:
  type: LoadBalancer  # ← 唯一需要配置的
  selector:
    app.kubernetes.io/name: ingress-nginx
  ports:
  - port: 80
    targetPort: 80
```

### 问题 2 总结

**Webhook 在 API Server 和 Ingress Controller 之间验证**：
- ✅ 用户创建 Ingress 时先经过 Webhook 验证
- ✅ 验证不通过直接拒绝，不保存到 etcd
- ✅ 验证通过后 Ingress 对象保存到 etcd
- ✅ Ingress Controller 通过 Informer 监听变化
- ✅ 只有合法的 Ingress 才会被 Ingress Controller 应用

**执行顺序**:
```
用户请求
  ↓
Mutating Webhook (自动添加注解)
  ↓
Validating Webhook (验证域名/CNAME/所有权/ICP)
  ↓
持久化到 etcd
  ↓
Ingress Controller 应用配置
  ↓
处理实际流量
```

---

**作者**: cunzili
**日期**: 2025-01-15
**版本**: v1.0
**相关文档**: [ingress-deep-dive.md](./ingress-deep-dive.md) | [ingress-dns-workflow.md](./ingress-dns-workflow.md)
