const DashboardView = {
    setup() {
        const {ref, computed, onMounted, watch} = Vue;
        const health = computed(() => store.dashboard?.health || {});
        const stats = computed(() => store.dashboard?.stats || {});
        const strategy = computed(() => store.dashboard?.strategy || 'balanced');
        const forcedModel = computed(() => store.dashboard?.forced_model);
        const lastUsed = computed(() => store.dashboard?.last_used);
        const models = computed(() => stats.value.models || []);

        const strategyOptions = [
            {value: 'balanced', label: '平衡'},
            {value: 'smartest', label: '智能优先'},
            {value: 'fastest', label: '速度优先'},
            {value: 'reliable', label: '可靠优先'},
        ];

        const routeMode = ref('auto');
        const selectedModel = ref('');
        const modelSearch = ref('');
        const showModelDropdown = ref(false);
        const isSwitching = ref(false);
        const switchError = ref('');
        const modelDropdownRef = ref(null);
        const selectedProvider = ref(null);

        watch(forcedModel, (fm) => {
            if (fm?.provider_id && fm?.model_id) {
                routeMode.value = 'manual';
                selectedModel.value = fm.provider_id + ':' + fm.model_id;
            } else {
                routeMode.value = 'auto';
                selectedModel.value = '';
            }
        }, {immediate: true});

        async function onRouteModeChange(mode) {
            routeMode.value = mode;
            if (mode === 'auto') {
                await clearManual();
            }
        }

        async function onStrategyChange(mode) {
            try {
                await store.switchStrategy(mode);
            } catch (e) {
                switchError.value = e.message || '策略切换失败';
            }
        }

        async function selectModel(modelKey) {
            if (!modelKey) return;
            isSwitching.value = true;
            switchError.value = '';
            try {
                const [providerID, modelID] = modelKey.split(':');
                await store.switchModel(providerID, modelID);
                selectedModel.value = modelKey;
                showModelDropdown.value = false;
                modelSearch.value = '';
            } catch (e) {
                switchError.value = e.message || '切换失败';
            } finally {
                isSwitching.value = false;
            }
        }

        async function clearManual() {
            isSwitching.value = true;
            switchError.value = '';
            try {
                await store.clearForcedModel();
                selectedModel.value = '';
                selectedProvider.value = null;
                modelSearch.value = '';
            } catch (e) {
                switchError.value = e.message || '取消失败';
            } finally {
                isSwitching.value = false;
            }
        }

        const filteredProviders = computed(() => {
            const query = modelSearch.value.toLowerCase();
            if (!query) return store.providers;
            return store.providers.filter(pg => {
                const providerMatch = pg.name.toLowerCase().includes(query);
                const modelMatch = (pg.models || []).some(m =>
                    (m.name || m.model_id).toLowerCase().includes(query)
                );
                return providerMatch || modelMatch;
            });
        });

        const currentProviderModels = computed(() => {
            if (!selectedProvider.value) return [];
            const pg = store.providers.find(p => p.provider_id === selectedProvider.value.provider_id);
            if (!pg) return [];
            const query = modelSearch.value.toLowerCase();
            return (pg.models || []).filter(m => {
                const text = (m.name || m.model_id).toLowerCase();
                return !query || text.includes(query);
            }).map(m => ({
                key: pg.provider_id + ':' + m.model_id,
                provider: pg.name,
                modelId: m.model_id,
                name: m.name || m.model_id,
                enabled: m.is_enabled,
            }));
        });

        const selectedModelLabel = computed(() => {
            if (!selectedModel.value) return '选择模型...';
            for (const pg of store.providers) {
                for (const m of pg.models || []) {
                    if (pg.provider_id + ':' + m.model_id === selectedModel.value) {
                        return (m.name || m.model_id);
                    }
                }
            }
            return '选择模型...';
        });

        function onModelSearchFocus() {
            showModelDropdown.value = true;
            if (!selectedProvider.value && store.providers.length > 0) {
                selectedProvider.value = store.providers[0];
            }
        }

        function selectProvider(pg) {
            selectedProvider.value = pg;
        }

        watch(filteredProviders, (list) => {
            if (list.length === 0) {
                selectedProvider.value = null;
                return;
            }
            const stillVisible = list.some(p => p.provider_id === selectedProvider.value?.provider_id);
            if (!stillVisible) {
                selectedProvider.value = list[0];
            }
        });

        function onClickOutside(e) {
            if (modelDropdownRef.value && !modelDropdownRef.value.contains(e.target)) {
                showModelDropdown.value = false;
            }
        }

        onMounted(() => {
            store.loadDashboard();
            store.loadProviders();
            document.addEventListener('click', onClickOutside);
        });

        const fmt = (n) => {
            if (!n) return '0';
            if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
            if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K';
            return String(n);
        };

        const missingApiKeyProviders = computed(() => {
            return store.providers.filter(p => !p.api_key_set);
        });

        const hasMissingApiKeys = computed(() => missingApiKeyProviders.value.length > 0);

        return {
            health,
            stats,
            strategy,
            forcedModel,
            lastUsed,
            models,
            fmt,
            store,
            missingApiKeyProviders,
            hasMissingApiKeys,
            strategyOptions,
            routeMode,
            selectedModel,
            modelSearch,
            showModelDropdown,
            isSwitching,
            switchError,
            filteredProviders,
            currentProviderModels,
            selectedModelLabel,
            selectedProvider,
            modelDropdownRef,
            onRouteModeChange,
            onStrategyChange,
            selectModel,
            selectProvider,
            clearManual,
            onModelSearchFocus,
        };
    },
    template: `
    <div class="dashboard">
      <div v-if="hasMissingApiKeys" class="setup-guide-banner">
        <div class="setup-guide-content">
          <span class="setup-guide-icon">⚠️</span>
          <div class="setup-guide-text">
            <strong>欢迎使用 Free Model Gateway！</strong>
            检测到以下 Provider 尚未配置 API Key，请前往 Provider 管理页面填写：
            <span class="setup-guide-providers">{{ missingApiKeyProviders.map(p => p.name).join('、') }}</span>
          </div>
          <router-link to="/providers" class="setup-guide-btn">去配置 API Key →</router-link>
        </div>
      </div>
      <div class="cards">
        <div class="card card-green">
          <div class="card-label">Healthy</div>
          <div class="card-value">{{ health.healthy ?? '--' }}</div>
        </div>
        <div class="card card-yellow">
          <div class="card-label">Cooldown</div>
          <div class="card-value">{{ health.cooldown ?? '--' }}</div>
        </div>
        <div class="card card-blue">
          <div class="card-label">总模型</div>
          <div class="card-value">{{ health.models_total ?? '--' }}</div>
        </div>
        <div class="card card-purple">
          <div class="card-label">总请求</div>
          <div class="card-value">{{ fmt(stats.total_requests) }}</div>
        </div>
      </div>

      <div class="toolbar">
        <div class="route-mode-group">
          <span class="toolbar-label">路由模式</span>
          <div class="segmented-control">
            <button
              :class="['segment-btn', routeMode === 'auto' ? 'segment-active' : '']"
              @click="onRouteModeChange('auto')"
            >
              自动路由
            </button>
            <button
              :class="['segment-btn', routeMode === 'manual' ? 'segment-active' : '']"
              @click="onRouteModeChange('manual')"
            >
              手动路由
            </button>
          </div>
        </div>

        <div v-if="routeMode === 'auto'" class="strategy-group">
          <span class="toolbar-label">自动策略</span>
          <div class="strategy-pills">
            <button
              v-for="opt in strategyOptions"
              :key="opt.value"
              :class="['strategy-pill', strategy === opt.value ? 'strategy-active' : '']"
              @click="onStrategyChange(opt.value)"
            >
              {{ opt.label }}
            </button>
          </div>
        </div>

        <div v-if="routeMode === 'manual'" class="model-select-group" ref="modelDropdownRef">
          <span class="toolbar-label">选择模型</span>
          <div class="custom-dropdown">
            <div class="dropdown-trigger" @click="onModelSearchFocus">
              <span v-if="!selectedModel" class="dropdown-placeholder">搜索并选择模型...</span>
              <span v-else class="dropdown-value">{{ selectedModelLabel }}</span>
              <span class="dropdown-arrow">▼</span>
            </div>
            <div v-if="showModelDropdown" class="dropdown-menu split-panel">
              <div class="dropdown-search">
                <input
                  v-model="modelSearch"
                  placeholder="搜索模型或Provider..."
                  @click.stop
                  ref="searchInput"
                />
              </div>
              <div class="split-content">
                <div class="provider-list">
                  <div
                    v-for="pg in filteredProviders"
                    :key="pg.provider_id"
                    :class="['provider-item', selectedProvider?.provider_id === pg.provider_id ? 'provider-active' : '']"
                    @click="selectProvider(pg)"
                  >
                    <div class="provider-item-name">{{ pg.name }}</div>
                    <div class="provider-item-meta">{{ pg.models?.length || 0 }} 模型</div>
                  </div>
                  <div v-if="filteredProviders.length === 0" class="dropdown-empty">
                    未找到匹配的Provider
                  </div>
                </div>
                <div class="model-list">
                  <div
                    v-for="m in currentProviderModels"
                    :key="m.key"
                    :class="['model-item', selectedModel === m.key ? 'model-selected' : '', !m.enabled ? 'model-disabled' : '']"
                    @click="m.enabled && selectModel(m.key)"
                  >
                    <div class="model-item-name">{{ m.name }}</div>
                    <div class="model-item-id">{{ m.modelId }}</div>
                  </div>
                  <div v-if="selectedProvider && currentProviderModels.length === 0" class="dropdown-empty">
                    该Provider暂无模型
                  </div>
                  <div v-if="!selectedProvider" class="dropdown-empty">
                    请选择左侧Provider
                  </div>
                </div>
              </div>
            </div>
          </div>
          <button v-if="selectedModel" class="btn btn-sm" @click="clearManual">取消</button>
        </div>

        <div v-if="isSwitching" class="switch-loading">切换中...</div>

        <div class="toolbar-spacer"></div>

        <button class="btn btn-recover" @click="store.recoverAll()">恢复全部</button>
        <button class="btn" @click="store.loadDashboard()">刷新</button>
      </div>

      <div v-if="switchError" class="alert alert-error">
        {{ switchError }}
        <button class="alert-close" @click="switchError = ''">×</button>
      </div>

      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Provider</th>
              <th>Model</th>
              <th>Status</th>
              <th>Requests</th>
              <th>Success Rate</th>
              <th>Latency</th>
              <th>Tokens</th>
              <th>路由状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="store.loading">
              <td colspan="8" class="td-center">加载中...</td>
            </tr>
            <tr v-else-if="models.length === 0">
              <td colspan="8" class="td-center">暂无模型</td>
            </tr>
            <tr v-for="m in models" :key="m.provider_id + ':' + m.model_id">
              <td><span class="provider-tag">{{ m.provider_name || m.provider_id }}</span></td>
              <td>
                <span class="model-id">{{ m.model_id }}</span>
                <span class="model-name">{{ m.model_name || '' }}</span>
              </td>
              <td>
                <span :class="'status-badge status-' + (m.status || 'unknown')">
                  {{ m.status || 'unknown' }}
                  <span v-if="m.status === 'invalid' && m.last_error" class="status-error-icon" :title="m.last_error">&#9432;</span>
                </span>
              </td>
              <td>{{ fmt(m.total_requests) }}</td>
              <td>{{ m.success_rate != null ? (m.success_rate * 100).toFixed(1) + '%' : '--' }}</td>
              <td>{{ m.avg_latency_ms ? m.avg_latency_ms + 'ms' : '--' }}</td>
              <td>{{ fmt((m.input_tokens || 0) + (m.output_tokens || 0)) }}</td>
              <td>
                <span v-if="forcedModel?.provider_id === m.provider_id && forcedModel?.model_id === m.model_id" class="route-status manual">手动路由</span>
                <span v-else-if="lastUsed?.provider_id === m.provider_id && lastUsed?.model_id === m.model_id" class="route-status active">使用中</span>
                <span v-else class="route-status auto">自动</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="footer">
        <a href="https://github.com/HugeRivers/FreeModelGateway" target="_blank" rel="noopener" class="footer-link">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>
          HugeRivers/FreeModelGateway
        </a>
        <span class="footer-sep">|</span>
        <span class="footer-text">&copy;2026 liteliu</span>
      </div>
    </div>
  `
};
