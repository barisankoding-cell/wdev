<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import { api } from '#lib/api/client.js';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import { cn } from '#lib/utils.js';
	import type {
		MountDriveInfo,
		ServiceConfigKey,
		ServiceStatus,
		UpdaterStatus
	} from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import SearchIcon from '@lucide/svelte/icons/search';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import PlayIcon from '@lucide/svelte/icons/play';
	import SquareIcon from '@lucide/svelte/icons/square';
	import RotateCwIcon from '@lucide/svelte/icons/rotate-cw';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import ArrowUpCircleIcon from '@lucide/svelte/icons/arrow-up-circle';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import MemoryStickIcon from '@lucide/svelte/icons/memory-stick';
	import ServerIcon from '@lucide/svelte/icons/server';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';

	let refreshingServices = $state(false);
	let servicePending = $state<Record<string, boolean>>({});
	let updaterStatus = $state<UpdaterStatus | null>(null);
	let checkingSelfUpdate = $state(false);
	let applyingUpdate = $state(false);

	const configurableServices = new Set<string>(['redis', 'postgresql', 'php-fpm', 'nginx', 'docker']);

	/** Merge external and /mnt mounts without duplicating mount_point. */
	function mergeExtraMounts(other?: MountDriveInfo[], mnt?: MountDriveInfo[]): MountDriveInfo[] {
		const byPath = new Map<string, MountDriveInfo>();
		for (const d of other ?? []) {
			byPath.set(d.mount_point, d);
		}
		for (const d of mnt ?? []) {
			if (!byPath.has(d.mount_point)) {
				byPath.set(d.mount_point, d);
			}
		}
		return Array.from(byPath.values());
	}

	function formatBinaryBytes(bytes: number): string {
		if (!bytes || bytes <= 0) return '0 B';
		const tib = 1024 * 1024 * 1024 * 1024;
		const gib = 1024 * 1024 * 1024;
		const mib = 1024 * 1024;
		const kib = 1024;
		if (bytes >= tib) return `${(bytes / tib).toFixed(2)} TiB`;
		if (bytes >= gib) return `${(bytes / gib).toFixed(2)} GiB`;
		if (bytes >= mib) return `${(bytes / mib).toFixed(2)} MiB`;
		if (bytes >= kib) return `${(bytes / kib).toFixed(2)} KiB`;
		return `${Math.round(bytes)} B`;
	}

	function getProgressToneClass(pct: number): string {
		if (pct >= 90) return '[&>[data-slot=progress-indicator]]:bg-destructive';
		if (pct >= 75) return '[&>[data-slot=progress-indicator]]:bg-warning';
		return '';
	}

	function localizeServiceCategory(category: string): string {
		return i18n.tryTranslate(`dashboard.services.categories.${category}`) ?? category;
	}

	async function copyAddress(val: string, label: string) {
		if (!val || val === '-') return;
		try {
			await navigator.clipboard.writeText(val);
			toast.success(t('dashboard.copySuccess', { label, val }));
		} catch {
			toast.error(t('dashboard.copyFailed', { label }));
		}
	}

	onMount(() => {
		api.getUpdaterStatus(false).then((res) => (updaterStatus = res)).catch(() => {});
	});

	async function refreshServices() {
		refreshingServices = true;
		try {
			await cp.loadStatus(true);
			toast.success(t('dashboard.servicesRefreshed'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('dashboard.checkUpdatesFailed'));
		} finally {
			refreshingServices = false;
		}
	}

	async function checkSelfUpdate() {
		checkingSelfUpdate = true;
		try {
			const res = await api.getUpdaterStatus(true);
			updaterStatus = res;
			if (res.error) {
				toast.info(res.error);
			} else if (res.has_update) {
				toast.info(t('dashboard.selfUpdateAvailable', { latest: res.latest ?? '' }));
			} else {
				toast.success(t('dashboard.selfUpdateLatest', { current: res.current }));
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('dashboard.selfUpdateCheckFailed'));
		} finally {
			checkingSelfUpdate = false;
		}
	}

	async function applySelfUpdate() {
		const changelogSnippet = updaterStatus?.changelog
			? `\n\n${t('dashboard.selfUpdateReleaseNotes', { latest: updaterStatus?.latest ?? '' })}\n${updaterStatus.changelog.trim().slice(0, 300)}${updaterStatus.changelog.trim().length > 300 ? '...' : ''}`
			: '';
		const ok = await cp.confirm({
			title: t('dashboard.selfUpdateConfirmTitle', {
				latest: updaterStatus?.latest || t('dashboard.selfUpdateConfirmLatest')
			}),
			description: t('dashboard.selfUpdateConfirmDesc', { notes: changelogSnippet }),
			confirmText: t('dashboard.selfUpdateConfirmBtn')
		});
		if (!ok) return;
		applyingUpdate = true;
		try {
			const res = await cp.runWithSudoRetry(
				t('dashboard.selfUpdateSudoPrompt'),
				(sudoPass) => api.applySelfUpdate('', sudoPass)
			);
			if (res) {
				if (res.status === 'started') {
					cp.armSelfUpdateReload();
				}
				cp.openTerminal(t('dashboard.selfUpdateTerminalLabel'));
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('dashboard.selfUpdateFailed'));
		} finally {
			applyingUpdate = false;
		}
	}

	async function handleServiceAction(svc: ServiceStatus, action: 'start' | 'stop' | 'restart') {
		servicePending = { ...servicePending, [svc.name]: true };
		try {
			await api.controlService(svc.name, action);
			toast.success(
				t('dashboard.serviceActionSuccess', {
					name: svc.display_name,
					action: t(`dashboard.actionVerbs.${action}`)
				})
			);
			await cp.loadStatus(false);
		} catch (err) {
			toast.error(
				err instanceof Error
					? err.message
					: t('dashboard.serviceActionFailed', { action, name: svc.display_name })
			);
		} finally {
			servicePending = { ...servicePending, [svc.name]: false };
		}
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('dashboard.title')}</h1>
			<p class="text-sm text-muted-foreground">
				{t('dashboard.subtitle')}
			</p>
		</div>
		<div
			class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center"
			role="toolbar"
			aria-label={t('dashboard.toolbarAria')}
		>
			<Button size="sm" onclick={() => cp.triggerInstall('update')}>
				<RefreshCwIcon data-icon="inline-start" />
				{t('dashboard.updateAllBtn')}
			</Button>
			<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('check-updates')}>
				<SearchIcon data-icon="inline-start" />
				{t('dashboard.checkUpdatesBtn')}
			</Button>
			<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('self-check')}>
				<ShieldCheckIcon data-icon="inline-start" />
				{t('dashboard.selfCheckBtn')}
			</Button>
			<Button variant="outline" size="sm" onclick={() => cp.openTerminal()}>
				<TerminalIcon data-icon="inline-start" />
				{t('dashboard.liveTerminalBtn')}
			</Button>
		</div>
	</div>

	<!-- wdev Panel Update Banner -->
	{#if updaterStatus?.has_update}
		<div
			class="relative flex flex-col gap-3 rounded-xl border border-amber-500/40 bg-amber-500/10 p-4 text-card-foreground shadow-sm dark:bg-amber-950/25 sm:flex-row sm:items-center sm:justify-between"
			role="alert"
		>
			<div class="flex items-start gap-3">
				<div class="rounded-lg bg-amber-500/20 p-2 text-amber-600 dark:text-amber-400 shrink-0">
					<ArrowUpCircleIcon class="size-5" />
				</div>
				<div>
					<div class="flex flex-wrap items-center gap-2">
						<span class="font-semibold text-foreground">{t('dashboard.updaterBannerTitle')}</span>
						<Badge variant="default" class="bg-amber-600 font-mono text-xs text-white hover:bg-amber-700">
							v{updaterStatus.latest}
						</Badge>
					</div>
					<p class="mt-0.5 text-xs text-muted-foreground sm:text-sm">
						{t('dashboard.updaterBannerDesc', { current: updaterStatus.current })}
					</p>
				</div>
			</div>
			<div class="flex items-center gap-2 self-end sm:self-auto shrink-0">
				<Button
					size="sm"
					class="bg-amber-600 text-white hover:bg-amber-700"
					disabled={applyingUpdate}
					onclick={applySelfUpdate}
				>
					{#if applyingUpdate}
						<Spinner data-icon="inline-start" />
						{t('dashboard.updatingBtn')}
					{:else}
						<ArrowUpCircleIcon data-icon="inline-start" />
						{t('dashboard.updateNowBtn')}
					{/if}
				</Button>
			</div>
		</div>
	{/if}

	{#if cp.statusError}
		<Alert.Root variant="destructive">
			<AlertCircleIcon />
			<Alert.Title>{t('dashboard.statusErrorTitle')}</Alert.Title>
			<Alert.Description>{cp.statusError}</Alert.Description>
		</Alert.Root>
	{/if}

	<!-- System Node Summary (Proxmox-style Unified Panel) -->
	{#if !cp.systemStatus}
		<Card.Root>
			<Card.Header class="border-b">
				<Skeleton class="h-5 w-64" />
			</Card.Header>
			<Card.Content class="flex flex-col gap-5 pt-4">
				<div class="grid grid-cols-1 gap-x-8 gap-y-4 md:grid-cols-2">
					{#each [1, 2, 3, 4, 5, 6] as idx (idx)}
						<div class="flex flex-col gap-2">
							<div class="flex items-center justify-between gap-2">
								<Skeleton class="h-4 w-24" />
								<Skeleton class="h-4 w-32" />
							</div>
							<Skeleton class="h-1.5 w-full" />
						</div>
					{/each}
				</div>
				<Separator />
				<div class="flex flex-col gap-2.5">
					{#each [1, 2, 3, 4, 5] as idx (idx)}
						<div class="flex items-center justify-between gap-4">
							<Skeleton class="h-4 w-28" />
							<Skeleton class="h-4 w-56" />
						</div>
					{/each}
				</div>
			</Card.Content>
		</Card.Root>
	{:else}
		{@const st = cp.systemStatus}
		{@const cpuCores = st.cpu_cores && st.cpu_cores > 0 ? st.cpu_cores : 1}
		{@const cpuPct = st.cpu_usage_percent ?? Math.min(st.load_percent ?? 0, 100)}
		{@const loadPct = st.load_percent ?? st.load_1m_percent ?? 0}
		{@const memTotalBytes = st.mem_total_bytes ?? st.mem_total_mb * 1024 * 1024}
		{@const memUsedBytes = st.mem_used_bytes ?? st.mem_used_mb * 1024 * 1024}
		{@const memPctExact = st.mem_percent_exact ?? st.mem_percent}
		{@const diskTotalBytes = st.disk_total_bytes ?? st.disk_total_gb * 1024 * 1024 * 1024}
		{@const diskUsedBytes = st.disk_used_bytes ?? st.disk_used_gb * 1024 * 1024 * 1024}
		{@const diskPctExact = st.disk_percent_exact ?? st.disk_percent}
		{@const swapTotalBytes = st.swap_total_bytes ?? 0}
		{@const swapUsedBytes = st.swap_used_bytes ?? 0}
		{@const swapPct = st.swap_percent ?? 0}
		{@const extraMounts = mergeExtraMounts(st.other_mounts, st.mount_drives)}
		{@const uptimeLabel = i18n.formatDaysHMS(st.uptime_seconds, st.uptime_string)}
		{@const cpuHardwareLabel = st.cpu_model
			? t('dashboard.hardware.cpuSocketLabel', {
					cores: cpuCores,
					model: st.cpu_model,
					sockets: st.cpu_sockets || 1
				})
			: `${cpuCores} vCPU`}
		{@const installedServices = (st.services || []).filter((s) => s.installed)}
		{@const activeServicesCount = installedServices.filter((s) => s.active).length}
		{@const updatesCount = installedServices.filter((s) => s.has_update).length}
		{@const hasPanelUpdate = Boolean(updaterStatus?.has_update)}
		{@const totalPendingUpdates = updatesCount + (hasPanelUpdate ? 1 : 0)}
		{@const vpsIP = (st.public_ip && st.public_ip !== '-' && st.public_ip !== '127.0.0.1')
			? st.public_ip
			: (modeStore.mode !== 'local' && modeStore.publicIP && modeStore.publicIP !== '-' ? modeStore.publicIP : '')}
		{@const lanIP = st.lan_ip || ''}
		{@const netbirdInstalled = Boolean((st.services || []).find((s) => s.name === 'netbird')?.installed)}
		{@const netbirdIP = netbirdInstalled && st.netbird_ip && st.netbird_ip !== '-' ? st.netbird_ip : ''}
		<Card.Root class="gap-0 py-0">
			<Card.Header class="border-b px-4 py-3 [.border-b]:pb-3 sm:px-5">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<span class="inline-flex min-w-0 items-center gap-1.5 font-semibold text-primary">
						<ServerIcon class="size-4 shrink-0" aria-hidden="true" />
						<span class="truncate">{st.hostname || 'localhost'}</span>
					</span>
					{#if st.os_name}
						<Badge variant="outline" class="max-w-full truncate font-normal text-xs">
							{st.os_name}
						</Badge>
					{/if}
				</div>
			</Card.Header>

			<Card.Content class="flex flex-col gap-5 px-4 py-4 sm:px-5 sm:py-5">
				<!-- Upper 2-Column Resource Telemetry -->
				<div class="grid grid-cols-1 items-start gap-x-8 gap-y-4 md:grid-cols-2">
					<!-- CPU usage -->
					<div class="flex min-w-0 flex-col gap-1.5">
						<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
							<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
								<CpuIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								<span>{t('dashboard.metrics.cpuUsage')}</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{t('dashboard.metrics.cpuOfTotal', {
									pct: cpuPct.toFixed(2),
									cores: cpuCores,
									unit: cpuCores === 1 ? t('dashboard.metrics.cpuSingular') : t('dashboard.metrics.cpuPlural')
								})}
							</span>
						</div>
						<Progress
							value={Math.min(cpuPct, 100)}
							max={100}
							class={cn('h-1.5', getProgressToneClass(cpuPct))}
							aria-label={t('dashboard.metrics.cpuUsage')}
						/>
					</div>

					<!-- Load average -->
					<div class="flex min-w-0 flex-col gap-1.5">
						<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
							<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
								<ActivityIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								<span>{t('dashboard.metrics.loadAverage')}</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{loadPct}% ({st.load_avg || '-'})
							</span>
						</div>
						<Progress
							value={Math.min(loadPct, 100)}
							max={100}
							class={cn('h-1.5', getProgressToneClass(loadPct))}
							aria-label={t('dashboard.metrics.loadAverage')}
						/>
					</div>

					<!-- RAM usage -->
					<div class="flex min-w-0 flex-col gap-1.5">
						<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
							<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
								<MemoryStickIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								<span>{t('dashboard.metrics.ramUsage')}</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{memPctExact.toFixed(2)}% ({t('dashboard.metrics.ofTotal', {
									used: formatBinaryBytes(memUsedBytes),
									total: formatBinaryBytes(memTotalBytes)
								})})
							</span>
						</div>
						<Progress
							value={Math.min(memPctExact, 100)}
							max={100}
							class={cn('h-1.5', getProgressToneClass(memPctExact))}
							aria-label={t('dashboard.metrics.ramUsage')}
						/>
					</div>

					<!-- SWAP usage -->
					<div class="flex min-w-0 flex-col gap-1.5">
						<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
							<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
								<RefreshCwIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								<span>{t('dashboard.metrics.swapUsage')}</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{#if swapTotalBytes > 0}
									{swapPct.toFixed(2)}% ({t('dashboard.metrics.ofTotal', {
										used: formatBinaryBytes(swapUsedBytes),
										total: formatBinaryBytes(swapTotalBytes)
									})})
								{:else}
									{t('dashboard.metrics.swapInactive')}
								{/if}
							</span>
						</div>
						<Progress
							value={Math.min(swapPct, 100)}
							max={100}
							class={cn('h-1.5', getProgressToneClass(swapPct))}
							aria-label={t('dashboard.metrics.swapUsage')}
						/>
					</div>

					<!-- / HD space -->
					<div class="flex min-w-0 flex-col gap-1.5">
						<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
							<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
								<HardDriveIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								<span>{t('dashboard.metrics.rootDisk')}</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{diskPctExact.toFixed(2)}% ({t('dashboard.metrics.ofTotal', {
									used: formatBinaryBytes(diskUsedBytes),
									total: formatBinaryBytes(diskTotalBytes)
								})})
							</span>
						</div>
						<Progress
							value={Math.min(diskPctExact, 100)}
							max={100}
							class={cn('h-1.5', getProgressToneClass(diskPctExact))}
							aria-label={t('dashboard.metrics.rootDiskAria')}
						/>
					</div>

					<!-- External Disks & Other Mounts -->
					{#if extraMounts.length === 0}
						<div class="flex min-w-0 flex-col gap-1.5">
							<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
								<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
									<HardDriveIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
									<span>{t('dashboard.metrics.extraMounts')}</span>
								</span>
								<span class="font-mono text-xs text-muted-foreground">{t('dashboard.metrics.noneDetected')}</span>
							</div>
							<Progress value={0} max={100} class="h-1.5" aria-label={t('dashboard.metrics.extraMountsAria')} />
						</div>
					{:else}
						{#each extraMounts as drv (drv.mount_point)}
							{@const drvPct = drv.percent_exact ?? (drv.total_bytes > 0 ? (drv.used_bytes / drv.total_bytes) * 100 : drv.percent)}
							<div class="flex min-w-0 flex-col gap-1.5">
								<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:flex-nowrap sm:text-[13px]">
									<span class="inline-flex min-w-0 items-center gap-1.5 font-medium text-foreground" title={drv.device ? `${drv.mount_point} (${drv.device})` : drv.mount_point}>
										<HardDriveIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
										<span class="truncate font-mono text-xs">{drv.mount_point}</span>
										{#if drv.fs_type}
											<Badge variant="secondary" class="h-4 shrink-0 px-1.5 font-mono text-[10px] uppercase">
												{drv.fs_type}
											</Badge>
										{/if}
									</span>
									<span class="shrink-0 font-mono text-xs tabular-nums text-foreground">
										{drvPct.toFixed(2)}% ({t('dashboard.metrics.ofTotal', {
											used: formatBinaryBytes(drv.used_bytes),
											total: formatBinaryBytes(drv.total_bytes)
										})})
									</span>
								</div>
								<Progress
									value={Math.min(drvPct, 100)}
									max={100}
									class={cn('h-1.5', getProgressToneClass(drvPct))}
									aria-label={t('dashboard.metrics.mountUsageAria', { mount: drv.mount_point })}
								/>
							</div>
						{/each}
					{/if}
				</div>

				<Separator />

				<!-- Lower Hardware & Kernel Metadata Table -->
				<dl class="flex flex-col divide-y divide-border/50 text-xs sm:text-[13px]">
					<div class="flex flex-col gap-0.5 py-2 first:pt-0 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">{t('dashboard.hardware.cpus')}</dt>
						<dd class="min-w-0 font-mono text-xs break-words text-foreground sm:text-right">
							{cpuHardwareLabel}
						</dd>
					</div>

					<div class="flex flex-col gap-0.5 py-2 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">{t('dashboard.hardware.kernelVersion')}</dt>
						<dd class="min-w-0 font-mono text-xs break-words text-foreground sm:text-right">
							{st.kernel_full || (st.kernel ? `Linux ${st.kernel}` : '-')}
						</dd>
					</div>

					<div class="flex flex-col gap-0.5 py-2 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">{t('dashboard.hardware.bootMode')}</dt>
						<dd class="min-w-0 font-mono text-xs text-foreground sm:text-right">
							{st.boot_mode || '-'}
						</dd>
					</div>

					<div class="flex flex-col gap-1.5 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">{t('dashboard.hardware.managerVersion')}</dt>
						<dd class="flex flex-wrap items-center gap-2 sm:justify-end">
							<span class="font-mono text-xs text-foreground">
								wdev/{st.wdev_version || updaterStatus?.current || '-'}
							</span>
							{#if updaterStatus?.has_update}
								<Badge variant="outline" class="border-warning/50 bg-warning/15 font-mono text-[10px] text-foreground">
									{t('dashboard.hardware.versionAvailable', { version: updaterStatus.latest ?? '' })}
								</Badge>
							{/if}
							<Button
								variant="outline"
								size="xs"
								disabled={checkingSelfUpdate}
								onclick={checkSelfUpdate}
							>
								{#if checkingSelfUpdate}
									<Spinner data-icon="inline-start" />
									{t('dashboard.hardware.checkingBtn')}
								{:else}
									{t('dashboard.hardware.checkReleaseBtn')}
								{/if}
							</Button>
							{#if updaterStatus?.has_update}
								<Button
									size="xs"
									disabled={applyingUpdate}
									onclick={applySelfUpdate}
								>
									{#if applyingUpdate}
										<Spinner data-icon="inline-start" />
										{t('dashboard.updatingBtn')}
									{:else}
										<ArrowUpCircleIcon data-icon="inline-start" />
										{t('common.actions.update')}
									{/if}
								</Button>
							{/if}
						</dd>
					</div>

					<div class="flex flex-col gap-0.5 py-2 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">{t('dashboard.hardware.uptime')}</dt>
						<dd class="min-w-0 font-mono text-xs tabular-nums text-foreground sm:text-right">
							{uptimeLabel}
						</dd>
					</div>

					<div class="flex flex-col gap-1.5 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">{t('dashboard.hardware.repoStatus')}</dt>
						<dd class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs sm:justify-end">
							<span class="inline-flex items-center gap-1.5 text-foreground">
								<CircleCheckIcon class="size-3.5 shrink-0 text-success" aria-hidden="true" />
								<span>{t('dashboard.hardware.servicesActiveCount', { active: activeServicesCount, total: installedServices.length })}</span>
							</span>
							{#if totalPendingUpdates > 0}
								<button
									type="button"
									class="inline-flex min-h-6 items-center gap-1.5 rounded px-1.5 py-0.5 text-foreground transition-colors hover:bg-muted hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
									onclick={refreshServices}
								>
									<CircleAlertIcon class="size-3.5 shrink-0 text-warning" aria-hidden="true" />
									<span>{t('dashboard.hardware.updatesAvailableCount', { count: totalPendingUpdates })}</span>
									<ChevronRightIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								</button>
							{:else}
								<span class="inline-flex items-center gap-1.5 text-foreground">
									<CircleCheckIcon class="size-3.5 shrink-0 text-success" aria-hidden="true" />
									<span>{t('dashboard.hardware.allUpToDate')}</span>
								</span>
							{/if}
						</dd>
					</div>

					<div class="flex flex-col gap-1 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">{t('dashboard.hardware.vpsIp')}</dt>
						<dd class="flex min-w-0 items-center justify-between gap-2 sm:justify-end">
							<span class={cn('truncate font-mono text-xs font-medium', vpsIP ? 'text-primary' : 'text-muted-foreground')}>
								{vpsIP || '-'}
							</span>
							{#if vpsIP}
								<Button
									variant="ghost"
									size="icon-xs"
									class="fm-touch-btn shrink-0"
									aria-label={t('dashboard.hardware.copyVpsIpAria')}
									onclick={() => copyAddress(vpsIP, t('dashboard.hardware.vpsIp'))}
								>
									<CopyIcon />
								</Button>
							{/if}
						</dd>
					</div>

					<div class="flex flex-col gap-1 py-2 last:pb-0 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">{t('dashboard.hardware.lanIp')}</dt>
						<dd class="flex min-w-0 items-center justify-between gap-2 sm:justify-end">
							<span class={cn('truncate font-mono text-xs font-medium', lanIP ? 'text-success' : 'text-muted-foreground')}>
								{lanIP || '-'}
							</span>
							{#if lanIP}
								<Button
									variant="ghost"
									size="icon-xs"
									class="fm-touch-btn shrink-0"
									aria-label={t('dashboard.hardware.copyLanIpAria')}
									onclick={() => copyAddress(lanIP, 'LAN IP')}
								>
									<CopyIcon />
								</Button>
							{/if}
						</dd>
					</div>

					{#if netbirdInstalled}
						<div class="flex flex-col gap-1 py-2 last:pb-0 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
							<dt class="shrink-0 font-medium text-muted-foreground">{t('dashboard.hardware.netbirdIp')}</dt>
							<dd class="flex min-w-0 items-center justify-between gap-2 sm:justify-end">
								<span class="truncate font-mono text-xs font-medium text-foreground">
									{netbirdIP || '-'}
								</span>
								{#if netbirdIP}
									<Button
										variant="ghost"
										size="icon-xs"
										class="fm-touch-btn shrink-0"
										aria-label={t('dashboard.hardware.copyNetbirdIpAria')}
										onclick={() => copyAddress(netbirdIP, 'NetBird IP')}
									>
										<CopyIcon />
									</Button>
								{/if}
							</dd>
						</div>
					{/if}
				</dl>
			</Card.Content>
		</Card.Root>

		<!-- Active Runtimes -->
		<section class="flex flex-col gap-3">
			<div class="flex flex-wrap items-center justify-between gap-2">
				<h2 class="text-base font-semibold tracking-tight sm:text-lg">{t('dashboard.runtimes.title')}</h2>
				<Button variant="ghost" size="sm" href="/switchers">
					{t('dashboard.runtimes.manageBtn')}
					<ChevronRightIcon data-icon="inline-end" />
				</Button>
			</div>
			<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
				{#each Object.entries(st.runtimes || {}) as [name, ver] (name)}
					<a
						href="/switchers"
						class="flex items-center justify-between gap-2 rounded-lg border bg-card px-3 py-2 text-xs transition-colors hover:bg-accent sm:inline-flex"
					>
						<div class="flex min-w-0 items-center gap-2">
							<span class="font-semibold uppercase text-muted-foreground">{name}</span>
							<span class="truncate font-mono font-medium">{ver}</span>
						</div>
						<ChevronRightIcon class="size-3.5 shrink-0 text-muted-foreground" />
					</a>
				{/each}
			</div>
		</section>

		<!-- Core Services Grid -->
		<section class="flex flex-col gap-4">
			<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<div>
					<h2 class="text-base font-semibold tracking-tight sm:text-lg">{t('dashboard.services.title')}</h2>
					<p class="text-xs text-muted-foreground">
						{t('dashboard.services.subtitle')}
					</p>
				</div>
				<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
					<Button
						variant="outline"
						size="sm"
						disabled={refreshingServices}
						onclick={refreshServices}
					>
						{#if refreshingServices}
							<Spinner data-icon="inline-start" />
							{t('dashboard.services.checkingUpdates')}
						{:else}
							<RefreshCwIcon data-icon="inline-start" />
							{t('dashboard.checkUpdatesBtn')}
						{/if}
					</Button>
					<Button size="sm" onclick={() => cp.triggerInstall('update')}>
						<DownloadIcon data-icon="inline-start" />
						{t('dashboard.services.updateServicesBtn')}
					</Button>
				</div>
			</div>

			<div class="grid gap-3 sm:gap-4 md:grid-cols-2 xl:grid-cols-3">
				{#each st.services as svc (svc.name)}
					{@const busy = Boolean(servicePending[svc.name])}
					<Card.Root class="flex flex-col justify-between">
						<Card.Header>
							<div class="flex flex-wrap items-start justify-between gap-2">
								<div class="min-w-0">
									<Card.Title class="truncate text-base">{svc.display_name}</Card.Title>
									<Card.Description>
										{localizeServiceCategory(svc.category)}{svc.port ? ` · ${svc.port}` : ''}
									</Card.Description>
								</div>
								{#if !svc.installed}
									<StatusBadge tone="neutral" label={t('common.status.notInstalled')} />
								{:else if svc.has_update}
									<StatusBadge tone="warning" label={t('common.status.updateAvailable')} />
								{:else if svc.is_remote}
									<StatusBadge
										tone={svc.active ? 'success' : 'destructive'}
										label={svc.active ? t('dashboard.services.onlineEdge') : t('common.status.offline')}
									/>
								{:else}
									<StatusBadge
										tone={svc.active ? 'success' : svc.starting ? 'info' : 'neutral'}
										label={svc.active
											? t('common.status.active')
											: svc.starting
												? t('common.status.starting')
												: t('common.status.inactive')}
									/>
								{/if}
							</div>
						</Card.Header>

						<Card.Content>
							<div class="flex flex-col gap-2 rounded-md border bg-muted/40 p-2.5 text-xs">
								<div class="flex items-center justify-between gap-2">
									<span class="text-muted-foreground">{t('dashboard.services.installedVersion')}</span>
									<span class="truncate font-mono font-semibold">
										{svc.installed ? svc.version || t('common.status.installed') : t('common.status.notInstalled')}
									</span>
								</div>
								<Separator />
								<div class="flex items-center justify-between gap-2">
									<span class="text-muted-foreground">{t('dashboard.services.officialRelease')}</span>
									<span class="truncate font-mono">
										{svc.latest_version || svc.version || '-'}
									</span>
								</div>
								{#if svc.installed && svc.config_preset}
									<Separator />
									<div class="flex items-center justify-between gap-2">
										<span class="text-muted-foreground">{t('dashboard.services.configPreset')}</span>
										<Badge
											variant={svc.config_preset === 'Custom' ? 'outline' : 'secondary'}
											class="max-w-full truncate"
										>
											{svc.config_preset}
										</Badge>
									</div>
								{/if}
							</div>
						</Card.Content>

						<Card.Footer class="flex flex-wrap gap-2 [&>*]:flex-1 sm:[&>*]:flex-initial">
							{#if !svc.installed}
								{#if svc.name === 'stalwart'}
									<Button size="sm" href="/mail" class="w-full">
										<DownloadIcon data-icon="inline-start" />
										{t('dashboard.services.setupStalwart')}
									</Button>
								{:else if svc.name === 'pdns'}
									<Button size="sm" href="/powerdns" class="w-full">
										<DownloadIcon data-icon="inline-start" />
										{t('dashboard.services.setupPowerdns')}
									</Button>
								{:else if svc.name === 'docker'}
									<Button size="sm" href="/compose">
										<DownloadIcon data-icon="inline-start" />
										{t('dashboard.services.installManageDocker')}
									</Button>
									<Button
										variant="outline"
										size="sm"
										onclick={() => cp.openServiceConfig('docker')}
									>
										<SettingsIcon data-icon="inline-start" />
										{t('dashboard.services.preConfigureDocker')}
									</Button>
								{:else}
									<Button
										size="sm"
										class="w-full"
										onclick={() => cp.triggerInstall(svc.install_component || svc.name)}
									>
										<DownloadIcon data-icon="inline-start" />
										{t('dashboard.services.installService', { name: svc.display_name.split(' (')[0] })}
									</Button>
								{/if}
							{:else if svc.is_remote}
								<Button size="sm" href="/powerdns">{t('dashboard.services.manageDns')}</Button>
								<Button
									variant="outline"
									size="sm"
									disabled={busy}
									onclick={() => handleServiceAction(svc, 'restart')}
								>
									{#if busy}
										<Spinner data-icon="inline-start" />
									{:else}
										<RotateCwIcon data-icon="inline-start" />
									{/if}
									{t('dashboard.services.testConnection')}
								</Button>
							{:else}
								{#if svc.has_update}
									<Button
										size="sm"
										onclick={() => cp.triggerInstall(svc.install_component || svc.name)}
									>
										<ArrowUpCircleIcon data-icon="inline-start" />
										{t('dashboard.services.updateTo', { version: svc.latest_version ?? '' })}
									</Button>
								{/if}
								{#if svc.active}
									<Button
										variant="outline"
										size="sm"
										disabled={busy}
										onclick={() => handleServiceAction(svc, 'restart')}
									>
										{#if busy}
											<Spinner data-icon="inline-start" />
										{:else}
											<RotateCwIcon data-icon="inline-start" />
										{/if}
										{t('common.actions.restart')}
									</Button>
									<Button
										variant="destructive"
										size="sm"
										disabled={busy}
										onclick={() => handleServiceAction(svc, 'stop')}
									>
										{#if busy}
											<Spinner data-icon="inline-start" />
										{:else}
											<SquareIcon data-icon="inline-start" />
										{/if}
										{t('common.actions.stop')}
									</Button>
								{:else if svc.starting}
									<Button size="sm" variant="outline" disabled>
										<Spinner data-icon="inline-start" />
										{t('common.status.starting')}
									</Button>
								{:else}
									<Button
										size="sm"
										disabled={busy}
										onclick={() => handleServiceAction(svc, 'start')}
									>
										{#if busy}
											<Spinner data-icon="inline-start" />
										{:else}
											<PlayIcon data-icon="inline-start" />
										{/if}
										{t('common.actions.start')}
									</Button>
								{/if}

								{#if configurableServices.has(svc.name)}
									<Button
										variant="outline"
										size="sm"
										onclick={() => cp.openServiceConfig(svc.name as ServiceConfigKey)}
									>
										<SettingsIcon data-icon="inline-start" />
										{t('common.actions.configure')}
									</Button>
								{/if}
								{#if svc.name === 'docker'}
									<Button variant="outline" size="sm" href="/compose">
										<ChevronRightIcon data-icon="inline-start" />
										{t('dashboard.services.manageCompose')}
									</Button>
								{/if}
							{/if}
						</Card.Footer>
					</Card.Root>
				{/each}
			</div>
		</section>
	{/if}
</div>
