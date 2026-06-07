const HelpView = {
    setup() {
        const activeTab = Vue.ref('clients');
        const copiedKey = Vue.ref('');
        const user = Vue.ref(null);

        const fetchUser = async () => {
            const token = localStorage.getItem('token');
            if (!token) return;
            try {
                const res = await fetch('/api/auth/me', {
                    headers: {'Authorization': `Bearer ${token}`}
                });
                if (res.ok) {
                    user.value = await res.json();
                }
            } catch (e) {
            }
        };

        const copyToClipboard = async (text, label) => {
            try {
                await navigator.clipboard.writeText(text);
                copiedKey.value = label;
                setTimeout(() => copiedKey.value = '', 2000);
            } catch (e) {
                const textarea = document.createElement('textarea');
                textarea.value = text;
                document.body.appendChild(textarea);
                textarea.select();
                document.execCommand('copy');
                document.body.removeChild(textarea);
                copiedKey.value = label;
                setTimeout(() => copiedKey.value = '', 2000);
            }
        };

        const baseURL = window.location.origin + '/v1';

        const providers = [
            {name: 'OpenCode Zen', key: 'OPENCODE_API_KEY', url: 'https://opencode.ai/auth'},
            {name: 'AIHubMix', key: 'AIHUBMIX_API_KEY', url: 'https://console.aihubmix.com/token'},
            {name: 'ZenMux', key: 'ZENMUX_API_KEY', url: 'https://zenmux.ai/platform/pay-as-you-go'},
            {name: 'OpenRouter', key: 'OPENROUTER_API_KEY', url: 'https://openrouter.ai/models'},
        ];

        const clients = [
            {
                name: 'OpenCode',
                icon: '🤖',
                description: 'AI-native IDE with built-in agent capabilities',
                config: {
                    file: '~/.config/opencode/opencode.json',
                    content: `{
  "provider": {
    "fmg": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Free Model Gateway",
      "options": {
        "baseURL": "${baseURL}",
        "apiKey": "YOUR_API_KEY"
      },
      "models": {
        "auto": { "name": "Auto (智能路由)" }
      }
    }
  }
}`
                }
            },
            {
                name: 'Claude Code',
                icon: '⚡',
                description: 'Anthropic\'s official CLI tool',
                config: {
                    file: '~/.bashrc or ~/.zshrc',
                    content: `export OPENAI_BASE_URL="${baseURL}"
export OPENAI_API_KEY="YOUR_API_KEY"

# Or use Anthropic protocol
export ANTHROPIC_BASE_URL="${window.location.origin}"
export ANTHROPIC_API_KEY="YOUR_API_KEY"`
                }
            },
            {
                name: 'Trae',
                icon: '🎯',
                description: 'ByteDance\'s AI-powered IDE',
                config: {
                    file: 'Settings → AI Assistant',
                    content: `Provider: OpenAI Compatible
Base URL: ${baseURL}
API Key: YOUR_API_KEY
Model: auto`
                }
            },
            {
                name: 'Cursor',
                icon: '✨',
                description: 'AI-first code editor',
                config: {
                    file: 'Settings → Models',
                    content: `OpenAI API Key: YOUR_API_KEY
OpenAI Base URL: ${baseURL}
Override OpenAI Base URL: ✓`
                }
            },
            {
                name: 'OpenClaw',
                icon: '🦀',
                description: 'Self-hosted gateway for chat apps → AI agents',
                config: {
                    file: '~/.openclaw/openclaw.json',
                    content: `{
  models: {
    providers: {
      "fmg": {
        baseUrl: "${baseURL}",
        apiKey: "YOUR_API_KEY",
        api: "openai-completions",
        models: [
          { id: "auto", name: "Auto (智能路由)" }
        ]
      }
    }
  }
}`
                }
            },
            {
                name: 'Hermes',
                icon: '⚡',
                description: 'Autonomous AI agent with persistent memory',
                config: {
                    file: '~/.hermes/config.yaml',
                    content: `model:
  provider: custom
  default: "auto"
  base_url: "${baseURL}"
  api_key: "YOUR_API_KEY"`
                }
            }
        ];

        Vue.onMounted(() => {
            fetchUser();
        });

        return {
            activeTab,
            copiedKey,
            user,
            providers,
            clients,
            baseURL,
            copyToClipboard
        };
    },
    template: `
    <div class="help-page">
      <h1>帮助文档</h1>
      
      <div class="help-tabs">
        <button 
          :class="['tab-btn', activeTab === 'clients' ? 'active' : '']" 
          @click="activeTab = 'clients'"
        >
          客户端配置
        </button>
        <button 
          :class="['tab-btn', activeTab === 'providers' ? 'active' : '']" 
          @click="activeTab = 'providers'"
        >
          Provider API Keys
        </button>
        <button 
          :class="['tab-btn', activeTab === 'api' ? 'active' : '']" 
          @click="activeTab = 'api'"
        >
          API 使用
        </button>
      </div>

      <!-- 客户端配置 -->
      <div v-if="activeTab === 'clients'" class="help-section">
        <div class="help-intro">
          <p>将 FMG 配置到您喜爱的开发工具中，使用 <code>{{ baseURL }}</code> 作为 Base URL。</p>
          <div v-if="user" class="api-key-box">
            <span class="label">您的 API Key：</span>
            <code class="api-key-value">{{ user.api_key }}</code>
            <button 
              class="btn-sm" 
              @click="copyToClipboard(user.api_key, 'apikey')"
            >
              {{ copiedKey === 'apikey' ? '已复制!' : '复制' }}
            </button>
          </div>
          <div v-else class="login-tip">
            <router-link to="/login">登录</router-link> 后查看您的 API Key
          </div>
        </div>

        <div class="clients-grid">
          <div v-for="client in clients" :key="client.name" class="client-card">
            <div class="client-header">
              <span class="client-icon">{{ client.icon }}</span>
              <div class="client-info">
                <h3>{{ client.name }}</h3>
                <p class="client-desc">{{ client.description }}</p>
              </div>
            </div>
            <div class="client-config">
              <div class="config-file">配置文件: {{ client.config.file }}</div>
              <pre class="config-code"><code>{{ client.config.content }}</code></pre>
              <button 
                class="btn-sm copy-btn" 
                @click="copyToClipboard(client.config.content, client.name)"
              >
                {{ copiedKey === client.name ? '已复制!' : '复制配置' }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- Provider API Keys -->
      <div v-if="activeTab === 'providers'" class="help-section">
        <div class="help-intro">
          <p>在 Dashboard → Providers 页面配置以下 API Keys。点击链接跳转到对应平台获取。</p>
        </div>
        
        <table class="data-table provider-table">
          <thead>
            <tr>
              <th>Provider</th>
              <th>环境变量名</th>
              <th>获取地址</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in providers" :key="p.name">
              <td><strong>{{ p.name }}</strong></td>
              <td><code>{{ p.key }}</code></td>
              <td>
                <a :href="p.url" target="_blank" rel="noopener" class="link">
                  {{ p.url }}
                  <svg width="12" height="12" viewBox="0 0 12 12" fill="none">
                    <path d="M3.5 8.5L8.5 3.5M8.5 3.5H4.5M8.5 3.5V7.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
                  </svg>
                </a>
              </td>
            </tr>
          </tbody>
        </table>

        <div class="help-note">
          <h4>配置步骤</h4>
          <ol>
            <li>点击上方链接，注册并登录对应平台</li>
            <li>在平台内创建或复制 API Key</li>
            <li>返回 FMG Dashboard，进入 <router-link to="/providers">Providers</router-link> 页面</li>
            <li>点击对应 Provider 的编辑按钮，粘贴 API Key</li>
            <li>保存后，该 Provider 下的模型将自动可用</li>
          </ol>
        </div>
      </div>

      <!-- API 使用 -->
      <div v-if="activeTab === 'api'" class="help-section">
        <div class="help-intro">
          <p>直接使用 HTTP API 访问 FMG，兼容 OpenAI Chat Completions 格式。</p>
        </div>

        <div class="api-examples">
          <div class="example-card">
            <h4>非流式请求</h4>
            <pre class="config-code"><code>curl -X POST {{ baseURL }}/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer YOUR_API_KEY" \\
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'</code></pre>
          </div>

          <div class="example-card">
            <h4>流式请求 (SSE)</h4>
            <pre class="config-code"><code>curl -X POST {{ baseURL }}/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer YOUR_API_KEY" \\
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'</code></pre>
          </div>

          <div class="example-card">
            <h4>列出可用模型</h4>
            <pre class="config-code"><code>curl {{ baseURL }}/models \\
  -H "Authorization: Bearer YOUR_API_KEY"</code></pre>
          </div>

          <div class="example-card">
            <h4>指定模型（跳过路由）</h4>
            <pre class="config-code"><code>curl -X POST {{ baseURL }}/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer YOUR_API_KEY" \\
  -d '{
    "model": "deepseek-v4-flash-free",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'</code></pre>
          </div>
        </div>

        <div class="help-note">
          <h4>支持的协议</h4>
          <ul>
            <li><strong>OpenAI Chat Completions</strong> - <code>POST /v1/chat/completions</code></li>
            <li><strong>OpenAI Responses</strong> - <code>POST /v1/responses</code></li>
            <li><strong>Anthropic Messages</strong> - <code>POST /v1/messages</code></li>
            <li><strong>Google Gemini</strong> - <code>POST /v1/models/:model/generateContent</code></li>
            <li><strong>Amazon Bedrock</strong> - <code>POST /model/:modelId/converse</code></li>
          </ul>
        </div>
      </div>
    </div>
  `
};