# 故障排查手册

> Kubernetes Ingress 常见问题的系统化解决方案

---

## 📋 问题目录

### 问题 1: Service Endpoints 为空
### 问题 2: Pod 无法调度（污点问题）
### 问题 3: Secret 未找到
### 问题 4: Ingress ADDRESS 为空
### 问题 5: 集群外无法访问

---

## 通用诊断流程

```
无法访问应用
    │
    ▼
1. 检查 Pod 状态
kubectl get pods -l app=<app-name>
    │
    ├─→ Pod 不正常 → 查看日志、describe
    │
    ▼ Pod 正常
2. 检查 Endpoints
kubectl get endpoints <service-name>
    │
    ├─→ Endpoints 为空 → 问题 1
    │
    ▼ Endpoints 正常
3. 检查 Service 连通性
kubectl run test --rm -it -- wget -O- http://<service-name>/
    │
    ├─→ 无法访问 → 检查端口配置
    │
    ▼ Service 正常
4. 检查 Ingress
kubectl get ingress <ingress-name>
kubectl describe ingress <ingress-name>
    │
    ├─→ ADDRESS 为空 → 问题 4
    │
    ▼ Ingress 正常
5. 检查 Ingress Controller
kubectl get pods -n ingress-nginx
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx
    │
    ├─→ Controller 不正常 → 查看日志
    │
    ▼ Controller 正常
6. 检查 DNS
ping <domain>
nslookup <domain>
cat /etc/hosts | grep <domain>
    │
    ├─→ DNS 无法解析 → 配置 DNS
    │
    ▼ DNS 正常
7. 检查网络连通性
ping <LoadBalancer IP>
telnet <LoadBalancer IP> 80
    │
    └─→ 无法连接 → 检查防火墙
```

---

## 问题 1: Service Endpoints 为空

### 症状

```bash
kubectl get endpoints myapp-service
# NAME            ENDPOINTS   AGE
# myapp-service   <none>      10m
```

### 原因分析

**原因 1**: Service 的 selector 与 Pod 的 labels 不匹配

**原因 2**: 端口配置错误（targetPort 使用了不存在的端口名称）

**原因 3**: Pod 还没有完全启动（Not Ready）

### 诊断步骤

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

### 解决方案

#### 方案 1: 修正标签（推荐重新创建）

```bash
kubectl delete deployment myapp
kubectl delete service myapp-service

# 确保标签完全一致，重新创建
```

#### 方案 2: 修复 Service 端口

```bash
# 使用端口号而不是名称
kubectl patch svc myapp-service -p '{"spec":{"ports":[{"name":"http","port":80,"targetPort":80}]}}'
```

#### 方案 3: 给 Pod 端口命名

```bash
kubectl patch deployment myapp -p '{"spec":{"template":{"spec":{"containers":[{"name":"myapp","ports":[{"containerPort":80,"name":"http"}]}}}}'
```

---

## 问题 2: Pod 无法调度（污点问题）

### 症状

```bash
kubectl get pods
# NAME                     READY   STATUS    RESTARTS   AGE
# ingress-nginx-xxx        0/1     Pending   0          5m

kubectl describe pod ingress-nginx-xxx
# Warning  FailedScheduling  0/2 nodes are available: 2 node(s) had untolerated taint {devbox.sealos.io/node: }.
```

### 原因

节点有污点（Taint），Pod 没有对应的容忍（Toleration）。

### 诊断步骤

```bash
# 1. 查看污点
kubectl describe nodes | grep Taints

# 预期输出:
# Taints: devbox.sealos.io/node:NoSchedule

# 2. 查看是否有容忍
kubectl get pod <pod-name> -o yaml | grep -A 5 tolerations
```

### 解决方案

```bash
# 添加容忍
kubectl patch deployment <deployment-name> --type=json -p='[
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

# 或容忍所有污点（不推荐生产）
kubectl patch deployment <deployment-name> -p '{"spec":{"template":{"spec":{"tolerations":[{"operator":"Exists"}]}}}'
```

---

## 问题 3: Secret 未找到

### 症状

```bash
kubectl get pods -n ingress-nginx
# NAME                                        READY   STATUS              RESTARTS   AGE
# ingress-nginx-controller-xxx                0/1     ContainerCreating   0          5m

kubectl describe pod ingress-nginx-controller-xxx
# Warning  FailedMount  secret "ingress-nginx-admission" not found
```

### 原因链

```
1. admission-create Job 无法调度（没有容忍污点）
   ↓
2. Job 无法运行，Secret 未创建
   ↓
3. Controller Pod 无法挂载 Secret
   ↓
4. Pod 卡在 ContainerCreating
```

### 诊断步骤

```bash
# 1. 检查 Job
kubectl get job -n ingress-nginx

# 预期输出（如果有问题）:
# NAME                             COMPLETIONS   AGE
# ingress-nginx-admission-create   0/1           10m

# 2. 检查 Secret
kubectl get secret -n ingress-nginx ingress-nginx-admission

# 预期输出（如果不存在）:
# Error from server (NotFound): secrets "ingress-nginx-admission" not found

# 3. 检查 Job 的调度
kubectl describe pod -n ingress-nginx ingress-nginx-admission-create-xxx
# Warning  FailedScheduling  ...
```

### 解决方案

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

# 4. Pod 会自动重启
kubectl get pods -n ingress-nginx
```

---

## 问题 4: Ingress ADDRESS 为空

### 症状

```bash
kubectl get ingress myapp-ingress
# NAME            CLASS   HOSTS         ADDRESS   PORTS   AGE
# myapp-ingress   nginx   myapp.local             80      10m
#                                         ↑ 空的
```

### 原因

**这是正常的！** 在裸机/本地环境中，如果不使用 MetalLB 或云厂商 LoadBalancer，ADDRESS 字段会是空的。

### 原理解释

| Service 类型 | ADDRESS 字段 | 环境 |
|-------------|-------------|------|
| **LoadBalancer**（云） | 公网 IP | AWS/阿里云/Azure |
| **LoadBalancer**（MetalLB） | 分配的 IP | 本地/裸机 |
| **NodePort** | 节点 IP 或空 | 本地/裸机 |
| **ClusterIP** | 空 | 本地/裸机 |

### 诊断步骤

```bash
# 1. 检查 Service 类型
kubectl get svc -n ingress-nginx ingress-nginx-controller

# 预期输出（如果是 ClusterIP）:
# TYPE        EXTERNAL-IP
# ClusterIP   <none>

# 预期输出（如果是 LoadBalancer）:
# TYPE           EXTERNAL-IP
# LoadBalancer   192.168.12.200

# 2. 检查是否安装了 MetalLB
kubectl get pods -n metallb-system

# 3. 检查 IP 地址池
kubectl get ipaddresspool -n metallb-system
```

### 解决方案

#### 方案 1: 使用 MetalLB（推荐）

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

# 配置 L2 广播
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

# 修改 Service 类型
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"LoadBalancer"}}'

# 等待 IP 分配
sleep 10
kubectl get svc -n ingress-nginx ingress-nginx-controller
```

#### 方案 2: 使用 NodePort（替代方案）

```bash
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"NodePort"}}'

# 使用 节点IP:NodePort 访问
```

---

## 问题 5: 集群外无法访问

### 症状

```bash
# 集群内访问正常
kubectl run test --rm -it -- wget -O- http://myapp-service/
# ✓ 成功

# 集群外访问失败
# 在另一台机器上配置了 /etc/hosts
echo "192.168.12.200 myapp.local" >> /etc/hosts
curl http://myapp.local/
# ✗ 失败: Connection timed out
```

### 原因

**MetalLB L2 模式的限制**: ARP 广播无法跨越网段。

### 诊断步骤

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

### 解决方案

#### 同一网段但仍无法访问

```bash
# 检查防火墙
sudo iptables -L -n | grep -E "80|443"

# 开放端口
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# 或
sudo iptables -I INPUT -p tcp --dport 80 -j ACCEPT
sudo iptables -I INPUT -p tcp --dport 443 -j ACCEPT

# 重启 Speaker
kubectl delete pod -n metallb-system -l app.kubernetes.io/component=speaker
```

#### 不同网段

**方案 A: 改用 NodePort**（最简单）

```bash
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"NodePort"}}'

NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
NODE_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[0].nodePort}')

echo "$NODE_IP myapp.local" | sudo tee -a /etc/hosts

# 访问（带端口）
http://myapp.local:$NODE_PORT/
```

**方案 B: 使用反向代理**

在 K8s 节点上配置 Nginx：

```bash
sudo apt install nginx -y

cat <<EOF | sudo tee /etc/nginx/sites-available/k8s-ingress
upstream k8s_ingress {
    server 192.168.12.200:80;
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

---

## 🛠️ 常用诊断命令

### 快速诊断脚本

```bash
#!/bin/bash

echo "=== 快速诊断 ==="

POD_NAME="myapp-xxx-xxx"
SVC_NAME="myapp-service"
INGRESS_NAME="myapp-ingress"
NAMESPACE="default"

# 1. Pod
echo -e "\n1. Pod 状态"
kubectl get pods -l app=myapp

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

---

## 📊 问题总结

| 问题 | 症状 | 原因 | 解决方案 |
|------|------|------|----------|
| **Endpoints 为空** | `kubectl get endpoints` 显示 `<none>` | labels/selector 不匹配或端口配置错误 | 确保标签一致，端口名称匹配或使用端口号 |
| **Pod Pending** | Pod 一直处于 Pending 状态 | 节点有污点，Pod 没有容忍 | 添加 tolerations |
| **Secret 未找到** | Controller 无法挂载 Secret | init Job 无法调度 | 给 Job 也添加容忍 |
| **ADDRESS 为空** | Ingress 的 ADDRESS 字段为空 | 没有配置 LoadBalancer | 使用 MetalLB 或 NodePort |
| **集群外无法访问** | 同网段可访问，不同网段无法 | MetalLB L2 模式无法跨越网段 | 使用 NodePort 或反向代理 |

---

## 🎯 预防措施

### 1. 标签命名规范

```yaml
# 统一使用小写
metadata:
  labels:
    app: myapp    # ✓ 小写
    # App: myapp   # ✗ 大写

spec:
  selector:
    app: myapp    # ✓ 一致
```

### 2. 端口配置规范

```yaml
# 推荐使用端口号，避免端口名称
ports:
- port: 80
  targetPort: 80  # ✓ 直接使用数字
```

### 3. 污点容忍规范

```yaml
# 如果环境有污点，在所有 Deployment/Job 中添加
tolerations:
- key: devbox.sealos.io/node
  operator: Exists
  effect: NoSchedule
```

### 4. 监控和告警

```bash
# 定期检查关键资源
kubectl get pods -A | grep -v Running
kubectl get endpoints -A
kubectl get ingress
```

---

**下一步**: [05-faq.md](./05-faq.md) 或返回 [01-concepts.md](./01-concepts.md)
