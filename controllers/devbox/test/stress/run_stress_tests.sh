#!/bin/bash

# Devbox 压测执行脚本
# 自动化执行各种压测场景

set -e

# 配置参数
KUBECONFIG=${KUBECONFIG:-"$HOME/.kube/config"}
NAMESPACE=${NAMESPACE:-"devbox-stress-test"}
IMAGE=${IMAGE:-"ubuntu:22.04"}
CPU=${CPU:-"100m"}
MEMORY=${MEMORY:-"128Mi"}
STORAGE=${STORAGE:-"1Gi"}

# 测试场景配置
SCALE_TESTS=(100 300 500 1000)
CONCURRENT_LEVELS=(5 10 20 50)

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查环境
check_environment() {
    log_info "检查测试环境..."
    
    # 检查 kubectl
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl 未安装"
        exit 1
    fi
    
    # 检查集群连接
    if ! kubectl cluster-info &> /dev/null; then
        log_error "无法连接到 Kubernetes 集群"
        exit 1
    fi
    
    # 检查 devbox CRD
    if ! kubectl get crd devboxes.devbox.sealos.io &> /dev/null; then
        log_error "Devbox CRD 未安装"
        exit 1
    fi
    
    log_info "环境检查通过"
}

# 创建测试命名空间
setup_namespace() {
    log_info "设置测试命名空间: $NAMESPACE"
    
    kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
    
    # 设置资源配额（可选）
    cat << EOF | kubectl apply -f -
apiVersion: v1
kind: ResourceQuota
metadata:
  name: devbox-stress-quota
  namespace: $NAMESPACE
spec:
  hard:
    requests.cpu: "50"
    requests.memory: "50Gi"
    requests.storage: "500Gi"
    pods: "2000"
EOF
}

# 构建压测工具
build_stress_tool() {
    log_info "构建压测工具..."
    
    cd "$(dirname "$0")"
    
    if [ ! -f "devbox_stress_test.go" ]; then
        log_error "压测工具源码不存在"
        exit 1
    fi
    
    go mod init devbox-stress-test 2>/dev/null || true
    go mod tidy
    go build -o devbox-stress-test devbox_stress_test.go
    
    log_info "压测工具构建完成"
}

# 执行规模测试
run_scale_tests() {
    log_info "开始执行规模测试..."
    
    for count in "${SCALE_TESTS[@]}"; do
        log_info "执行规模测试: $count 个 devbox"
        
        # 启动监控
        ./monitor.sh 1800 15 &  # 30分钟监控，15秒间隔
        monitor_pid=$!
        
        # 执行测试
        ./devbox-stress-test \
            -kubeconfig="$KUBECONFIG" \
            -namespace="$NAMESPACE" \
            -test-type=scale \
            -devbox-count=$count \
            -create-interval=2s \
            -timeout=30m \
            -image="$IMAGE" \
            -cpu="$CPU" \
            -memory="$MEMORY" \
            -storage="$STORAGE" \
            -cleanup=true
        
        # 停止监控
        kill $monitor_pid 2>/dev/null || true
        wait $monitor_pid 2>/dev/null || true
        
        log_info "规模测试 $count 完成"
        
        # 等待集群恢复
        log_info "等待集群恢复..."
        sleep 60
    done
}

# 执行并发测试
run_concurrent_tests() {
    log_info "开始执行并发测试..."
    
    for concurrent in "${CONCURRENT_LEVELS[@]}"; do
        for count in 100 200; do
            log_info "执行并发测试: $concurrent 并发创建 $count 个 devbox"
            
            # 启动监控
            ./monitor.sh 900 10 &  # 15分钟监控，10秒间隔
            monitor_pid=$!
            
            # 执行测试
            ./devbox-stress-test \
                -kubeconfig="$KUBECONFIG" \
                -namespace="$NAMESPACE" \
                -test-type=concurrent \
                -devbox-count=$count \
                -concurrent=$concurrent \
                -timeout=15m \
                -image="$IMAGE" \
                -cpu="$CPU" \
                -memory="$MEMORY" \
                -storage="$STORAGE" \
                -cleanup=true
            
            # 停止监控
            kill $monitor_pid 2>/dev/null || true
            wait $monitor_pid 2>/dev/null || true
            
            log_info "并发测试 $concurrent 并发 $count devbox 完成"
            
            # 等待集群恢复
            log_info "等待集群恢复..."
            sleep 30
        done
    done
}

# 执行容量测试（寻找集群上限）
run_capacity_test() {
    log_info "开始执行容量测试..."
    
    local current_count=500
    local step=100
    local max_attempts=10
    local attempts=0
    
    while [ $attempts -lt $max_attempts ]; do
        log_info "容量测试: 尝试创建 $current_count 个 devbox"
        
        # 启动监控
        ./monitor.sh 2400 20 &  # 40分钟监控，20秒间隔
        monitor_pid=$!
        
        # 执行测试
        if ./devbox-stress-test \
            -kubeconfig="$KUBECONFIG" \
            -namespace="$NAMESPACE" \
            -test-type=scale \
            -devbox-count=$current_count \
            -create-interval=1s \
            -timeout=40m \
            -image="$IMAGE" \
            -cpu="$CPU" \
            -memory="$MEMORY" \
            -storage="$STORAGE" \
            -cleanup=true; then
            
            log_info "成功创建 $current_count 个 devbox"
            current_count=$((current_count + step))
        else
            log_warn "创建 $current_count 个 devbox 失败，集群容量上限约为 $((current_count - step))"
            break
        fi
        
        # 停止监控
        kill $monitor_pid 2>/dev/null || true
        wait $monitor_pid 2>/dev/null || true
        
        attempts=$((attempts + 1))
        
        # 等待集群恢复
        log_info "等待集群恢复..."
        sleep 120
    done
    
    log_info "容量测试完成，估计集群最大容量: $((current_count - step)) 个 devbox"
}

# 压力测试（持续负载）
run_stress_test() {
    log_info "开始执行压力测试..."
    
    local duration=3600  # 1小时持续压力测试
    
    log_info "压力测试: 持续 1 小时创建和删除 devbox"
    
    # 启动监控
    ./monitor.sh $duration 30 &  # 1小时监控，30秒间隔
    monitor_pid=$!
    
    # 启动持续压力测试
    local end_time=$(($(date +%s) + duration))
    local test_round=1
    
    while [ $(date +%s) -lt $end_time ]; do
        log_info "压力测试轮次 $test_round"
        
        # 创建一批 devbox
        ./devbox-stress-test \
            -kubeconfig="$KUBECONFIG" \
            -namespace="$NAMESPACE" \
            -test-type=concurrent \
            -devbox-count=50 \
            -concurrent=10 \
            -timeout=5m \
            -image="$IMAGE" \
            -cpu="$CPU" \
            -memory="$MEMORY" \
            -storage="$STORAGE" \
            -cleanup=true
        
        test_round=$((test_round + 1))
        
        # 短暂休息
        sleep 30
    done
    
    # 停止监控
    kill $monitor_pid 2>/dev/null || true
    wait $monitor_pid 2>/dev/null || true
    
    log_info "压力测试完成"
}

# 生成最终报告
generate_final_report() {
    log_info "生成最终测试报告..."
    
    local report_dir="./stress_test_final_report_$(date +%Y%m%d_%H%M%S)"
    mkdir -p "$report_dir"
    
    # 收集所有测试结果
    find ./stress_test_results -name "*.log" -o -name "*.md" | while read -r file; do
        cp "$file" "$report_dir/"
    done
    
    # 生成汇总报告
    cat > "$report_dir/FINAL_REPORT.md" << EOF
# Devbox 压测最终报告

## 测试概述
- 测试时间: $(date)
- 测试环境: Kubernetes 集群
- 测试命名空间: $NAMESPACE
- Devbox 镜像: $IMAGE
- 资源配置: CPU($CPU), Memory($MEMORY), Storage($STORAGE)

## 测试场景
1. 规模测试: $(printf "%s " "${SCALE_TESTS[@]}")个 devbox
2. 并发测试: $(printf "%s " "${CONCURRENT_LEVELS[@]}")个并发级别
3. 容量测试: 寻找集群最大容量
4. 压力测试: 1小时持续负载

## 测试结果分析
请查看各个测试结果文件获取详细信息。

## 关键指标
- 最大 devbox 容量: 待分析
- 最大并发创建能力: 待分析
- 平均创建时间: 待分析
- 系统瓶颈分析: 待分析

## 建议
基于测试结果，建议：
1. 监控 etcd 存储增长
2. 关注节点资源使用
3. 优化 devbox 创建流程
4. 调整控制器并发参数

EOF

    log_info "最终报告已生成: $report_dir/FINAL_REPORT.md"
}

# 清理测试环境
cleanup() {
    log_info "清理测试环境..."
    
    # 删除测试命名空间
    kubectl delete namespace "$NAMESPACE" --ignore-not-found=true
    
    log_info "清理完成"
}

# 显示帮助信息
show_help() {
    cat << EOF
Devbox 压测工具

用法: $0 [选项] [测试类型]

测试类型:
  scale      - 规模测试
  concurrent - 并发测试
  capacity   - 容量测试
  stress     - 压力测试
  all        - 执行所有测试 (默认)

选项:
  -h, --help     显示帮助信息
  -n, --namespace NAMESPACE  指定测试命名空间 (默认: devbox-stress-test)
  -i, --image IMAGE         指定 devbox 镜像 (默认: ubuntu:22.04)
  --cpu CPU                 指定 CPU 资源 (默认: 100m)
  --memory MEMORY           指定内存资源 (默认: 128Mi)
  --storage STORAGE         指定存储资源 (默认: 1Gi)
  --cleanup                 测试后清理环境

示例:
  $0 scale                  # 只执行规模测试
  $0 --namespace test all   # 在 test 命名空间执行所有测试
  $0 --cpu 200m concurrent  # 使用 200m CPU 执行并发测试

EOF
}

# 主函数
main() {
    local test_type="all"
    local cleanup_after=false
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            -i|--image)
                IMAGE="$2"
                shift 2
                ;;
            --cpu)
                CPU="$2"
                shift 2
                ;;
            --memory)
                MEMORY="$2"
                shift 2
                ;;
            --storage)
                STORAGE="$2"
                shift 2
                ;;
            --cleanup)
                cleanup_after=true
                shift
                ;;
            scale|concurrent|capacity|stress|all)
                test_type="$1"
                shift
                ;;
            *)
                log_error "未知参数: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    log_info "Devbox 压测开始"
    log_info "测试类型: $test_type"
    log_info "命名空间: $NAMESPACE"
    log_info "镜像: $IMAGE"
    log_info "资源配置: CPU($CPU), Memory($MEMORY), Storage($STORAGE)"
    
    # 执行测试前检查
    check_environment
    setup_namespace
    build_stress_tool
    
    # 根据测试类型执行相应测试
    case $test_type in
        scale)
            run_scale_tests
            ;;
        concurrent)
            run_concurrent_tests
            ;;
        capacity)
            run_capacity_test
            ;;
        stress)
            run_stress_test
            ;;
        all)
            run_scale_tests
            run_concurrent_tests
            run_capacity_test
            run_stress_test
            ;;
    esac
    
    # 生成最终报告
    generate_final_report
    
    # 清理环境
    if [ "$cleanup_after" = true ]; then
        cleanup
    fi
    
    log_info "Devbox 压测完成"
}

# 信号处理
trap cleanup INT TERM

# 启动主函数
main "$@"
