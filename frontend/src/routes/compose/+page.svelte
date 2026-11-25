<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { ScrollArea } from '#lib/components/ui/scroll-area/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import CreateComposeModal from '#lib/components/compose/CreateComposeModal.svelte';
	import ExposeComposeModal from '#lib/components/compose/ExposeComposeModal.svelte';
	import DockerConfigPanel, {
		buildDockerConfigPayload,
		createDefaultDockerConfig
	} from '#lib/components/modals/service-config/DockerConfigPanel.svelte';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { cn } from '#lib/utils.js';
	import type {
		ComposeActionPayload,
		ComposeDiskUsageSummary,
		ComposeEngineInfo,
		ComposeProject,
		ComposeTemplateSpec,
		DockerConfig
	} from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import ArrowUpCircleIcon from '@lucide/svelte/icons/arrow-up-circle';
	import CheckIcon from '@lucide/svelte/icons/check';
	import ContainerIcon from '@lucide/svelte/icons/container';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import EyeIcon from '@lucide/svelte/icons/eye';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';
	import FileCodeIcon from '@lucide/svelte/icons/file-code';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import HammerIcon from '@lucide/svelte/icons/hammer';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import PlayIcon from '@lucide/svelte/icons/play';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import RotateCwIcon from '@lucide/svelte/icons/rotate-cw';
	import SearchIcon from '@lucide/svelte/icons/search';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import SquareIcon from '@lucide/svelte/icons/square';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

	let loading = $state(true);
	let refreshing = $state(false);
	let engine = $state<ComposeEngineInfo | null>(null);
	let projects = $state<ComposeProject[]>([]);
	let templates = $state<ComposeTemplateSpec[]>([]);

	let searchQuery = $state('');
	let statusFilter = $state<'all' | 'running' | 'stopped'>('all');
	let selectedName = $state('');
	let detailLoading = $state(false);
	let selectedDetail = $state<ComposeProject | null>(null);
	let activeTab = $state<'services' | 'files' | 'logs'>('services');

	// Profiles selection for selected stack
	let selectedProfiles = $state<string[]>([]);

	// Create / Register Modal
	let createOpen = $state(false);
	let createInitialDir = $state('');

	// Expose to VHost Modal
	let exposeOpen = $state(false);
	let exposePort = $state(8080);
	let exposeService = $state('');

	// Down confirmation dialog (optional --volumes)
	let downOpen = $state(false);
	let downVolumes = $state(false);
	let downOrphans = $state(true);
	let actionPending = $state(false);
	let daemonActionPending = $state(false);

	// Files Editor state
	let filesLoading = $state(false);
	let filesSaving = $state(false);
	let composeFileLabel = $state('compose.yaml');
	let composeContent = $state('');
	let initialComposeContent = $state('');
	let envContent = $state('');
	let initialEnvContent = $state('');
	let hasEnvFile = $state(false);
	let maskEnvValues = $state(true);

	// Resolved JSON Config modal
	let configModalOpen = $state(false);
	let configLoading = $state(false);
	let configJSONText = $state('');

	// Logs state
	let logsLoading = $state(false);
	let logsText = $state('');
	let logServiceFilter = $state('');
	let logTail = $state(200);
	let liveFollow = $state(false);
	let logEventSource: EventSource | null = null;

	// Disk Usage & Prune modal
	let pruneModalOpen = $state(false);
	let dfLoading = $state(false);
	let diskUsage = $state<ComposeDiskUsageSummary | null>(null);

	// Inline Docker Engine & Compose v5 installation + pre-configuration state
	let dockerInstallCfg = $state<DockerConfig>(createDefaultDockerConfig());
	let dockerPreconfigLoaded = $state(false);
	let installSource = $state<'official' | 'distro'>('official');
	let installEnableService = $state(true);
	let savingPreconfig = $state(false);
	let preconfigError = $state('');
	let showPreconfigForm = $state(true);

	const installSourceLabels = $derived<Record<'official' | 'distro', string>>({
		official: t('compose.install.sourceOfficial'),
		distro: t('compose.install.sourceDistro')
	});

	let lastRefreshTick = 0;

	const filteredProjects = $derived.by(() => {
		const q = searchQuery.trim().toLowerCase();
		return projects.filter((p) => {
			if (statusFilter === 'running' && p.status !== 'running' && p.status !== 'partial') {
				return false;
			}
			if (statusFilter === 'stopped' && (p.status === 'running' || p.status === 'partial')) {
				return false;
			}
			if (!q) return true;
			return (
				p.name.toLowerCase().includes(q) ||
				(p.dir_path || '').toLowerCase().includes(q) ||
				(p.description || '').toLowerCase().includes(q)
			);
		});
	});

	const kpiCounts = $derived.by(() => {
		let running = 0;
		let stopped = 0;
		let warnings = 0;
		for (const p of projects) {
			if (p.status === 'running' || p.status === 'partial') running++;
			else stopped++;
			warnings += p.security_warnings?.length ?? 0;
		}
		return { total: projects.length, running, stopped, warnings };
	});

	const filesDirty = $derived(
		composeContent !== initialComposeContent || envContent !== initialEnvContent
	);

	const maskedEnvPreview = $derived.by(() => {
		if (!envContent) return '';
		return envContent
			.split('\n')
			.map((line) => {
				const trimmed = line.trim();
				if (!trimmed || trimmed.startsWith('#') || !line.includes('=')) return line;
				const idx = line.indexOf('=');
				const key = line.slice(0, idx);
				const val = line.slice(idx + 1);
				return val.length > 0 ? `${key}=••••••••` : line;
			})
			.join('\n');
	});

	function statusTone(st: string): 'success' | 'warning' | 'destructive' | 'neutral' {
		switch (st) {
			case 'running':
				return 'success';
			case 'partial':
				return 'warning';
			case 'exited':
			case 'missing':
				return 'destructive';
			default:
				return 'neutral';
		}
	}

	function statusLabel(st: string): string {
		switch (st) {
			case 'running':
				return t('compose.status.running');
			case 'partial':
				return t('compose.status.partial');
			case 'exited':
				return t('compose.status.exited');
			case 'missing':
				return t('compose.status.missing');
			default:
				return t('compose.status.stopped');
		}
	}

	function stopLogStream() {
		if (logEventSource) {
			logEventSource.close();
			logEventSource = null;
		}
		liveFollow = false;
	}

	async function loadDockerPreconfigIfNeeded(eng: ComposeEngineInfo | null) {
		if (!eng || (eng.docker_installed && eng.compose_installed && eng.is_compose_v5)) {
			return;
		}
		if (dockerPreconfigLoaded) return;
		try {
			const res = await api.getServiceConfig<DockerConfig>('docker');
			if (res.config) {
				dockerInstallCfg = res.config;
				dockerPreconfigLoaded = true;
			}
		} catch {
			// Pertahankan default aman jika /etc/docker/daemon.json belum ada atau sudo belum terbuka
		}
	}

	async function saveDockerPreconfig() {
		savingPreconfig = true;
		preconfigError = '';
		try {
			const payload = {
				...buildDockerConfigPayload(dockerInstallCfg),
				restart: false
			};
			const res = await cp.runWithSudoRetry(t('compose.install.preconfigSudoPrompt'), (sudoPass) =>
				api.saveServiceConfig<DockerConfig>('docker', {
					...payload,
					...(sudoPass ? { sudo_pass: sudoPass } : {})
				})
			);
			if (!res) return;
			if (res.config) {
				dockerInstallCfg = res.config;
				dockerPreconfigLoaded = true;
			}
			toast.success(
				res.message || t('compose.install.preconfigSavedFallback')
			);
		} catch (e) {
			const msg = e instanceof Error ? e.message : t('compose.install.preconfigSaveFailed');
			preconfigError = msg;
			toast.error(msg);
		} finally {
			savingPreconfig = false;
		}
	}

	async function installDockerWithConfig() {
		preconfigError = '';
		await cp.triggerDockerInstall({
			source: installSource,
			add_user_to_group: dockerInstallCfg.add_user_to_docker_group,
			enable_service: installEnableService,
			daemon_config: buildDockerConfigPayload(dockerInstallCfg) as Partial<DockerConfig>
		});
	}

	async function loadStatus(silent = false) {
		if (!silent) loading = true;
		else refreshing = true;
		try {
			const res = await api.getComposeStatus();
			engine = res.engine;
			projects = res.projects ?? [];
			templates = res.templates ?? [];
			void loadDockerPreconfigIfNeeded(res.engine);

			if (projects.length > 0) {
				const stillExists = projects.some((p) => p.name === selectedName);
				if (!stillExists) {
					selectedName = projects[0].name;
				}
				await loadProjectDetail(selectedName);
			} else {
				selectedName = '';
				selectedDetail = null;
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('compose.toast.loadFailed'));
		} finally {
			loading = false;
			refreshing = false;
		}
	}

	async function loadProjectDetail(name: string) {
		if (!name) return;
		detailLoading = true;
		try {
			const proj = await api.getComposeProject(name);
			selectedDetail = proj;
			selectedProfiles = [...(proj.active_profiles ?? [])];
			if (activeTab === 'files') {
				await loadProjectFiles(name);
			} else if (activeTab === 'logs' && !liveFollow) {
				await loadProjectLogs(name);
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('compose.toast.detailFailed', { name }));
		} finally {
			detailLoading = false;
		}
	}

	function scrollElementOnMobile(id: string) {
		if (typeof window === 'undefined' || window.innerWidth >= 1024) return;
		requestAnimationFrame(() => {
			document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
		});
	}

	async function selectProject(name: string) {
		if (name === selectedName && selectedDetail) {
			scrollElementOnMobile('compose-detail-panel');
			return;
		}
		stopLogStream();
		selectedName = name;
		logServiceFilter = '';
		scrollElementOnMobile('compose-detail-panel');
		await loadProjectDetail(name);
	}

	async function loadProjectFiles(name: string) {
		if (!name) return;
		filesLoading = true;
		try {
			const res = await api.getComposeFiles(name);
			composeFileLabel = res.compose_file || 'compose.yaml';
			composeContent = res.compose_content;
			initialComposeContent = res.compose_content;
			envContent = res.env_content || '';
			initialEnvContent = res.env_content || '';
			hasEnvFile = res.has_env_file;
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('compose.toast.filesReadFailed'));
		} finally {
			filesLoading = false;
		}
	}

	async function saveProjectFiles() {
		if (!selectedDetail || filesSaving) return;
		filesSaving = true;
		try {
			const res = await api.saveComposeFiles(selectedDetail.name, {
				compose_content: composeContent,
				env_content: envContent,
				update_env: true
			});
			composeContent = res.files.compose_content;
			initialComposeContent = res.files.compose_content;
			envContent = res.files.env_content || '';
			initialEnvContent = res.files.env_content || '';
			hasEnvFile = res.files.has_env_file;
			toast.success(t('compose.toast.filesSaved', { file: res.files.compose_file }));
			await loadProjectDetail(selectedDetail.name);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('compose.toast.filesSaveFailed'));
		} finally {
			filesSaving = false;
		}
	}

	async function openResolvedConfigModal() {
		if (!selectedDetail) return;
		configModalOpen = true;
		configLoading = true;
		configJSONText = '';
		try {
			const res = await api.getComposeConfig(selectedDetail.name);
			configJSONText = JSON.stringify(res.config, null, 2);
		} catch (err) {
			configJSONText = err instanceof Error ? err.message : t('compose.toast.configRenderFailed');
		} finally {
			configLoading = false;
		}
	}

	async function loadProjectLogs(name: string) {
		if (!name) return;
		logsLoading = true;
		try {
			const res = await api.getComposeLogs(name, logServiceFilter, logTail);
			logsText = res.logs || t('compose.logs.empty');
		} catch (err) {
			logsText = err instanceof Error ? err.message : t('compose.toast.logsLoadFailed');
		} finally {
			logsLoading = false;
		}
	}

	function toggleLiveLogs() {
		if (!selectedDetail) return;
		if (liveFollow) {
			stopLogStream();
			return;
		}
		liveFollow = true;
		logsText = '';
		const url = api.composeLogsStreamUrl(selectedDetail.name, logServiceFilter, 120);
		const es = new EventSource(url);
		logEventSource = es;
		es.onmessage = (ev) => {
			try {
				const parsed = JSON.parse(ev.data) as { type?: string; message?: string };
				if (parsed.type === 'log' && parsed.message !== undefined) {
					logsText = logsText ? `${logsText}\n${parsed.message}` : parsed.message;
					if (logsText.length > 180000) {
						logsText = logsText.slice(-140000);
					}
				} else if (parsed.type === 'error' && parsed.message) {
					toast.error(parsed.message);
					stopLogStream();
				} else if (parsed.type === 'finish') {
					stopLogStream();
				}
			} catch {
				// Abaikan event non-JSON
			}
		};
		es.onerror = () => {
			stopLogStream();
		};
	}

	async function triggerComposeAction(
		action: ComposeActionPayload['action'],
		extra: Partial<ComposeActionPayload> = {}
	) {
		if (!selectedDetail || actionPending) return;
		actionPending = true;
		const projName = selectedDetail.name;
		try {
			const payload: ComposeActionPayload = {
				action,
				profiles: selectedProfiles,
				...extra
			};
			const res = await cp.runWithSudoRetry(
				t('compose.toast.actionSudoPrompt', { action }),
				(sudoPass) => api.runComposeAction(projName, { ...payload, sudo_pass: sudoPass })
			);
			if (res?.status === 'started') {
				const label = extra.service
					? `Compose ${action.toUpperCase()}: ${projName}/${extra.service}`
					: `Compose ${action.toUpperCase()}: ${projName}`;
				cp.openTerminal(label);
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('compose.toast.actionFailed', { action }));
		} finally {
			actionPending = false;
		}
	}

	async function confirmDownSubmit() {
		downOpen = false;
		await triggerComposeAction('down', {
			remove_volumes: downVolumes,
			remove_orphans: downOrphans
		});
	}

	async function handleDeleteProject() {
		if (!selectedDetail) return;
		const ok = await cp.confirm({
			title: t('compose.toast.deleteConfirmTitle', { name: selectedDetail.name }),
			description: t('compose.toast.deleteConfirmDesc'),
			confirmText: t('compose.toast.deleteConfirmBtn'),
			destructive: true
		});
		if (!ok) return;
		try {
			await api.deleteComposeProject(selectedDetail.name, false);
			toast.success(t('compose.toast.deleted', { name: selectedDetail.name }));
			selectedName = '';
			selectedDetail = null;
			await loadStatus(true);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('compose.toast.deleteFailed'));
		}
	}

	async function handleDaemonControl(action: 'start' | 'restart' | 'stop') {
		daemonActionPending = true;
		try {
			await api.controlService('docker', action);
			toast.success(t('compose.toast.daemonSuccess', { action }));
			await loadStatus(true);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('compose.toast.daemonFailed', { action }));
		} finally {
			daemonActionPending = false;
		}
	}

	async function openPruneModal() {
		pruneModalOpen = true;
		dfLoading = true;
		try {
			diskUsage = await api.getComposeDiskUsage();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('compose.toast.dfFailed'));
		} finally {
			dfLoading = false;
		}
	}

	async function triggerPrune(target: string, label: string) {
		const destructive = target === 'volumes' || target === 'images-all';
		const ok = await cp.confirm({
			title: t('compose.toast.pruneConfirmTitle', { label }),
			description:
				target === 'volumes'
					? t('compose.toast.pruneVolumesWarning')
					: t('compose.toast.pruneConfirmDesc', { target }),
			confirmText: t('compose.toast.pruneConfirmBtn'),
			destructive
		});
		if (!ok) return;
		try {
			const res = await cp.runWithSudoRetry(
				t('compose.toast.pruneSudoPrompt'),
				(sudoPass) => api.pruneComposeSystem(target, sudoPass)
			);
			if (res?.status === 'started') {
				pruneModalOpen = false;
				cp.openTerminal(`Docker Prune: ${label}`);
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('compose.toast.pruneFailed'));
		}
	}

	function openExecShell(service: string) {
		if (!selectedDetail) return;
		const url = `/terminal?compose_project=${encodeURIComponent(selectedDetail.name)}&compose_service=${encodeURIComponent(service)}`;
		void goto(url);
	}

	function openExposePort(port: number, service: string) {
		exposePort = port;
		exposeService = service;
		exposeOpen = true;
	}

	function toggleProfile(prof: string, checked: boolean) {
		if (checked) {
			if (!selectedProfiles.includes(prof)) selectedProfiles = [...selectedProfiles, prof];
		} else {
			selectedProfiles = selectedProfiles.filter((p) => p !== prof);
		}
	}

	async function copyText(text: string, label: string) {
		try {
			await navigator.clipboard.writeText(text);
			toast.success(t('compose.toast.copied', { label }));
		} catch {
			toast.error(t('compose.toast.copyFailed', { label }));
		}
	}

	$effect(() => {
		if (activeTab === 'files' && selectedDetail) {
			void loadProjectFiles(selectedDetail.name);
		} else if (activeTab === 'logs' && selectedDetail) {
			if (!liveFollow) {
				void loadProjectLogs(selectedDetail.name);
			}
		} else if (activeTab !== 'logs' && liveFollow) {
			stopLogStream();
		}
	});

	$effect(() => {
		const tick = cp.refreshTick;
		if (tick > lastRefreshTick) {
			lastRefreshTick = tick;
			if (!loading) {
				void loadStatus(true);
			}
		}
	});

	onMount(() => {
		const dirParam = page.url.searchParams.get('dir')?.trim() ?? '';
		if (dirParam) {
			createInitialDir = dirParam;
			createOpen = true;
		}
		void loadStatus(false);
	});

	onDestroy(() => {
		stopLogStream();
	});
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<!-- Page Header -->
	<div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
		<div class="flex min-w-0 flex-col gap-1">
			<div class="flex flex-wrap items-center gap-2">
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('compose.title')}</h1>
				{#if engine?.docker_installed}
					<Badge variant="secondary" class="font-mono">
						{t('compose.dockerBadge', { version: engine.docker_version || '?' })}
					</Badge>
					{#if engine.compose_installed}
						<Badge variant={engine.is_compose_v5 ? 'default' : 'outline'} class="font-mono">
							{t('compose.composeBadge', { version: engine.compose_version ?? '' })}
							{#if engine.is_compose_v5}
								(v5)
							{/if}
						</Badge>
					{/if}
				{/if}
			</div>
			<p class="text-xs text-muted-foreground sm:text-sm">
				{t('compose.subtitle1')}<code>compose.yaml</code>{t('compose.subtitle2')}<code>.env</code>{t('compose.subtitle3')}
			</p>
		</div>

		<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center" role="toolbar" aria-label={t('compose.toolbarAria')}>
			<Button variant="outline" size="sm" class="min-h-9 sm:min-h-0" disabled={refreshing} onclick={() => loadStatus(true)}>
				<RefreshCwIcon class={cn(refreshing && 'animate-spin')} data-icon="inline-start" />
				{t('compose.refreshBtn')}
			</Button>
			<Button variant="outline" size="sm" class="min-h-9 sm:min-h-0" onclick={() => cp.openServiceConfig('docker')}>
				<SettingsIcon data-icon="inline-start" />
				{t('compose.configureDockerBtn')}
			</Button>
			{#if engine?.docker_installed && engine.daemon_running}
				<Button variant="outline" size="sm" class="min-h-9 sm:min-h-0" onclick={openPruneModal}>
					<HardDriveIcon data-icon="inline-start" />
					{t('compose.diskPruneBtn')}
				</Button>
			{/if}
			<Button
				size="sm"
				class={cn(
					'min-h-9 sm:min-h-0',
					!(engine?.docker_installed && engine.daemon_running) && 'col-span-2 sm:col-span-1'
				)}
				onclick={() => {
					createInitialDir = '';
					createOpen = true;
				}}
			>
				<PlusIcon data-icon="inline-start" />
				{t('compose.addStackBtn')}
			</Button>
		</div>
	</div>

	{#if loading}
		<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
			{#each [1, 2, 3, 4] as idx (idx)}
				<Skeleton class="h-24 rounded-xl" />
			{/each}
		</div>
		<Skeleton class="h-96 rounded-xl" />
	{:else}
		<!-- Inline Installation & Pre-Configuration Card -->
		{#if engine && (!engine.docker_installed || !engine.compose_installed)}
			<Card.Root class="border-primary/30">
				<Card.Header>
					<Card.Title class="flex items-center gap-2">
						<ContainerIcon class="size-4 shrink-0 text-primary" />
						<span>
							{engine.docker_installed
								? t('compose.install.pluginMissingTitle')
								: t('compose.install.engineMissingTitle')}
						</span>
					</Card.Title>
					<Card.Description>
						{t('compose.install.description1')}<code>/etc/docker/daemon.json</code>{t('compose.install.description2')}
					</Card.Description>
					<Card.Action class="col-span-full row-start-3 mt-1 justify-self-stretch sm:col-span-1 sm:col-start-2 sm:row-span-2 sm:row-start-1 sm:mt-0 sm:justify-self-end">
						<Button
							type="button"
							variant="outline"
							size="sm"
							class="min-h-9 w-full sm:min-h-0 sm:w-auto"
							onclick={() => (showPreconfigForm = !showPreconfigForm)}
						>
							<SettingsIcon data-icon="inline-start" />
							{showPreconfigForm ? t('compose.install.hideConfig') : t('compose.install.showConfig')}
						</Button>
					</Card.Action>
				</Card.Header>

				<Card.Content class="flex flex-col gap-5">
					<Field.FieldSet>
						<Field.FieldLegend variant="label">{t('compose.install.optionsLegend')}</Field.FieldLegend>
						<Field.FieldGroup class="grid gap-4 rounded-lg border bg-muted/20 p-3 sm:grid-cols-2 sm:p-4">
							<Field.Field class="min-w-0">
								<Field.FieldLabel for="compose-docker-install-source">
									{t('compose.install.sourceLabel')}
								</Field.FieldLabel>
								<Select.Root
									type="single"
									value={installSource}
									onValueChange={(v) => {
										if (v === 'official' || v === 'distro') installSource = v;
									}}
								>
									<Select.Trigger id="compose-docker-install-source" class="h-10 w-full min-w-0 sm:h-8">
										<span class="truncate">{installSourceLabels[installSource]}</span>
									</Select.Trigger>
									<Select.Content>
										<Select.Group>
											<Select.Item value="official">
												{t('compose.install.sourceOfficial')}
											</Select.Item>
											<Select.Item value="distro">
												{t('compose.install.sourceDistro')}
											</Select.Item>
										</Select.Group>
									</Select.Content>
								</Select.Root>
								<Field.FieldDescription>
									{t('compose.install.sourceDesc1')}<code>docker-compose-plugin</code>{t('compose.install.sourceDesc2')}
								</Field.FieldDescription>
							</Field.Field>

							<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
								<Checkbox id="compose-docker-enable-svc" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={installEnableService} />
								<Field.FieldContent>
									<Field.FieldLabel for="compose-docker-enable-svc" class="cursor-pointer leading-snug font-medium">
										<span>{t('compose.install.autostartLabel1')}<code>docker.service</code>{t('compose.install.autostartLabel2')}</span>
									</Field.FieldLabel>
									<Field.FieldDescription>
										{t('compose.install.autostartDesc1')}<code>systemctl enable --now docker</code>{t('compose.install.autostartDesc2')}<code>daemon.json</code>{t('compose.install.autostartDesc3')}
									</Field.FieldDescription>
								</Field.FieldContent>
							</Field.Field>
						</Field.FieldGroup>
					</Field.FieldSet>

					{#if showPreconfigForm}
						<Separator />
						<DockerConfigPanel
							bind:config={dockerInstallCfg}
							showActions={false}
							formError={preconfigError}
						/>
					{/if}
				</Card.Content>

				<Card.Footer class="flex flex-col items-stretch gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
					<span class="text-xs text-muted-foreground">
						{t('compose.install.footerNote1')}<code>daemon.json</code>{t('compose.install.footerNote2')}
					</span>
					<div class="flex flex-col-reverse gap-2 sm:flex-row sm:flex-wrap sm:items-center">
						<Button
							type="button"
							variant="outline"
							size="sm"
							class="min-h-9 w-full sm:min-h-0 sm:w-auto"
							disabled={savingPreconfig}
							onclick={saveDockerPreconfig}
						>
							{#if savingPreconfig}
								<Spinner data-icon="inline-start" />
								{t('compose.install.savingPreconfig')}
							{:else}
								<CheckIcon data-icon="inline-start" />
								{t('compose.install.savePreconfigOnly')}
							{/if}
						</Button>
						<Button
							type="button"
							size="sm"
							class="h-auto min-h-9 w-full py-1.5 text-center leading-snug whitespace-normal sm:h-7 sm:min-h-0 sm:w-auto sm:py-0 sm:whitespace-nowrap"
							onclick={installDockerWithConfig}
						>
							<DownloadIcon data-icon="inline-start" />
							{engine.docker_installed
								? t('compose.install.installComposeOnly')
								: t('compose.install.installDockerAndCompose')}
						</Button>
					</div>
				</Card.Footer>
			</Card.Root>
		{/if}

		<!-- Daemon Status / Compose Upgrade Alerts -->
		{#if engine && engine.docker_installed && !engine.daemon_running}
			<Alert.Root variant="destructive">
				<TriangleAlertIcon />
				<Alert.Title>{t('compose.alerts.daemonDownTitle')}</Alert.Title>
				<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
					<span class="text-xs">{engine.daemon_error || t('compose.alerts.daemonDownFallback')}</span>
					<Button size="sm" class="min-h-9 w-full sm:min-h-0 sm:w-auto" disabled={daemonActionPending} onclick={() => handleDaemonControl('start')}>
						{#if daemonActionPending}
							<Spinner data-icon="inline-start" />
						{:else}
							<PlayIcon data-icon="inline-start" />
						{/if}
						{t('compose.alerts.startDockerBtn')}
					</Button>
				</Alert.Description>
			</Alert.Root>
		{:else if engine && engine.docker_installed && engine.compose_installed && !engine.is_compose_v5}
			<Alert.Root>
				<ArrowUpCircleIcon />
				<Alert.Title>
					{t('compose.alerts.upgradeComposeTitle', { version: engine.compose_version ?? '' })}
				</Alert.Title>
				<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
					<span class="text-xs">
						{t('compose.alerts.upgradeComposeDesc')}
					</span>
					<div class="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
						<Button size="sm" variant="outline" class="min-h-9 w-full sm:min-h-0 sm:w-auto" onclick={() => cp.openServiceConfig('docker')}>
							<SettingsIcon data-icon="inline-start" />
							{t('compose.configureDockerBtn')}
						</Button>
						<Button size="sm" variant="outline" class="min-h-9 w-full sm:min-h-0 sm:w-auto" onclick={() => cp.triggerInstall('docker')}>
							<ArrowUpCircleIcon data-icon="inline-start" />
							{t('compose.alerts.upgradeComposeBtn')}
						</Button>
					</div>
				</Alert.Description>
			</Alert.Root>
		{/if}

		<!-- KPI Summary Cards -->
		<div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
			<Card.Root size="sm">
				<Card.Header>
					<Card.Description>{t('compose.kpi.total')}</Card.Description>
				</Card.Header>
				<Card.Content>
					<p class="text-2xl font-bold tabular-nums">{kpiCounts.total}</p>
				</Card.Content>
			</Card.Root>

			<Card.Root size="sm">
				<Card.Header>
					<Card.Description>{t('compose.kpi.running')}</Card.Description>
				</Card.Header>
				<Card.Content>
					<p class="text-2xl font-bold text-success tabular-nums">{kpiCounts.running}</p>
				</Card.Content>
			</Card.Root>

			<Card.Root size="sm">
				<Card.Header>
					<Card.Description>{t('compose.kpi.stopped')}</Card.Description>
				</Card.Header>
				<Card.Content>
					<p class="text-2xl font-bold tabular-nums">{kpiCounts.stopped}</p>
				</Card.Content>
			</Card.Root>

			<Card.Root size="sm">
				<Card.Header>
					<Card.Description>{t('compose.kpi.warnings')}</Card.Description>
				</Card.Header>
				<Card.Content>
					<p class={cn('text-2xl font-bold tabular-nums', kpiCounts.warnings > 0 && 'text-warning')}>
						{kpiCounts.warnings}
					</p>
				</Card.Content>
			</Card.Root>
		</div>

		{#if projects.length === 0}
			<Card.Root>
				<Card.Content>
					<Empty.Root class="py-10">
						<Empty.Header>
							<Empty.Media variant="icon">
								<ContainerIcon />
							</Empty.Media>
							<Empty.Title>{t('compose.empty.title')}</Empty.Title>
							<Empty.Description>
								{t('compose.empty.desc1')}<code>compose.yaml</code>{t('compose.empty.desc2')}
							</Empty.Description>
						</Empty.Header>
						<Empty.Content>
							<Button
								onclick={() => {
									createInitialDir = '';
									createOpen = true;
								}}
							>
								<PlusIcon data-icon="inline-start" />
								{t('compose.empty.cta')}
							</Button>
						</Empty.Content>
					</Empty.Root>
				</Card.Content>
			</Card.Root>
		{:else}
			<div class="grid gap-5 lg:grid-cols-12">
				<!-- Left Column: Project Selector -->
				<div id="compose-project-list" class="flex min-w-0 scroll-mt-16 flex-col gap-3 lg:col-span-4 xl:col-span-3">
					<Card.Root class="min-w-0">
						<Card.Header class="flex flex-col gap-2.5 border-b">
							<InputGroup.Root class="h-10 sm:h-8">
								<InputGroup.Addon>
									<SearchIcon />
								</InputGroup.Addon>
								<InputGroup.Input
									bind:value={searchQuery}
									placeholder={t('compose.sidebar.searchPlaceholder')}
									aria-label={t('compose.sidebar.searchAria')}
								/>
							</InputGroup.Root>

							<ToggleGroup.Root
								type="single"
								variant="outline"
								size="sm"
								value={statusFilter}
								onValueChange={(v) => {
									if (v === 'all' || v === 'running' || v === 'stopped') {
										statusFilter = v;
									}
								}}
								aria-label={t('compose.sidebar.filterAria')}
								class="grid w-full grid-cols-3"
							>
								<ToggleGroup.Item value="all" class="min-w-0 justify-center px-1.5 text-xs">
									<span class="truncate">{t('compose.sidebar.filterAll', { count: kpiCounts.total })}</span>
								</ToggleGroup.Item>
								<ToggleGroup.Item value="running" class="min-w-0 justify-center px-1.5 text-xs">
									<span class="truncate">{t('compose.sidebar.filterRunning', { count: kpiCounts.running })}</span>
								</ToggleGroup.Item>
								<ToggleGroup.Item value="stopped" class="min-w-0 justify-center px-1.5 text-xs">
									<span class="truncate">{t('compose.sidebar.filterStopped', { count: kpiCounts.stopped })}</span>
								</ToggleGroup.Item>
							</ToggleGroup.Root>
						</Card.Header>

						<Card.Content class="min-w-0">
							{#if filteredProjects.length === 0}
								<Empty.Root class="py-8">
									<Empty.Header>
										<Empty.Media variant="icon">
											<SearchIcon />
										</Empty.Media>
										<Empty.Title>{t('compose.sidebar.noMatchTitle')}</Empty.Title>
										<Empty.Description>
											{t('compose.sidebar.noMatchDesc')}
										</Empty.Description>
									</Empty.Header>
									{#if searchQuery || statusFilter !== 'all'}
										<Empty.Content>
											<Button
												variant="outline"
												size="sm"
												class="min-h-9 sm:min-h-0"
												onclick={() => {
													searchQuery = '';
													statusFilter = 'all';
												}}
											>
												{t('compose.sidebar.resetFilter')}
											</Button>
										</Empty.Content>
									{/if}
								</Empty.Root>
							{:else}
								<ScrollArea
									class="w-full min-w-0 pr-2 [&_[data-slot=scroll-area-viewport]]:max-h-[36dvh] lg:[&_[data-slot=scroll-area-viewport]]:max-h-[min(560px,62dvh)] [&_[data-slot=scroll-area-viewport]>div]:!block [&_[data-slot=scroll-area-viewport]>div]:w-full [&_[data-slot=scroll-area-viewport]>div]:min-w-0"
								>
									<div class="flex min-w-0 flex-col gap-2">
										{#each filteredProjects as proj (proj.name)}
											{@const active = proj.name === selectedName}
											<button
												type="button"
												onclick={() => selectProject(proj.name)}
												class={cn(
													'flex w-full min-w-0 flex-col gap-1.5 rounded-lg border p-3 text-left transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
													active
														? 'border-primary bg-primary/5 ring-1 ring-primary/20'
														: 'border-border hover:bg-muted/50'
												)}
											>
												<div class="flex w-full min-w-0 items-start justify-between gap-2">
													<span class="min-w-0 truncate font-mono text-sm font-semibold">{proj.name}</span>
													<StatusBadge tone={statusTone(proj.status)} label={statusLabel(proj.status)} />
												</div>
												<div class="w-full min-w-0 truncate font-mono text-xs text-muted-foreground">
													{proj.dir_path || t('compose.sidebar.unknownFolder')}
												</div>
												<div class="flex flex-wrap items-center gap-1.5 pt-0.5">
													<Badge variant="outline" class="font-mono">
														{t('compose.sidebar.servicesCount', { running: proj.running_count, total: proj.services_count })}
													</Badge>
													{#if (proj.security_warnings?.length ?? 0) > 0}
														<Badge variant="destructive">
															{t('compose.sidebar.warningsCount', { count: proj.security_warnings?.length ?? 0 })}
														</Badge>
													{/if}
													{#if (proj.linked_vhosts?.length ?? 0) > 0}
														<Badge variant="secondary">
															{t('compose.sidebar.vhostsCount', { count: proj.linked_vhosts?.length ?? 0 })}
														</Badge>
													{/if}
												</div>
											</button>
										{/each}
									</div>
								</ScrollArea>
							{/if}
						</Card.Content>
					</Card.Root>
				</div>

				<!-- Right Column: Selected Project Detail -->
				<div id="compose-detail-panel" class="min-w-0 scroll-mt-16 lg:col-span-8 xl:col-span-9">
					{#if !selectedDetail}
						<Card.Root>
							<Card.Content>
								<Empty.Root class="py-12">
									<Empty.Header>
										<Empty.Media variant="icon">
											<ContainerIcon />
										</Empty.Media>
										<Empty.Title>{t('compose.detail.emptyTitle')}</Empty.Title>
										<Empty.Description>
											{t('compose.detail.emptyDesc')}
										</Empty.Description>
									</Empty.Header>
								</Empty.Root>
							</Card.Content>
						</Card.Root>
					{:else}
						<Card.Root>
							<Card.Header class="flex flex-col gap-3 border-b">
								<div class="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
									<div class="flex min-w-0 flex-col gap-1.5">
										<div class="flex flex-wrap items-center gap-2">
											<Card.Title class="max-w-full truncate font-mono text-lg font-bold">
												{selectedDetail.name}
											</Card.Title>
											<StatusBadge
												tone={statusTone(selectedDetail.status)}
												label={statusLabel(selectedDetail.status)}
											/>
											<Badge variant="outline" class="max-w-full font-mono">
												<span class="truncate">{selectedDetail.compose_file || 'compose.yaml'}</span>
											</Badge>
											{#if !selectedDetail.registered}
												<Badge variant="secondary">{t('compose.detail.autoDetected')}</Badge>
											{/if}
											{#if projects.length > 1}
												<Button
													type="button"
													variant="outline"
													size="xs"
													class="ml-auto min-h-7 lg:hidden"
													onclick={() => scrollElementOnMobile('compose-project-list')}
												>
													{t('compose.detail.stackListMobile', { count: projects.length })}
												</Button>
											{/if}
										</div>
										<div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
											<span class="min-w-0 max-w-full font-mono break-all sm:truncate">{selectedDetail.dir_path || '-'}</span>
											{#if selectedDetail.dir_path}
												<div class="flex flex-wrap items-center gap-1.5">
													<Button
														variant="ghost"
														size="xs"
														class="min-h-8 sm:min-h-0"
														href={`/files?path=${encodeURIComponent(selectedDetail.dir_path)}`}
													>
														<FolderOpenIcon data-icon="inline-start" />
														{t('compose.detail.fileManager')}
													</Button>
													<Button
														variant="ghost"
														size="xs"
														class="min-h-8 sm:min-h-0"
														href={`/terminal?cwd=${encodeURIComponent(selectedDetail.dir_path)}`}
													>
														<TerminalIcon data-icon="inline-start" />
														{t('compose.detail.hostTerminal')}
													</Button>
												</div>
											{/if}
										</div>
									</div>

									<!-- Stack Lifecycle Actions -->
									<div
										class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center sm:gap-1.5"
										role="toolbar"
										aria-label={t('compose.detail.actionsAria')}
									>
										<Button
											size="sm"
											class="min-h-9 sm:min-h-0"
											disabled={actionPending}
											onclick={() => triggerComposeAction('up')}
										>
											<PlayIcon data-icon="inline-start" />
											{t('compose.detail.upD')}
										</Button>
										<Button
											variant="outline"
											size="sm"
											class="min-h-9 sm:min-h-0"
											disabled={actionPending}
											onclick={() => triggerComposeAction('up', { build: true })}
										>
											<HammerIcon data-icon="inline-start" />
											{t('compose.detail.upBuild')}
										</Button>
										<Button
											variant="outline"
											size="sm"
											class="min-h-9 sm:min-h-0"
											disabled={actionPending}
											onclick={() => triggerComposeAction('restart')}
										>
											<RotateCwIcon data-icon="inline-start" />
											{t('compose.detail.restart')}
										</Button>
										<Button
											variant="outline"
											size="sm"
											class="min-h-9 sm:min-h-0"
											disabled={actionPending}
											onclick={() => triggerComposeAction('pull')}
										>
											<DownloadIcon data-icon="inline-start" />
											{t('compose.detail.pull')}
										</Button>
										<Button
											variant="destructive"
											size="sm"
											class={cn('min-h-9 sm:min-h-0', !selectedDetail.registered && 'col-span-2 sm:col-span-1')}
											disabled={actionPending}
											onclick={() => {
												downVolumes = false;
												downOpen = true;
											}}
										>
											<SquareIcon data-icon="inline-start" />
											{t('compose.detail.down')}
										</Button>
										{#if selectedDetail.registered}
											<Button
												variant="outline"
												size="sm"
												class="min-h-9 text-destructive sm:size-7 sm:min-h-0 sm:border-transparent sm:bg-transparent sm:px-0"
												aria-label={t('compose.detail.deleteAria')}
												title={t('compose.detail.deleteAria')}
												onclick={handleDeleteProject}
											>
												<Trash2Icon />
												<span class="sm:sr-only">{t('compose.detail.deleteMobile')}</span>
											</Button>
										{/if}
									</div>
								</div>

								<!-- Profiles selector (Compose v5 profiles) -->
								{#if (selectedDetail.available_profiles?.length ?? 0) > 0}
									<Field.FieldSet class="rounded-lg border bg-muted/30 px-3 py-2">
										<div class="flex flex-wrap items-center gap-3">
											<Field.FieldLegend variant="label" class="mb-0 text-xs">
												{t('compose.detail.profilesLegend')}
											</Field.FieldLegend>
											{#each selectedDetail.available_profiles ?? [] as prof (prof)}
												{@const profileId = `compose-profile-${prof}`}
												<Field.Field orientation="horizontal" class="w-auto">
													<Checkbox
														id={profileId}
														checked={selectedProfiles.includes(prof)}
														onCheckedChange={(v) => toggleProfile(prof, Boolean(v))}
													/>
													<Field.FieldLabel for={profileId} class="cursor-pointer font-mono text-xs font-normal">
														{prof}
													</Field.FieldLabel>
												</Field.Field>
											{/each}
										</div>
									</Field.FieldSet>
								{/if}

								<!-- Linked VHosts -->
								{#if (selectedDetail.linked_vhosts?.length ?? 0) > 0}
									<div class="flex flex-wrap items-center gap-2 text-xs">
										<span class="font-medium text-muted-foreground">{t('compose.detail.linkedVhosts')}</span>
										{#each selectedDetail.linked_vhosts ?? [] as vh (vh.domain)}
											<Badge
												href={`${vh.ssl_enabled ? 'https' : 'http'}://${vh.domain}`}
												target="_blank"
												rel="noopener noreferrer"
												variant="outline"
												class="max-w-full border-success/30 bg-success/10 font-mono text-success"
											>
												<GlobeIcon data-icon="inline-start" />
												<span class="truncate">{vh.domain} (:{vh.proxy_port})</span>
											</Badge>
										{/each}
									</div>
								{/if}

								<!-- Security Warnings for 0.0.0.0 UFW bypass -->
								{#if (selectedDetail.security_warnings?.length ?? 0) > 0}
									<Alert.Root variant="destructive">
										<TriangleAlertIcon />
										<Alert.Title>{t('compose.detail.securityWarningTitle')}</Alert.Title>
										<Alert.Description class="flex flex-col gap-1 text-xs">
											{#each selectedDetail.security_warnings ?? [] as warn, i (i)}
												<p class="break-words">{warn}</p>
											{/each}
										</Alert.Description>
									</Alert.Root>
								{/if}
							</Card.Header>

							<Card.Content>
								<Tabs.Root bind:value={activeTab}>
									<Tabs.List
										class="flex w-full flex-nowrap justify-start overflow-x-auto overflow-y-hidden [scrollbar-width:none] [&::-webkit-scrollbar]:hidden sm:inline-flex sm:w-auto"
									>
										<Tabs.Trigger value="services" class="flex-none shrink-0 px-2.5 text-xs sm:flex-1 sm:px-3 sm:text-sm">
											{t('compose.detail.tabServices')}
										</Tabs.Trigger>
										<Tabs.Trigger value="files" class="flex-none shrink-0 px-2.5 text-xs sm:flex-1 sm:px-3 sm:text-sm">
											{t('compose.detail.tabFiles')}
										</Tabs.Trigger>
										<Tabs.Trigger value="logs" class="flex-none shrink-0 px-2.5 text-xs sm:flex-1 sm:px-3 sm:text-sm">
											{t('compose.detail.tabLogs')}
										</Tabs.Trigger>
									</Tabs.List>

									<!-- TAB 1: Services & Containers -->
									<Tabs.Content value="services" class="mt-4 flex flex-col gap-5">
										{#if detailLoading}
											<div class="flex items-center gap-2 py-6 text-sm text-muted-foreground">
												<Spinner />
												<span>{t('compose.detail.loadingContainers')}</span>
											</div>
										{:else if (selectedDetail.containers?.length ?? 0) === 0 && (selectedDetail.defined_services?.length ?? 0) === 0}
											<Empty.Root class="border py-10">
												<Empty.Header>
													<Empty.Media variant="icon">
														<ContainerIcon />
													</Empty.Media>
													<Empty.Title>{t('compose.detail.noDefinedTitle')}</Empty.Title>
													<Empty.Description>
														{t('compose.detail.noDefinedDesc1')}<code>{selectedDetail.compose_file}</code>{t('compose.detail.noDefinedDesc2')}
													</Empty.Description>
												</Empty.Header>
											</Empty.Root>
										{:else}
											<!-- Live Containers -->
											{#if (selectedDetail.containers?.length ?? 0) > 0}
												<div class="flex flex-col gap-2">
													<h3 class="text-sm font-semibold">{t('compose.detail.activeContainers')}</h3>
													<ResponsiveTableWrapper label={t('compose.detail.containersTableAria')}>
														<Table.Root>
															<Table.Header>
																<Table.Row>
																	<Table.Head>{t('compose.detail.colService')}</Table.Head>
																	<Table.Head>{t('compose.detail.colStatus')}</Table.Head>
																	<Table.Head>{t('compose.detail.colCpuRam')}</Table.Head>
																	<Table.Head>{t('compose.detail.colPortExpose')}</Table.Head>
																	<Table.Head class="text-right">{t('compose.detail.colActions')}</Table.Head>
																</Table.Row>
															</Table.Header>
															<Table.Body>
																{#each selectedDetail.containers ?? [] as cont (cont.id || cont.name)}
																	<Table.Row>
																		<Table.Cell data-label={t('compose.detail.colService')} class="align-top">
																			<div class="flex flex-col gap-0.5">
																				<span class="font-mono text-xs font-semibold">{cont.service}</span>
																				<span class="font-mono text-xs text-muted-foreground">{cont.name}</span>
																				<span class="max-w-full truncate text-xs text-muted-foreground sm:max-w-52" title={cont.image}>
																					{cont.image}
																				</span>
																			</div>
																		</Table.Cell>
																		<Table.Cell data-label={t('compose.detail.colStatus')} class="align-top">
																			<div class="flex flex-col gap-1">
																				<div class="flex flex-wrap items-center gap-1.5">
																					<StatusBadge
																						tone={cont.state === 'running' ? 'success' : cont.state === 'restarting' ? 'warning' : 'neutral'}
																						label={cont.state || 'unknown'}
																					/>
																					{#if cont.health}
																						<Badge variant={cont.health === 'healthy' ? 'secondary' : 'destructive'}>
																							{cont.health}
																						</Badge>
																					{/if}
																				</div>
																				<span class="text-xs text-muted-foreground">{cont.status}</span>
																			</div>
																		</Table.Cell>
																		<Table.Cell data-label={t('compose.detail.colCpuRam')} class="align-top font-mono text-xs">
																			{#if cont.cpu_perc || cont.mem_usage}
																				<div class="flex flex-col gap-0.5">
																					<span>CPU: {cont.cpu_perc || '-'}</span>
																					<span class="text-muted-foreground">RAM: {cont.mem_usage || '-'}</span>
																				</div>
																			{:else}
																				<span class="text-muted-foreground">—</span>
																			{/if}
																		</Table.Cell>
																		<Table.Cell data-label={t('compose.detail.colPortExpose')} class="align-top">
																			{#if (cont.ports?.length ?? 0) === 0}
																				<span class="text-xs text-muted-foreground">—</span>
																			{:else}
																				<div class="flex flex-col gap-1">
																					{#each cont.ports ?? [] as pb (pb.summary)}
																						<div class="flex flex-wrap items-center gap-1.5">
																							<Badge
																								variant={pb.exposed_public && pb.sensitive_port ? 'destructive' : 'outline'}
																								class="max-w-full font-mono"
																							>
																								<span class="truncate">{pb.summary}</span>
																							</Badge>
																							{#if pb.host_port > 0}
																								<Button
																									variant="ghost"
																									size="xs"
																									class="min-h-8 sm:min-h-0"
																									onclick={() => openExposePort(pb.host_port, cont.service)}
																								>
																									<GlobeIcon data-icon="inline-start" />
																									{t('compose.detail.exposeBtn')}
																								</Button>
																							{/if}
																						</div>
																					{/each}
																				</div>
																			{/if}
																		</Table.Cell>
																		<Table.Cell data-label={t('compose.detail.colActions')} class="text-right align-top">
																			<div class="flex flex-wrap justify-end gap-1.5 sm:gap-1">
																				{#if cont.state === 'running'}
																					<Button
																						variant="outline"
																						size="xs"
																						class="min-h-8 sm:min-h-0"
																						onclick={() => openExecShell(cont.service)}
																						title={t('compose.detail.execTitle')}
																					>
																						<TerminalIcon data-icon="inline-start" />
																						{t('compose.detail.execBtn')}
																					</Button>
																					<Button
																						variant="outline"
																						size="xs"
																						class="min-h-8 sm:min-h-0"
																						onclick={() => triggerComposeAction('restart', { service: cont.service })}
																					>
																						<RotateCwIcon data-icon="inline-start" />
																						{t('compose.detail.restart')}
																					</Button>
																					<Button
																						variant="ghost"
																						size="xs"
																						class="min-h-8 sm:min-h-0"
																						onclick={() => triggerComposeAction('stop', { service: cont.service })}
																					>
																						{t('compose.detail.stopBtn')}
																					</Button>
																				{:else}
																					<Button
																						variant="outline"
																						size="xs"
																						class="min-h-8 sm:min-h-0"
																						onclick={() => triggerComposeAction('start', { service: cont.service })}
																					>
																						<PlayIcon data-icon="inline-start" />
																						{t('compose.detail.startBtn')}
																					</Button>
																				{/if}
																			</div>
																		</Table.Cell>
																	</Table.Row>
																{/each}
															</Table.Body>
														</Table.Root>
													</ResponsiveTableWrapper>
												</div>
											{/if}

											<!-- Defined Services & Compose v5 Jobs from YAML -->
											{#if (selectedDetail.defined_services?.length ?? 0) > 0}
												<div class="flex flex-col gap-2.5">
													<h3 class="text-sm font-semibold">
														{t('compose.detail.definedServicesHeading', { file: selectedDetail.compose_file })}
													</h3>
													<div class="grid gap-3 md:grid-cols-2">
														{#each selectedDetail.defined_services ?? [] as svc (svc.name)}
															<Card.Root size="sm">
																<Card.Header>
																	<Card.Title class="min-w-0 truncate font-mono">{svc.name}</Card.Title>
																	<Card.Description class="min-w-0 truncate font-mono text-xs">
																		{svc.image || (svc.build ? t('compose.detail.buildPrefix', { build: svc.build }) : t('compose.detail.imageUnspecified'))}
																	</Card.Description>
																	{#if svc.is_job || svc.has_healthcheck}
																		<Card.Action class="flex flex-wrap justify-end gap-1">
																			{#if svc.is_job}
																				<Badge variant="secondary">{t('compose.detail.jobBadge')}</Badge>
																			{/if}
																			{#if svc.has_healthcheck}
																				<Badge variant="outline">
																					<ShieldCheckIcon data-icon="inline-start" class="text-success" />
																					{t('compose.detail.healthcheckBadge')}
																				</Badge>
																			{/if}
																		</Card.Action>
																	{/if}
																</Card.Header>

																{#if svc.schedule_cron || (svc.depends_on?.length ?? 0) > 0}
																	<Card.Content class="flex flex-col gap-1 text-xs">
																		{#if svc.schedule_cron}
																			<div class="font-mono text-primary">
																				{t('compose.detail.cronPrefix', { cron: svc.schedule_cron })}
																			</div>
																		{/if}
																		{#if (svc.depends_on?.length ?? 0) > 0}
																			<div class="text-muted-foreground">
																				{t('compose.detail.dependsOn')} <span class="font-mono">{svc.depends_on?.join(', ')}</span>
																			</div>
																		{/if}
																	</Card.Content>
																{/if}

																{#if (svc.ports?.length ?? 0) > 0 || svc.is_job}
																	<Card.Footer class="mt-auto flex flex-wrap items-center justify-between gap-2">
																		<div class="flex min-w-0 flex-wrap gap-1.5">
																			{#each svc.ports ?? [] as pb (pb.summary)}
																				{#if pb.host_port > 0}
																					<Button
																						type="button"
																						size="xs"
																						variant={pb.exposed_public && pb.sensitive_port ? 'destructive' : 'outline'}
																						class="min-h-8 max-w-full font-mono sm:min-h-0"
																						title={t('compose.detail.exposePortTitle')}
																						onclick={() => openExposePort(pb.host_port, svc.name)}
																					>
																						<GlobeIcon data-icon="inline-start" />
																						<span class="truncate">{pb.summary}</span>
																					</Button>
																				{:else}
																					<Badge variant="outline" class="max-w-full font-mono">
																						<span class="truncate">{pb.summary}</span>
																					</Badge>
																				{/if}
																			{/each}
																		</div>
																		{#if svc.is_job}
																			<Button
																				size="xs"
																				variant="outline"
																				class="min-h-8 sm:min-h-0"
																				onclick={() => triggerComposeAction('run-job', { service: svc.name })}
																			>
																				<PlayIcon data-icon="inline-start" />
																				{t('compose.detail.runJobBtn')}
																			</Button>
																		{/if}
																	</Card.Footer>
																{/if}
															</Card.Root>
														{/each}
													</div>
												</div>
											{/if}
										{/if}
									</Tabs.Content>

									<!-- TAB 2: Editor YAML & .env -->
									<Tabs.Content value="files" class="mt-4 flex flex-col gap-4">
										<div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
											<div class="flex flex-wrap items-center gap-2">
												<Badge variant="outline" class="font-mono">{composeFileLabel}</Badge>
												<Badge variant={hasEnvFile ? 'secondary' : 'outline'} class="font-mono">
													{hasEnvFile ? '.env (0600)' : t('compose.files.envNew')}
												</Badge>
												{#if filesDirty}
													<Badge>{t('compose.files.unsaved')}</Badge>
												{/if}
											</div>
											<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
												<Button variant="outline" size="sm" class="min-h-9 sm:min-h-0" onclick={openResolvedConfigModal}>
													<FileCodeIcon data-icon="inline-start" />
													<span class="truncate">{t('compose.files.inspectConfig')}</span>
												</Button>
												<Button
													variant="outline"
													size="sm"
													class="min-h-9 sm:min-h-0"
													disabled={filesLoading}
													onclick={() => selectedDetail && loadProjectFiles(selectedDetail.name)}
												>
													{t('compose.files.reload')}
												</Button>
												<Button
													size="sm"
													class="col-span-2 min-h-9 sm:col-span-1 sm:min-h-0"
													disabled={filesSaving || !filesDirty}
													onclick={saveProjectFiles}
												>
													{#if filesSaving}
														<Spinner data-icon="inline-start" />
														{t('compose.files.validatingSaving')}
													{:else}
														{t('compose.files.validateSave')}
													{/if}
												</Button>
											</div>
										</div>

										{#if filesLoading}
											<div class="flex items-center gap-2 py-8 text-sm text-muted-foreground">
												<Spinner />
												<span>{t('compose.files.loadingFiles')}</span>
											</div>
										{:else}
											<Field.FieldGroup class="grid gap-4 xl:grid-cols-12">
												<Field.Field class="min-w-0 xl:col-span-8">
													<div class="flex flex-wrap items-center justify-between gap-2">
														<Field.FieldLabel for="compose-yaml-editor" class="font-mono">
															{composeFileLabel}
														</Field.FieldLabel>
														<span class="text-xs text-muted-foreground">
															{t('compose.files.validatedViaPrefix')} <code>docker compose config --quiet</code>
														</span>
													</div>
													<Textarea
														id="compose-yaml-editor"
														bind:value={composeContent}
														rows={18}
														class="max-h-[50dvh] min-h-48 resize-y overflow-y-auto font-mono text-xs leading-relaxed sm:min-h-72"
														spellcheck={false}
													/>
												</Field.Field>

												<Field.Field class="min-w-0 xl:col-span-4">
													<div class="flex items-center justify-between gap-2">
														<Field.FieldLabel for="compose-env-editor" class="font-mono">
															.env
														</Field.FieldLabel>
														<Button
															variant="ghost"
															size="xs"
															class="min-h-8 sm:min-h-0"
															onclick={() => (maskEnvValues = !maskEnvValues)}
														>
															{#if maskEnvValues}
																<EyeIcon data-icon="inline-start" />
																{t('compose.files.showValues')}
															{:else}
																<EyeOffIcon data-icon="inline-start" />
																{t('compose.files.hideValues')}
															{/if}
														</Button>
													</div>
													{#if maskEnvValues}
														<Textarea
															id="compose-env-editor"
															value={maskedEnvPreview}
															readonly
															rows={18}
															class="max-h-[50dvh] min-h-40 resize-y overflow-y-auto bg-muted/40 font-mono text-xs leading-relaxed sm:min-h-72"
															placeholder={t('compose.files.envEmptyPlaceholder')}
														/>
														<Field.FieldDescription>
															{t('compose.files.envMaskHint1')} <strong>{t('compose.files.envMaskHintBold')}</strong> {t('compose.files.envMaskHint2')} <code>.env</code>.
														</Field.FieldDescription>
													{:else}
														<Textarea
															id="compose-env-editor"
															bind:value={envContent}
															rows={18}
															class="max-h-[50dvh] min-h-40 resize-y overflow-y-auto font-mono text-xs leading-relaxed sm:min-h-72"
															placeholder={t('compose.files.envPlaceholder')}
															spellcheck={false}
														/>
													{/if}
												</Field.Field>
											</Field.FieldGroup>
										{/if}
									</Tabs.Content>

									<!-- TAB 3: Logs -->
									<Tabs.Content value="logs" class="mt-4 flex flex-col gap-3">
										<div class="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
											<div class="grid w-full grid-cols-2 gap-2 sm:flex sm:w-auto sm:flex-wrap sm:items-center">
												<Select.Root
													type="single"
													value={logServiceFilter}
													onValueChange={(v) => {
														logServiceFilter = v ?? '';
														if (liveFollow) {
															stopLogStream();
															toggleLiveLogs();
														} else if (selectedDetail) {
															void loadProjectLogs(selectedDetail.name);
														}
													}}
												>
													<Select.Trigger size="sm" class="h-9 w-full min-w-0 sm:h-7 sm:w-44" aria-label={t('compose.logs.filterServiceAria')}>
														<span class="truncate">{logServiceFilter || t('compose.logs.allServices')}</span>
													</Select.Trigger>
													<Select.Content>
														<Select.Group>
															<Select.Item value="">{t('compose.logs.allServices')}</Select.Item>
															{#each selectedDetail.defined_services ?? [] as svc (svc.name)}
																<Select.Item value={svc.name}>{svc.name}</Select.Item>
															{/each}
														</Select.Group>
													</Select.Content>
												</Select.Root>

												<Select.Root
													type="single"
													value={String(logTail)}
													onValueChange={(v) => {
														if (!v) return;
														logTail = Number(v) || 200;
														if (selectedDetail && !liveFollow) {
															void loadProjectLogs(selectedDetail.name);
														}
													}}
												>
													<Select.Trigger size="sm" class="h-9 w-full min-w-0 sm:h-7 sm:w-32" aria-label={t('compose.logs.lineCountAria')}>
														<span class="truncate">{t('compose.logs.linesCount', { count: logTail })}</span>
													</Select.Trigger>
													<Select.Content>
														<Select.Group>
															<Select.Item value="100">{t('compose.logs.linesCount', { count: 100 })}</Select.Item>
															<Select.Item value="200">{t('compose.logs.linesCount', { count: 200 })}</Select.Item>
															<Select.Item value="500">{t('compose.logs.linesCount', { count: 500 })}</Select.Item>
														</Select.Group>
													</Select.Content>
												</Select.Root>
											</div>

											<div class="grid w-full grid-cols-3 gap-2 sm:flex sm:w-auto sm:flex-wrap sm:items-center">
												<Button
													variant={liveFollow ? 'destructive' : 'default'}
													size="sm"
													class="min-h-9 sm:min-h-0"
													onclick={toggleLiveLogs}
												>
													{#if liveFollow}
														<SquareIcon data-icon="inline-start" />
														<span class="truncate">{t('compose.logs.stop')}</span>
													{:else}
														<PlayIcon data-icon="inline-start" />
														<span class="truncate">{t('compose.logs.live')}</span>
													{/if}
												</Button>
												<Button
													variant="outline"
													size="sm"
													class="min-h-9 sm:min-h-0"
													disabled={logsLoading || liveFollow}
													onclick={() => selectedDetail && loadProjectLogs(selectedDetail.name)}
												>
													<RefreshCwIcon class={cn(logsLoading && 'animate-spin')} data-icon="inline-start" />
													<span class="truncate">{t('compose.logs.refresh')}</span>
												</Button>
												<Button
													variant="outline"
													size="sm"
													class="min-h-9 sm:min-h-0"
													onclick={() => copyText(logsText, t('compose.logs.copyLabel'))}
												>
													<CopyIcon data-icon="inline-start" />
													<span class="truncate">{t('compose.logs.copy')}</span>
												</Button>
											</div>
										</div>

										<pre
											class="max-h-[52dvh] min-h-52 overflow-auto overscroll-contain rounded-lg border bg-terminal p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words text-terminal-foreground sm:max-h-[460px] sm:min-h-[260px]"
										>{logsLoading && !logsText ? t('compose.logs.loading') : logsText || t('compose.logs.empty')}</pre>
									</Tabs.Content>
								</Tabs.Root>
							</Card.Content>
						</Card.Root>
					{/if}
				</div>
			</div>
		{/if}
	{/if}
</div>

<!-- Create / Register Compose Modal -->
<CreateComposeModal
	bind:open={createOpen}
	{templates}
	initialDir={createInitialDir}
	onCreated={async (proj) => {
		selectedName = proj.name;
		await loadStatus(true);
	}}
/>

<!-- Expose Port to Nginx VHost Modal -->
<ExposeComposeModal
	bind:open={exposeOpen}
	project={selectedDetail}
	port={exposePort}
	serviceName={exposeService}
	onExposed={() => loadStatus(true)}
/>

<!-- Confirm Compose Down Dialog -->
<Dialog.Root bind:open={downOpen}>
	<Dialog.Content class="flex max-h-[90dvh] max-w-[calc(100vw-1.5rem)] flex-col gap-0 overflow-hidden p-4 sm:max-w-[min(calc(100vw-2rem),28rem)]">
		<Dialog.Header class="shrink-0 border-b border-border/50 pb-3 pr-8">
			<Dialog.Title class="leading-snug break-words">{t('compose.downDialog.title', { name: selectedDetail?.name ?? '' })}</Dialog.Title>
			<Dialog.Description class="text-xs sm:text-sm">
				{t('compose.downDialog.desc1')}<code>docker compose down</code>{t('compose.downDialog.desc2')}
			</Dialog.Description>
		</Dialog.Header>
		<div class="-mx-1 min-h-0 flex-1 overflow-y-auto overscroll-contain px-1 py-4">
			<Field.FieldGroup class="gap-3">
				<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
					<Checkbox id="compose-down-orphans" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={downOrphans} />
					<Field.FieldLabel for="compose-down-orphans" class="cursor-pointer text-xs leading-snug font-normal">
						<span>{t('compose.downDialog.orphans1')}<code>--remove-orphans</code>{t('compose.downDialog.orphans2')}</span>
					</Field.FieldLabel>
				</Field.Field>
				<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
					<Checkbox id="compose-down-volumes" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={downVolumes} />
					<Field.FieldLabel
						for="compose-down-volumes"
						class="cursor-pointer text-xs leading-snug font-normal text-destructive"
					>
						<span>{t('compose.downDialog.volumes1')}<code>--volumes</code>{t('compose.downDialog.volumes2')}</span>
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>
		</div>
		<Dialog.Footer class="shrink-0 pb-[max(1rem,env(safe-area-inset-bottom,0px))]">
			<Button variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (downOpen = false)}>{t('compose.downDialog.cancel')}</Button>
			<Button variant="destructive" class="min-h-10 sm:min-h-0" onclick={confirmDownSubmit}>
				{t('compose.downDialog.confirm')}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Resolved JSON Config Modal -->
<Dialog.Root bind:open={configModalOpen}>
	<Dialog.Content class="flex max-h-[90dvh] max-w-[calc(100vw-1.5rem)] flex-col gap-0 overflow-hidden p-4 sm:max-w-[min(calc(100vw-2rem),48rem)]">
		<Dialog.Header class="shrink-0 border-b border-border/50 pb-3 pr-8">
			<Dialog.Title class="leading-snug">{t('compose.configDialog.title')}</Dialog.Title>
			<Dialog.Description class="text-xs sm:text-sm">
				{t('compose.configDialog.desc1')}<code>docker compose config --format json</code>{t('compose.configDialog.desc2')}<code>.env</code>{t('compose.configDialog.desc3')}<code>include</code>{t('compose.configDialog.desc4')}
			</Dialog.Description>
		</Dialog.Header>
		<div class="my-3 min-h-0 min-w-0 flex-1 overflow-auto overscroll-contain rounded-lg border bg-muted/40 p-3">
			{#if configLoading}
				<div class="flex items-center gap-2 py-8 text-sm">
					<Spinner />
					<span>{t('compose.configDialog.rendering')}</span>
				</div>
			{:else}
				<pre class="font-mono text-xs leading-relaxed whitespace-pre">{configJSONText}</pre>
			{/if}
		</div>
		<Dialog.Footer class="shrink-0 pb-[max(1rem,env(safe-area-inset-bottom,0px))]">
			<Button
				variant="outline"
				class="min-h-10 sm:min-h-0"
				onclick={() => copyText(configJSONText, t('compose.configDialog.copyLabel'))}
			>
				<CopyIcon data-icon="inline-start" />
				{t('compose.configDialog.copyJson')}
			</Button>
			<Button class="min-h-10 sm:min-h-0" onclick={() => (configModalOpen = false)}>{t('compose.configDialog.close')}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Docker Disk Usage & Prune Modal -->
<Dialog.Root bind:open={pruneModalOpen}>
	<Dialog.Content class="flex max-h-[90dvh] max-w-[calc(100vw-1.5rem)] flex-col gap-0 overflow-hidden p-4 sm:max-w-[min(calc(100vw-2rem),36rem)]">
		<Dialog.Header class="shrink-0 border-b border-border/50 pb-3 pr-8">
			<Dialog.Title class="leading-snug">{t('compose.pruneDialog.title')}</Dialog.Title>
			<Dialog.Description class="text-xs sm:text-sm">
				{t('compose.pruneDialog.desc1')}<code>docker system df</code>{t('compose.pruneDialog.desc2')}
			</Dialog.Description>
		</Dialog.Header>

		<div class="-mx-1 flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain px-1 py-4">
			{#if dfLoading}
				<div class="flex items-center gap-2 py-6 text-sm">
					<Spinner />
					<span>{t('compose.pruneDialog.loading')}</span>
				</div>
			{:else if diskUsage?.items?.length}
				<!-- Mobile compact cards -->
				<div class="flex flex-col gap-2 sm:hidden">
					{#each diskUsage.items as item (item.type)}
						<Card.Root size="sm">
							<Card.Header class="pb-1">
								<Card.Title class="text-sm font-semibold">{item.type}</Card.Title>
								<Card.Action>
									<Badge variant="secondary" class="font-mono">{item.size}</Badge>
								</Card.Action>
							</Card.Header>
							<Card.Content class="grid grid-cols-3 gap-2 text-xs">
								<div class="flex flex-col gap-0.5">
									<span class="text-[11px] text-muted-foreground">{t('compose.pruneDialog.total')}</span>
									<span class="font-mono font-medium">{item.total_count}</span>
								</div>
								<div class="flex flex-col gap-0.5">
									<span class="text-[11px] text-muted-foreground">{t('compose.pruneDialog.active')}</span>
									<span class="font-mono font-medium">{item.active}</span>
								</div>
								<div class="flex flex-col gap-0.5">
									<span class="text-[11px] text-muted-foreground">{t('compose.pruneDialog.reclaimable')}</span>
									<span class="font-mono font-medium">{item.reclaimable}</span>
								</div>
							</Card.Content>
						</Card.Root>
					{/each}
				</div>

				<!-- Desktop table -->
				<div class="hidden sm:block">
					<ResponsiveTableWrapper label={t('compose.pruneDialog.tableAria')} stackOnMobile={false}>
						<Table.Root>
							<Table.Header>
								<Table.Row>
									<Table.Head>{t('compose.pruneDialog.colType')}</Table.Head>
									<Table.Head>{t('compose.pruneDialog.colTotal')}</Table.Head>
									<Table.Head>{t('compose.pruneDialog.colActive')}</Table.Head>
									<Table.Head>{t('compose.pruneDialog.colSize')}</Table.Head>
									<Table.Head>{t('compose.pruneDialog.colReclaimable')}</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each diskUsage.items as item (item.type)}
									<Table.Row>
										<Table.Cell data-label={t('compose.pruneDialog.colType')} class="font-medium">{item.type}</Table.Cell>
										<Table.Cell data-label={t('compose.pruneDialog.colTotal')} class="font-mono text-xs">{item.total_count}</Table.Cell>
										<Table.Cell data-label={t('compose.pruneDialog.colActive')} class="font-mono text-xs">{item.active}</Table.Cell>
										<Table.Cell data-label={t('compose.pruneDialog.colSize')} class="font-mono text-xs">{item.size}</Table.Cell>
										<Table.Cell data-label={t('compose.pruneDialog.colReclaimable')} class="font-mono text-xs">{item.reclaimable}</Table.Cell>
									</Table.Row>
								{/each}
							</Table.Body>
						</Table.Root>
					</ResponsiveTableWrapper>
				</div>
			{:else}
				<Empty.Root class="border py-6">
					<Empty.Header>
						<Empty.Media variant="icon">
							<HardDriveIcon />
						</Empty.Media>
						<Empty.Title>{t('compose.pruneDialog.emptyTitle')}</Empty.Title>
						<Empty.Description>
							{t('compose.pruneDialog.emptyDesc')}
						</Empty.Description>
					</Empty.Header>
				</Empty.Root>
			{/if}

			<Separator />

			<div class="flex flex-col gap-2">
				<span class="text-xs font-medium text-muted-foreground">{t('compose.pruneDialog.pruneSection')}</span>
				<div class="grid grid-cols-1 gap-2 min-[380px]:grid-cols-2">
					<Button
						variant="outline"
						size="sm"
						class="min-h-9 sm:min-h-0"
						onclick={() => triggerPrune('containers', t('compose.pruneDialog.pruneContainersLabel'))}
					>
						{t('compose.pruneDialog.pruneContainers')}
					</Button>
					<Button
						variant="outline"
						size="sm"
						class="min-h-9 sm:min-h-0"
						onclick={() => triggerPrune('images', t('compose.pruneDialog.pruneImagesLabel'))}
					>
						{t('compose.pruneDialog.pruneImages')}
					</Button>
					<Button
						variant="outline"
						size="sm"
						class="min-h-9 sm:min-h-0"
						onclick={() => triggerPrune('builder', t('compose.pruneDialog.pruneBuilderLabel'))}
					>
						{t('compose.pruneDialog.pruneBuilder')}
					</Button>
					<Button
						variant="outline"
						size="sm"
						class="min-h-9 sm:min-h-0"
						onclick={() => triggerPrune('networks', t('compose.pruneDialog.pruneNetworksLabel'))}
					>
						{t('compose.pruneDialog.pruneNetworks')}
					</Button>
					<Button
						variant="destructive"
						size="sm"
						class="min-h-9 min-[380px]:col-span-2 sm:min-h-0"
						onclick={() => triggerPrune('volumes', t('compose.pruneDialog.pruneVolumesLabel'))}
					>
						{t('compose.pruneDialog.pruneVolumes')}
					</Button>
				</div>
			</div>
		</div>

		<Dialog.Footer class="shrink-0 pb-[max(1rem,env(safe-area-inset-bottom,0px))]">
			<Button variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (pruneModalOpen = false)}>
				{t('compose.pruneDialog.close')}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
