const LoginView = {
    setup() {
        const {ref, onMounted} = Vue;
        const username = ref('');
        const password = ref('');
        const captcha = ref('');
        const captchaImage = ref('');
        const loading = ref(false);
        const error = ref('');
        const router = VueRouter.useRouter();

        const loadCaptcha = async () => {
            try {
                const res = await fetch('/api/auth/captcha');
                const data = await res.json();
                captchaImage.value = data.captcha;
            } catch (e) {
                captchaImage.value = '----';
            }
        };

        const login = async () => {
            if (!username.value || !password.value || !captcha.value) {
                error.value = '请填写所有字段';
                return;
            }

            loading.value = true;
            error.value = '';

            try {
                const res = await fetch('/api/auth/login', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({
                        username: username.value,
                        password: password.value,
                        captcha: captcha.value
                    })
                });

                const data = await res.json();
                if (!res.ok) {
                    error.value = data.error || '登录失败';
                    loadCaptcha();
                    return;
                }

                localStorage.setItem('token', data.token);
                localStorage.setItem('user', JSON.stringify(data.user));
                router.push('/');
            } catch (e) {
                error.value = '网络错误';
                loadCaptcha();
            } finally {
                loading.value = false;
            }
        };

        onMounted(() => {
            loadCaptcha();
            const token = localStorage.getItem('token');
            if (token) {
                router.push('/');
            }
        });

        return {
            username,
            password,
            captcha,
            captchaImage,
            loading,
            error,
            login,
            loadCaptcha
        };
    },
    template: `
    <div class="login-page">
      <div class="login-box">
        <h2>FMG 登录</h2>
        <div class="login-form">
          <div class="form-group">
            <label>用户名</label>
            <input type="text" v-model="username" placeholder="请输入用户名" @keyup.enter="login">
          </div>
          <div class="form-group">
            <label>密码</label>
            <input type="password" v-model="password" placeholder="请输入密码" @keyup.enter="login">
          </div>
          <div class="form-group">
            <label>验证码</label>
            <div class="captcha-row">
              <input type="text" v-model="captcha" placeholder="请输入验证码" @keyup.enter="login">
              <span class="captcha-code" @click="loadCaptcha">{{ captchaImage }}</span>
            </div>
          </div>
          <div v-if="error" class="login-error">{{ error }}</div>
          <button class="login-btn" :disabled="loading" @click="login">
            {{ loading ? '登录中...' : '登录' }}
          </button>
        </div>
      </div>
    </div>
  `
};