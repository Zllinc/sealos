# Sealos Admission Webhook 使用示例

> 配合 README.md 使用的示例文档

---

## 1. 典型使用场景

### 场景 1: 用户使用系统提供的子域名

**需求**: 用户想在 `ns-user1` 中部署应用，使用 `myapp.sealos.io` 访问

**操作**:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  namespace: ns-user1
spec:
  rules:
  - host: myapp.sealos.io
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

**验证流程**:
1. ✅ 用户 SA (`system:serviceaccount:ns-user1:*`)
2. ✅ 用户 Namespace (`ns-user1`)
3. ✅ 域名以 `sealos.io` 结尾（系统域名）
4. ✅ 无其他 NS 使用该域名
5. **结果**: 创建成功

---

### 场景 2: 用户使用自定义域名 + CNAME

**需求**: 用户拥有 `example.com`，想通过 `app.example.com` 访问

**前提条件**: 已配置 CNAME 记录
```
app.example.com CNAME myapp.sealos.io
```

**操作**:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  namespace: ns-user1
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

**验证流程**:
1. ✅ 用户 SA 和 Namespace
2. ✅ DNS 查询: `app.example.com` → `myapp.sealos.io`
3. ✅ CNAME 以 `sealos.io` 结尾
4. ✅ 无域名冲突
5. **结果**: 创建成功

---

### 场景 3: 域名冲突

**需求**: 两个用户都想使用 `app.example.com`

**用户 A 的操作**:
```yaml
# 先创建 - 成功
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
spec:
  rules:
  - host: app.example.com
    # ...
```

**用户 B 的操作**:
```yaml
# 后创建 - 失败！
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user2
spec:
  rules:
  - host: app.example.com  # ❌ 被 ns-user1 占用
    # ...
```

**错误信息**:
```
Error from server (InternalError): admission webhook denied the request:
40301: ingress host app.example.com is owned by ns-user1, you can not create ingress with same host.
```

**解决方案**:
1. 用户 B 使用其他域名: `app2.example.com`
2. 用户 B 与用户 A 协商，等待其删除 Ingress

---

### 场景 4: CNAME 未配置

**操作**:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  namespace: ns-user1
spec:
  rules:
  - host: app.example.com  # 未配置 CNAME
    # ...
```

**验证流程**:
1. ✅ 用户 SA 和 Namespace
2. ❌ DNS 查询: `app.example.com` → 查询失败或无 CNAME
3. ❌ CNAME 不以系统域名结尾
4. **结果**: 创建失败

**错误信息**:
```
Error from server (InternalError): admission webhook denied the request:
40300: can not verify ingress host app.example.com, cname is not end with any domains in sealos.io
```

**解决方案**:
```bash
# 在 DNS 服务商处添加 CNAME 记录
# 记录类型: CNAME
# 主机记录: app
# 记录值: myapp.sealos.io
```

---

### 场景 5: ICP 备案验证（中国场景）

**配置**:
```bash
export ICP_ENABLED=true
export ICP_ENDPOINT=http://icp-api.example.com/query
export ICP_KEY=your-api-key
```

**已备案域名**:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
spec:
  rules:
  - host: example.com  # ✅ 已完成 ICP 备案
    # ...
```

**ICP API 响应**:
```json
{
  "error_code": 0,
  "result": {
    "SiteLicense": "京ICP备12345678号",
    "CompanyName": "示例公司",
    "SiteName": "example.com"
  }
}
```

**结果**: 创建成功

---

**未备案域名**:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
spec:
  rules:
  - host: new-example.com  # ❌ 未备案
    # ...
```

**ICP API 响应**:
```json
{
  "error_code": 0,
  "result": {
    "SiteLicense": ""  // 空值表示未备案
  }
}
```

**错误信息**:
```
Error from server (InternalError): admission webhook denied the request:
40302: icp query result is empty
```

---

## 2. 自动注解示例

**配置参数**:
```bash
--ingress-mutating-annotations=nginx.ingress.kubernetes.io/proxy-body-size=100m,nginx.ingress.kubernetes.io/proxy-connect-timeout=60
```

**创建前**:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
  annotations:
    user.custom.annotation: custom-value
spec:
  rules:
  - host: myapp.sealos.io
```

**创建后（Webhook 自动修改）**:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp
  namespace: ns-user1
  annotations:
    sealos.io/namespace: ns-user1              # 自动添加
    user.custom.annotation: custom-value       # 保留
    nginx.ingress.kubernetes.io/proxy-body-size: 100m  # 自动添加
    nginx.ingress.kubernetes.io/proxy-connect-timeout: "60"  # 自动添加
spec:
  rules:
  - host: myapp.sealos.io
```

---

## 3. Namespace 创建示例

### 3.1 用户尝试创建 Namespace

**操作**:
```bash
kubectl create namespace ns-user1
--as=system:serviceaccount:ns-user1:default
```

**结果**: 失败
```
Error from server (InternalError): admission webhook denied the request:
user can not create/update/delete namespace
```

**原因**: 用户 ServiceAccount 无权限创建 Namespace

### 3.2 管理员创建 Namespace

**操作**:
```bash
kubectl create namespace ns-user1
```

**创建前**:
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: ns-user1
```

**创建后（Webhook 自动修改）**:
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: ns-user1
  annotations:
    sealos.io/namespace: ns-user1  # 自动添加
```

---

## 4. 系统资源跳过验证

### 4.1 系统 Namespace 中的 Ingress

**操作**:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: system-ingress
  namespace: kube-system  # 系统 Namespace
spec:
  rules:
  - host: arbitrary-domain.com  # 任意域名
```

**结果**: 创建成功（跳过验证）

**原因**: `kube-system` 不是用户 Namespace（不以 `ns-` 开头）

### 4.2 系统账号创建的 Ingress

**操作**:
```bash
kubectl apply -f ingress.yaml --as=system:admin
```

**结果**: 创建成功（跳过验证）

**原因**: `system:admin` 不是用户 ServiceAccount（不以 `system:serviceaccount:ns-` 开头）

---

## 5. 部署配置示例

### 5.1 Deployment 配置

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: admission-webhook
  namespace: sealos-system
spec:
  replicas: 2
  selector:
    matchLabels:
      app: admission-webhook
  template:
    metadata:
      labels:
        app: admission-webhook
    spec:
      containers:
      - name: webhook
        image: sealos/admission-webhook:latest
        args:
        - --domains=sealos.io,example.com
        - --ingress-mutating-annotations=nginx.ingress.kubernetes.io/proxy-body-size=100m
        - --leader-elect=true
        env:
        - name: ICP_ENABLED
          value: "true"
        - name: ICP_ENDPOINT
          value: "http://icp-api.example.com/query"
        - name: ICP_KEY
          valueFrom:
            secretKeyRef:
              name: icp-secret
              key: api-key
        ports:
        - containerPort: 9443
          name: webhook
          protocol: TCP
        - containerPort: 8080
          name: metrics
          protocol: TCP
        - containerPort: 8081
          name: healthz
          protocol: TCP
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8081
          initialDelaySeconds: 15
          periodSeconds: 20
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8081
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
```

### 5.2 Service 配置

```yaml
apiVersion: v1
kind: Service
metadata:
  name: webhook-service
  namespace: sealos-system
spec:
  ports:
  - port: 443
    targetPort: 9443
    name: webhook
  selector:
    app: admission-webhook
```

### 5.3 Secret 配置（ICP API Key）

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: icp-secret
  namespace: sealos-system
type: Opaque
data:
  api-key: eW91ci1hcGkta2V5LWJhc2U2NC1lbmNvZGVk
```

### 5.4 ValidatingWebhookConfiguration 配置

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: ingress-validator
webhooks:
- name: vingress.sealos.io
  rules:
  - operations: ["CREATE", "UPDATE"]
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

### 5.5 MutatingWebhookConfiguration 配置

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: ingress-mutator
webhooks:
- name: mingress.sealos.io
  rules:
  - operations: ["CREATE", "UPDATE"]
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
      path: /mutate-networking-k8s-io-v1-ingress
```

---

## 6. 调试技巧

### 6.1 查看 Webhook 日志

```bash
# 查看 Pod 列表
kubectl get pods -n sealos-system -l app=admission-webhook

# 查看日志
kubectl logs -n sealos-system <pod-name> --tail=100 -f

# 搜索特定 Ingress 的日志
kubectl logs -n sealos-system <pod-name> | grep "ingress name=myapp"
```

### 6.2 测试 DNS 解析

```bash
# 测试 CNAME 查询
dig CNAME app.example.com +short

# 使用 nslookup
nslookup -type=CNAME app.example.com

# 使用 Go 代码测试（与 Webhook 相同的逻辑）
go run -e 'package main; import ("net"; "fmt"; func main() { cname, _ := net.LookupCNAME("app.example.com"); fmt.Println(cname) }'
```

### 6.3 临时禁用 Webhook

**方法 1: 删除 Webhook 配置**
```bash
kubectl delete validatingwebhookconfiguration ingress-validator
kubectl delete mutatingwebhookconfiguration ingress-mutator
```

**方法 2: 修改 failurePolicy**
```yaml
# 将 failurePolicy 从 Fail 改为 Ignore
# 这样 Webhook 故障时不会阻止请求
```

### 6.4 测试 Webhook 响应

```bash
# 创建测试 Ingress
cat <<EOF | kubectl apply -f -
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: test-ingress
  namespace: ns-test
spec:
  rules:
  - host: test.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: test-service
            port:
              number: 80
EOF

# 查看详细错误
kubectl describe ingress test-ingress -n ns-test
```

---

## 7. 性能测试

### 7.1 压力测试脚本

```bash
#!/bin/bash

# 并发创建 100 个 Ingress
for i in {1..100}; do
  cat <<EOF | kubectl apply -f -
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: test-ingress-$i
  namespace: ns-test
spec:
  rules:
  - host: test-$i.sealos.io
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: test-service
            port:
              number: 80
EOF
done &

# 等待完成
wait

# 检查结果
kubectl get ingress -n ns-test | wc -l
```

### 7.2 监控指标

```bash
# 查看 Webhook 响应时间
kubectl port-forward -n sealos-system <pod-name> 8080:8080
curl http://localhost:8080/metrics

# 关键指标
# - controller_runtime_webhook_requests_total
# - controller_runtime_webhook_latency_seconds
```

---

## 8. 故障排查

### 问题 1: Webhook 无响应

**现象**:
```
Error from server (InternalError): failed calling webhook "vingress.sealos.io":
Post "https://webhook-service.sealos.svc:443/validate-networking-k8s-io-v1-ingress":
dial tcp: lookup webhook-service.sealos.svc: no such host
```

**原因**: Service 不存在或网络问题

**解决方案**:
```bash
# 检查 Service
kubectl get svc -n sealos-system webhook-service

# 检查 Pod
kubectl get pods -n sealos-system -l app=admission-webhook

# 检查网络连接
kubectl run -it --rm debug --image=nicolaka/netshoot --restart=Never -- \
  curl webhook-service.sealos-system.svc:443
```

### 问题 2: 所有请求都被拒绝

**现象**: 所有 Ingress 创建请求都失败

**可能原因**:
1. 域名配置错误
2. DNS 服务不可用
3. Webhook 配置错误

**排查步骤**:
```bash
# 1. 检查域名配置
kubectl get deployment admission-webhook -n sealos-system -o jsonpath='{.spec.template.spec.containers[0].args}'

# 2. 检查 DNS
dig sealos.io

# 3. 查看 Webhook 日志
kubectl logs -n sealos-system <pod-name> --tail=50
```

### 问题 3: 性能问题

**现象**: Ingress 创建速度慢

**解决方案**:
1. 增加副本数
2. 启用索引
3. 调整缓存时间
4. 检查 DNS 查询延迟

```bash
# 水平扩展
kubectl scale deployment admission-webhook -n sealos-system --replicas=5

# 检查索引
kubectl logs -n sealos-system <pod-name> | grep "IndexField"
```

---

## 9. 升级指南

### 从 v1.0 升级到 v2.0

**步骤**:
1. 备份现有配置
2. 更新镜像
3. 验证功能

```bash
# 1. 备份
kubectl get validatingwebhookconfiguration ingress-validator -o yaml > backup.yaml
kubectl get mutatingwebhookconfiguration ingress-mutator -o yaml >> backup.yaml

# 2. 更新
kubectl set image deployment/admission-webhook webhook=sealos/admission-webhook:v2.0 -n sealos-system

# 3. 验证
kubectl rollout status deployment/admission-webhook -n sealos-system

# 4. 测试
kubectl apply -f test-ingress.yaml
```

---

## 10. 常见问题 FAQ

**Q1: 如何添加新的系统域名？**
```bash
# 修改 Deployment 的 args
kubectl set env deployment/admission-webhook -n sealos-system -- \
  DOMAINS=sealos.io,example.com,newdomain.com
```

**Q2: 如何禁用 ICP 验证？**
```bash
kubectl set env deployment/admission-webhook -n sealos-system ICP_ENABLED=false
```

**Q3: 如何查看当前配置？**
```bash
kubectl get deployment admission-webhook -n sealos-system -o jsonpath='{.spec.template.spec.containers[0].args}' | jq
kubectl get deployment admission-webhook -n sealos-system -o jsonpath='{.spec.template.spec.containers[0].env}' | jq
```

**Q4: Webhook 是否影响已有 Ingress？**
- 否，Webhook 只拦截 CREATE/UPDATE/DELETE 操作
- 已存在的 Ingress 不受影响

**Q5: 如何临时绕过 Webhook？**
```bash
# 方法 1: 使用系统账号
kubectl apply -f ingress.yaml --as=system:admin

# 方法 2: 在系统 Namespace 中创建
kubectl apply -f ingress.yaml -n kube-system

# 方法 3: 删除 Webhook 配置（不推荐）
kubectl delete validatingwebhookconfiguration ingress-validator
```

---

## 11. 监控和告警

### 11.1 Prometheus 监控

```yaml
# ServiceMonitor
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: admission-webhook
  namespace: sealos-system
spec:
  selector:
    matchLabels:
      app: admission-webhook
  endpoints:
  - port: metrics
    interval: 30s
```

### 11.2 告警规则

```yaml
# PrometheusRule
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: admission-webhook-alerts
  namespace: sealos-system
spec:
  groups:
  - name: admission-webhook
    rules:
    - alert: WebhookHighErrorRate
      expr: rate(controller_runtime_webhook_requests_total{result="error"}[5m]) > 0.1
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "Webhook error rate is high"

    - alert: WebhookLatencyHigh
      expr: histogram_quantile(0.99, rate(controller_runtime_webhook_latency_seconds_bucket[5m])) > 1
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "Webhook latency is high"
```

---

## 12. 最佳实践总结

1. **域名管理**:
   - 优先使用系统提供的子域名
   - 自定义域名必须配置正确的 CNAME

2. **性能优化**:
   - 启用 Ingress 索引
   - 合理配置缓存时间
   - 水平扩展 Pod 副本数

3. **安全配置**:
   - 使用 RBAC 限制访问
   - 定期轮换 ICP API Key
   - 启用审计日志

4. **监控告警**:
   - 监控 Webhook 响应时间
   - 监控错误率
   - 设置合理的告警阈值

5. **故障恢复**:
   - 配置适当的 failurePolicy
   - 准备回滚方案
   - 定期备份配置
