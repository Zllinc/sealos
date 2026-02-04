# Sealos Admission Webhook 快速参考

> 快速查阅手册

---

## 核心概念速查

### 用户识别规则

| 资源类型 | 用户格式 | 示例 |
|---------|---------|------|
| Namespace | `ns-*` | `ns-user1`, `ns-app123` |
| ServiceAccount | `system:serviceaccount:ns-*` | `system:serviceaccount:ns-user1:default` |

### 验证规则矩阵

| 资源 | 操作 | 用户 SA | 系统 SA | 用户 NS | 系统 NS |
|------|------|---------|---------|---------|---------|
| Ingress | CREATE | ✅ 验证 | ❌ 跳过 | ✅ 验证 | ❌ 跳过 |
| Ingress | UPDATE | ✅ 验证 | ❌ 跳过 | ✅ 验证 | ❌ 跳过 |
| Ingress | DELETE | ❌ 跳过 | ❌ 跳过 | ❌ 跳过 | ❌ 跳过 |
| Namespace | CREATE | ❌ 拒绝 | ✅ 允许 | - | - |
| Namespace | UPDATE | ❌ 拒绝 | ✅ 允许 | - | - |
| Namespace | DELETE | ❌ 拒绝 | ✅ 允许 | - | - |

---

## 域名验证流程

```
┌────────────────────────────────────────────────────────┐
│ 用户创建 Ingress (host: example.com)                  │
└────────────────────────────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────┐
│ 1. 身份验证                                           │
│    - 是否是用户 SA?    → 否: 跳过验证                 │
│    - 是否是用户 NS?    → 否: 跳过验证                 │
└────────────────────────────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────┐
│ 2. CNAME 验证                                        │
│    - 查询 DNS CNAME 记录                              │
│    - host 是否以系统域名结尾?                         │
│      → 是: 通过                                       │
│    - CNAME 是否以系统域名结尾?                        │
│      → 是: 通过                                       │
│      → 否: 拒绝 (40300)                               │
└────────────────────────────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────┐
│ 3. 所有权验证                                        │
│    - 查询是否有其他 NS 使用该 host                    │
│    - 无冲突: 通过                                     │
│    - 有冲突: 拒绝 (40301)                             │
└────────────────────────────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────┐
│ 4. ICP 验证（可选）                                  │
│    - 未启用: 跳过                                     │
│    - 已备案: 通过                                     │
│    - 未备案: 拒绝 (40302)                             │
└────────────────────────────────────────────────────────┘
                         │
                         ▼
                     创建成功
```

---

## 错误码速查表

| 错误码 | 名称 | 原因 | 解决方案 |
|-------|------|------|----------|
| 40300 | CNAME 验证失败 | CNAME 未指向系统域名 | 配置正确的 CNAME |
| 40301 | 所有权冲突 | 域名已被其他 NS 使用 | 使用其他域名 |
| 40302 | ICP 备案失败 | 域名未备案 | 完成域名备案 |
| 50000 | 内部错误 | Webhook 故障 | 联系管理员 |

---

## 配置参数速查

### 命令行参数

```bash
# 必需参数
--domains=<domain1>,<domain2>     # 系统域名列表

# 可选参数
--ingress-mutating-annotations=<key1>=<value1>,<key2>=<value2>  # 自动注解
--metrics-bind-address=:8080       # Metrics 端口
--health-probe-bind-address=:8081  # 健康检查端口
--leader-elect=false               # Leader 选举
```

### 环境变量

```bash
# ICP 备案验证
ICP_ENABLED=true                    # 启用 ICP 验证
ICP_ENDPOINT=<url>                  # ICP API 地址
ICP_KEY=<key>                       # API 密钥
```

---

## Webhook 端点

| 类型 | 路径 | 功能 |
|------|------|------|
| Validating | `/validate-networking-k8s-io-v1-ingress` | 验证 Ingress |
| Mutating | `/mutate-networking-k8s-io-v1-ingress` | 修改 Ingress |
| Validating | `/validate--v1-namespace` | 验证 Namespace |
| Mutating | `/mutate--v1-namespace` | 修改 Namespace |

---

## 常用命令

### 部署

```bash
# 部署 Webhook
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
kubectl apply -f webhook-configuration.yaml

# 验证部署
kubectl get pods -n sealos-system -l app=admission-webhook
kubectl get svc -n sealos-system webhook-service
```

### 调试

```bash
# 查看日志
kubectl logs -n sealos-system -l app=admission-webhook --tail=100 -f

# 查看 Webhook 配置
kubectl get validatingwebhookconfiguration
kubectl get mutatingwebhookconfiguration

# 测试 DNS 解析
dig CNAME example.com
```

### 配置管理

```bash
# 更新镜像
kubectl set image deployment/admission-webhook webhook=sealos/admission-webhook:v1.0 -n sealos-system

# 扩容
kubectl scale deployment/admission-webhook --replicas=3 -n sealos-system

# 查看配置
kubectl get deployment admission-webhook -n sealos-system -o jsonpath='{.spec.template.spec.containers[0].args}' | jq
```

### 故障排查

```bash
# 检查 Pod 状态
kubectl describe pod -n sealos-system -l app=admission-webhook

# 查看 Webhook 调用日志
kubectl logs -n sealos-system <pod-name> | grep "validating"

# 测试 Webhook 连通性
kubectl run -it --rm debug --image=nicolaka/netshoot --restart=Never -- \
  curl https://webhook-service.sealos-system.svc:443/healthz
```

---

## 文件位置速查

| 功能 | 文件路径 |
|------|----------|
| 主程序 | `admission/cmd/main.go` |
| Ingress 处理 | `admission/api/v1/ingress_webhook.go` |
| Namespace 处理 | `admission/api/v1/namespace_webhook.go` |
| ICP 验证 | `admission/api/v1/icp.go` |
| 工具函数 | `admission/api/v1/utils.go` |
| 错误码 | `admission/pkg/code/code.go` |

---

## 关键代码位置

### 用户识别
```go
// utils.go:31-37
func isUserServiceAccount(sa string) bool
func isUserNamespace(ns string) bool
```

### CNAME 验证
```go
// ingress_webhook.go:203-227
func (v *IngressValidator) checkCname(i *netv1.Ingress, rule *netv1.IngressRule) error
```

### 所有权验证
```go
// ingress_webhook.go:229-245
func (v *IngressValidator) checkOwner(i *netv1.Ingress, rule *netv1.IngressRule) error
```

### ICP 验证
```go
// ingress_webhook.go:247-270
func (v *IngressValidator) checkIcp(i *netv1.Ingress, rule *netv1.IngressRule) error

// icp.go:58-94
func (i *IcpValidator) Query(rule *netv1.IngressRule) (*IcpResponse, error)
```

---

## 典型场景处理

### 场景 1: 使用系统子域名

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
spec:
  rules:
  - host: myapp.sealos.io  # ✅ 系统域名
```

**结果**: 通过

---

### 场景 2: 使用自定义域名 + CNAME

```yaml
# DNS 配置: app.example.com CNAME myapp.sealos.io
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
spec:
  rules:
  - host: app.example.com  # ✅ CNAME 指向系统域名
```

**结果**: 通过

---

### 场景 3: 域名冲突

```yaml
# ns-user1 中已创建 host: app.example.com
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user2
spec:
  rules:
  - host: app.example.com  # ❌ 被 ns-user1 占用
```

**结果**: 拒绝 (40301)

---

### 场景 4: CNAME 未配置

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
spec:
  rules:
  - host: app.example.com  # ❌ 无 CNAME 或 CNAME 不正确
```

**结果**: 拒绝 (40300)

---

## 性能指标

| 指标 | 正常值 | 告警阈值 |
|------|--------|----------|
| Webhook 响应时间 | < 100ms | > 500ms |
| 错误率 | < 1% | > 5% |
| DNS 查询时间 | < 50ms | > 200ms |
| ICP 查询时间 | < 500ms | > 2000ms |

---

## 监控指标

### Prometheus 指标

```bash
# 请求总数
controller_runtime_webhook_requests_total

# 请求延迟
controller_runtime_webhook_latency_seconds

# 重新入队次数
controller_runtime_reconcile_total

# Worker 队列长度
controller_runtime_worker_queue_length
```

### 关键告警规则

```yaml
# 高错误率
rate(controller_runtime_webhook_requests_total{result="error"}[5m]) > 0.1

# 高延迟
histogram_quantile(0.99, rate(controller_runtime_webhook_latency_seconds_bucket[5m])) > 1
```

---

## 升级检查清单

- [ ] 备份现有配置
- [ ] 测试新版本镜像
- [ ] 准备回滚方案
- [ ] 更新镜像
- [ ] 验证功能
- [ ] 监控告警
- [ ] 更新文档

---

## 应急处理

### Webhook 故障导致无法创建资源

```bash
# 临时禁用 Webhook
kubectl delete validatingwebhookconfiguration ingress-validator
kubectl delete mutatingwebhookconfiguration ingress-mutator

# 或修改 failurePolicy
kubectl patch validatingwebhookconfiguration ingress-validator -p '{"webhooks":[{"failurePolicy":"Ignore"}]}'
```

### 所有请求被拒绝

```bash
# 检查日志
kubectl logs -n sealos-system -l app=admission-webhook --tail=50

# 检查配置
kubectl get deployment admission-webhook -n sealos-system -o yaml | grep -A 5 "domains"

# 测试 DNS
dig sealos.io
```

### 性能下降

```bash
# 扩容
kubectl scale deployment admission-webhook --replicas=5 -n sealos-system

# 检查 DNS 延迟
time dig CNAME test.example.com

# 查看索引状态
kubectl logs -n sealos-system -l app=admission-webhook | grep "IndexField"
```

---

## 相关资源

- [Kubernetes Admission Webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
- [controller-runtime Webhook](https://book.kubebuilder.io/reference/webhooks.html)
- [Sealos 官方文档](https://sealos.io/docs/)

---

**快速导航**:
- [详细文档](./README.md)
- [使用示例](./examples.md)
- [本文档](./quick-reference.md)
