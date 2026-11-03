#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""用 WeTextProcessing 源码把规则编译成 OpenFST 二进制 .fst。

这是自建规则时才走的一步：Pynini 负责把规则图编译出来落到磁盘。
导出的 .fst 还不能被服务直接加载，需要再跑 export_model.py 转成
Go 运行期使用的 .wtfst；运行期不依赖 Python / Pynini。

目录结构按 wetext 包内布局摆放，方便 export_model.py 原样读取：
    {out}/{lang}/tn/tagger.fst
    {out}/{lang}/tn/verbalizer.fst

用法（在 build.sh 里已封装）：
    python export_fst.py --out /out
"""

import argparse
import importlib
import os
import sys
import time

# 语种 -> WeTextProcessing 源码里的模块名
LANGS = {
    "zh": "chinese",
    "en": "english",
    "de": "german",
    "es": "spanish",
    "fr": "french",
    "it": "italian",
    "hi": "hindi",
    "ar": "arabic",
}

# 导出后跑一遍自检，输出不对就说明规则或版本有问题
SMOKE = {
    "zh": "共465篇，约315万字",
    "en": "I have 300$ and 12:35:36",
    "de": "Ich habe 300 $ und 12:35:36",
    "es": "Tengo 300$ y 12:35:36",
    "fr": "J'ai 300$ et 12:35:36",
    "it": "Ho 300$ e 12:35:36",
    "hi": "12:35:36",
    "ar": "لدي 300 دولار",
}


def export_one(code, module_name, out_dir):
    mod = importlib.import_module("tn.{}.normalizer".format(module_name))
    normalizer_cls = getattr(mod, "Normalizer")

    # cache_dir=False 表示只在内存里构图，不写缓存目录，导出更干净。
    normalizer = normalizer_cls(cache_dir=False)

    tagger_path = os.path.join(out_dir, code, "tn", "tagger.fst")
    verbalizer_path = os.path.join(out_dir, code, "tn", "verbalizer.fst")
    os.makedirs(os.path.dirname(tagger_path), exist_ok=True)
    normalizer.tagger.write(tagger_path)
    normalizer.verbalizer.write(verbalizer_path)

    return normalizer, tagger_path, verbalizer_path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", default="/out", help="输出目录")
    parser.add_argument("--lang", default="", help="只导出指定语种，逗号分隔，空表示全部")
    args = parser.parse_args()

    os.makedirs(args.out, exist_ok=True)

    wanted = [x.strip() for x in args.lang.split(",") if x.strip()]
    targets = {k: v for k, v in LANGS.items() if not wanted or k in wanted}

    failed = []
    for code, module_name in targets.items():
        start = time.time()
        try:
            normalizer, tagger_path, verbalizer_path = export_one(code, module_name, args.out)
            tagger_size = os.path.getsize(tagger_path) / 1024.0 / 1024.0
            verbalizer_size = os.path.getsize(verbalizer_path) / 1024.0 / 1024.0
            cost = time.time() - start

            print("[ok]   {}  tagger={:.2f}MB verbalizer={:.2f}MB  {:.1f}s".format(
                code, tagger_size, verbalizer_size, cost))

            sample = SMOKE.get(code)
            if sample:
                try:
                    print("[test] {}  {}  ->  {}".format(
                        code, sample, normalizer.normalize(sample)))
                except Exception as exc:  # 自检失败不影响模型导出
                    print("[test] {}  自检异常: {}".format(code, exc))
        except Exception as exc:
            failed.append((code, exc))
            print("[fail] {}  {}".format(code, exc))

    print("-" * 60)
    total = 0
    for root, _, names in os.walk(args.out):
        for name in names:
            if name.endswith(".fst"):
                total += os.path.getsize(os.path.join(root, name))
    print("输出目录 {}  共 {:.2f} MB".format(args.out, total / 1024.0 / 1024.0))

    if failed:
        print("失败语种: {}".format(", ".join(c for c, _ in failed)))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
