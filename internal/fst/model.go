// Package fst 是 WeTextProcessing(https://github.com/wenet-e2e/WeTextProcessing)
// 归一化模型的 Go 运行时。
//
// 规则本身被编译成 FST（tagger + verbalizer），运行期只做一件事：
// 把输入字符串当成字节序列喂给 FST，取最短路径的输出。因此不需要 Python，
// 也不需要 OpenFST 动态库；模型由 tools/wetext/export_model.py 转成紧凑格式。
package fst

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"sync"
)

// Arc 一条弧：输入/输出标签、tropical 权重、目标状态
type Arc struct {
	ILabel int32
	OLabel int32
	Weight float32
	Next   int32
}

// Model 一张编译好的 FST（VectorFst，StdArc，字节标签，无符号表）
type Model struct {
	Start   int32
	Offsets []uint32 // 长度 nstates+1，第 s 个状态的弧在 Arcs[Offsets[s]:Offsets[s+1]]
	Arcs    []Arc
	Finals  []float32 // +Inf 表示非终态

	// 预处理结果，见 prepare。
	// epsEnd[s] 是状态 s 的出弧里 epsilon 弧的结束位置：
	// Arcs[Offsets[s]:epsEnd[s]] 全是 epsilon 弧，
	// Arcs[epsEnd[s]:Offsets[s+1]] 按输入标签升序排列。
	// 这样运行时求 epsilon 闭包不必扫描普通弧，读输入时也不必扫描 epsilon 弧。
	epsEnd   []uint32
	minLabel []int32 // 非 epsilon 出弧的最小输入标签，无出弧时为 +1
	maxLabel []int32 // 非 epsilon 出弧的最大输入标签，无出弧时为 -1

	pool sync.Pool // 转导用的临时缓冲，避免每次调用都重新分配
}

const (
	modelMagic   = 0x57544631 // "WTF1"
	modelVersion = 1
)

var infWeight = float32(math.Inf(1))

// LoadModel 读取磁盘上的 .wtfst 模型文件
func LoadModel(file string) (*Model, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	m, err := ParseModel(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}

	return m, nil
}

// ParseModel 解析 .wtfst 字节（gzip 压缩的紧凑 FST）
func ParseModel(b []byte) (*Model, error) {
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	buf, err := io.ReadAll(gz)
	if err != nil {
		return nil, err
	}

	return parseModel(buf)
}

func parseModel(b []byte) (*Model, error) {
	if len(b) < 16 {
		return nil, fmt.Errorf("truncated header")
	}
	magic := binary.LittleEndian.Uint32(b[0:])
	if magic != modelMagic {
		return nil, fmt.Errorf("bad magic %#x, want .wtfst (run tools/wetext/export_model.py)", magic)
	}
	version := binary.LittleEndian.Uint32(b[4:])
	if version != modelVersion {
		return nil, fmt.Errorf("unsupported model version %d", version)
	}

	m := &Model{Start: int32(binary.LittleEndian.Uint32(b[8:]))}
	nstates := int(binary.LittleEndian.Uint32(b[12:]))
	if nstates <= 0 {
		return nil, fmt.Errorf("bad state count %d", nstates)
	}

	off := 16
	need := off + 4*(nstates+1)
	if len(b) < need {
		return nil, fmt.Errorf("truncated offsets")
	}
	m.Offsets = make([]uint32, nstates+1)
	for i := range m.Offsets {
		m.Offsets[i] = binary.LittleEndian.Uint32(b[off+i*4:])
	}
	off = need

	narcs := int(m.Offsets[nstates])
	need = off + 16*narcs
	if len(b) < need {
		return nil, fmt.Errorf("truncated arcs")
	}
	m.Arcs = make([]Arc, narcs)
	for i := range m.Arcs {
		p := off + i*16
		m.Arcs[i] = Arc{
			ILabel: int32(binary.LittleEndian.Uint32(b[p:])),
			OLabel: int32(binary.LittleEndian.Uint32(b[p+4:])),
			Weight: math.Float32frombits(binary.LittleEndian.Uint32(b[p+8:])),
			Next:   int32(binary.LittleEndian.Uint32(b[p+12:])),
		}
	}
	off = need

	need = off + 4*nstates
	if len(b) < need {
		return nil, fmt.Errorf("truncated final states")
	}
	m.Finals = make([]float32, nstates)
	for i := range m.Finals {
		m.Finals[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[off+i*4:]))
	}

	m.prepare()

	return m, nil
}

// prepare 把每个状态的出弧整理成「epsilon 弧在前，普通弧按输入标签升序」，
// 并记录标签范围，供运行时快速跳过无关状态。
func (m *Model) prepare() {
	n := len(m.Finals)
	m.epsEnd = make([]uint32, n)
	m.minLabel = make([]int32, n)
	m.maxLabel = make([]int32, n)

	for s := 0; s < n; s++ {
		lo, hi := int(m.Offsets[s]), int(m.Offsets[s+1])
		arcs := m.Arcs[lo:hi]
		sort.SliceStable(arcs, func(i, j int) bool {
			ai, aj := arcs[i].ILabel, arcs[j].ILabel
			if (ai == 0) != (aj == 0) {
				return ai == 0 // epsilon 弧排最前
			}
			return ai < aj
		})

		e := lo
		for e < hi && m.Arcs[e].ILabel == 0 {
			e++
		}
		m.epsEnd[s] = uint32(e)

		if e == hi {
			m.minLabel[s], m.maxLabel[s] = 1, 0 // 无普通出弧
			continue
		}
		m.minLabel[s] = m.Arcs[e].ILabel
		m.maxLabel[s] = m.Arcs[hi-1].ILabel
	}
}

// NumStates 状态数
func (m *Model) NumStates() int { return len(m.Finals) }

// finalWeight 终态权重，非终态返回 +Inf
func (m *Model) finalWeight(s int32) float32 {
	if s < 0 || int(s) >= len(m.Finals) {
		return infWeight
	}
	return m.Finals[s]
}

// epsArcsOf 返回状态的 epsilon 弧
func (m *Model) epsArcsOf(s int32) []Arc {
	if s < 0 || int(s) >= len(m.epsEnd) {
		return nil
	}
	return m.Arcs[m.Offsets[s]:m.epsEnd[s]]
}

// arcsOfLabel 返回状态上输入标签恰为 label 的弧（按标签排好序，可二分）。
// 标签范围不包含 label 时直接返回 nil，这是最常见的分支。
func (m *Model) arcsOfLabel(s int32, label int32) []Arc {
	if s < 0 || int(s) >= len(m.epsEnd) {
		return nil
	}
	if label < m.minLabel[s] || label > m.maxLabel[s] {
		return nil
	}

	lo, hi := int(m.epsEnd[s]), int(m.Offsets[s+1])
	if hi-lo == 1 {
		if m.Arcs[lo].ILabel == label {
			return m.Arcs[lo:hi]
		}
		return nil
	}

	i := lo + sort.Search(hi-lo, func(k int) bool { return m.Arcs[lo+k].ILabel >= label })
	if i >= hi || m.Arcs[i].ILabel != label {
		return nil
	}
	j := i + 1
	for j < hi && m.Arcs[j].ILabel == label {
		j++
	}

	return m.Arcs[i:j]
}
