# WeTextProcessing 规则编译环境。
#
# 只在「生成 .fst 模型」时用一次，产物是纯二进制的 FST 文件，
# 运行期不需要 Python、不需要 Pynini、不需要本镜像。
#
# 用法见 build.sh。

FROM python:3.11-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential git \
    && rm -rf /var/lib/apt/lists/*

# Pynini 在 Linux 上有预编译 wheel，这里不会走本地编译。
RUN pip install --no-cache-dir pynini

# 用源码安装，因为 PyPI 上的发布包只带 zh/en/ja，其余语种只在源码树里。
RUN git clone --depth 1 https://github.com/wenet-e2e/WeTextProcessing.git /wetext \
    && cd /wetext \
    && pip install --no-cache-dir -e .

WORKDIR /work

CMD ["python", "export_fst.py", "--out", "/out"]
