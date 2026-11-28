<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { SccacheStatsReport } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw';
	import CheckCircle2Icon from '@lucide/svelte/icons/check-circle-2';
	import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import WrenchIcon from '@lucide/svelte/icons/wrench';

	let stats = $state<SccacheStatsReport | null>(null);
	let loading = $state(false);
	let resetting = $state(false);
	let loadError = $state('');
	let showRaw = $state(false);

	function formatBytes(bytes: number): string {
		if (!bytes || bytes <= 0) return '0 B';
		const gib = 1024 * 1024 * 1024;
		const mib = 1024 * 1024;
		const kib = 1024;
		if (bytes >= gib) return `${(bytes / gib).toFixed(2)} GiB`;
		if (bytes >= mib) return `${(bytes / mib).toFixed(2)} MiB`;
		if (bytes >= kib) return `${(bytes / kib).toFixed(1)} KiB`;
		return `${Math.round(bytes)} B`;
	}

	function formatMs(ms: number): string {
		if (!ms || ms <= 0) return '0.0 ms';
		if (ms >= 1000) return `${(ms / 1000).toFixed(2)} s`;
		return `${ms.toFixed(1)} ms`;
	}

	async function loadStats() {
		loading = true;
		loadError = '';
		try {
			stats = await api.getSccacheStats();
		} catch (err) {
			loadError = err instanceof Error ? err.message : t('modals.sccacheStats.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function resetCounters() {
		resetting = true;
		try {
			stats = await api.zeroSccacheStats();
			toast.success(t('modals.sccacheStats.resetSuccess'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('modals.sccacheStats.resetFailed'));
		} finally {
			resetting = false;
		}
	}

	async function syncEnvFromModal() {
		cp.sccacheStatsOpen = false;
		await cp.syncToolchainEnv();
	}

	$effect(() => {
		if (cp.sccacheStatsOpen) {
			loadStats();
		}
	});

	$effect(() => {
		if (cp.sccacheStatsOpen && cp.refreshTick > 0) {
			loadStats();
		}
	});

	const overallBadge = $derived.by(() => {
		if (!stats || !stats.installed) {
			return { tone: 'neutral' as const, label: t('modals.sccacheStats.statusNotInstalled') };
		}
		if (!stats.active_configured) {
			return { tone: 'warning' as const, label: t('modals.sccacheStats.statusNotConfigured') };
		}
		if (stats.compile_requests > 0 || stats.cache_hits > 0 || stats.cache_misses > 0) {
			return { tone: 'success' as const, label: t('modals.sccacheStats.statusInUse') };
		}
		return { tone: 'info' as const, label: t('modals.sccacheStats.statusReadyIdle') };
	});

	const cachePct = $derived.by(() => {
		if (!stats || !stats.max_cache_size_bytes || stats.max_cache_size_bytes <= 0) return 0;
		return Math.min(100, (stats.cache_size_bytes / stats.max_cache_size_bytes) * 100);
	});

	const langBreakdown = $derived.by(() => {
		if (!stats) return [] as { lang: string; hits: number; misses: number }[];
		const keys = new Set([
			...Object.keys(stats.cache_hits_by_lang ?? {}),
			...Object.keys(stats.cache_misses_by_lang ?? {})
		]);
		return Array.from(keys).map((lang) => ({
			lang,
			hits: stats?.cache_hits_by_lang?.[lang] ?? 0,
			misses: stats?.cache_misses_by_lang?.[lang] ?? 0
		}));
	});
</script>

<Dialog.Root bind:open={cp.sccacheStatsOpen}>
	<Dialog.Content
		class="flex max-h-[88dvh] w-full max-w-[calc(100%-1.5rem)] flex-col gap-0 overflow-hidden p-4 sm:max-w-2xl sm:p-6"
	>
		<Dialog.Header class="shrink-0 border-b border-border/40 pb-3">
			<div class="flex flex-wrap items-start justify-between gap-2 pr-6">
				<div class="flex min-w-0 items-center gap-2.5">
					<div class="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
						<ActivityIcon class="size-4" />
					</div>
					<div class="min-w-0">
						<Dialog.Title class="text-base sm:text-lg">{t('modals.sccacheStats.title')}</Dialog.Title>
						<Dialog.Description class="truncate text-xs">
							{#if stats?.installed}
								v{stats.version || '-'} · {stats.binary_path || '/usr/local/bin/sccache'}
								{#if stats.target_user}
									· {stats.target_user}
								{/if}
							{:else}
								{t('modals.sccacheStats.subtitle')}
							{/if}
						</Dialog.Description>
					</div>
				</div>
				{#if stats}
					<div class="flex flex-wrap items-center gap-1.5">
						<StatusBadge tone={overallBadge.tone} label={overallBadge.label} />
						{#if stats.installed}
							<Badge variant="outline" class="text-[11px]">
								{stats.server_running
									? t('modals.sccacheStats.daemonRunning')
									: t('modals.sccacheStats.daemonStandby')}
							</Badge>
						{/if}
					</div>
				{/if}
			</div>
		</Dialog.Header>

		<div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto py-4 pr-1 -mr-1">
			{#if loading && !stats}
				<div class="grid gap-3 sm:grid-cols-2">
					{#each [1, 2, 3, 4] as idx (idx)}
						<Skeleton class="h-24 w-full rounded-lg" />
					{/each}
				</div>
				<Skeleton class="h-32 w-full rounded-lg" />
			{:else if loadError}
				<Alert.Root variant="destructive">
					<AlertCircleIcon class="size-4" />
					<Alert.Description>{loadError}</Alert.Description>
				</Alert.Root>
			{:else if stats}
				{#if stats.error}
					<Alert.Root variant="destructive">
						<AlertCircleIcon class="size-4" />
						<Alert.Description class="font-mono text-xs">{stats.error}</Alert.Description>
					</Alert.Root>
				{/if}

				<!-- Configuration & Activation Scope -->
				<div class="flex flex-col gap-2 rounded-lg border bg-muted/30 p-3">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<span class="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
							{t('modals.sccacheStats.configTitle')}
						</span>
						{#if stats.installed && !stats.active_configured}
							<Button size="xs" onclick={syncEnvFromModal}>
								<WrenchIcon data-icon="inline-start" />
								{t('modals.sccacheStats.syncEnvBtn')}
							</Button>
						{/if}
					</div>
					<div class="flex flex-col gap-1.5 text-xs">
						{#if stats.user_cargo_config_path}
							<div class="flex items-center justify-between gap-2">
								<span class="flex min-w-0 items-center gap-1.5 truncate">
									{#if stats.user_cargo_configured}
										<CheckCircle2Icon class="size-3.5 shrink-0 text-success" />
									{:else}
										<AlertTriangleIcon class="size-3.5 shrink-0 text-warning" />
									{/if}
									<span class="truncate">
										{t('modals.sccacheStats.userCargoLabel', { user: stats.target_user || 'non-root' })}
									</span>
									<span class="hidden truncate font-mono text-[11px] text-muted-foreground sm:inline">
										({stats.user_cargo_config_path})
									</span>
								</span>
								<StatusBadge
									tone={stats.user_cargo_configured ? 'success' : 'warning'}
									label={stats.user_cargo_configured
										? t('modals.sccacheStats.configuredBadge')
										: t('modals.sccacheStats.missingBadge')}
								/>
							</div>
						{/if}

						{#if stats.system_cargo_config_path}
							<div class="flex items-center justify-between gap-2">
								<span class="flex min-w-0 items-center gap-1.5 truncate">
									{#if stats.system_cargo_configured}
										<CheckCircle2Icon class="size-3.5 shrink-0 text-success" />
									{:else}
										<AlertTriangleIcon class="size-3.5 shrink-0 text-warning" />
									{/if}
									<span class="truncate">{t('modals.sccacheStats.systemCargoLabel')}</span>
									<span class="hidden truncate font-mono text-[11px] text-muted-foreground sm:inline">
										({stats.system_cargo_config_path})
									</span>
								</span>
								<StatusBadge
									tone={stats.system_cargo_configured ? 'success' : 'warning'}
									label={stats.system_cargo_configured
										? t('modals.sccacheStats.configuredBadge')
										: t('modals.sccacheStats.missingBadge')}
								/>
							</div>
						{/if}

						<div class="flex items-center justify-between gap-2">
							<span class="flex min-w-0 items-center gap-1.5 truncate">
								{#if stats.profile_wrapper_enabled}
									<CheckCircle2Icon class="size-3.5 shrink-0 text-success" />
								{:else}
									<AlertTriangleIcon class="size-3.5 shrink-0 text-warning" />
								{/if}
								<span class="truncate">{t('modals.sccacheStats.profileWrapperLabel')}</span>
							</span>
							<StatusBadge
								tone={stats.profile_wrapper_enabled ? 'success' : 'neutral'}
								label={stats.profile_wrapper_enabled
									? t('modals.sccacheStats.configuredBadge')
									: t('modals.sccacheStats.missingBadge')}
							/>
						</div>
					</div>
				</div>

				<!-- KPI Cards -->
				{#if stats.installed}
					<div class="grid gap-3 sm:grid-cols-2">
						<div class="flex flex-col gap-2 rounded-lg border bg-card p-3">
							<div class="flex items-center justify-between text-xs text-muted-foreground">
								<span>{t('modals.sccacheStats.hitRateLabel')}</span>
								<span class="font-mono font-semibold text-foreground">
									{stats.cache_hit_rate.toFixed(1)}%
								</span>
							</div>
							<Progress value={stats.cache_hit_rate} max={100} class="h-1.5" />
							<span class="text-[11px] text-muted-foreground">
								{t('modals.sccacheStats.hitsMissesSub', {
									hits: stats.cache_hits,
									misses: stats.cache_misses
								})}
							</span>
						</div>

						<div class="flex flex-col justify-between gap-1 rounded-lg border bg-card p-3">
							<span class="text-xs text-muted-foreground">
								{t('modals.sccacheStats.compileRequestsLabel')}
							</span>
							<div class="flex items-baseline justify-between gap-2">
								<span class="font-mono text-xl font-bold text-foreground">
									{stats.compile_requests}
								</span>
								<span class="font-mono text-xs text-muted-foreground">
									{t('modals.sccacheStats.executedSub', { count: stats.requests_executed })}
								</span>
							</div>
							{#if langBreakdown.length > 0}
								<div class="mt-0.5 flex flex-wrap gap-1">
									{#each langBreakdown as lb (lb.lang)}
										<Badge variant="secondary" class="font-mono text-[10px]">
											{lb.lang}: {lb.hits}/{lb.hits + lb.misses}
										</Badge>
									{/each}
								</div>
							{/if}
						</div>

						<div class="flex flex-col justify-between gap-1 rounded-lg border bg-card p-3">
							<span class="text-xs text-muted-foreground">
								{t('modals.sccacheStats.hitsMissesLabel')}
							</span>
							<div class="flex items-baseline gap-3 font-mono">
								<span class="text-lg font-bold text-success">{stats.cache_hits}</span>
								<span class="text-xs text-muted-foreground">/</span>
								<span class="text-lg font-bold text-foreground">{stats.cache_misses}</span>
							</div>
							<span class="text-[11px] text-muted-foreground">
								{t('modals.sccacheStats.hitsMissesSub', {
									hits: stats.cache_hits,
									misses: stats.cache_misses
								})}
							</span>
						</div>

						<div class="flex flex-col gap-2 rounded-lg border bg-card p-3">
							<div class="flex items-center justify-between text-xs text-muted-foreground">
								<span>{t('modals.sccacheStats.diskCacheLabel')}</span>
								<span class="font-mono font-semibold text-foreground">
									{formatBytes(stats.cache_size_bytes)}
								</span>
							</div>
							<Progress value={cachePct} max={100} class="h-1.5" />
							<span class="text-[11px] text-muted-foreground">
								{t('modals.sccacheStats.maxCacheSub', {
									max: formatBytes(stats.max_cache_size_bytes)
								})}
							</span>
						</div>
					</div>

					<!-- Telemetry Details -->
					<div class="flex flex-col gap-2 rounded-lg border bg-muted/20 p-3 text-xs">
						<span class="font-semibold uppercase tracking-wide text-muted-foreground">
							{t('modals.sccacheStats.detailsTitle')}
						</span>
						<div class="grid gap-1.5 sm:grid-cols-2">
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">{t('modals.sccacheStats.preprocessorLabel')}</span>
								<span class="font-mono font-medium">
									{stats.preprocessor_mode
										? t('modals.sccacheStats.enabledVal')
										: t('modals.sccacheStats.disabledVal')}
								</span>
							</div>
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">{t('modals.sccacheStats.avgReadHitLabel')}</span>
								<span class="font-mono font-medium">{formatMs(stats.avg_cache_read_hit_ms)}</span>
							</div>
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">{t('modals.sccacheStats.avgWriteLabel')}</span>
								<span class="font-mono font-medium">{formatMs(stats.avg_cache_write_ms)}</span>
							</div>
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">{t('modals.sccacheStats.avgCompilerLabel')}</span>
								<span class="font-mono font-medium">{formatMs(stats.avg_compiler_ms)}</span>
							</div>
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">{t('modals.sccacheStats.errorsTimeoutsLabel')}</span>
								<span class="font-mono font-medium">
									{stats.cache_errors} / {stats.cache_timeouts} / {stats.compile_fails}
								</span>
							</div>
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">{t('modals.sccacheStats.nonCacheableLabel')}</span>
								<span class="font-mono font-medium">
									{stats.non_cacheable_calls} / {stats.unsupported_compiler}
								</span>
							</div>
						</div>
						{#if stats.cache_location}
							<Separator class="my-1" />
							<div class="flex flex-col gap-0.5">
								<span class="text-[11px] text-muted-foreground">
									{t('modals.sccacheStats.cacheLocationLabel')}
								</span>
								<code class="truncate font-mono text-[11px] text-foreground">
									{stats.cache_location}
								</code>
							</div>
						{/if}
					</div>

					<!-- Raw Output Toggle -->
					{#if stats.raw_output}
						<div class="flex flex-col gap-2">
							<div class="flex items-center justify-between">
								<span class="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
									<TerminalIcon class="size-3.5" />
									{t('modals.sccacheStats.rawOutputTitle')}
								</span>
								<Button variant="ghost" size="xs" onclick={() => (showRaw = !showRaw)}>
									{showRaw ? t('modals.sccacheStats.hideRawBtn') : t('modals.sccacheStats.showRawBtn')}
								</Button>
							</div>
							{#if showRaw}
								<pre
									class="max-h-56 overflow-auto rounded-md border bg-muted/50 p-3 font-mono text-[11px] leading-relaxed"
								>{stats.raw_output}</pre>
							{/if}
						</div>
					{/if}
				{/if}
			{/if}
		</div>

		<Dialog.Footer
			class="shrink-0 flex flex-col-reverse gap-2 border-t border-border/40 pt-3 sm:flex-row sm:items-center sm:justify-between"
		>
			<div>
				{#if stats?.installed}
					<Button
						variant="outline"
						size="sm"
						disabled={loading || resetting}
						onclick={resetCounters}
					>
						{#if resetting}
							<Spinner data-icon="inline-start" />
						{:else}
							<RotateCcwIcon data-icon="inline-start" />
						{/if}
						{t('modals.sccacheStats.resetCountersBtn')}
					</Button>
				{/if}
			</div>
			<div class="flex flex-wrap items-center justify-end gap-2">
				<Button variant="outline" size="sm" disabled={loading || resetting} onclick={loadStats}>
					{#if loading}
						<Spinner data-icon="inline-start" />
					{:else}
						<RefreshCwIcon data-icon="inline-start" />
					{/if}
					{t('modals.sccacheStats.refreshBtn')}
				</Button>
				<Button size="sm" onclick={() => (cp.sccacheStatsOpen = false)}>
					{t('common.actions.close')}
				</Button>
			</div>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
