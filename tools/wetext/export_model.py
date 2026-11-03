#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把 WeTextProcessing 的 .fst 模型转成服务运行期用的紧凑格式 .wtfst。

为什么需要这一步：
    官方 .fst 是 OpenFST VectorFst 二进制，Go 侧直接解析要复刻 OpenFST 的
    header/超头布局，容易随版本漂移。这里用 kaldifst（纯 C++ 实现的 OpenFST，
    带 Python 绑定，不依赖 pynini）读出来，转成一份只有状态数/弧表/终态的
    定长二进制；Go 只用 encoding/binary 顺序读，格式永不变。

模型来源（两种都可以，产物一致）：
    1. pip install wetext    ——  官方已编译好的 .fst（推荐，无需 pynini）
    2. 自建规则：先跑 export_fst.py（需要 docker + pynini）再用本脚本转换

用法：
    python export_model.py                     # 自动定位 wetext 包，导出 zh,en,de,es,fr,it,hi,ar
    python export_model.py --lang zh,en
    python export_model.py --src /path/to/fsts --out ../../assets
"""

import argparse
import gzip
import importlib.util
import json
import os
import struct
import sys

LANGS = ["zh", "en", "de", "es", "fr", "it", "hi", "ar"]

MAGIC = 0x57544631  # "WTF1"
VERSION = 1


def find_src():
    """定位 wetext 包内置的 fsts 目录。"""
    spec = importlib.util.find_spec("wetext")
    if spec is None or not spec.submodule_search_locations:
        return None
    return os.path.join(list(spec.submodule_search_locations)[0], "fsts")


def export_one(kaldifst, path, out_path):
    fst = kaldifst.StdVectorFst.read(path)

    nstates = kaldifst.NumStates(fst) if hasattr(kaldifst, "NumStates") else None
    states = list(kaldifst.StateIterator(fst))
    if nstates is None:
        nstates = len(states)

    arcs = []          # (ilabel, olabel, weight, nextstate)
    offsets = [0]      # 每个状态在弧表里的起始下标
    finals = []
    min_weight = 0.0

    for state in states:
        for arc in kaldifst.ArcIterator(fst, state):
            w = float(arc.weight.value)
            min_weight = min(min_weight, w)
            arcs.append((int(arc.ilabel), int(arc.olabel), w, int(arc.nextstate)))
        offsets.append(len(arcs))
        finals.append(float(fst.final(state).value))

    start = int(fst.start)

    buf = bytearray()
    buf += struct.pack("<IIiI", MAGIC, VERSION, start, nstates)
    buf += struct.pack("<%dI" % len(offsets), *offsets)
    for il, ol, w, ns in arcs:
        buf += struct.pack("<iifi", il, ol, w, ns)
    buf += struct.pack("<%df" % nstates, *finals)

    with gzip.open(out_path, "wb", compresslevel=9) as f:
        f.write(buf)

    return {
        "nstates": nstates,
        "narcs": len(arcs),
        "min_weight": min_weight,
        "raw": len(buf),
        "gz": os.path.getsize(out_path),
    }


def export_orders(src, out_dir, targets):
    """导出各语种 token 字段顺序，运行期重排 tagger 输出用。

    zh/en/de/ko 的顺序写在 wetext.constants 里，其余语种在模型目录下
    的 token_orders.json 中。
    """
    from wetext import constants as C

    builtin = {
        "zh": C.TN_ORDERS,
        "ja": C.TN_ORDERS,
        "en": C.EN_TN_ORDERS,
        "de": C.DE_TN_ORDERS,
        "ko": C.KO_TN_ORDERS,
    }

    orders = {}
    for lang in targets:
        if lang in builtin:
            orders[lang] = builtin[lang]
            continue
        p = os.path.join(src, lang, "tn", "token_orders.json")
        if os.path.isfile(p):
            with open(p, "r", encoding="utf-8") as f:
                orders[lang] = json.load(f)

    out = os.path.join(out_dir, "orders.json")
    with open(out, "w", encoding="utf-8") as f:
        json.dump(orders, f, ensure_ascii=False, indent=2)
    print("[ok] orders.json  {}".format(", ".join(sorted(orders))))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--lang", default="", help="逗号分隔，空表示全部")
    ap.add_argument("--src", default="", help=".fst 所在根目录，默认取 wetext 包内置目录")
    ap.add_argument("--out", default="", help="输出目录，默认 conf/wetext_model")
    args = ap.parse_args()

    try:
        import kaldifst
    except ImportError:
        print("缺少 kaldifst:  pip install kaldifst", file=sys.stderr)
        return 2

    src = args.src or find_src()
    if not src or not os.path.isdir(src):
        print("找不到 .fst 目录: {}".format(src), file=sys.stderr)
        print("请 pip install wetext，或用 --src 指定", file=sys.stderr)
        return 2

    here = os.path.dirname(os.path.abspath(__file__))
    out_dir = args.out or os.path.join(here, "..", "..", "assets")
    out_dir = os.path.abspath(out_dir)
    os.makedirs(out_dir, exist_ok=True)

    wanted = [x.strip() for x in args.lang.split(",") if x.strip()]
    targets = [x for x in LANGS if not wanted or x in wanted]
    if wanted:
        targets += [x for x in wanted if x not in LANGS]

    total = 0
    failed = []
    for lang in targets:
        for stage in ("tagger", "verbalizer"):
            fst_path = os.path.join(src, lang, "tn", stage + ".fst")
            if not os.path.isfile(fst_path):
                failed.append("{} {}: 文件不存在".format(lang, stage))
                continue
            out_path = os.path.join(out_dir, "{}_{}_tn.wtfst".format(lang, stage))
            try:
                st = export_one(kaldifst, fst_path, out_path)
            except Exception as exc:  # 单语种失败不影响其它
                failed.append("{} {}: {}".format(lang, stage, exc))
                continue
            total += st["gz"]
            print("[ok] {:>10}  states={:<7} arcs={:<8} min_w={:<5} raw={:>7.1f}KB gz={:>6.1f}KB".format(
                os.path.basename(out_path), st["nstates"], st["narcs"],
                round(st["min_weight"], 3), st["raw"] / 1024.0, st["gz"] / 1024.0))

    print("-" * 78)
    print("输出目录 {}  合计 {:.1f} MB".format(out_dir, total / 1024.0 / 1024.0))

    try:
        export_orders(src, out_dir, [x for x in targets if x not in failed])
    except Exception as exc:
        print("导出 orders.json 失败: {}".format(exc), file=sys.stderr)
        failed.append("orders.json: {}".format(exc))

    if failed:
        print("失败:", "; ".join(failed), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
