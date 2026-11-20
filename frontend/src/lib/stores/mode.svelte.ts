import { api, ApiError } from '#lib/api/client.js';
import { toast } from 'svelte-sonner';

class ModeStore {
	mode = $state<'vps' | 'edge'>('edge');
	isSet = $state(false);
	modalOpen = $state(false);
	loading = $state(false);
	publicIP = $state('');
	netbirdIP = $state('');
	hasLocalPDNS = $state(false);
	reachable = $state(false);

	async fetchMode() {
		try {
			const res = await api.getMode();
			this.mode = res.mode;
			this.isSet = res.is_set;
			this.publicIP = res.public_ip || '';
			this.netbirdIP = res.netbird_ip || '';
			this.hasLocalPDNS = res.has_local_pdns;
			this.reachable = res.reachable;
			return res;
		} catch (err) {
			console.error('Gagal mengambil status mode server:', err);
			return null;
		}
	}

	async switchMode(target: 'vps' | 'edge') {
		this.loading = true;
		try {
			await api.setMode(target);
			this.mode = target;
			this.isSet = true;
			toast.success('Mode Server Berhasil Diubah', {
				description: `Mode operasional panel beralih ke: ${target === 'vps' ? 'Full VPS (aaPanel Style)' : 'Edge Gateway (Hybrid)'}.`
			});
			this.modalOpen = false;
			await this.fetchMode();
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : 'Gagal mengubah mode server';
			toast.error('Gagal Mengubah Mode', { description: msg });
		} finally {
			this.loading = false;
		}
	}

	openModal() {
		this.modalOpen = true;
	}

	closeModal() {
		this.modalOpen = false;
	}
}

export const modeStore = new ModeStore();
