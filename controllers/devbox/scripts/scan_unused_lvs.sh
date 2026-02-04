#!/bin/bash

# 扫描当前节点中未被 Devbox 使用的 LV（Logical Volume）
# LV 命名格式：devbox-{contentID}
# 如果 LV 对应的 contentID 不在任何 Devbox 的 Status.ContentID 中，则标记为可删除

set -euo pipefail

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

# 获取当前节点名称
get_current_node_name() {
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

    log_error "Failed to detect current node name. Please set NODE_NAME environment variable"
    exit 1
}

# 检查命令是否存在
check_command() {
    if ! command -v "$1" &> /dev/null; then
        log_error "Command '$1' not found. Please install it first."
        exit 1
    fi
}

# 获取所有 devbox LV
get_devbox_lvs() {
    # 打印日志到 stderr，避免混入数据流
    log_info "Scanning LVs on current node..." >&2
    
    # 使用 lvs 命令获取所有 LV，过滤出以 devbox- 开头的
    # 输出格式：LV_NAME VG_NAME ...
    local lvs_output
    lvs_output=$(lvs --noheadings --separator='|' -o lv_name,vg_name,lv_size,pool_lv,origin 2>/dev/null || echo "")
    
    if [[ -z "$lvs_output" ]]; then
        log_warn "No LVs found or lvs command failed"
        return
    fi
    
    # 过滤出 devbox- 开头且非 thinpool 的 LV
    # lvs 输出前面可能有空格，先去掉行首空格再匹配
    echo "$lvs_output" | sed 's/^ *//' | awk -F'|' '$1 ~ /^devbox-/ && $1 !~ /thinpool/ {print}' || true
}

# 从 LV 名称提取 contentID
extract_content_id_from_lv() {
    local lv_name="$1"
    # LV 格式：devbox-{contentID}
    # 去掉 devbox- 前缀
    echo "${lv_name#devbox-}"
}

# 获取当前节点的所有 Devbox 的 ContentID
get_devbox_content_ids() {
    local node_name="$1"
    
    log_info "Fetching Devboxes on node: $node_name"
    
    # 使用 kubectl 获取所有 Devbox
    # 过滤出 Status.Node 等于当前节点的
    local devboxes_json
    devboxes_json=$(kubectl get devboxes --all-namespaces -o json 2>/dev/null || echo '{"items":[]}')
    
    if [[ -z "$devboxes_json" ]] || [[ "$devboxes_json" == '{"items":[]}' ]]; then
        log_warn "No Devboxes found or kubectl command failed"
        echo ""
        return
    fi
    
    # 使用 jq 提取当前节点的 Devbox 的 ContentID
    # 需要检查 Status.Node 是否等于当前节点
    local content_ids
    content_ids=$(echo "$devboxes_json" | jq -r --arg node "$node_name" '
        .items[] | 
        select(.status.node == $node) | 
        .status.contentID // empty
    ' | grep -v '^$' | sort -u)
    
    echo "$content_ids"
}

# 主函数
main() {
    local dry_run=false
    local verbose=false
    local output_file="unused_lvs.txt"
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            --dry-run)
                dry_run=true
                shift
                ;;
            -v|--verbose)
                verbose=true
                shift
                ;;
            -o|--output)
                output_file="$2"
                shift 2
                ;;
            -h|--help)
                echo "Usage: $0 [OPTIONS]"
                echo ""
                echo "Scan LVs on current node and find unused ones (not referenced by any Devbox)"
                echo ""
                echo "Options:"
                echo "  --dry-run           Dry run mode: only print what would be deleted"
                echo "  -o, --output FILE   Output unused LV list to file (default: unused_lvs.txt)"
                echo "  -v, --verbose       Verbose output"
                echo "  -h, --help          Show this help message"
                echo ""
                echo "Environment variables:"
                echo "  NODE_NAME    Current node name (auto-detected from hostname if not set)"
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                echo "Use -h or --help for usage information"
                exit 1
                ;;
        esac
    done
    
    log_info "=========================================="
    log_info "Scan Unused Devbox LVs Script"
    log_info "=========================================="
    
    # 检查必要的命令
    check_command "lvs"
    check_command "kubectl"
    check_command "jq"
    
    # 获取当前节点名称
    local current_node
    current_node=$(get_current_node_name)
    log_info "Current node name: $current_node"
    
    # 获取所有 devbox LV
    local lvs_list
    lvs_list=$(get_devbox_lvs)
    
    if [[ -z "$lvs_list" ]]; then
        log_info "No devbox LVs found on this node"
        exit 0
    fi
    
    local lv_count
    lv_count=$(echo "$lvs_list" | wc -l)
    log_info "Found $lv_count devbox LV(s)"
    
    # 获取当前节点的所有 Devbox 的 ContentID
    local content_ids
    content_ids=$(get_devbox_content_ids "$current_node")
    
    local devbox_count
    devbox_count=$(echo "$content_ids" | grep -c . || echo "0")
    log_info "Found $devbox_count Devbox(es) on this node"
    
    if [[ "$verbose" == "true" ]] && [[ -n "$content_ids" ]]; then
        log_info "ContentIDs in use:"
        echo "$content_ids" | while read -r cid; do
            echo "  - $cid"
        done
    fi
    
    log_info "=========================================="
    log_info "Analyzing LVs..."
    log_info "=========================================="
    
    # 分析每个 LV
    local unused_count=0
    local used_count=0
    local unused_lvs=()
    
    while IFS='|' read -r lv_name vg_name lv_size pool_lv origin; do
        # 去除空格
        lv_name=$(echo "$lv_name" | xargs)
        vg_name=$(echo "$vg_name" | xargs)
        lv_size=$(echo "$lv_size" | xargs)
        
        # 提取 contentID
        local content_id
        content_id=$(extract_content_id_from_lv "$lv_name")
        
        # 检查 contentID 是否在 Devbox 的 ContentID 列表中
        local is_used=false
        if [[ -n "$content_ids" ]]; then
            if echo "$content_ids" | grep -qFx "$content_id"; then
                is_used=true
            fi
        fi
        
        if [[ "$is_used" == "true" ]]; then
            used_count=$((used_count + 1))
            if [[ "$verbose" == "true" ]]; then
                log_success "LV $lv_name (ContentID: $content_id) - IN USE"
            fi
        else
            unused_count=$((unused_count + 1))
            unused_lvs+=("$lv_name|$vg_name|$lv_size")
            log_warn "LV $lv_name (ContentID: $content_id) - UNUSED"
            if [[ "$verbose" == "true" ]]; then
                echo "    VG: $vg_name, Size: $lv_size"
            fi
        fi
    done <<< "$lvs_list"
    
    log_info "=========================================="
    log_info "Summary:"
    log_info "  Total LVs: $lv_count"
    log_info "  Used LVs: $used_count"
    log_info "  Unused LVs: $unused_count"
    log_info "=========================================="
    
    # 如果有未使用的 LV，显示详细信息
    if [[ $unused_count -gt 0 ]]; then
        log_warn "Unused LVs (can be deleted):"
        for lv_info in "${unused_lvs[@]}"; do
            IFS='|' read -r lv_name vg_name lv_size <<< "$lv_info"
            echo "  - LV: $lv_name, VG: $vg_name, Size: $lv_size"
        done
        
        # 输出到文件供删除脚本使用（格式：vg_name|lv_name）
        if [[ -n "$output_file" ]]; then
            > "$output_file"  # 清空文件
            for lv_info in "${unused_lvs[@]}"; do
                IFS='|' read -r lv_name vg_name lv_size <<< "$lv_info"
                echo "$vg_name|$lv_name" >> "$output_file"
            done
            log_info "Unused LV list saved to: $output_file"
        fi
        
        if [[ "$dry_run" == "false" ]]; then
            log_info ""
            log_info "To delete these LVs, run:"
            log_info "  ./remove_unused_lvs.sh $output_file"
        else
            log_info ""
            log_info "Dry-run mode: No LVs will be deleted"
        fi
    else
        log_success "All LVs are in use. No cleanup needed."
    fi
}

# 运行主函数
main "$@"

