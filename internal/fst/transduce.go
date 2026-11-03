package fst

// 转导实现：把输入按字节逐个喂给 FST，求最短路径上的输出标签。
//
// 建模：节点是 (状态, 已消耗的输入位置)。输入标签 0 是 epsilon，不消耗输入，
// 因此同一层内要做一次最短路松弛；非 epsilon 弧一定让位置 +1，所以层与层之间
// 是 DAG，整体不会出现负环（模型权重存在负值，不能用全局 Dijkstra）。
//
// 这里的实现刻意不用 map：活跃状态集合用「稠密数组 + 位置标记」表示，
// 换层时只改标记不清数组；临时缓冲挂在 Model 上复用（sync.Pool），
// 每个输入位置不再产生哈希表分配，长文本下差一个数量级。

// layerRec 某一输入位置上某个状态的最优前驱，用于回溯输出标签。
type layerRec struct {
	state  int32
	parent int32
	olabel int32
	prev   bool // 前驱在上一层（即这一步消耗了一个输入字节）
}

// scratch 一次转导复用的临时缓冲。
// dist/stamp 按状态稠密索引，stamp 记录该状态的距离属于哪个 (调用, 输入位置)，
// 因此跨调用也不需要清零。
type scratch struct {
	dist   []float32
	stamp  []uint32
	parent []int32
	polab  []int32
	pprev  []bool
	inq    []uint32 // 层内 SPFA 队列标记

	front []int32 // 当前输入位置的活跃状态
	next  []int32 // 下一个输入位置的活跃状态
	queue []int32 // epsilon 闭包的工作队列

	recs   []layerRec // 逐位置快照的前驱记录
	posIdx []int32    // posIdx[p] 到 posIdx[p+1] 是第 p 个位置的记录

	tick uint32 // 单调递增的标记基址
}

func newScratch(nstates int) *scratch {
	return &scratch{
		dist:   make([]float32, nstates),
		stamp:  make([]uint32, nstates),
		parent: make([]int32, nstates),
		polab:  make([]int32, nstates),
		pprev:  make([]bool, nstates),
		inq:    make([]uint32, nstates),
	}
}

// Transduce 把输入交给 FST，返回最短路径上的输出标签字节序列。
func (m *Model) Transduce(input []byte) ([]byte, bool) {
	nstates := len(m.Finals)
	if m.Start < 0 || int(m.Start) >= nstates {
		return nil, false
	}

	sc, _ := m.pool.Get().(*scratch)
	if sc == nil || len(sc.dist) != nstates {
		sc = newScratch(nstates)
	}

	out, ok := m.transduce(sc, input)
	m.pool.Put(sc)

	return out, ok
}

func (m *Model) transduce(sc *scratch, input []byte) ([]byte, bool) {
	n := len(input)

	// 本调用可用的标记区间 [base+1, base+n+1]，与历史调用不重叠；
	// 只有在计数回绕时才需要清零。
	base := sc.tick
	sc.tick += uint32(n) + 2
	if sc.tick < base {
		clear(sc.stamp)
		clear(sc.inq)
		base = 0
		sc.tick = uint32(n) + 2
	}

	start := m.Start
	sc.front = append(sc.front[:0], start)
	sc.recs = sc.recs[:0]
	sc.posIdx = append(sc.posIdx[:0], 0)
	sc.stamp[start] = base + 1
	sc.dist[start] = 0
	sc.setParent(start, -1, false, 0)

	for pos := 0; pos <= n; pos++ {
		tag := base + uint32(pos) + 1
		sc.closure(m, tag)

		for _, s := range sc.front {
			sc.recs = append(sc.recs, layerRec{state: s, parent: sc.parent[s], olabel: sc.polab[s], prev: sc.pprev[s]})
		}
		sc.posIdx = append(sc.posIdx, int32(len(sc.recs)))

		if pos == n {
			break
		}
		if !sc.step(m, int32(input[pos]), tag+1) {
			return nil, false
		}
	}

	bestState, bestCost := int32(-1), infWeight
	for _, s := range sc.front {
		fw := m.finalWeight(s)
		if fw == infWeight {
			continue
		}
		if c := sc.dist[s] + fw; c < bestCost {
			bestCost, bestState = c, s
		}
	}
	if bestState < 0 {
		return nil, false
	}

	out := make([]byte, 0, n)
	pos := int32(n)
	cur := bestState
	for cur >= 0 && pos >= 0 {
		r, ok := sc.find(pos, cur)
		if !ok {
			break
		}
		if r.olabel != 0 {
			out = append(out, byte(r.olabel))
		}
		cur = r.parent
		if r.prev {
			pos--
		}
	}

	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}

	return out, true
}

// closure 在固定输入位置内做 epsilon 闭包松弛（SPFA）。
// 新到达的状态追加进 front，因此闭包结束后 front 就是该位置的完整活跃集合。
func (sc *scratch) closure(m *Model, tag uint32) {
	q := sc.queue[:0]
	for _, s := range sc.front {
		sc.inq[s] = tag
		q = append(q, s)
	}

	for h := 0; h < len(q); h++ {
		s := q[h]
		sc.inq[s] = 0
		d := sc.dist[s]

		for _, a := range m.epsArcsOf(s) {
			nd := d + a.Weight
			if sc.stamp[a.Next] != tag {
				sc.stamp[a.Next] = tag
				sc.dist[a.Next] = nd
				sc.setParent(a.Next, s, false, a.OLabel)
				sc.front = append(sc.front, a.Next)
				sc.inq[a.Next] = tag
				q = append(q, a.Next)
				continue
			}
			if nd >= sc.dist[a.Next] {
				continue
			}
			sc.dist[a.Next] = nd
			sc.setParent(a.Next, s, false, a.OLabel)
			if sc.inq[a.Next] != tag {
				sc.inq[a.Next] = tag
				q = append(q, a.Next)
			}
		}
	}

	sc.queue = q[:0]
}

// step 消耗一个输入字节，把活跃集合推进到下一个位置。
func (sc *scratch) step(m *Model, in int32, tag uint32) bool {
	next := sc.next[:0]
	for _, s := range sc.front {
		d := sc.dist[s]
		for _, a := range m.arcsOfLabel(s, in) {
			nd := d + a.Weight
			if sc.stamp[a.Next] != tag {
				sc.stamp[a.Next] = tag
				sc.dist[a.Next] = nd
				sc.setParent(a.Next, s, true, a.OLabel)
				next = append(next, a.Next)
				continue
			}
			if nd < sc.dist[a.Next] {
				sc.dist[a.Next] = nd
				sc.setParent(a.Next, s, true, a.OLabel)
			}
		}
	}

	sc.next = next
	sc.front, sc.next = sc.next, sc.front

	return len(sc.front) > 0
}

func (sc *scratch) setParent(s, from int32, prev bool, olabel int32) {
	sc.parent[s] = from
	sc.pprev[s] = prev
	sc.polab[s] = olabel
}

// find 在第 pos 个位置的快照里找状态 state 的前驱记录。
// 每层活跃状态通常只有几十个，线性扫描比维护哈希表更划算。
func (sc *scratch) find(pos, state int32) (layerRec, bool) {
	if pos < 0 || int(pos)+1 >= len(sc.posIdx) {
		return layerRec{}, false
	}
	for _, r := range sc.recs[sc.posIdx[pos]:sc.posIdx[pos+1]] {
		if r.state == state {
			return r, true
		}
	}

	return layerRec{}, false
}
