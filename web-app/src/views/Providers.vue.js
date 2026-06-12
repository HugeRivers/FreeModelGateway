const ProvidersView = {
    setup() {
        const {ref, onMounted} = Vue;
        const showModal = ref(false);
        const showKeyModal = ref(false);
        const selectedProvider = ref(null);
        const revealedKey = ref('');
        const isEdit = ref(false);
        const editId = ref(null);

        const providerType = ref('');
        const providerID = ref('');
        const providerName = ref('');
        const apiFormat = ref('openai-compatible');
        const baseURL = ref('');
        const apiKey = ref('');

        const apiFormatOptions = [
            {value: 'openai-compatible', label: 'OpenAI Chat Completions API'},
            {value: 'openai-responses', label: 'OpenAI Responses API'},
            {value: 'anthropic', label: 'Anthropic Messages API'},
            {value: 'gemini', label: 'Gemini Messages API'},
            {value: 'bedrock', label: 'Amazon Bedrock Converse API'}
        ];

        const builtinProviders = {
            'opencode-zen': {
                name: 'OpenCode Zen',
                base_url: 'https://opencode.ai/zen/v1',
                api_format: 'openai-compatible'
            },
            'openrouter': {
                name: 'OpenRouter',
                base_url: 'https://openrouter.ai/api/v1',
                api_format: 'openai-compatible'
            },
            'aihubmix': {
                name: 'AIHubMix',
                base_url: 'https://aihubmix.com/v1',
                api_format: 'openai-compatible'
            },
            'zenmux': {
                name: 'ZenMux',
                base_url: 'https://zenmux.ai/api/v1',
                api_format: 'openai-compatible'
            },

        };

        function openAdd() {
            isEdit.value = false;
            editId.value = null;
            resetForm();
            showModal.value = true;
        }

        function openEdit(p) {
            isEdit.value = true;
            editId.value = p.id;
            providerType.value = p.template_id === 'custom' ? 'custom' : p.template_id;
            providerID.value = p.template_id;
            providerName.value = p.name;
            apiFormat.value = p.api_format || 'openai-compatible';
            baseURL.value = p.base_url || '';
            showModal.value = true;
        }

        function onProviderTypeChange() {
            if (providerType.value && providerType.value !== 'custom' && !isEdit.value) {
                const info = builtinProviders[providerType.value];
                if (info) {
                    providerID.value = providerType.value;
                    providerName.value = info.name;
                    apiFormat.value = info.api_format;
                    baseURL.value = info.base_url;
                }
            } else if (providerType.value === 'custom' && !isEdit.value) {
                providerID.value = '';
                providerName.value = '';
                apiFormat.value = 'openai-compatible';
                baseURL.value = '';
            }
        }

        async function submitProvider() {
            const data = {
                template_id: providerType.value === 'custom' ? 'custom' : providerType.value,
                provider_id: providerID.value,
                name: providerName.value,
                api_key: apiKey.value,
                api_format: apiFormat.value,
                base_url: baseURL.value,
            };
            if (isEdit.value) {
                await store.updateProvider(editId.value, data);
            } else {
                await store.createProvider(data);
            }
            showModal.value = false;
            resetForm();
        }

        function resetForm() {
            providerType.value = '';
            providerID.value = '';
            providerName.value = '';
            apiFormat.value = 'openai-compatible';
            baseURL.value = '';
            apiKey.value = '';
        }

        async function revealKey(provider) {
            selectedProvider.value = provider;
            const key = await store.revealKey(provider.id);
            revealedKey.value = key;
            showKeyModal.value = true;
        }

        function copyKey() {
            if (!revealedKey.value) return;
            navigator.clipboard.writeText(revealedKey.value).then(() => {
                alert('API Key 已复制到剪贴板');
            }).catch(() => {
                const textarea = document.createElement('textarea');
                textarea.value = revealedKey.value;
                document.body.appendChild(textarea);
                textarea.select();
                document.execCommand('copy');
                document.body.removeChild(textarea);
                alert('API Key 已复制到剪贴板');
            });
        }

        function closeKeyModal() {
            showKeyModal.value = false;
            revealedKey.value = '';
            selectedProvider.value = null;
        }

        onMounted(() => store.loadProviders());

        return {
            store,
            showModal,
            showKeyModal,
            selectedProvider,
            revealedKey,
            isEdit,
            providerType,
            providerID,
            providerName,
            apiFormat,
            baseURL,
            apiKey,
            apiFormatOptions,
            builtinProviders,
            openAdd,
            openEdit,
            onProviderTypeChange,
            submitProvider,
            revealKey,
            copyKey,
            closeKeyModal,
        };
    },
    template: `
    <div class="providers-view">
      <h2>Provider 管理</h2>
      <div class="toolbar">
        <button class="btn btn-primary" @click="openAdd">+ 添加 Provider</button>
      </div>
      <div v-if="store.loading" class="loading">加载中...</div>
      <div v-else-if="store.providers.length === 0" class="empty">暂无 Provider</div>
      <div v-else class="provider-table-wrap">
        <div v-if="store.providers.some(p => !p.api_key_set)" class="setup-guide-hint">
          <span class="setup-guide-hint-icon">💡</span>
          <span>首次使用需要为每个 Provider 配置 API Key。点击「编辑」按钮，在弹窗中填写对应的 API Key。</span>
        </div>
        <table class="provider-table">
          <thead>
            <tr>
              <th style="width: 20%;">Provider ID</th>
              <th style="width: 20%;">显示名称</th>
              <th style="width: 15%;">API 模式</th>
              <th style="width: 30%;">基础 URL</th>
              <th style="text-align: center;">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in store.providers" :key="p.id" :class="['provider-row', !p.api_key_set ? 'provider-missing-key' : '']">
              <td><span class="provider-id-text">{{ p.template_id }}</span></td>
              <td>
                {{ p.name }}
                <span v-if="!p.api_key_set" class="missing-key-badge">未配置 Key</span>
              </td>
              <td><span class="api-format-badge">{{ p.api_format }}</span></td>
              <td><span class="base-url-text">{{ p.base_url }}</span></td>
              <td class="provider-actions-cell">
                <button :class="['btn', 'btn-sm', !p.api_key_set ? 'btn-warn' : '']" @click="openEdit(p)">
                  {{ !p.api_key_set ? '⚠️ 配置 Key' : '编辑' }}
                </button>
                <button v-if="p.api_key_set" class="btn btn-sm" @click="revealKey(p)">查看 Key</button>
                <button class="btn btn-sm btn-danger" @click="store.deleteProvider(p.id)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Add/Edit Provider Modal -->
      <div v-if="showModal" class="modal-overlay" @click="showModal = false">
        <div class="modal modal-wide" @click.stop>
          <h3>{{ isEdit ? '编辑 Provider' : '添加 Provider' }}</h3>
          <form @submit.prevent="submitProvider">
            <label>提供商类型</label>
            <select v-model="providerType" @change="onProviderTypeChange" :disabled="isEdit" required>
              <option value="">选择类型...</option>
              <option value="opencode-zen">OpenCode Zen</option>
              <option value="openrouter">OpenRouter</option>
              <option value="aihubmix">AIHubMix</option>
              <option value="zenmux">ZenMux</option>
              <option value="custom">Custom (自定义)</option>
            </select>

            <label>提供商 ID</label>
            <input v-model="providerID" placeholder="provider-id" :disabled="isEdit" required>

            <label>显示名称</label>
            <input v-model="providerName" placeholder="Provider 显示名称" required>

            <label>API 模式</label>
            <select v-model="apiFormat" required>
              <option v-for="opt in apiFormatOptions" :value="opt.value">{{ opt.label }}</option>
            </select>

            <label>基础 URL</label>
            <input v-model="baseURL" placeholder="https://api.example.com/v1" required>

            <label>API 密钥 {{ isEdit ? '(留空则保持不变)' : '' }}</label>
            <input v-model="apiKey" type="password" placeholder="API Key">

            <div class="modal-actions">
              <button type="button" class="btn" @click="showModal = false">取消</button>
              <button type="submit" class="btn btn-primary">保存</button>
            </div>
          </form>
        </div>
      </div>

      <!-- View API Key Modal -->
      <div v-if="showKeyModal" class="modal-overlay" @click="closeKeyModal">
        <div class="modal" @click.stop>
          <h3>API Key - {{ selectedProvider?.name }}</h3>
          <div class="key-display">
            <input :value="revealedKey" readonly type="text" class="key-input">
            <button class="btn btn-primary" @click="copyKey">复制</button>
          </div>
          <div class="modal-actions">
            <button class="btn" @click="closeKeyModal">关闭</button>
          </div>
        </div>
      </div>
    </div>
  `
};
