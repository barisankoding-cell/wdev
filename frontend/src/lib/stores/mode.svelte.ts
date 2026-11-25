import { api, ApiError } from '#lib/api/client.js';
import { t } from '#lib/i18n/index.svelte.js';
import { cp } from '#lib/stores/control-plane.svelte.js';
import { toast } from 'svelte-sonner';

export type ServerMode = 'local' | 'edge' | 'vps';

export const MODE_LABELS: Record<ServerMode, string> = {
	get local() {
		return t('stores.mode.labels.local');
	},
	get edge() {
		return t('stores.mode.labels.edge');
	},
	get vps() {
		return t('stores.mode.labels.vps');
	}
};

class ModeStore {
	mode = $state<ServerMode>('local');
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
			console.error('Failed to fetch server mode status:', err);
			return null;
		}
	}

	async switchMode(target: ServerMode) {
		this.loading = true;
		try {
			await api.setMode(target);
			this.mode = target;
			this.isSet = true;
			toast.success(t('stores.mode.switchSuccessTitle'), {
				description: t('stores.mode.switchSuccessDesc', { label: MODE_LABELS[target] })
			});
			this.modalOpen = false;
			await this.fetchMode();
			void cp.loadStatus(false);
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : t('stores.mode.switchErrorFallback');
			toast.error(t('stores.mode.switchErrorTitle'), { description: msg });
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
