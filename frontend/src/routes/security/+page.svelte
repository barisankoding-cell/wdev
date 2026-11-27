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
	import { Switch } from '#lib/components/ui/switch/index.js';
	import * as NativeSelect from '#lib/components/ui/native-select/index.js';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import { api, ApiError } from '#lib/api/client.js';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type {
		BlockedIPRecord,
		SecurityEventRecord,
		SecurityMonitorStatusReport,
		SecurityPolicyConfig,
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
	import PlusIcon from '@lucide/svelte/icons/plus';
	import BarChart3Icon from '@lucide/svelte/icons/bar-chart-3';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import FlameIcon from '@lucide/svelte/icons/flame';
	import SaveIcon from '@lucide/svelte/icons/save';
	import XIcon from '@lucide/svelte/icons/x';

	let activeTab = $state<'events' | 'blocked' | 'analytics' | 'monitors'>('events');

	let stats = $state<SecurityStatsReport | null>(null);
	let monitorStatus = $state<SecurityMonitorStatusReport | null>(null);
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

	// Policy & Whitelist Editor
	let policyDraft = $state<SecurityPolicyConfig>({
		auto_ban_enabled: true,
		firewall_sync_enabled: true,
		ssh_monitor_enabled: true,
		nginx_monitor_enabled: true,
		port_scan_monitor_enabled: true,
		panel_scanner_enabled: true,
		mail_monitor_enabled: true,
		nginx_block_sync_enabled: true,
		cloudflare_real_ip_mode: 'auto',
		strike_threshold: 6,
		ban_duration_hours: 24,
		critical_ban_hours: 48,
		retention_days: 30,
		alert_webhook_url: '',
		alert_min_severity: 'CRITICAL',
		cf_block_sync_enabled: false,
		cf_zone_id: '',
		cf_api_token: '',
		cf_token_configured: false,
		whitelist: []
	});
	let policyDirty = $state(false);
	let savingPolicy = $state(false);
	let testingWebhook = $state(false);
	let newWhitelistEntry = $state('');

	// Manual Block Dialog
	let blockDialogOpen = $state(false);
	let blockIPVal = $state('');
	let blockReasonVal = $state('');
	let blockDurationMinutes = $state(1440); // 24 hours default
	let isBlocking = $state(false);

	// Auto refresh interval
	let autoRefreshTimer: ReturnType<typeof setInterval> | null = null;

	const runningMonitorsCount = $derived(
		monitorStatus?.watchers.filter((w) => w.enabled && w.running).length ?? 0
	);
	const totalMonitorsCount = $derived(monitorStatus?.watchers.length ?? 0);
	const total7DayAttacks = $derived.by(() => {
		if (!stats) return 1;
		if (stats.total_7_days && stats.total_7_days > 0) return stats.total_7_days;
		const sum = Object.values(stats.attacks_by_type || {}).reduce((acc, n) => acc + n, 0);
		return Math.max(1, sum);
	});

	async function loadAllData(quiet = false) {
		if (!quiet) refreshing = true;
		try {
			const [statsData, eventsData, blockedData, statusData] = await Promise.all([
				api.getSecurityStats(),
				api.getSecurityEvents({
					limit: pageSize,
					offset: pageOffset,
					type: filterType,
					severity: filterSeverity,
					search: searchQuery
				}),
				api.getBlockedIPs(),
				api.getSecurityStatus()
			]);

			stats = statsData;
			events = eventsData.events || [];
			totalEvents = eventsData.total || 0;
			blockedIPs = blockedData.blocked_ips || [];
			monitorStatus = statusData;
			if (!policyDirty && statusData?.policy) {
				policyDraft = {
					...statusData.policy,
					cf_api_token: '',
					whitelist: [...(statusData.policy.whitelist || [])]
				};
			}
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

	async function savePolicy(nextPolicy = policyDraft): Promise<boolean> {
		savingPolicy = true;
		try {
			const res = await api.updateSecurityConfig({
				...nextPolicy,
				strike_threshold: Number(nextPolicy.strike_threshold) || 6,
				ban_duration_hours: Number(nextPolicy.ban_duration_hours) || 24,
				critical_ban_hours: Number(nextPolicy.critical_ban_hours) || 48,
				retention_days: Number(nextPolicy.retention_days) || 30
			});
			policyDraft = {
				...res.policy,
				cf_api_token: '',
				whitelist: [...(res.policy.whitelist || [])]
			};
			policyDirty = false;
			toast.success(res.message || t('security.toastPolicySaved'));
			await loadAllData(true);
			return true;
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : t('security.toastError');
			toast.error(msg);
			return false;
		} finally {
			savingPolicy = false;
		}
	}

	async function testWebhook() {
		testingWebhook = true;
		try {
			const res = await api.testSecurityWebhook(policyDraft.alert_webhook_url);
			toast.success(res.message || t('security.toastWebhookSent'));
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : t('security.toastError');
			toast.error(msg);
		} finally {
			testingWebhook = false;
		}
	}

	async function clearCFToken() {
		await savePolicy({
			...policyDraft,
			cf_block_sync_enabled: false,
			cf_api_token: '',
			clear_cf_token: true
		});
	}

	async function addWhitelistEntry() {
		const candidate = newWhitelistEntry.trim();
		if (!candidate) return;
		const next: SecurityPolicyConfig = {
			...policyDraft,
			whitelist: [...(policyDraft.whitelist || []), candidate]
		};
		if (await savePolicy(next)) {
			newWhitelistEntry = '';
		}
	}

	async function removeWhitelistEntry(entry: string) {
		const next: SecurityPolicyConfig = {
			...policyDraft,
			whitelist: (policyDraft.whitelist || []).filter((item) => item !== entry)
		};
		await savePolicy(next);
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
			case 'MAIL_BRUTE_FORCE':
				return t('security.typeMailBruteForce');
			case 'PORT_SCAN':
				return t('security.typePortScan');
			default:
				return type;
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

<div class="flex min-w-0 flex-col gap-5 sm:gap-6">
	<!-- Page Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div class="flex min-w-0 flex-col gap-1">
			<div class="flex items-center gap-2.5">
				<div class="flex size-8 shrink-0 items-center justify-center rounded-lg bg-destructive/10 text-destructive sm:size-9">
					<ShieldAlertIcon class="size-4 sm:size-5" />
				</div>
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('security.title')}</h1>
			</div>
			<p class="text-xs text-muted-foreground sm:text-sm">{t('security.subtitle')}</p>
		</div>

		<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
			<Button
				variant="outline"
				size="sm"
				onclick={() => loadAllData()}
				disabled={refreshing}
				class="h-9 gap-1.5 sm:h-8"
			>
				<RefreshCwIcon class="size-3.5 shrink-0 {refreshing ? 'animate-spin' : ''}" />
				<span class="truncate">{refreshing ? t('security.refreshing') : t('security.refresh')}</span>
			</Button>

			<Button
				variant="outline"
				size="sm"
				onclick={handleClearLogs}
				class="h-9 gap-1.5 text-muted-foreground hover:text-destructive sm:h-8"
			>
				<Trash2Icon class="size-3.5 shrink-0" />
				<span class="truncate">{t('security.clearLogs')}</span>
			</Button>

			<Button
				variant="default"
				size="sm"
				onclick={() => openBlockModal()}
				class="col-span-2 h-9 gap-1.5 sm:col-span-1 sm:h-8"
			>
				<PlusIcon class="size-3.5 shrink-0" />
				<span>{t('security.blockIPBtn')}</span>
			</Button>
		</div>
	</div>

	<!-- Host Firewall & Monitor Status Strip -->
	{#if monitorStatus}
		<Card.Root class="border-border/80 bg-card/70 px-3.5 py-3 sm:px-4">
			<div class="flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
				<div class="flex min-w-0 items-start gap-2.5 sm:items-center">
					<div
						class="mt-0.5 shrink-0 rounded-md p-1.5 sm:mt-0 {monitorStatus.firewall.active
							? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
							: 'bg-amber-500/10 text-amber-600 dark:text-amber-400'}"
					>
						<FlameIcon class="size-4" />
					</div>
					<div class="flex min-w-0 flex-1 flex-col gap-0.5">
						<div class="flex flex-wrap items-center gap-1.5 sm:gap-2">
							<span class="text-xs font-semibold">{t('security.fwStatusTitle')}</span>
							<Badge
								variant={monitorStatus.firewall.active ? 'secondary' : 'outline'}
								class="text-[10px] uppercase"
							>
								{monitorStatus.firewall.engine === 'none'
									? t('security.fwEngineNone')
									: `${monitorStatus.firewall.engine} • ${
											monitorStatus.firewall.active
												? t('security.fwActiveBadge')
												: t('security.fwInactiveBadge')
										}`}
							</Badge>
							{#if monitorStatus.firewall.active && !monitorStatus.firewall.can_manage}
								<Badge variant="outline" class="border-amber-500/40 text-[10px] text-amber-600 dark:text-amber-400">
									{t('security.fwNoPermission')}
								</Badge>
							{/if}
						</div>
						<p class="text-xs leading-relaxed text-muted-foreground">{monitorStatus.firewall.summary}</p>
					</div>
				</div>

				<div class="flex flex-wrap items-center gap-1.5 sm:shrink-0 sm:gap-2">
					{#if monitorStatus.nginx?.available}
						<Badge
							variant="outline"
							class="text-[11px] {monitorStatus.nginx.blocklist_sync
								? 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-400'
								: 'text-muted-foreground'}"
						>
							{monitorStatus.nginx.blocklist_sync
								? t('security.nginxL7BadgeOn', { count: monitorStatus.nginx.blocked_entries })
								: t('security.nginxL7BadgeOff')}
						</Badge>
						<Badge variant="outline" class="text-[11px] uppercase">
							{t('security.nginxRealIPBadge', { mode: monitorStatus.nginx.real_ip_mode || 'off' })}
						</Badge>
					{/if}
					<Badge
						variant="outline"
						class="text-[11px] {monitorStatus.policy.auto_ban_enabled
							? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400'
							: 'text-muted-foreground'}"
					>
						{monitorStatus.policy.auto_ban_enabled
							? t('security.autoBanOnBadge')
							: t('security.autoBanOffBadge')}
					</Badge>
					<Badge variant="secondary" class="text-[11px]">
						{t('security.monitorsActiveSummary', {
							running: runningMonitorsCount,
							total: totalMonitorsCount
						})}
					</Badge>
				</div>
			</div>
		</Card.Root>
	{/if}

	<!-- Top Stats Metric Cards (2x2 on mobile, 4-col on desktop) -->
	<div class="grid grid-cols-2 gap-2.5 sm:gap-3.5 lg:grid-cols-4">
		<!-- Attacks Today -->
		<Card.Root class="relative overflow-hidden border-border/80 bg-card">
			<Card.Header class="px-3.5 pt-3.5 pb-1.5 sm:px-6 sm:pt-6 sm:pb-2">
				<div class="flex items-center justify-between gap-1.5 text-muted-foreground">
					<span class="text-[11px] font-medium uppercase tracking-wider sm:text-xs">{t('security.attacksToday')}</span>
					<div class="shrink-0 rounded-md bg-amber-500/10 p-1 text-amber-600 dark:text-amber-400 sm:p-1.5">
						<AlertTriangleIcon class="size-3.5 sm:size-4" />
					</div>
				</div>
				<Card.Title class="text-xl font-bold tabular-nums sm:text-2xl">
					{stats?.total_today ?? 0}
				</Card.Title>
			</Card.Header>
			<Card.Content class="px-3.5 pt-0 pb-3.5 sm:px-6 sm:pb-6">
				<p class="text-[11px] leading-snug text-muted-foreground sm:text-xs">{t('security.attacksTodaySub')}</p>
			</Card.Content>
		</Card.Root>

		<!-- Blocked IPs -->
		<Card.Root class="relative overflow-hidden border-border/80 bg-card">
			<Card.Header class="px-3.5 pt-3.5 pb-1.5 sm:px-6 sm:pt-6 sm:pb-2">
				<div class="flex items-center justify-between gap-1.5 text-muted-foreground">
					<span class="text-[11px] font-medium uppercase tracking-wider sm:text-xs">{t('security.blockedIPs')}</span>
					<div class="shrink-0 rounded-md bg-destructive/10 p-1 text-destructive sm:p-1.5">
						<BanIcon class="size-3.5 sm:size-4" />
					</div>
				</div>
				<Card.Title class="text-xl font-bold tabular-nums sm:text-2xl">
					{stats?.total_blocked ?? 0}
				</Card.Title>
			</Card.Header>
			<Card.Content class="px-3.5 pt-0 pb-3.5 sm:px-6 sm:pb-6">
				<p class="text-[11px] leading-snug text-muted-foreground sm:text-xs">{t('security.blockedIPsSub')}</p>
			</Card.Content>
		</Card.Root>

		<!-- Critical Threats -->
		<Card.Root class="relative overflow-hidden border-border/80 bg-card">
			<Card.Header class="px-3.5 pt-3.5 pb-1.5 sm:px-6 sm:pt-6 sm:pb-2">
				<div class="flex items-center justify-between gap-1.5 text-muted-foreground">
					<span class="text-[11px] font-medium uppercase tracking-wider sm:text-xs">{t('security.criticalThreats')}</span>
					<div class="shrink-0 rounded-md bg-red-600/10 p-1 text-red-600 dark:text-red-400 sm:p-1.5">
						<ShieldXIcon class="size-3.5 sm:size-4" />
					</div>
				</div>
				<Card.Title class="text-xl font-bold text-red-600 tabular-nums dark:text-red-400 sm:text-2xl">
					{stats?.critical_count ?? 0}
				</Card.Title>
			</Card.Header>
			<Card.Content class="px-3.5 pt-0 pb-3.5 sm:px-6 sm:pb-6">
				<p class="text-[11px] leading-snug text-muted-foreground sm:text-xs">{t('security.criticalThreatsSub')}</p>
			</Card.Content>
		</Card.Root>

		<!-- High Threats -->
		<Card.Root class="relative overflow-hidden border-border/80 bg-card">
			<Card.Header class="px-3.5 pt-3.5 pb-1.5 sm:px-6 sm:pt-6 sm:pb-2">
				<div class="flex items-center justify-between gap-1.5 text-muted-foreground">
					<span class="text-[11px] font-medium uppercase tracking-wider sm:text-xs">{t('security.highThreats')}</span>
					<div class="shrink-0 rounded-md bg-yellow-500/10 p-1 text-yellow-600 dark:text-yellow-400 sm:p-1.5">
						<AlertCircleIcon class="size-3.5 sm:size-4" />
					</div>
				</div>
				<Card.Title class="text-xl font-bold text-yellow-600 tabular-nums dark:text-yellow-400 sm:text-2xl">
					{stats?.high_count ?? 0}
				</Card.Title>
			</Card.Header>
			<Card.Content class="px-3.5 pt-0 pb-3.5 sm:px-6 sm:pb-6">
				<p class="text-[11px] leading-snug text-muted-foreground sm:text-xs">{t('security.highThreatsSub')}</p>
			</Card.Content>
		</Card.Root>
	</div>

	<!-- Main Content Tabs -->
	<Tabs.Root bind:value={activeTab} class="flex min-w-0 flex-col gap-4">
		<!-- Mobile Tab Switcher (Select) -->
		<div class="flex flex-col gap-2 md:hidden">
			<div class="flex items-center justify-between gap-2">
				<Label for="security-tab-select" class="text-xs font-medium text-muted-foreground">
					{t('security.mobileSectionLabel')}
				</Label>
				<div class="flex items-center gap-1.5 text-xs text-muted-foreground">
					<span class="relative flex size-2">
						<span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75"></span>
						<span class="relative inline-flex size-2 rounded-full bg-emerald-500"></span>
					</span>
					<span>{t('security.liveFeedActive')}</span>
				</div>
			</div>
			<NativeSelect.Root
				id="security-tab-select"
				bind:value={activeTab}
				aria-label={t('security.mobileSectionLabel')}
				class="w-full [&>select]:h-10 [&>select]:bg-card [&>select]:text-sm [&>select]:font-medium"
			>
				<NativeSelect.Option value="events">{t('security.tabEvents')}</NativeSelect.Option>
				<NativeSelect.Option value="blocked">
					{blockedIPs.length > 0
						? `${t('security.tabBlocked')} (${blockedIPs.length})`
						: t('security.tabBlocked')}
				</NativeSelect.Option>
				<NativeSelect.Option value="analytics">{t('security.tabAnalytics')}</NativeSelect.Option>
				<NativeSelect.Option value="monitors">{t('security.tabMonitors')}</NativeSelect.Option>
			</NativeSelect.Root>
		</div>

		<!-- Desktop / Tablet Horizontal Tabs -->
		<div class="hidden md:flex md:flex-col md:gap-2.5 lg:flex-row lg:items-center lg:justify-between">
			<Tabs.List class="inline-flex w-fit">
				<Tabs.Trigger value="events" class="gap-1.5 px-3 text-xs lg:text-sm">
					<ShieldAlertIcon class="size-3.5 shrink-0" />
					<span>{t('security.tabEvents')}</span>
				</Tabs.Trigger>
				<Tabs.Trigger value="blocked" class="gap-1.5 px-3 text-xs lg:text-sm">
					<BanIcon class="size-3.5 shrink-0" />
					<span>{t('security.tabBlocked')}</span>
					{#if blockedIPs.length > 0}
						<Badge variant="secondary" class="ml-0.5 px-1.5 py-0 text-[10px]">{blockedIPs.length}</Badge>
					{/if}
				</Tabs.Trigger>
				<Tabs.Trigger value="analytics" class="gap-1.5 px-3 text-xs lg:text-sm">
					<BarChart3Icon class="size-3.5 shrink-0" />
					<span>{t('security.tabAnalytics')}</span>
				</Tabs.Trigger>
				<Tabs.Trigger value="monitors" class="gap-1.5 px-3 text-xs lg:text-sm">
					<ActivityIcon class="size-3.5 shrink-0" />
					<span>{t('security.tabMonitors')}</span>
				</Tabs.Trigger>
			</Tabs.List>

			<!-- Live Feed Status Pill -->
			<div class="flex items-center justify-end gap-2 text-xs text-muted-foreground">
				<span class="relative flex size-2">
					<span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75"></span>
					<span class="relative inline-flex size-2 rounded-full bg-emerald-500"></span>
				</span>
				<span>{t('security.liveFeedActive')}</span>
			</div>
		</div>

		<!-- Tab 1: Attack Events Log -->
		<Tabs.Content value="events" class="flex min-w-0 flex-col gap-3.5 sm:gap-4">
			<!-- Filters & Search Toolbar -->
			<Card.Root class="p-3 sm:p-3.5">
				<div class="flex flex-col gap-2.5 lg:flex-row lg:items-center lg:justify-between">
					<div class="relative min-w-0 flex-1">
						<SearchIcon class="absolute top-1/2 left-3 size-3.5 -translate-y-1/2 text-muted-foreground" />
						<Input
							type="text"
							placeholder={t('security.searchPlaceholder')}
							bind:value={searchQuery}
							onkeydown={(e) => e.key === 'Enter' && handleSearch()}
							class="pl-9 text-xs sm:text-sm"
						/>
					</div>

					<div class="grid grid-cols-1 gap-2 sm:flex sm:flex-wrap sm:items-center">
						<!-- Attack Type Filter -->
						<select
							bind:value={filterType}
							onchange={handleFilterChange}
							class="h-9 w-full min-w-0 rounded-md border border-input bg-background px-3 py-1 text-xs text-foreground shadow-sm focus:ring-1 focus:ring-ring focus:outline-none sm:w-auto sm:text-sm"
						>
							<option value="all">{t('security.filterAllTypes')}</option>
							<option value="PANEL_BRUTE_FORCE">{t('security.typePanelBruteForce')}</option>
							<option value="HONEYPOT_PROBE">{t('security.typeHoneypot')}</option>
							<option value="SECURITY_ENTRANCE_PROBE">{t('security.typeSecurityEntrance')}</option>
							<option value="SUDO_BRUTE_FORCE">{t('security.typeSudoBruteForce')}</option>
							<option value="WEB_SCANNER">{t('security.typeWebScanner')}</option>
							<option value="SSH_BRUTE_FORCE">{t('security.typeSSHBruteForce')}</option>
							<option value="MAIL_BRUTE_FORCE">{t('security.typeMailBruteForce')}</option>
							<option value="PORT_SCAN">{t('security.typePortScan')}</option>
						</select>

						<!-- Severity Filter -->
						<select
							bind:value={filterSeverity}
							onchange={handleFilterChange}
							class="h-9 w-full min-w-0 rounded-md border border-input bg-background px-3 py-1 text-xs text-foreground shadow-sm focus:ring-1 focus:ring-ring focus:outline-none sm:w-auto sm:text-sm"
						>
							<option value="all">{t('security.filterAllSeverities')}</option>
							<option value="CRITICAL">{t('security.sevCritical')}</option>
							<option value="HIGH">{t('security.sevHigh')}</option>
							<option value="MEDIUM">{t('security.sevMedium')}</option>
							<option value="LOW">{t('security.sevLow')}</option>
						</select>

						<Button size="sm" variant="secondary" onclick={handleSearch} class="h-9 w-full sm:w-auto">
							{t('security.filterBtn')}
						</Button>
					</div>
				</div>
			</Card.Root>

			<!-- Attack Events Table (stacked cards on mobile <600px, bordered table on sm+) -->
			<div class="flex flex-col gap-3 sm:gap-0 sm:overflow-hidden sm:rounded-xl sm:border sm:bg-card">
				{#if loading}
					<div class="flex h-36 items-center justify-center gap-2 rounded-xl border bg-card text-sm text-muted-foreground sm:rounded-none sm:border-0">
						<Spinner class="size-4 text-primary" />
						<span>{t('common.actions.loading')}</span>
					</div>
				{:else if events.length === 0}
					<div class="flex flex-col items-center justify-center gap-2 rounded-xl border bg-card px-4 py-10 text-center text-muted-foreground sm:rounded-none sm:border-0">
						<div class="rounded-full bg-emerald-500/10 p-3 text-emerald-500">
							<ShieldCheckIcon class="size-6" />
						</div>
						<span class="text-sm font-medium">{t('security.emptyEvents')}</span>
					</div>
				{:else}
					<ResponsiveTableWrapper label={t('security.tableEventsAria')}>
						<Table.Root>
							<Table.Header>
								<Table.Row class="hover:bg-transparent">
									<Table.Head class="w-[140px]">{t('security.colTime')}</Table.Head>
									<Table.Head class="w-[140px]">{t('security.colIP')}</Table.Head>
									<Table.Head class="w-[85px]">{t('security.colCountry')}</Table.Head>
									<Table.Head class="w-[175px]">{t('security.colType')}</Table.Head>
									<Table.Head class="w-[95px]">{t('security.colSeverity')}</Table.Head>
									<Table.Head class="w-[175px]">{t('security.colTarget')}</Table.Head>
									<Table.Head class="w-[120px]">{t('security.colAction')}</Table.Head>
									<Table.Head>{t('security.colDetails')}</Table.Head>
									<Table.Head class="w-[80px] text-right">{t('security.colActions')}</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each events as ev (ev.id)}
									<Table.Row class="transition-colors hover:bg-muted/40">
										<Table.Cell data-label={t('security.colTime')} class="font-mono text-xs text-muted-foreground">
											{ev.timestamp ? i18n.formatTime(ev.timestamp) : '-'}
										</Table.Cell>

										<Table.Cell data-label={t('security.colIP')} class="font-mono text-xs font-semibold break-all">
											{ev.source_ip}
										</Table.Cell>

										<Table.Cell data-label={t('security.colCountry')}>
											<Badge variant="outline" class="font-mono text-[10px] tracking-wider uppercase">
												{ev.country_code || 'EXT'}
											</Badge>
										</Table.Cell>

										<Table.Cell data-label={t('security.colType')} class="text-xs font-medium">
											{formatAttackType(ev.attack_type)}
										</Table.Cell>

										<Table.Cell data-label={t('security.colSeverity')}>
											<Badge class="text-[10px] {severityBadgeClass(ev.severity)}">
												{ev.severity}
											</Badge>
										</Table.Cell>

										<Table.Cell
											data-label={t('security.colTarget')}
											class="font-mono text-xs break-all text-muted-foreground sm:max-w-[180px] sm:truncate"
										>
											{ev.target || '-'}
										</Table.Cell>

										<Table.Cell data-label={t('security.colAction')}>
											<Badge variant="outline" class="text-[10px] {actionBadgeClass(ev.action_taken)}">
												{ev.action_taken}
											</Badge>
										</Table.Cell>

										<Table.Cell
											data-label={t('security.colDetails')}
											class="text-xs break-words text-muted-foreground sm:max-w-[280px] sm:truncate"
											title={ev.details}
										>
											{ev.details || '-'}
										</Table.Cell>

										<Table.Cell data-label={t('security.colActions')} class="text-right">
											<Button
												size="xs"
												variant="outline"
												class="h-8 w-full gap-1.5 border-destructive/30 text-destructive hover:bg-destructive/10 sm:h-7 sm:w-auto sm:border-0 sm:bg-transparent sm:px-2"
												onclick={() => openBlockModal(ev.source_ip)}
												title={t('security.blockThisIPTitle')}
											>
												<BanIcon class="size-3.5 shrink-0" />
												<span class="sm:hidden">{t('security.blockThisIPTitle')}</span>
											</Button>
										</Table.Cell>
									</Table.Row>
								{/each}
							</Table.Body>
						</Table.Root>
					</ResponsiveTableWrapper>
				{/if}

				<!-- Pagination Bar -->
				<div class="flex flex-col gap-2.5 rounded-xl border bg-card px-4 py-3 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between sm:rounded-none sm:border-0 sm:border-t sm:border-border/60 sm:bg-transparent">
					<span class="text-center sm:text-left">
						{t('security.totalLabel')}: <strong class="text-foreground">{totalEvents}</strong> {t('security.tabEvents')}
					</span>
					<div class="flex items-center justify-between gap-2 sm:justify-end">
						<Button
							size="xs"
							variant="outline"
							class="h-8 px-3 sm:h-7 sm:px-2.5"
							onclick={prevPage}
							disabled={pageOffset === 0}
						>
							{t('common.actions.previous')}
						</Button>
						<span class="tabular-nums">
							{Math.floor(pageOffset / pageSize) + 1} / {Math.max(1, Math.ceil(totalEvents / pageSize))}
						</span>
						<Button
							size="xs"
							variant="outline"
							class="h-8 px-3 sm:h-7 sm:px-2.5"
							onclick={nextPage}
							disabled={pageOffset + pageSize >= totalEvents}
						>
							{t('common.actions.next')}
						</Button>
					</div>
				</div>
			</div>
		</Tabs.Content>

		<!-- Tab 2: Blocked IPs -->
		<Tabs.Content value="blocked" class="flex min-w-0 flex-col gap-4">
			<div class="flex flex-col sm:overflow-hidden sm:rounded-xl sm:border sm:bg-card">
				{#if loading}
					<div class="flex h-32 items-center justify-center gap-2 rounded-xl border bg-card text-sm text-muted-foreground sm:rounded-none sm:border-0">
						<Spinner class="size-4 text-primary" />
						<span>{t('common.actions.loading')}</span>
					</div>
				{:else if blockedIPs.length === 0}
					<div class="flex flex-col items-center justify-center gap-2 rounded-xl border bg-card px-4 py-10 text-center text-muted-foreground sm:rounded-none sm:border-0">
						<div class="rounded-full bg-muted p-2.5 text-muted-foreground">
							<ShieldIcon class="size-5" />
						</div>
						<span class="text-sm font-medium">{t('security.emptyBlocked')}</span>
					</div>
				{:else}
					<ResponsiveTableWrapper label={t('security.tableBlockedAria')}>
						<Table.Root>
							<Table.Header>
								<Table.Row class="hover:bg-transparent">
									<Table.Head class="w-[180px]">{t('security.colIP')}</Table.Head>
									<Table.Head class="w-[190px]">{t('security.colBlockedAt')}</Table.Head>
									<Table.Head class="w-[190px]">{t('security.colExpiresAt')}</Table.Head>
									<Table.Head>{t('security.colReason')}</Table.Head>
									<Table.Head class="w-[120px] text-right">{t('security.colActions')}</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each blockedIPs as blk (blk.ip)}
									<Table.Row>
										<Table.Cell data-label={t('security.colIP')} class="font-mono text-xs font-bold break-all text-destructive">
											{blk.ip}
										</Table.Cell>
										<Table.Cell data-label={t('security.colBlockedAt')} class="font-mono text-xs text-muted-foreground">
											{blk.blocked_at ? i18n.formatTime(blk.blocked_at) : '-'}
										</Table.Cell>
										<Table.Cell data-label={t('security.colExpiresAt')} class="font-mono text-xs text-muted-foreground">
											{#if blk.expires_at}
												{i18n.formatTime(blk.expires_at)}
											{:else}
												<Badge variant="outline" class="text-[10px] uppercase">{t('security.permanent')}</Badge>
											{/if}
										</Table.Cell>
										<Table.Cell data-label={t('security.colReason')} class="text-xs break-words text-muted-foreground">
											{blk.reason || '-'}
										</Table.Cell>
										<Table.Cell data-label={t('security.colActions')} class="text-right">
											<Button
												size="xs"
												variant="outline"
												class="h-8 w-full text-xs sm:h-7 sm:w-auto"
												onclick={() => handleUnblockIP(blk.ip)}
											>
												{t('security.unblockBtn')}
											</Button>
										</Table.Cell>
									</Table.Row>
								{/each}
							</Table.Body>
						</Table.Root>
					</ResponsiveTableWrapper>
				{/if}
			</div>
		</Tabs.Content>

		<!-- Tab 3: Threat Analytics -->
		<Tabs.Content value="analytics" class="flex min-w-0 flex-col gap-4">
			<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
				<!-- Top Attack Types Breakdown -->
				<Card.Root>
					<Card.Header>
						<Card.Title class="text-base font-semibold">{t('security.analyticsTitle7d')}</Card.Title>
						<Card.Description class="text-xs">{t('security.analyticsDesc7d')}</Card.Description>
					</Card.Header>
					<Card.Content>
						{#if !stats || Object.keys(stats.attacks_by_type || {}).length === 0}
							<div class="py-8 text-center text-xs text-muted-foreground">
								{t('security.analyticsEmpty')}
							</div>
						{:else}
							<div class="flex flex-col gap-3">
								{#each Object.entries(stats.attacks_by_type) as [type, count]}
									<div class="flex flex-col gap-1.5">
										<div class="flex items-center justify-between gap-2 text-xs">
											<span class="min-w-0 truncate font-medium text-foreground">{formatAttackType(type)}</span>
											<span class="shrink-0 font-mono text-muted-foreground">
												{t('security.analyticsHits', { count })}
											</span>
										</div>
										<div class="h-2 w-full overflow-hidden rounded-full bg-secondary">
											<div
												class="h-full rounded-full bg-primary"
												style="width: {Math.min(100, (count / total7DayAttacks) * 100)}%"
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
						<Card.Title class="text-base font-semibold">{t('security.topAttackersTitle')}</Card.Title>
						<Card.Description class="text-xs">{t('security.topAttackersDesc')}</Card.Description>
					</Card.Header>
					<Card.Content>
						{#if !stats || (stats.top_attacker_ips || []).length === 0}
							<div class="py-8 text-center text-xs text-muted-foreground">
								{t('security.topAttackersEmpty')}
							</div>
						{:else}
							<div class="flex flex-col divide-y divide-border/60">
								{#each stats.top_attacker_ips as top}
									<div class="flex flex-wrap items-center justify-between gap-2 py-2.5">
										<div class="flex min-w-0 items-center gap-2">
											<Badge variant="outline" class="shrink-0 font-mono text-[10px] uppercase">
												{top.country_code || 'EXT'}
											</Badge>
											<span class="font-mono text-xs font-semibold break-all">{top.ip}</span>
										</div>
										<div class="flex shrink-0 items-center gap-2">
											<Badge variant="secondary" class="font-mono text-xs">
												{t('security.topAttackersCount', { count: top.count })}
											</Badge>
											<Button
												size="xs"
												variant="ghost"
												class="h-8 px-2 text-destructive hover:bg-destructive/10 sm:h-7"
												onclick={() => openBlockModal(top.ip)}
												title={t('security.blockThisIPTitle')}
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

		<!-- Tab 4: Protection, Monitors & Whitelist -->
		<Tabs.Content value="monitors" class="flex min-w-0 flex-col gap-4">
			<!-- Active Watchers Grid -->
			<Card.Root>
				<Card.Header>
					<Card.Title class="text-base font-semibold">{t('security.monitorsCardTitle')}</Card.Title>
					<Card.Description class="text-xs">{t('security.monitorsCardDesc')}</Card.Description>
				</Card.Header>
				<Card.Content>
					<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
						{#each monitorStatus?.watchers ?? [] as w (w.id)}
							<div class="flex min-w-0 flex-col justify-between gap-2.5 rounded-lg border border-border/80 bg-muted/20 p-3.5">
								<div class="flex items-start justify-between gap-2">
									<div class="flex min-w-0 flex-1 flex-col gap-0.5">
										<span class="text-sm font-semibold">{w.name}</span>
										<span class="font-mono text-[11px] break-all text-muted-foreground">{w.source || '-'}</span>
									</div>
									{#if !w.enabled}
										<Badge variant="outline" class="shrink-0 text-[10px] text-muted-foreground">
											{t('security.monitorDisabled')}
										</Badge>
									{:else if !w.available}
										<Badge variant="outline" class="shrink-0 border-amber-500/40 text-[10px] text-amber-600 dark:text-amber-400">
											{t('security.monitorUnavailable')}
										</Badge>
									{:else if w.running}
										<Badge class="shrink-0 bg-emerald-600 text-[10px] text-white hover:bg-emerald-600">
											{t('security.monitorRunning')}
										</Badge>
									{:else}
										<Badge variant="secondary" class="shrink-0 text-[10px]">
											{t('security.monitorPaused')}
										</Badge>
									{/if}
								</div>

								{#if w.note}
									<p class="text-xs leading-relaxed text-muted-foreground">{w.note}</p>
								{/if}

								<div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t border-border/50 pt-2 text-[11px] text-muted-foreground">
									<span>
										{t('security.monitorHits')}: <strong class="font-mono text-foreground">{w.events_count}</strong>
									</span>
									<span>
										{t('security.monitorLastHit')}:
										<strong class="font-mono text-foreground">
											{w.last_event_at ? i18n.formatTime(w.last_event_at) : t('security.monitorNever')}
										</strong>
									</span>
								</div>
							</div>
						{/each}
					</div>
				</Card.Content>
			</Card.Root>

			<div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
				<!-- Policy Controls -->
				<Card.Root>
					<Card.Header>
						<Card.Title class="text-base font-semibold">{t('security.policyTitle')}</Card.Title>
						<Card.Description class="text-xs">{t('security.policyDesc')}</Card.Description>
					</Card.Header>
					<Card.Content class="flex flex-col gap-4">
						<div class="flex items-start justify-between gap-3 py-0.5">
							<div class="flex min-w-0 flex-1 flex-col gap-0.5">
								<Label class="text-xs font-semibold">{t('security.policyAutoBan')}</Label>
								<span class="text-xs leading-snug text-muted-foreground">{t('security.policyAutoBanDesc')}</span>
							</div>
							<Switch
								class="mt-0.5 shrink-0"
								checked={policyDraft.auto_ban_enabled}
								onCheckedChange={(v) => {
									policyDraft.auto_ban_enabled = v;
									policyDirty = true;
								}}
							/>
						</div>

						<div class="flex items-start justify-between gap-3 py-0.5">
							<div class="flex min-w-0 flex-1 flex-col gap-0.5">
								<Label class="text-xs font-semibold">{t('security.policyFirewallSync')}</Label>
								<span class="text-xs leading-snug text-muted-foreground">{t('security.policyFirewallSyncDesc')}</span>
							</div>
							<Switch
								class="mt-0.5 shrink-0"
								checked={policyDraft.firewall_sync_enabled}
								onCheckedChange={(v) => {
									policyDraft.firewall_sync_enabled = v;
									policyDirty = true;
								}}
							/>
						</div>

						<div class="flex items-start justify-between gap-3 py-0.5">
							<div class="flex min-w-0 flex-1 flex-col gap-0.5">
								<Label class="text-xs font-semibold">{t('security.policyPanelScanner')}</Label>
								<span class="text-xs leading-snug text-muted-foreground">{t('security.policyPanelScannerDesc')}</span>
							</div>
							<Switch
								class="mt-0.5 shrink-0"
								checked={policyDraft.panel_scanner_enabled}
								onCheckedChange={(v) => {
									policyDraft.panel_scanner_enabled = v;
									policyDirty = true;
								}}
							/>
						</div>

						<div class="flex items-start justify-between gap-3 py-0.5">
							<div class="flex min-w-0 flex-1 flex-col gap-0.5">
								<Label class="text-xs font-semibold">{t('security.policySSHMonitor')}</Label>
								<span class="text-xs leading-snug text-muted-foreground">{t('security.policySSHMonitorDesc')}</span>
							</div>
							<Switch
								class="mt-0.5 shrink-0"
								checked={policyDraft.ssh_monitor_enabled}
								onCheckedChange={(v) => {
									policyDraft.ssh_monitor_enabled = v;
									policyDirty = true;
								}}
							/>
						</div>

						<div class="flex items-start justify-between gap-3 py-0.5">
							<div class="flex min-w-0 flex-1 flex-col gap-0.5">
								<Label class="text-xs font-semibold">{t('security.policyNginxMonitor')}</Label>
								<span class="text-xs leading-snug text-muted-foreground">{t('security.policyNginxMonitorDesc')}</span>
							</div>
							<Switch
								class="mt-0.5 shrink-0"
								checked={policyDraft.nginx_monitor_enabled}
								onCheckedChange={(v) => {
									policyDraft.nginx_monitor_enabled = v;
									policyDirty = true;
								}}
							/>
						</div>

						<div class="flex items-start justify-between gap-3 py-0.5">
							<div class="flex min-w-0 flex-1 flex-col gap-0.5">
								<Label class="text-xs font-semibold">{t('security.policyMailMonitor')}</Label>
								<span class="text-xs leading-snug text-muted-foreground">{t('security.policyMailMonitorDesc')}</span>
							</div>
							<Switch
								class="mt-0.5 shrink-0"
								checked={policyDraft.mail_monitor_enabled}
								onCheckedChange={(v) => {
									policyDraft.mail_monitor_enabled = v;
									policyDirty = true;
								}}
							/>
						</div>

						<div class="flex items-start justify-between gap-3 py-0.5">
							<div class="flex min-w-0 flex-1 flex-col gap-0.5">
								<Label class="text-xs font-semibold">{t('security.policyPortScanMonitor')}</Label>
								<span class="text-xs leading-snug text-muted-foreground">{t('security.policyPortScanMonitorDesc')}</span>
							</div>
							<Switch
								class="mt-0.5 shrink-0"
								checked={policyDraft.port_scan_monitor_enabled}
								onCheckedChange={(v) => {
									policyDraft.port_scan_monitor_enabled = v;
									policyDirty = true;
								}}
							/>
						</div>

						<div class="flex items-start justify-between gap-3 border-t border-border/60 pt-3 py-0.5">
							<div class="flex min-w-0 flex-1 flex-col gap-0.5">
								<Label class="text-xs font-semibold">{t('security.policyNginxBlockSync')}</Label>
								<span class="text-xs leading-snug text-muted-foreground">{t('security.policyNginxBlockSyncDesc')}</span>
							</div>
							<Switch
								class="mt-0.5 shrink-0"
								checked={policyDraft.nginx_block_sync_enabled}
								onCheckedChange={(v) => {
									policyDraft.nginx_block_sync_enabled = v;
									policyDirty = true;
								}}
							/>
						</div>

						<div class="flex flex-col gap-1.5 py-0.5">
							<Label for="cf-realip-mode" class="text-xs font-semibold">{t('security.policyCFRealIPMode')}</Label>
							<span class="text-xs leading-snug text-muted-foreground">{t('security.policyCFRealIPModeDesc')}</span>
							<select
								id="cf-realip-mode"
								bind:value={policyDraft.cloudflare_real_ip_mode}
								onchange={() => (policyDirty = true)}
								class="mt-1 h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-xs text-foreground shadow-sm focus:ring-1 focus:ring-ring focus:outline-none sm:text-sm"
							>
								<option value="auto">{t('security.cfRealIPAuto')}</option>
								<option value="on">{t('security.cfRealIPOn')}</option>
								<option value="off">{t('security.cfRealIPOff')}</option>
							</select>
						</div>

						{#if monitorStatus?.nginx?.last_error}
							<div class="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
								{monitorStatus.nginx.last_error}
							</div>
						{/if}

						<div class="grid grid-cols-2 gap-3 border-t border-border/60 pt-3 sm:grid-cols-4">
							<div class="flex flex-col gap-1.5">
								<Label for="strike-threshold" class="text-xs">{t('security.policyStrikeThreshold')}</Label>
								<Input
									id="strike-threshold"
									type="number"
									min={2}
									max={100}
									bind:value={policyDraft.strike_threshold}
									oninput={() => (policyDirty = true)}
									class="font-mono text-xs sm:text-sm"
								/>
							</div>
							<div class="flex flex-col gap-1.5">
								<Label for="ban-dur" class="text-xs">{t('security.policyBanDurationHours')}</Label>
								<Input
									id="ban-dur"
									type="number"
									min={1}
									max={8760}
									bind:value={policyDraft.ban_duration_hours}
									oninput={() => (policyDirty = true)}
									class="font-mono text-xs sm:text-sm"
								/>
							</div>
							<div class="flex flex-col gap-1.5">
								<Label for="crit-dur" class="text-xs">{t('security.policyCriticalBanHours')}</Label>
								<Input
									id="crit-dur"
									type="number"
									min={1}
									max={8760}
									bind:value={policyDraft.critical_ban_hours}
									oninput={() => (policyDirty = true)}
									class="font-mono text-xs sm:text-sm"
								/>
							</div>
							<div class="flex flex-col gap-1.5">
								<Label for="retention-days" class="text-xs">{t('security.policyRetentionDays')}</Label>
								<Input
									id="retention-days"
									type="number"
									min={1}
									max={365}
									bind:value={policyDraft.retention_days}
									oninput={() => (policyDirty = true)}
									class="font-mono text-xs sm:text-sm"
								/>
							</div>
						</div>

						<div class="flex justify-end pt-1">
							<Button
								size="sm"
								onclick={() => savePolicy()}
								disabled={savingPolicy}
								class="h-9 w-full gap-1.5 sm:h-8 sm:w-auto"
							>
								{#if savingPolicy}
									<Spinner class="size-3.5" />
									<span>{t('security.savingPolicy')}</span>
								{:else}
									<SaveIcon class="size-3.5" />
									<span>{t('security.savePolicyBtn')}</span>
								{/if}
							</Button>
						</div>
					</Card.Content>
				</Card.Root>

				<div class="flex min-w-0 flex-col gap-4">
					<!-- Whitelist Management -->
					<Card.Root>
						<Card.Header>
							<Card.Title class="text-base font-semibold">{t('security.whitelistTitle')}</Card.Title>
							<Card.Description class="text-xs">{t('security.whitelistDesc')}</Card.Description>
						</Card.Header>
						<Card.Content class="flex flex-col gap-4">
							<div class="flex flex-col gap-2 sm:flex-row sm:items-center">
								<Input
									type="text"
									placeholder={t('security.whitelistPlaceholder')}
									bind:value={newWhitelistEntry}
									onkeydown={(e) => e.key === 'Enter' && addWhitelistEntry()}
									class="min-w-0 flex-1 font-mono text-xs sm:text-sm"
								/>
								<Button
									size="sm"
									variant="secondary"
									onclick={addWhitelistEntry}
									disabled={savingPolicy || !newWhitelistEntry.trim()}
									class="h-9 w-full shrink-0 gap-1 sm:h-9 sm:w-auto"
								>
									<PlusIcon class="size-3.5" />
									<span>{t('security.whitelistAddBtn')}</span>
								</Button>
							</div>

							<p class="rounded-md bg-muted/40 px-3 py-2 text-xs leading-relaxed text-muted-foreground">
								{t('security.whitelistLocalNote')}
							</p>

							{#if (policyDraft.whitelist || []).length === 0}
								<div class="py-6 text-center text-xs text-muted-foreground">
									{t('security.whitelistEmpty')}
								</div>
							{:else}
								<div class="flex flex-wrap gap-2">
									{#each policyDraft.whitelist as entry (entry)}
										<div class="inline-flex max-w-full items-center gap-1.5 rounded-md border border-border bg-secondary/50 px-2.5 py-1 font-mono text-xs break-all">
											<span>{entry}</span>
											<button
												type="button"
												onclick={() => removeWhitelistEntry(entry)}
												disabled={savingPolicy}
												aria-label={t('security.whitelistRemoveAria', { entry })}
												class="shrink-0 rounded p-0.5 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
											>
												<XIcon class="size-3" />
											</button>
										</div>
									{/each}
								</div>
							{/if}
						</Card.Content>
					</Card.Root>

					<!-- Alert Webhook & Cloudflare WAF Sync -->
					<Card.Root>
						<Card.Header>
							<Card.Title class="text-base font-semibold">{t('security.alertsCFTitle')}</Card.Title>
							<Card.Description class="text-xs">{t('security.alertsCFDesc')}</Card.Description>
						</Card.Header>
						<Card.Content class="flex flex-col gap-4">
							<div class="flex flex-col gap-1.5">
								<Label for="alert-webhook-url" class="text-xs font-semibold">{t('security.webhookURLLabel')}</Label>
								<Input
									id="alert-webhook-url"
									type="url"
									placeholder={t('security.webhookURLPlaceholder')}
									bind:value={policyDraft.alert_webhook_url}
									oninput={() => (policyDirty = true)}
									class="font-mono text-xs sm:text-sm"
								/>
							</div>

							<div class="flex flex-col gap-2 sm:flex-row sm:items-end">
								<div class="flex min-w-0 flex-1 flex-col gap-1.5">
									<Label for="alert-min-sev" class="text-xs">{t('security.webhookMinSevLabel')}</Label>
									<select
										id="alert-min-sev"
										bind:value={policyDraft.alert_min_severity}
										onchange={() => (policyDirty = true)}
										class="h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-xs text-foreground shadow-sm focus:ring-1 focus:ring-ring focus:outline-none sm:text-sm"
									>
										<option value="CRITICAL">{t('security.sevCritical')}</option>
										<option value="HIGH">{t('security.sevHigh')}</option>
									</select>
								</div>
								<Button
									size="sm"
									variant="secondary"
									onclick={testWebhook}
									disabled={testingWebhook || !policyDraft.alert_webhook_url?.trim()}
									class="h-9 w-full shrink-0 gap-1.5 sm:w-auto"
								>
									{#if testingWebhook}
										<Spinner class="size-3.5" />
										<span>{t('security.testingWebhook')}</span>
									{:else}
										<span>{t('security.testWebhookBtn')}</span>
									{/if}
								</Button>
							</div>

							<div class="flex items-start justify-between gap-3 border-t border-border/60 pt-3 py-0.5">
								<div class="flex min-w-0 flex-1 flex-col gap-0.5">
									<Label class="text-xs font-semibold">{t('security.cfSyncToggle')}</Label>
									<span class="text-xs leading-snug text-muted-foreground">{t('security.cfSyncToggleDesc')}</span>
								</div>
								<Switch
									class="mt-0.5 shrink-0"
									checked={policyDraft.cf_block_sync_enabled}
									onCheckedChange={(v) => {
										policyDraft.cf_block_sync_enabled = v;
										policyDirty = true;
									}}
								/>
							</div>

							<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
								<div class="flex flex-col gap-1.5">
									<Label for="cf-zone-id" class="text-xs">{t('security.cfZoneIDLabel')}</Label>
									<Input
										id="cf-zone-id"
										type="text"
										placeholder={t('security.cfZoneIDPlaceholder')}
										bind:value={policyDraft.cf_zone_id}
										oninput={() => (policyDirty = true)}
										class="font-mono text-xs sm:text-sm"
									/>
								</div>
								<div class="flex flex-col gap-1.5">
									<Label for="cf-api-token" class="text-xs">{t('security.cfAPITokenLabel')}</Label>
									<div class="flex items-center gap-1.5">
										<Input
											id="cf-api-token"
											type="password"
											placeholder={policyDraft.cf_token_configured
												? t('security.cfAPITokenConfigured')
												: t('security.cfAPITokenPlaceholderNew')}
											bind:value={policyDraft.cf_api_token}
											oninput={() => (policyDirty = true)}
											class="min-w-0 flex-1 font-mono text-xs sm:text-sm"
										/>
										{#if policyDraft.cf_token_configured}
											<Button
												size="sm"
												variant="outline"
												onclick={clearCFToken}
												disabled={savingPolicy}
												class="h-9 shrink-0 px-2.5 text-xs text-destructive hover:text-destructive"
											>
												{t('security.cfClearTokenBtn')}
											</Button>
										{/if}
									</div>
								</div>
							</div>

							<div class="flex justify-end pt-1">
								<Button
									size="sm"
									onclick={() => savePolicy()}
									disabled={savingPolicy}
									class="h-9 w-full gap-1.5 sm:h-8 sm:w-auto"
								>
									{#if savingPolicy}
										<Spinner class="size-3.5" />
										<span>{t('security.savingPolicy')}</span>
									{:else}
										<SaveIcon class="size-3.5" />
										<span>{t('security.savePolicyBtn')}</span>
									{/if}
								</Button>
							</div>
						</Card.Content>
					</Card.Root>
				</div>
			</div>
		</Tabs.Content>
	</Tabs.Root>
</div>

<!-- Manual Block Dialog -->
<Dialog.Root bind:open={blockDialogOpen}>
	<Dialog.Content class="max-w-[calc(100vw-2rem)] sm:max-w-md">
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
					class="h-9 rounded-md border border-input bg-background px-3 py-1 text-xs text-foreground shadow-sm focus:ring-1 focus:ring-ring focus:outline-none sm:text-sm"
				>
					<option value={60}>{t('security.dur1Hour')}</option>
					<option value={1440}>{t('security.dur24Hours')}</option>
					<option value={10080}>{t('security.dur7Days')}</option>
					<option value={0}>{t('security.durPermanent')}</option>
				</select>
			</div>
		</div>

		<Dialog.Footer class="flex-col-reverse gap-2 sm:flex-row sm:justify-end">
			<Button
				variant="outline"
				size="sm"
				class="h-9 w-full sm:h-8 sm:w-auto"
				onclick={() => (blockDialogOpen = false)}
			>
				{t('security.cancel')}
			</Button>
			<Button
				variant="destructive"
				size="sm"
				onclick={submitBlockIP}
				disabled={isBlocking || !blockIPVal.trim()}
				class="h-9 w-full gap-1.5 sm:h-8 sm:w-auto"
			>
				{#if isBlocking}
					<Spinner class="size-3.5" />
				{/if}
				<span>{isBlocking ? t('security.blocking') : t('security.confirmBlock')}</span>
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
