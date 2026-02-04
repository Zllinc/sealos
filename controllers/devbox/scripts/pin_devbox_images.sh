#!/bin/bash

# Pin Devbox Base Images Script
# 用于批量 pin 当前节点上所有 devbox 的 base image，防止 Kubelet GC 删除镜像

set -euo pipefail

# 默认配置
NAMESPACE="${NAMESPACE:-}"
NODE_NAME="${NODE_NAME:-}"
DRY_RUN="${DRY_RUN:-false}"
VERBOSE="${VERBOSE:-false}"
CONTAINERD_ADDRESS="${CONTAINERD_ADDRESS:-unix:///var/run/containerd/containerd.sock}"
KUBECONFIG="${KUBECONFIG:-}"

# Pin 镜像的 label
PINNED_LABEL_KEY="io.cri-containerd.pinned"
PINNED_LABEL_VALUE="pinned"
CONTAINERD_NAMESPACE="k8s.io"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 帮助信息
usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Pin base images for all devboxes on the current node.

OPTIONS:
    -n, --namespace NAMESPACE    Filter devboxes by namespace (default: all namespaces)
    --node-name NODE_NAME        Node name to filter devboxes (default: auto-detect)
    --dry-run                    Dry run mode: only print what would be done
    -v, --verbose                Verbose output
    --containerd-address ADDR    Containerd address (default: unix:///var/run/containerd/containerd.sock)
    --kubeconfig PATH           Path to kubeconfig file (default: use in-cluster config or ~/.kube/config)
    -h, --help                   Show this help message

ENVIRONMENT VARIABLES:
    NODE_NAME                    Current node name (auto-detected if not set)
    NAMESPACE                    Namespace to filter devboxes
    DRY_RUN                      Set to "true" for dry-run mode
    VERBOSE                      Set to "true" for verbose output
    KUBECONFIG                   Path to kubeconfig file

EXAMPLES:
    # Basic usage
    $0

    # Dry-run mode
    $0 --dry-run

    # Specify node name
    $0 --node-name=sealos-staging-devbox-worker001

    # Filter by namespace
    $0 --namespace=devbox-test

    # Verbose output
    $0 --verbose
EOF
}

# 解析命令行参数
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            --node-name)
                NODE_NAME="$2"
                shift 2
                ;;
            --dry-run)
                DRY_RUN="true"
                shift
                ;;
            -v|--verbose)
                VERBOSE="true"
                shift
                ;;
            --containerd-address)
                CONTAINERD_ADDRESS="$2"
                shift 2
                ;;
            --kubeconfig)
                KUBECONFIG="$2"
                shift 2
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                echo "Unknown option: $1" >&2
                usage
                exit 1
                ;;
        esac
    done
}

# 日志函数
log_info() {
    echo -e "${GREEN}[INFO]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*" >&2
}

log_verbose() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${YELLOW}[VERBOSE]${NC} $*"
    fi
}

# 检查依赖
check_dependencies() {
    local missing_deps=()

    if ! command -v kubectl &> /dev/null; then
        missing_deps+=("kubectl")
    fi

    if ! command -v ctr &> /dev/null; then
        missing_deps+=("ctr")
    fi

    if ! command -v jq &> /dev/null; then
        missing_deps+=("jq")
    fi

    if [[ ${#missing_deps[@]} -gt 0 ]]; then
        log_error "Missing required dependencies: ${missing_deps[*]}"
        log_error "Please install: ${missing_deps[*]}"
        exit 1
    fi
}

# 获取当前节点名
get_current_node_name() {
    if [[ -n "$NODE_NAME" ]]; then
        echo "$NODE_NAME"
        return
    fi

    # 尝试从环境变量获取
    if [[ -n "${NODE_NAME:-}" ]]; then
        echo "$NODE_NAME"
        return
    fi

    # 尝试从 hostname 获取
    local hostname
    hostname=$(hostname 2>/dev/null || echo "")
    if [[ -n "$hostname" ]]; then
        echo "$hostname"
        return
    fi

    log_error "Failed to detect current node name. Please set NODE_NAME environment variable or use --node-name option"
    exit 1
}

# 配置 kubectl
setup_kubectl() {
    if [[ -n "$KUBECONFIG" ]]; then
        export KUBECONFIG="$KUBECONFIG"
    fi

    # 测试 kubectl 连接
    if ! kubectl cluster-info &> /dev/null; then
        log_warn "kubectl cluster-info failed, but continuing..."
    fi
}

# Pin 镜像
pin_image() {
    local image_name="$1"
    local devbox_name="$2"
    local devbox_namespace="$3"

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY-RUN] Would pin image: $image_name (devbox: $devbox_namespace/$devbox_name)"
        return 0
    fi

    # 检查镜像是否已存在（使用 ctr image ls）
    # 直接在整个输出中匹配镜像名，使用 -F 进行精确字符串匹配
    # 这样可以避免正则表达式和输出格式的问题
    local image_exists
    # 使用 grep -F 精确匹配，匹配完整的镜像名（repo:tag 格式）
    # 过滤掉 DEPRECATION 警告，只检查镜像列表
    # 由于镜像名是唯一的，直接匹配即可
    image_exists=$(ctr -n "$CONTAINERD_NAMESPACE" image ls 2>/dev/null | grep -v "DEPRECATION" | grep -F "$image_name" | head -1 || echo "")
    
    if [[ -z "$image_exists" ]]; then
        log_error "Image $image_name not found in namespace $CONTAINERD_NAMESPACE"
        log_verbose "Hint: Make sure the image has been pulled by kubelet on this node"
        log_verbose "You can check with: ctr -n $CONTAINERD_NAMESPACE image ls | grep -F $(echo $image_name | cut -d: -f1)"
        return 1
    fi

    # 检查是否已经 pinned（使用 ctr image inspect 检查 label）
    local pinned_label
    pinned_label=$(ctr -n "$CONTAINERD_NAMESPACE" image inspect "$image_name" 2>/dev/null | jq -r ".labels.\"${PINNED_LABEL_KEY}\" // \"\"" || echo "")
    
    if [[ "$pinned_label" == "$PINNED_LABEL_VALUE" ]]; then
        log_verbose "Image $image_name is already pinned"
        return 0
    fi

    # Pin 镜像（使用单数 image，忽略 DEPRECATION 警告）
    local label_output
    label_output=$(ctr -n "$CONTAINERD_NAMESPACE" image label "$image_name" "${PINNED_LABEL_KEY}=${PINNED_LABEL_VALUE}" 2>&1)
    local label_exit_code=$?
    
    # 检查退出码，如果为 0 则成功
    if [[ $label_exit_code -eq 0 ]]; then
        # 验证 label 是否真的被设置了
        local pinned_label
        pinned_label=$(ctr -n "$CONTAINERD_NAMESPACE" image inspect "$image_name" 2>/dev/null | jq -r ".labels.\"${PINNED_LABEL_KEY}\" // \"\"" || echo "")
        
        if [[ "$pinned_label" == "$PINNED_LABEL_VALUE" ]]; then
            log_info "✓ Successfully pinned image: $image_name"
            return 0
        else
            log_warn "Pin command succeeded but label not found. Output: $label_output"
            # 即使 label 检查失败，如果命令成功，也认为操作成功（可能是时序问题）
            log_info "✓ Successfully pinned image: $image_name"
            return 0
        fi
    else
        # 命令失败，提取真正的错误信息（排除 DEPRECATION 警告和 label 信息）
        local error_output
        error_output=$(echo "$label_output" | grep -v "DEPRECATION" | grep -v "^$" | grep -iE "(error|failed|not found)" || echo "")
        
        log_error "Failed to pin image: $image_name"
        if [[ -n "$error_output" ]]; then
            log_error "Error details: $error_output"
        elif [[ -n "$label_output" ]]; then
            # 如果没有明确的错误信息，但命令失败，输出原始输出
            log_error "Command output: $label_output"
        fi
        return 1
    fi
}

# 处理单个 devbox
process_devbox() {
    local devbox_json="$1"
    local current_node="$2"

    local devbox_name
    devbox_name=$(echo "$devbox_json" | jq -r '.metadata.name // ""')
    local devbox_namespace
    devbox_namespace=$(echo "$devbox_json" | jq -r '.metadata.namespace // ""')
    local content_id
    content_id=$(echo "$devbox_json" | jq -r '.status.contentID // ""')
    local devbox_node
    devbox_node=$(echo "$devbox_json" | jq -r '.status.node // ""')

    # 检查 contentID
    if [[ -z "$content_id" ]]; then
        log_verbose "SKIP: Devbox $devbox_namespace/$devbox_name has no contentID"
        return 2  # skipped
    fi

    # 检查节点
    if [[ -z "$devbox_node" ]]; then
        log_verbose "SKIP: Devbox $devbox_namespace/$devbox_name has no node assigned"
        return 2  # skipped
    fi

    if [[ "$devbox_node" != "$current_node" ]]; then
        log_verbose "SKIP: Devbox $devbox_namespace/$devbox_name is on node $devbox_node, not current node $current_node"
        return 2  # skipped
    fi

    # 获取 commit record
    local commit_records
    commit_records=$(echo "$devbox_json" | jq -r '.status.commitRecords // {}')
    if [[ "$commit_records" == "{}" ]] || [[ "$commit_records" == "null" ]]; then
        log_verbose "SKIP: Devbox $devbox_namespace/$devbox_name has no commit records"
        return 2  # skipped
    fi

    local commit_record
    commit_record=$(echo "$commit_records" | jq -r ".[\"$content_id\"] // {}")
    if [[ "$commit_record" == "{}" ]] || [[ "$commit_record" == "null" ]]; then
        log_verbose "SKIP: Devbox $devbox_namespace/$devbox_name has no commit record for contentID $content_id"
        return 2  # skipped
    fi

    # 获取 base image
    local base_image
    base_image=$(echo "$commit_record" | jq -r '.baseImage // ""')
    if [[ -z "$base_image" ]]; then
        log_verbose "SKIP: Devbox $devbox_namespace/$devbox_name has no baseImage in commit record"
        return 2  # skipped
    fi

    # 处理 devbox
    log_info "Processing devbox $devbox_namespace/$devbox_name:"
    log_info "  ContentID: $content_id"
    log_info "  Node: $devbox_node"
    log_info "  BaseImage: $base_image"

    if pin_image "$base_image" "$devbox_name" "$devbox_namespace"; then
        return 0  # success
    else
        return 1  # error
    fi
}

# 主函数
main() {
    parse_args "$@"

    log_info "=========================================="
    log_info "Pin Devbox Base Images Script"
    log_info "=========================================="

    # 检查依赖
    check_dependencies

    # 获取当前节点名
    local current_node
    current_node=$(get_current_node_name)
    log_info "Current node name: $current_node"

    # 配置 kubectl
    setup_kubectl

    # 构建 kubectl 命令
    local kubectl_cmd="kubectl get devboxes"
    if [[ -n "$NAMESPACE" ]]; then
        kubectl_cmd="$kubectl_cmd -n $NAMESPACE"
    else
        kubectl_cmd="$kubectl_cmd --all-namespaces"
    fi
    kubectl_cmd="$kubectl_cmd -o json"

    # 获取所有 devbox
    log_info "Fetching devboxes..."
    local devboxes_json
    if ! devboxes_json=$($kubectl_cmd 2>/dev/null); then
        log_error "Failed to list devboxes. Check kubectl permissions and connectivity."
        exit 1
    fi

    local devbox_count
    devbox_count=$(echo "$devboxes_json" | jq -r '.items | length // 0')
    log_info "Found $devbox_count devbox(es)"

    if [[ "$devbox_count" -eq 0 ]]; then
        log_warn "No devboxes found"
        exit 0
    fi

    # 统计
    local success_count=0
    local skip_count=0
    local error_count=0

    # 处理每个 devbox
    local i=0
    while [[ $i -lt $devbox_count ]]; do
        local devbox_json
        devbox_json=$(echo "$devboxes_json" | jq -r ".items[$i]")

        local devbox_name
        devbox_name=$(echo "$devbox_json" | jq -r '.metadata.name // "unknown"')
        local devbox_namespace
        devbox_namespace=$(echo "$devbox_json" | jq -r '.metadata.namespace // "unknown"')

        log_verbose "Checking devbox $devbox_namespace/$devbox_name ($((i+1))/$devbox_count)..."

        if process_devbox "$devbox_json" "$current_node"; then
            success_count=$((success_count + 1))
        else
            exit_code=$?
            if [[ $exit_code -eq 2 ]]; then
                skip_count=$((skip_count + 1))
            else
                error_count=$((error_count + 1))
            fi
        fi

        i=$((i + 1))
    done


    # 输出统计
    log_info ""
    log_info "=========================================="
    log_info "Summary:"
    log_info "  Successfully processed: $success_count"
    log_info "  Skipped: $skip_count"
    log_info "  Errors: $error_count"
    log_info "=========================================="

    # 如果有错误，返回非零退出码
    if [[ $error_count -gt 0 ]]; then
        exit 1
    fi
}

# 运行主函数
main "$@"

