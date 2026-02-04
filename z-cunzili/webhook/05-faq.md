# Kubernetes Ingress 常见问题解答 (FAQ)

> 20+ 常见问题快速索引，帮你快速找到答案

**作者**: cunzili
**版本**: v2.0
**更新日期**: 2025-01-15

---

## 📋 问题分类

### [基础概念（5 问）](#基础概念)
1. [Ingress 和 Ingress Controller 有什么区别？](#q1-ingress-和-ingress-controller-有什么区别)
2. [为什么需要 Ingress + Service，不能直接用 Ingress 吗？](#q2-为什么需要-ingress--service不能直接用-ingress-吗)
3. [Service 如何找到 Pod？通过标签还是名字？](#q3-service-如何找到-pod通过标签还是名字)
4. [什么是 Endpoints？它是怎么自动更新的？](#q4-什么是-endpoints它是怎么自动更新的)
5. [公网 IP 和私网 IP 有什么区别？](#q5-公网-ip-和私网-ip-有什么区别)

### [配置实践（6 问）](#配置实践)
6. [Service 的 Endpoints 为空怎么办？](#q6-service-的-endpoints-为空怎么办)
7. [Ingress 的 ADDRESS 字段为什么是空的？](#q7-ingress-的-address-字段为什么是空的)
8. [targetPort 应该用端口号还是端口名称？](#q8-targetport-应该用端口号还是端口名称)
9. [为什么我配置了域名还是无法访问？](#q9-为什么我配置了域名还是无法访问)
10. [MetalLB L2 模式无法跨网段访问怎么办？](#q10-metallb-l2-模式无法跨网段访问怎么办)
11. [Ingress Controller 应该用 Deployment 还是 DaemonSet？](#q11-ingress-controller-应该用-deployment-还是-daemonset)

### [故障排查（5 问）](#故障排查)
12. [Pod 一直 Pending 怎么办？](#q12-pod-一直-pending-怎么办)
13. [如何在集群外访问 Ingress？](#q13-如何在集群外访问-ingress)
14. [如何快速定位 Ingress 无法访问的问题？](#q14-如何快速定位-ingress-无法访问的问题)
15. [证书签发失败怎么办？](#q15-证书签发失败怎么办)
16. [如何查看 Ingress Controller 的日志？](#q16-如何查看-ingress-controller-的日志)

### [生产环境（4 问）](#生产环境)
17. [生产环境应该用 MetalLB 还是云厂商 LoadBalancer？](#q17-生产环境应该用-metallb-还是云厂商-loadbalancer)
18. [如何配置 HTTPS？](#q18-如何配置-https)
19. [生产环境需要多少个副本？](#q19-生产环境需要多少个副本)
20. [如何实现高可用？](#q20-如何实现高可用)

---

## 基础概念

### Q1: Ingress 和 Ingress Controller 有什么区别？

**核心区别**:

| 特性 | Ingress | Ingress Controller |
|------|---------|-------------------|
| **类型** | 资源对象（YAML 配置） | 软件（Nginx/Traefik） |
| **作用** | 定义路由规则 | 实际处理流量 |
| **数量** | 可以有多个 | 通常一个集群一个 |
| **工作方式** | 静态配置 | 动态运行 |

**类比**:
```
Ingress = 路由表（告诉怎么走）
Ingress Controller = 路由器（实际转发流量）
```

**详细解释**:
- **Ingress**: 只是 Kubernetes 的一个资源对象，存储在 etcd 中，定义了域名、路径、后端 Service 的映射关系
- **Ingress Controller**: 实际运行的软件（如 Nginx），监听 Ingress 资源变化，动态更新配置并处理流量

**相关文档**: [01-concepts.md](./01-concepts.md) 第 1 节

---

### Q2: 为什么需要 Ingress + Service，不能直接用 Ingress 吗？

**原因**: Ingress 只能引用 Service，不能直接引用 Pod

**技术原因**:
1. **Pod IP 不稳定**: Pod 重建后 IP 会变化
2. **服务发现**: Service 提供稳定的虚拟 IP（ClusterIP）
3. **负载均衡**: Service 自动实现负载均衡
4. **解耦**: Ingress 不需要知道 Pod 的存在

**错误示例**（Ingress 不能直接引用 Pod）:
```yaml
# ❌ 错误：Ingress 不能直接引用 Pod
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
spec:
  rules:
  - host: myapp.local
    http:
      paths:
      - backend:
          service:  # 必须是 Service
            name: 10.0.0.203  # ❌ 不能直接写 Pod IP
```

**正确示例**:
```yaml
# ✅ 正确：Ingress 引用 Service
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
spec:
  rules:
  - host: myapp.local
    http:
      paths:
      - backend:
          service:
            name: myapp-service  # ✅ Service 名称
```

**架构关系**:
```
Ingress (规则)
    ↓ 引用
Service (服务发现 + 负载均衡)
    ↓ 标签选择
Pod (实际运行)
```

**相关文档**: [01-concepts.md](./01-concepts.md) 第 5 节

---

### Q3: Service 如何找到 Pod？通过标签还是名字？

**答案**: 通过 **标签（labels）**，不是名字

**工作原理**:
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

**匹配逻辑**:
```
1. Service 定义 selector: app=myapp
2. Endpoint Controller 监听所有 Pod
3. 找到所有 labels.app=myapp 的 Pod
4. 自动创建/更新 Endpoints 对象
5. Service 通过 Endpoints 获取 Pod IP 列表
```

**常见错误**:
```yaml
# ❌ 错误：大小写不一致
# Pod labels: App: myapp (大写)
# Service selector: app: myapp (小写)

# ❌ 错误：标签名不一致
# Pod labels: app: myapp
# Service selector: application: myapp
```

**验证方法**:
```bash
# 查看 Pod labels
kubectl get pods --show-labels

# 查看 Service selector
kubectl get svc myapp-service -o jsonpath='{.spec.selector}'

# 验证匹配
kubectl get pods -l app=myapp
```

**关键点**:
- ✅ Service 通过 **标签** 选择 Pod
- ✅ Ingress 通过 **名称** 引用 Service
- ❌ Ingress 不通过标签选择 Service

**相关文档**: [01-concepts.md](./01-concepts.md) 第 3 节

---

### Q4: 什么是 Endpoints？它是怎么自动更新的？

**定义**: Endpoints 是 Kubernetes 自动管理的资源对象，记录了 Service 对应的所有 Pod IP

**示例**:
```bash
kubectl get endpoints myapp-service

# 输出:
# NAME            ENDPOINTS                          AGE
# myapp-service   10.0.0.203:80,10.0.0.239:80        5m
#                            ↑
#                    Pod IP:Port 列表
```

**自动更新机制**:
```
1. Pod 创建
   ↓
2. Endpoint Controller 监听 Pod 事件
   ↓
3. 检查 Pod labels 是否匹配 Service selector
   ↓
4. 自动添加到 Endpoints
   ↓
5. Pod 删除/不健康
   ↓
6. 自动从 Endpoints 移除
```

**YAML 结构**:
```yaml
apiVersion: v1
kind: Endpoints
metadata:
  name: myapp-service
subsets:
- addresses:
  - ip: 10.0.0.203
    nodeName: node-1
    targetRef:
      kind: Pod
      name: myapp-pod-1
  - ip: 10.0.0.239
    nodeName: node-1
    targetRef:
      kind: Pod
      name: myapp-pod-2
  ports:
  - port: 80
```

**自动化特性**:
- ✅ Pod 创建 → 自动添加
- ✅ Pod 删除 → 自动移除
- ✅ Pod 不健康 → 自动移除
- ✅ Pod IP 变化 → 自动更新

**验证自动化**:
```bash
# 监控 Endpoints 变化
kubectl get endpoints myapp-service -w

# 另一个终端：删除 Pod
kubectl delete pod <pod-name>

# 观察：Endpoints 自动更新
```

**相关文档**: [01-concepts.md](./01-concepts.md) 第 4 节

---

### Q5: 公网 IP 和私网 IP 有什么区别？

**核心区别**: 全球可访问性

| 特性 | 公网 IP | 私网 IP |
|------|---------|---------|
| **访问范围** | 全球任何地方 | 仅局域网内 |
| **唯一性** | 全球唯一 | 可重复使用 |
| **成本** | 通常需要付费 | 免费 |
| **示例** | 8.8.8.8, 47.96.123.45 | 192.168.1.1, 10.0.0.1 |

**私网 IP 范围**:
```
- 10.0.0.0    - 10.255.255.255    (10.0.0.0/8)
- 172.16.0.0  - 172.31.255.255    (172.16.0.0/12)
- 192.168.0.0 - 192.168.255.255   (192.168.0.0/16)
```

**公网 IP 范围**: 除私网 IP 外的所有 IP

**示例**:
```
家庭网络:
├─ 你的电脑: 192.168.1.100 (私网 IP)
├─ 路由器: 192.168.1.1 (私网 IP)
└─ 公网 IP: 1.2.3.4 (全球可访问)

公司网络:
├─ 你的电脑: 192.168.1.100 (私网 IP)
├─ 路由器: 192.168.1.1 (私网 IP)
└─ 公网 IP: 5.6.7.8 (全球可访问)

# 可以相同！因为不在同一网络
```

**NAT（网络地址转换）**:
```
家庭网络:
电脑 A: 192.168.1.100 ─┐
电脑 B: 192.168.1.101 ─┼─→ 路由器 ─→ 公网 IP: 1.2.3.4 ─→ 互联网
电脑 C: 192.168.1.102 ─┘

作用:
1. 多个私网 IP 共享一个公网 IP
2. 节省公网 IP 资源
3. 隐藏内网结构
```

**测试方法**:
```bash
# 查看本机 IP
ip addr show

# 查看公网 IP
curl ifconfig.me
curl ipinfo.io/ip

# 测试连通性
ping 8.8.8.8  # 公网 IP，应该通
ping 192.168.1.1  # 私网 IP，仅局域网通
```

**在 Kubernetes 中**:
```
Ingress Controller Service:
├─ ClusterIP: 20.98.94.88 (私网 IP，集群内访问)
└─ LoadBalancer: 47.96.123.45 (公网 IP，全球访问)
```

**相关文档**: [01-concepts.md](./01-concepts.md) 第 9 节

---

## 配置实践

### Q6: Service 的 Endpoints 为空怎么办？

**症状**:
```bash
kubectl get endpoints myapp-service
# NAME            ENDPOINTS   AGE
# myapp-service   <none>      10m
```

**原因**: Service 的 selector 与 Pod 的 labels 不匹配，或端口配置错误

**诊断步骤**:
```bash
# 1. 检查 Pod labels
kubectl get pods --show-labels

# 预期输出:
# NAME                     READY   LABELS
# myapp-xxx-xxx            1/1     app=myapp

# 2. 检查 Service selector
kubectl get svc myapp-service -o jsonpath='{.spec.selector}'

# 预期输出:
# {"app":"myapp"}

# 3. 验证匹配
kubectl get pods -l app=myapp

# 4. 检查端口配置
kubectl get svc myapp-service -o yaml | grep -A 5 "ports:"
kubectl get deployment myapp -o yaml | grep -A 5 "ports:"
```

**常见错误 1**: 标签大小写不一致
```yaml
# ❌ 错误
# Pod labels: App: myapp (大写)
# Service selector: app: myapp (小写)

# ✅ 正确（统一小写）
metadata:
  labels:
    app: myapp
spec:
  selector:
    app: myapp
```

**常见错误 2**: 端口名称不匹配
```yaml
# ❌ 错误
# Service targetPort: http (引用端口名称)
# Pod 没有定义端口名称

# ✅ 正确（方案 1: 使用端口号）
ports:
- port: 80
  targetPort: 80  # 直接使用数字

# ✅ 正确（方案 2: 使用端口名称）
# Pod
ports:
- containerPort: 80
  name: http  # 必须定义名称

# Service
ports:
- port: 80
  targetPort: http  # 引用名称
```

**解决方案**:
```bash
# 方案 1: 修复标签（重新创建）
kubectl delete deployment myapp
kubectl delete service myapp-service
# 使用正确的配置重新创建

# 方案 2: 修复 Service（直接修改端口）
kubectl patch svc myapp-service -p '{"spec":{"ports":[{"name":"http","port":80,"targetPort":80}]}}'

# 方案 3: 给 Pod 添加标签
kubectl label pod <pod-name> app=myapp --overwrite
```

**相关文档**: [04-troubleshooting.md](./04-troubleshooting.md) 问题 1

---

### Q7: Ingress 的 ADDRESS 字段为什么是空的？

**症状**:
```bash
kubectl get ingress myapp-ingress
# NAME            CLASS   HOSTS         ADDRESS   PORTS   AGE
# myapp-ingress   nginx   myapp.local             80      10m
#                                         ↑ 空的
```

**原因**: **这是正常的！** 在裸机/本地环境中，如果不使用 MetalLB 或云厂商 LoadBalancer，ADDRESS 字段会是空的。

**ADDRESS 字段填充条件**:

| Service 类型 | ADDRESS 字段 | 环境 |
|-------------|-------------|------|
| **LoadBalancer**（云） | 公网 IP | AWS/阿里云/Azure |
| **LoadBalancer**（MetalLB） | 分配的 IP | 本地/裸机 |
| **NodePort** | 节点 IP 或空 | 本地/裸机 |
| **ClusterIP** | 空 | 本地/裸机 |

**验证方法**:
```bash
# 1. 检查 Ingress Controller Service 类型
kubectl get svc -n ingress-nginx ingress-nginx-controller

# 预期输出（如果是 ClusterIP，ADDRESS 为空）:
# TYPE        EXTERNAL-IP
# ClusterIP   <none>

# 2. 检查是否安装了 MetalLB
kubectl get pods -n metallb-system

# 3. 检查 IP 地址池
kubectl get ipaddresspool -n metallb-system
```

**解决方案**:

**方案 1: 使用 MetalLB（推荐）**
```bash
# 安装 MetalLB
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/main/config/manifests/metallb-native.yaml

# 配置 IP 池
kubectl apply -f - <<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: ingress-ips
  namespace: metallb-system
spec:
  addresses:
  - 192.168.12.200-192.168.12.250
EOF

# 修改 Service 类型
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"LoadBalancer"}}'
```

**方案 2: 使用 NodePort（替代方案）**
```bash
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"NodePort"}}'
```

**方案 3: 忽略 ADDRESS 字段**
```bash
# ADDRESS 字段只是显示，不影响功能
# 直接使用节点 IP 访问即可
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
echo "$NODE_IP myapp.local" | sudo tee -a /etc/hosts
```

**相关文档**: [04-troubleshooting.md](./04-troubleshooting.md) 问题 4

---

### Q8: targetPort 应该用端口号还是端口名称？

**答案**: 推荐使用 **端口号**（更简单、更不易出错）

**方案对比**:

| 方案 | 优点 | 缺点 | 推荐度 |
|------|------|------|--------|
| **端口号** | 简单、不易出错 | 端口变化需要修改 Service | ⭐⭐⭐⭐⭐ |
| **端口名称** | 端口变化无需修改 Service | 需要确保 Pod 定义名称 | ⭐⭐⭐ |

**方案 1: 使用端口号（推荐）**
```yaml
# Service
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
spec:
  selector:
    app: myapp
  ports:
  - port: 80
    targetPort: 80  # ✅ 直接使用数字
```

**方案 2: 使用端口名称**
```yaml
# Pod
apiVersion: v1
kind: Pod
metadata:
  name: myapp
spec:
  containers:
  - name: myapp
    ports:
    - containerPort: 80
      name: http  # ✅ 必须定义名称

---
# Service
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
spec:
  selector:
    app: myapp
  ports:
  - port: 80
    targetPort: http  # ✅ 引用名称
```

**常见错误**:
```yaml
# ❌ 错误：Pod 没有定义端口名称
# Pod
ports:
- containerPort: 80  # 没有 name 字段

# Service
targetPort: http  # ❌ 引用不存在的名称

# 结果: Endpoints 为空
```

**建议**: 初学者和简单场景使用端口号，复杂场景使用端口名称

**相关文档**: [02-quick-start.md](./02-quick-start.md) 步骤 2

---

### Q9: 为什么我配置了域名还是无法访问？

**问题**: 配置了 `/etc/hosts`，但无法访问

**诊断流程**:

**步骤 1: 验证 DNS 解析**
```bash
# 测试域名解析
ping myapp.local

# 或
nslookup myapp.local

# 或
dig myapp.local

# 预期: 返回正确的 IP
```

**步骤 2: 验证 IP 可达**
```bash
# Ping IP 地址
ping 192.168.12.200

# 测试端口
telnet 192.168.12.200 80

# 或
nc -zv 192.168.12.200 80
```

**步骤 3: 验证 Ingress Controller**
```bash
# 检查 Ingress Controller 是否运行
kubectl get pods -n ingress-nginx

# 检查 Service
kubectl get svc -n ingress-nginx

# 检查 Ingress
kubectl get ingress
```

**步骤 4: 测试直接访问**
```bash
# 直接访问 IP
curl http://192.168.12.200/

# 访问 NodePort
kubectl get svc -n ingress-nginx ingress-nginx-controller
# 如果是 NodePort，使用: 节点IP:NodePort
curl http://节点IP:NodePort/
```

**常见原因**:

**原因 1: MetalLB L2 模式无法跨网段**
```bash
# 检查网段
# K8s 节点
ip addr show | grep "192.168.12"

# 你的电脑
ip addr show | grep "192.168"

# 如果前三位不同（如 192.168.12.x vs 192.168.20.x），就无法访问

# 解决: 使用 NodePort
```

**原因 2: 防火墙阻止**
```bash
# 检查防火墙
sudo iptables -L -n | grep -E "80|443"

# 开放端口
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
```

**原因 3: DNS 配置错误**
```bash
# 检查 /etc/hosts
cat /etc/hosts | grep myapp.local

# 确保格式正确: IP 域名
# 192.168.12.200 myapp.local
```

**原因 4: Ingress Controller Service 类型错误**
```bash
# 检查 Service 类型
kubectl get svc -n ingress-nginx ingress-nginx-controller

# 如果是 ClusterIP，改为 LoadBalancer 或 NodePort
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"LoadBalancer"}}'
```

**相关文档**: [04-troubleshooting.md](./04-troubleshooting.md) 问题 5

---

### Q10: MetalLB L2 模式无法跨网段访问怎么办？

**原因**: MetalLB L2 模式使用 ARP 广播，ARP 无法跨越网段

**诊断**:
```bash
# 检查网段
# K8s 节点
ip addr show | grep "192.168.12"

# 你的电脑
ip addr show | grep "192.168"

# 如果前三位不同（如 192.168.12.x vs 192.168.20.x），就不在同一网段
```

**解决方案**:

**方案 1: 改用 NodePort（最简单）**
```bash
# 修改 Service 为 NodePort
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"NodePort"}}'

# 获取节点 IP 和 NodePort
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
NODE_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[0].nodePort}')

# 配置 DNS
echo "$NODE_IP myapp.local" | sudo tee -a /etc/hosts

# 访问（带端口）
curl http://myapp.local:$NODE_PORT/
```

**方案 2: 使用反向代理**
```bash
# 在 K8s 节点上配置 Nginx 反向代理
sudo apt install nginx -y

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
    }
}
EOF

sudo ln -s /etc/nginx/sites-available/k8s-ingress /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx

# 配置 DNS（使用节点 IP）
NODE_IP=192.168.12.214
echo "$NODE_IP myapp.local" | sudo tee -a /etc/hosts
```

**方案 3: 使用 BGP 模式（高级）**
```bash
# 需要路由器支持 BGP
# 配置复杂，不推荐初学者
```

**相关文档**: [04-troubleshooting.md](./04-troubleshooting.md) 问题 5

---

### Q11: Ingress Controller 应该用 Deployment 还是 DaemonSet？

**对比**:

| 特性 | Deployment | DaemonSet |
|------|-----------|-----------|
| **Pod 数量** | 固定副本数 | 每个节点一个 Pod |
| **资源利用率** | 高（可集中调度） | 低（每个节点都有） |
| **高可用** | 中等 | 高 |
| **复杂度** | 低 | 中等 |
| **推荐场景** | 生产环境（推荐） | 特殊需求 |

**Deployment（推荐）**:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ingress-nginx-controller
  namespace: ingress-nginx
spec:
  replicas: 2  # 2-3 个副本即可
  # ...
```

**优点**:
- ✅ 资源利用率高
- ✅ 容易管理
- ✅ 容易扩展

**DaemonSet**:
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: ingress-nginx-controller
  namespace: ingress-nginx
spec:
  # ...
```

**优点**:
- ✅ 每个节点都有，高可用
- ✅ 流量本地处理，性能好

**缺点**:
- ❌ 资源浪费（每个节点都运行）
- ❌ 管理复杂

**建议**:
- 生产环境使用 **Deployment**（2-3 个副本）
- 大规模集群（100+ 节点）考虑 **DaemonSet**

---

## 故障排查

### Q12: Pod 一直 Pending 怎么办？

**症状**:
```bash
kubectl get pods
# NAME                     READY   STATUS    RESTARTS   AGE
# ingress-nginx-xxx        0/1     Pending   0          5m
```

**诊断**:
```bash
# 查看 Pod 详情
kubectl describe pod ingress-nginx-xxx

# 常见错误信息:
# Warning  FailedScheduling  0/2 nodes are available: 2 node(s) had untolerated taint {devbox.sealos.io/node: }.
```

**原因**: 节点有污点（Taint），Pod 没有对应的容忍（Toleration）

**解决方案**:
```bash
# 添加容忍
kubectl patch deployment ingress-nginx-controller -n ingress-nginx --type=json -p='[
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
```

**相关文档**: [04-troubleshooting.md](./04-troubleshooting.md) 问题 2

---

### Q13: 如何在集群外访问 Ingress？

**方案对比**:

| 方案 | 复杂度 | 推荐度 | 适用场景 |
|------|--------|--------|----------|
| **LoadBalancer** | 低 | ⭐⭐⭐⭐⭐ | 云环境 |
| **NodePort** | 低 | ⭐⭐⭐⭐ | 裸机环境 |
| **MetalLB** | 中 | ⭐⭐⭐⭐ | 裸机环境 |
| **反向代理** | 中 | ⭐⭐⭐ | 特殊需求 |

**方案 1: LoadBalancer（云环境）**
```bash
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"LoadBalancer"}}'

LB_IP=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "$LB_IP myapp.local" | sudo tee -a /etc/hosts
```

**方案 2: NodePort（裸机环境）**
```bash
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"NodePort"}}'

NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
NODE_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[0].nodePort}')

echo "$NODE_IP myapp.local" | sudo tee -a /etc/hosts
curl http://myapp.local:$NODE_PORT/
```

**方案 3: MetalLB（裸机环境）**
```bash
# 安装 MetalLB
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/main/config/manifests/metallb-native.yaml

# 配置 IP 池
kubectl apply -f - <<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: ingress-ips
  namespace: metallb-system
spec:
  addresses:
  - 192.168.12.200-192.168.12.250
EOF

kubectl apply -f - <<EOF
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: ingress-l2
  namespace: metallb-system
spec:
  ipAddressPools:
  - ingress-ips
EOF

kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"LoadBalancer"}}'
```

**相关文档**: [03-complete-guide.md](./03-complete-guide.md) 第 2 节

---

### Q14: 如何快速定位 Ingress 无法访问的问题？

**诊断流程图**:
```
无法访问
    │
    ▼
1. 检查 Pod
kubectl get pods -l app=myapp
    │
    ├─→ Pod 不正常 → 查看日志、describe
    │
    ▼ Pod 正常
2. 检查 Endpoints
kubectl get endpoints myapp-service
    │
    ├─→ Endpoints 为空 → 检查 labels/selector
    │
    ▼ Endpoints 正常
3. 检查 Service 连通性
kubectl run test --rm -it -- wget -O- http://myapp-service/
    │
    ├─→ 无法访问 → 检查端口配置
    │
    ▼ Service 正常
4. 检查 Ingress Controller
kubectl get pods -n ingress-nginx
    │
    ├─→ Controller 不正常 → 查看日志
    │
    ▼ Controller 正常
5. 检查 DNS
ping myapp.local
    │
    ├─→ DNS 无法解析 → 检查 /etc/hosts
    │
    ▼ DNS 正常
6. 检查网络连通性
ping <LoadBalancer IP>
telnet <LoadBalancer IP> 80
```

**快速诊断脚本**:
```bash
#!/bin/bash

echo "=== 快速诊断 ==="

POD_NAME="myapp"
SVC_NAME="myapp-service"
INGRESS_NAME="myapp-ingress"
NAMESPACE="default"

# 1. Pod
echo -e "\n1. Pod 状态"
kubectl get pods -l app=$POD_NAME

# 2. Service
echo -e "\n2. Service Endpoints"
kubectl get endpoints $SVC_NAME

# 3. Ingress Controller
echo -e "\n3. Ingress Controller"
kubectl get pods -n ingress-nginx

# 4. Ingress
echo -e "\n4. Ingress"
kubectl get ingress $INGRESS_NAME

# 5. LoadBalancer
echo -e "\n5. LoadBalancer IP"
kubectl get svc -n ingress-nginx ingress-nginx-controller

# 6. 测试访问
echo -e "\n6. 测试访问"
kubectl run test --rm -it --restart=Never -- wget -O- -q http://$SVC_NAME/ 2>/dev/null | head -5

echo -e "\n=== 完成 ==="
```

**相关文档**: [04-troubleshooting.md](./04-troubleshooting.md) 通用诊断流程

---

### Q15: 证书签发失败怎么办？

**症状**:
```bash
kubectl get certificate
# NAME          READY   SECRET         AGE
# myapp-tls     False   myapp-tls-cert  5m
#                      ↑ Ready = False
```

**诊断**:
```bash
# 查看证书详情
kubectl describe certificate myapp-tls

# 常见错误:
# - Failed to determine ACME account URL
# - Failed to verify domain ownership
# - Rate limited
```

**常见原因**:

**原因 1: ClusterIssuer 未创建**
```bash
# 检查 ClusterIssuer
kubectl get clusterissuer

# 创建 ClusterIssuer
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

**原因 2: 域名无法解析**
```bash
# 测试域名解析
dig +short www.example.com

# 确保 DNS 已生效，然后重新创建 Ingress
kubectl delete ingress myapp-ingress
kubectl apply -f ingress.yaml
```

**原因 3: 被限流**
```bash
# Let's Encrypt 限制: 每周 5 个失败证书
# 解决: 等待一周或使用生产环境 URL
```

**相关文档**: [03-complete-guide.md](./03-complete-guide.md) 第 4 节

---

### Q16: 如何查看 Ingress Controller 的日志？

**查看日志**:
```bash
# 查看最近 100 行日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx --tail=100

# 实时查看日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx -f

# 查看特定请求的日志
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx | grep "www.example.com"
```

**日志格式**:
```
<datetime> <nginx_version> <remote_addr> - <request> <status> <bytes_sent> <http_referer> <http_user_agent>

示例:
2025-01-15T10:30:45.123Z 1.25.1 192.168.1.100 - GET www.example.com/ HTTP/1.1 200 1234 "-" "Mozilla/5.0"
```

**过滤日志**:
```bash
# 只看 404 错误
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx | grep " 404 "

# 只看特定域名
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx | grep "myapp.local"

# 统计状态码
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx | awk '{print $9}' | sort | uniq -c
```

**相关文档**: [03-complete-guide.md](./03-complete-guide.md) 第 7 节

---

## 生产环境

### Q17: 生产环境应该用 MetalLB 还是云厂商 LoadBalancer？

**对比**:

| 方案 | 成本/月 | 复杂度 | 高可用 | 推荐度 |
|------|---------|--------|--------|--------|
| **云厂商 LoadBalancer** | $15-30 | 低 | 高 | ⭐⭐⭐⭐⭐ |
| **MetalLB + BGP** | 服务器成本 | 高 | 高 | ⭐⭐⭐⭐ |
| **MetalLB + L2** | 免费 | 低 | 低 | ⭐⭐ |

**建议**:

**生产环境（云）**: 使用云厂商 LoadBalancer
```
优点:
✅ 自动分配公网 IP
✅ DDoS 防护
✅ SSL 卸载
✅ 全球加速
✅ 高可用
✅ 运维成本低

成本:
- AWS NLB: ~$20/月
- 阿里云 SLB: ~¥56/月
```

**生产环境（裸机/IDC）**: 使用 MetalLB + BGP
```
优点:
✅ 完全控制
✅ 不依赖云厂商
✅ 真正的 LoadBalancer

缺点:
❌ 配置复杂
❌ 需要 BGP 路由器支持
❌ 运维成本高
```

**测试/开发环境**: 使用 MetalLB + L2
```
优点:
✅ 免费
✅ 配置简单

缺点:
❌ L2 模式无法跨网段
❌ 单点故障
```

**相关文档**: [03-complete-guide.md](./03-complete-guide.md) 第 1 节

---

### Q18: 如何配置 HTTPS？

**方案对比**:

| 方案 | 成本 | 自动续期 | 推荐度 |
|------|------|---------|--------|
| **Let's Encrypt** | 免费 | ✅ | ⭐⭐⭐⭐⭐ |
| **云厂商证书** | ¥200-2000/年 | ❌ | ⭐⭐⭐ |
| **自签名证书** | 免费 | ❌ | ⭐（仅测试） |

**方案 1: Let's Encrypt（推荐）**

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
kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - www.example.com
    secretName: myapp-tls-cert
  rules:
  - host: www.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: myapp-service
            port:
              number: 80
EOF

# 4. 验证证书签发
kubectl get certificate
```

**相关文档**: [03-complete-guide.md](./03-complete-guide.md) 第 4 节

---

### Q19: 生产环境需要多少个副本？

**建议**:

```
小型应用（< 1000 用户）:
├─ 应用副本: 2-3 个
├─ Ingress Controller: 2 个
└─ 资源限制: CPU 100m-500m, 内存 128Mi-512Mi

中型应用（1000-10000 用户）:
├─ 应用副本: 3-5 个
├─ Ingress Controller: 2-3 个
└─ 资源限制: CPU 500m-1000m, 内存 512Mi-1Gi

大型应用（> 10000 用户）:
├─ 应用副本: 5-10 个（HPA 自动扩展）
├─ Ingress Controller: 3+ 个（DaemonSet）
└─ 资源限制: CPU 1000m+, 内存 1Gi+
```

**配置示例**:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
spec:
  replicas: 3  # 生产环境至少 3 个
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0  # 滚动更新时 0 个不可用
```

**高可用配置**:
```yaml
spec:
  # 反亲和性（Pod 分散到不同节点）
  affinity:
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchExpressions:
            - key: app
              operator: In
              values:
              - myapp
          topologyKey: kubernetes.io/hostname
```

**相关文档**: [03-complete-guide.md](./03-complete-guide.md) 第 6 节

---

### Q20: 如何实现高可用？

**高可用架构**:

```
                        用户
                          │
                          ▼ DNS: www.example.com
              ┌───────────────────────┐
              │ 云 CDN（可选）         │
              └───────────────────────┘
                          │
                          ▼
              ┌───────────────────────┐
              │ 负载均衡器（主备）      │
              │  - LB1 (主)           │
              │  - LB2 (备)           │
              └───────────────────────┘
                          │
                          ▼
              ┌───────────────────────┐
              │ Kubernetes 集群       │
              │                        │
              │  ┌─────────────────┐  │
              │  │ Ingress Controller│ │
              │  │ - Controller-1   │  │
              │  │ - Controller-2   │  │
              │  │ - Controller-3   │  │
              │  └─────────────────┘  │
              │           │            │
              │  ┌─────────────────┐  │
              │  │ Service         │  │
              │  └─────────────────┘  │
              │           │            │
              │  ┌─────────────────┐  │
              │  │ Pods (×3)       │  │
              │  │ - Pod-1 (Node1) │  │
              │  │ - Pod-2 (Node2) │  │
              │  │ - Pod-3 (Node3) │  │
              │  └─────────────────┘  │
              └───────────────────────┘
```

**高可用配置清单**:

**1. 应用层高可用**
```yaml
# 至少 3 个副本
replicas: 3

# 反亲和性（分散到不同节点）
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app
            operator: In
            values:
            - myapp
        topologyKey: kubernetes.io/hostname
```

**2. Ingress Controller 高可用**
```yaml
# 2-3 个副本
replicas: 2

# 或使用 DaemonSet（每个节点一个）
kind: DaemonSet
```

**3. 负载均衡器高可用**
```yaml
# 云厂商自动处理（主备）
# 或使用 keepalived + 虚拟 IP
```

**4. 健康检查**
```yaml
# Liveness Probe（存活探针）
livenessProbe:
  httpGet:
    path: /healthz
    port: http
  failureThreshold: 3  # 连续失败 3 次重启

# Readiness Probe（就绪探针）
readinessProbe:
  httpGet:
    path: /ready
    port: http
  failureThreshold: 3  # 连续失败 3 次移出 Service
```

**5. 资源限制**
```yaml
resources:
  requests:
    cpu: 100m      # 保证资源
    memory: 128Mi
  limits:
    cpu: 500m      # 限制资源
    memory: 512Mi
```

**6. 滚动更新**
```yaml
strategy:
  type: RollingUpdate
  rollingUpdate:
    maxSurge: 1        # 最多多 1 个 Pod
    maxUnavailable: 0  # 0 个 Pod 不可用
```

**7. 监控和告警**
```bash
# 安装 Prometheus + Grafana
# 配置告警规则
# - Pod 不可用
# - Endpoints 为空
# - 错误率 > 1%
# - 响应时间 > 1s
```

**相关文档**: [03-complete-guide.md](./03-complete-guide.md) 第 6 节

---

## 快速索引

### 按问题类型

| 问题类型 | 查看文档 |
|---------|---------|
| **不理解概念** | [01-concepts.md](./01-concepts.md) |
| **快速上手** | [02-quick-start.md](./02-quick-start.md) |
| **生产部署** | [03-complete-guide.md](./03-complete-guide.md) |
| **遇到问题** | [04-troubleshooting.md](./04-troubleshooting.md) |
| **需要配置模板** | [examples.md](./examples.md) |
| **需要查命令** | [quick-reference.md](./quick-reference.md) |

### 按学习路径

```
初学者:
01-concepts.md → 02-quick-start.md → 遇到问题查看 04-troubleshooting.md

进阶:
01-concepts.md → 02-quick-start.md → 03-complete-guide.md

查阅:
需要什么看什么，按需查阅
```

---

**返回**: [00-INDEX.md](./00-INDEX.md)

**祝你学习顺利！** 🚀
