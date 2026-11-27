<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import * as NativeSelect from '#lib/components/ui/native-select/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import { api, ApiError } from '#lib/api/client.js';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import { modeStore, MODE_LABELS } from '#lib/stores/mode.svelte.js';
	import type { PortColorTone, PortEntry, PortScopeDetail, PortStatusReport } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import EthernetPortIcon from '@lucide/svelte/icons/ethernet-port';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import LaptopIcon from '@lucide/svelte/icons/laptop';
	import InfoIcon from '@lucide/svelte/icons/info';
	import CheckIcon from '@lucide/svelte/icons/check';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import CopyIcon from '@lucide/svelte/icons/copy';

	type ColorFilter = 'all' | PortColorTone;

	let report = $state<PortStatusReport | null>(null);
	let loading = $state(true);
	let refreshing = $state(false);
	let loadError = $state('');
	let categoryFilter = $state<string>('all');
	let colorFilter = $state<ColorFilter>('all');
	let showNotInstalled = $state(true);
	let expandedPortIds = $state<Record<string, boolean>>({});

	function togglePortDetail(id: string) {
		expandedPortIds[id] = !expandedPortIds[id];
	}

	const categories = $derived([
		{ id: 'all', label: t('ports.categories.all') },
		{ id: 'system', label: t('ports.categories.system') },
		{ id: 'web', label: t('ports.categories.web') },
		{ id: 'database', label: t('ports.categories.database') },
		{ id: 'dns', label: t('ports.categories.dns') },
		{ id: 'mail', label: t('ports.categories.mail') }
	]);

	function badgeToneFromColor(color: PortColorTone): 'success' | 'warning' | 'destructive' | 'neutral' {
		switch (color) {
			case 'green':
				return 'success';
			case 'yellow':
				return 'warning';
			case 'red':
				return 'destructive';
			default:
				return 'neutral';
		}
	}

	function pillClassFromColor(color: PortColorTone): string {
		switch (color) {
			case 'green':
				return 'border-success/35 bg-success/15 text-success';
			case 'yellow':
				return 'border-amber-500/35 bg-amber-500/15 text-amber-700 dark:text-amber-400';
			case 'red':
				return 'border-destructive/35 bg-destructive/15 text-destructive';
			default:
				return 'border-border bg-secondary text-secondary-foreground';
		}
	}

	function dotClassFromColor(color: PortColorTone): string {
		switch (color) {
			case 'green':
				return 'bg-success';
			case 'yellow':
				return 'bg-amber-500';
			case 'red':
				return 'bg-destructive';
			default:
				return 'bg-muted-foreground';
		}
	}

	function scopeTooltip(port: PortEntry, scopeName: string, detail: PortScopeDetail): string {
		const targetPart = detail.target ? ` (${detail.target})` : '';
		const msgPart = detail.detail ? ` — ${detail.detail}` : '';
		return `${port.name} :${port.port} [${scopeName}]${targetPart}: ${detail.label}${msgPart}`;
	}

	function buildPortScopes(item: PortEntry) {
		return [
			{
				key: 'local',
				shortLabel: t('ports.table.scopeLocal'),
				headerLabel: t('ports.table.colLocal', { ip: report?.network.local_ip || '127.0.0.1' }),
				detail: item.local
			},
			{
				key: 'vpn',
				shortLabel: t('ports.table.scopeVpn'),
				headerLabel: t('ports.table.colVpn', { target: report?.network.vpn_ip || report?.network.vps_vpn_ip || 'NetBird' }),
				detail: item.vpn
			},
			{
				key: 'vps',
				shortLabel: t('ports.table.scopeVps'),
				headerLabel: t('ports.table.colVps', {
					target:
						report?.network.mode === 'local'
							? t('ports.cards.localMode')
							: report?.network.vps_public_ip || t('ports.table.publicFallback')
				}),
				detail: item.vps
			}
		];
	}

	const allPorts = $derived(report?.ports ?? []);

	const pillPorts = $derived.by(() =>
		showNotInstalled ? allPorts : allPorts.filter((p) => p.installed)
	);

	const filteredPorts = $derived.by(() => {
		return allPorts.filter((p) => {
			if (!showNotInstalled && !p.installed) return false;
			if (categoryFilter !== 'all' && p.category !== categoryFilter) return false;
			if (colorFilter !== 'all' && p.overall_color !== colorFilter) return false;
			return true;
		});
	});

	const blockedOrWarningPorts = $derived.by(() =>
		allPorts.filter((p) => p.installed && p.overall_color === 'yellow')
	);

	const closedPorts = $derived.by(() =>
		allPorts.filter((p) => p.installed && p.overall_color === 'red')
	);

	async function copyText(val: string, label: string) {
		if (!val || val === '-') return;
		try {
			await navigator.clipboard.writeText(val);
			toast.success(t('ports.toast.copied', { label, val }));
		} catch {
			toast.error(t('ports.toast.copyFailed', { label }));
		}
	}

	async function loadPorts(forceRefresh = false) {
		if (forceRefresh) {
			refreshing = true;
		} else if (!report) {
			loading = true;
		}
		loadError = '';
		try {
			report = await api.getPortStatus(forceRefresh);
			if (forceRefresh) {
				toast.success(t('ports.toast.refreshed'));
			}
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : t('ports.toast.loadFailed');
			loadError = msg;
			if (forceRefresh) {
				toast.error(t('ports.toast.checkFailedTitle'), { description: msg });
			}
		} finally {
			loading = false;
			refreshing = false;
		}
	}

	onMount(() => {
		void loadPorts(false);
	});
</script>

<div class="min-w-0 space-y-4 sm:space-y-6">
	<!-- Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div class="min-w-0 space-y-1">
			<div class="flex flex-wrap items-center gap-2">
				<EthernetPortIcon class="size-5 shrink-0 text-primary" />
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('ports.title')}</h1>
				{#if report?.network?.mode}
					<Badge variant="outline" class="font-mono text-xs">
						{t('ports.modePrefix', { mode: MODE_LABELS[report.network.mode] ?? report.network.mode })}
					</Badge>
				{/if}
			</div>
			<p class="text-xs leading-relaxed text-muted-foreground sm:text-sm">
				{t('ports.subtitlePrefix')} <span class="font-mono font-medium">wdev</span> {t('ports.subtitleMiddle')} <strong>{t('ports.scopeLocalBold')}</strong>, <strong>{t('ports.scopeVpnBold')}</strong>, {t('ports.andWord')} <strong>{t('ports.scopeVpsBold')}</strong>.
			</p>
		</div>

		<div class="flex flex-wrap items-center gap-2 sm:shrink-0">
			<Button
				variant="outline"
				size="sm"
				class="h-9 w-full sm:h-8 sm:w-auto"
				disabled={refreshing || loading}
				onclick={() => loadPorts(true)}
			>
				{#if refreshing}
					<Spinner class="size-3.5" />
				{:else}
					<RefreshCwIcon data-icon="inline-start" />
				{/if}
				<span>{refreshing ? t('ports.refreshingBtn') : t('ports.refreshBtn')}</span>
			</Button>
		</div>
	</div>

	{#if loadError}
		<Alert.Root variant="destructive">
			<AlertCircleIcon />
			<Alert.Title>{t('ports.loadErrorTitle')}</Alert.Title>
			<Alert.Description>{loadError}</Alert.Description>
		</Alert.Root>
	{/if}

	<!-- Ringkasan Alamat Jaringan & Firewall (4 Kartu) -->
	<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 lg:grid-cols-4">
		<Card.Root>
			<Card.Content class="space-y-2 p-4">
				<div class="flex items-center justify-between">
					<span class="text-xs font-medium text-muted-foreground">{t('ports.cards.localTitle')}</span>
					<LaptopIcon class="size-4 text-primary" />
				</div>
				<div class="flex items-center justify-between gap-2">
					<span class="font-mono text-base font-bold tabular-nums">
						{report?.network.local_ip || '127.0.0.1'}
					</span>
					<StatusBadge tone="success" label={t('ports.cards.loopbackBadge')} />
				</div>
				<div class="flex items-center justify-between text-xs text-muted-foreground">
					<span>{t('ports.cards.lanLabel')} <span class="font-mono">{report?.network.lan_ip || '-'}</span></span>
					{#if report?.network.lan_ip}
						<button
							type="button"
							class="inline-flex items-center gap-1 hover:text-foreground"
							onclick={() => copyText(report?.network.lan_ip || '', t('ports.cards.lanIpLabel'))}
							title={t('ports.cards.copyLanTitle')}
						>
							<CopyIcon class="size-3" />
						</button>
					{/if}
				</div>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Content class="space-y-2 p-4">
				<div class="flex items-center justify-between">
					<span class="text-xs font-medium text-muted-foreground">{t('ports.cards.vpnTitle')}</span>
					<NetworkIcon class="size-4 text-primary" />
				</div>
				<div class="flex items-center justify-between gap-2">
					<span class="truncate font-mono text-base font-bold tabular-nums">
						{report?.network.vpn_ip || '-'}
					</span>
					{#if report?.network.netbird_active && report?.network.vpn_ip}
						<StatusBadge tone="success" label={t('ports.cards.activeBadge')} />
					{:else if report?.network.netbird_active}
						<StatusBadge tone="warning" label={t('ports.cards.ipNaBadge')} />
					{:else}
						<StatusBadge tone="neutral" label={t('ports.cards.inactiveBadge')} />
					{/if}
				</div>
				<div class="flex items-center justify-between text-xs text-muted-foreground">
					{#if report?.network.vps_vpn_ip}
						<span>{t('ports.cards.edgeVpsVpnLabel')} <span class="font-mono">{report.network.vps_vpn_ip}</span></span>
					{:else if report?.network.vpn_ip}
						<span>{t('ports.cards.cgnatLabel')} <span class="font-mono">100.64.0.0/10</span></span>
						<button
							type="button"
							class="inline-flex items-center gap-1 hover:text-foreground"
							onclick={() => copyText(report?.network.vpn_ip || '', t('ports.cards.netbirdIpLabel'))}
							title={t('ports.cards.copyNetbirdTitle')}
						>
							<CopyIcon class="size-3" />
						</button>
					{:else}
						<span>{t('ports.cards.netbirdInactive')}</span>
					{/if}
				</div>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Content class="space-y-2 p-4">
				<div class="flex items-center justify-between">
					<span class="text-xs font-medium text-muted-foreground">{t('ports.cards.vpsTitle')}</span>
					<GlobeIcon class="size-4 text-primary" />
				</div>
				<div class="flex items-center justify-between gap-2">
					<span class="truncate font-mono text-base font-bold tabular-nums">
						{report?.network.mode === 'local'
							? t('ports.cards.localMode')
							: report?.network.vps_public_ip || t('ports.cards.notDetected')}
					</span>
					{#if report?.network.mode === 'local'}
						<StatusBadge tone="neutral" label={t('ports.cards.nonPublicBadge')} />
					{:else if report?.network.vps_public_ip}
						<StatusBadge tone="info" label={report?.network.mode === 'edge' ? t('ports.cards.edgeIpBadge') : t('ports.cards.publicIpBadge')} />
					{:else}
						<StatusBadge tone="warning" label={t('ports.cards.naBadge')} />
					{/if}
				</div>
				<div class="flex items-center justify-between text-xs text-muted-foreground">
					{#if report?.network.mode === 'local'}
						<span>{t('ports.cards.localNoExpose')}</span>
					{:else if report?.network.mode === 'edge'}
						<span>{t('ports.cards.edgeTestHint')}</span>
					{:else}
						<span>{t('ports.cards.vpsTestHint')}</span>
					{/if}
					{#if report?.network.vps_public_ip}
						<button
							type="button"
							class="inline-flex items-center gap-1 hover:text-foreground"
							onclick={() => copyText(report?.network.vps_public_ip || '', t('ports.cards.vpsIpLabel'))}
							title={t('ports.cards.copyVpsTitle')}
						>
							<CopyIcon class="size-3" />
						</button>
					{/if}
				</div>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Content class="space-y-2 p-4">
				<div class="flex items-center justify-between">
					<span class="text-xs font-medium text-muted-foreground">{t('ports.cards.firewallTitle')}</span>
					<ShieldCheckIcon class="size-4 text-primary" />
				</div>
				<div class="flex flex-wrap items-center gap-1.5">
					<span class="inline-flex items-center gap-1 rounded-md border border-success/35 bg-success/15 px-2 py-0.5 font-mono text-xs font-semibold text-success tabular-nums">
						<span class="size-1.5 rounded-full bg-success"></span>
						{t('ports.cards.openCount', { count: report?.summary.green ?? 0 })}
					</span>
					<span class="inline-flex items-center gap-1 rounded-md border border-amber-500/35 bg-amber-500/15 px-2 py-0.5 font-mono text-xs font-semibold text-amber-700 dark:text-amber-400 tabular-nums">
						<span class="size-1.5 rounded-full bg-amber-500"></span>
						{t('ports.cards.blockedCount', { count: report?.summary.yellow ?? 0 })}
					</span>
					<span class="inline-flex items-center gap-1 rounded-md border border-destructive/35 bg-destructive/15 px-2 py-0.5 font-mono text-xs font-semibold text-destructive tabular-nums">
						<span class="size-1.5 rounded-full bg-destructive"></span>
						{t('ports.cards.closedCount', { count: report?.summary.red ?? 0 })}
					</span>
				</div>
				<p class="truncate text-xs text-muted-foreground" title={report?.network.firewall.summary}>
					{report?.network.firewall.summary || t('ports.cards.checkingFirewall')}
				</p>
			</Card.Content>
		</Card.Root>
	</div>

	<!-- Ringkasan Cepat Pill Port (Pola Mail Server: Hijau / Kuning / Merah / Abu) -->
	<Card.Root>
		<Card.Header class="pb-3">
			<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
				<div>
					<Card.Title class="text-base">{t('ports.quickMap.title')}</Card.Title>
					<Card.Description>
						{t('ports.quickMap.description')}
					</Card.Description>
				</div>
				<div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
					<span class="inline-flex items-center gap-1.5">
						<span class="size-2 rounded-full bg-success"></span> {t('ports.quickMap.legendOpen')}
					</span>
					<span class="inline-flex items-center gap-1.5">
						<span class="size-2 rounded-full bg-amber-500"></span> {t('ports.quickMap.legendBlocked')}
					</span>
					<span class="inline-flex items-center gap-1.5">
						<span class="size-2 rounded-full bg-destructive"></span> {t('ports.quickMap.legendClosed')}
					</span>
					<span class="inline-flex items-center gap-1.5">
						<span class="size-2 rounded-full bg-muted-foreground"></span> {t('ports.quickMap.legendInternal')}
					</span>
				</div>
			</div>
		</Card.Header>
		<Card.Content class="space-y-4">
			{#if loading && !report}
				<div class="flex items-center gap-2 py-4 text-sm text-muted-foreground">
					<Spinner class="size-4" />
					<span>{t('ports.quickMap.checkingAll')}</span>
				</div>
			{:else}
				<div class="flex flex-wrap gap-1.5" aria-label={t('ports.quickMap.ariaSummary')}>
					{#each pillPorts as item (item.id)}
						<button
							type="button"
							onclick={() => {
								categoryFilter = item.category;
								colorFilter = 'all';
							}}
							title={t('ports.quickMap.pillTitle', {
								name: item.name,
								port: item.port,
								protocol: item.protocol,
								overall: item.overall_label,
								local: item.local.label,
								vpn: item.vpn.label,
								vps: item.vps.label
							})}
							class={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1 font-mono text-xs font-medium tabular-nums transition-opacity hover:opacity-85 ${pillClassFromColor(item.overall_color)}`}
						>
							<span class={`size-1.5 shrink-0 rounded-full ${dotClassFromColor(item.overall_color)}`}></span>
							<span>{item.port}</span>
							<span class="font-sans text-[11px] opacity-80">{item.service}</span>
						</button>
					{/each}
				</div>

				<!-- Mobile Filter Controls (< md) -->
				<div class="grid grid-cols-2 gap-2 border-t pt-3 md:hidden">
					<NativeSelect.Root
						aria-label={t('ports.categories.all')}
						bind:value={categoryFilter}
						class="w-full [&>select]:h-9 [&>select]:bg-background [&>select]:text-xs"
					>
						{#each categories as cat (cat.id)}
							<NativeSelect.Option value={cat.id}>{cat.label}</NativeSelect.Option>
						{/each}
					</NativeSelect.Root>

					<NativeSelect.Root
						aria-label={t('ports.quickMap.filterAllStatus')}
						bind:value={colorFilter}
						class="w-full [&>select]:h-9 [&>select]:bg-background [&>select]:text-xs"
					>
						<NativeSelect.Option value="all">{t('ports.quickMap.filterAllStatus')}</NativeSelect.Option>
						<NativeSelect.Option value="green">{t('ports.quickMap.filterGreen')}</NativeSelect.Option>
						<NativeSelect.Option value="yellow">{t('ports.quickMap.filterYellow')}</NativeSelect.Option>
						<NativeSelect.Option value="red">{t('ports.quickMap.filterRed')}</NativeSelect.Option>
					</NativeSelect.Root>

					<Button
						variant={showNotInstalled ? 'outline' : 'secondary'}
						size="sm"
						class="col-span-2 h-8 text-xs"
						onclick={() => (showNotInstalled = !showNotInstalled)}
					>
						{showNotInstalled ? t('ports.quickMap.hideNotInstalled') : t('ports.quickMap.showAllPorts')}
					</Button>
				</div>

				<!-- Desktop Filter Kategori & Warna (md+) -->
				<div class="hidden flex-col gap-2 border-t pt-3 md:flex lg:flex-row lg:items-center lg:justify-between">
					<div class="flex flex-wrap gap-1.5">
						{#each categories as cat (cat.id)}
							<Button
								variant={categoryFilter === cat.id ? 'default' : 'outline'}
								size="sm"
								class="h-7 px-2.5 text-xs"
								onclick={() => (categoryFilter = cat.id)}
							>
								{cat.label}
							</Button>
						{/each}
					</div>

					<div class="flex flex-wrap items-center gap-1.5">
						<Button
							variant={colorFilter === 'all' ? 'secondary' : 'ghost'}
							size="sm"
							class="h-7 px-2 text-xs"
							onclick={() => (colorFilter = 'all')}
						>
							{t('ports.quickMap.filterAllStatus')}
						</Button>
						<Button
							variant={colorFilter === 'green' ? 'secondary' : 'ghost'}
							size="sm"
							class="h-7 px-2 text-xs text-success"
							onclick={() => (colorFilter = 'green')}
						>
							{t('ports.quickMap.filterGreen')}
						</Button>
						<Button
							variant={colorFilter === 'yellow' ? 'secondary' : 'ghost'}
							size="sm"
							class="h-7 px-2 text-xs text-amber-600 dark:text-amber-400"
							onclick={() => (colorFilter = 'yellow')}
						>
							{t('ports.quickMap.filterYellow')}
						</Button>
						<Button
							variant={colorFilter === 'red' ? 'secondary' : 'ghost'}
							size="sm"
							class="h-7 px-2 text-xs text-destructive"
							onclick={() => (colorFilter = 'red')}
						>
							{t('ports.quickMap.filterRed')}
						</Button>
						<Button
							variant={showNotInstalled ? 'outline' : 'ghost'}
							size="sm"
							class="h-7 px-2.5 text-xs"
							onclick={() => (showNotInstalled = !showNotInstalled)}
						>
							{showNotInstalled ? t('ports.quickMap.hideNotInstalled') : t('ports.quickMap.showAllPorts')}
						</Button>
					</div>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>

	<!-- Tabel Detail Port: Lokal · VPN · IP VPS (Mobile Accordion Cards + Desktop Expandable Table) -->
	<div class="flex flex-col gap-3 sm:gap-0 sm:overflow-hidden sm:rounded-lg sm:border sm:bg-card">
		<div class="flex flex-col gap-1.5 rounded-xl border bg-card px-3.5 py-3 sm:flex-row sm:items-center sm:justify-between sm:rounded-none sm:border-0 sm:border-b sm:px-4">
			<div class="min-w-0">
				<h2 class="text-sm font-semibold">{t('ports.table.title')}</h2>
				<p class="text-xs text-muted-foreground">
					{t('ports.table.showingCount', { filtered: filteredPorts.length, total: allPorts.length })} · {t('ports.table.clickToExpand')}
				</p>
			</div>
			{#if report?.checked_at}
				<span class="shrink-0 font-mono text-[11px] text-muted-foreground">
					{t('ports.table.lastChecked', { time: i18n.formatTime(report.checked_at) })}
				</span>
			{/if}
		</div>

		{#snippet portExpandedDetail(
			item: PortEntry,
			scopes: { key: string; shortLabel: string; headerLabel: string; detail: PortScopeDetail }[]
		)}
			<div class="space-y-3">
				<div class="grid grid-cols-1 gap-2.5 md:grid-cols-3">
					{#each scopes as sc (sc.key)}
						<div class="min-w-0 space-y-1.5 rounded-md border border-border/70 bg-background/80 p-3">
							<div class="flex flex-wrap items-center justify-between gap-2">
								<span class="text-xs font-semibold text-muted-foreground">
									{sc.headerLabel}
								</span>
								<StatusBadge
									tone={badgeToneFromColor(sc.detail.color)}
									label={sc.detail.label}
								/>
							</div>
							{#if sc.detail.target && item.installed}
								<div class="font-mono text-xs break-all text-foreground">
									{sc.detail.target}
								</div>
							{/if}
							{#if sc.detail.detail}
								<p class="text-xs leading-relaxed break-words text-muted-foreground">
									{sc.detail.detail}
								</p>
							{/if}
						</div>
					{/each}
				</div>

				<div class="min-w-0 space-y-2 rounded-md border border-border/60 bg-background/60 p-3">
					{#if (item.bind_addresses && item.bind_addresses.length > 0) || item.config_hint}
						<div class="flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[11px] break-all text-muted-foreground">
							{#if item.bind_addresses && item.bind_addresses.length > 0}
								<span>{t('ports.table.bindPrefix')} {item.bind_addresses.join(', ')}</span>
							{/if}
							{#if item.config_hint}
								<span>{item.config_hint}</span>
							{/if}
						</div>
					{/if}
					<p class="text-xs leading-relaxed break-words text-muted-foreground">{item.description}</p>
					{#if item.recommendation}
						<p class={`rounded border px-2.5 py-1.5 text-xs leading-relaxed break-words ${
							item.overall_color === 'red'
								? 'border-destructive/30 bg-destructive/5 text-destructive'
								: item.overall_color === 'yellow'
									? 'border-amber-500/30 bg-amber-500/5 text-amber-700 dark:text-amber-400'
									: 'border-border bg-muted/40 text-muted-foreground'
						}`}>
							{item.recommendation}
						</p>
					{/if}
				</div>
			</div>
		{/snippet}

		{#if loading && !report}
			<div class="flex items-center justify-center gap-2 rounded-xl border bg-card p-8 text-sm text-muted-foreground sm:rounded-none sm:border-0">
				<Spinner class="size-4" />
				<span>{t('ports.table.loadingList')}</span>
			</div>
		{:else if filteredPorts.length === 0}
			<div class="rounded-xl border bg-card sm:rounded-none sm:border-0">
				<Empty.Root class="border-0 py-10">
					<Empty.Header>
						<Empty.Media variant="icon">
							<EthernetPortIcon />
						</Empty.Media>
						<Empty.Title>{t('ports.table.emptyTitle')}</Empty.Title>
						<Empty.Description>
							{t('ports.table.emptyDesc')}
						</Empty.Description>
					</Empty.Header>
					<Empty.Content>
						<Button
							variant="outline"
							size="sm"
							onclick={() => {
								categoryFilter = 'all';
								colorFilter = 'all';
								showNotInstalled = true;
							}}
						>
							{t('ports.table.resetFilter')}
						</Button>
					</Empty.Content>
				</Empty.Root>
			</div>
		{:else}
			<!-- Mobile (< 640px): Accordion Cards -->
			<div class="flex flex-col gap-2.5 sm:hidden" aria-label={t('ports.table.ariaLabel')}>
				{#each filteredPorts as item (item.id)}
					{@const isExpanded = !!expandedPortIds[item.id]}
					{@const scopes = buildPortScopes(item)}
					<div class={`overflow-hidden rounded-xl border bg-card transition-colors ${!item.installed ? 'opacity-75' : ''} ${isExpanded ? 'border-primary/40 ring-1 ring-primary/15' : ''}`}>
						<button
							type="button"
							onclick={() => togglePortDetail(item.id)}
							aria-expanded={isExpanded}
							aria-label={t('ports.table.expandRowAria', { name: item.name, port: item.port })}
							class="flex w-full flex-col gap-2.5 p-3.5 text-left transition-colors active:bg-muted/40"
						>
							<div class="flex items-start justify-between gap-2">
								<div class="flex min-w-0 flex-1 flex-wrap items-center gap-2">
									<span class="inline-flex items-center gap-1.5 rounded-md border bg-muted/40 px-2 py-0.5 font-mono text-xs font-bold tabular-nums">
										<span class={`size-2 shrink-0 rounded-full ${dotClassFromColor(item.overall_color)}`}></span>
										<span>{item.port}</span>
										<span class="text-[10px] font-normal text-muted-foreground">/{item.protocol}</span>
									</span>
									<StatusBadge
										tone={badgeToneFromColor(item.overall_color)}
										label={item.overall_label}
									/>
								</div>
								<ChevronDownIcon
									class={`mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform duration-150 ${isExpanded ? 'rotate-180 text-primary' : ''}`}
								/>
							</div>

							<div class="flex flex-wrap items-center justify-between gap-1.5">
								<span class="text-sm font-semibold text-foreground">{item.name}</span>
								<Badge variant="outline" class="text-[10px] font-normal">
									{item.category_label}
								</Badge>
							</div>

							<div class="flex flex-wrap items-center gap-1.5">
								{#each scopes as sc (sc.key)}
									<span
										class={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] font-medium ${pillClassFromColor(sc.detail.color)}`}
									>
										<span class={`size-1.5 shrink-0 rounded-full ${dotClassFromColor(sc.detail.color)}`}></span>
										<span class="font-semibold">{sc.shortLabel}:</span>
										<span>{sc.detail.label}</span>
									</span>
								{/each}
							</div>
						</button>

						{#if isExpanded}
							<div class="border-t border-border/60 bg-muted/20 p-3.5">
								{@render portExpandedDetail(item, scopes)}
							</div>
						{/if}
					</div>
				{/each}
			</div>

			<!-- Desktop / Tablet (>= 640px): Expandable 3-Column Table -->
			<div class="hidden sm:block">
				<ResponsiveTableWrapper label={t('ports.table.ariaLabel')} stackOnMobile={false}>
					<Table.Root>
						<Table.Header>
							<Table.Row>
								<Table.Head class="w-[88px] whitespace-nowrap">{t('ports.table.colPort')}</Table.Head>
								<Table.Head>{t('ports.table.colService')}</Table.Head>
								<Table.Head>{t('ports.table.colScopes')}</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each filteredPorts as item (item.id)}
								{@const isExpanded = !!expandedPortIds[item.id]}
								{@const scopes = buildPortScopes(item)}
								<Table.Row
									onclick={() => togglePortDetail(item.id)}
									class={`cursor-pointer transition-colors hover:bg-muted/40 ${!item.installed ? 'opacity-70' : ''} ${isExpanded ? 'bg-muted/25' : ''}`}
								>
									<Table.Cell data-label={t('ports.table.dataPort')} class="align-top whitespace-nowrap">
										<div class="flex items-center gap-1.5 font-mono text-sm font-bold">
											<span class={`size-2 shrink-0 rounded-full ${dotClassFromColor(item.overall_color)}`}></span>
											<span>{item.port}</span>
										</div>
										<div class="mt-0.5 font-mono text-[11px] text-muted-foreground">
											{item.protocol}
										</div>
									</Table.Cell>

									<Table.Cell data-label={t('ports.table.dataService')} class="align-top">
										<button
											type="button"
											onclick={(e) => {
												e.stopPropagation();
												togglePortDetail(item.id);
											}}
											aria-expanded={isExpanded}
											aria-label={t('ports.table.expandRowAria', { name: item.name, port: item.port })}
											class="group inline-flex items-center gap-1.5 rounded-sm text-left font-semibold text-foreground hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
										>
											<span>{item.name}</span>
										</button>
										<div class="mt-1 flex flex-wrap items-center gap-1.5">
											<StatusBadge
												tone={badgeToneFromColor(item.overall_color)}
												label={item.overall_label}
											/>
											<Badge variant="outline" class="text-[10px] font-normal">
												{item.category_label}
											</Badge>
										</div>
									</Table.Cell>

									<Table.Cell data-label={t('ports.table.dataScopes')} class="align-middle">
										<div class="flex items-center justify-between gap-3">
											<div class="flex flex-wrap items-center gap-1.5">
												{#each scopes as sc (sc.key)}
													<span
														title={scopeTooltip(item, sc.shortLabel, sc.detail)}
														class={`inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-[11px] font-medium ${pillClassFromColor(sc.detail.color)}`}
													>
														<span class={`size-1.5 shrink-0 rounded-full ${dotClassFromColor(sc.detail.color)}`}></span>
														<span class="font-semibold">{sc.shortLabel}:</span>
														<span>{sc.detail.label}</span>
													</span>
												{/each}
											</div>
											<ChevronDownIcon
												class={`size-4 shrink-0 text-muted-foreground transition-transform duration-150 ${isExpanded ? 'rotate-180 text-primary' : ''}`}
											/>
										</div>
									</Table.Cell>
								</Table.Row>

								{#if isExpanded}
									<Table.Row class="bg-muted/15 hover:bg-muted/15">
										<Table.Cell colspan={3} class="p-0">
											<div class="border-t border-border/60 p-4">
												{@render portExpandedDetail(item, scopes)}
											</div>
										</Table.Cell>
									</Table.Row>
								{/if}
							{/each}
						</Table.Body>
					</Table.Root>
				</ResponsiveTableWrapper>
			</div>
		{/if}

		<!-- Ringkasan Aksi / Rekomendasi Bawah Tabel -->
		{#if report}
			<div class="space-y-2 rounded-xl border bg-card p-3.5 sm:rounded-none sm:border-0 sm:border-t sm:p-4">
				{#if closedPorts.length > 0}
					<Alert.Root variant="destructive">
						<AlertCircleIcon />
						<Alert.Title>
							{t('ports.alerts.closedTitle', { list: closedPorts.map((p) => `${p.port}/${p.service}`).join(', ') })}
						</Alert.Title>
						<Alert.Description class="text-xs leading-relaxed">
							{t('ports.alerts.closedDescPrefix')} <strong>{t('ports.alerts.closedDescLink')}</strong> {t('ports.alerts.closedDescSuffix')}
						</Alert.Description>
					</Alert.Root>
				{/if}

				{#if blockedOrWarningPorts.length > 0}
					<Alert.Root class="border-amber-500/30 bg-amber-500/5">
						<InfoIcon class="text-amber-600 dark:text-amber-400" />
						<Alert.Title class="font-semibold text-amber-700 dark:text-amber-400">
							{t('ports.alerts.blockedTitle', { list: blockedOrWarningPorts.map((p) => p.port).join(', ') })}
						</Alert.Title>
						<Alert.Description class="text-xs leading-relaxed text-muted-foreground">
							{t('ports.alerts.blockedDescPrefix')} <span class="font-mono">127.0.0.1</span>{t('ports.alerts.blockedDescMiddle')} <strong>{t('ports.table.scopeVpn')}</strong>{t('ports.alerts.blockedDescAnd')} <strong>{t('ports.table.scopeVps')}</strong>{t('ports.alerts.blockedDescSuffix')}
						</Alert.Description>
					</Alert.Root>
				{/if}

				{#if closedPorts.length === 0 && blockedOrWarningPorts.length === 0}
					<div class="flex items-center gap-2 text-xs text-muted-foreground">
						<CheckIcon class="size-4 text-success" />
						<span>{t('ports.alerts.allOptimal')}</span>
					</div>
				{/if}
			</div>
		{/if}
	</div>
</div>
