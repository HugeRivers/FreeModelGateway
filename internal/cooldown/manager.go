package cooldown

import (
	"sync"
	"time"

	"github.com/free-model-gateway/fmg/internal/model"
	"github.com/sirupsen/logrus"
)

// Manager 维护所有 backend 的冷却定时器。
//
// ## 行为契约
//
//   - EnterCooldown(backend, reason) 立即把 backend 置为冷却态，duration 由
//     当前连续错误数决定（指数退避），并在 duration 后自动调用 Recover。
//   - 同一 backend 重复进入冷却时，**取消前一个定时器**，以最新的 duration 为准
//     （避免"老的 timer 把刚拉长冷却的 backend 提前放出来"）。
//   - Recover 可被外部显式调用（/admin/recover），也可被 timer 触发。
//
// ## 指数退避公式
//
//	duration = min(baseCooldown * 2^(consec-1), maxCooldown)
//
// 其中 consec = backend.ConsecErrors（连续失败次数）。
//
// 示例（baseCooldown=5min=300s, maxCooldown=1h=3600s）：
//
//	consec=1 → 300s    (5 min)
//	consec=2 → 600s    (10 min)
//	consec=3 → 1200s   (20 min)
//	consec=4 → 2400s   (40 min)
//	consec=5 → 3600s   (1h, 已到上限)
//	consec=6+ → 3600s  (封顶)
//
// ## 幂等性
//
// EnterCooldown/Recover 接受 nil backend 时直接 return（不 panic），方便调用方
// 兜底（router.fallback.go 中已用此特性）。
type Manager struct {
	pool         *model.Pool
	baseCooldown time.Duration // 基础冷却（对应 consec=1）
	maxCooldown  time.Duration // 冷却上限（封顶值）
	mu           sync.Mutex
	timers       map[string]*time.Timer // key = backend.Key() → 当前活跃 timer
	log          *logrus.Logger
}

func NewManager(pool *model.Pool, baseSeconds, maxSeconds int, log *logrus.Logger) *Manager {
	return &Manager{
		pool:         pool,
		baseCooldown: time.Duration(baseSeconds) * time.Second,
		maxCooldown:  time.Duration(maxSeconds) * time.Second,
		timers:       make(map[string]*time.Timer),
		log:          log,
	}
}

// EnterCooldown 把 backend 置入冷却，并在 duration 后自动恢复。
// reason 用于日志（"429 rate limited" / "consecutive errors" / 任意字符串）。
//
// 注意：先 Stop 老 timer，再 AfterFunc 新 timer，**这是必须的** —— 否则
// "老 timer 先到时把刚延长的冷却提前放出来"，表现就是 cooldown 实际时长比
// 预期的短。
func (m *Manager) EnterCooldown(backend *model.BackendModel, reason string) {
	if backend == nil {
		return
	}
	duration := m.calculateDuration(backend)
	backend.EnterCooldown(duration)

	m.mu.Lock()
	key := backend.Key()
	if t, ok := m.timers[key]; ok {
		// 关键：Stop 不保证 100% 生效（如果 timer 已触发但还没跑回调，
		// Go 文档明确说 race）。但即便 race 发生，也只是多调一次 Recover，
		// Recover 内部 backend.Recover() 是幂等的（连续状态机）。
		t.Stop()
	}
	m.timers[key] = time.AfterFunc(duration, func() {
		m.Recover(backend)
	})
	m.mu.Unlock()

	if m.log != nil {
		m.log.WithFields(logrus.Fields{
			"model":         backend.ModelID,
			"provider":      backend.ProviderName,
			"duration":      duration.String(),
			"reason":        reason,
			"consec_errors": backend.ConsecErrors,
		}).Warn("[COOLDOWN] model entered cooldown")
	}
}

// Recover 手动 / 自动恢复 backend。timer 回调和 /admin/recover 都走这里。
func (m *Manager) Recover(backend *model.BackendModel) {
	if backend == nil {
		return
	}
	backend.Recover()

	m.mu.Lock()
	key := backend.Key()
	if t, ok := m.timers[key]; ok {
		t.Stop()
		delete(m.timers, key)
	}
	m.mu.Unlock()

	if m.log != nil {
		m.log.WithFields(logrus.Fields{
			"model":    backend.ModelID,
			"provider": backend.ProviderName,
		}).Info("[RECOVERED] model auto-recovered from cooldown")
	}
}

// calculateDuration 根据连续错误数算指数退避时长。
// mult 截断到 30 防止左移溢出（uint 在 Go 是平台相关，64 位平台上 1<<30 ≈ 17 亿，足够大）。
// maxCooldown 二次封顶。
func (m *Manager) calculateDuration(b *model.BackendModel) time.Duration {
	consec := b.ConsecErrors
	if consec <= 1 {
		return m.baseCooldown
	}
	mult := uint(consec - 1)
	if mult > 30 {
		mult = 30
	}
	d := m.baseCooldown * (1 << mult)
	if d > m.maxCooldown {
		d = m.maxCooldown
	}
	return d
}
