#!/usr/bin/env bash
# 从 WeTextProcessing 源码编译 .fst 模型，再转成 Go 运行期用的 .wtfst。
#
# 只有改了 WTP 规则、或者升级 WTP 版本时才需要重跑。
# 只有第一步需要 Docker + Pynini；第二步 export_model.py 只依赖 kaldifst，
# 可以直接用本地 .venv 跑。产物是可提交进仓库的二进制，运行期不依赖 Python。
#
#   ./build.sh            # 编译全部 8 个语种
#   ./build.sh en,de      # 只编译指定语种
#
# 用官方预编译模型（推荐，不必进 Docker）：
#   .venv/bin/pip install wetext kaldifst
#   .venv/bin/python export_model.py

set -euo pipefail

cd "$(dirname "$0")"

OUT_DIR="$(cd ../.. && pwd)/assets"
mkdir -p "$OUT_DIR"

# Docker Desktop 在 Windows 上需要 Windows 风格路径，Git Bash 下用 cygpath 转换。
if command -v cygpath >/dev/null 2>&1; then
    OUT_MOUNT="$(cygpath -w "$OUT_DIR")"
else
    OUT_MOUNT="$OUT_DIR"
fi

echo "输出目录: $OUT_DIR"

if command -v cygpath >/dev/null 2>&1; then
    WORK_MOUNT="$(cygpath -w "$PWD")"
else
    WORK_MOUNT="$PWD"
fi

docker build -t wetext-fst-builder .

LANG_ARG="${1:-}"
docker run --rm \
    -v "${OUT_MOUNT}:/out" \
    -v "${WORK_MOUNT}:/work" \
    wetext-fst-builder \
    python export_fst.py --out /out ${LANG_ARG:+--lang "$LANG_ARG"}

# 把导出的 .fst 转成 Go 运行期加载的 .wtfst（只依赖 kaldifst）
python3 export_model.py --src "$OUT_DIR" --out "$OUT_DIR" ${LANG_ARG:+--lang "$LANG_ARG"}

echo "完成:"
ls -lh "$OUT_DIR"
