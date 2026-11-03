#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""用官方 wetext 生成的黄金语料，供 Go 侧回归测试。

golden.json 每一项记录四个阶段，Go 单测逐段比对，定位问题更快：
    input   原始输入
    tagged  tagger 输出（未重排）
    ordered 重排后（token 字段顺序）
    output  verbalizer 输出

用法：
    pip install wetext kaldifst
    python gen_golden.py            # 写入 internal/fst/testdata/golden.json
"""

import json
import os
import sys

CASES = [
    ("zh", "2.5平方电线"),
    ("zh", "共465篇，约315万字"),
    ("zh", "2024年"),
    ("zh", "2023年5月11日"),
    ("zh", "12:35:36"),
    ("zh", "12:00"),
    ("zh", "50-100"),
    ("zh", "30%"),
    ("zh", "3.14"),
    ("zh", "-99.99%"),
    ("zh", "13800138000"),
    ("zh", "1/5"),
    ("zh", "20/20"),
    ("zh", "1000000"),
    ("zh", "0.5"),
    ("zh", "第1章"),
    ("zh", "100元"),
    ("zh", "3.5万元"),
    ("zh", "1,234,567"),
    ("zh", "500kV变压器"),
    ("zh", "1.5MW"),
    ("zh", "985高校"),
    ("zh", "IGBT"),
    ("zh", "RT-LAB"),
    ("zh", "2024[千伏]"),
    ("zh", "千伏"),
    ("zh", "温度-5度"),
    ("zh", "2020-2024年"),
    ("zh", "第3.5节"),
    ("en", "I have 300$ and 12:35:36"),
    ("en", "2.5 sq mm wire"),
    ("en", "+33 1 23 45 67 89"),
    ("en", "The 21st century"),
    ("en", "3.14"),
    ("en", "1/5"),
    ("en", "30%"),
    ("en", "2024-05-11"),
    ("en", "$1,234.56"),
    ("de", "Ich habe 300 Euro und 12:35:36"),
    ("de", "3,14"),
    ("de", "1/5"),
    ("fr", "J'ai 300 euros et 12:35:36"),
    ("fr", "3,14"),
    ("fr", "1/5"),
    ("es", "Tengo 300$ y 12:35:36"),
    ("es", "3,14"),
    ("it", "Ho 300$ e 12:35:36"),
    ("it", "3,14"),
    ("hi", "12:35:36"),
    ("hi", "100"),
    ("ar", "لدي 300 دولار"),
    ("ar", "12:35:36"),
]


def main():
    from wetext import utils
    from wetext.token_parser import TokenParser

    rows = []
    for lang, text in CASES:
        try:
            tagged = utils.tag(text, lang, "tn").strip()
            ordered = TokenParser(lang, "tn").reorder(tagged)
            output = utils.verbalize(ordered, lang, "tn")
        except Exception as exc:  # 记录失败，Go 侧跳过该条
            rows.append({"lang": lang, "input": text, "error": str(exc)})
            continue
        rows.append({"lang": lang, "input": text, "tagged": tagged, "ordered": ordered, "output": output})

    here = os.path.dirname(os.path.abspath(__file__))
    out = os.path.abspath(os.path.join(here, "..", "..", "internal", "fst", "testdata", "golden.json"))
    os.makedirs(os.path.dirname(out), exist_ok=True)
    with open(out, "w", encoding="utf-8") as f:
        json.dump(rows, f, ensure_ascii=False, indent=2)
    ok = sum(1 for r in rows if "error" not in r)
    print("wrote {} cases ({} ok, {} failed) -> {}".format(len(rows), ok, len(rows) - ok, out))
    for r in rows:
        if "error" in r:
            print("[fail]", r["lang"], r["input"], r["error"], file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
