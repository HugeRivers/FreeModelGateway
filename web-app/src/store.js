const store = Vue.reactive({
    dashboard: null,
    providers: [],
    models: [],
    loading: false,
    error: null,

    async loadDashboard() {
        this.loading = true;
        try {
            this.dashboard = await api('GET', '/api/dashboard');
            this.error = null;
        } catch (e) {
            this.error = e.message;
        } finally {
            this.loading = false;
        }
    },

    async loadProviders() {
        this.loading = true;
        try {
            const data = await api('GET', '/api/providers?_=' + Date.now());
            this.providers = (data.providers || []).map(pg => ({
                ...pg,
                provider_id: pg.provider_id || (pg.template_id + '-' + pg.id),
                api_key_set: pg.api_key_set || false,
            }));
            this.error = null;
        } catch (e) {
            this.error = e.message;
        } finally {
            this.loading = false;
        }
    },

    async switchStrategy(mode) {
        await api('POST', '/admin/strategy', {mode});
        await this.loadDashboard();
    },

    async recoverAll() {
        await api('POST', '/admin/recover', {});
        await this.loadDashboard();
    },

    async switchModel(providerID, modelID) {
        try {
            await api('POST', '/admin/switch', {provider_id: providerID, model_id: modelID});
            await this.loadDashboard();
        } catch (e) {
            throw e;
        }
    },

    async clearForcedModel() {
        try {
            await api('POST', '/admin/switch', {auto: true});
            await this.loadDashboard();
        } catch (e) {
            throw e;
        }
    },

    async deleteProvider(id) {
        try {
            await api('DELETE', `/api/providers/${id}`);
            await this.loadProviders();
        } catch (e) {
            alert(e.message);
        }
    },

    async toggleModel(id, enabled) {
        await api('PUT', `/api/models/${id}`, {is_enabled: enabled});
        await this.loadProviders();
    },

    async deleteModel(id) {
        if (!confirm('确定要删除这个模型吗？')) return;
        await api('DELETE', `/api/models/${id}`);
        await this.loadProviders();
    },

    async createProvider(data) {
        await api('POST', '/api/providers', data);
        await this.loadProviders();
    },

    async updateProvider(id, data) {
        await api('PUT', `/api/providers/${id}`, data);
        await this.loadProviders();
    },

    async createModel(data) {
        await api('POST', '/api/models', data);
        await this.loadProviders();
    },

    async getModel(id) {
        return await api('GET', `/api/models/${id}`);
    },

    async updateModel(id, data) {
        await api('PUT', `/api/models/${id}`, data);
        await this.loadProviders();
    },

    async revealKey(id) {
        const data = await api('GET', `/api/providers/${id}/key`);
        return data.api_key;
    }
});
