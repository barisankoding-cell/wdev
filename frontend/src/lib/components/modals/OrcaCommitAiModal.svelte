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

	const syncBadge = $derived.by((): { label: string; variant: BadgeVariant } => {
		if (!syncStatus?.managed) return { label: 'Belum dikelola', variant: 'outline' };
		switch (syncStatus.state) {
			case 'synced':
				return { label: 'Sinkron', variant: 'secondary' };
			case 'drift':
				return { label: 'Perlu Sinkron', variant: 'destructive' };
			case 'blocked':
				return { label: 'Orca Aktif', variant: 'destructive' };
			case 'conflict':
				return { label: 'Konflik', variant: 'destructive' };
			case 'error':
				return { label: 'Error', variant: 'destructive' };
			default:
				return { label: 'Belum dikelola', variant: 'outline' };
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
			? 'Perintah custom agent wajib diisi saat memilih Custom Agent Command.'
			: '';
	});

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
			thinkingLevel =
				res.thinking_level ||
				defaultThinkingForModel(currentChoice, models, config?.selected_thinking_by_model ?? {});
		} catch (err) {
			if (agentId !== targetAgent) return;
			modelsError = err instanceof Error ? err.message : 'Gagal memuat daftar model agent';
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
		void loadAgentModels(nextAgent, false);
	}

	function handleModelChange(nextModel: string) {
		thinkingLevel = defaultThinkingForModel(
			nextModel,
			models,
			config?.selected_thinking_by_model ?? {}
		);
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
				void loadAgentModels(agentId, false);
			} else {
				models = [];
				modelId = '';
				thinkingLevel = '';
			}
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Gagal memuat konfigurasi Orca Commit AI';
		} finally {
			loading = false;
		}
	}

	async function loadSyncStatus() {
		try {
			syncStatus = await api.getOrcaCommitAISync();
		} catch {
			// Status sinkron bersifat informatif; jangan mengganggu form utama.
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
				toast.success(res.message || 'Orca Commit AI sudah sinkron.');
			} else {
				toast.error(res.message || 'Sinkronisasi belum berhasil.');
			}
		} catch (err) {
			formError = err instanceof Error ? err.message : 'Gagal menerapkan sinkronisasi';
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
			formError = 'Perintah custom agent wajib diisi sebelum menyimpan.';
			customCmdInputRef?.focus();
			return;
		}

		saving = true;
		try {
			const res = await api.saveOrcaCommitAI({
				enabled,
				agent_id: agentId,
				model_id: agentId === 'custom' ? '' : modelId,
				thinking_level: agentId === 'custom' ? '' : thinkingLevel,
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
				formError = res.message || 'Konfigurasi disimpan, tetapi Orca belum sinkron.';
				toast.error(formError);
				await loadConfig();
				return;
			}
			toast.success(res.message || 'Konfigurasi Orca Commit AI berhasil disimpan dan disinkronkan.');
			onSaved?.(res.config);
			open = false;
		} catch (err) {
			formError = err instanceof Error ? err.message : 'Gagal menyimpan konfigurasi Orca Commit AI';
		} finally {
			saving = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
		<Dialog.Header>
			<div class="flex items-center justify-between gap-2 pr-6">
				<Dialog.Title id="orca-ai-dialog-title" class="flex items-center gap-2">
					<SparklesIcon class="size-4 text-primary" />
					Konfigurasi Orca Commit AI
					<Badge variant={syncBadge.variant} class="text-[10px]">{syncBadge.label}</Badge>
				</Dialog.Title>
				<Button
					type="button"
					variant="ghost"
					size="icon-sm"
					disabled={loading || saving}
					onclick={() => loadConfig()}
					aria-label="Muat ulang konfigurasi Commit AI"
				>
					<RefreshCwIcon class="size-4 {loading ? 'animate-spin' : ''}" />
				</Button>
			</div>
			<Dialog.Description>
				Pilih agent AI aktif dan template prompt Git Commit Generator yang disimpan langsung ke database profil Orca ADE.
			</Dialog.Description>
		</Dialog.Header>

		{#if loading}
			<div class="flex flex-col gap-4 py-2">
				<Skeleton class="h-12 w-full" />
				<Skeleton class="h-20 w-full" />
				<Skeleton class="h-32 w-full" />
			</div>
		{:else if loadError}
			<Alert.Root variant="destructive" role="alert">
				<AlertCircleIcon />
				<Alert.Title>Gagal Memuat Konfigurasi</Alert.Title>
				<Alert.Description>{loadError}</Alert.Description>
			</Alert.Root>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => loadConfig()}>Coba Lagi</Button>
				<Button type="button" variant="ghost" onclick={() => (open = false)}>Tutup</Button>
			</Dialog.Footer>
		{:else if config}
			<form
				onsubmit={handleSubmit}
				class="flex flex-col gap-5"
				aria-labelledby="orca-ai-dialog-title"
				novalidate
			>
				{#if formError}
					<Alert.Root variant="destructive" role="alert">
						<AlertCircleIcon />
						<Alert.Title>Penyimpanan Gagal</Alert.Title>
						<Alert.Description>{formError}</Alert.Description>
					</Alert.Root>
				{/if}

				{#if syncStatus?.managed}
					<div class="flex flex-col gap-2 rounded-lg border bg-muted/20 p-3 text-sm">
						<div class="flex flex-wrap items-center justify-between gap-2">
							<div class="flex items-center gap-2">
								<span class="font-medium">Status Sinkronisasi</span>
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
									Menerapkan...
								{:else}
									Terapkan Ulang
								{/if}
							</Button>
						</div>
						{#if syncStatus.last_applied_at_ms}
							<p class="text-muted-foreground text-xs">
								Terakhir diterapkan: {new Date(syncStatus.last_applied_at_ms).toLocaleString('id-ID')}
								· revisi Orca #{syncStatus.settings_revision}
							</p>
						{/if}
						{#if syncStatus.drift?.length}
							<p class="text-destructive text-xs">Perbedaan: {syncStatus.drift.join(', ')}</p>
						{/if}
						{#if syncStatus.orca_desktop_open}
							<p class="text-warning text-xs">
								Orca Desktop sedang terbuka. Tutup agar wdev bisa menulis konfigurasi tanpa ditimpa.
							</p>
						{/if}
						{#if syncStatus.cloud_linked}
							<p class="text-muted-foreground text-xs">
								Profil Orca terhubung cloud — pengaturan dapat ditimpa saat sinkronisasi cloud. wdev akan menerapkan ulang otomatis setiap 30 detik.
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
						<Alert.Title>Database Profil Orca Belum Tersedia</Alert.Title>
						<Alert.Description>
							File <code class="font-mono text-xs">{config.db_path}</code> belum dibuat. Jalankan layanan Orca ADE terlebih dahulu agar profil diinisialisasi.
						</Alert.Description>
					</Alert.Root>
				{:else if config.orca_running}
					<Alert.Root>
						<InfoIcon />
						<Alert.Title>Layanan Orca Sedang Aktif</Alert.Title>
						<Alert.Description>
							{#if config.orca_mode === 'headless'}
								Saat menyimpan, layanan Orca Headless akan dihentikan sebentar lalu dimulai ulang secara otomatis agar pengaturan tidak tertimpa oleh cache memori.
								<span class="mt-1 block text-xs text-muted-foreground">
									Catatan Web Client: jika sebelumnya Anda mengubah setelan AI di UI browser Orca, muat ulang tab Web Client (atau bersihkan cache <code class="font-mono">orca.web.settings.v1</code> di browser) karena Web Client memprioritaskan cache browser.
								</span>
							{:else}
								Orca sedang berjalan. Pastikan aplikasi Orca Desktop tidak sedang menimpa database profil saat menyimpan.
							{/if}
						</Alert.Description>
					</Alert.Root>
				{/if}

				<!-- Status aktif -->
				<div class="flex items-center justify-between gap-4 rounded-lg border bg-muted/30 p-3">
					<div class="flex flex-col gap-0.5">
						<Field.FieldLabel for="orca-ai-enabled" class="cursor-pointer font-medium">
							Aktifkan AI Commit Message Generator
						</Field.FieldLabel>
						<p class="text-xs text-muted-foreground">
							Profil aktif: <span class="font-mono font-medium">{config.profile_id}</span>
						</p>
					</div>
					<Switch id="orca-ai-enabled" bind:checked={enabled} />
				</div>

				<!-- Agent Selection -->
				<Field.FieldSet>
					<Field.FieldLegend variant="label">Agent AI Aktif</Field.FieldLegend>
					<Field.FieldGroup>
						<Field.Field>
							<Field.FieldLabel for="orca-ai-agent">Agent Generator Commit</Field.FieldLabel>
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
											? 'Perintah Kustom'
											: agent.installed
												? 'Terpasang'
												: 'Belum terpasang'})
									</NativeSelect.Option>
								{/each}
							</NativeSelect.Root>
							<div id="orca-ai-agent-help" class="flex flex-wrap items-center gap-2 pt-1 text-xs text-muted-foreground">
								{#if selectedAgent}
									{#if selectedAgent.id === 'custom'}
										<Badge variant="outline">Mode Perintah Kustom</Badge>
									{:else if selectedAgent.installed}
										<Badge variant="secondary">Terpasang</Badge>
										<span class="truncate font-mono">{selectedAgent.path}</span>
									{:else}
										<Badge variant="destructive">Biner "{selectedAgent.binary}" tidak ditemukan</Badge>
										<span>Pastikan CLI terpasang agar Orca dapat memanggilnya.</span>
									{/if}
								{/if}
							</div>
						</Field.Field>

						{#if agentId !== 'custom'}
							<Field.Field>
								<div class="flex items-center justify-between gap-2">
									<Field.FieldLabel for="orca-ai-model">Model AI</Field.FieldLabel>
									<Button
										type="button"
										variant="ghost"
										size="xs"
										disabled={modelsLoading || saving}
										onclick={() => loadAgentModels(agentId, true)}
									>
										<RefreshCwIcon class="size-3.5 {modelsLoading ? 'animate-spin' : ''}" data-icon="inline-start" />
										{modelsLoading ? 'Memuat model...' : 'Muat Ulang Model'}
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
											{modelId} (tersimpan)
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
										<Badge variant="outline">Mengambil daftar model dari CLI...</Badge>
									{:else if modelsLiveDiscovered}
										<Badge variant="secondary">Live CLI ({models.length} model)</Badge>
									{:else if models.length > 0}
										<Badge variant="outline">{models.length} model tersedia</Badge>
									{/if}
									{#if !modelsSupportsDiscovery && models.length > 0}
										<span>Daftar model bawaan agent</span>
									{/if}
									{#if modelsError}
										<span class="text-amber-600 dark:text-amber-400">
											Probe CLI: {modelsError} (menampilkan daftar fallback)
										</span>
									{/if}
								</div>
							</Field.Field>

							{#if selectedModelObj?.thinkingLevels && selectedModelObj.thinkingLevels.length > 0}
								<Field.Field>
									<Field.FieldLabel for="orca-ai-thinking">Tingkat Reasoning / Thinking</Field.FieldLabel>
									<NativeSelect.Root
										id="orca-ai-thinking"
										name="thinking_level"
										bind:value={thinkingLevel}
										class="w-full"
										aria-describedby="orca-ai-thinking-help"
									>
										<NativeSelect.Option value="">
											Bawaan model ({selectedModelObj.defaultThinkingLevel || 'default'})
										</NativeSelect.Option>
										{#each selectedModelObj.thinkingLevels as lvl (lvl.id)}
											<NativeSelect.Option value={lvl.id}>{lvl.label}</NativeSelect.Option>
										{/each}
									</NativeSelect.Root>
									<Field.FieldDescription id="orca-ai-thinking-help">
										Mengatur effort / reasoning level yang dikirim ke agent saat membuat pesan commit.
									</Field.FieldDescription>
								</Field.Field>
							{/if}
						{/if}

						{#if agentId === 'custom'}
							<Field.Field>
								<Field.FieldLabel for="orca-ai-custom-cmd">
									Perintah Custom Agent <span aria-hidden="true">*</span>
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
									placeholder="contoh: llm -m gpt-4o-mini"
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
									Perintah CLI yang membaca prompt dari stdin / argumen untuk menghasilkan pesan commit.
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
										Jadikan juga sebagai Default TUI Agent Orca
									</Field.FieldLabel>
									<p class="text-xs text-muted-foreground">
										Default saat ini: <span class="font-mono">{config.default_tui_agent || '-'}</span>
									</p>
								</div>
							</div>
						{/if}
					</Field.FieldGroup>
				</Field.FieldSet>

				<Separator />

				<!-- Template Prompt -->
				<Field.FieldSet>
					<Field.FieldLegend variant="label">Template Commit Message</Field.FieldLegend>
					<Field.FieldGroup>
						<div class="flex flex-wrap items-center gap-1.5">
							<span class="mr-1 text-xs font-medium text-muted-foreground">Preset Cepat:</span>
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
								<Badge variant="outline">Kustom</Badge>
							{/if}
						</div>

						<Field.Field>
							<Field.FieldLabel for="orca-ai-prompt">Instruksi / Template Prompt</Field.FieldLabel>
							<Textarea
								id="orca-ai-prompt"
								name="custom_prompt"
								rows={4}
								bind:value={customPrompt}
								aria-describedby="orca-ai-prompt-help"
								placeholder={'Kosongkan untuk memakai prompt bawaan Orca ({basePrompt}), atau tulis aturan format commit...'}
							/>
							<Field.FieldDescription id="orca-ai-prompt-help">
								Tulis aturan commit (bahasa, format Conventional Commits, batas panjang judul), atau gunakan placeholder <code class="font-mono">&#123;basePrompt&#125;</code> untuk kontrol penuh.
							</Field.FieldDescription>
						</Field.Field>

						<div class="rounded-md border bg-muted/40 p-2.5 text-xs">
							<div class="mb-1 font-medium text-muted-foreground">
								Pratinjau <code class="font-mono">commandInputTemplate</code> yang disimpan ke Orca:
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
							Terapkan ke semua repositori Orca ({config.repo_count} repositori)
						</Field.FieldLabel>
						<p class="text-xs text-muted-foreground">
							Membersihkan override commit message lama per-repositori agar semua proyek langsung mewarisi pengaturan ini.
							{#if config.repo_override_count > 0}
								<span class="font-medium text-amber-600 dark:text-amber-400">
									Terdeteksi {config.repo_override_count} repositori memiliki override lokal.
								</span>
							{/if}
						</p>
					</div>
				</div>

				<Dialog.Footer class="gap-2 sm:justify-end">
					<Button type="button" variant="outline" disabled={saving} onclick={() => (open = false)}>
						Batal
					</Button>
					<Button type="submit" disabled={saving || !config.db_exists}>
						{#if saving}
							<Spinner data-icon="inline-start" />
							Menyimpan ke Orca...
						{:else}
							Simpan Konfigurasi
						{/if}
					</Button>
				</Dialog.Footer>
			</form>
		{/if}
	</Dialog.Content>
</Dialog.Root>
