<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { cn } from '#lib/utils.js';
	import type {
		NginxConfig,
		PHPConfig,
		PostgresConfig,
		PostgresCreateDBPayload,
		PostgresInspectResult,
		RedisConfig,
		RedisInspectResult
	} from '#lib/types/api.js';
	import RedisConfigPanel from './service-config/RedisConfigPanel.svelte';
	import PostgresConfigPanel from './service-config/PostgresConfigPanel.svelte';
	import PhpConfigPanel from './service-config/PhpConfigPanel.svelte';
	import NginxConfigPanel from './service-config/NginxConfigPanel.svelte';
	import { toast } from 'svelte-sonner';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import Settings2Icon from '@lucide/svelte/icons/settings-2';

	let activeTab = $state<'settings' | 'inspect' | 'logs'>('settings');
	let loading = $state(false);
	let loadError = $state('');
	let saving = $state(false);
	let formError = $state('');

	let redisCfg = $state<RedisConfig | null>(null);
	let pgCfg = $state<PostgresConfig | null>(null);
	let phpCfg = $state<PHPConfig | null>(null);
	let nginxCfg = $state<NginxConfig | null>(null);

	let redisInspect = $state<RedisInspectResult | null>(null);
	let pgInspect = $state<PostgresInspectResult | null>(null);
	let inspectLoading = $state(false);
	let inspectError = $state('');
	let inspectSuccess = $state('');
	let creatingDB = $state(false);

	let logsText = $state('');
	let logsUnit = $state('');
	let logsLoading = $state(false);

	const hasInspect = $derived(
		cp.serviceConfigTarget === 'redis' || cp.serviceConfigTarget === 'postgresql'
	);

	const modalTitle = $derived(
		cp.serviceConfigTarget === 'redis'
			? 'Konfigurasi Redis / Valkey'
			: cp.serviceConfigTarget === 'postgresql'
				? 'Konfigurasi PostgreSQL'
				: cp.serviceConfigTarget === 'php-fpm'
					? 'Konfigurasi PHP & PHP-FPM'
					: 'Konfigurasi Global Nginx'
	);

	const metaSubtitle = $derived.by(() => {
		if (cp.serviceConfigTarget === 'redis' && redisCfg) {
			return `${redisCfg.config_path || '-'} · Unit: ${redisCfg.service_unit || '-'} (${redisCfg.active ? 'Aktif' : 'Nonaktif'})`;
		}
		if (cp.serviceConfigTarget === 'postgresql' && pgCfg) {
			return `${pgCfg.config_path || '-'} · Unit: ${pgCfg.service_unit || '-'} (${pgCfg.active ? 'Aktif' : 'Nonaktif'})`;
		}
		if (cp.serviceConfigTarget === 'php-fpm' && phpCfg) {
			return `${phpCfg.dropin_path || phpCfg.ini_path || '-'} · Pool: ${phpCfg.pool_path || '-'} (${phpCfg.active ? 'Aktif' : 'Nonaktif'})`;
		}
		if (cp.serviceConfigTarget === 'nginx' && nginxCfg) {
			return `${nginxCfg.config_path || '-'} · Unit: ${nginxCfg.service_unit || 'nginx'} (${nginxCfg.active ? 'Aktif' : 'Nonaktif'})`;
		}
		return 'Memuat konfigurasi layanan...';
	});

	$effect(() => {
		if (cp.serviceConfigOpen) {
			activeTab = 'settings';
			loadConfig(cp.serviceConfigVersion);
		}
	});

	async function loadConfig(version = '') {
		loading = true;
		loadError = '';
		formError = '';
		try {
			const res = await cp.runWithSudoRetry(
				'Membaca konfigurasi layanan ini memerlukan hak akses administrator (sudo).',
				(sudoPass) => {
					if (sudoPass) {
						return api.verifySudo(sudoPass).then(() => api.getServiceConfig(cp.serviceConfigTarget, version));
					}
					return api.getServiceConfig(cp.serviceConfigTarget, version);
				}
			);
			if (!res) {
				cp.serviceConfigOpen = false;
				return;
			}
			if (cp.serviceConfigTarget === 'redis') {
				redisCfg = res.config as RedisConfig;
			} else if (cp.serviceConfigTarget === 'postgresql') {
				pgCfg = res.config as PostgresConfig;
			} else if (cp.serviceConfigTarget === 'php-fpm') {
				phpCfg = res.config as PHPConfig;
			} else if (cp.serviceConfigTarget === 'nginx') {
				nginxCfg = res.config as NginxConfig;
			}
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Gagal memuat konfigurasi';
		} finally {
			loading = false;
		}
	}

	async function saveConfig(payload: Record<string, unknown>) {
		saving = true;
		formError = '';
		try {
			const res = await cp.runWithSudoRetry(
				'Menyimpan konfigurasi & me-restart layanan memerlukan hak akses sudo.',
				(sudoPass) =>
					api.saveServiceConfig(cp.serviceConfigTarget, sudoPass ? { ...payload, sudo_pass: sudoPass } : payload)
			);
			if (!res) return;
			cp.serviceConfigOpen = false;
			cp.loadStatus(false);
			toast.success(`${modalTitle} berhasil disimpan dan diterapkan.`);
		} catch (err) {
			formError = err instanceof Error ? err.message : 'Gagal menyimpan konfigurasi';
		} finally {
			saving = false;
		}
	}

	async function loadInspect() {
		if (cp.serviceConfigTarget !== 'redis' && cp.serviceConfigTarget !== 'postgresql') return;
		inspectLoading = true;
		inspectError = '';
		inspectSuccess = '';
		const ver = cp.serviceConfigTarget === 'postgresql' ? pgCfg?.version || '' : '';
		const svc = cp.serviceConfigTarget;
		try {
			const res = await cp.runWithSudoRetry(
				'Inspeksi layanan ini memerlukan hak akses administrator (sudo).',
				(sudoPass) => {
					if (sudoPass) {
						return api.verifySudo(sudoPass).then(() => api.inspectService(svc, ver));
					}
					return api.inspectService(svc, ver);
				}
			);
			if (!res) return;
			if (svc === 'redis') {
				redisInspect = res.inspect as RedisInspectResult;
			} else {
				pgInspect = res.inspect as PostgresInspectResult;
			}
		} catch (err) {
			inspectError = err instanceof Error ? err.message : 'Gagal mengambil data inspeksi';
		} finally {
			inspectLoading = false;
		}
	}

	async function handleFlushRedisDB(dbIndex: number) {
		const confirmed = await cp.confirm({
			title: `Flush Redis db${dbIndex}`,
			description: `Kosongkan seluruh key pada db${dbIndex}? Tindakan ini tidak dapat dibatalkan.`,
			confirmText: 'Ya, Flush DB',
			destructive: true
		});
		if (!confirmed) return;
		inspectError = '';
		try {
			const res = await cp.runWithSudoRetry(
				'Operasi flush Redis memerlukan password sudo.',
				(sudoPass) => api.flushRedisDB(dbIndex, sudoPass)
			);
			if (!res) return;
			toast.success(`Redis db${dbIndex} berhasil dikosongkan.`);
			await loadInspect();
		} catch (err) {
			inspectError = err instanceof Error ? err.message : 'Gagal melakukan flush DB';
		}
	}

	async function handleCreatePostgresDB(payload: PostgresCreateDBPayload): Promise<boolean> {
		creatingDB = true;
		inspectError = '';
		inspectSuccess = '';
		try {
			const res = await cp.runWithSudoRetry(
				'Membuat database PostgreSQL memerlukan password sudo.',
				(sudoPass) => api.createPostgresDB(sudoPass ? { ...payload, sudo_pass: sudoPass } : payload)
			);
			if (!res) return false;
			inspectSuccess = `Database "${payload.database}" berhasil dibuat.`;
			await loadInspect();
			return true;
		} catch (err) {
			inspectError = err instanceof Error ? err.message : 'Gagal membuat database';
			return false;
		} finally {
			creatingDB = false;
		}
	}

	async function loadLogs() {
		logsLoading = true;
		logsText = 'Memuat log layanan...';
		const ver =
			cp.serviceConfigTarget === 'postgresql'
				? pgCfg?.version || ''
				: cp.serviceConfigTarget === 'php-fpm'
					? phpCfg?.version || ''
					: '';
		try {
			const res = await cp.runWithSudoRetry(
				'Membaca log layanan memerlukan hak akses administrator (sudo).',
				(sudoPass) => {
					if (sudoPass) {
						return api
							.verifySudo(sudoPass)
							.then(() => api.getServiceLogs(cp.serviceConfigTarget, ver, 100));
					}
					return api.getServiceLogs(cp.serviceConfigTarget, ver, 100);
				}
			);
			if (!res) return;
			logsUnit = res.unit || '';
			logsText = res.logs || '(Log kosong)';
		} catch (err) {
			logsText = `Gagal memuat log: ${err instanceof Error ? err.message : 'Error'}`;
		} finally {
			logsLoading = false;
		}
	}

	function handleTabChange(val: string) {
		if (val === 'inspect') loadInspect();
		else if (val === 'logs') loadLogs();
	}
</script>

<Dialog.Root bind:open={cp.serviceConfigOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-3xl">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2 pr-6">
				<Settings2Icon class="size-4 shrink-0 text-primary" />
				<span class="truncate">{modalTitle}</span>
			</Dialog.Title>
			<Dialog.Description class="break-all font-mono text-xs">{metaSubtitle}</Dialog.Description>
		</Dialog.Header>

		{#if loadError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>Gagal Memuat Konfigurasi</Alert.Title>
				<Alert.Description>{loadError}</Alert.Description>
			</Alert.Root>
		{:else if loading && !redisCfg && !pgCfg && !phpCfg && !nginxCfg}
			<div class="flex flex-col gap-3 py-4">
				<Skeleton class="h-8 w-1/2" />
				<Skeleton class="h-24 w-full" />
				<Skeleton class="h-24 w-full" />
			</div>
		{:else}
			<Tabs.Root bind:value={activeTab} onValueChange={handleTabChange} class="flex min-h-0 flex-1 flex-col gap-3">
				<Tabs.List class={cn('grid w-full sm:inline-flex sm:w-auto', hasInspect ? 'grid-cols-3' : 'grid-cols-2')}>
					<Tabs.Trigger value="settings">Pengaturan</Tabs.Trigger>
					{#if hasInspect}
						<Tabs.Trigger value="inspect">
							<span class="truncate">{cp.serviceConfigTarget === 'redis' ? 'Inspeksi' : 'Database'}</span>
						</Tabs.Trigger>
					{/if}
					<Tabs.Trigger value="logs">Log</Tabs.Trigger>
				</Tabs.List>

				<div class="min-h-0 flex-1 overflow-y-auto pr-1">
					<Tabs.Content value="settings" class="mt-0">
						{#if cp.serviceConfigTarget === 'redis' && redisCfg}
							<RedisConfigPanel
								mode="settings"
								bind:config={redisCfg}
								inspectData={redisInspect}
								{inspectLoading}
								{inspectError}
								{saving}
								{formError}
								onSave={saveConfig}
								onCancel={() => (cp.serviceConfigOpen = false)}
								onRefreshInspect={loadInspect}
								onFlushDB={handleFlushRedisDB}
							/>
						{:else if cp.serviceConfigTarget === 'postgresql' && pgCfg}
							<PostgresConfigPanel
								mode="settings"
								bind:config={pgCfg}
								inspectData={pgInspect}
								{inspectLoading}
								{inspectError}
								{inspectSuccess}
								{saving}
								{creatingDB}
								{formError}
								onVersionChange={(v) => loadConfig(v)}
								onSave={saveConfig}
								onCancel={() => (cp.serviceConfigOpen = false)}
								onRefreshInspect={loadInspect}
								onCreateDB={handleCreatePostgresDB}
							/>
						{:else if cp.serviceConfigTarget === 'php-fpm' && phpCfg}
							<PhpConfigPanel
								bind:config={phpCfg}
								{saving}
								{formError}
								onVersionChange={(v) => loadConfig(v)}
								onSave={saveConfig}
								onCancel={() => (cp.serviceConfigOpen = false)}
							/>
						{:else if cp.serviceConfigTarget === 'nginx' && nginxCfg}
							<NginxConfigPanel
								bind:config={nginxCfg}
								{saving}
								{formError}
								onSave={saveConfig}
								onCancel={() => (cp.serviceConfigOpen = false)}
							/>
						{/if}
					</Tabs.Content>

					{#if hasInspect}
						<Tabs.Content value="inspect" class="mt-0">
							{#if cp.serviceConfigTarget === 'redis' && redisCfg}
								<RedisConfigPanel
									mode="inspect"
									bind:config={redisCfg}
									inspectData={redisInspect}
									{inspectLoading}
									{inspectError}
									{saving}
									{formError}
									onSave={saveConfig}
									onCancel={() => (cp.serviceConfigOpen = false)}
									onRefreshInspect={loadInspect}
									onFlushDB={handleFlushRedisDB}
								/>
							{:else if cp.serviceConfigTarget === 'postgresql' && pgCfg}
								<PostgresConfigPanel
									mode="inspect"
									bind:config={pgCfg}
									inspectData={pgInspect}
									{inspectLoading}
									{inspectError}
									{inspectSuccess}
									{saving}
									{creatingDB}
									{formError}
									onVersionChange={(v) => loadConfig(v)}
									onSave={saveConfig}
									onCancel={() => (cp.serviceConfigOpen = false)}
									onRefreshInspect={loadInspect}
									onCreateDB={handleCreatePostgresDB}
								/>
							{/if}
						</Tabs.Content>
					{/if}

					<Tabs.Content value="logs" class="mt-0">
						<div class="flex flex-col gap-3">
							<div class="flex items-center justify-between gap-2">
								<h4 class="text-sm font-semibold">
									{logsUnit ? `Log Layanan (${logsUnit})` : 'Log Layanan'}
								</h4>
								<Button type="button" variant="outline" size="sm" disabled={logsLoading} onclick={loadLogs}>
									<RefreshCwIcon data-icon="inline-start" />
									Muat Ulang Log
								</Button>
							</div>
							<pre
								class="max-h-[48dvh] overflow-auto rounded-lg border bg-terminal p-4 font-mono text-xs leading-relaxed text-terminal-foreground whitespace-pre-wrap break-words"
							>{logsText}</pre>
						</div>
					</Tabs.Content>
				</div>
			</Tabs.Root>
		{/if}
	</Dialog.Content>
</Dialog.Root>
