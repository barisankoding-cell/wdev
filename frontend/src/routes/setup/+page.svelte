<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PaginationBar from '#lib/components/shared/PaginationBar.svelte';
	import { api } from '#lib/api/client.js';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore, MODE_LABELS, type ServerMode } from '#lib/stores/mode.svelte.js';
	import type { SetupComponentStatus, TaskRecord } from '#lib/types/api.js';
	import CheckIcon from '@lucide/svelte/icons/check';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import ZapIcon from '@lucide/svelte/icons/zap';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import FileTextIcon from '@lucide/svelte/icons/file-text';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import ActivityIcon from '@lucide/svelte/icons/activity';

	const fullSetupSteps = $derived([
		t('setup.fullSteps.s1'),
		t('setup.fullSteps.s2'),
		t('setup.fullSteps.s3'),
		t('setup.fullSteps.s4'),
		t('setup.fullSteps.s5'),
		t('setup.fullSteps.s6'),
		t('setup.fullSteps.s7'),
		t('setup.fullSteps.s8'),
		t('setup.fullSteps.s9'),
		t('setup.fullSteps.s10'),
		t('setup.fullSteps.s11'),
		t('setup.fullSteps.s12'),
		t('setup.fullSteps.s13'),
		t('setup.fullSteps.s14')
	]);

	interface ModularItem {
		component: string;
		label: string;
		description: string;
		versions?: string[];
		defaultVersion?: string;
		modes?: ServerMode[];
	}

	interface ModularCategory {
		title: string;
		description: string;
		items: ModularItem[];
	}

	interface InstallPreset {
		id: string;
		title: string;
		badge: string;
		recommended?: boolean;
		description: string;
		components: string[];
	}

	const modularCategories = $derived<ModularCategory[]>([
		{
			title: t('setup.categories.core.title'),
			description: t('setup.categories.core.description'),
			items: [
				{
					component: 'base',
					label: t('setup.items.base.label'),
					description: t('setup.items.base.description')
				},
				{
					component: 'node',
					label: t('setup.items.node.label'),
					description: t('setup.items.node.description'),
					versions: ['24', '22', '20'],
					defaultVersion: '22'
				},
				{
					component: 'go',
					label: t('setup.items.go.label'),
					description: t('setup.items.go.description')
				},
				{
					component: 'rust',
					label: t('setup.items.rust.label'),
					description: t('setup.items.rust.description')
				},
				{
					component: 'java',
					label: t('setup.items.java.label'),
					description: t('setup.items.java.description'),
					versions: ['25', '21', '17'],
					defaultVersion: '21'
				}
			]
		},
		{
			title: t('setup.categories.webDb.title'),
			description: t('setup.categories.webDb.description'),
			items: [
				{
					component: 'nginx',
					label: t('setup.items.nginx.label'),
					description: t('setup.items.nginx.description')
				},
				{
					component: 'php',
					label: t('setup.items.php.label'),
					description: t('setup.items.php.description'),
					versions: ['8.4', '8.3', '8.2', '8.1'],
					defaultVersion: '8.4'
				},
				{
					component: 'composer',
					label: t('setup.items.composer.label'),
					description: t('setup.items.composer.description')
				},
				{
					component: 'ssl',
					label: t('setup.items.ssl.label'),
					description: t('setup.items.ssl.description'),
					modes: ['local', 'edge']
				},
				{
					component: 'postgres',
					label: t('setup.items.postgres.label'),
					description: t('setup.items.postgres.description'),
					versions: ['18', '17', '16'],
					defaultVersion: '18'
				},
				{
					component: 'redis',
					label: t('setup.items.redis.label'),
					description: t('setup.items.redis.description')
				},
				{
					component: 'php-stack',
					label: t('setup.items.php-stack.label'),
					description: t('setup.items.php-stack.description'),
					modes: ['local']
				},
				{
					component: 'docker',
					label: t('setup.items.docker.label'),
					description: t('setup.items.docker.description')
				}
			]
		},
		{
			title: t('setup.categories.mobile.title'),
			description: t('setup.categories.mobile.description'),
			items: [
				{
					component: 'android',
					label: t('setup.items.android.label'),
					description: t('setup.items.android.description'),
					modes: ['local', 'edge']
				},
				{
					component: 'flutter',
					label: t('setup.items.flutter.label'),
					description: t('setup.items.flutter.description'),
					modes: ['local', 'edge']
				}
			]
		},
		{
			title: t('setup.categories.networkWorkspace.title'),
			description: t('setup.categories.networkWorkspace.description'),
			items: [
				{
					component: 'netbird',
					label: t('setup.items.netbird.label'),
					description: t('setup.items.netbird.description'),
					modes: ['edge']
				},
				{
					component: 'pdns',
					label: t('setup.items.pdns.label'),
					description: t('setup.items.pdns.description'),
					modes: ['vps']
				},
				{
					component: 'mail',
					label: t('setup.items.mail.label'),
					description: t('setup.items.mail.description'),
					modes: ['vps']
				},
				{
					component: 'cloudflare',
					label: t('setup.items.cloudflare.label'),
					description: t('setup.items.cloudflare.description')
				},
				{
					component: 'zsh',
					label: t('setup.items.zsh.label'),
					description: t('setup.items.zsh.description'),
					versions: ['powerlevel10k', 'agnoster', 'robbyrussell'],
					defaultVersion: 'powerlevel10k'
				},
				{
					component: 'orca-desktop',
					label: t('setup.items.orca-desktop.label'),
					description: t('setup.items.orca-desktop.description'),
					modes: ['local', 'edge']
				},
				{
					component: 'orca-headless',
					label: t('setup.items.orca-headless.label'),
					description: t('setup.items.orca-headless.description'),
					modes: ['edge', 'vps']
				}
			]
		}
	]);

	const presetsByMode = $derived<Record<ServerMode, InstallPreset[]>>({
		local: [
			{
				id: 'local-dev',
				title: t('setup.presets.local-dev.title'),
				badge: t('setup.presets.local-dev.badge'),
				recommended: true,
				description: t('setup.presets.local-dev.description'),
				components: ['base', 'node', 'go', 'rust', 'nginx', 'php', 'composer', 'ssl', 'postgres', 'redis', 'zsh']
			},
			{
				id: 'local-web',
				title: t('setup.presets.local-web.title'),
				badge: t('setup.presets.local-web.badge'),
				description: t('setup.presets.local-web.description'),
				components: ['base', 'nginx', 'php', 'composer', 'ssl', 'postgres', 'redis']
			},
			{
				id: 'local-cf',
				title: t('setup.presets.local-cf.title'),
				badge: t('setup.presets.local-cf.badge'),
				description: t('setup.presets.local-cf.description'),
				components: ['base', 'nginx', 'php', 'composer', 'ssl', 'cloudflare']
			},
			{
				id: 'local-mobile',
				title: t('setup.presets.local-mobile.title'),
				badge: t('setup.presets.local-mobile.badge'),
				description: t('setup.presets.local-mobile.description'),
				components: ['base', 'java', 'node', 'android', 'flutter']
			}
		],
		edge: [
			{
				id: 'edge-client',
				title: t('setup.presets.edge-client.title'),
				badge: t('setup.presets.edge-client.badge'),
				recommended: true,
				description: t('setup.presets.edge-client.description'),
				components: ['base', 'netbird', 'nginx', 'php', 'composer', 'postgres', 'redis']
			},
			{
				id: 'edge-dev',
				title: t('setup.presets.edge-dev.title'),
				badge: t('setup.presets.edge-dev.badge'),
				description: t('setup.presets.edge-dev.description'),
				components: ['base', 'netbird', 'node', 'go', 'rust', 'nginx', 'php', 'composer', 'postgres', 'redis', 'zsh']
			},
			{
				id: 'edge-lite',
				title: t('setup.presets.edge-lite.title'),
				badge: t('setup.presets.edge-lite.badge'),
				description: t('setup.presets.edge-lite.description'),
				components: ['base', 'netbird', 'nginx', 'php', 'composer']
			},
			{
				id: 'edge-mobile',
				title: t('setup.presets.edge-mobile.title'),
				badge: t('setup.presets.edge-mobile.badge'),
				description: t('setup.presets.edge-mobile.description'),
				components: ['base', 'java', 'node', 'android', 'flutter']
			}
		],
		vps: [
			{
				id: 'vps-complete',
				title: t('setup.presets.vps-complete.title'),
				badge: t('setup.presets.vps-complete.badge'),
				recommended: true,
				description: t('setup.presets.vps-complete.description'),
				components: ['base', 'nginx', 'php', 'composer', 'postgres', 'redis', 'pdns', 'mail']
			},
			{
				id: 'vps-hosting',
				title: t('setup.presets.vps-hosting.title'),
				badge: t('setup.presets.vps-hosting.badge'),
				description: t('setup.presets.vps-hosting.description'),
				components: ['base', 'nginx', 'php', 'composer', 'postgres', 'redis']
			},
			{
				id: 'vps-dns-mail',
				title: t('setup.presets.vps-dns-mail.title'),
				badge: t('setup.presets.vps-dns-mail.badge'),
				description: t('setup.presets.vps-dns-mail.description'),
				components: ['base', 'pdns', 'mail']
			},
			{
				id: 'vps-web-lite',
				title: t('setup.presets.vps-web-lite.title'),
				badge: t('setup.presets.vps-web-lite.badge'),
				description: t('setup.presets.vps-web-lite.description'),
				components: ['base', 'nginx', 'php', 'composer']
			}
		]
	});

	const modularItemMap = $derived(
		new Map<string, ModularItem>(
			modularCategories.flatMap((cat) => cat.items.map((item) => [item.component, item]))
		)
	);

	let componentStatuses = $state<Record<string, SetupComponentStatus>>({});
	let statusLoading = $state(true);
	let showAllComponents = $state(false);
	let hasUserToggled = $state(false);
	let selectedComponents = $state<Record<string, boolean>>({});
	let selectedVersions = $state<Record<string, string>>({
		node: '22',
		java: '21',
		postgres: '18',
		php: '8.4',
		zsh: 'powerlevel10k'
	});

	let tasks = $state<TaskRecord[]>([]);
	const TASK_PAGE_SIZE = 10;
	let taskPage = $state(1);
	const paginatedTasks = $derived(
		tasks.slice((taskPage - 1) * TASK_PAGE_SIZE, taskPage * TASK_PAGE_SIZE)
	);
	let logDialogOpen = $state(false);
	let selectedTask = $state<TaskRecord | null>(null);

	const activeMode = $derived<ServerMode>(modeStore.mode);
	const installPresets = $derived<InstallPreset[]>(presetsByMode[activeMode] ?? presetsByMode.local);

	const hiddenCount = $derived.by(() => {
		let count = 0;
		for (const cat of modularCategories) {
			for (const item of cat.items) {
				if (item.modes && !item.modes.includes(activeMode)) {
					count++;
				}
			}
		}
		return count;
	});

	const visibleCategories = $derived.by(() =>
		modularCategories
			.map((cat) => ({
				...cat,
				items: cat.items.filter(
					(item) => showAllComponents || !item.modes || item.modes.includes(activeMode)
				)
			}))
			.filter((cat) => cat.items.length > 0)
	);

	const visibleComponentSet = $derived(
		new Set(visibleCategories.flatMap((c) => c.items.map((i) => i.component)))
	);

	const selectedList = $derived.by(() => {
		const ordered: string[] = [];
		for (const cat of modularCategories) {
			for (const item of cat.items) {
				if (selectedComponents[item.component] && visibleComponentSet.has(item.component)) {
					ordered.push(item.component);
				}
			}
		}
		return ordered;
	});

	const installedInSelection = $derived(
		selectedList.filter((comp) => Boolean(componentStatuses[comp]?.installed))
	);
	const missingInSelection = $derived(
		selectedList.filter((comp) => !componentStatuses[comp]?.installed)
	);

	const installedCountTotal = $derived(
		Array.from(visibleComponentSet).filter((comp) => Boolean(componentStatuses[comp]?.installed)).length
	);

	const matchedPreset = $derived.by(() => {
		if (selectedList.length === 0) return undefined;
		return installPresets.find(
			(preset) =>
				preset.components.length === selectedList.length &&
				preset.components.every((comp) => Boolean(selectedComponents[comp]))
		);
	});

	function isInstalled(component: string): boolean {
		return Boolean(componentStatuses[component]?.installed);
	}

	function syncVersionsFromStatuses(statuses: Record<string, SetupComponentStatus>) {
		const next = { ...selectedVersions };
		const phpVer = statuses.php?.version;
		if (phpVer && ['8.4', '8.3', '8.2', '8.1'].includes(phpVer)) {
			next.php = phpVer;
		}
		const pgVer = statuses.postgres?.version;
		if (pgVer && ['18', '17', '16'].includes(pgVer)) {
			next.postgres = pgVer;
		}
		const nodeVer = statuses.node?.version?.replace(/^v/, '').split('.')[0];
		if (nodeVer && ['24', '22', '20'].includes(nodeVer)) {
			next.node = nodeVer;
		}
		const zshVer = statuses.zsh?.version;
		if (zshVer && ['powerlevel10k', 'agnoster', 'robbyrussell'].includes(zshVer)) {
			next.zsh = zshVer;
		}
		selectedVersions = next;
	}

	function applyInstalledChecklist(resetUserFlag = false) {
		const next: Record<string, boolean> = {};
		for (const [comp, st] of Object.entries(componentStatuses)) {
			if (comp === 'php-stack') continue;
			if (st.installed) {
				next[comp] = true;
			}
		}
		selectedComponents = next;
		if (resetUserFlag) {
			hasUserToggled = false;
		}
	}

	function formatComponentChip(component: string): string {
		const item = modularItemMap.get(component);
		if (!item) return component;
		if (item.versions && item.versions.length > 0) {
			const ver = selectedVersions[component] ?? item.defaultVersion;
			return ver ? `${item.label} (${ver})` : item.label;
		}
		return item.label;
	}

	function buildPayloadItems(components: string[]): { component: string; version?: string }[] {
		return components.map((comp) => {
			const item = modularItemMap.get(comp);
			const ver = item?.versions?.length
				? (selectedVersions[comp] ?? item.defaultVersion)
				: undefined;
			return ver ? { component: comp, version: ver } : { component: comp };
		});
	}

	function toggleComponent(component: string) {
		hasUserToggled = true;
		selectedComponents[component] = !selectedComponents[component];
	}

	function applyPresetToModular(preset: InstallPreset) {
		hasUserToggled = true;
		const next: Record<string, boolean> = {};
		for (const comp of preset.components) {
			next[comp] = true;
		}
		selectedComponents = next;
		document.getElementById('modular-selection-anchor')?.scrollIntoView({
			behavior: 'smooth',
			block: 'start'
		});
	}

	function clearSelection() {
		hasUserToggled = true;
		selectedComponents = {};
	}

	function installPresetDirect(preset: InstallPreset) {
		const missing = preset.components.filter((comp) => !isInstalled(comp));
		const targets = missing.length > 0 ? missing : preset.components;
		const title =
			missing.length > 0 && missing.length < preset.components.length
				? t('setup.completingPresetLabel', { title: preset.title })
				: t('setup.presetTaskLabel', { title: preset.title });
		cp.triggerBatchInstall(title, buildPayloadItems(targets));
	}

	function installSingleModular(item: ModularItem) {
		const ver = item.versions?.length
			? (selectedVersions[item.component] ?? item.defaultVersion)
			: undefined;
		if (item.component === 'mail') {
			cp.triggerBatchInstall(item.label, [{ component: item.component }]);
			return;
		}
		cp.triggerInstall(item.component, ver);
	}

	function installModularList(components: string[], labelPrefix?: string) {
		if (components.length === 0) return;
		const label = labelPrefix
			? labelPrefix
			: matchedPreset
				? t('setup.presetTaskLabel', { title: matchedPreset.title })
				: t('setup.modularTaskTitle', { count: components.length });
		cp.triggerBatchInstall(label, buildPayloadItems(components));
	}

	async function loadComponentStatuses() {
		statusLoading = true;
		try {
			const res = await api.getSetupComponents();
			componentStatuses = res ?? {};
			syncVersionsFromStatuses(componentStatuses);
			if (!hasUserToggled) {
				applyInstalledChecklist(false);
			}
		} catch {
			// Keep modular installer usable if detection fails
		} finally {
			statusLoading = false;
		}
	}

	async function loadTaskHistory() {
		try {
			const res = await api.getTasks(30);
			tasks = res.tasks ?? [];
		} catch {
			tasks = [];
		}
	}

	async function refreshAll() {
		await Promise.all([loadComponentStatuses(), loadTaskHistory()]);
	}

	onMount(() => {
		void refreshAll();
	});

	$effect(() => {
		if (cp.refreshTick > 0) {
			void refreshAll();
		}
	});

	function openTaskLog(task: TaskRecord) {
		selectedTask = task;
		logDialogOpen = true;
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<div class="flex flex-wrap items-center gap-2">
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">
					{t('setup.title')}
				</h1>
				<Badge variant="outline" class="font-medium">
					{t('setup.modeBadgePrefix')} {MODE_LABELS[activeMode]}
				</Badge>
			</div>
			<p class="text-sm text-muted-foreground">
				{#if activeMode === 'vps'}
					{t('setup.subtitleVps')}
				{:else if activeMode === 'edge'}
					{t('setup.subtitleEdge')}
				{:else}
					{t('setup.subtitleLocal')}
				{/if}
			</p>
		</div>
		<div class="flex flex-wrap items-center gap-2">
			<Button variant="outline" size="sm" onclick={() => modeStore.openModal()}>
				{t('setup.switchModeBtn')}
			</Button>
			<Button variant="outline" size="sm" disabled={statusLoading} onclick={refreshAll}>
				{#if statusLoading}
					<Spinner data-icon="inline-start" />
					{t('setup.checkingBtn')}
				{:else}
					<RefreshCwIcon data-icon="inline-start" />
					{t('setup.checkInstalledBtn')}
				{/if}
			</Button>
		</div>
	</div>

	<!-- Mode Presets & Modular Installer -->
	<Card.Root>
		<Card.Header>
			<div class="flex flex-wrap items-start justify-between gap-2">
				<div>
					<Card.Title>{t('setup.cardTitle', { mode: MODE_LABELS[activeMode] })}</Card.Title>
					<Card.Description>
						{t('setup.cardDesc')}
					</Card.Description>
				</div>
				<Badge variant="secondary" class="shrink-0">
					{#if statusLoading}
						{t('setup.scanningBadge')}
					{:else}
						{t('setup.installedSummaryBadge', { installed: installedCountTotal, total: visibleComponentSet.size })}
					{/if}
				</Badge>
			</div>
		</Card.Header>
		<Card.Content class="flex flex-col gap-8">
			<!-- Quick Install Presets -->
			<section aria-labelledby="preset-heading" class="flex flex-col gap-3">
				<div>
					<h3 id="preset-heading" class="text-sm font-semibold text-foreground">
						{t('setup.presetHeading', { mode: MODE_LABELS[activeMode] })}
					</h3>
					<p class="text-xs text-muted-foreground">
						{t('setup.presetSubheading')}
					</p>
				</div>

				<div class="grid grid-cols-1 gap-3 md:grid-cols-2">
					{#each installPresets as preset (preset.id)}
						{@const isActivePreset = matchedPreset?.id === preset.id}
						{@const installedInPreset = preset.components.filter((c) => isInstalled(c)).length}
						{@const missingInPreset = preset.components.filter((c) => !isInstalled(c))}
						{@const allPresetInstalled = missingInPreset.length === 0}
						<div
							class="flex flex-col justify-between gap-4 rounded-xl border-2 p-4 transition-colors {isActivePreset
								? 'border-primary/70 bg-primary/[0.03]'
								: preset.recommended
									? 'border-primary/35 bg-card'
									: 'border-border/80 bg-card'}"
						>
							<div class="flex flex-col gap-2">
								<div class="flex items-start justify-between gap-2">
									<h4 class="text-sm font-semibold leading-snug text-foreground">
										{preset.title}
									</h4>
									<div class="flex shrink-0 flex-wrap items-center gap-1.5">
										<span
											class="rounded-md border px-2 py-0.5 text-[11px] font-medium {allPresetInstalled
												? 'border-success/40 bg-success/10 text-success'
												: 'border-border bg-muted text-muted-foreground'}"
										>
											{allPresetInstalled
												? t('setup.presetCompleteBadge')
												: t('setup.presetInstalledRatio', {
														installed: installedInPreset,
														total: preset.components.length
													})}
										</span>
										<span
											class="rounded-md border px-2 py-0.5 text-[11px] font-medium {preset.recommended
												? 'border-primary/40 bg-primary/10 text-primary'
												: 'border-border bg-muted text-muted-foreground'}"
										>
											{isActivePreset ? t('setup.presetActiveInModular') : preset.badge}
										</span>
									</div>
								</div>
								<p class="text-xs leading-relaxed text-muted-foreground">
									{preset.description}
								</p>
								<div class="mt-1 flex flex-wrap gap-1.5">
									{#each preset.components as comp (comp)}
										{@const done = isInstalled(comp)}
										<span
											class="inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[11px] {done
												? 'border-success/40 bg-success/10 text-success'
												: 'border-border/70 bg-background text-foreground/85'}"
										>
											{#if done}
												<CheckIcon class="size-3 shrink-0" />
											{/if}
											{formatComponentChip(comp)}
										</span>
									{/each}
								</div>
							</div>

							<div class="flex flex-wrap items-center gap-2 border-t border-border/60 pt-3">
								<Button
									type="button"
									size="sm"
									variant={allPresetInstalled ? 'outline' : 'default'}
									aria-label={t('setup.installPresetAria', { title: preset.title })}
									onclick={() => installPresetDirect(preset)}
								>
									{allPresetInstalled
										? t('setup.reinstallPresetBtn')
										: missingInPreset.length < preset.components.length
											? t('setup.installMissingPresetBtn', { count: missingInPreset.length })
											: t('setup.installPresetBtn')}
								</Button>
								<Button
									type="button"
									variant="outline"
									size="sm"
									aria-label={t('setup.selectModularAria', { title: preset.title })}
									onclick={() => applyPresetToModular(preset)}
								>
									{t('setup.selectInModularBtn')}
								</Button>
							</div>
						</div>
					{/each}
				</div>
			</section>

			<!-- Modular Component Selection -->
			<div id="modular-selection-anchor" class="flex flex-col gap-6 border-t border-border pt-6">
				<div class="flex flex-col gap-3 rounded-xl border bg-muted/35 p-3.5 sm:flex-row sm:items-center sm:justify-between">
					<div class="flex flex-col gap-0.5">
						<span class="text-sm font-semibold text-foreground">
							{t('setup.modularTitle')}
						</span>
						<span class="text-xs text-muted-foreground">
							{#if selectedList.length === 0}
								{t('setup.modularEmptyHint')}
							{:else if matchedPreset}
								{t('setup.modularMatchedPreset', {
									total: selectedList.length,
									title: matchedPreset.title,
									installed: installedInSelection.length,
									missing: missingInSelection.length
								})}
							{:else}
								{t('setup.modularSelectedSummary', {
									total: selectedList.length,
									installed: installedInSelection.length,
									missing: missingInSelection.length
								})}
							{/if}
						</span>
					</div>
					<div class="flex flex-wrap items-center gap-2">
						{#if hiddenCount > 0}
							<Button
								type="button"
								variant="outline"
								size="sm"
								onclick={() => (showAllComponents = !showAllComponents)}
							>
								<LayersIcon data-icon="inline-start" />
								{showAllComponents
									? t('setup.filterModeBtn', { mode: MODE_LABELS[activeMode] })
									: t('setup.allComponentsBtn', { count: hiddenCount })}
							</Button>
						{/if}
						<Button
							type="button"
							variant="outline"
							size="sm"
							onclick={() => applyInstalledChecklist(true)}
						>
							{t('setup.checkInstalledSelectionBtn')}
						</Button>
						<Button
							type="button"
							variant="ghost"
							size="sm"
							disabled={selectedList.length === 0}
							onclick={clearSelection}
						>
							{t('common.actions.clear')}
						</Button>
						{#if missingInSelection.length > 0}
							<Button
								type="button"
								size="sm"
								onclick={() =>
									installModularList(
										missingInSelection,
										t('setup.installNewBtn', { count: missingInSelection.length })
									)}
							>
								{t('setup.installNewBtn', { count: missingInSelection.length })}
							</Button>
							{#if installedInSelection.length > 0}
								<Button
									type="button"
									variant="outline"
									size="sm"
									onclick={() => installModularList(selectedList)}
								>
									{t('setup.reinstallAllBtn', { count: selectedList.length })}
								</Button>
							{/if}
						{:else}
							<Button
								type="button"
								variant="outline"
								size="sm"
								disabled={selectedList.length === 0}
								onclick={() =>
									installModularList(
										selectedList,
										t('setup.updateReinstallTask', { count: selectedList.length })
									)}
							>
								{selectedList.length > 0
									? t('setup.updateReinstallBtn', { count: selectedList.length })
									: t('setup.installSelectedBtn')}
							</Button>
						{/if}
					</div>
				</div>

				{#each visibleCategories as cat (cat.title)}
					<fieldset class="flex flex-col gap-3 border-0 p-0">
						<legend class="mb-0.5 flex flex-wrap items-baseline gap-2">
							<span class="text-xs font-bold uppercase tracking-wider text-muted-foreground">
								{cat.title}
							</span>
							<span class="text-xs text-muted-foreground/80">
								— {cat.description}
							</span>
						</legend>
						<div class="grid grid-cols-1 gap-2.5 sm:grid-cols-2 xl:grid-cols-3">
							{#each cat.items as item (item.component)}
								{@const isChecked = Boolean(selectedComponents[item.component])}
								{@const st = componentStatuses[item.component]}
								{@const installed = Boolean(st?.installed)}
								{@const inputId = `modular-item-${item.component}`}
								<div
									class="flex flex-col justify-between gap-3 rounded-lg border-2 p-3 transition-colors {isChecked
										? 'border-primary/70 bg-primary/[0.04]'
										: installed
											? 'border-success/40 bg-success/[0.02] hover:border-success/70'
											: 'border-border/75 bg-card hover:border-border'}"
								>
									<label for={inputId} class="flex cursor-pointer items-start gap-2.5 select-none">
										<input
											id={inputId}
											type="checkbox"
											checked={isChecked}
											onchange={() => toggleComponent(item.component)}
											class="mt-0.5 size-4 shrink-0 cursor-pointer rounded border-input accent-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
										/>
										<span class="flex min-w-0 flex-1 flex-col gap-1">
											<span class="flex flex-wrap items-center justify-between gap-1.5">
												<span class="text-sm font-medium leading-tight text-foreground">
													{item.label}
												</span>
												{#if installed}
													<StatusBadge
														tone="success"
														label={st?.version
															? t('setup.installedBadgeVer', { ver: st.version })
															: t('common.status.installed')}
													/>
												{/if}
											</span>
											<span class="text-xs leading-snug text-muted-foreground">
												{item.description}
											</span>
										</span>
									</label>

									<div class="flex items-center justify-between gap-2 border-t border-border/50 pt-2">
										{#if item.versions && item.versions.length > 0}
											<Select.Root
												type="single"
												value={selectedVersions[item.component] ?? item.defaultVersion}
												onValueChange={(v) => {
													if (v) selectedVersions = { ...selectedVersions, [item.component]: v };
												}}
											>
												<Select.Trigger class="h-7 min-w-24 text-xs" aria-label={t('setup.versionAria', { label: item.label })}>
													{t('setup.versionPrefix', { ver: selectedVersions[item.component] ?? item.defaultVersion ?? '' })}
												</Select.Trigger>
												<Select.Content>
													<Select.Group>
														{#each item.versions as v (v)}
															<Select.Item value={v}>{v}</Select.Item>
														{/each}
													</Select.Group>
												</Select.Content>
											</Select.Root>
										{:else}
											<span class="text-[11px] text-muted-foreground/80">{t('setup.autoLatest')}</span>
										{/if}

										<div class="flex items-center gap-1">
											{#if item.component === 'docker'}
												<Button
													type="button"
													variant="ghost"
													size="sm"
													class="h-7 px-2 text-xs"
													aria-label={t('setup.dockerConfigAria')}
													onclick={() => cp.openServiceConfig('docker')}
												>
													<SettingsIcon class="size-3.5" data-icon="inline-start" />
													{t('common.actions.configure')}
												</Button>
											{/if}
								{#if item.component === 'rust' && installed}
									<Button
										type="button"
										variant="ghost"
										size="sm"
										class="h-7 px-2 text-xs"
										aria-label={t('setup.sccacheStatsAria')}
										onclick={() => cp.openSccacheStats()}
									>
										<ActivityIcon class="size-3.5" data-icon="inline-start" />
										{t('setup.sccacheStatsBtn')}
									</Button>
								{/if}
											<Button
												type="button"
												variant="ghost"
												size="sm"
												class="h-7 px-2.5 text-xs"
												aria-label={t('setup.installOnlyAria', {
													action: installed ? t('setup.reinstallVerb') : t('setup.installVerb'),
													label: item.label
												})}
												onclick={() => installSingleModular(item)}
											>
												{installed ? t('setup.reinstallOnlyBtn') : t('setup.installOnlyBtn')}
											</Button>
										</div>
									</div>
								</div>
							{/each}
						</div>
					</fieldset>
				{/each}

				{#if selectedList.length > 0}
					<div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-primary/30 bg-primary/[0.04] p-3.5">
						<div class="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
							<span class="mr-1 text-xs font-medium text-foreground">
								{t('setup.selectedCountLabel', { count: selectedList.length })}
							</span>
							{#each selectedList as comp (comp)}
								{@const done = isInstalled(comp)}
								<span
									class="inline-flex items-center gap-1 rounded border px-2 py-0.5 text-xs {done
										? 'border-success/40 bg-success/10 text-success'
										: 'border-border bg-background text-foreground'}"
								>
									{#if done}
										<CheckIcon class="size-3 shrink-0" />
									{/if}
									{formatComponentChip(comp)}
								</span>
							{/each}
						</div>
						{#if missingInSelection.length > 0}
							<Button
								type="button"
								size="sm"
								onclick={() =>
									installModularList(
										missingInSelection,
										t('setup.installNewBtn', { count: missingInSelection.length })
									)}
							>
								{t('setup.installNewBtn', { count: missingInSelection.length })}
							</Button>
						{:else}
							<Button
								type="button"
								variant="outline"
								size="sm"
								onclick={() =>
									installModularList(
										selectedList,
										t('setup.updateReinstallTask', { count: selectedList.length })
									)}
							>
								{t('setup.updateReinstallBtn', { count: selectedList.length })}
							</Button>
						{/if}
					</div>
				{/if}
			</div>
		</Card.Content>
	</Card.Root>

	<!-- Full Auto Setup (14-Step Workstation Bundle) -->
	<Card.Root>
		<Card.Header>
			<div class="flex flex-wrap items-center justify-between gap-2">
				<Card.Title>{t('setup.fullSetupTitle')}</Card.Title>
				{#if activeMode !== 'local'}
					<Badge variant="outline">{t('setup.fullSetupBadge')}</Badge>
				{/if}
			</div>
			<Card.Description>
				{#if activeMode === 'vps'}
					{t('setup.fullSetupDescVps')}
				{:else}
					{t('setup.fullSetupDescDefault')}
				{/if}
			</Card.Description>
		</Card.Header>
		<Card.Content>
			<div class="grid gap-2 text-xs text-muted-foreground sm:grid-cols-2 lg:grid-cols-3">
				{#each fullSetupSteps as step (step)}
					<div class="flex items-center gap-1.5">
						<CheckIcon class="size-3.5 shrink-0 text-success" />
						<span>{step}</span>
					</div>
				{/each}
			</div>
		</Card.Content>
		<Card.Footer>
			<Button
				variant={activeMode === 'vps' ? 'outline' : 'default'}
				class="w-full sm:w-auto"
				onclick={() => cp.triggerInstall('full-install')}
			>
				<ZapIcon data-icon="inline-start" />
				{t('setup.runFullSetupBtn')}
			</Button>
		</Card.Footer>
	</Card.Root>

	<!-- Task History -->
	<Card.Root>
		<Card.Header>
			<div class="flex flex-wrap items-start justify-between gap-3">
				<div>
					<Card.Title>{t('setup.historyTitle')}</Card.Title>
					<Card.Description>{t('setup.historyDesc')}</Card.Description>
				</div>
				<Button variant="outline" size="sm" class="w-full sm:w-auto" onclick={loadTaskHistory}>
					<RefreshCwIcon data-icon="inline-start" />
					{t('common.actions.refresh')}
				</Button>
			</div>
		</Card.Header>
		<Card.Content>
			<div class="flex flex-col sm:overflow-hidden sm:rounded-lg sm:border sm:bg-card">
				<ResponsiveTableWrapper label={t('setup.historyTableAria')} class="rounded-none border-0 bg-transparent">
					<Table.Root>
						<Table.Header>
							<Table.Row>
								<Table.Head>{t('setup.taskCol')}</Table.Head>
								<Table.Head>{t('setup.statusCol')}</Table.Head>
								<Table.Head>{t('setup.startedAtCol')}</Table.Head>
								<Table.Head class="text-right">{t('setup.logCol')}</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#if tasks.length === 0}
								<Table.Row>
									<Table.Cell colspan={4} class="py-6 text-center text-muted-foreground">
										{t('setup.noHistory')}
									</Table.Cell>
								</Table.Row>
							{:else}
								{#each paginatedTasks as task (task.id)}
									<Table.Row>
										<Table.Cell data-label={t('setup.taskCol')} class="font-medium">{task.command}</Table.Cell>
										<Table.Cell data-label={t('setup.statusCol')}>
											<StatusBadge
												tone={task.status === 'success'
													? 'success'
													: task.status === 'failed'
														? 'destructive'
														: 'info'}
												label={task.status}
											/>
										</Table.Cell>
										<Table.Cell data-label={t('setup.startedAtCol')} class="font-mono text-xs text-muted-foreground">
											{i18n.formatDateTime(task.started_at)}
										</Table.Cell>
										<Table.Cell data-label={t('setup.logCol')} class="text-right">
											<Button variant="outline" size="xs" onclick={() => openTaskLog(task)}>
												<FileTextIcon data-icon="inline-start" />
												{t('setup.viewLogBtn')}
											</Button>
										</Table.Cell>
									</Table.Row>
								{/each}
							{/if}
						</Table.Body>
					</Table.Root>
				</ResponsiveTableWrapper>
				<PaginationBar
					bind:page={taskPage}
					totalItems={tasks.length}
					pageSize={TASK_PAGE_SIZE}
					itemLabel={t('setup.historyItemLabel')}
					class="rounded-lg border bg-card sm:rounded-none sm:border-0 sm:border-t mt-3 sm:mt-0"
				/>
			</div>
		</Card.Content>
	</Card.Root>
</div>

<Dialog.Root bind:open={logDialogOpen}>
	<Dialog.Content class="flex max-h-[85dvh] flex-col gap-3 sm:max-w-3xl">
		<Dialog.Header>
			<Dialog.Title>{selectedTask?.command || t('setup.logDialogDefaultTitle')}</Dialog.Title>
			{#if selectedTask}
				<Dialog.Description class="font-mono text-xs">
					{t('setup.logDialogStatus', {
						status: selectedTask.status,
						time: i18n.formatDateTime(selectedTask.started_at)
					})}
				</Dialog.Description>
			{/if}
		</Dialog.Header>
		<pre
			class="max-h-[55dvh] overflow-auto rounded-lg border bg-terminal p-4 font-mono text-xs leading-relaxed text-terminal-foreground whitespace-pre-wrap break-words"
		>{selectedTask?.log_output || t('setup.logDialogEmpty')}</pre>
		<Dialog.Footer>
			<Button type="button" onclick={() => (logDialogOpen = false)}>{t('common.actions.close')}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
