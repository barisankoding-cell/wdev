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

	const configurableServices = new Set<string>(['redis', 'postgresql', 'php-fpm', 'nginx']);

	/** Menggabungkan mount eksternal dan /mnt tanpa duplikasi mount_point. */
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

	function formatUptimeClock(seconds: number | undefined, fallback: string): string {
		if (seconds == null || seconds < 0) return fallback || 'N/A';
		const total = Math.floor(seconds);
		const days = Math.floor(total / 86400);
		const hours = Math.floor((total % 86400) / 3600);
		const mins = Math.floor((total % 3600) / 60);
		const secs = total % 60;
		const hms = [hours, mins, secs].map((n) => String(n).padStart(2, '0')).join(':');
		return days > 0 ? `${days} hari ${hms}` : hms;
	}

	function getProgressToneClass(pct: number): string {
		if (pct >= 90) return '[&>[data-slot=progress-indicator]]:bg-destructive';
		if (pct >= 75) return '[&>[data-slot=progress-indicator]]:bg-warning';
		return '';
	}

	async function copyAddress(val: string, label: string) {
		if (!val || val === '-') return;
		try {
			await navigator.clipboard.writeText(val);
			toast.success(`${label} (${val}) disalin.`);
		} catch {
			toast.error(`Gagal menyalin ${label}.`);
		}
	}

	onMount(() => {
		api.getUpdaterStatus(false).then((res) => (updaterStatus = res)).catch(() => {});
	});

	async function refreshServices() {
		refreshingServices = true;
		try {
			await cp.loadStatus(true);
			toast.success('Status pembaruan layanan diperbarui.');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memeriksa pembaruan');
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
				toast.info(`Pembaruan wdev tersedia: v${res.latest}`);
			} else {
				toast.success(`wdev v${res.current} sudah versi terbaru.`);
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memeriksa update wdev');
		} finally {
			checkingSelfUpdate = false;
		}
	}

	async function applySelfUpdate() {
		const changelogSnippet = updaterStatus?.changelog
			? `\n\nCatatan Rilis (v${updaterStatus.latest}):\n${updaterStatus.changelog.trim().slice(0, 300)}${updaterStatus.changelog.trim().length > 300 ? '...' : ''}`
			: '';
		const ok = await cp.confirm({
			title: `Perbarui Biner wdev ke v${updaterStatus?.latest || 'Terbaru'}`,
			description: `Unduh dan pasang rilis wdev terbaru lalu restart layanan wdev?${changelogSnippet}`,
			confirmText: 'Perbarui Sekarang'
		});
		if (!ok) return;
		applyingUpdate = true;
		try {
			const res = await cp.runWithSudoRetry(
				'Pembaruan biner wdev memerlukan hak akses administrator (sudo).',
				(sudoPass) => api.applySelfUpdate('', sudoPass)
			);
			if (res) {
				if (res.status === 'started') {
					cp.armSelfUpdateReload();
				}
				cp.openTerminal('Pembaruan Biner wdev');
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memperbarui wdev');
		} finally {
			applyingUpdate = false;
		}
	}

	async function handleServiceAction(svc: ServiceStatus, action: 'start' | 'stop' | 'restart') {
		servicePending = { ...servicePending, [svc.name]: true };
		try {
			await api.controlService(svc.name, action);
			toast.success(`Layanan ${svc.display_name} berhasil di-${action}.`);
			await cp.loadStatus(false);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : `Gagal ${action} ${svc.display_name}`);
		} finally {
			servicePending = { ...servicePending, [svc.name]: false };
		}
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Ringkasan Sistem</h1>
			<p class="text-sm text-muted-foreground">
				Pantau kesehatan server Linux, runtime aktif, dan kendalikan daemon latar belakang.
			</p>
		</div>
		<div
			class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center"
			role="toolbar"
			aria-label="Aksi Pemeliharaan Sistem"
		>
			<Button size="sm" onclick={() => cp.triggerInstall('update')}>
				<RefreshCwIcon data-icon="inline-start" />
				Perbarui Semua
			</Button>
			<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('check-updates')}>
				<SearchIcon data-icon="inline-start" />
				Cek Pembaruan
			</Button>
			<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('self-check')}>
				<ShieldCheckIcon data-icon="inline-start" />
				Self-Check
			</Button>
			<Button variant="outline" size="sm" onclick={() => cp.openTerminal()}>
				<TerminalIcon data-icon="inline-start" />
				Live Terminal
			</Button>
		</div>
	</div>

	<!-- Notifikasi Rilis Pembaruan wdev Panel (aaPanel Style) -->
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
						<span class="font-semibold text-foreground">Pembaruan wdev Control Plane Tersedia!</span>
						<Badge variant="default" class="bg-amber-600 font-mono text-xs text-white hover:bg-amber-700">
							v{updaterStatus.latest}
						</Badge>
					</div>
					<p class="mt-0.5 text-xs text-muted-foreground sm:text-sm">
						Versi baru wdev siap dipasang (saat ini: v{updaterStatus.current}). Dapatkan fitur dan perbaikan terbaru dengan satu klik.
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
						Memperbarui...
					{:else}
						<ArrowUpCircleIcon data-icon="inline-start" />
						Perbarui Sekarang
					{/if}
				</Button>
			</div>
		</div>
	{/if}

	{#if cp.statusError}
		<Alert.Root variant="destructive">
			<AlertCircleIcon />
			<Alert.Title>Gagal Memuat Status Sistem</Alert.Title>
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
		{@const uptimeLabel = formatUptimeClock(st.uptime_seconds, st.uptime_string)}
		{@const cpuHardwareLabel = st.cpu_model
			? `${cpuCores} x ${st.cpu_model} (${st.cpu_sockets || 1} Socket)`
			: `${cpuCores} vCPU`}
		{@const installedServices = (st.services || []).filter((s) => s.installed)}
		{@const activeServicesCount = installedServices.filter((s) => s.active).length}
		{@const updatesCount = installedServices.filter((s) => s.has_update).length}
		{@const hasPanelUpdate = Boolean(updaterStatus?.has_update)}
		{@const totalPendingUpdates = updatesCount + (hasPanelUpdate ? 1 : 0)}
		{@const vpsIP = (st.public_ip && st.public_ip !== '-' && st.public_ip !== '127.0.0.1')
			? st.public_ip
			: (modeStore.publicIP && modeStore.publicIP !== '-' ? modeStore.publicIP : '')}
		{@const lanIP = st.lan_ip || ''}
		{@const netbirdIP = st.netbird_ip && st.netbird_ip !== '-' ? st.netbird_ip : ''}
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
								<span>CPU usage</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{cpuPct.toFixed(2)}% of {cpuCores} {cpuCores === 1 ? 'CPU' : 'CPUs'}
							</span>
						</div>
						<Progress
							value={Math.min(cpuPct, 100)}
							max={100}
							class={cn('h-1.5', getProgressToneClass(cpuPct))}
							aria-label="CPU usage"
						/>
					</div>

					<!-- Load average -->
					<div class="flex min-w-0 flex-col gap-1.5">
						<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
							<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
								<ActivityIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								<span>Load average</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{loadPct}% ({st.load_avg || '-'})
							</span>
						</div>
						<Progress
							value={Math.min(loadPct, 100)}
							max={100}
							class={cn('h-1.5', getProgressToneClass(loadPct))}
							aria-label="Load average"
						/>
					</div>

					<!-- RAM usage -->
					<div class="flex min-w-0 flex-col gap-1.5">
						<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
							<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
								<MemoryStickIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								<span>RAM usage</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{memPctExact.toFixed(2)}% ({formatBinaryBytes(memUsedBytes)} of {formatBinaryBytes(memTotalBytes)})
							</span>
						</div>
						<Progress
							value={Math.min(memPctExact, 100)}
							max={100}
							class={cn('h-1.5', getProgressToneClass(memPctExact))}
							aria-label="RAM usage"
						/>
					</div>

					<!-- SWAP usage -->
					<div class="flex min-w-0 flex-col gap-1.5">
						<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
							<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
								<RefreshCwIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								<span>SWAP usage</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{#if swapTotalBytes > 0}
									{swapPct.toFixed(2)}% ({formatBinaryBytes(swapUsedBytes)} of {formatBinaryBytes(swapTotalBytes)})
								{:else}
									0.00% (Tidak aktif)
								{/if}
							</span>
						</div>
						<Progress
							value={Math.min(swapPct, 100)}
							max={100}
							class={cn('h-1.5', getProgressToneClass(swapPct))}
							aria-label="SWAP usage"
						/>
					</div>

					<!-- / HD space -->
					<div class="flex min-w-0 flex-col gap-1.5">
						<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
							<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
								<HardDriveIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								<span>/ HD space</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{diskPctExact.toFixed(2)}% ({formatBinaryBytes(diskUsedBytes)} of {formatBinaryBytes(diskTotalBytes)})
							</span>
						</div>
						<Progress
							value={Math.min(diskPctExact, 100)}
							max={100}
							class={cn('h-1.5', getProgressToneClass(diskPctExact))}
							aria-label="Penggunaan disk root"
						/>
					</div>

					<!-- Disk Eksternal & Mount Lain -->
					{#if extraMounts.length === 0}
						<div class="flex min-w-0 flex-col gap-1.5">
							<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
								<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
									<HardDriveIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
									<span>Disk Eksternal / Mount</span>
								</span>
								<span class="font-mono text-xs text-muted-foreground">Tidak ada terdeteksi</span>
							</div>
							<Progress value={0} max={100} class="h-1.5" aria-label="Disk eksternal atau mount lain" />
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
										{drvPct.toFixed(2)}% ({formatBinaryBytes(drv.used_bytes)} of {formatBinaryBytes(drv.total_bytes)})
									</span>
								</div>
								<Progress
									value={Math.min(drvPct, 100)}
									max={100}
									class={cn('h-1.5', getProgressToneClass(drvPct))}
									aria-label={`Penggunaan disk ${drv.mount_point}`}
								/>
							</div>
						{/each}
					{/if}
				</div>

				<Separator />

				<!-- Lower Hardware & Kernel Metadata Table -->
				<dl class="flex flex-col divide-y divide-border/50 text-xs sm:text-[13px]">
					<div class="flex flex-col gap-0.5 py-2 first:pt-0 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">CPU(s)</dt>
						<dd class="min-w-0 font-mono text-xs break-words text-foreground sm:text-right">
							{cpuHardwareLabel}
						</dd>
					</div>

					<div class="flex flex-col gap-0.5 py-2 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">Kernel Version</dt>
						<dd class="min-w-0 font-mono text-xs break-words text-foreground sm:text-right">
							{st.kernel_full || (st.kernel ? `Linux ${st.kernel}` : '-')}
						</dd>
					</div>

					<div class="flex flex-col gap-0.5 py-2 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">Boot Mode</dt>
						<dd class="min-w-0 font-mono text-xs text-foreground sm:text-right">
							{st.boot_mode || '-'}
						</dd>
					</div>

					<div class="flex flex-col gap-1.5 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">Manager Version</dt>
						<dd class="flex flex-wrap items-center gap-2 sm:justify-end">
							<span class="font-mono text-xs text-foreground">
								wdev/{st.wdev_version || updaterStatus?.current || '-'}
							</span>
							{#if updaterStatus?.has_update}
								<Badge variant="outline" class="border-warning/50 bg-warning/15 font-mono text-[10px] text-foreground">
									v{updaterStatus.latest} tersedia
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
									Cek...
								{:else}
									Cek Rilis
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
										Memperbarui...
									{:else}
										<ArrowUpCircleIcon data-icon="inline-start" />
										Update
									{/if}
								</Button>
							{/if}
						</dd>
					</div>

					<div class="flex flex-col gap-0.5 py-2 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">Uptime</dt>
						<dd class="min-w-0 font-mono text-xs tabular-nums text-foreground sm:text-right">
							{uptimeLabel}
						</dd>
					</div>

					<div class="flex flex-col gap-1.5 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">Repository Status</dt>
						<dd class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs sm:justify-end">
							<span class="inline-flex items-center gap-1.5 text-foreground">
								<CircleCheckIcon class="size-3.5 shrink-0 text-success" aria-hidden="true" />
								<span>{activeServicesCount}/{installedServices.length} layanan aktif</span>
							</span>
							{#if totalPendingUpdates > 0}
								<button
									type="button"
									class="inline-flex min-h-6 items-center gap-1.5 rounded px-1.5 py-0.5 text-foreground transition-colors hover:bg-muted hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
									onclick={refreshServices}
								>
									<CircleAlertIcon class="size-3.5 shrink-0 text-warning" aria-hidden="true" />
									<span>{totalPendingUpdates} pembaruan tersedia</span>
									<ChevronRightIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								</button>
							{:else}
								<span class="inline-flex items-center gap-1.5 text-foreground">
									<CircleCheckIcon class="size-3.5 shrink-0 text-success" aria-hidden="true" />
									<span>Semua paket mutakhir</span>
								</span>
							{/if}
						</dd>
					</div>

					<div class="flex flex-col gap-1 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">IP VPS</dt>
						<dd class="flex min-w-0 items-center justify-between gap-2 sm:justify-end">
							<span class={cn('truncate font-mono text-xs font-medium', vpsIP ? 'text-primary' : 'text-muted-foreground')}>
								{vpsIP || '-'}
							</span>
							{#if vpsIP}
								<Button
									variant="ghost"
									size="icon-xs"
									class="fm-touch-btn shrink-0"
									aria-label="Salin IP VPS"
									onclick={() => copyAddress(vpsIP, 'IP VPS')}
								>
									<CopyIcon />
								</Button>
							{/if}
						</dd>
					</div>

					<div class="flex flex-col gap-1 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">IP Jaringan Lokal (LAN)</dt>
						<dd class="flex min-w-0 items-center justify-between gap-2 sm:justify-end">
							<span class={cn('truncate font-mono text-xs font-medium', lanIP ? 'text-success' : 'text-muted-foreground')}>
								{lanIP || '-'}
							</span>
							{#if lanIP}
								<Button
									variant="ghost"
									size="icon-xs"
									class="fm-touch-btn shrink-0"
									aria-label="Salin IP LAN"
									onclick={() => copyAddress(lanIP, 'IP LAN')}
								>
									<CopyIcon />
								</Button>
							{/if}
						</dd>
					</div>

					<div class="flex flex-col gap-1 py-2 last:pb-0 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">IP NetBird VPN (Mesh)</dt>
						<dd class="flex min-w-0 items-center justify-between gap-2 sm:justify-end">
							<span class="truncate font-mono text-xs font-medium text-foreground">
								{netbirdIP || '-'}
							</span>
							{#if netbirdIP}
								<Button
									variant="ghost"
									size="icon-xs"
									class="fm-touch-btn shrink-0"
									aria-label="Salin IP NetBird"
									onclick={() => copyAddress(netbirdIP, 'IP NetBird')}
								>
									<CopyIcon />
								</Button>
							{/if}
						</dd>
					</div>
				</dl>
			</Card.Content>
		</Card.Root>

		<!-- Active Runtimes -->
		<section class="flex flex-col gap-3">
			<div class="flex flex-wrap items-center justify-between gap-2">
				<h2 class="text-base font-semibold tracking-tight sm:text-lg">Versi Runtime Aktif</h2>
				<Button variant="ghost" size="sm" href="/switchers">
					Kelola Switchers
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
					<h2 class="text-base font-semibold tracking-tight sm:text-lg">Status Layanan Inti</h2>
					<p class="text-xs text-muted-foreground">
						Status daemon background, versi terpasang, dan ketersediaan pembaruan paket.
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
							Memeriksa...
						{:else}
							<RefreshCwIcon data-icon="inline-start" />
							Cek Pembaruan
						{/if}
					</Button>
					<Button size="sm" onclick={() => cp.triggerInstall('update')}>
						<DownloadIcon data-icon="inline-start" />
						Perbarui Layanan
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
										{svc.category}{svc.port ? ` · ${svc.port}` : ''}
									</Card.Description>
								</div>
								{#if !svc.installed}
									<StatusBadge tone="neutral" label="Belum Terpasang" />
								{:else if svc.has_update}
									<StatusBadge tone="warning" label="Update Tersedia" />
								{:else if svc.is_remote}
									<StatusBadge
										tone={svc.active ? 'success' : 'destructive'}
										label={svc.active ? 'Online (Edge)' : 'Offline'}
									/>
								{:else}
									<StatusBadge
										tone={svc.active ? 'success' : 'neutral'}
										label={svc.active ? 'Aktif' : 'Nonaktif'}
									/>
								{/if}
							</div>
						</Card.Header>

						<Card.Content>
							<div class="flex flex-col gap-2 rounded-md border bg-muted/40 p-2.5 text-xs">
								<div class="flex items-center justify-between gap-2">
									<span class="text-muted-foreground">Versi Terpasang</span>
									<span class="truncate font-mono font-semibold">
										{svc.installed ? svc.version || 'Terpasang' : 'Belum terpasang'}
									</span>
								</div>
								<Separator />
								<div class="flex items-center justify-between gap-2">
									<span class="text-muted-foreground">Rilis Resmi</span>
									<span class="truncate font-mono">
										{svc.latest_version || svc.version || '-'}
									</span>
								</div>
								{#if svc.installed && svc.config_preset}
									<Separator />
									<div class="flex items-center justify-between gap-2">
										<span class="text-muted-foreground">Preset Konfigurasi</span>
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
										Setup Stalwart Mail
									</Button>
								{:else if svc.name === 'pdns'}
									<Button size="sm" href="/powerdns" class="w-full">
										<DownloadIcon data-icon="inline-start" />
										Setup PowerDNS / Edge
									</Button>
								{:else}
									<Button
										size="sm"
										class="w-full"
										onclick={() => cp.triggerInstall(svc.install_component || svc.name)}
									>
										<DownloadIcon data-icon="inline-start" />
										Pasang {svc.display_name.split(' (')[0]}
									</Button>
								{/if}
							{:else if svc.is_remote}
								<Button size="sm" href="/powerdns">Kelola DNS</Button>
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
									Tes Koneksi
								</Button>
							{:else}
								{#if svc.has_update}
									<Button
										size="sm"
										onclick={() => cp.triggerInstall(svc.install_component || svc.name)}
									>
										<ArrowUpCircleIcon data-icon="inline-start" />
										Perbarui ke {svc.latest_version}
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
										Restart
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
										Stop
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
										Jalankan
									</Button>
								{/if}

								{#if configurableServices.has(svc.name)}
									<Button
										variant="outline"
										size="sm"
										onclick={() => cp.openServiceConfig(svc.name as ServiceConfigKey)}
									>
										<SettingsIcon data-icon="inline-start" />
										Konfigurasi
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
