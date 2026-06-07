const ModelsView = {
    setup() {
        const {ref, onMounted} = Vue;
        const editingModel = ref(null);
        const showAddModal = ref(false);
        const addTargetProvider = ref(null);
        const form = ref({
            model_id: '',
            name: '',
            description: '',
        });

        onMounted(() => store.loadProviders());

        function openEdit(m) {
            editingModel.value = m.id;
            form.value = {
                model_id: m.model_id,
                name: m.name,
                description: m.description || '',
            };
        }

        function closeEdit() {
            editingModel.value = null;
        }

        async function saveEdit(id) {
            await store.updateModel(id, form.value);
            editingModel.value = null;
        }

        function openAdd(providerId) {
            addTargetProvider.value = providerId;
            form.value = {
                model_id: '',
                name: '',
                description: '',
            };
            showAddModal.value = true;
        }

        async function submitAdd() {
            await store.createModel({
                provider_instance_id: addTargetProvider.value,
                ...form.value,
            });
            showAddModal.value = false;
        }

        return {
            store,
            editingModel,
            showAddModal,
            form,
            openEdit,
            closeEdit,
            saveEdit,
            openAdd,
            submitAdd,
        };
    },
    template: `
    <div class="models-view">
      <h2>模型管理</h2>
      <div v-if="store.loading" class="loading">加载中...</div>
      <div v-else-if="store.providers.length === 0" class="empty">暂无 Provider</div>
      <div v-else class="provider-cards">
        <div v-for="pg in store.providers" :key="pg.id" class="provider-card">
          <div class="provider-card-header">
            <div>
              <h3>{{ pg.name }}</h3>
              <div class="provider-meta">{{ pg.template_id }} · {{ pg.models?.length || 0 }} 个模型</div>
            </div>
            <div class="provider-actions">
              <button class="btn btn-primary" @click="openAdd(pg.id)">+ 添加模型</button>
            </div>
          </div>
          <div class="provider-card-body">
            <table class="model-table">
              <thead>
                <tr>
                  <th style="width: 20%;">Model ID</th>
                  <th style="width: 20%;">模型名称</th>
                  <th style="width: 25%;">描述</th>
                  <th style="text-align: center;">操作</th>
                </tr>
              </thead>
              <tbody>
                <template v-for="m in pg.models" :key="m.id">
                  <tr v-if="editingModel === m.id" class="model-edit-row">
                    <td colspan="4">
                      <div class="model-edit">
                        <div class="edit-fields">
                          <label>Model ID</label>
                          <input v-model="form.model_id" placeholder="model-id">
                          <label>名称</label>
                          <input v-model="form.name" placeholder="Model Name">
                          <label>描述</label>
                          <input v-model="form.description" placeholder="可选">
                        </div>
                        <div class="edit-actions">
                          <button class="btn" @click="closeEdit()">取消</button>
                          <button class="btn btn-primary" @click="saveEdit(m.id)">保存</button>
                        </div>
                      </div>
                    </td>
                  </tr>
                  <tr v-else class="model-data-row">
                    <td><span class="model-id-text">{{ m.model_id }}</span></td>
                    <td>{{ m.name }}</td>
                    <td><span class="model-desc">{{ m.description || '-' }}</span></td>
                    <td class="model-actions-cell">
                      <label class="toggle">
                        <input type="checkbox" :checked="m.is_enabled" @change="store.toggleModel(m.id, $event.target.checked)">
                        <span class="toggle-slider"></span>
                      </label>
                      <button class="btn btn-sm" @click="openEdit(m)">编辑</button>
                      <button class="btn btn-sm btn-danger" @click="store.deleteModel(m.id)">删除</button>
                    </td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <div v-if="showAddModal" class="modal-overlay" @click="showAddModal = false">
        <div class="modal" @click.stop>
          <h3>添加模型</h3>
          <form @submit.prevent="submitAdd">
            <label>Model ID</label>
            <input v-model="form.model_id" placeholder="model-id" required>
            <label>名称</label>
            <input v-model="form.name" placeholder="Model Name" required>
            <label>描述</label>
            <input v-model="form.description" placeholder="可选">
            <div class="modal-actions">
              <button type="button" class="btn" @click="showAddModal = false">取消</button>
              <button type="submit" class="btn btn-primary">保存</button>
            </div>
          </form>
        </div>
      </div>
    </div>
  `
};
