# 快速开始：5 步部署 Ingress 应用

> 最小可运行案例，从零到域名可访问

**预计时间**: 15 分钟 + 实践时间

---

## 📋 前置条件

### 检查环境

```bash
# 1. 检查 Kubernetes 集群
kubectl cluster-info
# 预期: Kubernetes control plane is running at ...

# 2. 检查节点
kubectl get nodes
# 预期: STATUS = Ready

# 3. 检查是否有污点
kubectl describe nodes | grep Taints
# 如果有污点，记录下来（后面需要容忍）
```

---

## 步骤 1: 部署应用

### 创建 Deployment

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
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
        # 使用国内镜像
        image: registry.cn-hangzhou.aliyuncs.com/google_containers/nginx:1.25
        ports:
        - containerPort: 80
          name: http
        livenessProbe:
          httpGet:
            path: /
            port: http
          initialDelaySeconds: 30
        readinessProbe:
          httpGet:
            path: /
            port: http
          initialDelaySeconds: 10
```

```bash
# 部署
kubectl apply -f deployment.yaml

# 验证
kubectl get pods -l app=myapp

# 预期输出:
# NAME                     READY   STATUS    RESTARTS   AGE
# myapp-xxx-xxx            1/1     Running   0          30s
```

### 验证 Pod

```bash
# 获取 Pod 名称
POD=$(kubectl get pods -l app=myapp -o jsonpath='{.items[0].metadata.name}')

# 进入 Pod 测试
kubectl exec -it $POD -- /bin/sh

# 在 Pod 内执行:
wget -O- -q http://localhost/
# 应该返回 HTML 内容
exit
```

---

## 步骤 2: 创建 Service

### 问题：Endpoints 为空

**原因**: Service 的 targetPort 使用了端口名称，但 Pod 没有给端口命名

**解决**: 直接使用端口号（推荐）

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
  - port: 80
    targetPort: 80  # 直接使用数字，不用名称
  type: ClusterIP
```

```bash
# 创建 Service
kubectl apply -f service.yaml

# 验证
kubectl get svc myapp-service
kubectl get endpoints myapp-service

# 预期输出:
# NAME            ENDPOINTS                          AGE
# myapp-service   10.0.0.203:80,10.0.0.239:80        10s
#                            ↑ 必须有 IP！
```

### 测试 Service

```bash
# 集群内测试
kubectl run test --image=busybox:1.28 --rm -it --restart=Never -- \
  wget -O- -q http://myapp-service/
```

---

## 步骤 3: 安装 Ingress Controller

### 问题：节点有污点

**症状**: Pod 一直 Pending

**解决**: 给 Deployment 和 Job 添加容忍

```bash
# 安装 Ingress Controller
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.9.4/deploy/static/provider/cloud/deploy.yaml

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

# 等待 Pod 就绪
kubectl get pods -n ingress-nginx -w
```

**预期输出**:
```
NAME                                       READY   STATUS      RESTARTS   AGE
ingress-nginx-admission-create-xxx        0/1     Completed   0          1m
ingress-nginx-admission-patch-xxx         0/1     Completed   0          1m
ingress-nginx-controller-xxx              1/1     Running     0          2m
```

---

## 步骤 4: 创建 Ingress

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
# 创建 Ingress
kubectl apply -f ingress.yaml

# 验证
kubectl get ingress myapp-ingress
```

---

## 步骤 5: 配置访问

### 方案 A: 使用 MetalLB（推荐）

```bash
# 1. 安装 MetalLB
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/main/config/manifests/metallb-native.yaml

# 2. 配置 IP 池
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

# 3. 配置 L2 广播
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

# 4. 修改 Service 为 LoadBalancer
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"LoadBalancer"}}'

# 5. 获取 LoadBalancer IP
LB_IP=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "LoadBalancer IP: $LB_IP"

# 6. 配置 DNS
echo "$LB_IP myapp.local" | sudo tee -a /etc/hosts

# 7. 测试访问
curl http://myapp.local/
```

### 方案 B: 使用 NodePort（替代方案）

```bash
# 1. 修改为 NodePort
kubectl patch svc ingress-nginx-controller -n ingress-nginx -p '{"spec":{"type":"NodePort"}}'

# 2. 获取节点 IP 和 NodePort
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
NODE_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[0].nodePort}')

# 3. 配置 DNS
echo "$NODE_IP myapp.local" | sudo tee -a /etc/hosts

# 4. 测试访问（带端口）
curl http://myapp.local:$NODE_PORT/
```

---

## ✅ 验证清单

```bash
#!/bin/bash

echo "=== 验证清单 ==="

# 1. Pod
echo -e "\n1. Pod 状态"
kubectl get pods -l app=myapp

# 2. Service
echo -e "\n2. Service Endpoints"
kubectl get endpoints myapp-service

# 3. Ingress Controller
echo -e "\n3. Ingress Controller"
kubectl get pods -n ingress-nginx

# 4. LoadBalancer
echo -e "\n4. LoadBalancer IP"
kubectl get svc -n ingress-nginx ingress-nginx-controller

# 5. Ingress
echo -e "\n5. Ingress ADDRESS"
kubectl get ingress myapp-ingress

# 6. 测试访问
echo -e "\n6. 测试访问"
LB_IP=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
if [ -n "$LB_IP" ]; then
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://myapp.local/)
    if [ "$HTTP_CODE" = "200" ]; then
        echo "✓ 访问成功！"
    else
        echo "✗ 访问失败 (HTTP $HTTP_CODE)"
    fi
else
    echo "! 使用 NodePort 方案"
    NODE_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[0].nodePort}')
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://myapp.local:$NODE_PORT/)
    echo "HTTP 状态码: $HTTP_CODE"
fi

echo -e "\n=== 完成 ==="
```

---

## 🐛 常见问题

### 问题 1: Pod 无法启动

**症状**: `CrashLoopBackOff`

**解决**:
```bash
# 查看日志
kubectl logs <pod-name>

# 查看镜像
kubectl get pod <pod-name> -o jsonpath='{.spec.containers[0].image}'

# 使用正确的镜像
kubectl set image deployment/myapp myapp=registry.cn-hangzhou.aliyuncs.com/google_containers/nginx:1.25
```

### 问题 2: Endpoints 为空

**解决**:
```bash
# 检查标签
kubectl get pods --show-labels
kubectl get svc myapp-service -o jsonpath='{.spec.selector}'

# 确保一致：app=myapp（小写）
```

### 问题 3: Ingress Controller 无法调度

**解决**:
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

### 问题 4: 集群外无法访问

**解决**:
```bash
# 检查是否在同一网段
kubectl get nodes -o wide
ip addr show

# 如果不同网段，使用 NodePort 方案
```

---

## 📊 成功标志

执行完所有步骤后，你应该看到：

```bash
$ kubectl get pods -l app=myapp
NAME                     READY   STATUS    RESTARTS   AGE
myapp-xxx-xxx            1/1     Running   0          5m

$ kubectl get endpoints myapp-service
NAME            ENDPOINTS                          AGE
myapp-service   10.0.0.203:80,10.0.0.239:80        5m

$ kubectl get ingress myapp-ingress
NAME            CLASS   HOSTS         ADDRESS          PORTS   AGE
myapp-ingress   nginx   myapp.local   192.168.12.200   80      5m

$ curl http://myapp.local/
<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
...
```

---

## 🎯 下一步

✅ 完成快速开始后，你可以：

1. **深入学习**: [01-concepts.md](./01-concepts.md)
2. **生产部署**: [03-complete-guide.md](./03-complete-guide.md)
3. **解决问题**: [04-troubleshooting.md](./04-troubleshooting.md)

---

**预计总时间**: 15-30 分钟

**祝学习顺利！** 🚀
