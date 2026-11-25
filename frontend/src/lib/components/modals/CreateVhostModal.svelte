<script lang="ts">
	import { tick, untrack } from 'svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Collapsible from '#lib/components/ui/collapsible/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import FolderPickerDialog from '#lib/components/modals/FolderPickerDialog.svelte';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import type { FileRootInfo, SiteAppInspectResult, SiteRecord } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import HammerIcon from '@lucide/svelte/icons/hammer';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import SaveIcon from '@lucide/svelte/icons/save';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';

	let {
		open = $bindable(false),
		pdnsZones = [],
		parentDomains = [],
		onRefreshZones,
		onCreated,
		siteToEdit = null,
		onUpdated
	}: {
		open: boolean;
		pdnsZones: string[];
		parentDomains: string[];
		onRefreshZones: () => void | Promise<void>;
		onCreated?: (opts: { domain: string; wantVPSLetsEncrypt: boolean }) => void | Promise<void>;
		siteToEdit?: SiteRecord | null;
		onUpdated?: (site: SiteRecord) => void | Promise<void>;
	} = $props();

	const isEdit = $derived(Boolean(siteToEdit));

	let fileRoots = $state<FileRootInfo[]>([]);
	let folderPickerOpen = $state(false);
	let creating = $state(false);
	let formError = $state('');

	let domainMode = $state<'custom' | 'subdomain'>('custom');
	let domain = $state('');
	let subdomainPrefix = $state('');
	let parentDomain = $state('');
	let domainTouched = $state(false);

	let framework = $state('laravel');
	let phpVersion = $state('8.4');
	let rootPath = $state('');
	let selectedDriveRoot = $state<string | null>(null);
	let proxyPortStr = $state('3000');
	let prevOpen = false;

	let sslEnabled = $state(true);
	let edgeExposeEnabled = $state(true);
	let edgeSSLEnabled = $state(true);
	let addLocalHosts = $state(false);

	// Managed App (Node.js / Go / Rust via systemd) state
	let buildCommand = $state('');
	let startCommand = $state('');
	let runUser = $state('');
	let autoStart = $state(true);
	let runBuildNow = $state(true);
	let useProjectEnv = $state(false);
	let portTouched = $state(false);
	let portConflictHint = $state('');
	let inspectResult = $state<SiteAppInspectResult | null>(null);
	let inspectLoading = $state(false);
	let portSuggesting = $state(false);
	let advancedAppOpen = $state(false);

	const webFrameworks = $derived([
		{ value: 'laravel', label: t('webstack.createModal.frameworks.laravel') },
		{ value: 'wordpress', label: t('webstack.createModal.frameworks.wordpress') },
		{ value: 'ci4', label: t('webstack.createModal.frameworks.ci4') },
		{ value: 'ci3', label: t('webstack.createModal.frameworks.ci3') },
		{ value: 'symfony', label: t('webstack.createModal.frameworks.symfony') },
		{ value: 'generic', label: t('webstack.createModal.frameworks.generic') },
		{ value: 'static', label: t('webstack.createModal.frameworks.static') }
	]);

	const managedAppFrameworks = $derived([
		{ value: 'nodejs', label: t('webstack.createModal.frameworks.nodejs') },
		{ value: 'go', label: t('webstack.createModal.frameworks.go') },
		{ value: 'rust', label: t('webstack.createModal.frameworks.rust') }
	]);

	const proxyFrameworks = $derived([
		{ value: 'proxy', label: t('webstack.createModal.frameworks.proxy') }
	]);

	const frameworks = $derived([...webFrameworks, ...managedAppFrameworks, ...proxyFrameworks]);

	const phpVersions = ['8.4', '8.3', '8.2', '8.1'];

	const isProxy = $derived(framework === 'proxy');
	const isStatic = $derived(framework === 'static');
	const isManagedApp = $derived(framework === 'nodejs' || framework === 'go' || framework === 'rust');
	const usesProxyPort = $derived(isProxy || isManagedApp);
	const isVPS = $derived(modeStore.mode === 'vps');
	const isEdge = $derived(modeStore.mode === 'edge');
	const isLocal = $derived(modeStore.mode === 'local');
	const supportsDNS = $derived(isVPS || isEdge);

	function isLocalOnlyDomain(dom: string): boolean {
		const clean = dom.trim().replace(/\.$/, '').toLowerCase();
		return /\.(test|local|localhost|invalid|example|arpa|internal)$/.test(clean);
	}

	const domainChoices = $derived(isEdge ? pdnsZones : parentDomains);

	const detectedDrives = $derived.by(() => {
		const byPath = new Map<string, { name: string; path: string }>();
		for (const r of fileRoots) {
			if (r.kind === 'drive') {
				byPath.set(r.path, { name: r.name, path: r.path });
			}
		}
		for (const d of cp.systemStatus?.mount_drives ?? []) {
			if (!byPath.has(d.mount_point)) {
				byPath.set(d.mount_point, { name: d.name, path: d.mount_point });
			}
		}
		return Array.from(byPath.values());
	});

	const cleanSubPrefix = $derived(
		subdomainPrefix.trim().replace(/^\.+|\.+$/g, '').toLowerCase()
	);

	const effectiveDomain = $derived(
		isEdge
			? !parentDomain
				? ''
				: cleanSubPrefix
					? `${cleanSubPrefix}.${parentDomain}`
					: parentDomain
			: isVPS && domainMode === 'subdomain'
				? cleanSubPrefix && parentDomain
					? `${cleanSubPrefix}.${parentDomain}`
					: ''
				: domain.trim().replace(/^\.+|\.+$/g, '').toLowerCase()
	);

	const domainInvalid = $derived(
		domainTouched &&
			(isEdge
				? !parentDomain
				: isVPS && domainMode === 'subdomain'
					? cleanSubPrefix.length === 0 || !parentDomain
					: effectiveDomain.length === 0)
	);

	const activePreset = $derived.by(() => {
		if (framework === 'laravel' && phpVersion === '8.4') return 'laravel';
		if (framework === 'wordpress' && phpVersion === '8.4') return 'wordpress';
		if (framework === 'nodejs') return 'nodejs';
		if (framework === 'go') return 'go';
		if (framework === 'rust') return 'rust';
		if (framework === 'proxy') return 'proxy';
		if (framework === 'static') return 'static';
		return 'custom';
	});

	const metaSubtitle = $derived.by(() => {
		const dom = effectiveDomain || (isLocal ? '<domain>.test' : '<domain>');
		const portDisplay = proxyPortStr.trim() || '3100';
		const targetDesc = isManagedApp
			? `systemd → 127.0.0.1:${portDisplay}`
			: isProxy
				? `http://127.0.0.1:${proxyPortStr.trim() || '3000'}`
				: rootPath.trim()
					? rootPath.trim()
					: `/var/www/${effectiveDomain || '<domain>'}`;
		if (isEdge) {
			return t('webstack.createModal.subtitleEdge', {
				domain: dom,
				target: targetDesc,
				ipPart: modeStore.publicIP ? ` (${modeStore.publicIP})` : ''
			});
		}
		if (isVPS) {
			return t('webstack.createModal.subtitleVps', { domain: dom, target: targetDesc });
		}
		return t('webstack.createModal.subtitleLocal', { domain: dom, target: targetDesc });
	});

	async function refreshZonesQuiet() {
		if (!supportsDNS) return;
		await onRefreshZones();
		if (!parentDomain && domainChoices.length > 0) {
			parentDomain = domainChoices[0];
		}
	}

	async function fillSuggestedFreePort(force = false) {
		if (portSuggesting) return;
		if (!force && isEdit && siteToEdit?.proxy_port) return;
		portSuggesting = true;
		try {
			const res = await api.suggestSitePort();
			if (res?.port) {
				proxyPortStr = String(res.port);
			}
		} catch {
			if (!proxyPortStr.trim()) proxyPortStr = '3100';
		} finally {
			portSuggesting = false;
		}
	}

	function applyManagedAppDefaults(rt: 'nodejs' | 'go' | 'rust', onlyIfEmpty = true) {
		if (rt === 'nodejs') {
			if (!onlyIfEmpty || !startCommand.trim()) startCommand = 'pnpm run start';
		} else if (rt === 'go') {
			if (!onlyIfEmpty || !buildCommand.trim()) buildCommand = 'go build -o ./bin/app .';
			if (!onlyIfEmpty || !startCommand.trim()) startCommand = './bin/app';
		} else if (rt === 'rust') {
			if (!onlyIfEmpty || !buildCommand.trim()) buildCommand = 'cargo build --release';
			if (!onlyIfEmpty || !startCommand.trim()) startCommand = './target/release/app';
		}
		runBuildNow = Boolean(buildCommand.trim());
	}

	async function inspectFolder(dirPath: string, applyDetected = true) {
		const target = dirPath.trim();
		if (!target) return;
		inspectLoading = true;
		portConflictHint = '';
		try {
			const res = await api.inspectSiteDir(target);
			inspectResult = res.inspect;
			portConflictHint = res.port_conflict_hint || '';
			if (!isEdit && res.suggested_port && (!portTouched || !proxyPortStr.trim() || proxyPortStr === '3000')) {
				proxyPortStr = String(res.suggested_port);
			}
			if (res.inspect?.has_dot_env) {
				useProjectEnv = true;
			} else if (!isEdit) {
				useProjectEnv = false;
			}
			if (applyDetected && res.inspect?.detected) {
				if (res.inspect.runtime) {
					framework = res.inspect.runtime;
				}
				if (res.inspect.recommended_build !== undefined) {
					buildCommand = res.inspect.recommended_build || '';
				}
				if (res.inspect.recommended_start) {
					startCommand = res.inspect.recommended_start;
				}
				runBuildNow = Boolean(buildCommand.trim());
			}
		} catch (err) {
			inspectResult = null;
			toast.error(err instanceof Error ? err.message : t('webstack.createModal.inspectFailedToast'));
		} finally {
			inspectLoading = false;
		}
	}

	function onFrameworkChanged(nextFw: string) {
		framework = nextFw;
		formError = '';
		if (nextFw === 'nodejs' || nextFw === 'go' || nextFw === 'rust') {
			applyManagedAppDefaults(nextFw, true);
			if (!isEdit && (!proxyPortStr.trim() || proxyPortStr === '3000')) {
				void fillSuggestedFreePort(true);
			}
			if (rootPath.trim() && !inspectResult) {
				void inspectFolder(rootPath.trim(), false);
			}
		} else if (nextFw === 'proxy' && !proxyPortStr.trim()) {
			proxyPortStr = '3000';
		}
	}

	function applyPreset(preset: 'laravel' | 'wordpress' | 'nodejs' | 'go' | 'rust' | 'proxy' | 'static') {
		formError = '';
		if (preset === 'laravel' || preset === 'wordpress') {
			framework = preset;
			phpVersion = '8.4';
			return;
		}
		if (preset === 'nodejs' || preset === 'go' || preset === 'rust') {
			buildCommand = '';
			startCommand = '';
			onFrameworkChanged(preset);
			return;
		}
		framework = preset;
		if (preset === 'proxy' && !proxyPortStr.trim()) {
			proxyPortStr = '3000';
		}
	}

	function switchDomainMode(nextMode: string) {
		if (nextMode !== 'custom' && nextMode !== 'subdomain') return;
		domainMode = nextMode;
		domainTouched = false;
		formError = '';
		if (nextMode === 'subdomain') {
			if (!parentDomain && domainChoices.length > 0) {
				parentDomain = domainChoices[0];
			}
			void refreshZonesQuiet();
		}
	}

	$effect(() => {
		const isOpen = open;
		if (isOpen && !prevOpen) {
			prevOpen = true;
			untrack(() => {
				if (siteToEdit) {
					domainMode = 'custom';
					domain = siteToEdit.domain;
					subdomainPrefix = '';
					parentDomain = '';
					domainTouched = false;
					formError = '';
					framework = siteToEdit.framework || 'laravel';
					phpVersion = siteToEdit.php_version || '8.4';
					rootPath = siteToEdit.app?.working_dir || siteToEdit.root_path || '';
					selectedDriveRoot = null;
					proxyPortStr = siteToEdit.proxy_port ? String(siteToEdit.proxy_port) : '3100';
					sslEnabled = Boolean(siteToEdit.ssl_enabled);
					edgeExposeEnabled = isEdge && Boolean(siteToEdit.exposed_to);
					edgeSSLEnabled = true;
					addLocalHosts = false;
					buildCommand = siteToEdit.app?.build_command || '';
					startCommand = siteToEdit.app?.start_command || '';
					runUser = siteToEdit.app?.run_user || '';
					autoStart = siteToEdit.app?.auto_start ?? true;
					runBuildNow = false;
					useProjectEnv = Boolean(siteToEdit.app?.use_project_env);
					portTouched = Boolean(siteToEdit.proxy_port);
					portConflictHint = '';
					inspectResult = null;
					advancedAppOpen = Boolean(siteToEdit.app?.run_user);
				} else {
					domainMode = isEdge ? 'subdomain' : 'custom';
					domain = '';
					subdomainPrefix = '';
					parentDomain = domainChoices[0] ?? '';
					domainTouched = false;
					formError = '';
					framework = 'laravel';
					phpVersion = '8.4';
					rootPath = '';
					selectedDriveRoot = null;
					proxyPortStr = '3000';
					sslEnabled = isLocal;
					edgeExposeEnabled = isEdge;
					edgeSSLEnabled = true;
					addLocalHosts = false;
					buildCommand = '';
					startCommand = '';
					runUser = '';
					autoStart = true;
					runBuildNow = true;
					useProjectEnv = false;
					portTouched = false;
					portConflictHint = '';
					inspectResult = null;
					advancedAppOpen = false;
				}

				void api
					.getFileRoots()
					.then((rootsRes) => {
						fileRoots = rootsRes ?? [];
					})
					.catch(() => {});
				if (supportsDNS && !siteToEdit) {
					void refreshZonesQuiet();
				}
				void tick().then(() => {
					if (!siteToEdit) {
						const targetId =
							isEdge || (isVPS && domainMode === 'subdomain') ? 'site-sub-prefix' : 'site-domain';
						document.getElementById(targetId)?.focus();
					} else {
						document.getElementById('site-framework')?.focus();
					}
				});
			});
		} else if (!isOpen) {
			prevOpen = false;
		}
	});

	$effect(() => {
		if (!siteToEdit && domainChoices.length > 0 && (!parentDomain || !domainChoices.includes(parentDomain))) {
			parentDomain = domainChoices[0];
		}
	});

	$effect(() => {
		const drv = selectedDriveRoot;
		if (!drv) return;
		rootPath = effectiveDomain ? `${drv}/${effectiveDomain}` : drv;
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		domainTouched = true;
		formError = '';

		if (!isEdit && (isEdge || (isVPS && domainMode === 'subdomain'))) {
			if (!parentDomain) {
				formError = isEdge
					? t('webstack.createModal.errNeedEdgeZone')
					: t('webstack.createModal.errNeedParentOrMain');
				document.getElementById('site-parent-domain')?.focus();
				return;
			}
			if (!isEdge && !cleanSubPrefix) {
				formError = t('webstack.createModal.errSubPrefixRequired');
				document.getElementById('site-sub-prefix')?.focus();
				return;
			}
			if (
				cleanSubPrefix &&
				!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*$/.test(cleanSubPrefix)
			) {
				formError = t('webstack.createModal.errInvalidSubPrefix');
				document.getElementById('site-sub-prefix')?.focus();
				return;
			}
		}

		const cleanDomain = isEdit && siteToEdit ? siteToEdit.domain : effectiveDomain;
		if (!cleanDomain) {
			formError = t('webstack.createModal.errDomainRequired');
			document.getElementById('site-domain')?.focus();
			return;
		}

		const proxyPort = parseInt(proxyPortStr, 10) || 0;
		if (usesProxyPort && (proxyPort < 1 || proxyPort > 65535)) {
			formError = t('webstack.createModal.errPortRange');
			document.getElementById('site-proxy-port')?.focus();
			return;
		}
		if (isManagedApp) {
			if (!rootPath.trim()) {
				formError = t('webstack.createModal.errProjectDirRequired');
				document.getElementById('site-root-path')?.focus();
				return;
			}
			if (!startCommand.trim()) {
				formError = t('webstack.createModal.errStartCmdRequired');
				document.getElementById('site-start-cmd')?.focus();
				return;
			}
		}

		const appConfigPayload = isManagedApp
			? {
					build_command: buildCommand.trim(),
					start_command: startCommand.trim(),
					...(runUser.trim() ? { run_user: runUser.trim() } : {}),
					auto_start: autoStart,
					run_build_now: runBuildNow && Boolean(buildCommand.trim()),
					use_project_env: useProjectEnv
				}
			: undefined;

		if (isEdit) {
			creating = true;
			try {
				const res = await cp.runWithSudoRetry(
					t('webstack.createModal.sudoUpdatePrompt', {
						appSuffix: isManagedApp ? t('webstack.createModal.sudoAppSuffix') : '',
						domain: cleanDomain
					}),
					(sudoPass) =>
						api.updateSite(cleanDomain, {
							domain: cleanDomain,
							framework,
							php_version: usesProxyPort || isStatic ? '' : phpVersion,
							root_path: rootPath.trim(),
							proxy_port: proxyPort,
							ssl_enabled: sslEnabled,
							...(appConfigPayload ? { app_config: appConfigPayload } : {}),
							sudo_pass: sudoPass
						})
				);
				if (res) {
					toast.success(t('webstack.createModal.updatedToast', { domain: cleanDomain }));
					open = false;
					if (res.build_task_id) {
						cp.openTerminal(t('webstack.createModal.buildTerminalTitle', { domain: cleanDomain }));
					} else if (res.build_error) {
						toast.warning(t('webstack.createModal.autoBuildWarning', { error: res.build_error }));
					}
					if (onUpdated) {
						await onUpdated(
							res.site ||
								({
									...siteToEdit,
									framework,
									php_version: usesProxyPort || isStatic ? '' : phpVersion,
									root_path: rootPath.trim(),
									proxy_port: proxyPort,
									ssl_enabled: sslEnabled
								} as SiteRecord)
						);
					}
				}
			} catch (err) {
				formError = err instanceof Error ? err.message : t('webstack.createModal.updateFailed');
			} finally {
				creating = false;
			}
			return;
		}

		const cleanIsLocal = isLocalOnlyDomain(cleanDomain);
		const wantVPSLetsEncrypt = isVPS && sslEnabled && !cleanIsLocal;
		const wantEdgeTunnel = isEdge && edgeExposeEnabled;

		creating = true;
		try {
			const res = await cp.runWithSudoRetry(
				t('webstack.createModal.sudoCreatePrompt', {
					domain: cleanDomain,
					appSuffix: isManagedApp ? t('webstack.createModal.sudoAppSuffix') : ''
				}),
				(sudoPass) =>
					api.createSite({
						domain: cleanDomain,
						framework,
						php_version: usesProxyPort || isStatic ? '' : phpVersion,
						root_path: rootPath.trim(),
						proxy_port: proxyPort,
						ssl_enabled: wantVPSLetsEncrypt || wantEdgeTunnel ? false : sslEnabled,
						...(isEdge ? { add_hosts: addLocalHosts } : {}),
						...(wantEdgeTunnel
							? {
									expose_edge: true,
									edge_domain: cleanDomain,
									edge_ssl: edgeSSLEnabled
								}
							: {}),
						...(appConfigPayload ? { app_config: appConfigPayload } : {}),
						...(sudoPass ? { sudo_pass: sudoPass } : {})
					})
			);
			if (!res) return;

			if (res?.edge_error) {
				toast.warning(
					t('webstack.createModal.edgeExposeWarning', { domain: cleanDomain, error: res.edge_error })
				);
			} else if (res?.edge_exposed) {
				toast.success(
					t('webstack.createModal.createdEdgeToast', {
						domain: cleanDomain,
						edgeDomain: res.edge_domain || cleanDomain,
						dnsSuffix: res.dns_synced ? t('webstack.createModal.dnsSuffixActive') : ''
					})
				);
			} else if (res?.dns_synced && res?.dns_message) {
				toast.success(t('webstack.createModal.createdDnsToast', { domain: cleanDomain, message: res.dns_message }));
			} else {
				toast.success(t('webstack.createModal.createdToast', { domain: cleanDomain }));
			}

			open = false;
			if (res?.build_task_id) {
				cp.openTerminal(t('webstack.createModal.buildTerminalTitle', { domain: cleanDomain }));
			} else if (res?.build_error) {
				toast.warning(t('webstack.createModal.createAutoBuildWarning', { error: res.build_error }));
			}
			if (onCreated) {
				await onCreated({ domain: cleanDomain, wantVPSLetsEncrypt });
			}
		} catch (err) {
			formError = err instanceof Error ? err.message : t('webstack.createModal.createFailed');
		} finally {
			creating = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="flex max-h-[92dvh] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col gap-0 overflow-hidden p-0 sm:w-full sm:max-w-[min(94vw,64rem)]">
		<Dialog.Header class="shrink-0 border-b px-4 pt-4 pb-2.5 pr-10 sm:px-6 sm:pt-5 sm:pb-3">
			<Dialog.Title class="flex min-w-0 items-center gap-2">
				{#if isEdit}
					<PencilIcon class="size-4 shrink-0 text-primary" />
					<span class="truncate text-base sm:text-lg">{t('webstack.createModal.titleEdit')}</span>
				{:else}
					<GlobeIcon class="size-4 shrink-0 text-primary" />
					<span class="truncate text-base sm:text-lg">{t('webstack.createModal.titleCreate')}</span>
				{/if}
			</Dialog.Title>
			<Dialog.Description class="line-clamp-2 break-all font-mono text-[11px] sm:line-clamp-none sm:text-xs">{metaSubtitle}</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={handleSubmit} class="flex min-h-0 flex-1 flex-col overflow-hidden">
			<div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-3 sm:px-6 sm:py-4">
				<div class="flex min-w-0 flex-col gap-4 sm:gap-5">
					{#if formError}
						<Alert.Root variant="destructive" role="alert">
							<AlertCircleIcon />
							<Alert.Title>{isEdit ? t('webstack.createModal.errorTitleEdit') : t('webstack.createModal.errorTitleCreate')}</Alert.Title>
							<Alert.Description class="break-words">{formError}</Alert.Description>
						</Alert.Root>
					{/if}

					<div class="flex flex-col gap-1.5 sm:flex-row sm:flex-wrap sm:items-center sm:gap-2">
						<span class="shrink-0 text-xs font-medium text-muted-foreground">{t('webstack.createModal.presetsLabel')}</span>
						<div class="-mx-1 flex items-center gap-1.5 overflow-x-auto px-1 pb-1 sm:mx-0 sm:flex-wrap sm:overflow-visible sm:px-0 sm:pb-0">
							<Button
								type="button"
								class="shrink-0"
								variant={activePreset === 'laravel' ? 'default' : 'outline'}
								size="xs"
								onclick={() => applyPreset('laravel')}
							>
								{t('webstack.createModal.presetLaravel')}
							</Button>
							<Button
								type="button"
								class="shrink-0"
								variant={activePreset === 'wordpress' ? 'default' : 'outline'}
								size="xs"
								onclick={() => applyPreset('wordpress')}
							>
								{t('webstack.createModal.presetWordPress')}
							</Button>
							<Button
								type="button"
								class="shrink-0"
								variant={activePreset === 'nodejs' ? 'default' : 'outline'}
								size="xs"
								onclick={() => applyPreset('nodejs')}
							>
								{t('webstack.createModal.presetNode')}
							</Button>
							<Button
								type="button"
								class="shrink-0"
								variant={activePreset === 'go' ? 'default' : 'outline'}
								size="xs"
								onclick={() => applyPreset('go')}
							>
								{t('webstack.createModal.presetGo')}
							</Button>
							<Button
								type="button"
								class="shrink-0"
								variant={activePreset === 'rust' ? 'default' : 'outline'}
								size="xs"
								onclick={() => applyPreset('rust')}
							>
								{t('webstack.createModal.presetRust')}
							</Button>
							<Button
								type="button"
								class="shrink-0"
								variant={activePreset === 'proxy' ? 'default' : 'outline'}
								size="xs"
								onclick={() => applyPreset('proxy')}
							>
								{t('webstack.createModal.presetProxy')}
							</Button>
							<Button
								type="button"
								class="shrink-0"
								variant={activePreset === 'static' ? 'default' : 'outline'}
								size="xs"
								onclick={() => applyPreset('static')}
							>
								{t('webstack.createModal.presetStatic')}
							</Button>
							{#if activePreset === 'custom'}
								<Badge variant="outline" class="shrink-0">{t('webstack.createModal.presetCustom')}</Badge>
							{/if}
						</div>
					</div>

					<Field.FieldSet>
						<Field.FieldLegend variant="label">{t('webstack.createModal.legendDomain')}</Field.FieldLegend>
						<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
							{#if isEdit}
								<Field.Field class="sm:col-span-2">
									<Field.FieldLabel for="site-domain">{t('webstack.createModal.domainLockedLabel')}</Field.FieldLabel>
									<Input
										id="site-domain"
										name="domain"
										class="h-11 font-mono text-base bg-muted/50 cursor-not-allowed sm:h-9 sm:text-sm"
										value={domain}
										disabled
										readonly
									/>
									<Field.FieldDescription>
										{t('webstack.createModal.domainLockedDesc')}
									</Field.FieldDescription>
								</Field.Field>
							{:else}
							<Field.Field class="sm:col-span-2" data-invalid={domainInvalid ? 'true' : undefined}>
								<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
									<Field.FieldLabel
										for={isEdge || (isVPS && domainMode === 'subdomain') ? 'site-sub-prefix' : 'site-domain'}
									>
										{isLocal ? t('webstack.createModal.domainLocalLabel') : isEdge ? t('webstack.createModal.domainPdnsLabel') : t('webstack.createModal.domainLabel')}
									</Field.FieldLabel>
									{#if isEdge}
										<Badge variant="outline" class="w-fit text-[11px] font-medium">
											{t('webstack.createModal.zoneBadgeCount', { count: domainChoices.length })}
										</Badge>
									{:else if isVPS}
										<ToggleGroup.Root
											type="single"
											bind:value={() => domainMode, (v) => switchDomainMode(v)}
											variant="outline"
											spacing={2}
											class="grid w-full grid-cols-2 sm:inline-flex sm:w-auto"
											aria-label={t('webstack.createModal.domainModeAria')}
										>
											<ToggleGroup.Item value="custom" class="min-h-10 text-xs sm:min-h-8 sm:px-3">
												{t('webstack.createModal.modeMainDomain')}
											</ToggleGroup.Item>
											<ToggleGroup.Item value="subdomain" class="min-h-10 text-xs sm:min-h-8 sm:px-3">
												{t('webstack.createModal.modeSubdomainCount', { count: domainChoices.length })}
											</ToggleGroup.Item>
										</ToggleGroup.Root>
									{/if}
								</div>

								{#if isEdge || (isVPS && domainMode === 'subdomain')}
									{#if domainChoices.length === 0}
										<div class="flex flex-col gap-2 rounded-md border border-dashed bg-muted/40 px-3 py-3 text-xs text-muted-foreground">
											{#if isEdge}
												<p>
													{t('webstack.createModal.noEdgeZone1')}<strong>{t('webstack.createModal.noEdgeZoneBold')}</strong>{t('webstack.createModal.noEdgeZone2')}
												</p>
												<div>
													<Button size="sm" variant="outline" href="/powerdns">
														<NetworkIcon data-icon="inline-start" />
														{t('webstack.createModal.managePdnsZoneBtn')}
													</Button>
												</div>
											{:else}
												<p>
													{t('webstack.createModal.noVpsParent1')}<strong>{t('webstack.createModal.noVpsParentBold')}</strong>{t('webstack.createModal.noVpsParent2')}
												</p>
											{/if}
										</div>
									{:else}
										<div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)]">
											<InputGroup.Root class="h-11 min-w-0 sm:h-9">
												<InputGroup.Input
													id="site-sub-prefix"
													name="subdomain_prefix"
													placeholder={isEdge ? t('webstack.createModal.subPrefixPlaceholderEdge') : t('webstack.createModal.subPrefixPlaceholderVps')}
													class="h-full text-base md:text-sm"
													autocorrect="off"
													autocapitalize="none"
													spellcheck={false}
													enterkeyhint="next"
													aria-invalid={domainInvalid ? 'true' : undefined}
													bind:value={subdomainPrefix}
													onblur={() => (domainTouched = true)}
													oninput={() => {
														if (subdomainPrefix.trim()) domainTouched = false;
														formError = '';
													}}
													required={!isEdge}
												/>
												<InputGroup.Addon align="inline-end">
													<InputGroup.Text>.</InputGroup.Text>
												</InputGroup.Addon>
											</InputGroup.Root>

											<Select.Root
												type="single"
												value={parentDomain}
												onValueChange={(v) => {
													if (v) parentDomain = v;
													formError = '';
												}}
											>
												<Select.Trigger
													id="site-parent-domain"
													class="h-11 w-full text-base sm:h-9 sm:text-sm"
													aria-label={t('webstack.createModal.selectPdnsAria')}
												>
													<span class="truncate">{parentDomain || t('webstack.createModal.selectPdnsPlaceholder')}</span>
												</Select.Trigger>
												<Select.Content>
													<Select.Group>
														{#each domainChoices as pd (pd)}
															<Select.Item value={pd}>{pd}</Select.Item>
														{/each}
													</Select.Group>
												</Select.Content>
											</Select.Root>
										</div>
										<div class="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
											<span class="shrink-0">{t('webstack.createModal.resultDomainLabel')}</span>
											<code class="min-w-0 break-all rounded bg-muted px-1.5 py-0.5 font-mono text-foreground">
												{effectiveDomain || `<subdomain>.${parentDomain}`}
											</code>
										</div>
									{/if}
								{:else}
									<Input
										id="site-domain"
										name="domain"
										class="h-11 text-base sm:h-9 sm:text-sm"
										placeholder={isLocal ? t('webstack.createModal.domainPlaceholderLocal') : t('webstack.createModal.domainPlaceholderPublic')}
										autocorrect="off"
										autocapitalize="none"
										spellcheck={false}
										enterkeyhint="next"
										aria-invalid={domainInvalid ? 'true' : undefined}
										bind:value={domain}
										onblur={() => (domainTouched = true)}
										oninput={() => {
											if (domain.trim()) domainTouched = false;
											formError = '';
										}}
										required
									/>
								{/if}

								<Field.FieldDescription>
									{#if isEdge}
										{t('webstack.createModal.domainDescEdge')}
									{:else if isVPS && domainMode === 'subdomain'}
										{t('webstack.createModal.domainDescVpsSub')}
									{:else if isVPS}
										{t('webstack.createModal.domainDescVpsMain')}
									{:else}
										{t('webstack.createModal.domainDescLocal1')}<code>.test</code>{t('webstack.createModal.domainDescLocal2')}
									{/if}
								</Field.FieldDescription>
								{#if domainInvalid}
									<Field.FieldError
										errors={[
											{
												message: isEdge
													? t('webstack.createModal.errEdgeZoneMissing')
													: isVPS && domainMode === 'subdomain'
														? t('webstack.createModal.errSubRequired')
														: t('webstack.createModal.errDomainRequired')
											}
										]}
									/>
								{/if}
							</Field.Field>
							{/if}
						</Field.FieldGroup>
					</Field.FieldSet>

					<Separator />

					<Field.FieldSet class="min-w-0">
						<Field.FieldLegend variant="label">{t('webstack.createModal.legendRuntime')}</Field.FieldLegend>
						<Field.FieldGroup class="grid min-w-0 gap-4 sm:grid-cols-2">
							<Field.Field class="min-w-0">
								<Field.FieldLabel for="site-framework">{t('webstack.createModal.frameworkLabel')}</Field.FieldLabel>
								<Select.Root
									type="single"
									value={framework}
									onValueChange={(v) => {
										if (v) onFrameworkChanged(v);
									}}
								>
									<Select.Trigger id="site-framework" class="h-11 w-full text-base sm:h-9 sm:text-sm">
										<span class="truncate">{frameworks.find((f) => f.value === framework)?.label ?? framework}</span>
									</Select.Trigger>
									<Select.Content>
										<Select.Group>
											<Select.Label>{t('webstack.createModal.groupWebPhp')}</Select.Label>
											{#each webFrameworks as fw (fw.value)}
												<Select.Item value={fw.value}>{fw.label}</Select.Item>
											{/each}
										</Select.Group>
										<Select.Separator />
										<Select.Group>
											<Select.Label>{t('webstack.createModal.groupManagedApp')}</Select.Label>
											{#each managedAppFrameworks as fw (fw.value)}
												<Select.Item value={fw.value}>{fw.label}</Select.Item>
											{/each}
										</Select.Group>
										<Select.Separator />
										<Select.Group>
											<Select.Label>{t('webstack.createModal.groupProxy')}</Select.Label>
											{#each proxyFrameworks as fw (fw.value)}
												<Select.Item value={fw.value}>{fw.label}</Select.Item>
											{/each}
										</Select.Group>
									</Select.Content>
								</Select.Root>
							</Field.Field>

							{#if usesProxyPort}
								<Field.Field>
									<Field.FieldLabel for="site-proxy-port">{t('webstack.createModal.portLabel')}</Field.FieldLabel>
									<InputGroup.Root class="h-11 sm:h-9">
										<InputGroup.Input
											id="site-proxy-port"
											type="text"
											name="proxy_port"
											class="h-full font-mono text-base sm:text-sm"
											inputmode="numeric"
											pattern="[0-9]+"
											enterkeyhint="done"
											placeholder={isManagedApp ? '3100' : '3000'}
											bind:value={proxyPortStr}
											oninput={() => {
												formError = '';
												portTouched = true;
											}}
											required
										/>
										{#if isManagedApp}
											<InputGroup.Addon align="inline-end">
												<Button
													type="button"
													variant="ghost"
													size="xs"
													class="h-7 px-2 text-xs"
													disabled={portSuggesting}
													onclick={() => {
														portTouched = false;
														portConflictHint = '';
														void fillSuggestedFreePort(true);
													}}
												>
													{#if portSuggesting}
														<Spinner data-icon="inline-start" />
													{:else}
														{t('webstack.createModal.autoBtn')}
													{/if}
												</Button>
											</InputGroup.Addon>
										{/if}
									</InputGroup.Root>
									<Field.FieldDescription>
										{#if portConflictHint}
											<span class="font-medium text-warning">
												{t('webstack.createModal.portConflictSuggested', { hint: portConflictHint, port: proxyPortStr })}
											</span>
										{:else if isManagedApp}
											{inspectResult?.detected_port_hint
												? t('webstack.createModal.portDetectedHint', { port: inspectResult.detected_port_hint })
												: t('webstack.createModal.portManagedDefaultHint')}
										{:else}
											{t('webstack.createModal.portProxyExampleHint')}
										{/if}
									</Field.FieldDescription>
								</Field.Field>
							{:else if !isStatic}
								<Field.Field>
									<Field.FieldLabel for="site-php-ver">{t('webstack.createModal.phpVersionLabel')}</Field.FieldLabel>
									<Select.Root type="single" bind:value={phpVersion}>
										<Select.Trigger id="site-php-ver" class="h-11 w-full text-base sm:h-9 sm:text-sm">
											PHP {phpVersion}
										</Select.Trigger>
										<Select.Content>
											<Select.Group>
												{#each phpVersions as ver (ver)}
													<Select.Item value={ver}>PHP {ver}</Select.Item>
												{/each}
											</Select.Group>
										</Select.Content>
									</Select.Root>
									<Field.FieldDescription>{t('webstack.createModal.phpVersionDesc')}</Field.FieldDescription>
								</Field.Field>
							{:else}
								<div class="flex flex-col justify-center rounded-lg border bg-muted/25 px-3.5 py-2.5 text-xs">
									<span class="font-medium text-foreground">{t('webstack.createModal.staticModeTitle')}</span>
									<span class="mt-0.5 text-muted-foreground">
										{t('webstack.createModal.staticModeDesc1')}<code>index.html</code>{t('webstack.createModal.staticModeDesc2')}
									</span>
								</div>
							{/if}

							{#if !isProxy}
								<Field.Field class="sm:col-span-2">
									<Field.FieldLabel for="site-root-path">
										{isManagedApp ? t('webstack.createModal.rootDirRequired') : t('webstack.createModal.docRootOptional')}
									</Field.FieldLabel>
									<div class="flex flex-col gap-2 sm:flex-row sm:items-center">
										<Input
											id="site-root-path"
											name="root_path"
											class="h-11 min-w-0 flex-1 font-mono text-base sm:h-9 sm:text-xs"
											placeholder={isManagedApp
												? '/home/dietpi/apps/my-app'
												: effectiveDomain
													? `/var/www/${effectiveDomain}`
													: isLocal
														? t('webstack.createModal.placeholderDocRootLocal')
														: t('webstack.createModal.placeholderDocRootPublic')}
											autocorrect="off"
											autocapitalize="none"
											spellcheck={false}
											enterkeyhint="done"
											bind:value={rootPath}
											oninput={() => {
												selectedDriveRoot = null;
											}}
											required={isManagedApp}
										/>
										<div class="grid grid-cols-1 gap-2 sm:flex sm:shrink-0 sm:items-center">
											{#if isManagedApp}
												<Button
													type="button"
													variant="secondary"
													class="min-h-11 sm:min-h-0 sm:h-9"
													disabled={inspectLoading || !rootPath.trim()}
													onclick={() => inspectFolder(rootPath, true)}
												>
													{#if inspectLoading}
														<Spinner data-icon="inline-start" />
													{:else}
														<SparklesIcon data-icon="inline-start" />
													{/if}
													{t('webstack.createModal.inspectBtn')}
												</Button>
											{/if}
											<Button
												type="button"
												variant="outline"
												class="min-h-11 w-full shrink-0 sm:min-h-0 sm:h-9 sm:w-auto"
												onclick={() => (folderPickerOpen = true)}
											>
												<FolderOpenIcon data-icon="inline-start" />
												{t('webstack.createModal.browseFolderBtn')}
											</Button>
										</div>
									</div>
									{#if detectedDrives.length > 0 && !isManagedApp}
										<div class="flex flex-wrap items-center gap-1.5 pt-1">
											<span class="text-xs text-muted-foreground">{t('webstack.createModal.quickDriveLabel')}</span>
											<Button
												type="button"
												size="xs"
												variant={!rootPath.trim() ? 'secondary' : 'outline'}
												class="h-8 text-xs sm:h-7"
												onclick={() => {
													selectedDriveRoot = null;
													rootPath = '';
												}}
											>
												{t('webstack.createModal.defaultDriveBtn')}
											</Button>
											{#each detectedDrives as drv (drv.path)}
												{@const active = rootPath.trim() === drv.path || rootPath.trim().startsWith(drv.path + '/')}
												<Button
													type="button"
													size="xs"
													variant={active ? 'secondary' : 'outline'}
													class="h-8 max-w-full text-xs sm:h-7"
													onclick={() => {
														selectedDriveRoot = drv.path;
														rootPath = effectiveDomain ? `${drv.path}/${effectiveDomain}` : drv.path;
													}}
												>
													<HardDriveIcon data-icon="inline-start" />
													<span class="truncate">{drv.name}</span>
												</Button>
											{/each}
										</div>
									{/if}
									<Field.FieldDescription>
										{#if isManagedApp}
											{t('webstack.createModal.rootDescApp1')}<code>package.json</code>{t('webstack.createModal.rootDescApp2')}<code>go.mod</code>{t('webstack.createModal.rootDescAppOr')}<code>Cargo.toml</code>{t('webstack.createModal.rootDescApp3')}<code>/home</code>, <code>/var/www</code>, <code>/srv</code>{t('webstack.createModal.rootDescAppOr')}<code>/mnt</code>{t('webstack.createModal.rootDescApp4')}
										{:else}
											{t('webstack.createModal.rootDescWeb1')}<code>/var/www/&lt;domain&gt;</code>{t('webstack.createModal.rootDescWeb2')}<strong>{t('webstack.createModal.rootDescWebBold')}</strong>{t('webstack.createModal.rootDescWeb3')}
											{#if !isStatic}
												<code>/public</code>{t('webstack.createModal.rootDescWeb4')}
											{/if}
										{/if}
									</Field.FieldDescription>
								</Field.Field>
							{/if}

							{#if isManagedApp}
								{#if inspectResult?.detected}
									<div class="flex flex-col gap-2 rounded-lg border bg-muted/35 p-3 sm:col-span-2">
										<div class="flex flex-wrap items-center justify-between gap-2">
											<div class="flex flex-wrap items-center gap-2 text-xs">
												<CpuIcon class="size-3.5 text-primary" />
												<span class="font-medium text-foreground">
													{t('webstack.createModal.detectedLabel', { runtime: inspectResult.runtime?.toUpperCase() || '' })}
													{inspectResult.package_manager ? ` (${inspectResult.package_manager})` : ''}
												</span>
												{#if inspectResult.app_name}
													<Badge variant="outline" class="font-mono text-[11px]">{inspectResult.app_name}</Badge>
												{/if}
												{#if !inspectResult.toolchain_installed}
													<Badge variant="destructive" class="text-[11px]">{t('webstack.createModal.toolchainMissing')}</Badge>
												{/if}
											</div>
											{#if inspectResult.has_dot_env}
												<Badge variant="secondary" class="text-[11px]">
													{t('webstack.createModal.projectEnvVarsBadge', { count: inspectResult.dot_env_var_count ?? 0 })}
												</Badge>
											{/if}
										</div>

										{#if inspectResult.candidates && inspectResult.candidates.length > 0}
											<div class="flex flex-wrap items-center gap-1.5">
												<span class="text-[11px] text-muted-foreground">{t('webstack.createModal.quickModeLabel')}</span>
												{#each inspectResult.candidates as cand (cand.label)}
													{@const activeCand = startCommand.trim() === cand.start_command}
													<Button
														type="button"
														size="xs"
														variant={activeCand ? 'default' : 'outline'}
														class="h-7 text-[11px]"
														onclick={() => {
															buildCommand = cand.build_command || '';
															startCommand = cand.start_command;
															runBuildNow = Boolean(buildCommand.trim());
														}}
													>
														{cand.label}
													</Button>
												{/each}
											</div>
										{/if}
									</div>
								{/if}

								<Field.Field class="sm:col-span-2">
									<Field.FieldLabel for="site-build-cmd">{t('webstack.createModal.buildCmdLabel')}</Field.FieldLabel>
									<Input
										id="site-build-cmd"
										name="build_command"
										class="h-11 font-mono text-base sm:h-9 sm:text-xs"
										placeholder={framework === 'nodejs'
											? 'pnpm install && pnpm run build'
											: framework === 'go'
												? 'go build -o ./bin/app .'
												: 'cargo build --release'}
										autocorrect="off"
										autocapitalize="none"
										spellcheck={false}
										bind:value={buildCommand}
										oninput={() => {
											if (!buildCommand.trim()) runBuildNow = false;
										}}
									/>
									<Field.FieldDescription>
										{t('webstack.createModal.buildCmdDesc1')}<strong>{t('webstack.createModal.buildCmdDescBold')}</strong>{t('webstack.createModal.buildCmdDesc2')}
									</Field.FieldDescription>
								</Field.Field>

								<Field.Field class="min-w-0 sm:col-span-2">
									<Field.FieldLabel for="site-start-cmd">{t('webstack.createModal.startCmdLabel1')}<code>ExecStart</code>{t('webstack.createModal.startCmdLabel2')}</Field.FieldLabel>
									<Input
										id="site-start-cmd"
										name="start_command"
										class="h-11 font-mono text-base sm:h-9 sm:text-xs"
										placeholder={framework === 'nodejs'
											? t('webstack.createModal.placeholderNodeStart')
											: framework === 'go'
												? './bin/app'
												: './target/release/app'}
										autocorrect="off"
										autocapitalize="none"
										spellcheck={false}
										bind:value={startCommand}
										required
									/>
									<Field.FieldDescription>
										{t('webstack.createModal.startCmdDesc1')}<code>systemd</code>{t('webstack.createModal.startCmdDesc2')}<code>$PORT</code>{t('webstack.createModal.startCmdDesc3')}<code>pnpm run dev -- --port $PORT</code>{t('webstack.createModal.startCmdDesc4')}
									</Field.FieldDescription>
								</Field.Field>

								<div class="flex min-w-0 flex-col gap-3 rounded-lg border bg-muted/25 p-3 sm:col-span-2">
									<div class="flex items-center justify-between gap-3">
										<div class="flex min-w-0 flex-1 flex-col gap-0.5">
											<span class="text-xs font-medium text-foreground">{t('webstack.createModal.autoStartTitle')}</span>
											<span class="text-[11px] text-muted-foreground">{t('webstack.createModal.autoStartDesc1')}<code>systemctl enable</code>{t('webstack.createModal.autoStartDesc2')}</span>
										</div>
										<Switch class="shrink-0" bind:checked={autoStart} aria-label={t('webstack.createModal.autoStartAria')} />
									</div>

									{#if buildCommand.trim()}
										<div class="flex items-center justify-between gap-3 border-t pt-2.5">
											<div class="flex min-w-0 flex-1 flex-col gap-0.5">
												<span class="text-xs font-medium text-foreground">
													{isEdit ? t('webstack.createModal.rebuildAfterSave') : t('webstack.createModal.buildStartAfterCreate')}
												</span>
												<span class="text-[11px] text-muted-foreground">
													{t('webstack.createModal.rebuildLiveDesc')}
												</span>
											</div>
											<Switch class="shrink-0" bind:checked={runBuildNow} aria-label={t('webstack.createModal.buildNowAria')} />
										</div>
									{/if}

									<div class="flex items-center justify-between gap-3 border-t pt-2.5">
										<div class="flex min-w-0 flex-1 flex-col gap-0.5">
											<span class="text-xs font-medium text-foreground">
												{t('webstack.createModal.useProjectEnv1')}<code>.env</code>{t('webstack.createModal.useProjectEnv2')}
											</span>
											<span class="text-[11px] break-words text-muted-foreground">
												{#if useProjectEnv}
													<code>systemd</code>{t('webstack.createModal.useProjectEnvOn1')}<code class="break-all">{rootPath.trim() ? `${rootPath.trim().replace(/\/+$/, '')}/.env` : '<folder>/.env'}</code>{t('webstack.createModal.useProjectEnvOn2')}
												{:else if inspectResult?.has_dot_env}
													{t('webstack.createModal.useProjectEnvDetected1')}<code>.env</code>{t('webstack.createModal.useProjectEnvDetected2', { count: inspectResult.dot_env_var_count ?? 0 })}<code>.env</code>{t('webstack.createModal.useProjectEnvDetected3')}<code>/etc/wdev/apps/</code>.
												{:else}
													{t('webstack.createModal.useProjectEnvOff1')}<code>.env</code>{t('webstack.createModal.useProjectEnvOff2')}<code>/etc/wdev/apps/</code> (<code>0600</code>){t('webstack.createModal.useProjectEnvOff3')}
												{/if}
											</span>
										</div>
										<Switch class="shrink-0" bind:checked={useProjectEnv} aria-label={t('webstack.createModal.useProjectEnvAria')} />
									</div>
								</div>

								<Collapsible.Root bind:open={advancedAppOpen} class="sm:col-span-2">
									<Collapsible.Trigger class="flex w-full items-center justify-between rounded-md border bg-muted/20 px-3 py-2 text-xs font-medium text-muted-foreground hover:bg-muted/40 hover:text-foreground">
										<span class="flex items-center gap-1.5">
											<HammerIcon class="size-3.5" />
											{t('webstack.createModal.advancedSystemd')}
										</span>
										<ChevronDownIcon class={advancedAppOpen ? 'size-4 rotate-180 transition-transform' : 'size-4 transition-transform'} />
									</Collapsible.Trigger>
									<Collapsible.Content class="pt-3">
										<Field.Field>
											<Field.FieldLabel for="site-run-user">{t('webstack.createModal.runAsUserLabel')}</Field.FieldLabel>
											<Input
												id="site-run-user"
												name="run_user"
												class="h-10 font-mono text-base sm:h-9 sm:text-xs"
												placeholder={t('webstack.createModal.runAsUserPlaceholder')}
												autocorrect="off"
												autocapitalize="none"
												spellcheck={false}
												bind:value={runUser}
											/>
											<Field.FieldDescription>
												{t('webstack.createModal.runAsUserDesc1')}<code>node_modules</code>{t('webstack.createModal.runAsUserDesc2')}<code>target</code>{t('webstack.createModal.runAsUserDesc3')}
											</Field.FieldDescription>
										</Field.Field>
									</Collapsible.Content>
								</Collapsible.Root>
							{/if}
						</Field.FieldGroup>
					</Field.FieldSet>

					<Separator />

					<Field.FieldSet>
						<Field.FieldLegend variant="label">{t('webstack.createModal.legendNetwork')}</Field.FieldLegend>
						<Field.FieldGroup class="grid gap-3 sm:grid-cols-2">
							{#if isEdge}
								<Field.Field orientation="horizontal" class="sm:col-span-2">
									<Checkbox id="site-edge-expose" bind:checked={edgeExposeEnabled} />
									<Field.FieldLabel for="site-edge-expose" class="font-normal">
										{t('webstack.createModal.edgeExposeCheckbox', { ipPart: modeStore.publicIP ? ` → ${modeStore.publicIP}` : '' })}
									</Field.FieldLabel>
								</Field.Field>

								{#if edgeExposeEnabled}
									<Field.Field orientation="horizontal">
										<Checkbox id="site-edge-ssl" bind:checked={edgeSSLEnabled} />
										<Field.FieldLabel for="site-edge-ssl" class="font-normal">
											{t('webstack.createModal.edgeSslCheckbox')}
										</Field.FieldLabel>
									</Field.Field>
									<Field.Field orientation="horizontal">
										<Checkbox id="site-add-hosts" bind:checked={addLocalHosts} />
										<Field.FieldLabel for="site-add-hosts" class="font-normal">
											{t('webstack.createModal.addHostsEdge1')}<code>/etc/hosts</code>{t('webstack.createModal.addHostsEdge2')}
										</Field.FieldLabel>
									</Field.Field>
								{:else}
									<Field.Field orientation="horizontal" class="sm:col-span-2">
										<Checkbox id="site-ssl" bind:checked={sslEnabled} />
										<Field.FieldLabel for="site-ssl" class="font-normal">
											{t('webstack.createModal.localMkcertCheckbox')}
										</Field.FieldLabel>
									</Field.Field>
								{/if}
							{:else if isVPS}
								<Field.Field orientation="horizontal" class="sm:col-span-2">
									<Checkbox id="site-ssl" bind:checked={sslEnabled} />
									<Field.FieldLabel for="site-ssl" class="font-normal">
										{t('webstack.createModal.vpsLeCheckbox')}
									</Field.FieldLabel>
								</Field.Field>
							{:else}
								<Field.Field orientation="horizontal" class="sm:col-span-2">
									<Checkbox id="site-ssl" bind:checked={sslEnabled} />
									<Field.FieldLabel for="site-ssl" class="font-normal">
										{t('webstack.createModal.localAutoMkcertCheckbox')}
									</Field.FieldLabel>
								</Field.Field>
							{/if}
						</Field.FieldGroup>
					</Field.FieldSet>
				</div>
			</div>

			<div class="shrink-0 border-t bg-muted/20 px-4 py-2.5 sm:px-6 sm:py-3">
				<div class="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
					<p class="hidden text-xs text-muted-foreground sm:block">
						{#if effectiveDomain}
							{t('webstack.createModal.footerApplied1')}<code class="rounded bg-muted px-1.5 py-0.5 font-mono text-foreground">{effectiveDomain}</code>{t('webstack.createModal.footerApplied2')}
						{:else}
							{t('webstack.createModal.footerIncomplete')}
						{/if}
					</p>
					<div class="grid w-full grid-cols-2 gap-2 sm:flex sm:w-auto sm:items-center">
						<Button
							type="button"
							variant="outline"
							class="min-h-10 sm:min-h-0"
							onclick={() => (open = false)}
						>
							{t('webstack.createModal.cancel')}
						</Button>
						<Button
							type="submit"
							class="min-h-10 px-2.5 text-xs sm:min-h-0 sm:px-4 sm:text-sm"
							disabled={creating || (!isEdit && isEdge && domainChoices.length === 0)}
						>
							{#if creating}
								<Spinner data-icon="inline-start" />
								{isEdit ? t('webstack.createModal.saving') : t('webstack.createModal.creating')}
							{:else if isEdit}
								<SaveIcon data-icon="inline-start" />
								<span class="truncate sm:hidden">{t('webstack.createModal.saveShort')}</span>
								<span class="hidden sm:inline">{t('webstack.createModal.saveFull')}</span>
							{:else}
								<PlusIcon data-icon="inline-start" />
								<span class="truncate sm:hidden">{t('webstack.createModal.createShort')}</span>
								<span class="hidden sm:inline">{t('webstack.createModal.createFull')}</span>
							{/if}
						</Button>
					</div>
				</div>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>

<FolderPickerDialog
	bind:open={folderPickerOpen}
	initialPath={rootPath.trim()}
	onSelect={(picked) => {
		selectedDriveRoot = null;
		rootPath = picked;
		if (isManagedApp) {
			void inspectFolder(picked, !isEdit);
		}
	}}
/>
