import { api, ApiError } from '#lib/api/client.js';
import { t } from '#lib/i18n/index.svelte.js';
import { toast } from 'svelte-sonner';

export interface LoginResult {
	ok: boolean;
	error?: string;
	status?: number;
}

class AuthStore {
	authChecked = $state(false);
	isAuthenticated = $state(false);
	authEnabled = $state(true);
	entranceEnabled = $state(false);
	username = $state('admin');
	changeCredsOpen = $state(false);
	panelDomainOpen = $state(false);
	loading = $state(false);

	constructor() {
		if (typeof window !== 'undefined') {
			window.addEventListener('wdev:unauthorized', () => {
				if (this.authEnabled && this.isAuthenticated) {
					this.isAuthenticated = false;
					toast.error(t('auth.store.sessionExpiredTitle'), {
						description: t('auth.store.sessionExpiredDesc')
					});
				}
			});
			window.addEventListener('wdev:domain_required', () => {
				toast.error(t('auth.store.ipBlockedTitle'), {
					description: t('auth.store.ipBlockedDesc')
				});
			});
		}
	}

	async checkAuth(): Promise<boolean> {
		try {
			const res = await api.getAuthMe();
			this.authEnabled = res.auth_enabled;
			this.entranceEnabled = res.entrance_enabled;
			this.username = res.username || 'admin';
			this.isAuthenticated = !res.auth_enabled || res.authenticated;
			this.authChecked = true;
			return this.isAuthenticated;
		} catch (err) {
			console.error('Failed to check auth status:', err);
			this.authChecked = true;
			this.isAuthenticated = false;
			return false;
		}
	}

	async login(user: string, pass: string): Promise<LoginResult> {
		this.loading = true;
		try {
			const res = await api.login({ username: user, password: pass });
			this.username = res.username;
			this.isAuthenticated = true;
			toast.success(t('auth.store.loginSuccessTitle'), {
				description: t('auth.store.loginSuccessDesc', { username: res.username })
			});
			return { ok: true };
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : t('auth.store.networkError');
			const status = err instanceof ApiError ? err.status : 0;
			return { ok: false, error: msg, status };
		} finally {
			this.loading = false;
		}
	}

	async logout() {
		try {
			await api.logout();
		} catch {
			// ignore
		}
		this.isAuthenticated = false;
		toast.info(t('auth.store.logoutTitle'), { description: t('auth.store.logoutDesc') });
	}

	openChangeCreds() {
		this.changeCredsOpen = true;
	}

	closeChangeCreds() {
		this.changeCredsOpen = false;
	}

	openPanelDomain() {
		this.panelDomainOpen = true;
	}

	closePanelDomain() {
		this.panelDomainOpen = false;
	}
}

export const authStore = new AuthStore();
