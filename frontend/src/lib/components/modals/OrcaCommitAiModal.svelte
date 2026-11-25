<script lang="ts">
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as NativeSelect from '#lib/components/ui/native-select/index.js';
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { api } from '#lib/api/client.js';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import type {
		OrcaCommitAIConfig,
		OrcaCommitAIModel,
		OrcaCommitAISyncStatus
	} from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import InfoIcon from '@lucide/svelte/icons/info';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import { toast } from 'svelte-sonner';

	let {
		open = $bindable(false),
		onSaved
	}: {
		open: boolean;
		onSaved?: (config: OrcaCommitAIConfig) => void;
	} = $props();

	let loading = $state(false);
	let saving = $state(false);
	let loadError = $state('');
	let formError = $state('');
	let config = $state<OrcaCommitAIConfig | null>(null);
	let syncStatus = $state<OrcaCommitAISyncStatus | null>(null);
	let syncing = $state(false);

	// Form state
	let enabled = $state(true);
	let agentId = $state('opencode');
	let modelId = $state('default');
	let thinkingLevel = $state('');
	let models = $state<OrcaCommitAIModel[]>([]);
	let modelsLoading = $state(false);
	let modelsLiveDiscovered = $state(false);
	let modelsSupportsDiscovery = $state(false);
	let modelsError = $state('');
	let customAgentCommand = $state('');
	let agentArgs = $state('');
	let agentArgsAutoSync = $state(true);
	let agentArgsInputRef = $state<HTMLInputElement | null>(null);
	let customPrompt = $state('');
	let updateDefaultTuiAgent = $state(true);
	let clearRepoOverrides = $state(true);
	let customCmdTouched = $state(false);
	let customCmdInputRef = $state<HTMLInputElement | null>(null);

	const selectedAgent = $derived(
		config?.available_agents.find((agent) => agent.id === agentId) ?? null
	);

	const selectedModelObj = $derived(
		models.find((m) => m.id === modelId) ?? null
	);

	const defaultModelArgs = $derived(
		buildDefaultAgentArgs(agentId, modelId, thinkingLevel)
	);

	const syncBadge = $derived.by((): { label: string; variant: BadgeVariant } => {
		if (!syncStatus?.managed) return { label: t('workspace.commitAi.syncStates.unmanaged'), variant: 'outline' };
		switch (syncStatus.state) {
			case 'synced':
				return { label: t('workspace.commitAi.syncStates.synced'), variant: 'secondary' };
			case 'drift':
				return { label: t('workspace.commitAi.syncStates.drift'), variant: 'destructive' };
			case 'blocked':
				return { label: t('workspace.commitAi.syncStates.blocked'), variant: 'destructive' };
			case 'conflict':
				return { label: t('workspace.commitAi.syncStates.conflict'), variant: 'destructive' };
			case 'error':
				return { label: t('workspace.commitAi.syncStates.error'), variant: 'destructive' };
			default:
				return { label: t('workspace.commitAi.syncStates.unmanaged'), variant: 'outline' };
		}
	});

	const hasMissingSelectedModel = $derived(
		Boolean(modelId) && !models.some((m) => m.id === modelId)
	);

	const activePresetId = $derived.by(() => {
		if (!config) return 'custom';
		const trimmed = customPrompt.trim();
		for (const preset of config.template_presets) {
			if (preset.prompt.trim() === trimmed) {
				return preset.id;
			}
		}
		return 'custom';
	});

	const previewCommandTemplate = $derived.by(() => {
		const trimmed = customPrompt.trim();
		if (!trimmed) return '{basePrompt}';
		if (trimmed.includes('{basePrompt}') || trimmed.includes('{{basePrompt}}')) {
			return trimmed;
		}
		return `{basePrompt}\n\n${trimmed}`;
	});

	const customCmdError = $derived.by(() => {
		if (agentId !== 'custom' || !customCmdTouched) return '';
		return customAgentCommand.trim() === ''
			? t('workspace.commitAi.customCmdRequired')
			: '';
	});

	function quoteCliArg(value: string): string {
		if (/^[A-Za-z0-9_./:@#+=,-]+$/.test(value)) return value;
		return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
	}

	function buildDefaultAgentArgs(targetAgent: string, targetModel: string, targetThinking: string): string {
		const model = targetModel.trim();
		const thinking = targetThinking.trim();
		if (!targetAgent || targetAgent === 'custom' || targetAgent === 'dsh') {
			return '';
		}
		const parts: string[] = [];
		if (targetAgent === 'opencode2') {
			if (model && model !== 'default') {
				parts.push('--model', quoteCliArg(thinking ? `${model}#${thinking}` : model));
			}
			return parts.join(' ');
		}
		const omitsDefaultModel =
			targetAgent === 'omp' ||
			targetAgent === 'opencode' ||
			targetAgent === 'antigravity' ||
			targetAgent === 'pi' ||
			targetAgent === 'kimi' ||
			targetAgent === 'jcode';
		if (model && (!omitsDefaultModel || model !== 'default')) {
			parts.push('--model', quoteCliArg(model));
		}
		if (thinking) {
			switch (targetAgent) {
				case 'omp':
					parts.push('--thinking', quoteCliArg(thinking));
					break;
				case 'opencode':
					parts.push('--variant', quoteCliArg(thinking));
					break;
				case 'antigravity':
					parts.push('--effort', quoteCliArg(thinking));
					break;
			}
		}
		return parts.join(' ');
	}

	function hasBalancedAgentArgQuotes(value: string): boolean {
		let quote: '"' | "'" | null = null;
		for (let i = 0; i < value.length; i++) {
			const ch = value[i];
			if (quote) {
				if (ch === '\\' && quote === '"' && i + 1 < value.length) {
					i++;
					continue;
				}
				if (ch === quote) {
					quote = null;
				}
				continue;
			}
			if (ch === '"' || ch === "'") {
				quote = ch;
				continue;
			}
			if (ch === '\\' && i + 1 < value.length) {
				i++;
				continue;
			}
		}
		return quote === null;
	}

	function syncAgentArgsIfAuto(
		targetAgent = agentId,
		targetModel = modelId,
		targetThinking = thinkingLevel
	) {
		if (agentArgsAutoSync) {
			agentArgs = buildDefaultAgentArgs(targetAgent, targetModel, targetThinking);
		}
	}

	function resetAgentArgsToSelectedModel() {
		agentArgs = buildDefaultAgentArgs(agentId, modelId, thinkingLevel);
		agentArgsAutoSync = true;
		formError = '';
	}

	function defaultThinkingForModel(mId: string, list: OrcaCommitAIModel[], savedThinkingMap: Record<string, string>): string {
		const saved = savedThinkingMap[mId];
		const found = list.find((m) => m.id === mId);
		if (saved && (!found?.thinkingLevels?.length || found.thinkingLevels.some((l) => l.id === saved))) {
			return saved;
		}
		return found?.defaultThinkingLevel || '';
	}

	async function loadAgentModels(targetAgent: string, forceRefresh = false) {
		if (!targetAgent || targetAgent === 'custom') {
			models = [];
			modelId = '';
			thinkingLevel = '';
			modelsError = '';
			modelsLiveDiscovered = false;
			modelsSupportsDiscovery = false;
			syncAgentArgsIfAuto(targetAgent, '', '');
			return;
		}
		modelsLoading = true;
		modelsError = '';
		try {
			const res = await api.getOrcaCommitAIModels(targetAgent, forceRefresh);
			if (agentId !== targetAgent) return;
			models = res.models ?? [];
			modelsLiveDiscovered = res.live_discovered;
			modelsSupportsDiscovery = res.supports_discovery;
			modelsError = res.discovery_error || '';

			const currentChoice =
				modelId && (forceRefresh || models.some((m) => m.id === modelId))
					? modelId
					: res.selected_model || res.default_model_id || models[0]?.id || '';
			modelId = currentChoice;
			const savedThinking = currentChoice === res.selected_model ? res.thinking_level : '';
			thinkingLevel =
				savedThinking ||
				defaultThinkingForModel(currentChoice, models, config?.selected_thinking_by_model ?? {});
			syncAgentArgsIfAuto(targetAgent, modelId, thinkingLevel);
		} catch (err) {
			if (agentId !== targetAgent) return;
			modelsError = err instanceof Error ? err.message : t('workspace.commitAi.modelsLoadFailed');
		} finally {
			if (agentId === targetAgent) {
				modelsLoading = false;
			}
		}
	}

	function handleAgentChange(nextAgent: string) {
		if (nextAgent === 'custom') {
			models = [];
			modelId = '';
			thinkingLevel = '';
			modelsError = '';
			modelsLiveDiscovered = false;
			modelsSupportsDiscovery = false;
			syncAgentArgsIfAuto('custom', '', '');
			return;
		}
		const cachedList = config?.cached_models_by_agent?.[nextAgent] ?? [];
		models = cachedList;
		const savedForAgent = config?.selected_models_by_agent?.[nextAgent] || cachedList[0]?.id || '';
		modelId = savedForAgent;
		thinkingLevel = defaultThinkingForModel(
			savedForAgent,
			cachedList,
			config?.selected_thinking_by_model ?? {}
		);
		updateDefaultTuiAgent = Boolean(
			config?.available_agents.find((a) => a.id === nextAgent)?.supports_tui
		);
		syncAgentArgsIfAuto(nextAgent, modelId, thinkingLevel);
		void loadAgentModels(nextAgent, false);
	}

	function handleModelChange(nextModel: string) {
		thinkingLevel = defaultThinkingForModel(
			nextModel,
			models,
			config?.selected_thinking_by_model ?? {}
		);
		syncAgentArgsIfAuto(agentId, nextModel, thinkingLevel);
	}

	function handleThinkingChange(nextThinking: string) {
		syncAgentArgsIfAuto(agentId, modelId, nextThinking);
	}

	async function loadConfig() {
		loading = true;
		loadError = '';
		formError = '';
		customCmdTouched = false;
		try {
			const res = await api.getOrcaCommitAI();
			config = res;
			enabled = res.enabled;
			agentId = res.agent_id || 'opencode';
			customAgentCommand = res.custom_agent_command || '';
			customPrompt = res.custom_prompt || '';
			updateDefaultTuiAgent = agentId !== 'custom';
			clearRepoOverrides = true;

			if (agentId !== 'custom') {
				const cachedList = res.cached_models_by_agent?.[agentId] ?? [];
				models = cachedList;
				modelId = res.model_id || res.selected_models_by_agent?.[agentId] || cachedList[0]?.id || '';
				thinkingLevel =
					res.thinking_level ||
					defaultThinkingForModel(modelId, cachedList, res.selected_thinking_by_model ?? {});
				const autoArgs = buildDefaultAgentArgs(agentId, modelId, thinkingLevel);
				const savedArgs = (res.agent_args || '').trim();
				agentArgs = savedArgs || autoArgs;
				agentArgsAutoSync = savedArgs === '' || savedArgs === autoArgs;
				void loadAgentModels(agentId, false);
			} else {
				models = [];
				modelId = '';
				thinkingLevel = '';
				agentArgs = (res.agent_args || '').trim();
				agentArgsAutoSync = agentArgs === '';
			}
		} catch (err) {
			loadError = err instanceof Error ? err.message : t('workspace.commitAi.loadErrorFallback');
		} finally {
			loading = false;
		}
	}

	async function loadSyncStatus() {
		try {
			syncStatus = await api.getOrcaCommitAISync();
		} catch {
			// Sync status is informational only
		}
	}

	async function handleApplySync() {
		syncing = true;
		formError = '';
		try {
			const res = await api.applyOrcaCommitAISync();
			syncStatus = res.sync ?? (await api.getOrcaCommitAISync());
			await loadConfig();
			if (config) {
				onSaved?.(config);
			}
			if (syncStatus?.in_sync) {
				toast.success(res.message || t('workspace.commitAi.syncSuccessFallback'));
			} else {
				toast.error(res.message || t('workspace.commitAi.syncFailFallback'));
			}
		} catch (err) {
			formError = err instanceof Error ? err.message : t('workspace.commitAi.syncErrorFallback');
			toast.error(formError);
		} finally {
			syncing = false;
		}
	}

	$effect(() => {
		if (!open) return;
		void loadConfig();
		void loadSyncStatus();
		const timer = setInterval(() => void loadSyncStatus(), 10000);
		return () => clearInterval(timer);
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (saving || loading || !config?.db_exists) return;
		formError = '';

		if (agentId === 'custom' && customAgentCommand.trim() === '') {
			customCmdTouched = true;
			formError = t('workspace.commitAi.customCmdSaveRequired');
			customCmdInputRef?.focus();
			return;
		}
		if (!hasBalancedAgentArgQuotes(agentArgs.trim())) {
			formError = t('workspace.commitAi.unbalancedQuotes');
			agentArgsInputRef?.focus();
			return;
		}

		saving = true;
		try {
			const res = await api.saveOrcaCommitAI({
				enabled,
				agent_id: agentId,
				model_id: agentId === 'custom' ? '' : modelId,
				thinking_level: agentId === 'custom' ? '' : thinkingLevel,
				agent_args: agentArgs.trim(),
				discovered_models:
					agentId === 'custom' ? [] : models.filter((m) => m.source !== 'seed'),
				custom_agent_command: customAgentCommand.trim(),
				custom_prompt: customPrompt,
				update_default_tui_agent: Boolean(selectedAgent?.supports_tui && updateDefaultTuiAgent),
				clear_repo_overrides: clearRepoOverrides
			});
			config = res.config;
			syncStatus = res.sync ?? null;
			if (syncStatus && !syncStatus.in_sync) {
				formError = res.message || t('workspace.commitAi.saveNotSynced');
				toast.error(formError);
				await loadConfig();
				return;
			}
			toast.success(res.message || t('workspace.commitAi.saveSuccess'));
			onSaved?.(res.config);
			open = false;
		} catch (err) {
			formError = err instanceof Error ? err.message : t('workspace.commitAi.saveErrorFallback');
		} finally {
			saving = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-0 overflow-hidden p-4 sm:max-w-2xl sm:p-6">
		<Dialog.Header class="shrink-0 border-b border-border/40 pb-3">
			<div class="flex items-center justify-between gap-2 pr-6">
				<Dialog.Title id="orca-ai-dialog-title" class="flex items-center gap-2">
					<SparklesIcon class="size-4 text-primary" />
					{t('workspace.commitAi.title')}
					<Badge variant={syncBadge.variant} class="text-[10px]">{syncBadge.label}</Badge>
				</Dialog.Title>
				<Button
					type="button"
					variant="ghost"
					size="icon-sm"
					disabled={loading || saving}
					onclick={() => loadConfig()}
					aria-label={t('workspace.commitAi.reloadAria')}
				>
					<RefreshCwIcon class="size-4 {loading ? 'animate-spin' : ''}" />
				</Button>
			</div>
			<Dialog.Description>
				{t('workspace.commitAi.description')}
			</Dialog.Description>
		</Dialog.Header>

		{#if loading}
			<div class="flex flex-col gap-4 py-4">
				<Skeleton class="h-12 w-full" />
				<Skeleton class="h-20 w-full" />
				<Skeleton class="h-32 w-full" />
			</div>
		{:else if loadError}
			<div class="py-4">
				<Alert.Root variant="destructive" role="alert">
					<AlertCircleIcon />
					<Alert.Title>{t('workspace.commitAi.loadFailedTitle')}</Alert.Title>
					<Alert.Description>{loadError}</Alert.Description>
				</Alert.Root>
			</div>
			<Dialog.Footer class="shrink-0 border-t border-border/40 pt-3">
				<Button type="button" variant="outline" onclick={() => loadConfig()}>{t('workspace.commitAi.retryBtn')}</Button>
				<Button type="button" variant="ghost" onclick={() => (open = false)}>{t('common.actions.close')}</Button>
			</Dialog.Footer>
		{:else if config}
			<form
				onsubmit={handleSubmit}
				class="flex min-h-0 flex-1 flex-col"
				aria-labelledby="orca-ai-dialog-title"
				novalidate
			>
				<div class="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto overscroll-contain py-4 pr-1">
				{#if formError}
					<Alert.Root variant="destructive" role="alert">
						<AlertCircleIcon />
						<Alert.Title>{t('workspace.commitAi.saveFailedTitle')}</Alert.Title>
						<Alert.Description>{formError}</Alert.Description>
					</Alert.Root>
				{/if}

				{#if syncStatus?.managed}
					<div class="flex flex-col gap-2 rounded-lg border bg-muted/20 p-3 text-sm">
						<div class="flex flex-wrap items-center justify-between gap-2">
							<div class="flex items-center gap-2">
								<span class="font-medium">{t('workspace.commitAi.syncStatusTitle')}</span>
								<Badge variant={syncBadge.variant} class="text-[10px]">{syncBadge.label}</Badge>
							</div>
							<Button
								type="button"
								variant="outline"
								size="sm"
								disabled={syncing || loading || saving}
								onclick={handleApplySync}
							>
								{#if syncing}
									<Spinner data-icon="inline-start" />
									{t('common.actions.applying')}
								{:else}
									{t('workspace.commitAi.reapplyBtn')}
								{/if}
							</Button>
						</div>
						{#if syncStatus.last_applied_at_ms}
							<p class="text-muted-foreground text-xs">
								{t('workspace.commitAi.lastApplied', {
									time: i18n.formatDateTime(syncStatus.last_applied_at_ms),
									rev: syncStatus.settings_revision
								})}
							</p>
						{/if}
						{#if syncStatus.drift?.length}
							<p class="text-destructive text-xs">
								{t('workspace.commitAi.driftDiff', { list: syncStatus.drift.join(', ') })}
							</p>
						{/if}
						{#if syncStatus.orca_desktop_open}
							<p class="text-warning text-xs">
								{t('workspace.commitAi.desktopOpenWarn')}
							</p>
						{/if}
						{#if syncStatus.cloud_linked}
							<p class="text-muted-foreground text-xs">
								{t('workspace.commitAi.cloudLinkedNote')}
							</p>
						{/if}
						{#if syncStatus.last_error}
							<p class="text-destructive text-xs">{syncStatus.last_error}</p>
						{/if}
					</div>
				{/if}

				{#if !config.db_exists}
					<Alert.Root variant="destructive" role="alert">
						<AlertCircleIcon />
						<Alert.Title>{t('workspace.commitAi.dbMissingTitle')}</Alert.Title>
						<Alert.Description>
							{t('workspace.commitAi.dbMissingDesc', { path: config.db_path })}
						</Alert.Description>
					</Alert.Root>
				{:else if config.orca_running}
					<Alert.Root>
						<InfoIcon />
						<Alert.Title>{t('workspace.commitAi.orcaRunningTitle')}</Alert.Title>
						<Alert.Description>
							{#if config.orca_mode === 'headless'}
								{t('workspace.commitAi.orcaRunningHeadless')}
								<span class="mt-1 block text-xs text-muted-foreground">
									{t('workspace.commitAi.orcaRunningWebClientNote')}
								</span>
							{:else}
								{t('workspace.commitAi.orcaRunningDesktop')}
							{/if}
						</Alert.Description>
					</Alert.Root>
				{/if}

				<!-- Active switch -->
				<div class="flex items-center justify-between gap-4 rounded-lg border bg-muted/30 p-3">
					<div class="flex flex-col gap-0.5">
						<Field.FieldLabel for="orca-ai-enabled" class="cursor-pointer font-medium">
							{t('workspace.commitAi.enableLabel')}
						</Field.FieldLabel>
						<p class="text-xs text-muted-foreground">
							{t('workspace.commitAi.activeProfile')} <span class="font-mono font-medium">{config.profile_id}</span>
						</p>
					</div>
					<Switch id="orca-ai-enabled" bind:checked={enabled} />
				</div>

				<!-- Agent Selection -->
				<Field.FieldSet>
					<Field.FieldLegend variant="label">{t('workspace.commitAi.agentLegend')}</Field.FieldLegend>
					<Field.FieldGroup>
						<Field.Field>
							<Field.FieldLabel for="orca-ai-agent">{t('workspace.commitAi.agentLabel')}</Field.FieldLabel>
							<NativeSelect.Root
								id="orca-ai-agent"
								name="agent_id"
								bind:value={agentId}
								onchange={(e) => handleAgentChange((e.currentTarget as HTMLSelectElement).value)}
								class="w-full"
								aria-describedby="orca-ai-agent-help"
							>
								{#each config.available_agents as agent (agent.id)}
									<NativeSelect.Option value={agent.id}>
										{agent.label} ({agent.id === 'custom'
											? t('workspace.commitAi.customCmdTag')
											: agent.installed
												? t('common.status.installed')
												: t('common.status.notInstalled')})
									</NativeSelect.Option>
								{/each}
							</NativeSelect.Root>
							<div id="orca-ai-agent-help" class="flex flex-wrap items-center gap-2 pt-1 text-xs text-muted-foreground">
								{#if selectedAgent}
									{#if selectedAgent.id === 'custom'}
										<Badge variant="outline">{t('workspace.commitAi.customCmdModeBadge')}</Badge>
									{:else if selectedAgent.installed}
										<Badge variant="secondary">{t('common.status.installed')}</Badge>
										<span class="truncate font-mono">{selectedAgent.path}</span>
									{:else}
										<Badge variant="destructive">{t('workspace.commitAi.binaryMissingBadge', { binary: selectedAgent.binary ?? '' })}</Badge>
										<span>{t('workspace.commitAi.binaryMissingHint')}</span>
									{/if}
								{/if}
							</div>
						</Field.Field>

						{#if agentId !== 'custom'}
							<Field.Field>
								<div class="flex items-center justify-between gap-2">
									<Field.FieldLabel for="orca-ai-model">{t('workspace.commitAi.aiModelLabel')}</Field.FieldLabel>
									<Button
										type="button"
										variant="ghost"
										size="xs"
										disabled={modelsLoading || saving}
										onclick={() => loadAgentModels(agentId, true)}
									>
										<RefreshCwIcon class="size-3.5 {modelsLoading ? 'animate-spin' : ''}" data-icon="inline-start" />
										{modelsLoading ? t('workspace.commitAi.loadingModels') : t('workspace.commitAi.reloadModels')}
									</Button>
								</div>
								<NativeSelect.Root
									id="orca-ai-model"
									name="model_id"
									bind:value={modelId}
									disabled={modelsLoading && models.length === 0}
									onchange={(e) => handleModelChange((e.currentTarget as HTMLSelectElement).value)}
									class="w-full"
									aria-describedby="orca-ai-model-help"
								>
									{#if hasMissingSelectedModel}
										<NativeSelect.Option value={modelId}>
											{modelId} {t('workspace.commitAi.savedTag')}
										</NativeSelect.Option>
									{/if}
									{#each models as m (m.id)}
										<NativeSelect.Option value={m.id}>
											{m.label}{m.id !== m.label ? ` — ${m.id}` : ''}{m.description ? ` (${m.description})` : ''}
										</NativeSelect.Option>
									{/each}
								</NativeSelect.Root>
								<div id="orca-ai-model-help" class="flex flex-wrap items-center gap-2 pt-1 text-xs text-muted-foreground">
									{#if modelsLoading}
										<Badge variant="outline">{t('workspace.commitAi.fetchingCliModels')}</Badge>
									{:else if modelsLiveDiscovered}
										<Badge variant="secondary">{t('workspace.commitAi.liveCliBadge', { count: models.length })}</Badge>
									{:else if models.length > 0}
										<Badge variant="outline">{t('workspace.commitAi.availableModelsBadge', { count: models.length })}</Badge>
									{/if}
									{#if !modelsSupportsDiscovery && models.length > 0}
										<span>{t('workspace.commitAi.builtinModelsNote')}</span>
									{/if}
									{#if modelsError}
										<span class="text-amber-600 dark:text-amber-400">
											{t('workspace.commitAi.probeErrorNote', { error: modelsError })}
										</span>
									{/if}
								</div>
							</Field.Field>

							{#if selectedModelObj?.thinkingLevels && selectedModelObj.thinkingLevels.length > 0}
								<Field.Field>
									<Field.FieldLabel for="orca-ai-thinking">{t('workspace.commitAi.thinkingLabel')}</Field.FieldLabel>
									<NativeSelect.Root
										id="orca-ai-thinking"
										name="thinking_level"
										bind:value={thinkingLevel}
										onchange={(e) => handleThinkingChange((e.currentTarget as HTMLSelectElement).value)}
										class="w-full"
										aria-describedby="orca-ai-thinking-help"
									>
										<NativeSelect.Option value="">
											{t('workspace.commitAi.thinkingDefaultOption', {
												level: selectedModelObj.defaultThinkingLevel || 'default'
											})}
										</NativeSelect.Option>
										{#each selectedModelObj.thinkingLevels as lvl (lvl.id)}
											<NativeSelect.Option value={lvl.id}>{lvl.label}</NativeSelect.Option>
										{/each}
									</NativeSelect.Root>
									<Field.FieldDescription id="orca-ai-thinking-help">
										{t('workspace.commitAi.thinkingHelp')}
									</Field.FieldDescription>
								</Field.Field>
							{/if}
						{/if}

						{#if agentId === 'custom'}
							<Field.Field>
								<Field.FieldLabel for="orca-ai-custom-cmd">
									{t('workspace.commitAi.customCmdLabel')} <span aria-hidden="true">*</span>
								</Field.FieldLabel>
								<Input
									id="orca-ai-custom-cmd"
									name="custom_agent_command"
									bind:ref={customCmdInputRef}
									bind:value={customAgentCommand}
									required
									autocorrect="off"
									autocapitalize="none"
									spellcheck={false}
									placeholder={t('workspace.commitAi.customCmdPlaceholder')}
									aria-invalid={customCmdError ? 'true' : undefined}
									aria-describedby={customCmdError
										? 'orca-ai-custom-cmd-help orca-ai-custom-cmd-err'
										: 'orca-ai-custom-cmd-help'}
									onblur={() => (customCmdTouched = true)}
									oninput={() => {
										if (customAgentCommand.trim() !== '') {
											formError = '';
										}
									}}
								/>
								<Field.FieldDescription id="orca-ai-custom-cmd-help">
									{t('workspace.commitAi.customCmdHelp')}
								</Field.FieldDescription>
								{#if customCmdError}
									<Field.FieldError id="orca-ai-custom-cmd-err">
										{customCmdError}
									</Field.FieldError>
								{/if}
							</Field.Field>
						{/if}

						{#if selectedAgent?.supports_tui}
							<div class="flex items-start gap-2.5 pt-1">
								<Checkbox id="orca-ai-default-tui" bind:checked={updateDefaultTuiAgent} />
								<div class="grid gap-0.5 leading-none">
									<Field.FieldLabel for="orca-ai-default-tui" class="cursor-pointer font-normal">
										{t('workspace.commitAi.defaultTuiLabel')}
									</Field.FieldLabel>
									<p class="text-xs text-muted-foreground">
										{t('workspace.commitAi.currentDefaultTui')} <span class="font-mono">{config.default_tui_agent || '-'}</span>
									</p>
								</div>
							</div>
						{/if}
					</Field.FieldGroup>
				</Field.FieldSet>

				<Separator />

				<!-- Prompt Template -->
				<Field.FieldSet>
					<Field.FieldLegend variant="label">{t('workspace.commitAi.templateLegend')}</Field.FieldLegend>
					<Field.FieldGroup>
						<div class="flex flex-wrap items-center gap-1.5">
							<span class="mr-1 text-xs font-medium text-muted-foreground">{t('workspace.commitAi.quickPresets')}</span>
							{#each config.template_presets as preset (preset.id)}
								<Button
									type="button"
									size="xs"
									variant={activePresetId === preset.id ? 'default' : 'outline'}
									onclick={() => (customPrompt = preset.prompt)}
								>
									{preset.label}
								</Button>
							{/each}
							{#if activePresetId === 'custom'}
								<Badge variant="outline">{t('workspace.commitAi.customPresetBadge')}</Badge>
							{/if}
						</div>

						<Field.Field>
							<Field.FieldLabel for="orca-ai-prompt">{t('workspace.commitAi.promptLabel')}</Field.FieldLabel>
							<Textarea
								id="orca-ai-prompt"
								name="custom_prompt"
								rows={4}
								bind:value={customPrompt}
								aria-describedby="orca-ai-prompt-help"
								placeholder={t('workspace.commitAi.promptPlaceholder', { basePrompt: '{basePrompt}' })}
							/>
							<Field.FieldDescription id="orca-ai-prompt-help">
								{t('workspace.commitAi.promptHelp', { basePrompt: '{basePrompt}' })}
							</Field.FieldDescription>
						</Field.Field>

						<Field.Field>
							<div class="flex items-center justify-between gap-2">
								<Field.FieldLabel for="orca-ai-agent-args">
									{t('workspace.commitAi.agentArgsLabel')} (<code class="font-mono">agentArgs</code>)
								</Field.FieldLabel>
								{#if defaultModelArgs && agentArgs.trim() !== defaultModelArgs}
									<Button
										type="button"
										variant="ghost"
										size="xs"
										disabled={saving}
										onclick={resetAgentArgsToSelectedModel}
									>
										{t('workspace.commitAi.useSelectedModelBtn')}
									</Button>
								{/if}
							</div>
							<Input
								id="orca-ai-agent-args"
								name="agent_args"
								bind:ref={agentArgsInputRef}
								bind:value={agentArgs}
								autocorrect="off"
								autocapitalize="none"
								spellcheck={false}
								class="font-mono text-xs"
								aria-describedby="orca-ai-agent-args-help"
								placeholder={defaultModelArgs || t('workspace.commitAi.agentArgsPlaceholder')}
								oninput={() => {
									agentArgsAutoSync = false;
									if (hasBalancedAgentArgQuotes(agentArgs.trim())) {
										formError = '';
									}
								}}
							/>
							<Field.FieldDescription id="orca-ai-agent-args-help">
								{t('workspace.commitAi.agentArgsHelpPrefix')}
								{#if defaultModelArgs}
									{t('workspace.commitAi.agentArgsAutoFollow', { args: defaultModelArgs })}
								{:else if agentId === 'dsh'}
									{t('workspace.commitAi.agentArgsNoModel')}
								{:else if agentId === 'custom'}
									{t('workspace.commitAi.agentArgsCustomHint')}
								{:else}
									{t('workspace.commitAi.agentArgsDefaultHint')}
								{/if}
							</Field.FieldDescription>
						</Field.Field>

						<div class="rounded-md border bg-muted/40 p-2.5 text-xs">
							<div class="mb-1 font-medium text-muted-foreground">
								{t('workspace.commitAi.previewLabel')}
							</div>
							<pre class="max-h-24 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px] text-foreground">{previewCommandTemplate}</pre>
						</div>
					</Field.FieldGroup>
				</Field.FieldSet>

				<Separator />

				<!-- Repo override sync -->
				<div class="flex items-start gap-2.5 rounded-lg border bg-muted/20 p-3">
					<Checkbox id="orca-ai-clear-overrides" bind:checked={clearRepoOverrides} />
					<div class="grid gap-1 leading-snug">
						<Field.FieldLabel for="orca-ai-clear-overrides" class="cursor-pointer font-medium">
							{t('workspace.commitAi.applyAllReposLabel', { count: config.repo_count })}
						</Field.FieldLabel>
						<p class="text-xs text-muted-foreground">
							{t('workspace.commitAi.applyAllReposDesc')}
							{#if config.repo_override_count > 0}
								<span class="font-medium text-amber-600 dark:text-amber-400">
									{t('workspace.commitAi.repoOverrideWarn', { count: config.repo_override_count })}
								</span>
							{/if}
						</p>
					</div>
				</div>
				</div>

				<Dialog.Footer class="shrink-0 gap-2 border-t border-border/40 pt-3 pb-[env(safe-area-inset-bottom,0px)] sm:justify-end sm:pb-0">
					<Button type="button" variant="outline" disabled={saving} onclick={() => (open = false)}>
						{t('common.actions.cancel')}
					</Button>
					<Button type="submit" disabled={saving || !config.db_exists}>
						{#if saving}
							<Spinner data-icon="inline-start" />
							{t('workspace.commitAi.savingToOrca')}
						{:else}
							{t('workspace.commitAi.saveConfigBtn')}
						{/if}
					</Button>
				</Dialog.Footer>
			</form>
		{/if}
	</Dialog.Content>
</Dialog.Root>
