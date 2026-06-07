const {createApp, ref, computed, onMounted, watch} = Vue;
const {createRouter, createWebHashHistory} = VueRouter;

const App = {
    setup() {
        const route = VueRouter.useRoute();
        const router = VueRouter.useRouter();
        const navActive = computed(() => route.path);
        const user = ref(null);
        const showUserMenu = ref(false);
        const providersNeedKey = ref(false);

        const fetchUser = async () => {
            const token = localStorage.getItem('token');
            if (!token) return;
            try {
                const res = await fetch('/api/auth/me', {
                    headers: {'Authorization': `Bearer ${token}`}
                });
                if (res.ok) {
                    user.value = await res.json();
                    localStorage.setItem('user', JSON.stringify(user.value));
                } else {
                    localStorage.removeItem('token');
                    localStorage.removeItem('user');
                    user.value = null;
                }
            } catch (e) {
                user.value = null;
            }
        };

        const checkProvidersKey = async () => {
            const token = localStorage.getItem('token');
            if (!token) {
                providersNeedKey.value = false;
                return;
            }
            try {
                const res = await fetch('/api/providers', {
                    headers: {'Authorization': `Bearer ${token}`}
                });
                if (res.ok) {
                    const data = await res.json();
                    const providers = data.providers || [];
                    providersNeedKey.value = providers.some(p => !p.api_key_set);
                }
            } catch (e) {
                providersNeedKey.value = false;
            }
        };

        const logout = async () => {
            try {
                await fetch('/api/auth/logout', {method: 'POST'});
            } catch (e) {
            }
            localStorage.removeItem('token');
            localStorage.removeItem('user');
            user.value = null;
            providersNeedKey.value = false;
            router.push('/login');
        };

        const isAdmin = computed(() => user.value?.role === 'admin');

        const loadUser = () => {
            const saved = localStorage.getItem('user');
            if (saved) {
                try {
                    user.value = JSON.parse(saved);
                } catch (e) {
                }
            }
            fetchUser();
            checkProvidersKey();
        };

        Vue.watch(() => route.path, loadUser);
        onMounted(loadUser);

        // Also check providers when route changes to providers page
        watch(() => route.path, (path) => {
            if (path === '/providers') {
                setTimeout(checkProvidersKey, 500);
            }
        });

        return {navActive, user, showUserMenu, logout, isAdmin, providersNeedKey};
    },
    template: `
    <div class="app">
      <nav class="nav">
        <h1><span class="logo">FMG</span> Free Model Gateway</h1>
        <div class="nav-links">
          <router-link to="/" :class="{active: navActive === '/'}">Dashboard</router-link>
          <router-link to="/models" :class="{active: navActive === '/models'}">模型管理</router-link>
          <router-link to="/providers" :class="{active: navActive === '/providers'}">
            Providers
            <span v-if="providersNeedKey" class="nav-badge" title="有 Provider 未配置 API Key">●</span>
          </router-link>
          <router-link to="/users" :class="{active: navActive === '/users'}">用户管理</router-link>
        </div>
        <div class="nav-right">
          <router-link to="/help" class="help-link" title="帮助文档">
            <svg height="20" viewBox="0 0 24 24" width="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="10"></circle>
              <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"></path>
              <line x1="12" y1="17" x2="12.01" y2="17"></line>
            </svg>
          </router-link>
          <a href="https://github.com/HugeRivers/FreeModelGateway" target="_blank" rel="noopener" class="github-link" title="GitHub">
            <svg height="20" viewBox="0 0 16 16" width="20" fill="currentColor">
              <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/>
            </svg>
          </a>
          <div v-if="user" class="user-menu-wrapper">
            <div class="user-trigger" @click="showUserMenu = !showUserMenu">
              <img v-if="user.avatar" :src="user.avatar" class="user-avatar">
              <span v-else class="user-avatar-placeholder">{{ user.username?.[0]?.toUpperCase() }}</span>
              <span class="user-name">{{ user.nickname || user.username }}</span>
              <span class="dropdown-arrow">▼</span>
            </div>
            <div v-if="showUserMenu" class="user-dropdown">
              <div class="dropdown-item">
                <span class="role-tag" :class="user.role">{{ user.role }}</span>
              </div>
              <router-link to="/users" class="dropdown-item" @click="showUserMenu = false">用户管理</router-link>
              <div class="dropdown-divider"></div>
              <div class="dropdown-item" @click="logout">退出登录</div>
            </div>
          </div>
          <router-link v-else to="/login" class="login-link">登录</router-link>
        </div>
      </nav>
      <main class="main">
        <router-view></router-view>
      </main>
    </div>
  `
};

const router = createRouter({
    history: createWebHashHistory(),
    routes: [
        {path: '/login', component: LoginView},
        {path: '/', component: DashboardView, meta: {requiresAuth: true}},
        {path: '/models', component: ModelsView, meta: {requiresAuth: true}},
        {path: '/providers', component: ProvidersView, meta: {requiresAuth: true, requiresAdmin: true}},
        {path: '/users', component: UsersView, meta: {requiresAuth: true}},
        {path: '/help', component: HelpView},
    ]
});

router.beforeEach((to, from, next) => {
    const token = localStorage.getItem('token');
    if (to.meta?.requiresAuth && !token) {
        next('/login');
        return;
    }
    if (to.meta?.requiresAdmin) {
        const userStr = localStorage.getItem('user');
        if (userStr) {
            try {
                const user = JSON.parse(userStr);
                if (user.role !== 'admin') {
                    next('/');
                    return;
                }
            } catch (e) {
                next('/login');
                return;
            }
        } else {
            next('/login');
            return;
        }
    }
    if (to.path === '/login' && token) {
        next('/');
        return;
    }
    next();
});

createApp(App).use(router).mount('#app');
