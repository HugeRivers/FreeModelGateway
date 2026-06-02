package model

import (
	"fmt"
	"sort"
	"sync"

	"github.com/free-model-gateway/fmg/internal/config"
	"github.com/sirupsen/logrus"
)

type Pool struct {
	mu     sync.RWMutex
	models []*BackendModel
	index  map[string]*BackendModel
}

type ProviderSummary struct {
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	Total        int    `json:"total"`
	Healthy      int    `json:"healthy"`
	Cooldown     int    `json:"cooldown"`
	HasAPIKey    bool   `json:"has_api_key"`
	Priority     int    `json:"priority"`
}

func NewPool(cfgs []config.ProviderConfig) (*Pool, error) {
	p := &Pool{index: make(map[string]*BackendModel)}
	for _, pc := range cfgs {
		if pc.APIKey == "" {
			continue
		}
		for _, mc := range pc.Models {
			bm := NewBackendModel(pc, mc)
			p.models = append(p.models, bm)
			p.index[bm.Key()] = bm
		}
	}
	if len(p.models) == 0 {
		return nil, fmt.Errorf("no models could be loaded (all providers have empty API keys)")
	}
	return p, nil
}

func (p *Pool) All() []*BackendModel {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*BackendModel, len(p.models))
	copy(out, p.models)
	return out
}

func (p *Pool) Available() []*BackendModel {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*BackendModel, 0, len(p.models))
	for _, m := range p.models {
		if m.IsAvailable() {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Priority < out[j].Priority
	})
	return out
}

func (p *Pool) Get(providerID, modelID string) (*BackendModel, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	m, ok := p.index[providerID+":"+modelID]
	if !ok {
		return nil, fmt.Errorf("model not found: %s/%s", providerID, modelID)
	}
	return m, nil
}

func (p *Pool) FindByModelID(modelID string) []*BackendModel {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*BackendModel, 0)
	for _, m := range p.models {
		if m.ModelID == modelID {
			out = append(out, m)
		}
	}
	return out
}

func (p *Pool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.models)
}

func (p *Pool) RecoverAll(log *logrus.Logger) int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, m := range p.models {
		m.Recover()
		count++
	}
	if log != nil {
		log.WithField("count", count).Info("[RECOVER] all models recovered")
	}
	return count
}

func (p *Pool) ProviderSummary() []ProviderSummary {
	p.mu.RLock()
	defer p.mu.RUnlock()
	type acc struct {
		summary ProviderSummary
	}
	groups := make(map[string]*acc)
	order := []string{}
	for _, m := range p.models {
		a, ok := groups[m.ProviderID]
		if !ok {
			a = &acc{summary: ProviderSummary{
				ProviderID:   m.ProviderID,
				ProviderName: m.ProviderName,
				Priority:     m.Priority,
				HasAPIKey:    m.APIKey != "",
			}}
			groups[m.ProviderID] = a
			order = append(order, m.ProviderID)
		}
		a.summary.Total++
		switch m.Status {
		case StatusHealthy:
			a.summary.Healthy++
		case StatusCooldown:
			a.summary.Cooldown++
		}
	}
	out := make([]ProviderSummary, 0, len(order))
	for _, id := range order {
		out = append(out, groups[id].summary)
	}
	return out
}

func (p *Pool) Replace(newCfgs []config.ProviderConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldByKey := make(map[string]*BackendModel, len(p.models))
	for _, m := range p.models {
		oldByKey[m.Key()] = m
	}

	newModels := make([]*BackendModel, 0)
	newIndex := make(map[string]*BackendModel)
	for _, pc := range newCfgs {
		if pc.APIKey == "" {
			continue
		}
		for _, mc := range pc.Models {
			bm := NewBackendModel(pc, mc)
			if existing, ok := oldByKey[bm.Key()]; ok {
				bm.SuccessCount = existing.SuccessCount
				bm.ErrorCount = existing.ErrorCount
				bm.InputTokens = existing.InputTokens
				bm.OutputTokens = existing.OutputTokens
				bm.TotalRequests = existing.TotalRequests
				bm.Status = existing.Status
				bm.CooldownUntil = existing.CooldownUntil
				bm.ConsecErrors = existing.ConsecErrors
			}
			newModels = append(newModels, bm)
			newIndex[bm.Key()] = bm
		}
	}

	if len(newModels) == 0 {
		return fmt.Errorf("hot reload resulted in zero models; aborting to preserve old pool")
	}
	p.models = newModels
	p.index = newIndex
	return nil
}
