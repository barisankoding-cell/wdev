<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { cn } from '#lib/utils.js';
	import type {
		DockerConfig,
		NginxConfig,
		PHPConfig,
		PHPInspectResult,
		PostgresConfig,
		PostgresCreateDBPayload,
		PostgresDeleteRolePayload,
		PostgresInspectResult,
		PostgresSaveRolePayload,
		RedisConfig,
		RedisInspectResult
	} from '#lib/types/api.js';
	import RedisConfigPanel from './service-config/RedisConfigPanel.svelte';
	import PostgresConfigPanel from './service-config/PostgresConfigPanel.svelte';
	import PhpConfigPanel from './service-config/PhpConfigPanel.svelte';
	import NginxConfigPanel from './service-config/NginxConfigPanel.svelte';
	import DockerConfigPanel from './service-config/DockerConfigPanel.svelte';
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
	let dockerCfg = $state<DockerConfig | null>(null);

	let redisInspect = $state<RedisInspectResult | null>(null);
	let pgInspect = $state<PostgresInspectResult | null>(null);
	let phpInspect = $state<PHPInspectResult | null>(null);
	let inspectLoading = $state(false);
	let inspectError = $state('');
	let inspectSuccess = $state('');
	let creatingDB = $state(false);
	let savingRole = $state(false);
	let deletingRole = $state('');
	let flushingDB = $state<number | null>(null);

	let logsText = $state('');
	let logsUnit = $state('');
	let logsLoading = $state(false);

	const hasInspect = $derived(
		cp.serviceConfigTarget === 'redis' ||
			cp.serviceConfigTarget === 'postgresql' ||
			cp.serviceConfigTarget === 'php-fpm'
	);

	const modalTitle = $derived(
		cp.serviceConfigTarget === 'redis'
			? t('modals.serviceConfig.titles.redis')
			: cp.serviceConfigTarget === 'postgresql'
				? t('modals.serviceConfig.titles.postgresql')
				: cp.serviceConfigTarget === 'php-fpm'
					? t('modals.serviceConfig.titles.phpFpm')
					: cp.serviceConfigTarget === 'docker'
						? t('modals.serviceConfig.titles.docker')
						: t('modals.serviceConfig.titles.nginx')
	);

	const metaSubtitle = $derived.by(() => {
		const activeStr = t('common.status.active');
		const inactiveStr = t('common.status.inactive');
		if (cp.serviceConfigTarget === 'redis' && redisCfg) {
			return `${redisCfg.config_path || '-'} · Unit: ${redisCfg.service_unit || '-'} (${redisCfg.active ? activeStr : inactiveStr})`;
		}
		if (cp.serviceConfigTarget === 'postgresql' && pgCfg) {
			return `${pgCfg.config_path || '-'} · Unit: ${pgCfg.service_unit || '-'} (${pgCfg.active ? activeStr : inactiveStr})`;
		}
		if (cp.serviceConfigTarget === 'php-fpm' && phpCfg) {
			return `${phpCfg.dropin_path || phpCfg.ini_path || '-'} · Pool: ${phpCfg.pool_path || '-'} (${phpCfg.active ? activeStr : inactiveStr})`;
		}
		if (cp.serviceConfigTarget === 'nginx' && nginxCfg) {
			return `${nginxCfg.config_path || '-'} · Unit: ${nginxCfg.service_unit || 'nginx'} (${nginxCfg.active ? activeStr : inactiveStr})`;
		}
		if (cp.serviceConfigTarget === 'docker' && dockerCfg) {
			const statusTxt = !dockerCfg.installed
				? t('modals.serviceConfig.dockerNotInstalledSub')
				: dockerCfg.active
					? activeStr
					: inactiveStr;
			return `${dockerCfg.config_path || '/etc/docker/daemon.json'} · Unit: ${dockerCfg.service_unit || 'docker'} (${statusTxt})`;
		}
		return t('modals.serviceConfig.loadingSub');
	});

	$effect(() => {
		if (cp.serviceConfigOpen) {
			activeTab = 'settings';
			redisInspect = null;
			pgInspect = null;
			phpInspect = null;
			loadConfig(cp.serviceConfigVersion);
		}
	});

	async function loadConfig(version = '') {
		loading = true;
		loadError = '';
		formError = '';
		try {
			const res = await cp.runWithSudoRetry(
				t('modals.serviceConfig.loadSudoPrompt'),
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
				if (activeTab === 'inspect') {
					void loadInspect(phpCfg.version);
				}
			} else if (cp.serviceConfigTarget === 'nginx') {
				nginxCfg = res.config as NginxConfig;
			} else if (cp.serviceConfigTarget === 'docker') {
				dockerCfg = res.config as DockerConfig;
			}
		} catch (err) {
			loadError = err instanceof Error ? err.message : t('modals.serviceConfig.loadErrorFallback');
		} finally {
			loading = false;
		}
	}

	async function saveConfig(payload: Record<string, unknown>) {
		saving = true;
		formError = '';
		try {
			const res = await cp.runWithSudoRetry(
				t('modals.serviceConfig.saveSudoPrompt'),
				(sudoPass) =>
					api.saveServiceConfig(cp.serviceConfigTarget, sudoPass ? { ...payload, sudo_pass: sudoPass } : payload)
			);
			if (!res) return;
			cp.serviceConfigOpen = false;
			cp.loadStatus(false);
			toast.success(t('modals.serviceConfig.saveSuccessToast', { title: modalTitle }));
		} catch (err) {
			formError = err instanceof Error ? err.message : t('modals.serviceConfig.saveErrorFallback');
		} finally {
			saving = false;
		}
	}

	async function loadInspect(overrideVer = '') {
		if (
			cp.serviceConfigTarget !== 'redis' &&
			cp.serviceConfigTarget !== 'postgresql' &&
			cp.serviceConfigTarget !== 'php-fpm'
		) {
			return;
		}
		inspectLoading = true;
		inspectError = '';
		inspectSuccess = '';
		const svc = cp.serviceConfigTarget;
		const ver =
			svc === 'postgresql'
				? overrideVer || pgCfg?.version || ''
				: svc === 'php-fpm'
					? overrideVer || phpCfg?.version || ''
					: '';
		try {
			const res = await cp.runWithSudoRetry(
				t('modals.serviceConfig.inspectSudoPrompt'),
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
			} else if (svc === 'postgresql') {
				pgInspect = res.inspect as PostgresInspectResult;
			} else {
				phpInspect = res.inspect as PHPInspectResult;
			}
		} catch (err) {
			inspectError = err instanceof Error ? err.message : t('modals.serviceConfig.inspectErrorFallback');
		} finally {
			inspectLoading = false;
		}
	}

	async function handleFlushRedisDB(dbIndex: number) {
		const confirmed = await cp.confirm({
			title: t('modals.serviceConfig.flushConfirmTitle', { db: dbIndex }),
			description: t('modals.serviceConfig.flushConfirmDesc', { db: dbIndex }),
			confirmText: t('modals.serviceConfig.flushConfirmBtn'),
			destructive: true
		});
		if (!confirmed) return;
		flushingDB = dbIndex;
		inspectError = '';
		try {
			const res = await cp.runWithSudoRetry(
				t('modals.serviceConfig.flushSudoPrompt'),
				(sudoPass) => api.flushRedisDB(dbIndex, sudoPass)
			);
			if (!res) return;
			toast.success(t('modals.serviceConfig.flushSuccessToast', { db: dbIndex }));
			await loadInspect();
		} catch (err) {
			inspectError = err instanceof Error ? err.message : t('modals.serviceConfig.flushErrorFallback');
		} finally {
			flushingDB = null;
		}
	}

	async function handleCreatePostgresDB(payload: PostgresCreateDBPayload): Promise<boolean> {
		creatingDB = true;
		inspectError = '';
		inspectSuccess = '';
		try {
			const res = await cp.runWithSudoRetry(
				t('modals.serviceConfig.createDbSudoPrompt'),
				(sudoPass) => api.createPostgresDB(sudoPass ? { ...payload, sudo_pass: sudoPass } : payload)
			);
			if (!res) return false;
			inspectSuccess = t('modals.serviceConfig.createDbSuccess', { database: payload.database });
			await loadInspect();
			return true;
		} catch (err) {
			inspectError = err instanceof Error ? err.message : t('modals.serviceConfig.createDbErrorFallback');
			return false;
		} finally {
			creatingDB = false;
		}
	}

	async function handleSavePostgresRole(payload: PostgresSaveRolePayload): Promise<boolean> {
		savingRole = true;
		inspectError = '';
		inspectSuccess = '';
		try {
			const res = await cp.runWithSudoRetry(
				t('modals.serviceConfig.saveRoleSudoPrompt'),
				(sudoPass) => api.savePostgresRole(sudoPass ? { ...payload, sudo_pass: sudoPass } : payload)
			);
			if (!res) return false;
			inspectSuccess =
				res.status === 'created'
					? t('modals.serviceConfig.createRoleSuccess', { role: payload.role })
					: t('modals.serviceConfig.updateRoleSuccess', { role: payload.role });
			await loadInspect();
			return true;
		} catch (err) {
			inspectError = err instanceof Error ? err.message : t('modals.serviceConfig.saveRoleErrorFallback');
			return false;
		} finally {
			savingRole = false;
		}
	}

	async function handleDeletePostgresRole(payload: PostgresDeleteRolePayload): Promise<boolean> {
		const confirmed = await cp.confirm({
			title: t('modals.serviceConfig.deleteRoleConfirmTitle', { role: payload.role }),
			description: t('modals.serviceConfig.deleteRoleConfirmDesc', { role: payload.role }),
			confirmText: t('modals.serviceConfig.deleteRoleConfirmBtn'),
			destructive: true
		});
		if (!confirmed) return false;
		deletingRole = payload.role;
		inspectError = '';
		inspectSuccess = '';
		try {
			const res = await cp.runWithSudoRetry(
				t('modals.serviceConfig.deleteRoleSudoPrompt'),
				(sudoPass) => api.deletePostgresRole(sudoPass ? { ...payload, sudo_pass: sudoPass } : payload)
			);
			if (!res) return false;
			inspectSuccess = t('modals.serviceConfig.deleteRoleSuccess', { role: payload.role });
			await loadInspect();
			return true;
		} catch (err) {
			inspectError = err instanceof Error ? err.message : t('modals.serviceConfig.deleteRoleErrorFallback');
			return false;
		} finally {
			deletingRole = '';
		}
	}

	async function loadLogs() {
		logsLoading = true;
		logsText = t('modals.serviceConfig.logsLoadingText');
		const ver =
			cp.serviceConfigTarget === 'postgresql'
				? pgCfg?.version || ''
				: cp.serviceConfigTarget === 'php-fpm'
					? phpCfg?.version || ''
					: '';
		try {
			const res = await cp.runWithSudoRetry(
				t('modals.serviceConfig.logsSudoPrompt'),
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
			logsText = res.logs || t('modals.serviceConfig.logsEmptyText');
		} catch (err) {
			logsText = t('modals.serviceConfig.logsLoadError', {
				error: err instanceof Error ? err.message : 'Error'
			});
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
	<Dialog.Content class="flex max-h-[92dvh] max-w-[calc(100vw-1.5rem)] flex-col gap-4 overflow-hidden sm:max-w-[min(94vw,80rem)]">
		<Dialog.Header class="shrink-0 pr-8">
			<Dialog.Title class="flex items-center gap-2">
				<Settings2Icon class="size-4 shrink-0 text-primary" />
				<span class="truncate">{modalTitle}</span>
			</Dialog.Title>
			<Dialog.Description class="font-mono text-xs break-words">{metaSubtitle}</Dialog.Description>
		</Dialog.Header>

		{#if loadError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>{t('modals.serviceConfig.loadFailedTitle')}</Alert.Title>
				<Alert.Description>{loadError}</Alert.Description>
			</Alert.Root>
		{:else if loading && !redisCfg && !pgCfg && !phpCfg && !nginxCfg && !dockerCfg}
			<div class="flex flex-col gap-3 py-4">
				<Skeleton class="h-8 w-1/2" />
				<Skeleton class="h-24 w-full" />
				<Skeleton class="h-24 w-full" />
			</div>
		{:else}
			<Tabs.Root bind:value={activeTab} onValueChange={handleTabChange} class="flex min-h-0 flex-1 flex-col gap-3">
				<Tabs.List class={cn('grid w-full shrink-0 sm:inline-flex sm:w-auto', hasInspect ? 'grid-cols-3' : 'grid-cols-2')}>
					<Tabs.Trigger value="settings">{t('modals.serviceConfig.tabSettings')}</Tabs.Trigger>
					{#if hasInspect}
						<Tabs.Trigger value="inspect">
							<span class="truncate">
								{cp.serviceConfigTarget === 'redis'
									? t('modals.serviceConfig.tabInspectRedis')
									: cp.serviceConfigTarget === 'php-fpm'
										? t('modals.serviceConfig.tabInspectPhp')
										: t('modals.serviceConfig.tabInspectPg')}
							</span>
						</Tabs.Trigger>
					{/if}
					<Tabs.Trigger value="logs">{t('modals.serviceConfig.tabLogs')}</Tabs.Trigger>
				</Tabs.List>

				<div class="-mx-1 min-h-0 flex-1 overflow-y-auto overscroll-contain px-1">
					<Tabs.Content value="settings" class="mt-0">
						{#if cp.serviceConfigTarget === 'redis' && redisCfg}
							<RedisConfigPanel
								mode="settings"
								bind:config={redisCfg}
								inspectData={redisInspect}
								{inspectLoading}
								{inspectError}
								{flushingDB}
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
								{savingRole}
								{deletingRole}
								{formError}
								onVersionChange={(v) => loadConfig(v)}
								onSave={saveConfig}
								onCancel={() => (cp.serviceConfigOpen = false)}
								onRefreshInspect={loadInspect}
								onCreateDB={handleCreatePostgresDB}
								onSaveRole={handleSavePostgresRole}
								onDeleteRole={handleDeletePostgresRole}
							/>
						{:else if cp.serviceConfigTarget === 'php-fpm' && phpCfg}
							<PhpConfigPanel
								mode="settings"
								bind:config={phpCfg}
								inspectData={phpInspect}
								{inspectLoading}
								{inspectError}
								{saving}
								{formError}
								onVersionChange={(v) => loadConfig(v)}
								onSave={saveConfig}
								onCancel={() => (cp.serviceConfigOpen = false)}
								onRefreshInspect={() => loadInspect()}
							/>
						{:else if cp.serviceConfigTarget === 'nginx' && nginxCfg}
							<NginxConfigPanel
								bind:config={nginxCfg}
								{saving}
								{formError}
								onSave={saveConfig}
								onCancel={() => (cp.serviceConfigOpen = false)}
							/>
						{:else if cp.serviceConfigTarget === 'docker' && dockerCfg}
							<DockerConfigPanel
								bind:config={dockerCfg}
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
									{flushingDB}
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
									{savingRole}
									{deletingRole}
									{formError}
									onVersionChange={(v) => loadConfig(v)}
									onSave={saveConfig}
									onCancel={() => (cp.serviceConfigOpen = false)}
									onRefreshInspect={loadInspect}
									onCreateDB={handleCreatePostgresDB}
									onSaveRole={handleSavePostgresRole}
									onDeleteRole={handleDeletePostgresRole}
								/>
							{:else if cp.serviceConfigTarget === 'php-fpm' && phpCfg}
								<PhpConfigPanel
									mode="inspect"
									bind:config={phpCfg}
									inspectData={phpInspect}
									{inspectLoading}
									{inspectError}
									{saving}
									{formError}
									onVersionChange={(v) => loadConfig(v)}
									onSave={saveConfig}
									onCancel={() => (cp.serviceConfigOpen = false)}
									onRefreshInspect={() => loadInspect()}
								/>
							{/if}
						</Tabs.Content>
					{/if}

					<Tabs.Content value="logs" class="mt-0">
						<div class="flex flex-col gap-3">
							<div class="flex items-center justify-between gap-2">
								<h4 class="text-sm font-semibold">
									{logsUnit ? t('modals.serviceConfig.logsHeadingUnit', { unit: logsUnit }) : t('modals.serviceConfig.logsHeading')}
								</h4>
								<Button type="button" variant="outline" size="sm" disabled={logsLoading} onclick={loadLogs}>
									{#if logsLoading}
										<Spinner data-icon="inline-start" />
										{t('common.actions.loading')}
									{:else}
										<RefreshCwIcon data-icon="inline-start" />
										{t('modals.serviceConfig.reloadLogsBtn')}
									{/if}
								</Button>
							</div>
							<pre
								class="max-h-[58dvh] overflow-auto rounded-lg border bg-terminal p-4 font-mono text-xs leading-relaxed text-terminal-foreground whitespace-pre-wrap break-words"
							>{logsText}</pre>
						</div>
					</Tabs.Content>
				</div>
			</Tabs.Root>
		{/if}
	</Dialog.Content>
</Dialog.Root>
