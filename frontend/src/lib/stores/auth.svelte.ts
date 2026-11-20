import { api, ApiError } from '#lib/api/client.js';
import { toast } from 'svelte-sonner';

class AuthStore {
	authChecked = $state(false);
	isAuthenticated = $state(false);
	authEnabled = $state(true);
	entranceEnabled = $state(false);
	username = $state('admin');
	changeCredsOpen = $state(false);
	loading = $state(false);

	constructor() {
		if (typeof window !== 'undefined') {
			window.addEventListener('wdev:unauthorized', () => {
				if (this.authEnabled && this.isAuthenticated) {
					this.isAuthenticated = false;
					toast.error('Sesi Berakhir', {
						description: 'Sesi login Anda telah berakhir. Silakan login kembali.'
					});
				}
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
			console.error('Gagal memeriksa status auth:', err);
			this.authChecked = true;
			this.isAuthenticated = false;
			return false;
		}
	}

	async login(user: string, pass: string): Promise<boolean> {
		this.loading = true;
		try {
			const res = await api.login({ username: user, password: pass });
			this.username = res.username;
			this.isAuthenticated = true;
			toast.success('Login berhasil', { description: `Selamat datang kembali, ${res.username}!` });
			return true;
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : 'Login gagal. Periksa kembali username dan password.';
			toast.error('Autentikasi Gagal', { description: msg });
			return false;
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
		toast.info('Sesi Ditutup', { description: 'Anda telah berhasil logout dari panel.' });
	}

	openChangeCreds() {
		this.changeCredsOpen = true;
	}

	closeChangeCreds() {
		this.changeCredsOpen = false;
	}
}

export const authStore = new AuthStore();
