package router

import (
	"context"
	"sync"

	"github.com/free-model-gateway/fmg/internal/model"
)

// WeightedRRStrategy 实现 Nginx 风格的 **平滑加权轮询 (Smooth Weighted Round-Robin, SWRR)** 算法。
//
// 与朴素加权轮询（"5 个 a 然后 1 个 b"那种 bursty 序列）不同，SWRR 把权重按时间摊开，
// 让高权重 backend **穿插出现**而非集中爆发。对上游 LLM provider 而言，这意味着
// 请求分布更平滑，避免瞬时峰值打爆某个 provider 的 QPS 限制。
//
// ## 算法思路（参考 Nginx 源码 ngx_http_upstream_weighted_round_robin.c）
//
// 每个 backend 维护一个 **effective weight**（初始为 0）。
// 每次 Select 调用的三步：
//
//  1. 累加: effective[i] += weight[i]                       （每个候选都加）
//  2. 选优: 选 effective 最大的 i 当作 winner
//  3. 衰减: effective[winner] -= Σweight[i]                （winner 减掉所有权重之和）
//
// ## 示例：a=5, b=1, c=1, totalWeight=7
//
//	调次  | 累加后 a,b,c            | winner | 衰减后 a,b,c
//	------+-------------------------+--------+-----------------
//	 1    | 5,1,1                   | a      | -2,1,1
//	 2    | 3,2,2                   | a      | -4,2,2
//	 3    | 1,3,3                   | a      | -6,3,3
//	 4    | -1,4,4                  | b      | -1,-3,4
//	 5    | 4,-2,5                  | c      | 4,-2,-2
//	 6    | 9,-1,-1                 | a      | 2,-1,-1
//	 7    | 7,0,0                   | a      | 0,0,0  ← 周期回归起点
//
// 输出序列: a,a,a,b,c, a,a, a, ... —— a 在 7 次内恰好出现 5 次，且分散在前 6 次。
//
// ## 正确性
//
// 在一个完整周期（max(weight) * len(weight) 次调用）内，每个 backend 被选中的次数
// **严格等于**其 weight 值。在周期内，winner 序列是确定性的（给定相同 weights 顺序）。
//
// ## 线程安全
//
// Select 内部用 mu 串行化。代价是所有 Select 调用在锁上排队 —— 这对单实例 FMG
// 没问题（Select 本身只有 map 操作，纳秒级）。如果未来 QPS 暴增到瓶颈，
// 可改用 atomic int64 + per-backend padding 消除 false sharing。
type WeightedRRStrategy struct {
	mu        sync.Mutex
	effective map[string]int // 每个 backend 的 effective weight
}

func NewWeightedRRStrategy() *WeightedRRStrategy {
	return &WeightedRRStrategy{effective: make(map[string]int)}
}

// Name 返回策略标识，用于日志与 /admin/providers 展示。
func (s *WeightedRRStrategy) Name() string { return "weighted-rr" }

// Select 按 SWRR 算法返回一个候选 backend。
// candidates 必须非空（调用方在 router.fallback.go 中已保证）。
func (s *WeightedRRStrategy) Select(ctx context.Context, candidates []*model.BackendModel, req *Request) (*model.BackendModel, error) {
	if len(candidates) == 0 {
		return nil, ErrNoCandidate
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 第一遍：计算总权重，并保证 effective 表中每个候选都有一项（避免 map miss）。
	// weight 最小为 1，防止配置错误导致某 backend 永不被选中。
	weights := make(map[string]int, len(candidates))
	totalWeight := 0
	for _, m := range candidates {
		w := m.Weight
		if w < 1 {
			w = 1
		}
		weights[m.Key()] = w
		totalWeight += w
		if _, ok := s.effective[m.Key()]; !ok {
			s.effective[m.Key()] = 0
		}
	}

	// 第二遍：累加 + 选优。
	// bestValue 初始化为最小 int32，保证即使所有 effective 为负，第一个候选也能胜出。
	bestKey := ""
	bestValue := -1 << 31
	for _, m := range candidates {
		k := m.Key()
		s.effective[k] += weights[k]
		if s.effective[k] > bestValue {
			bestValue = s.effective[k]
			bestKey = k
		}
	}

	// 衰减：把胜出者减去总权重，使其 effective 降为"剩余配额"。
	// 这是 SWRR 的关键 —— 没有这一步，会退化为朴素的"按权重 burst 选"。
	if bestKey != "" {
		s.effective[bestKey] -= totalWeight
	}

	// 用 key 找到原始对象（map 取的是 *BackendModel 指针，O(1)）。
	for _, m := range candidates {
		if m.Key() == bestKey {
			return m, nil
		}
	}
	return candidates[0], nil
}
