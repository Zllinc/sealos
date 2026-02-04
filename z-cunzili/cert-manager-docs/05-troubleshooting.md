# cert-manager 故障排查

> 常见问题诊断和解决方案

**作者**: cunzili
**版本**: v1.0
**更新日期**: 2025-01-15

---

## 📋 目录

- [1. 诊断流程](#1-诊断流程)
- [2. 问题 1: 证书签发失败（Ready=False）](#2-问题-1-证书签发失败readyfalse)
- [3. 问题 2: HTTP-01 验证失败](#3-问题-2-http-01-验证失败)
- [4. 问题 3: DNS-01 验证失败](#4-问题-3-dns-01-验证失败)
- [5. 问题 4: 证书无法自动续期](#5-问题-4-证书无法自动续期)
- [6. 问题 5: Ingress 无法访问](#6-问题-5-ingress-无法访问)
- [7. 日志查看技巧](#7-日志查看技巧)
- [8. 监控和告警](#8-监控和告警)

---

## 1. 诊断流程

### 1.1 通用诊断流程图

```
证书问题
    │
    ▼
1. 检查 Certificate 状态
kubectl get certificate
    │
    ├─→ Ready=False → 查看详情（问题 2/3）
    │
    ▼ Ready=True
2. 检查 Secret
kubectl get secret
    │
    ├─→ Secret 不存在 → 检查 cert-manager 日志
    │
    ▼ Secret 存在
3. 检查 Ingress 配置
kubectl get ingress
    │
    ├─→ Secret 引用错误 → 修改 Ingress
    │
    ▼ 配置正确
4. 测试 HTTPS 访问
curl -I https://domain/
    │
    ├─→ 无法访问 → 检查网络/DNS
    │
    ▼ 访问正常
5. 检查证书过期时间
openssl s_client -connect domain:443
    │
    ├─→ 即将过期（< 30 天）→ 检查续期（问题 4）
    │
    ▼ 证书有效
✅ 无问题
```

### 1.2 快速诊断脚本

```bash
#!/bin/bash

echo "=== cert-manager 诊断 ==="

NAMESPACE="default"
CERT_NAME="myapp-tls"

# 1. 检查 cert-manager Pod
echo -e "\n1. cert-manager Pods"
kubectl get pods -n cert-manager

# 2. 检查 Certificate
echo -e "\n2. Certificate 状态"
kubectl get certificate $CERT_NAME -n $NAMESPACE

# 3. 检查 Secret
echo -e "\n3. Secret 状态"
kubectl get secret ${CERT_NAME}-cert -n $NAMESPACE

# 4. 检查 CertificateRequest
echo -e "\n4. CertificateRequests"
kubectl get certificaterequest -n $NAMESPACE

# 5. 检查 Ingress
echo -e "\n5. Ingress 状态"
kubectl get ingress -n $NAMESPACE

# 6. 测试 HTTPS 访问
echo -e "\n6. HTTPS 测试"
DOMAIN="www.example.com"
curl -I https://$DOMAIN/ 2>/dev/null | head -1

# 7. 检查证书过期时间
echo -e "\n7. 证书过期时间"
kubectl get secret ${CERT_NAME}-cert -n $NAMESPACE -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -dates

echo -e "\n=== 诊断完成 ==="
```

---

## 2. 问题 1: 证书签发失败（Ready=False）

### 2.1 症状

```bash
kubectl get certificate myapp-tls

# 输出:
# NAME        READY   SECRET           AGE
# myapp-tls   False   myapp-tls-cert   10m
#              ↑
#          Ready = False
```

### 2.2 诊断步骤

```bash
# 1. 查看 Certificate 详情
kubectl describe certificate myapp-tls

# 输出:
# Status:
#   Conditions:
#     Type:    Ready
#     Status:  False
#     Message: Failed to wait for certificate order...

# 2. 查看 CertificateRequest
kubectl get certificaterequest

# 输出:
# NAME                         READY   AGE
# myapp-tls-1                  False   10m

# 3. 查看 CertificateRequest 详情
kubectl describe certificaterequest myapp-tls-1

# 输出:
# Status:
#   Conditions:
#     Type:    Ready
#     Status:  False
#     Message: Failed to authorize challenge...
```

### 2.3 常见原因和解决

#### 原因 1: ClusterIssuer 不存在

```bash
# 检查 ClusterIssuer
kubectl get clusterissuer

# 输出:
# No resources found

# 解决: 创建 ClusterIssuer
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
```

#### 原因 2: 域名未解析

```bash
# 检查域名解析
dig +short www.example.com

# 输出: 空（未解析）

# 解决: 配置 DNS
# 在域名服务商控制台添加 A 记录
# www A 47.96.123.45

# 等待 DNS 生效（几分钟到几小时）
```

#### 原因 3: Ingress Controller 未运行

```bash
# 检查 Ingress Controller
kubectl get pods -n ingress-nginx

# 输出: No resources found

# 解决: 安装 Ingress Controller
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.9.4/deploy/static/provider/cloud/deploy.yaml
```

#### 原因 4: 防火墙阻止 80 端口

```bash
# 测试 80 端口
telnet www.example.com 80

# 输出: Connection timed out

# 解决: 开放防火墙
# Ubuntu/Debian
sudo ufw allow 80/tcp

# CentOS/RHEL
sudo iptables -I INPUT -p tcp --dport 80 -j ACCEPT
```

#### 原因 5: Let's Encrypt 速率限制

```bash
# 查看 CertificateRequest 错误
kubectl describe certificaterequest myapp-tls-1 | grep Error

# 输出:
# Error: ... too many certificates already issued for: www.example.com

# 解决:
# 1. 等待一周（速率限制重置）
# 2. 或使用暂存环境测试（无限制）
# 3. 或删除未使用的证书
```

---

## 3. 问题 2: HTTP-01 验证失败

### 3.1 症状

```bash
kubectl describe certificaterequest myapp-tls-1

# 输出:
# Status:
#   Conditions:
#     Type:    Ready
#     Status:  False
#     Message: Failed to perform HTTP-01 challenge...
```

### 3.2 诊断步骤

```bash
# 1. 查看 Challenge 资源
kubectl get challenge

# 输出:
# NAME                               STATE      DOMAIN              AGE
# myapp-tls-1-xxx-xxx                 pending    www.example.com    5m

# 2. 查看 Challenge 详情
kubectl describe challenge myapp-tls-1-xxx-xxx

# 输出:
# Status:
#   Processing: true
#   Presented: true
#   Reason: 'Waiting for HTTP-01 challenge propagation...'
```

### 3.3 常见原因和解决

#### 原因 1: 临时 Ingress 失败

```bash
# 查看临时 Ingress
kubectl get ingress | grep acme-http-solver

# 输出: 空（临时 Ingress 未创建）

# 解决: 检查 cert-manager 日志
kubectl logs -n cert-manager deployment/cert-manager

# 常见错误:
# - ingress class not found
# - failed to create ingress

# 修复: 确保 Ingress Class 正确
kubectl get ingressclass

# 输出:
# NAME    CONTROLLER             PARAMETERS
# nginx   k8s.io/ingress-nginx   <none>
```

#### 原因 2: 域名解析到错误的 IP

```bash
# 检查域名解析
dig +short www.example.com

# 输出:
# 1.2.3.4  # 错误的 IP

# 检查 LoadBalancer IP
kubectl get svc -n ingress-nginx ingress-nginx-controller

# 输出:
# EXTERNAL-IP
# 47.96.123.45  # 正确的 IP

# 解决: 修改 DNS A 记录指向正确的 IP
```

#### 原因 3: 80 端口被其他服务占用

```bash
# 检查节点 80 端口
sudo netstat -tlnp | grep :80

# 输出:
# tcp  0  0  0.0.0.0:80  0.0.0.0:*  LISTEN  1234/nginx

# 检查是否是 Ingress Controller
kubectl get pods -n ingress-nginx -o wide

# 如果不是 Ingress Controller，需要排查端口冲突
```

---

## 4. 问题 3: DNS-01 验证失败

### 4.1 症状

```bash
kubectl describe certificaterequest myapp-tls-1

# 输出:
# Status:
#   Conditions:
#     Type:    Ready
#     Status:  False
#     Message: Failed to perform DNS-01 challenge...
```

### 4.2 诊断步骤

```bash
# 1. 查看 Challenge
kubectl get challenge

# 输出:
# NAME                               STATE      DOMAIN              AGE
# myapp-tls-1-xxx-xxx                 pending    *.example.com      10m

# 2. 查看 DNS TXT 记录
dig _acme-challenge.example.com TXT

# 输出: 空（记录未创建）
```

### 4.3 常见原因和解决

#### 原因 1: DNS API 凭证错误

```bash
# 检查 Secret
kubectl get secret cloudflare-api-token -n cert-manager

# 输出:
# NAME                      TYPE
# cloudflare-api-token      Opaque

# 验证 Secret 内容
kubectl get secret cloudflare-api-token -n cert-manager -o jsonpath='{.data.api-token}' | base64 -d

# 输出应该包含 API Token

# 解决: 重新创建 Secret
kubectl delete secret cloudflare-api-token -n cert-manager
kubectl create secret generic cloudflare-api-token \
  --from-literal=api-token=YOUR_CLOUDFLARE_API_TOKEN \
  -n cert-manager
```

#### 原因 2: DNS API 权限不足

```bash
# 查看 cert-manager 日志
kubectl logs -n cert-manager deployment/cert-manager | grep -i error

# 输出:
# E0115 10:00:00.000000       1 controller.go:157]  failed to determine zone for domain 'example.com': cloudflare: error (9103): failed to list zones: authentication error

# 解决:
# 1. 检查 Cloudflare API Token 权限
# 2. 确保包含 Zone - DNS - Edit 权限
# 3. 确保包含正确的 Zone 资源
```

#### 原因 3: DNS 生效延迟

```bash
# 检查 TXT 记录
dig _acme-challenge.example.com TXT

# 输出: 空（未生效）

# 解决: 等待 DNS 生效（1-5 分钟）
# 或检查 DNS 传播
watch dig _acme-challenge.example.com TXT
```

---

## 5. 问题 4: 证书无法自动续期

### 5.1 症状

```bash
# 查看证书过期时间
kubectl get secret myapp-tls-cert -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -dates

# 输出:
# notBefore=Jan 15 00:00:00 2025 GMT
# notAfter=Apr 15 00:00:00 2025 GMT  # 即将过期

# 查看 Certificate 状态
kubectl get certificate myapp-tls -o yaml | grep -A 5 status

# 输出:
# status:
#   lastRenewalTime: "2025-01-15T00:00:00Z"
#   notAfter: "2025-04-15T00:00:00Z"
#   renewalTime: "2025-03-16T00:00:00Z"  # 应该续期但未续期
```

### 5.2 诊断步骤

```bash
# 1. 检查 cert-manager Controller 是否运行
kubectl get deployment -n cert-manager cert-manager

# 输出:
# NAME            READY   UP-TO-DATE   AGE
# cert-manager    1/1     1            30d

# 2. 检查 Controller 日志
kubectl logs -n cert-manager deployment/cert-manager --tail=100

# 查找续期相关日志
# 3. 检查 CertificateRequest
kubectl get certificaterequest --sort-by=.metadata.creationTimestamp
```

### 5.3 常见原因和解决

#### 原因 1: Certificate 配置错误

```bash
# 检查 Certificate 配置
kubectl get certificate myapp-tls -o yaml | grep -E "duration|renewBefore"

# 输出:
# duration: 2160h  # 90天
# renewBefore: 720h  # 30天

# 计算续期时间: notAfter - renewBefore
# 例如: 2025-04-15 - 30天 = 2025-03-16

# 解决: 调整 renewBefore
kubectl patch certificate myapp-tls -p '{"spec":{"renewBefore":"1440h"}}'  # 60天
```

#### 原因 2: ClusterIssuer 不存在或失败

```bash
# 检查 ClusterIssuer
kubectl get clusterissuer

# 输出:
# NAME              READY   AGE
# letsencrypt-prod   False   30d  # Ready = False

# 解决: 修复 ClusterIssuer
kubectl describe clusterissuer letsencrypt-prod

# 查看 Events 找到错误原因
```

#### 原因 3: 证书签发失败

```bash
# 查看最近的 CertificateRequest
kubectl get certificaterequest --sort-by=.metadata.creationTimestamp | tail -5

# 查看失败的 CertificateRequest
kubectl describe certificaterequest <name>

# 查看错误原因
# 通常与首次签发错误相同（HTTP-01/DNS-01 验证失败）
```

### 5.4 手动触发续期

```bash
# 方法 1: 删除 Secret（不推荐）
kubectl delete secret myapp-tls-cert

# cert-manager 会自动重新申请证书

# 方法 2: 删除 CertificateRequest
kubectl get certificaterequest
kubectl delete certificaterequest <name>

# 方法 3: 修改 Certificate（触发重新协调）
kubectl patch certificate myapp-tls -p '{"metadata":{"annotations":{"renew-time":"$(date +%s)"}}}'

# 等待新证书签发
kubectl get certificate myapp-tls -w
```

---

## 6. 问题 5: Ingress 无法访问

### 6.1 症状

```bash
# HTTPS 访问失败
curl https://www.example.com/

# 输出:
# curl: (60) SSL: certificate subject name (www.example.com) does not match target host name
```

### 6.2 诊断步骤

```bash
# 1. 检查 Ingress 配置
kubectl get ingress myapp-ingress -o yaml

# 2. 检查 Secret
kubectl get secret myapp-tls-cert

# 3. 检查证书内容
kubectl get secret myapp-tls-cert -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text | grep -E "Subject|DNS"

# 4. 测试 HTTPS 连接
openssl s_client -connect www.example.com:443 -servername www.example.com
```

### 6.3 常见原因和解决

#### 原因 1: Secret 引用错误

```bash
# 检查 Ingress
kubectl get ingress myapp-ingress -o yaml | grep -A 5 tls

# 输出:
# tls:
# - hosts:
#   - www.example.com
#   secretName: wrong-cert  # 错误的 Secret

# 解决: 修改 Ingress
kubectl patch ingress myapp-ingress --type='json' -p='[{"op": "replace", "path": "/spec/tls/0/secretName", "value":"myapp-tls-cert"}]'
```

#### 原因 2: 证书域名不匹配

```bash
# 查看证书域名
kubectl get secret myapp-tls-cert -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text | grep -A 1 "Subject Alternative Name"

# 输出:
# DNS:example.com, DNS:www.example.com  # 缺少访问的域名

# 解决: 修改 Certificate dnsNames
kubectl patch certificate myapp-tls -p '{"spec":{"dnsNames":["www.example.com","example.com","api.example.com"]}}'
```

#### 原因 3: 浏览器缓存

```bash
# 清除浏览器缓存
# Chrome: F12 → Application → Clear storage → Clear site data

# 或使用无痕模式测试
```

---

## 7. 日志查看技巧

### 7.1 cert-manager Controller 日志

```bash
# 查看最近 100 行日志
kubectl logs -n cert-manager deployment/cert-manager --tail=100

# 实时查看日志
kubectl logs -n cert-manager deployment/cert-manager -f

# 只看错误
kubectl logs -n cert-manager deployment/cert-manager | grep -i error

# 只看特定证书
kubectl logs -n cert-manager deployment/cert-manager | grep "myapp-tls"
```

### 7.2 日志级别

```bash
# 修改日志级别（调试）
kubectl patch deployment cert-manager -n cert-manager -p '{"spec":{"template":{"spec":{"containers":[{"name":"cert-manager","args":["--v=6"]}]}}}}'

# 恢复默认
kubectl patch deployment cert-manager -n cert-manager -p '{"spec":{"template":{"spec":{"containers":[{"name":"cert-manager","args":["--v=2"]}]}}}}'
```

### 7.3 事件日志

```bash
# 查看 Certificate 事件
kubectl describe certificate myapp-tls

# 查看 CertificateRequest 事件
kubectl describe certificaterequest myapp-tls-1

# 查看 Challenge 事件
kubectl describe challenge myapp-tls-1-xxx-xxx
```

---

## 8. 监控和告警

### 8.1 Prometheus 指标

```bash
# cert-manager 提供的指标
- certificate_ready_status  # 证书就绪状态
- certificate_expiration_timestamp_seconds  # 证书过期时间
- certificate_renewal_timestamp_seconds  # 证书续期时间
- acme_client_request_count  # ACME 请求计数
- acme_client_request_duration_seconds  # ACME 请求耗时
```

### 8.2 Grafana Dashboard

```bash
# 导入 cert-manager 官方 Dashboard
# Dashboard ID: 13106

# 或手动配置
# PromQL 查询示例

# 证书就绪状态
certificate_ready_status{name="myapp-tls"}

# 证书过期时间
certificate_expiration_timestamp_seconds{name="myapp-tls"}

# 证书续期时间
certificate_renewal_timestamp_seconds{name="myapp-tls"}

# 证书即将过期（< 7 天）
certificate_expiration_timestamp_seconds{name="myapp-tls"} - time() < 604800
```

### 8.3 告警规则

```yaml
# cert-manager-alerts.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: cert-manager-alerts
  namespace: cert-manager
spec:
  groups:
  - name: cert-manager
    rules:
    # 证书即将过期
    - alert: CertificateExpiringSoon
      expr: certificate_expiration_timestamp_seconds - time() < 604800
      for: 1m
      labels:
        severity: warning
      annotations:
        summary: "Certificate {{ $labels.name }} expiring soon"
        description: "Certificate {{ $labels.name }} will expire in less than 7 days"

    # 证书已过期
    - alert: CertificateExpired
      expr: certificate_expiration_timestamp_seconds - time() < 0
      for: 1m
      labels:
        severity: critical
      annotations:
        summary: "Certificate {{ $labels.name }} expired"
        description: "Certificate {{ $labels.name }} has expired"

    # 证书未就绪
    - alert: CertificateNotReady
      expr: certificate_ready_status{name=~".+"} == 0
      for: 1h
      labels:
        severity: warning
      annotations:
        summary: "Certificate {{ $labels.name }} not ready"
        description: "Certificate {{ $labels.name }} is not ready for more than 1 hour"
```

---

## 总结

### 问题总结表

| 问题 | 症状 | 原因 | 解决 |
|------|------|------|------|
| **证书签发失败** | Ready=False | ClusterIssuer 不存在 / 域名未解析 / 防火墙 | 创建 ClusterIssuer / 配置 DNS / 开放防火墙 |
| **HTTP-01 验证失败** | Challenge pending | 临时 Ingress 失败 / 域名解析错误 / 端口冲突 | 检查 Ingress Controller / 修改 DNS / 检查端口 |
| **DNS-01 验证失败** | Challenge pending | API 凭证错误 / 权限不足 / DNS 生效延迟 | 重新创建 Secret / 检查权限 / 等待生效 |
| **证书无法续期** | 证书即将过期 | 配置错误 / ClusterIssuer 失败 / 签发失败 | 调整配置 / 修复 ClusterIssuer / 查看错误 |
| **Ingress 无法访问** | SSL 证书错误 | Secret 引用错误 / 域名不匹配 / 浏览器缓存 | 修改 Ingress / 更新证书 / 清除缓存 |

### 快速诊断命令

```bash
# 一键诊断
kubectl get certificate && \
kubectl get secret && \
kubectl get ingress && \
curl -I https://www.example.com/ && \
echo "✅ 一切正常"
```

---

**返回**: [00-INDEX.md](./00-INDEX.md) 🚀
