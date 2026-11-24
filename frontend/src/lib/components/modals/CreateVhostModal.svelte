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
	let importProjectEnv = $state(true);
	let inspectResult = $state<SiteAppInspectResult | null>(null);
	let inspectLoading = $state(false);
	let portSuggesting = $state(false);
	let advancedAppOpen = $state(false);

	const webFrameworks = [
		{ value: 'laravel', label: 'Laravel (root di /public)' },
		{ value: 'wordpress', label: 'WordPress' },
		{ value: 'ci4', label: 'CodeIgniter 4 (root di /public)' },
		{ value: 'ci3', label: 'CodeIgniter 3' },
		{ value: 'symfony', label: 'Symfony (root di /public)' },
		{ value: 'generic', label: 'Generic PHP' },
		{ value: 'static', label: 'Static HTML (index.html)' }
	];

	const managedAppFrameworks = [
		{ value: 'nodejs', label: 'Node.js (systemd · pnpm / npm / bun)' },
		{ value: 'go', label: 'Go (systemd · Binary Service)' },
		{ value: 'rust', label: 'Rust (systemd · Cargo Service)' }
	];

	const proxyFrameworks = [{ value: 'proxy', label: 'Reverse Proxy (Manual / Eksternal / Docker)' }];

	const frameworks = [...webFrameworks, ...managedAppFrameworks, ...proxyFrameworks];

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
			return `${dom} → ${targetDesc} · Tunnel VPS Edge${modeStore.publicIP ? ` (${modeStore.publicIP})` : ''}`;
		}
		if (isVPS) {
			return `/etc/nginx/sites-available/${dom} · Target: ${targetDesc} · VPS Publik`;
		}
		return `/etc/nginx/sites-available/${dom} · Target: ${targetDesc} · Lokal`;
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
		try {
			const res = await api.inspectSiteDir(target);
			inspectResult = res.inspect;
			if (!isEdit && res.suggested_port && (!proxyPortStr.trim() || proxyPortStr === '3000')) {
				proxyPortStr = String(res.suggested_port);
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
				importProjectEnv = Boolean(res.inspect.has_dot_env);
				runBuildNow = Boolean(buildCommand.trim());
			}
		} catch (err) {
			inspectResult = null;
			toast.error(err instanceof Error ? err.message : 'Gagal memeriksa direktori proyek');
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
					importProjectEnv = false;
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
					importProjectEnv = true;
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
					? 'Tambahkan domain/zone di menu PowerDNS & Edge terlebih dahulu.'
					: 'Pilih domain induk terlebih dahulu atau gunakan mode Domain Utama.';
				document.getElementById('site-parent-domain')?.focus();
				return;
			}
			if (!isEdge && !cleanSubPrefix) {
				formError = 'Awalan subdomain wajib diisi pada mode Subdomain.';
				document.getElementById('site-sub-prefix')?.focus();
				return;
			}
			if (
				cleanSubPrefix &&
				!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*$/.test(cleanSubPrefix)
			) {
				formError = 'Nama subdomain hanya boleh berisi huruf, angka, tanda hubung (-), atau titik.';
				document.getElementById('site-sub-prefix')?.focus();
				return;
			}
		}

		const cleanDomain = isEdit && siteToEdit ? siteToEdit.domain : effectiveDomain;
		if (!cleanDomain) {
			formError = 'Nama domain wajib diisi.';
			document.getElementById('site-domain')?.focus();
			return;
		}

		const proxyPort = parseInt(proxyPortStr, 10) || 0;
		if (usesProxyPort && (proxyPort < 1 || proxyPort > 65535)) {
			formError = 'Port aplikasi lokal harus antara 1 sampai 65535.';
			document.getElementById('site-proxy-port')?.focus();
			return;
		}
		if (isManagedApp) {
			if (!rootPath.trim()) {
				formError = 'Direktori proyek wajib dipilih untuk Managed App.';
				document.getElementById('site-root-path')?.focus();
				return;
			}
			if (!startCommand.trim()) {
				formError = 'Start Command wajib diisi (mis. pnpm run start, ./bin/app, atau cargo run).';
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
					import_project_env: !isEdit && importProjectEnv
				}
			: undefined;

		if (isEdit) {
			creating = true;
			try {
				const res = await cp.runWithSudoRetry(
					`Pembaruan konfigurasi Nginx${isManagedApp ? ' & service systemd' : ''} untuk ${cleanDomain} memerlukan hak akses administrator (sudo).`,
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
					toast.success(`Virtual host ${cleanDomain} berhasil diperbarui!`);
					open = false;
					if (res.build_task_id) {
						cp.openTerminal(`Build App: ${cleanDomain}`);
					} else if (res.build_error) {
						toast.warning(`VHost diperbarui, namun build otomatis gagal dimulai: ${res.build_error}`);
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
				formError = err instanceof Error ? err.message : 'Gagal memperbarui virtual host';
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
				`Pembuatan Virtual Host ${cleanDomain}${isManagedApp ? ' & service systemd' : ''} memerlukan hak akses administrator (sudo).`,
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
					`Virtual host ${cleanDomain} berhasil dibuat, namun eksposur VPS Edge gagal: ${res.edge_error}`
				);
			} else if (res?.edge_exposed) {
				toast.success(
					`Virtual host ${cleanDomain} dibuat & terekspos ke ${res.edge_domain || cleanDomain}${res.dns_synced ? ' (DNS A & Proxy VPS aktif)' : ''}.`
				);
			} else if (res?.dns_synced && res?.dns_message) {
				toast.success(`Virtual host ${cleanDomain} berhasil dibuat (${res.dns_message}).`);
			} else {
				toast.success(`Virtual host ${cleanDomain} berhasil dibuat!`);
			}

			open = false;
			if (res?.build_task_id) {
				cp.openTerminal(`Build App: ${cleanDomain}`);
			} else if (res?.build_error) {
				toast.warning(`VHost dibuat, namun build otomatis gagal dimulai: ${res.build_error}`);
			}
			if (onCreated) {
				await onCreated({ domain: cleanDomain, wantVPSLetsEncrypt });
			}
		} catch (err) {
			formError = err instanceof Error ? err.message : 'Gagal membuat virtual host';
		} finally {
			creating = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="flex max-h-[92dvh] max-w-[calc(100vw-1.5rem)] flex-col gap-4 sm:max-w-[min(94vw,64rem)]">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2 pr-6">
				{#if isEdit}
					<PencilIcon class="size-4 shrink-0 text-primary" />
					<span class="truncate">Ubah Virtual Host</span>
				{:else}
					<GlobeIcon class="size-4 shrink-0 text-primary" />
					<span class="truncate">Buat Virtual Host Baru</span>
				{/if}
			</Dialog.Title>
			<Dialog.Description class="break-words font-mono text-xs">{metaSubtitle}</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={handleSubmit} class="flex min-h-0 flex-1 flex-col gap-4">
			<div class="min-h-0 flex-1 overflow-y-auto overscroll-contain pr-1">
				<div class="flex flex-col gap-5">
					{#if formError}
						<Alert.Root variant="destructive" role="alert">
							<AlertCircleIcon />
							<Alert.Title>{isEdit ? 'Pembaruan Virtual Host Gagal' : 'Pembuatan Virtual Host Gagal'}</Alert.Title>
							<Alert.Description>{formError}</Alert.Description>
						</Alert.Root>
					{/if}

					<div class="flex flex-wrap items-center gap-2">
						<span class="text-xs font-medium text-muted-foreground">Preset Cepat:</span>
						<Button
							type="button"
							variant={activePreset === 'laravel' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('laravel')}
						>
							Laravel
						</Button>
						<Button
							type="button"
							variant={activePreset === 'wordpress' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('wordpress')}
						>
							WordPress
						</Button>
						<Button
							type="button"
							variant={activePreset === 'nodejs' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('nodejs')}
						>
							Node.js (systemd)
						</Button>
						<Button
							type="button"
							variant={activePreset === 'go' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('go')}
						>
							Go (systemd)
						</Button>
						<Button
							type="button"
							variant={activePreset === 'rust' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('rust')}
						>
							Rust (systemd)
						</Button>
						<Button
							type="button"
							variant={activePreset === 'proxy' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('proxy')}
						>
							Proxy Manual
						</Button>
						<Button
							type="button"
							variant={activePreset === 'static' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('static')}
						>
							Static HTML
						</Button>
						{#if activePreset === 'custom'}
							<Badge variant="outline">Custom</Badge>
						{/if}
					</div>

					<Field.FieldSet>
						<Field.FieldLegend variant="label">Domain &amp; Identitas Host</Field.FieldLegend>
						<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
							{#if isEdit}
								<Field.Field class="sm:col-span-2">
									<Field.FieldLabel for="site-domain">Domain (Terkunci)</Field.FieldLabel>
									<Input
										id="site-domain"
										name="domain"
										class="h-11 font-mono text-base bg-muted/50 cursor-not-allowed sm:h-9 sm:text-sm"
										value={domain}
										disabled
										readonly
									/>
									<Field.FieldDescription>
										Nama domain tidak dapat diubah setelah dibuat. Untuk mengganti nama domain, buat virtual host baru.
									</Field.FieldDescription>
								</Field.Field>
							{:else}
							<Field.Field class="sm:col-span-2" data-invalid={domainInvalid ? 'true' : undefined}>
								<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
									<Field.FieldLabel
										for={isEdge || (isVPS && domainMode === 'subdomain') ? 'site-sub-prefix' : 'site-domain'}
									>
										{isLocal ? 'Domain Lokal' : isEdge ? 'Domain PowerDNS' : 'Domain'}
									</Field.FieldLabel>
									{#if isEdge}
										<Badge variant="outline" class="w-fit text-[11px] font-medium">
											Zone PowerDNS ({domainChoices.length})
										</Badge>
									{:else if isVPS}
										<ToggleGroup.Root
											type="single"
											bind:value={() => domainMode, (v) => switchDomainMode(v)}
											variant="outline"
											spacing={2}
											class="grid w-full grid-cols-2 sm:inline-flex sm:w-auto"
											aria-label="Mode input domain"
										>
											<ToggleGroup.Item value="custom" class="min-h-10 text-xs sm:min-h-8 sm:px-3">
												Domain Utama
											</ToggleGroup.Item>
											<ToggleGroup.Item value="subdomain" class="min-h-10 text-xs sm:min-h-8 sm:px-3">
												Subdomain ({domainChoices.length})
											</ToggleGroup.Item>
										</ToggleGroup.Root>
									{/if}
								</div>

								{#if isEdge || (isVPS && domainMode === 'subdomain')}
									{#if domainChoices.length === 0}
										<div class="flex flex-col gap-2 rounded-md border border-dashed bg-muted/40 px-3 py-3 text-xs text-muted-foreground">
											{#if isEdge}
												<p>
													Belum ada zone di PowerDNS VPS Edge. Tambahkan domain zone di menu
													<strong>PowerDNS &amp; Edge</strong> terlebih dahulu sebelum membuat Virtual Host.
												</p>
												<div>
													<Button size="sm" variant="outline" href="/powerdns">
														<NetworkIcon data-icon="inline-start" />
														Kelola Zone PowerDNS
													</Button>
												</div>
											{:else}
												<p>
													Belum ada domain atau zone PowerDNS yang tersedia sebagai induk subdomain. Tambahkan zone di menu
													PowerDNS terlebih dahulu atau pilih <strong>Domain Utama</strong>.
												</p>
											{/if}
										</div>
									{:else}
										<div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)]">
											<InputGroup.Root class="h-11 min-w-0 sm:h-9">
												<InputGroup.Input
													id="site-sub-prefix"
													name="subdomain_prefix"
													placeholder={isEdge ? 'app / api (kosong = utama)' : 'app / api / admin'}
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
													aria-label="Pilih domain PowerDNS"
												>
													<span class="truncate">{parentDomain || 'Pilih domain PowerDNS'}</span>
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
											<span class="shrink-0">Domain hasil:</span>
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
										placeholder={isLocal ? 'proyekanda.test' : 'domainanda.com'}
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
										Pilih zone PowerDNS lalu isi awalan subdomain (atau kosongkan awalan untuk memakai domain utama zone).
									{:else if isVPS && domainMode === 'subdomain'}
										Pilih domain induk dan isi awalan subdomain. Record DNS A otomatis dibuat bila zone dikelola PowerDNS lokal.
									{:else if isVPS}
										Masukkan domain FQDN yang mengarah ke IP publik VPS ini.
									{:else}
										Akhiran <code>.test</code> disarankan untuk pengembangan lokal.
									{/if}
								</Field.FieldDescription>
								{#if domainInvalid}
									<Field.FieldError
										errors={[
											{
												message: isEdge
													? 'Pilih zone PowerDNS terlebih dahulu.'
													: isVPS && domainMode === 'subdomain'
														? 'Awalan subdomain dan domain induk wajib diisi.'
														: 'Domain wajib diisi.'
											}
										]}
									/>
								{/if}
							</Field.Field>
							{/if}
						</Field.FieldGroup>
					</Field.FieldSet>

					<Separator />

					<Field.FieldSet>
						<Field.FieldLegend variant="label">Runtime, Framework &amp; Target Aplikasi</Field.FieldLegend>
						<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
							<Field.Field>
								<Field.FieldLabel for="site-framework">Framework / Pola Aplikasi</Field.FieldLabel>
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
											<Select.Label>Web &amp; PHP</Select.Label>
											{#each webFrameworks as fw (fw.value)}
												<Select.Item value={fw.value}>{fw.label}</Select.Item>
											{/each}
										</Select.Group>
										<Select.Separator />
										<Select.Group>
											<Select.Label>Managed App (systemd)</Select.Label>
											{#each managedAppFrameworks as fw (fw.value)}
												<Select.Item value={fw.value}>{fw.label}</Select.Item>
											{/each}
										</Select.Group>
										<Select.Separator />
										<Select.Group>
											<Select.Label>Reverse Proxy Manual</Select.Label>
											{#each proxyFrameworks as fw (fw.value)}
												<Select.Item value={fw.value}>{fw.label}</Select.Item>
											{/each}
										</Select.Group>
									</Select.Content>
								</Select.Root>
							</Field.Field>

							{#if usesProxyPort}
								<Field.Field>
									<Field.FieldLabel for="site-proxy-port">Port Aplikasi Lokal</Field.FieldLabel>
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
											oninput={() => (formError = '')}
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
													onclick={() => fillSuggestedFreePort(true)}
												>
													{#if portSuggesting}
														<Spinner data-icon="inline-start" />
													{:else}
														Auto
													{/if}
												</Button>
											</InputGroup.Addon>
										{/if}
									</InputGroup.Root>
									<Field.FieldDescription>
										{isManagedApp
											? 'Aplikasi dijalankan pada localhost port ini dan diteruskan Nginx (env PORT diatur otomatis).'
											: 'Contoh: 3000 (Node/Vite), 8080 (Go/Docker), atau 3200 (Panel).'}
									</Field.FieldDescription>
								</Field.Field>
							{:else if !isStatic}
								<Field.Field>
									<Field.FieldLabel for="site-php-ver">Versi PHP Target</Field.FieldLabel>
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
									<Field.FieldDescription>Socket PHP-FPM yang melayani skrip PHP pada domain ini.</Field.FieldDescription>
								</Field.Field>
							{:else}
								<div class="flex flex-col justify-center rounded-lg border bg-muted/25 px-3.5 py-2.5 text-xs">
									<span class="font-medium text-foreground">Mode Static HTML</span>
									<span class="mt-0.5 text-muted-foreground">
										Nginx menyajikan berkas statis (<code>index.html</code>) secara langsung tanpa PHP-FPM.
									</span>
								</div>
							{/if}

							{#if !isProxy}
								<Field.Field class="sm:col-span-2">
									<Field.FieldLabel for="site-root-path">
										{isManagedApp ? 'Direktori Proyek Aplikasi (Wajib)' : 'Document Root (Opsional)'}
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
														? '/var/www/proyekanda.test'
														: '/var/www/domainanda.com'}
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
													Deteksi Proyek
												</Button>
											{/if}
											<Button
												type="button"
												variant="outline"
												class="min-h-11 w-full shrink-0 sm:min-h-0 sm:h-9 sm:w-auto"
												onclick={() => (folderPickerOpen = true)}
											>
												<FolderOpenIcon data-icon="inline-start" />
												Cari Folder
											</Button>
										</div>
									</div>
									{#if detectedDrives.length > 0 && !isManagedApp}
										<div class="flex flex-wrap items-center gap-1.5 pt-1">
											<span class="text-xs text-muted-foreground">Drive cepat:</span>
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
												Default (/var/www)
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
											Pilih folder kode proyek (<code>package.json</code>, <code>go.mod</code>, atau <code>Cargo.toml</code>) di bawah <code>/home</code>, <code>/var/www</code>, <code>/srv</code>, atau <code>/mnt</code>.
										{:else}
											Kosongkan untuk otomatis di <code>/var/www/&lt;domain&gt;</code>, atau klik <strong>Cari Folder</strong>.
											{#if !isStatic}
												Untuk Laravel/CI4/Symfony pilih folder root proyek (<code>/public</code> ditambahkan otomatis).
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
													Terdeteksi: {inspectResult.runtime?.toUpperCase()}
													{inspectResult.package_manager ? ` (${inspectResult.package_manager})` : ''}
												</span>
												{#if inspectResult.app_name}
													<Badge variant="outline" class="font-mono text-[11px]">{inspectResult.app_name}</Badge>
												{/if}
												{#if !inspectResult.toolchain_installed}
													<Badge variant="destructive" class="text-[11px]">Toolchain belum terpasang</Badge>
												{/if}
											</div>
											{#if inspectResult.has_dot_env}
												<Badge variant="secondary" class="text-[11px]">
													.env proyek ({inspectResult.dot_env_var_count} var)
												</Badge>
											{/if}
										</div>

										{#if inspectResult.candidates && inspectResult.candidates.length > 0}
											<div class="flex flex-wrap items-center gap-1.5">
												<span class="text-[11px] text-muted-foreground">Mode cepat:</span>
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
									<Field.FieldLabel for="site-build-cmd">Build Command (Opsional)</Field.FieldLabel>
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
										Dijalankan saat tombol <strong>Build &amp; Restart</strong> ditekan (output terminal live). Kosongkan bila menjalankan mode dev langsung.
									</Field.FieldDescription>
								</Field.Field>

								<Field.Field class="sm:col-span-2">
									<Field.FieldLabel for="site-start-cmd">Start Command (`ExecStart`, Wajib)</Field.FieldLabel>
									<Input
										id="site-start-cmd"
										name="start_command"
										class="h-11 font-mono text-base sm:h-9 sm:text-xs"
										placeholder={framework === 'nodejs'
											? 'pnpm run start atau pnpm run dev'
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
										Perintah yang dikelola daemon <code>systemd</code>. Gunakan <code>$PORT</code> bila CLI butuh flag port manual (mis. <code>pnpm run dev -- --port $PORT</code>).
									</Field.FieldDescription>
								</Field.Field>

								<div class="flex flex-col gap-3 rounded-lg border bg-muted/25 p-3 sm:col-span-2">
									<div class="flex items-center justify-between gap-3">
										<div class="flex flex-col gap-0.5">
											<span class="text-xs font-medium text-foreground">Auto-Start Saat Server Boot</span>
											<span class="text-[11px] text-muted-foreground">Aktifkan <code>systemctl enable</code> untuk unit aplikasi ini.</span>
										</div>
										<Switch bind:checked={autoStart} aria-label="Auto-start saat boot" />
									</div>

									{#if buildCommand.trim()}
										<div class="flex items-center justify-between gap-3 border-t pt-2.5">
											<div class="flex flex-col gap-0.5">
												<span class="text-xs font-medium text-foreground">
													{isEdit ? 'Rebuild & Restart Service Setelah Simpan' : 'Jalankan Build & Start Setelah VHost Dibuat'}
												</span>
												<span class="text-[11px] text-muted-foreground">
													Membuka Live Terminal untuk memantau proses build hingga aplikasi menyala.
												</span>
											</div>
											<Switch bind:checked={runBuildNow} aria-label="Jalankan build sekarang" />
										</div>
									{/if}

									{#if !isEdit && inspectResult?.has_dot_env}
										<div class="flex items-center justify-between gap-3 border-t pt-2.5">
											<div class="flex flex-col gap-0.5">
												<span class="text-xs font-medium text-foreground">Impor File `.env` Proyek</span>
												<span class="text-[11px] text-muted-foreground">
													Salin {inspectResult.dot_env_var_count} variabel dari <code>.env</code> proyek ke file env terkelola <code>systemd</code> (<code>0600</code>).
												</span>
											</div>
											<Switch bind:checked={importProjectEnv} aria-label="Impor .env proyek" />
										</div>
									{/if}
								</div>

								<Collapsible.Root bind:open={advancedAppOpen} class="sm:col-span-2">
									<Collapsible.Trigger class="flex w-full items-center justify-between rounded-md border bg-muted/20 px-3 py-2 text-xs font-medium text-muted-foreground hover:bg-muted/40 hover:text-foreground">
										<span class="flex items-center gap-1.5">
											<HammerIcon class="size-3.5" />
											Opsi Lanjutan Systemd (Run As User)
										</span>
										<ChevronDownIcon class={advancedAppOpen ? 'size-4 rotate-180 transition-transform' : 'size-4 transition-transform'} />
									</Collapsible.Trigger>
									<Collapsible.Content class="pt-3">
										<Field.Field>
											<Field.FieldLabel for="site-run-user">Run As User Linux (Opsional)</Field.FieldLabel>
											<Input
												id="site-run-user"
												name="run_user"
												class="h-10 font-mono text-base sm:h-9 sm:text-xs"
												placeholder="Kosongkan = user non-root aktif (mis. dietpi)"
												autocorrect="off"
												autocapitalize="none"
												spellcheck={false}
												bind:value={runUser}
											/>
											<Field.FieldDescription>
												Disarankan tetap non-root agar kepemilikan folder proyek (<code>node_modules</code>, <code>target</code>) tetap konsisten.
											</Field.FieldDescription>
										</Field.Field>
									</Collapsible.Content>
								</Collapsible.Root>
							{/if}
						</Field.FieldGroup>
					</Field.FieldSet>

					<Separator />

					<Field.FieldSet>
						<Field.FieldLegend variant="label">SSL, DNS &amp; Eksposur Jaringan</Field.FieldLegend>
						<Field.FieldGroup class="grid gap-3 sm:grid-cols-2">
							{#if isEdge}
								<Field.Field orientation="horizontal" class="sm:col-span-2">
									<Checkbox id="site-edge-expose" bind:checked={edgeExposeEnabled} />
									<Field.FieldLabel for="site-edge-expose" class="font-normal">
										Ekspos ke internet &amp; sinkronkan DNS via VPS Edge Tunnel (NetBird{modeStore.publicIP ? ` → ${modeStore.publicIP}` : ''})
									</Field.FieldLabel>
								</Field.Field>

								{#if edgeExposeEnabled}
									<Field.Field orientation="horizontal">
										<Checkbox id="site-edge-ssl" bind:checked={edgeSSLEnabled} />
										<Field.FieldLabel for="site-edge-ssl" class="font-normal">
											Terbitkan SSL Let's Encrypt (HTTPS) di VPS Edge Gateway
										</Field.FieldLabel>
									</Field.Field>
									<Field.Field orientation="horizontal">
										<Checkbox id="site-add-hosts" bind:checked={addLocalHosts} />
										<Field.FieldLabel for="site-add-hosts" class="font-normal">
											Tambahkan juga ke <code>/etc/hosts</code> lokal (opsional)
										</Field.FieldLabel>
									</Field.Field>
								{:else}
									<Field.Field orientation="horizontal" class="sm:col-span-2">
										<Checkbox id="site-ssl" bind:checked={sslEnabled} />
										<Field.FieldLabel for="site-ssl" class="font-normal">
											Aktifkan Sertifikat SSL Lokal (HTTPS mkcert)
										</Field.FieldLabel>
									</Field.Field>
								{/if}
							{:else if isVPS}
								<Field.Field orientation="horizontal" class="sm:col-span-2">
									<Checkbox id="site-ssl" bind:checked={sslEnabled} />
									<Field.FieldLabel for="site-ssl" class="font-normal">
										Terbitkan SSL Let's Encrypt (HTTPS) setelah virtual host dibuat
									</Field.FieldLabel>
								</Field.Field>
							{:else}
								<Field.Field orientation="horizontal" class="sm:col-span-2">
									<Checkbox id="site-ssl" bind:checked={sslEnabled} />
									<Field.FieldLabel for="site-ssl" class="font-normal">
										Aktifkan Sertifikat SSL Otomatis (HTTPS mkcert)
									</Field.FieldLabel>
								</Field.Field>
							{/if}
						</Field.FieldGroup>
					</Field.FieldSet>
				</div>
			</div>

			<Separator />

			<div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
				<p class="text-xs text-muted-foreground">
					{#if effectiveDomain}
						Konfigurasi Nginx akan diterapkan untuk <code class="rounded bg-muted px-1.5 py-0.5 font-mono text-foreground">{effectiveDomain}</code>.
					{:else}
						Lengkapi nama domain untuk membuat konfigurasi Virtual Host.
					{/if}
				</p>
				<div class="flex w-full flex-col-reverse gap-2 sm:w-auto sm:flex-row sm:items-center">
					<Button
						type="button"
						variant="outline"
						class="min-h-10 sm:min-h-0"
						onclick={() => (open = false)}
					>
						Batal
					</Button>
					<Button
						type="submit"
						class="min-h-10 sm:min-h-0"
						disabled={creating || (!isEdit && isEdge && domainChoices.length === 0)}
					>
						{#if creating}
							<Spinner data-icon="inline-start" />
							{isEdit ? 'Menyimpan...' : 'Membuat...'}
						{:else if isEdit}
							<SaveIcon data-icon="inline-start" />
							Simpan Perubahan
						{:else}
							<PlusIcon data-icon="inline-start" />
							Buat Virtual Host
						{/if}
					</Button>
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
