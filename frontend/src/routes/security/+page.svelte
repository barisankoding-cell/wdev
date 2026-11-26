<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import { api, ApiError } from '#lib/api/client.js';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type {
		BlockedIPRecord,
		SecurityEventRecord,
		SecurityStatsReport
	} from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';

	import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import ShieldXIcon from '@lucide/svelte/icons/shield-x';
	import ShieldIcon from '@lucide/svelte/icons/shield';
	import BanIcon from '@lucide/svelte/icons/ban';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import SearchIcon from '@lucide/svelte/icons/search';
	import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import RadioIcon from '@lucide/svelte/icons/radio';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import BarChart3Icon from '@lucide/svelte/icons/bar-chart-3';

	let activeTab = $state<'events' | 'blocked' | 'analytics'>('events');

	let stats = $state<SecurityStatsReport | null>(null);
	let events = $state<SecurityEventRecord[]>([]);
	let blockedIPs = $state<BlockedIPRecord[]>([]);
	let totalEvents = $state(0);
	let loading = $state(true);
	let refreshing = $state(false);

	// Filters & Pagination
	let filterType = $state('all');
	let filterSeverity = $state('all');
	let searchQuery = $state('');
	let pageOffset = $state(0);
	const pageSize = 25;

	// Manual Block Dialog
	let blockDialogOpen = $state(false);
	let blockIPVal = $state('');
	let blockReasonVal = $state('');
	let blockDurationMinutes = $state(1440); // 24 hours default
	let isBlocking = $state(false);

	// Auto refresh interval
	let autoRefreshTimer: ReturnType<typeof setInterval> | null = null;

	async function loadAllData(quiet = false) {
		if (!quiet) refreshing = true;
		try {
			const [statsData, eventsData, blockedData] = await Promise.all([
				api.getSecurityStats(),
				api.getSecurityEvents({
					limit: pageSize,
					offset: pageOffset,
					type: filterType,
					severity: filterSeverity,
					search: searchQuery
				}),
				api.getBlockedIPs()
			]);

			stats = statsData;
			events = eventsData.events || [];
			totalEvents = eventsData.total || 0;
			blockedIPs = blockedData.blocked_ips || [];
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : t('security.toastError');
			toast.error(msg);
		} finally {
			loading = false;
			refreshing = false;
		}
	}

	function handleSearch() {
		pageOffset = 0;
		loadAllData(true);
	}

	function handleFilterChange() {
		pageOffset = 0;
		loadAllData(true);
	}

	function nextPage() {
		if (pageOffset + pageSize < totalEvents) {
			pageOffset += pageSize;
			loadAllData(true);
		}
	}

	function prevPage() {
		if (pageOffset >= pageSize) {
			pageOffset -= pageSize;
			loadAllData(true);
		}
	}

	function openBlockModal(ip = '') {
		blockIPVal = ip;
		blockReasonVal = '';
		blockDurationMinutes = 1440;
		blockDialogOpen = true;
	}

	async function submitBlockIP() {
		if (!blockIPVal.trim()) return;
		isBlocking = true;
		try {
			const res = await api.blockIP({
				ip: blockIPVal.trim(),
				reason: blockReasonVal.trim() || undefined,
				duration_minutes: blockDurationMinutes
			});
			toast.success(res.message || t('security.toastBlockedSuccess'));
			blockDialogOpen = false;
			await loadAllData(true);
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : t('security.toastError');
			toast.error(msg);
		} finally {
			isBlocking = false;
		}
	}

	async function handleUnblockIP(ip: string) {
		try {
			const res = await api.unblockIP(ip);
			toast.success(res.message || t('security.toastUnblockedSuccess'));
			await loadAllData(true);
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : t('security.toastError');
			toast.error(msg);
		}
	}

	async function handleClearLogs() {
		const confirmed = await cp.confirm({
			title: t('security.confirmClearTitle'),
			description: t('security.confirmClearDesc'),
			confirmText: t('security.confirmClearBtn'),
			destructive: true
		});
		if (!confirmed) return;

		try {
			const res = await api.clearSecurityEvents();
			toast.success(res.message || t('security.toastClearSuccess'));
			await loadAllData(true);
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : t('security.toastError');
			toast.error(msg);
		}
	}

	function formatAttackType(type: string): string {
		switch (type) {
			case 'PANEL_BRUTE_FORCE':
				return t('security.typePanelBruteForce');
			case 'SECURITY_ENTRANCE_PROBE':
				return t('security.typeSecurityEntrance');
			case 'HONEYPOT_PROBE':
				return t('security.typeHoneypot');
			case 'SUDO_BRUTE_FORCE':
				return t('security.typeSudoBruteForce');
			case 'WEB_SCANNER':
				return t('security.typeWebScanner');
			case 'SSH_BRUTE_FORCE':
				return t('security.typeSSHBruteForce');
			case 'PORT_SCAN':
				return t('security.typePortScan');
			default:
				return type;
		}
	}

	function severityBadgeVariant(sev: string): 'destructive' | 'secondary' | 'outline' | 'default' {
		switch (sev) {
			case 'CRITICAL':
				return 'destructive';
			case 'HIGH':
				return 'destructive';
			case 'MEDIUM':
				return 'secondary';
			default:
				return 'outline';
		}
	}

	function severityBadgeClass(sev: string): string {
		switch (sev) {
			case 'CRITICAL':
				return 'bg-red-600 hover:bg-red-700 text-white font-semibold animate-pulse';
			case 'HIGH':
				return 'bg-amber-600 hover:bg-amber-700 text-white font-medium';
			case 'MEDIUM':
				return 'bg-yellow-500/20 text-yellow-700 dark:text-yellow-400 border border-yellow-500/30';
			default:
				return 'text-muted-foreground';
		}
	}

	function actionBadgeClass(act: string): string {
		switch (act) {
			case 'BLOCKED':
				return 'bg-destructive/20 text-destructive border-destructive/30 font-semibold';
			case 'RATE_LIMITED':
				return 'bg-amber-500/20 text-amber-700 dark:text-amber-400 border-amber-500/30';
			default:
				return 'bg-secondary text-secondary-foreground';
		}
	}

	// Trigger reload whenever SSE receives an attack or task event
	$effect(() => {
		if (cp.refreshTick > 0) {
			loadAllData(true);
		}
	});

	onMount(() => {
		loadAllData();
		// Auto refresh polling every 12 seconds
		autoRefreshTimer = setInterval(() => {
			loadAllData(true);
		}, 12000);
	});

	onDestroy(() => {
		if (autoRefreshTimer) {
			clearInterval(autoRefreshTimer);
		}
	});
</script>

<div class="flex flex-col gap-6">
	<!-- Page Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div class="flex flex-col gap-1">
			<div class="flex items-center gap-2.5">
				<div class="flex size-9 items-center justify-center rounded-lg bg-destructive/10 text-destructive">
					<ShieldAlertIcon class="size-5" />
				</div>
				<h1 class="text-2xl font-bold tracking-tight">{t('security.title')}</h1>
			</div>
			<p class="text-xs text-muted-foreground sm:text-sm">{t('security.subtitle')}</p>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			<Button
				variant="outline"
				size="sm"
				onclick={() => loadAllData()}
				disabled={refreshing}
				class="gap-1.5"
			>
				<RefreshCwIcon class="size-3.5 {refreshing ? 'animate-spin' : ''}" />
				<span>{refreshing ? t('security.refreshing') : t('security.refresh')}</span>
			</Button>

			<Button
				variant="outline"
				size="sm"
				onclick={handleClearLogs}
				class="gap-1.5 text-muted-foreground hover:text-destructive"
			>
				<Trash2Icon class="size-3.5" />
				<span>{t('security.clearLogs')}</span>
			</Button>

			<Button
				variant="default"
				size="sm"
				onclick={() => openBlockModal()}
				class="gap-1.5"
			>
				<PlusIcon class="size-3.5" />
				<span>{t('security.blockIPBtn')}</span>
			</Button>
		</div>
	</div>

	<!-- Top Stats Metric Cards -->
	<div class="grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-4">
		<!-- Attacks Today -->
		<Card.Root class="relative overflow-hidden border-border/80 bg-card">
			<Card.Header class="pb-2">
				<div class="flex items-center justify-between text-muted-foreground">
					<span class="text-xs font-medium uppercase tracking-wider">{t('security.attacksToday')}</span>
					<div class="rounded-md bg-amber-500/10 p-1.5 text-amber-600 dark:text-amber-400">
						<AlertTriangleIcon class="size-4" />
					</div>
				</div>
				<Card.Title class="text-2xl font-bold">
					{stats?.total_today ?? 0}
				</Card.Title>
			</Card.Header>
			<Card.Content class="pt-0">
				<p class="text-xs text-muted-foreground">{t('security.attacksTodaySub')}</p>
			</Card.Content>
		</Card.Root>

		<!-- Blocked IPs -->
		<Card.Root class="relative overflow-hidden border-border/80 bg-card">
			<Card.Header class="pb-2">
				<div class="flex items-center justify-between text-muted-foreground">
					<span class="text-xs font-medium uppercase tracking-wider">{t('security.blockedIPs')}</span>
					<div class="rounded-md bg-destructive/10 p-1.5 text-destructive">
						<BanIcon class="size-4" />
					</div>
				</div>
				<Card.Title class="text-2xl font-bold">
					{stats?.total_blocked ?? 0}
				</Card.Title>
			</Card.Header>
			<Card.Content class="pt-0">
				<p class="text-xs text-muted-foreground">{t('security.blockedIPsSub')}</p>
			</Card.Content>
		</Card.Root>

		<!-- Critical Threats -->
		<Card.Root class="relative overflow-hidden border-border/80 bg-card">
			<Card.Header class="pb-2">
				<div class="flex items-center justify-between text-muted-foreground">
					<span class="text-xs font-medium uppercase tracking-wider">{t('security.criticalThreats')}</span>
					<div class="rounded-md bg-red-600/10 p-1.5 text-red-600 dark:text-red-400">
						<ShieldXIcon class="size-4" />
					</div>
				</div>
				<Card.Title class="text-2xl font-bold text-red-600 dark:text-red-400">
					{stats?.critical_count ?? 0}
				</Card.Title>
			</Card.Header>
			<Card.Content class="pt-0">
				<p class="text-xs text-muted-foreground">{t('security.criticalThreatsSub')}</p>
			</Card.Content>
		</Card.Root>

		<!-- High Threats -->
		<Card.Root class="relative overflow-hidden border-border/80 bg-card">
			<Card.Header class="pb-2">
				<div class="flex items-center justify-between text-muted-foreground">
					<span class="text-xs font-medium uppercase tracking-wider">{t('security.highThreats')}</span>
					<div class="rounded-md bg-yellow-500/10 p-1.5 text-yellow-600 dark:text-yellow-400">
						<AlertCircleIcon class="size-4" />
					</div>
				</div>
				<Card.Title class="text-2xl font-bold text-yellow-600 dark:text-yellow-400">
					{stats?.high_count ?? 0}
				</Card.Title>
			</Card.Header>
			<Card.Content class="pt-0">
				<p class="text-xs text-muted-foreground">{t('security.highThreatsSub')}</p>
			</Card.Content>
		</Card.Root>
	</div>

	<!-- Main Content Tabs -->
	<Tabs.Root bind:value={activeTab} class="flex flex-col gap-4">
		<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
			<Tabs.List class="grid w-full grid-cols-3 sm:w-auto">
				<Tabs.Trigger value="events" class="gap-1.5 text-xs sm:text-sm">
					<ShieldAlertIcon class="size-3.5" />
					<span>{t('security.tabEvents')}</span>
				</Tabs.Trigger>
				<Tabs.Trigger value="blocked" class="gap-1.5 text-xs sm:text-sm">
					<BanIcon class="size-3.5" />
					<span>{t('security.tabBlocked')}</span>
					{#if blockedIPs.length > 0}
						<Badge variant="secondary" class="ml-1 px-1.5 py-0 text-[10px]">{blockedIPs.length}</Badge>
					{/if}
				</Tabs.Trigger>
				<Tabs.Trigger value="analytics" class="gap-1.5 text-xs sm:text-sm">
					<BarChart3Icon class="size-3.5" />
					<span>{t('security.tabAnalytics')}</span>
				</Tabs.Trigger>
			</Tabs.List>

			<!-- Live Feed Status Pill -->
			<div class="flex items-center gap-2 self-end text-xs text-muted-foreground sm:self-auto">
				<span class="relative flex size-2">
					<span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75"></span>
					<span class="relative inline-flex size-2 rounded-full bg-emerald-500"></span>
				</span>
				<span>{t('security.liveFeedActive')}</span>
			</div>
		</div>

		<!-- Tab 1: Attack Events Log -->
		<Tabs.Content value="events" class="flex flex-col gap-4">
			<!-- Filters & Search Toolbar -->
			<Card.Root class="p-3.5">
				<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
					<div class="relative flex-1">
						<SearchIcon class="absolute top-1/2 left-3 size-3.5 -translate-y-1/2 text-muted-foreground" />
						<Input
							type="text"
							placeholder={t('security.searchPlaceholder')}
							bind:value={searchQuery}
							onkeydown={(e) => e.key === 'Enter' && handleSearch()}
							class="pl-9 text-xs sm:text-sm"
						/>
					</div>

					<div class="flex flex-wrap items-center gap-2">
						<!-- Attack Type Filter -->
						<select
							bind:value={filterType}
							onchange={handleFilterChange}
							class="h-9 rounded-md border border-input bg-background px-3 py-1 text-xs text-foreground shadow-sm focus:outline-none focus:ring-1 focus:ring-ring sm:text-sm"
						>
							<option value="all">{t('security.filterAllTypes')}</option>
							<option value="PANEL_BRUTE_FORCE">{t('security.typePanelBruteForce')}</option>
							<option value="HONEYPOT_PROBE">{t('security.typeHoneypot')}</option>
							<option value="SECURITY_ENTRANCE_PROBE">{t('security.typeSecurityEntrance')}</option>
							<option value="SUDO_BRUTE_FORCE">{t('security.typeSudoBruteForce')}</option>
							<option value="WEB_SCANNER">{t('security.typeWebScanner')}</option>
						</select>

						<!-- Severity Filter -->
						<select
							bind:value={filterSeverity}
							onchange={handleFilterChange}
							class="h-9 rounded-md border border-input bg-background px-3 py-1 text-xs text-foreground shadow-sm focus:outline-none focus:ring-1 focus:ring-ring sm:text-sm"
						>
							<option value="all">{t('security.filterAllSeverities')}</option>
							<option value="CRITICAL">{t('security.sevCritical')}</option>
							<option value="HIGH">{t('security.sevHigh')}</option>
							<option value="MEDIUM">{t('security.sevMedium')}</option>
							<option value="LOW">{t('security.sevLow')}</option>
						</select>

						<Button size="sm" variant="secondary" onclick={handleSearch}>
							{t('common.actions.filter')}
						</Button>
					</div>
				</div>
			</Card.Root>

			<!-- Attack Events Table -->
			<Card.Root class="overflow-hidden">
				<ResponsiveTableWrapper>
					<Table.Root>
						<Table.Header>
							<Table.Row class="hover:bg-transparent">
								<Table.Head class="w-[150px]">{t('security.colTime')}</Table.Head>
								<Table.Head class="w-[140px]">{t('security.colIP')}</Table.Head>
								<Table.Head class="w-[90px]">{t('security.colCountry')}</Table.Head>
								<Table.Head class="w-[180px]">{t('security.colType')}</Table.Head>
								<Table.Head class="w-[100px]">{t('security.colSeverity')}</Table.Head>
								<Table.Head class="w-[180px]">{t('security.colTarget')}</Table.Head>
								<Table.Head class="w-[120px]">{t('security.colAction')}</Table.Head>
								<Table.Head>{t('security.colDetails')}</Table.Head>
								<Table.Head class="w-[80px] text-right">{t('security.colActions')}</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#if loading}
								<Table.Row>
									<Table.Cell colspan={9} class="h-32 text-center text-muted-foreground">
										<div class="flex items-center justify-center gap-2">
											<Spinner class="size-4 text-primary" />
											<span>{t('common.status.loading')}</span>
										</div>
									</Table.Cell>
								</Table.Row>
							{:else if events.length === 0}
								<Table.Row>
									<Table.Cell colspan={9} class="h-36 text-center text-muted-foreground">
										<div class="flex flex-col items-center justify-center gap-2 py-4">
											<div class="rounded-full bg-emerald-500/10 p-3 text-emerald-500">
												<ShieldCheckIcon class="size-6" />
											</div>
											<span class="text-sm font-medium">{t('security.emptyEvents')}</span>
										</div>
									</Table.Cell>
								</Table.Row>
							{:else}
								{#each events as ev (ev.id)}
									<Table.Row class="transition-colors hover:bg-muted/40">
										<!-- Time -->
										<Table.Cell class="font-mono text-xs text-muted-foreground">
											{ev.timestamp ? i18n.formatTime(ev.timestamp) : '-'}
										</Table.Cell>

										<!-- IP -->
										<Table.Cell class="font-mono text-xs font-semibold">
											{ev.source_ip}
										</Table.Cell>

										<!-- Country -->
										<Table.Cell>
											<Badge variant="outline" class="font-mono text-[10px] tracking-wider uppercase">
												{ev.country_code || 'EXT'}
											</Badge>
										</Table.Cell>

										<!-- Type -->
										<Table.Cell class="text-xs font-medium">
											{formatAttackType(ev.attack_type)}
										</Table.Cell>

										<!-- Severity -->
										<Table.Cell>
											<Badge class="text-[10px] {severityBadgeClass(ev.severity)}">
												{ev.severity}
											</Badge>
										</Table.Cell>

										<!-- Target -->
										<Table.Cell class="font-mono text-xs text-muted-foreground truncate max-w-[180px]">
											{ev.target || '-'}
										</Table.Cell>

										<!-- Action Taken -->
										<Table.Cell>
											<Badge variant="outline" class="text-[10px] {actionBadgeClass(ev.action_taken)}">
												{ev.action_taken}
											</Badge>
										</Table.Cell>

										<!-- Details -->
										<Table.Cell class="text-xs text-muted-foreground truncate max-w-[280px]" title={ev.details}>
											{ev.details || '-'}
										</Table.Cell>

										<!-- Actions -->
										<Table.Cell class="text-right">
											<Button
												size="xs"
												variant="ghost"
												class="h-7 px-2 text-destructive hover:bg-destructive/10"
												onclick={() => openBlockModal(ev.source_ip)}
												title="Block this IP"
											>
												<BanIcon class="size-3.5" />
											</Button>
										</Table.Cell>
									</Table.Row>
								{/each}
							{/if}
						</Table.Body>
					</Table.Root>
				</ResponsiveTableWrapper>

				<!-- Pagination Bar -->
				<div class="flex items-center justify-between border-t border-border/60 px-4 py-3 text-xs text-muted-foreground">
					<span>
						{t('common.status.total')}: <strong class="text-foreground">{totalEvents}</strong> {t('security.tabEvents')}
					</span>
					<div class="flex items-center gap-2">
						<Button
							size="xs"
							variant="outline"
							onclick={prevPage}
							disabled={pageOffset === 0}
						>
							{t('common.actions.prev')}
						</Button>
						<span>
							{Math.floor(pageOffset / pageSize) + 1} / {Math.max(1, Math.ceil(totalEvents / pageSize))}
						</span>
						<Button
							size="xs"
							variant="outline"
							onclick={nextPage}
							disabled={pageOffset + pageSize >= totalEvents}
						>
							{t('common.actions.next')}
						</Button>
					</div>
				</div>
			</Card.Root>
		</Tabs.Content>

		<!-- Tab 2: Blocked IPs -->
		<Tabs.Content value="blocked" class="flex flex-col gap-4">
			<Card.Root class="overflow-hidden">
				<ResponsiveTableWrapper>
					<Table.Root>
						<Table.Header>
							<Table.Row class="hover:bg-transparent">
								<Table.Head class="w-[180px]">{t('security.colIP')}</Table.Head>
								<Table.Head class="w-[200px]">{t('security.colBlockedAt')}</Table.Head>
								<Table.Head class="w-[200px]">{t('security.colExpiresAt')}</Table.Head>
								<Table.Head>{t('security.colReason')}</Table.Head>
								<Table.Head class="w-[120px] text-right">{t('security.colActions')}</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#if loading}
								<Table.Row>
									<Table.Cell colspan={5} class="h-28 text-center text-muted-foreground">
										<div class="flex items-center justify-center gap-2">
											<Spinner class="size-4 text-primary" />
											<span>{t('common.status.loading')}</span>
										</div>
									</Table.Cell>
								</Table.Row>
							{:else if blockedIPs.length === 0}
								<Table.Row>
									<Table.Cell colspan={5} class="h-32 text-center text-muted-foreground">
										<div class="flex flex-col items-center justify-center gap-2 py-4">
											<div class="rounded-full bg-muted p-2.5 text-muted-foreground">
												<ShieldIcon class="size-5" />
											</div>
											<span class="text-sm font-medium">{t('security.emptyBlocked')}</span>
										</div>
									</Table.Cell>
								</Table.Row>
							{:else}
								{#each blockedIPs as blk (blk.ip)}
									<Table.Row>
										<Table.Cell class="font-mono text-xs font-bold text-destructive">
											{blk.ip}
										</Table.Cell>
										<Table.Cell class="font-mono text-xs text-muted-foreground">
											{blk.blocked_at ? i18n.formatTime(blk.blocked_at) : '-'}
										</Table.Cell>
										<Table.Cell class="font-mono text-xs text-muted-foreground">
											{#if blk.expires_at}
												{i18n.formatTime(blk.expires_at)}
											{:else}
												<Badge variant="outline" class="text-[10px] uppercase">{t('security.permanent')}</Badge>
											{/if}
										</Table.Cell>
										<Table.Cell class="text-xs text-muted-foreground">
											{blk.reason || '-'}
										</Table.Cell>
										<Table.Cell class="text-right">
											<Button
												size="xs"
												variant="outline"
												class="text-xs"
												onclick={() => handleUnblockIP(blk.ip)}
											>
												{t('security.unblockBtn')}
											</Button>
										</Table.Cell>
									</Table.Row>
								{/each}
							{/if}
						</Table.Body>
					</Table.Root>
				</ResponsiveTableWrapper>
			</Card.Root>
		</Tabs.Content>

		<!-- Tab 3: Threat Analytics -->
		<Tabs.Content value="analytics" class="flex flex-col gap-4">
			<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
				<!-- Top Attack Types Breakdown -->
				<Card.Root>
					<Card.Header>
						<Card.Title class="text-base font-semibold">{t('security.tabAnalytics')} - 7 Hari Terakhir</Card.Title>
						<Card.Description class="text-xs">Distribusi tipe serangan yang menyasar server</Card.Description>
					</Card.Header>
					<Card.Content>
						{#if !stats || Object.keys(stats.attacks_by_type || {}).length === 0}
							<div class="py-8 text-center text-xs text-muted-foreground">
								Belum ada data serangan yang cukup untuk analitik.
							</div>
						{:else}
							<div class="flex flex-col gap-3">
								{#each Object.entries(stats.attacks_by_type) as [type, count]}
									<div class="flex flex-col gap-1.5">
										<div class="flex items-center justify-between text-xs">
											<span class="font-medium text-foreground">{formatAttackType(type)}</span>
											<span class="font-mono text-muted-foreground">{count} hit</span>
										</div>
										<div class="h-2 w-full overflow-hidden rounded-full bg-secondary">
											<div
												class="h-full rounded-full bg-primary"
												style="width: {Math.min(100, (count / Math.max(1, stats.total_today || 1)) * 100)}%"
											></div>
										</div>
									</div>
								{/each}
							</div>
						{/if}
					</Card.Content>
				</Card.Root>

				<!-- Top Attacker IPs -->
				<Card.Root>
					<Card.Header>
						<Card.Title class="text-base font-semibold">Top IP Penyerang Teratas</Card.Title>
						<Card.Description class="text-xs">Alamat IP publik yang paling sering melakukan percobaan serangan</Card.Description>
					</Card.Header>
					<Card.Content>
						{#if !stats || (stats.top_attacker_ips || []).length === 0}
							<div class="py-8 text-center text-xs text-muted-foreground">
								Belum ada IP penyerang berulang yang teridentifikasi.
							</div>
						{:else}
							<div class="flex flex-col divide-y divide-border/60">
								{#each stats.top_attacker_ips as top}
									<div class="flex items-center justify-between py-2.5">
										<div class="flex items-center gap-2">
											<Badge variant="outline" class="font-mono text-[10px] uppercase">
												{top.country_code || 'EXT'}
											</Badge>
											<span class="font-mono text-xs font-semibold">{top.ip}</span>
										</div>
										<div class="flex items-center gap-2">
											<Badge variant="secondary" class="font-mono text-xs">
												{top.count} serangan
											</Badge>
											<Button
												size="xs"
												variant="ghost"
												class="h-7 px-2 text-destructive hover:bg-destructive/10"
												onclick={() => openBlockModal(top.ip)}
											>
												<BanIcon class="size-3.5" />
											</Button>
										</div>
									</div>
								{/each}
							</div>
						{/if}
					</Card.Content>
				</Card.Root>
			</div>
		</Tabs.Content>
	</Tabs.Root>
</div>

<!-- Manual Block Dialog -->
<Dialog.Root bind:open={blockDialogOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2">
				<BanIcon class="size-4 text-destructive" />
				<span>{t('security.blockModalTitle')}</span>
			</Dialog.Title>
			<Dialog.Description class="text-xs">
				{t('security.blockModalDesc')}
			</Dialog.Description>
		</Dialog.Header>

		<div class="flex flex-col gap-4 py-2">
			<div class="flex flex-col gap-1.5">
				<Label for="block-ip" class="text-xs font-medium">{t('security.ipLabel')}</Label>
				<Input
					id="block-ip"
					type="text"
					placeholder={t('security.ipPlaceholder')}
					bind:value={blockIPVal}
					class="font-mono text-xs sm:text-sm"
				/>
			</div>

			<div class="flex flex-col gap-1.5">
				<Label for="block-reason" class="text-xs font-medium">{t('security.reasonLabel')}</Label>
				<Input
					id="block-reason"
					type="text"
					placeholder={t('security.reasonPlaceholder')}
					bind:value={blockReasonVal}
					class="text-xs sm:text-sm"
				/>
			</div>

			<div class="flex flex-col gap-1.5">
				<Label for="block-dur" class="text-xs font-medium">{t('security.durationLabel')}</Label>
				<select
					id="block-dur"
					bind:value={blockDurationMinutes}
					class="h-9 rounded-md border border-input bg-background px-3 py-1 text-xs text-foreground shadow-sm focus:outline-none focus:ring-1 focus:ring-ring sm:text-sm"
				>
					<option value={60}>{t('security.dur1Hour')}</option>
					<option value={1440}>{t('security.dur24Hours')}</option>
					<option value={10080}>{t('security.dur7Days')}</option>
					<option value={0}>{t('security.durPermanent')}</option>
				</select>
			</div>
		</div>

		<Dialog.Footer class="gap-2 sm:gap-0">
			<Button variant="outline" size="sm" onclick={() => (blockDialogOpen = false)}>
				{t('security.cancel')}
			</Button>
			<Button
				variant="destructive"
				size="sm"
				onclick={submitBlockIP}
				disabled={isBlocking || !blockIPVal.trim()}
				class="gap-1.5"
			>
				{#if isBlocking}
					<Spinner class="size-3.5" />
				{/if}
				<span>{isBlocking ? t('security.blocking') : t('security.confirmBlock')}</span>
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
