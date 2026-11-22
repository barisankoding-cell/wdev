<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Collapsible from '#lib/components/ui/collapsible/index.js';
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
		CPUCoreUsage,
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
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import ChevronUpIcon from '@lucide/svelte/icons/chevron-up';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import ArrowUpCircleIcon from '@lucide/svelte/icons/arrow-up-circle';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import ClockIcon from '@lucide/svelte/icons/clock';
	import MemoryStickIcon from '@lucide/svelte/icons/memory-stick';
	import ServerIcon from '@lucide/svelte/icons/server';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import LayersIcon from '@lucide/svelte/icons/layers';

	let refreshingServices = $state(false);
	let servicePending = $state<Record<string, boolean>>({});
	let updaterStatus = $state<UpdaterStatus | null>(null);
	let checkingSelfUpdate = $state(false);
	let applyingUpdate = $state(false);
	let coresOpen = $state(true);

	const configurableServices = new Set<string>(['redis', 'postgresql', 'php-fpm', 'nginx']);

	function getCoreGridColsClass(count: number): string {
		if (count <= 2) return 'grid-cols-2';
		if (count === 3) return 'grid-cols-2 @sm:grid-cols-3';
		if (count === 4 || count === 8 || count === 16) {
			return 'grid-cols-2 @lg:grid-cols-4';
		}
		if (count === 10) return 'grid-cols-2 @md:grid-cols-5';
		return 'grid-cols-2 @sm:grid-cols-3 @xl:grid-cols-4 @4xl:grid-cols-6';
	}

	function getCoreSummary(cores: CPUCoreUsage[]): { avg: number; peak: CPUCoreUsage } {
		let sum = 0;
		let peak = cores[0];
		for (const item of cores) {
			sum += item.percent;
			if (item.percent > peak.percent) peak = item;
		}
		return {
			avg: Math.round(sum / cores.length),
			peak
		};
	}

	function formatMountSize(bytes: number | undefined, gb: number): string {
		if (gb >= 1) return `${gb} GB`;
		const b = bytes ?? 0;
		if (b >= 1024 * 1024) return `${Math.round(b / (1024 * 1024))} MB`;
		return '0 GB';
	}

	function formatBinaryBytes(bytes: number): string {
		if (!bytes || bytes <= 0) return '0 B';
		const gib = 1024 * 1024 * 1024;
		const mib = 1024 * 1024;
		const kib = 1024;
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
		{@const ioDelayPct = st.io_delay_percent ?? 0}
		{@const memTotalBytes = st.mem_total_bytes ?? st.mem_total_mb * 1024 * 1024}
		{@const memUsedBytes = st.mem_used_bytes ?? st.mem_used_mb * 1024 * 1024}
		{@const memPctExact = st.mem_percent_exact ?? st.mem_percent}
		{@const diskTotalBytes = st.disk_total_bytes ?? st.disk_total_gb * 1024 * 1024 * 1024}
		{@const diskUsedBytes = st.disk_used_bytes ?? st.disk_used_gb * 1024 * 1024 * 1024}
		{@const diskPctExact = st.disk_percent_exact ?? st.disk_percent}
		{@const swapTotalBytes = st.swap_total_bytes ?? 0}
		{@const swapUsedBytes = st.swap_used_bytes ?? 0}
		{@const swapPct = st.swap_percent ?? 0}
		{@const ksmBytes = st.ksm_sharing_bytes ?? 0}
		{@const uptimeLabel = formatUptimeClock(st.uptime_seconds, st.uptime_string)}
		{@const cpuHardwareLabel = st.cpu_model
			? `${cpuCores} x ${st.cpu_model} (${st.cpu_sockets || 1} Socket)`
			: `${cpuCores} vCPU`}
		{@const installedServices = (st.services || []).filter((s) => s.installed)}
		{@const activeServicesCount = installedServices.filter((s) => s.active).length}
		{@const updatesCount = installedServices.filter((s) => s.has_update).length}
		{@const hasPanelUpdate = Boolean(updaterStatus?.has_update)}
		{@const totalPendingUpdates = updatesCount + (hasPanelUpdate ? 1 : 0)}
		<Card.Root class="gap-0 py-0">
			<Card.Header class="border-b px-4 py-3 [.border-b]:pb-3 sm:px-5">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<div class="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5">
						<span class="inline-flex items-center gap-1.5 font-semibold text-primary">
							<ServerIcon class="size-4 shrink-0" aria-hidden="true" />
							<span class="truncate">{st.hostname || 'localhost'}</span>
						</span>
						<span class="font-mono text-xs text-muted-foreground tabular-nums">
							(Uptime: {uptimeLabel})
						</span>
					</div>
					{#if st.os_name}
						<Badge variant="outline" class="max-w-full truncate font-normal text-xs">
							{st.os_name}
						</Badge>
					{/if}
				</div>
			</Card.Header>

			<Card.Content class="flex flex-col gap-5 px-4 py-4 sm:px-5 sm:py-5">
				<!-- Upper 2-Column Resource Telemetry -->
				<div class="flex flex-col gap-4">
					<!-- Row 1: CPU usage & Load average | IO delay -->
					<div class="grid grid-cols-1 items-start gap-x-8 gap-y-3.5 md:grid-cols-2">
						<div class="flex flex-col gap-3">
							<div class="flex flex-col gap-1.5">
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

							<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
								<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
									<ActivityIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
									<span>Load average</span>
								</span>
								<span class="font-mono text-xs tabular-nums text-foreground">
									{st.load_avg || '-'}
								</span>
							</div>
						</div>

						<div class="flex flex-col gap-1.5">
							<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
								<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
									<ClockIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
									<span>IO delay</span>
								</span>
								<span class="font-mono text-xs tabular-nums text-foreground">
									{ioDelayPct.toFixed(2)}%
								</span>
							</div>
							<Progress
								value={Math.min(ioDelayPct, 100)}
								max={100}
								class={cn('h-1.5', getProgressToneClass(ioDelayPct))}
								aria-label="IO delay"
							/>
						</div>
					</div>

					<!-- Row 2: RAM usage | KSM sharing -->
					<div class="grid grid-cols-1 items-start gap-x-8 gap-y-3.5 md:grid-cols-2">
						<div class="flex flex-col gap-1.5">
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

						<div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs sm:text-[13px]">
							<span class="inline-flex items-center gap-1.5 font-medium text-foreground">
								<LayersIcon class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
								<span>KSM sharing</span>
							</span>
							<span class="font-mono text-xs tabular-nums text-foreground">
								{formatBinaryBytes(ksmBytes)}
							</span>
						</div>
					</div>

					<!-- Row 3: / HD space | SWAP usage -->
					<div class="grid grid-cols-1 items-start gap-x-8 gap-y-3.5 md:grid-cols-2">
						<div class="flex flex-col gap-1.5">
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

						<div class="flex flex-col gap-1.5">
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
					</div>
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

					<div class="flex flex-col gap-0.5 py-2 sm:flex-row sm:items-baseline sm:justify-between sm:gap-4">
						<dt class="shrink-0 font-medium text-muted-foreground">Manager Version</dt>
						<dd class="min-w-0 font-mono text-xs text-foreground sm:text-right">
							wdev/{st.wdev_version || updaterStatus?.current || '-'}
						</dd>
					</div>

					<div class="flex flex-col gap-1.5 py-2 last:pb-0 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
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
				</dl>
			</Card.Content>
		</Card.Root>

		<!-- Drive Tambahan (/mnt) -->
		{#if st.mount_drives && st.mount_drives.length > 0}
			<section aria-labelledby="mount-drives-heading" class="flex flex-col gap-3">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<div class="flex flex-wrap items-center gap-2">
						<HardDriveIcon class="size-4 text-primary" aria-hidden="true" />
						<h2 id="mount-drives-heading" class="text-base font-semibold tracking-tight sm:text-lg">
							Drive Tambahan (/mnt)
						</h2>
						<Badge variant="outline" class="font-mono text-[10px]">
							{st.mount_drives.length} Terdeteksi
						</Badge>
					</div>
					<Button variant="ghost" size="sm" href="/files">
						File Manager
						<ChevronRightIcon data-icon="inline-end" />
					</Button>
				</div>

				<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 sm:gap-4">
					{#each st.mount_drives as drv (drv.mount_point)}
						{@const usedLabel = formatMountSize(drv.used_bytes, drv.used_gb)}
						{@const totalLabel = formatMountSize(drv.total_bytes, drv.total_gb)}
						{@const freeBytes = drv.total_bytes > drv.used_bytes ? drv.total_bytes - drv.used_bytes : 0}
						{@const freeLabel = formatMountSize(freeBytes, drv.free_gb)}
						<Card.Root>
							<Card.Header>
								<div class="flex flex-wrap items-center justify-between gap-2">
									<Card.Description class="font-medium text-foreground">{drv.name}</Card.Description>
									<div class="flex flex-wrap items-center gap-1">
										{#if drv.fs_type}
											<Badge variant="secondary" class="font-mono text-[10px] uppercase">
												{drv.fs_type}
											</Badge>
										{/if}
										<Badge variant="outline" class="font-mono text-[10px]">
											{drv.mount_point}
										</Badge>
									</div>
								</div>
								<Card.Title
									class={cn(
										'font-mono text-xl font-semibold tracking-tight tabular-nums',
										drv.percent >= 90 ? 'text-destructive' : drv.percent >= 80 && 'text-warning'
									)}
								>
									{drv.percent}%
								</Card.Title>
							</Card.Header>
							<Card.Content class="mt-auto flex flex-col gap-2.5">
								<Progress
									value={Math.min(drv.percent, 100)}
									max={100}
									aria-label={`Penggunaan drive ${drv.mount_point}`}
								/>
								<div class="flex flex-col gap-1 rounded-md border bg-muted/40 px-2.5 py-2 text-xs">
									<div class="flex items-center justify-between gap-2">
										<span class="text-muted-foreground">Terpakai</span>
										<span class="font-mono text-[11px] font-medium tabular-nums">
											{usedLabel} / {totalLabel}
										</span>
									</div>
									<div class="flex items-center justify-between gap-2">
										<span class="text-muted-foreground">Tersedia</span>
										<span class="font-mono text-[11px] text-muted-foreground tabular-nums">
											{freeLabel}
										</span>
									</div>
									{#if drv.device}
										<div class="flex items-center justify-between gap-2">
											<span class="shrink-0 text-muted-foreground">Perangkat</span>
											<span class="truncate font-mono text-[11px] text-muted-foreground">
												{drv.device}
											</span>
										</div>
									{/if}
								</div>
							</Card.Content>
							<Card.Footer>
								<Button
									variant="outline"
									size="xs"
									class="fm-touch-btn w-full"
									href={`/files?path=${encodeURIComponent(drv.mount_point)}`}
								>
									<FolderOpenIcon data-icon="inline-start" />
									Buka di File Manager
								</Button>
							</Card.Footer>
						</Card.Root>
					{/each}
				</div>
			</section>
		{/if}

		<!-- Per-Core CPU Utilization Strip -->
		{#if st.cpu_cores_usage && st.cpu_cores_usage.length > 1}
			{@const coreSummary = getCoreSummary(st.cpu_cores_usage)}
			<Card.Root class="@container gap-0">
				<Collapsible.Root bind:open={coresOpen}>
					<Card.Header>
						<div class="flex flex-wrap items-center gap-2">
							<Card.Title class="text-sm font-semibold">Penggunaan CPU per Core</Card.Title>
							<Badge variant="outline" class="font-mono text-[10px]">
								{st.cpu_cores_usage.length} Cores
							</Badge>
						</div>
						<Card.Description class="font-mono text-xs tabular-nums">
							Rata-rata {coreSummary.avg}% · Puncak Core {coreSummary.peak.core} ({coreSummary.peak.percent}%)
						</Card.Description>
						<Card.Action>
							<Collapsible.Trigger>
								{#snippet child({ props })}
									<Button variant="ghost" size="xs" {...props}>
										{coresOpen ? 'Sembunyikan' : 'Tampilkan'}
										{#if coresOpen}
											<ChevronUpIcon data-icon="inline-end" />
										{:else}
											<ChevronDownIcon data-icon="inline-end" />
										{/if}
									</Button>
								{/snippet}
							</Collapsible.Trigger>
						</Card.Action>
					</Card.Header>
					<Collapsible.Content class="pt-3">
						<Card.Content>
							<div
								class={cn(
									'grid gap-2',
									getCoreGridColsClass(st.cpu_cores_usage.length),
									st.cpu_cores_usage.length > 24 &&
										'max-h-56 overflow-y-auto overscroll-contain pr-1 [scrollbar-gutter:stable]'
								)}
							>
								{#each st.cpu_cores_usage as coreItem (coreItem.core)}
									<div class="flex flex-col gap-1.5 rounded-lg border bg-muted/30 px-2.5 py-2">
										<div class="flex items-center justify-between gap-2 font-mono text-xs">
											<span class="text-[11px] text-muted-foreground">Core {coreItem.core}</span>
											<span
												class={cn(
													'text-[11px] font-semibold tabular-nums',
													coreItem.percent >= 90
														? 'text-destructive'
														: coreItem.percent >= 75
															? 'text-warning'
															: 'text-foreground'
												)}
											>
												{coreItem.percent}%
											</span>
										</div>
										<Progress
											value={Math.min(coreItem.percent, 100)}
											max={100}
											class="h-1"
											aria-label="Penggunaan Core {coreItem.core}"
										/>
									</div>
								{/each}
							</div>
						</Card.Content>
					</Collapsible.Content>
				</Collapsible.Root>
			</Card.Root>
		{/if}

		<!-- Network & Self-Update Row -->
		{@const isVPS = modeStore.mode === 'vps' || st.server_mode === 'vps' || !!(st.public_ip && st.public_ip !== '-' && st.public_ip !== '127.0.0.1')}
		{@const vpsIP = (st.public_ip && st.public_ip !== '-' && st.public_ip !== '127.0.0.1')
			? st.public_ip
			: (modeStore.publicIP && modeStore.publicIP !== '-' ? modeStore.publicIP : '')}
		{@const networkIP = isVPS ? (vpsIP || (st.netbird_ip !== '-' ? st.netbird_ip : '')) : st.netbird_ip}
		{@const networkLabel = isVPS ? 'IP VPS' : 'IP Jaringan NetBird (VPN Mesh)'}
		{@const copyToastLabel = isVPS ? 'IP VPS' : 'IP NetBird'}
		{@const hasExtraNetbird = Boolean(isVPS && st.netbird_ip && st.netbird_ip !== '-' && st.netbird_ip !== vpsIP)}
		<div
			class={cn(
				'grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4',
				hasExtraNetbird ? 'xl:grid-cols-4' : 'lg:grid-cols-3'
			)}
		>
			<Card.Root class="justify-center">
				<Card.Header>
					<div class="flex items-start justify-between gap-2">
						<div class="min-w-0">
							<Card.Description>{networkLabel}</Card.Description>
							<Card.Title class="mt-0.5 truncate font-mono text-base text-primary">
								{networkIP || '-'}
							</Card.Title>
						</div>
						{#if networkIP && networkIP !== '-'}
							<Button
								variant="ghost"
								size="icon-sm"
								aria-label="Salin {copyToastLabel}"
								onclick={() => copyAddress(networkIP, copyToastLabel)}
							>
								<CopyIcon />
							</Button>
						{/if}
					</div>
				</Card.Header>
			</Card.Root>

			<Card.Root class="justify-center">
				<Card.Header>
					<div class="flex items-start justify-between gap-2">
						<div class="min-w-0">
							<Card.Description>IP Jaringan Lokal (LAN)</Card.Description>
							<Card.Title class="mt-0.5 truncate font-mono text-base text-success">
								{st.lan_ip || '-'}
							</Card.Title>
						</div>
						{#if st.lan_ip}
							<Button
								variant="ghost"
								size="icon-sm"
								aria-label="Salin IP LAN"
								onclick={() => copyAddress(st.lan_ip, 'IP LAN')}
							>
								<CopyIcon />
							</Button>
						{/if}
					</div>
				</Card.Header>
			</Card.Root>

			{#if hasExtraNetbird}
				<Card.Root class="justify-center">
					<Card.Header>
						<div class="flex items-start justify-between gap-2">
							<div class="min-w-0">
								<Card.Description>IP NetBird VPN (Mesh)</Card.Description>
								<Card.Title class="mt-0.5 truncate font-mono text-base text-muted-foreground">
									{st.netbird_ip}
								</Card.Title>
							</div>
							<Button
								variant="ghost"
								size="icon-sm"
								aria-label="Salin IP NetBird"
								onclick={() => copyAddress(st.netbird_ip, 'IP NetBird')}
							>
								<CopyIcon />
							</Button>
						</div>
					</Card.Header>
				</Card.Root>
			{/if}

			<Card.Root class={cn('justify-center', !hasExtraNetbird && 'sm:col-span-2 lg:col-span-1')}>
				<Card.Header>
					<div class="flex flex-wrap items-center justify-between gap-2">
						<div class="min-w-0">
							<Card.Description>Biner wdev Control Plane</Card.Description>
							<Card.Title class="mt-0.5 flex flex-wrap items-center gap-2 font-mono text-base">
								<span>v{st.wdev_version || updaterStatus?.current || '-'}</span>
								{#if updaterStatus?.has_update}
									<span class="inline-flex items-center gap-1.5 font-sans text-xs font-medium text-amber-600 dark:text-amber-400">
										<span class="relative flex size-2">
											<span class="absolute inline-flex size-full animate-ping rounded-full bg-amber-400 opacity-75"></span>
											<span class="relative inline-flex size-2 rounded-full bg-amber-500"></span>
										</span>
										v{updaterStatus.latest} tersedia
									</span>
								{/if}
							</Card.Title>
						</div>
						<div class="flex flex-wrap items-center gap-1.5">
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
									class="bg-amber-600 text-white hover:bg-amber-700"
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
						</div>
					</div>
				</Card.Header>
			</Card.Root>
		</div>

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
