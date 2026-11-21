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

	<!-- System Metrics -->
	{#if !cp.systemStatus}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 xl:grid-cols-4">
			{#each [1, 2, 3, 4] as idx (idx)}
				<Card.Root>
					<Card.Header>
						<Skeleton class="h-4 w-24" />
						<Skeleton class="h-7 w-28" />
					</Card.Header>
					<Card.Content class="mt-auto flex flex-col gap-2.5">
						<Skeleton class="h-1 w-full" />
						<Skeleton class="h-12 w-full" />
					</Card.Content>
				</Card.Root>
			{/each}
		</div>
	{:else}
		{@const st = cp.systemStatus}
		{@const cpuCores = st.cpu_cores && st.cpu_cores > 0 ? st.cpu_cores : 1}
		{@const loadPct = st.load_percent ?? st.load_1m_percent ?? 0}
		{@const load1Pct = st.load_1m_percent ?? loadPct}
		{@const load5Pct = st.load_5m_percent ?? 0}
		{@const load15Pct = st.load_15m_percent ?? 0}
		{@const load1Raw = (st.load_1m ?? 0).toFixed(2)}
		{@const load5Raw = (st.load_5m ?? 0).toFixed(2)}
		{@const load15Raw = (st.load_15m ?? 0).toFixed(2)}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 xl:grid-cols-4">
			<Card.Root>
				<Card.Header>
					<div class="flex items-center justify-between gap-2">
						<Card.Description>Sistem Operasi</Card.Description>
						{#if st.hostname}
							<Badge variant="outline" class="max-w-32 truncate font-mono text-[10px]">
								{st.hostname}
							</Badge>
						{/if}
					</div>
					<Card.Title class="text-base leading-snug break-words">{st.os_name}</Card.Title>
				</Card.Header>
				<Card.Content class="mt-auto">
					<div class="flex flex-col gap-1 rounded-md border bg-muted/40 px-2.5 py-2 text-xs">
						<div class="flex items-center justify-between gap-2">
							<span class="shrink-0 text-muted-foreground">Uptime</span>
							<span class="font-mono text-[11px] font-medium tabular-nums">
								{st.uptime_string}
							</span>
						</div>
						{#if st.kernel}
							<div class="flex items-baseline justify-between gap-2">
								<span class="shrink-0 text-muted-foreground">Kernel</span>
								<span class="min-w-0 break-all text-right font-mono text-[11px] text-muted-foreground">
									{st.kernel}
								</span>
							</div>
						{/if}
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header>
					<div class="flex items-center justify-between gap-2">
						<Card.Description>Load Average</Card.Description>
						<Badge variant="outline" class="font-mono text-[10px]">{cpuCores} vCPU</Badge>
					</div>
					<Card.Title
						class={cn(
							'font-mono text-xl font-semibold tracking-tight tabular-nums',
							loadPct > 100 ? 'text-destructive' : loadPct >= 75 && 'text-warning'
						)}
					>
						{loadPct}%
					</Card.Title>
				</Card.Header>
				<Card.Content class="mt-auto flex flex-col gap-2.5">
					<Progress value={Math.min(loadPct, 100)} max={100} aria-label="Persentase Load Average CPU" />
					<div
						class="grid grid-cols-3 divide-x divide-border/60 rounded-md border bg-muted/40 px-2.5 py-1.5 font-mono tabular-nums"
					>
						<div class="flex flex-col pr-2 leading-tight">
							<span class="text-[10px] text-muted-foreground">1m</span>
							<span class="mt-0.5 text-xs font-semibold">{load1Pct}%</span>
							<span class="text-[10px] text-muted-foreground">{load1Raw}</span>
						</div>
						<div class="flex flex-col px-2 leading-tight">
							<span class="text-[10px] text-muted-foreground">5m</span>
							<span class="mt-0.5 text-xs font-semibold">{load5Pct}%</span>
							<span class="text-[10px] text-muted-foreground">{load5Raw}</span>
						</div>
						<div class="flex flex-col pl-2 leading-tight">
							<span class="text-[10px] text-muted-foreground">15m</span>
							<span class="mt-0.5 text-xs font-semibold">{load15Pct}%</span>
							<span class="text-[10px] text-muted-foreground">{load15Raw}</span>
						</div>
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header>
					<div class="flex items-center justify-between gap-2">
						<Card.Description>Memori (RAM)</Card.Description>
						{#if st.mem_total_mb > 0}
							<Badge variant="outline" class="font-mono text-[10px]">
								{(st.mem_total_mb / 1024).toFixed(1)} GB
							</Badge>
						{/if}
					</div>
					<Card.Title
						class={cn(
							'font-mono text-xl font-semibold tracking-tight tabular-nums',
							st.mem_percent >= 90 ? 'text-destructive' : st.mem_percent >= 75 && 'text-warning'
						)}
					>
						{st.mem_percent}%
					</Card.Title>
				</Card.Header>
				<Card.Content class="mt-auto flex flex-col gap-2.5">
					<Progress value={st.mem_percent} max={100} aria-label="Penggunaan RAM" />
					<div class="flex flex-col gap-1 rounded-md border bg-muted/40 px-2.5 py-2 text-xs">
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Terpakai</span>
							<span class="font-mono text-[11px] font-medium tabular-nums">
								{st.mem_used_mb} / {st.mem_total_mb} MB
							</span>
						</div>
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Tersedia</span>
							<span class="font-mono text-[11px] text-muted-foreground tabular-nums">
								{Math.max(st.mem_total_mb - st.mem_used_mb, 0)} MB
							</span>
						</div>
					</div>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header>
					<div class="flex items-center justify-between gap-2">
						<Card.Description>Disk Root</Card.Description>
						<Badge variant="outline" class="font-mono text-[10px]">/</Badge>
					</div>
					<Card.Title
						class={cn(
							'font-mono text-xl font-semibold tracking-tight tabular-nums',
							st.disk_percent >= 90 ? 'text-destructive' : st.disk_percent >= 80 && 'text-warning'
						)}
					>
						{st.disk_percent}%
					</Card.Title>
				</Card.Header>
				<Card.Content class="mt-auto flex flex-col gap-2.5">
					<Progress value={st.disk_percent} max={100} aria-label="Penggunaan Disk" />
					<div class="flex flex-col gap-1 rounded-md border bg-muted/40 px-2.5 py-2 text-xs">
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Terpakai</span>
							<span class="font-mono text-[11px] font-medium tabular-nums">
								{st.disk_used_gb} / {st.disk_total_gb} GB
							</span>
						</div>
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Tersedia</span>
							<span class="font-mono text-[11px] text-muted-foreground tabular-nums">
								{Math.max(st.disk_total_gb - st.disk_used_gb, 0)} GB
							</span>
						</div>
					</div>
				</Card.Content>
			</Card.Root>
		</div>

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
