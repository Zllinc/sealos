# Sealos Admission Webhook 详细文档

> 作者: cunzili
> 日期: 2025-01-15
> 版本: v1.0

---

## 目录

- [1. 概述](#1-概述)
- [2. 需求场景](#2-需求场景)
- [3. 架构设计](#3-架构设计)
- [4. 核心功能](#4-核心功能)
- [5. 实现细节](#5-实现细节)
- [6. 部署配置](#6-部署配置)
- [7. 错误码说明](#7-错误码说明)

---

## 1. 概述

Sealos Admission Webhook 是一个基于 Kubernetes Admission Webhook 机制的准入控制服务，用于在资源创建、更新、删除时进行验证和修改操作。该服务主要处理 **Ingress** 和 **Namespace** 两种 Kubernetes 资源。

### 1.1 核心目标

- **多租户域名管理**: 防止用户滥用域名资源，确保域名使用的可控性
- **资源隔离**: 通过命名空间前缀区分用户和系统资源
- **安全合规**: 支持域名备案（ICP）验证，满足国内监管要求

### 1.2 文件结构

```
webhooks/
└── admission/
    ├── cmd/
    │   └── main.go                 # 程序入口
    ├── api/v1/
    │   ├── ingress_webhook.go      # Ingress 资源处理
    │   ├── namespace_webhook.go    # Namespace 资源处理
    │   ├── icp.go                  # ICP 备案验证
    │   ├── types.go                # 类型定义
    │   └── utils.go                # 工具函数
    ├── pkg/
    │   └── code/
    │       └── code.go             # 错误码定义
    └── config/
        ├── webhook/                # Webhook 配置
        ├── rbac/                   # RBAC 权限配置
        └── default/                # 默认配置
```

---

## 2. 需求场景

### 2.1 核心问题

在 Sealos 云原生操作系统的多租户环境中，用户需要创建 Ingress 来暴露服务，但面临以下挑战：

#### 问题 1: 域名滥用风险
**场景**: 用户可以随意创建任意域名的 Ingress，可能导致：
- 恶意占用系统域名
- 冒充其他服务
- DNS 解析冲突

**示例**:
```yaml
# 用户恶意创建系统域名的 Ingress
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: fake-console
  namespace: ns-attacker
spec:
  rules:
  - host: console.sealos.io  # 冒充系统控制台
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: malicious-service
            port:
              number: 80
```

#### 问题 2: 域名所有权冲突
**场景**: 多个用户尝试使用同一个域名

```yaml
# 用户 A 在 ns-user1 中创建
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
spec:
  rules:
  - host: myapp.com  # 用户 A 拥有的域名

---
# 用户 B 在 ns-user2 中尝试使用相同域名
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user2
spec:
  rules:
  - host: myapp.com  # 冲突！域名已被用户 A 使用
```

#### 问题 3: 缺乏域名验证机制
**场景**: 用户声称拥有某个域名，但实际无法控制该域名的 DNS 解析

#### 问题 4: 监管合规要求（中国场景）
**场景**: 根据中国法律法规，使用域名提供服务需要完成 ICP 备案

### 2.2 解决方案

Admission Webhook 通过以下机制解决上述问题：

| 问题 | 解决方案 | 实现位置 |
|------|----------|----------|
| 域名滥用 | CNAME 验证 + 域名白名单 | `checkCname()` |
| 域名冲突 | 域名所有权检查 | `checkOwner()` |
| 域名验证 | DNS CNAME 解析验证 | `checkCname()` |
| ICP 合规 | 可选的备案验证 | `checkIcp()` |

---

## 3. 架构设计

### 3.1 技术栈

```
┌─────────────────────────────────────────────────────────┐
│                   Kubernetes API Server                 │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│              Admission Webhook Service                   │
│  ┌────────────────────────────────────────────────────┐ │
│  │           controller-runtime                        │ │
│  │  ┌──────────────┐  ┌──────────────┐               │ │
│  │  │ Ingress      │  │ Namespace    │               │ │
│  │  │ Webhook      │  │ Webhook      │               │ │
│  │  │              │  │              │               │ │
│  │  │ - Validator  │  │ - Validator  │               │ │
│  │  │ - Mutator    │  │ - Mutator    │               │ │
│  │  └──────────────┘  └──────────────┘               │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                    External Services                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │   DNS    │  │   ICP    │  │  K8s API │              │
│  │  Server  │  │  API     │  │  Server  │              │
│  └──────────┘  └──────────┘  └──────────┘              │
└─────────────────────────────────────────────────────────┘
```

### 3.2 Webhook 类型

| Webhook | 类型 | 路径 | 功能 |
|---------|------|------|------|
| Ingress Mutator | Mutating | `/mutate-networking-k8s-io-v1-ingress` | 修改 Ingress 注解 |
| Ingress Validator | Validating | `/validate-networking-k8s-io-v1-ingress` | 验证 Ingress 合法性 |
| Namespace Mutator | Mutating | `/mutate--v1-namespace` | 添加 Namespace 注解 |
| Namespace Validator | Validating | `/validate--v1-namespace` | 验证 Namespace 权限 |

### 3.3 请求处理流程

```
用户请求
   │
   ▼
Kubernetes API Server
   │
   ▼
┌─────────────────────────────────────┐
│  是否匹配 Webhook 规则？             │
│  - 资源类型                          │
│  - 操作类型 (CREATE/UPDATE/DELETE)   │
└─────────────────────────────────────┘
   │                    │
   │ 是                 │ 否
   ▼                    ▼
┌─────────────┐    直接处理
│ 发送到 Webhook
│    服务
└─────────────┘
   │
   ▼
┌─────────────────────────────────────┐
│  识别请求身份                        │
│  - UserInfo.Username                 │
│  - UserInfo.Groups                   │
└─────────────────────────────────────┘
   │
   ▼
┌─────────────────────────────────────┐
│  判断是否需要验证？                  │
│  - 是否是用户 ServiceAccount？       │
│  - 是否是用户 Namespace？            │
└─────────────────────────────────────┘
   │                    │
   │ 是                 │ 否
   ▼                    ▼
┌─────────────┐    直接放行
│ 执行验证逻辑
│  - checkCname
│  - checkOwner
│  - checkIcp
└─────────────┘
   │
   ▼
返回结果 (Allow/Deny)
```

---

## 4. 核心功能

### 4.1 用户与系统资源隔离

#### 4.1.1 识别规则

**用户 ServiceAccount**:
```go
// 命名格式: system:serviceaccount:ns-*
const userServiceAccountPrefix = "system:serviceaccount:ns-"

func isUserServiceAccount(sa string) bool {
    return strings.HasPrefix(sa, userServiceAccountPrefix)
}
```

**用户 Namespace**:
```go
// 命名格式: ns-*
const userNamespacePrefix = "ns-"

func isUserNamespace(ns string) bool {
    return strings.HasPrefix(ns, userNamespacePrefix)
}
```

#### 4.1.2 隔离策略

| 资源类型 | 用户 ServiceAccount | 系统 ServiceAccount |
|---------|-------------------|-------------------|
| 用户 Namespace (ns-*) | 受限验证 | 跳过验证 |
| 系统 Namespace (kube-*, 等) | 跳过验证 | 跳过验证 |

**代码实现** (`ingress_webhook.go:176-184`):
```go
if !isUserServiceAccount(request.UserInfo.Username) {
    ilog.Info("user is not user's serviceaccount, skip validate")
    return nil  // 非用户 SA，跳过验证
}

if !isUserNamespace(i.Namespace) {
    ilog.Info("namespace is system namespace, skip validate")
    return nil  // 系统 NS，跳过验证
}
```

### 4.2 Ingress 域名验证

#### 4.2.1 CNAME 验证

**目的**: 确保用户使用的域名通过 CNAME 指向系统域名，证明域名控制权。

**验证逻辑**:

```go
func (v *IngressValidator) checkCname(i *netv1.Ingress, rule *netv1.IngressRule) error {
    // 1. 查询域名的 CNAME 记录
    cname, err := net.LookupCNAME(rule.Host)
    if err != nil {
        return err  // DNS 查询失败，拒绝请求
    }
    cname = strings.TrimSuffix(cname, ".")

    // 2. 检查域名本身是否是系统域名的子域名
    for _, domain := range v.Domains {
        if strings.HasSuffix(rule.Host, domain) {
            return nil  // 直接匹配，通过验证
        }
    }

    // 3. 检查 CNAME 是否指向系统域名
    for _, domain := range v.Domains {
        if strings.HasSuffix(cname, domain) {
            return nil  // CNAME 匹配，通过验证
        }
    }

    // 4. 都不匹配，拒绝请求
    return fmt.Errorf("can not verify ingress host %s, cname is not end with any domains", rule.Host)
}
```

**验证流程图**:
```
用户创建 Ingress (host: example.com)
         │
         ▼
┌────────────────────────────┐
│ DNS 查询: example.com 的 CNAME
│ 结果: app.sealos.io
└────────────────────────────┘
         │
         ▼
┌────────────────────────────┐
│ 检查: app.sealos.io 是否属于
│       系统域名 (*.sealos.io)
└────────────────────────────┘
         │
    ┌────┴────┐
    │         │
   是         否
    │         │
    ▼         ▼
  通过      拒绝
```

**示例**:

| 用户域名 | CNAME | 系统域名 | 结果 |
|---------|-------|---------|------|
| `app.example.com` | `app.sealos.io` | `sealos.io` | ✅ 通过 |
| `app.sealos.io` | - | `sealos.io` | ✅ 通过 |
| `app.example.com` | `app.other.io` | `sealos.io` | ❌ 拒绝 |
| `app.example.com` | (无 CNAME) | `sealos.io` | ❌ 拒绝 |

#### 4.2.2 域名所有权验证

**目的**: 防止多个用户（不同 Namespace）使用相同的域名。

**实现机制**:

1. **使用索引加速查询**:
```go
// 启动时为 Ingress 创建 host 索引
v.cache.IndexField(
    context.Background(),
    &netv1.Ingress{},
    IngressHostIndex,  // "host"
    func(obj client.Object) []string {
        ingress := obj.(*netv1.Ingress)
        var hosts []string
        for _, rule := range ingress.Spec.Rules {
            hosts = append(hosts, rule.Host)
        }
        return hosts
    },
)
```

2. **验证逻辑** (`ingress_webhook.go:229-245`):
```go
func (v *IngressValidator) checkOwner(i *netv1.Ingress, rule *netv1.IngressRule) error {
    // 1. 使用索引查询所有使用该 host 的 Ingress
    iList := &netv1.IngressList{}
    if err := v.cache.List(context.Background(), iList,
        client.MatchingFields{IngressHostIndex: rule.Host}); err != nil {
        return err
    }

    // 2. 检查是否有其他 Namespace 使用该 host
    for _, exitsIngress := range iList.Items {
        if exitsIngress.Namespace != i.Namespace {
            return fmt.Errorf("ingress host %s is owned by %s",
                rule.Host, exitsIngress.Namespace)
        }
    }

    return nil  // 该 host 仅在当前 Namespace 使用，通过验证
}
```

**场景示例**:

```yaml
# 用户 A 创建 Ingress
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
spec:
  rules:
  - host: myapp.com
    http:
      paths:
      - backend:
          service:
            name: myservice
            port:
              number: 80
```

```yaml
# 用户 B 尝试使用相同域名 - 会被拒绝！
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user2
spec:
  rules:
  - host: myapp.com  # 错误: 域名已被 ns-user1 占用
    http:
      paths:
      - backend:
          service:
            name: myservice
            port:
              number: 80
```

**错误响应**:
```
Error from server (InternalError): error when creating "ingress.yaml":
admission webhook "vingress.sealos.io" denied the request:
40301: ingress host myapp.com is owned by ns-user1, you can not create ingress with same host.
```

#### 4.2.3 ICP 备案验证（可选）

**目的**: 满足中国法律法规要求，确保域名已备案。

**启用条件**:
```go
v.IcpValidator = NewIcpValidator(
    os.Getenv("ICP_ENABLED") == "true",  // 环境变量控制
    os.Getenv("ICP_ENDPOINT"),
    os.Getenv("ICP_KEY"),
)
```

**验证逻辑** (`icp.go:58-94`):
```go
func (i *IcpValidator) Query(rule *netv1.IngressRule) (*IcpResponse, error) {
    // 1. 提取有效顶级域名
    domainName, err := publicsuffix.EffectiveTLDPlusOne(rule.Host)

    // 2. 检查缓存
    if cached, found := i.cache.Get(domainName); found {
        return cached.(*IcpResponse), nil
    }

    // 3. 调用 ICP 查询 API
    data := url.Values{}
    data.Set("domainName", domainName)
    data.Set("key", i.key)
    resp, err := http.PostForm(i.endpoint, data)

    // 4. 解析响应
    var response IcpResponse
    json.NewDecoder(resp.Body).Decode(&response)

    // 5. 缓存结果
    //    - 已备案: 缓存 30 天
    //    - 未备案: 缓存 5 分钟
    i.cache.Set(domainName, &response, genCacheTTL(&response))

    return &response, nil
}
```

**缓存策略**:
```go
func genCacheTTL(rsp *IcpResponse) time.Duration {
    // 已备案域名，长期缓存
    if rsp.ErrorCode == 0 && rsp.Result.SiteLicense != "" {
        return 30 * 24 * time.Hour  // 30 天
    }
    // 未备案域名，短期缓存
    return 5 * time.Minute
}
```

**ICP 验证逻辑** (`ingress_webhook.go:247-270`):
```go
func (v *IngressValidator) checkIcp(i *netv1.Ingress, rule *netv1.IngressRule) error {
    if !v.IcpValidator.enabled {
        return nil  // ICP 验证未启用，跳过
    }

    icpRep, err := v.IcpValidator.Query(rule)
    if err != nil {
        return fmt.Errorf("icp query error: %s", err.Error())
    }

    // 检查备案信息
    if icpRep.Result.SiteLicense == "" {
        return fmt.Errorf("icp query result is empty")
    }

    return nil
}
```

### 4.3 Ingress 修改（Mutating）

**目的**: 为用户创建的 Ingress 自动添加系统注解。

**触发条件**:
```go
if isUserNamespace(i.Namespace) && hasSubDomain(i, domain) {
    // 用户 Namespace + 使用系统域名子域名
    m.mutateUserIngressAnnotations(i)
}
```

**实现** (`ingress_webhook.go:77-82`):
```go
func (m *IngressMutator) mutateUserIngressAnnotations(i *netv1.Ingress) {
    initAnnotationAndLabels(&i.ObjectMeta)
    for k, v := range m.IngressAnnotations {
        i.Annotations[k] = v  // 添加配置的注解
    }
}
```

**配置参数**:
```bash
--ingress-mutating-annotations=key1=value1,key2=value2
```

**示例**:

创建前:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
  annotations: {}
spec:
  rules:
  - host: app.sealos.io
```

创建后（自动添加注解）:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
  annotations:
    sealos.io/namespace: ns-user1  # 自动添加
    kubernetes.io/ingress.class: nginx  # 配置中指定
spec:
  rules:
  - host: app.sealos.io
```

### 4.4 Namespace 控制

#### 4.4.1 Namespace 修改

**功能**: 为 Namespace 添加标识注解。

**实现** (`namespace_webhook.go:42-53`):
```go
func (m *NamespaceMutator) Default(_ context.Context, obj runtime.Object) error {
    i, ok := obj.(*corev1.Namespace)
    initAnnotationAndLabels(&i.ObjectMeta)

    // 添加 namespace 标识注解
    i.Annotations["sealos.io/namespace"] = i.Name
    return nil
}
```

**示例**:

创建前:
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: ns-user1
  annotations: {}
```

创建后:
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: ns-user1
  annotations:
    sealos.io/namespace: ns-user1  # 自动添加
```

#### 4.4.2 Namespace 验证

**目的**: 禁止用户 ServiceAccount 创建/更新/删除 Namespace。

**实现** (`namespace_webhook.go:94-101`):
```go
func (v *NamespaceValidator) validate(ctx context.Context, i *corev1.Namespace) error {
    request, _ := admission.RequestFromContext(ctx)

    // 检查是否是用户 ServiceAccount
    if isUserServiceAccount(request.UserInfo.Username) {
        return errors.New("user can not create/update/delete namespace")
    }

    return nil
}
```

**权限矩阵**:

| 操作 | 用户 SA | 系统 SA | 集群管理员 |
|------|---------|---------|-----------|
| 创建 Namespace | ❌ | ✅ | ✅ |
| 更新 Namespace | ❌ | ✅ | ✅ |
| 删除 Namespace | ❌ | ✅ | ✅ |

---

## 5. 实现细节

### 5.1 启动流程

```go
func main() {
    // 1. 解析命令行参数
    var metricsAddr string
    var probeAddr string
    var ingressAnnotationString string
    var domains v1.DomainList

    flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "")
    flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "")
    flag.StringVar(&ingressAnnotationString, "ingress-mutating-annotations", "", "")
    flag.Var(&domains, "domains", "Domains to be used for check ingress cname")

    // 2. 验证必需参数
    if len(domains) == 0 {
        setupLog.Error(nil, "domains is empty")
        os.Exit(1)
    }

    // 3. 解析注解配置
    ingressAnnotations := make(map[string]string)
    if ingressAnnotationString != "" {
        kvs := strings.Split(ingressAnnotationString, ",")
        for _, kv := range kvs {
            parts := strings.Split(kv, "=")
            if len(parts) == 2 {
                ingressAnnotations[parts[0]] = parts[1]
            }
        }
    }

    // 4. 创建 Controller Manager
    mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
        Scheme:                 scheme,
        MetricsBindAddress:     metricsAddr,
        Port:                   9443,
        HealthProbeBindAddress: probeAddr,
        LeaderElection:         enableLeaderElection,
        LeaderElectionID:       "849b6b0b.sealos.io",
    })

    // 5. 注册 Ingress Validator
    (&v1.IngressValidator{
        Domains: domains,
    }).SetupWithManager(mgr)

    // 6. 注册 Ingress Mutator
    (&v1.IngressMutator{
        IngressAnnotations: ingressAnnotations,
        Domains:            domains,
    }).SetupWithManager(mgr)

    // 7. 注册 Namespace Webhooks
    builder.WebhookManagedBy(mgr).
        For(&corev1.Namespace{}).
        WithValidator(&v1.NamespaceValidator{Client: mgr.GetClient()}).
        WithDefaulter(&v1.NamespaceMutator{Client: mgr.GetClient()}).
        Complete()

    // 8. 启动服务
    mgr.Start(ctrl.SetupSignalHandler())
}
```

### 5.2 Webhook 注册

**Ingress Validator**:
```go
//+kubebuilder:webhook:path=/validate-networking-k8s-io-v1-ingress,
//mutating=false,
//failurePolicy=ignore,
//sideEffects=None,
//groups=networking.k8s.io,
//resources=ingresses,
//verbs=create;update;delete,
//versions=v1,
//name=vingress.sealos.io,
//admissionReviewVersions=v1
```

**关键参数说明**:
- `mutating=false`: 验证型 Webhook（不修改资源）
- `failurePolicy=ignore`: Webhook 服务不可用时，允许请求继续
- `sideEffects=None`: Webhook 不会产生副作用
- `verbs=create;update;delete`: 拦截创建、更新、删除操作

### 5.3 上下文获取

```go
func (v *IngressValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
    // 从上下文中获取请求信息
    request, _ := admission.RequestFromContext(ctx)

    // 访问用户信息
    username := request.UserInfo.Username
    groups := request.UserInfo.Groups

    // 记录日志
    ilog.Info("validating",
        "user", username,
        "userGroups", groups,
        "namespace", i.Namespace,
        "name", i.Name)

    return v.validate(ctx, i)
}
```

### 5.4 错误处理

**统一错误格式**:
```go
const MessageFormat = "%d: %s"  // "错误码: 错误信息"

return fmt.Errorf(MessageFormat,
    code.IngressFailedCnameCheck,
    "can not verify ingress host " + rule.Host)
```

**错误码定义** (`pkg/code/code.go`):
```go
const (
    // 40xxx: 客户端错误（验证失败）
    IngressFailedCnameCheck = 40300  // CNAME 验证失败
    IngressFailedOwnerCheck = 40301  // 所有权验证失败
    IngressFailedIcpCheck   = 40302  // ICP 备案验证失败

    // 50xxx: 服务端错误（内部错误）
    IngressWebhookInternalError = 50000
)
```

### 5.5 性能优化

#### 5.5.1 索引优化

**问题**: 每次验证都需要查询所有 Ingress

```go
// 无索引 - 全表扫描
iList := &netv1.IngressList{}
v.Client.List(context.Background(), iList)  // 查询所有 Ingress
for _, item := range iList.Items {
    // 手动过滤 host
}
```

**解决方案**: 使用字段索引

```go
// 启动时创建索引
v.cache.IndexField(
    context.Background(),
    &netv1.Ingress{},
    IngressHostIndex,
    func(obj client.Object) []string {
        ingress := obj.(*netv1.Ingress)
        var hosts []string
        for _, rule := range ingress.Spec.Rules {
            hosts = append(hosts, rule.Host)
        }
        return hosts
    },
)

// 使用索引查询 - O(log n)
v.cache.List(context.Background(), iList,
    client.MatchingFields{IngressHostIndex: rule.Host})
```

#### 5.5.2 缓存优化

**ICP 查询缓存**:
```go
cache: cache.New(5*time.Minute, 3*time.Minute)

func genCacheTTL(rsp *IcpResponse) time.Duration {
    // 已备案: 30 天
    if rsp.ErrorCode == 0 && rsp.Result.SiteLicense != "" {
        return 30 * 24 * time.Hour
    }
    // 未备案: 5 分钟
    return 5 * time.Minute
}
```

**性能对比**:

| 场景 | 无缓存 | 有缓存 |
|------|--------|--------|
| 已备案域名查询 | 200ms | 1ms |
| 未备案域名查询 | 200ms | 1ms |
| 并发请求压力 | 高 | 极低 |

---

## 6. 部署配置

### 6.1 命令行参数

```bash
# 基础配置
--metrics-bind-address=:8080              # Metrics 服务端口
--health-probe-bind-address=:8081         # 健康检查端口
--leader-elect=false                      # 是否启用 Leader 选举

# 域名配置（必需）
--domains=sealos.io,example.com           # 系统域名列表，逗号分隔

# Ingress 注解配置（可选）
--ingress-mutating-annotations=           # 自动添加的注解
  kubernetes.io/ingress.class=nginx,      # 多个注解用逗号分隔
  nginx.ingress.kubernetes.io/proxy-body-size=100m
```

### 6.2 环境变量

```bash
# ICP 备案验证配置
ICP_ENABLED=true                          # 是否启用 ICP 验证
ICP_ENDPOINT=http://icp-api.example.com   # ICP 查询 API 地址
ICP_KEY=your-api-key                      # API 密钥
```

### 6.3 RBAC 权限

```yaml
#+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
#+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
```

**所需权限**:
- `ingresses`: get, list, watch, create, update, patch, delete
- `namespaces`: get, list, watch, create, update, patch, delete

### 6.4 Webhook 配置示例

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: ingress-validator
webhooks:
- name: vingress.sealos.io
  rules:
  - operations: ["CREATE", "UPDATE", "DELETE"]
    apiGroups: ["networking.k8s.io"]
    apiVersions: ["v1"]
    resources: ["ingresses"]
  failurePolicy: Ignore
  sideEffects: None
  admissionReviewVersions: ["v1"]
  clientConfig:
    service:
      namespace: sealos-system
      name: webhook-service
      path: /validate-networking-k8s-io-v1-ingress
```

---

## 7. 错误码说明

### 7.1 错误响应格式

```json
{
  "kind": "AdmissionReview",
  "apiVersion": "admission.k8s.io/v1",
  "response": {
    "uid": "xxxxx",
    "allowed": false,
    "result": {
      "message": "40301: ingress host example.com is owned by other user, you can not create ingress with same host."
    }
  }
}
```

### 7.2 错误码列表

| 错误码 | 名称 | 说明 | 用户操作 |
|-------|------|------|----------|
| 40001 | InsufficientBalance | 余额不足 | 充值账户 |
| 40300 | IngressFailedCnameCheck | CNAME 验证失败 | 配置正确的 CNAME 记录指向系统域名 |
| 40301 | IngressFailedOwnerCheck | 域名已被占用 | 使用其他域名或联系域名所有者 |
| 40302 | IngressFailedIcpCheck | ICP 备案验证失败 | 完成域名备案 |
| 50000 | IngressWebhookInternalError | Webhook 内部错误 | 联系系统管理员 |

### 7.3 常见错误场景

#### 错误 1: CNAME 验证失败

**错误信息**:
```
40300: can not verify ingress host myapp.com, cname is not end with any domains in sealos.io,example.com
```

**原因**:
- 域名的 CNAME 记录未配置
- CNAME 未指向系统域名

**解决方案**:
```bash
# 1. 检查当前 CNAME 记录
dig CNAME myapp.com

# 2. 添加正确的 CNAME 记录
# 在 DNS 服务商处配置:
# myapp.com CNAME app.sealos.io

# 3. 验证配置
dig CNAME myapp.com
# 应返回: app.sealos.io
```

#### 错误 2: 域名已被占用

**错误信息**:
```
40301: ingress host myapp.com is owned by ns-user1, you can not create ingress with same host.
```

**原因**: 该域名已被其他 Namespace 的 Ingress 使用

**解决方案**:
1. 使用其他域名
2. 联系域名所有者协商
3. 等待原 Ingress 被删除

#### 错误 3: ICP 备案验证失败

**错误信息**:
```
40302: icp query result is empty
```

**原因**: 域名未完成 ICP 备案

**解决方案**:
1. 在工信部备案系统完成备案
2. 等待备案信息生效（通常 1-3 天）
3. 重新创建 Ingress

---

## 8. 总结

### 8.1 核心价值

1. **安全性**: 防止域名冒充和滥用
2. **可控性**: 统一管理域名资源
3. **合规性**: 满足监管要求（ICP 备案）
4. **性能**: 通过索引和缓存优化响应速度

### 8.2 关键机制

| 机制 | 实现方式 | 目标 |
|------|----------|------|
| 用户识别 | Namespace/SA 前缀 | 区分用户和系统资源 |
| 域名验证 | DNS CNAME 查询 | 验证域名控制权 |
| 所有权保护 | Ingress 索引查询 | 防止域名冲突 |
| 自动注解 | Mutating Webhook | 统一资源配置 |
| 权限控制 | Validating Webhook | 限制用户操作 |

### 8.3 最佳实践

1. **域名管理**:
   - 使用系统提供的域名或配置正确的 CNAME
   - 避免使用通用域名

2. **多租户隔离**:
   - 严格区分用户 Namespace (ns-*) 和系统 Namespace
   - 使用 ServiceAccount 进行身份识别

3. **性能优化**:
   - 启用 Ingress 索引
   - 合理配置缓存时间

4. **监控告警**:
   - 监控 Webhook 响应时间
   - 收集验证失败日志
   - 设置错误率告警

---

## 附录

### A. 相关文档

- [Kubernetes Admission Webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
- [controller-runtim 文档](https://book.kubebuilder.io/reference/webhooks.html)
- [Sealos 官方文档](https://sealos.io/docs/)

### B. 代码位置

| 功能 | 文件路径 |
|------|----------|
| 主程序入口 | `admission/cmd/main.go` |
| Ingress Webhook | `admission/api/v1/ingress_webhook.go` |
| Namespace Webhook | `admission/api/v1/namespace_webhook.go` |
| ICP 验证 | `admission/api/v1/icp.go` |
| 工具函数 | `admission/api/v1/utils.go` |
| 类型定义 | `admission/api/v1/types.go` |
| 错误码 | `admission/pkg/code/code.go` |

### C. 更新日志

| 版本 | 日期 | 更新内容 |
|------|------|----------|
| v1.0 | 2025-01-15 | 初始版本，完整文档 |
