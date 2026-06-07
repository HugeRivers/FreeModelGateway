const UsersView = {
    setup() {
        const {ref, computed, onMounted} = Vue;
        const users = ref([]);
        const currentUser = ref(null);
        const loading = ref(false);
        const error = ref('');
        const showCreate = ref(false);
        const newUser = ref({username: '', password: '', nickname: '', role: 'user'});
        const editProfile = ref({nickname: '', avatar: ''});
        const showEdit = ref(false);
        const showPassword = ref(false);
        const passwordForm = ref({old_password: '', new_password: ''});
        const apiKeyVisible = ref({});

        const isAdmin = computed(() => currentUser.value?.role === 'admin');

        const fetchUsers = async () => {
            loading.value = true;
            try {
                const token = localStorage.getItem('token');
                const res = await fetch('/api/users', {
                    headers: {'Authorization': `Bearer ${token}`}
                });
                if (!res.ok) throw new Error('加载失败');
                const data = await res.json();
                users.value = data.users || [];
            } catch (e) {
                error.value = e.message;
            } finally {
                loading.value = false;
            }
        };

        const fetchCurrentUser = async () => {
            try {
                const token = localStorage.getItem('token');
                const res = await fetch('/api/auth/me', {
                    headers: {'Authorization': `Bearer ${token}`}
                });
                if (res.ok) {
                    currentUser.value = await res.json();
                }
            } catch (e) {
                console.error(e);
            }
        };

        const createUser = async () => {
            if (!newUser.value.username || !newUser.value.password) {
                alert('用户名和密码必填');
                return;
            }
            try {
                const token = localStorage.getItem('token');
                const res = await fetch('/api/users', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'Authorization': `Bearer ${token}`
                    },
                    body: JSON.stringify(newUser.value)
                });
                if (!res.ok) throw new Error('创建失败');
                showCreate.value = false;
                newUser.value = {username: '', password: '', nickname: '', role: 'user'};
                fetchUsers();
            } catch (e) {
                alert(e.message);
            }
        };

        const deleteUser = async (id) => {
            if (!confirm('确定删除该用户？')) return;
            try {
                const token = localStorage.getItem('token');
                const res = await fetch(`/api/users/${id}`, {
                    method: 'DELETE',
                    headers: {'Authorization': `Bearer ${token}`}
                });
                if (!res.ok) throw new Error('删除失败');
                fetchUsers();
            } catch (e) {
                alert(e.message);
            }
        };

        const openEdit = (user) => {
            editProfile.value = {nickname: user.nickname || '', avatar: user.avatar || ''};
            showEdit.value = true;
        };

        const updateProfile = async (id) => {
            try {
                const token = localStorage.getItem('token');
                const res = await fetch(`/api/users/${id}`, {
                    method: 'PUT',
                    headers: {
                        'Content-Type': 'application/json',
                        'Authorization': `Bearer ${token}`
                    },
                    body: JSON.stringify(editProfile.value)
                });
                if (!res.ok) throw new Error('更新失败');
                showEdit.value = false;
                fetchUsers();
                if (id === currentUser.value?.id) fetchCurrentUser();
            } catch (e) {
                alert(e.message);
            }
        };

        const updatePassword = async (id) => {
            if (!passwordForm.value.old_password || !passwordForm.value.new_password) {
                alert('请填写完整');
                return;
            }
            try {
                const token = localStorage.getItem('token');
                const res = await fetch(`/api/users/${id}/password`, {
                    method: 'PUT',
                    headers: {
                        'Content-Type': 'application/json',
                        'Authorization': `Bearer ${token}`
                    },
                    body: JSON.stringify(passwordForm.value)
                });
                if (!res.ok) throw new Error('修改失败');
                showPassword.value = false;
                passwordForm.value = {old_password: '', new_password: ''};
                alert('密码修改成功');
            } catch (e) {
                alert(e.message);
            }
        };

        const regenerateKey = async (id) => {
            if (!confirm('确定重新生成 API Key？旧 Key 将失效')) return;
            try {
                const token = localStorage.getItem('token');
                const res = await fetch(`/api/users/${id}/apikey`, {
                    method: 'POST',
                    headers: {'Authorization': `Bearer ${token}`}
                });
                if (!res.ok) throw new Error('生成失败');
                const data = await res.json();
                alert(`新 API Key: ${data.api_key}`);
                fetchUsers();
            } catch (e) {
                alert(e.message);
            }
        };

        const toggleApiKey = (id) => {
            apiKeyVisible.value[id] = !apiKeyVisible.value[id];
        };

        const formatDate = (ts) => {
            if (!ts) return '-';
            return new Date(ts * 1000).toLocaleString();
        };

        onMounted(() => {
            fetchCurrentUser();
            fetchUsers();
        });

        return {
            users,
            currentUser,
            loading,
            error,
            isAdmin,
            showCreate,
            newUser,
            showEdit,
            editProfile,
            showPassword,
            passwordForm,
            apiKeyVisible,
            fetchUsers,
            createUser,
            deleteUser,
            openEdit,
            updateProfile,
            updatePassword,
            regenerateKey,
            toggleApiKey,
            formatDate
        };
    },
    template: `
    <div class="users-page">
      <div class="users-header">
        <h2>用户管理</h2>
        <button v-if="isAdmin" class="btn btn-primary" @click="showCreate = true">+ 添加用户</button>
      </div>

      <div v-if="error" class="error-msg">{{ error }}</div>

      <div class="users-list">
        <div v-if="loading" class="loading">加载中...</div>
        <table v-else class="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>用户名</th>
              <th>昵称</th>
              <th>角色</th>
              <th>API Key</th>
              <th>创建时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in users" :key="u.id">
              <td>{{ u.id }}</td>
              <td>{{ u.username }}</td>
              <td>{{ u.nickname || '-' }}</td>
              <td>
                <span :class="['role-badge', u.role]">{{ u.role === 'admin' ? '管理员' : '用户' }}</span>
              </td>
              <td>
                <span class="api-key">
                  {{ apiKeyVisible[u.id] ? u.api_key : '****************' }}
                </span>
                <button class="btn btn-sm" @click="toggleApiKey(u.id)">{{ apiKeyVisible[u.id] ? '隐藏' : '显示' }}</button>
                <button class="btn btn-sm" @click="regenerateKey(u.id)">重新生成</button>
              </td>
              <td>{{ formatDate(u.created_at) }}</td>
              <td>
                <button class="btn btn-sm" @click="openEdit(u)">编辑</button>
                <button class="btn btn-sm" @click="showPassword = true">改密</button>
                <button v-if="isAdmin && u.id !== currentUser?.id" class="btn btn-sm btn-danger" @click="deleteUser(u.id)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Create User Modal -->
      <div v-if="showCreate" class="modal-overlay" @click.self="showCreate = false">
        <div class="modal">
          <h3>添加用户</h3>
          <div class="form-group">
            <label>用户名</label>
            <input v-model="newUser.username" placeholder="用户名">
          </div>
          <div class="form-group">
            <label>密码</label>
            <input v-model="newUser.password" type="password" placeholder="密码">
          </div>
          <div class="form-group">
            <label>昵称</label>
            <input v-model="newUser.nickname" placeholder="昵称">
          </div>
          <div class="form-group">
            <label>角色</label>
            <select v-model="newUser.role">
              <option value="user">用户</option>
              <option value="admin">管理员</option>
            </select>
          </div>
          <div class="modal-actions">
            <button class="btn btn-primary" @click="createUser">创建</button>
            <button class="btn" @click="showCreate = false">取消</button>
          </div>
        </div>
      </div>

      <!-- Edit Profile Modal -->
      <div v-if="showEdit" class="modal-overlay" @click.self="showEdit = false">
        <div class="modal">
          <h3>编辑资料</h3>
          <div class="form-group">
            <label>昵称</label>
            <input v-model="editProfile.nickname" placeholder="昵称">
          </div>
          <div class="form-group">
            <label>头像 URL</label>
            <input v-model="editProfile.avatar" placeholder="头像链接">
          </div>
          <div class="modal-actions">
            <button class="btn btn-primary" @click="updateProfile(currentUser?.id)">保存</button>
            <button class="btn" @click="showEdit = false">取消</button>
          </div>
        </div>
      </div>

      <!-- Change Password Modal -->
      <div v-if="showPassword" class="modal-overlay" @click.self="showPassword = false">
        <div class="modal">
          <h3>修改密码</h3>
          <div class="form-group">
            <label>旧密码</label>
            <input v-model="passwordForm.old_password" type="password" placeholder="旧密码">
          </div>
          <div class="form-group">
            <label>新密码</label>
            <input v-model="passwordForm.new_password" type="password" placeholder="新密码">
          </div>
          <div class="modal-actions">
            <button class="btn btn-primary" @click="updatePassword(currentUser?.id)">保存</button>
            <button class="btn" @click="showPassword = false">取消</button>
          </div>
        </div>
      </div>
    </div>
  `
};