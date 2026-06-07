#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

APP_BUNDLE=""

usage() {
    cat << 'EOF'
macOS Ad-hoc 签名工具

用法:
  $0 --app <.app路径>

选项:
  --app <路径>    .app 包路径（必需）
  --help          显示此帮助

示例:
  $0 --app "/Applications/Free Model Gateway.app"
  $0 --app "dist/dmg-tmp/Free Model Gateway.app"

说明:
  应用 ad-hoc 签名，解决 macOS "已损坏" 提示。
  签名后用户首次打开需右键 -> 打开。
  打包脚本 package-darwin.sh 已自动包含此签名。
EOF
}

while [[ $# -gt 0 ]]; do
    case $1 in
        --app)
            APP_BUNDLE="$2"
            shift 2
            ;;
        --help)
            usage
            exit 0
            ;;
        *)
            echo "未知选项: $1"
            usage
            exit 1
            ;;
    esac
done

if [ -z "$APP_BUNDLE" ]; then
    echo "错误: 缺少 --app 参数"
    usage
    exit 1
fi

if [ ! -d "$APP_BUNDLE" ]; then
    echo "错误: 找不到应用包: $APP_BUNDLE"
    exit 1
fi

echo "============================================="
echo "  Ad-hoc 签名"
echo "============================================="
echo ""
echo "正在应用 ad-hoc 签名..."
echo ""

codesign --force --deep --sign - "$APP_BUNDLE"

echo ""
echo "签名完成"
echo ""
echo "注意:"
echo "  - 应用现在可以打开，不会提示"已损坏""
echo "  - 用户首次打开时需要右键 -> 打开"
echo ""
