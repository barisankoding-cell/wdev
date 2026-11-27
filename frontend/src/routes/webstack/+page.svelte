<script lang="ts">
	import { onMount } from 'svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { cn } from '#lib/utils.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PaginationBar from '#lib/components/shared/PaginationBar.svelte';
	import CreateVhostModal from '#lib/components/modals/CreateVhostModal.svelte';
	import ManageSiteAppModal from '#lib/components/modals/ManageSiteAppModal.svelte';
	import { api } from '#lib/api/client.js';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import type { SiteRecord, ZoneInfo } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import SearchIcon from '@lucide/svelte/icons/search';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import CloudUploadIcon from '@lucide/svelte/icons/cloud-upload';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import LinkIcon from '@lucide/svelte/icons/link';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import HammerIcon from '@lucide/svelte/icons/hammer';
	import PlayIcon from '@lucide/svelte/icons/play';
	import RotateCwIcon from '@lucide/svelte/icons/rotate-cw';
	import SlidersHorizontalIcon from '@lucide/svelte/icons/sliders-horizontal';
	import SquareIcon from '@lucide/svelte/icons/square';

	let sites = $state<SiteRecord[]>([]);
	let zones = $state<ZoneInfo[]>([]);
	let loading = $state(true);
	let searchQuery = $state('');
	let categoryFilter = $state<'all' | 'web' | 'app' | 'proxy'>('all');
	let siteDeleting = $state<Record<string, boolean>>({});
	let sslPending = $state<Record<string, boolean>>({});
	let appActionPending = $state<Record<string, boolean>>({});
	let lastLoadedMode = $state<string | null>(null);

	// Modal Tambah & Ubah VHost
	let createOpen = $state(false);
	let editingSite = $state<SiteRecord | null>(null);

	// Modal Kelola Managed App (systemd + .env + logs)
	let manageAppOpen = $state(false);
	let manageAppDomain = $state('');
	const manageAppSite = $derived(sites.find((s) => s.domain === manageAppDomain) ?? null);

	// Modal Ekspos & SSL Edge (untuk situs yang belum terekspos)
	let exposeOpen = $state(false);
	let exposing = $state(false);
	let exposeLocalDomain = $state('');
	let exposePublicDomain = $state('');
	let exposeUseSSL = $state(true);

	// Modal Cabut Eksposur Tunnel (mode Edge)
	let unexposeOpen = $state(false);
	let unexposing = $state(false);
	let unexposeTarget = $state<SiteRecord | null>(null);
	let unexposeDeleteDNS = $state(false);

	// Modal Hapus Situs
	let deleteOpen = $state(false);
	let deleteTarget = $state<SiteRecord | null>(null);
	let deleteUnexpose = $state(true);
	let deleteDNSRecord = $state(false);

	const isVPS = $derived(modeStore.mode === 'vps');
	const isEdge = $derived(modeStore.mode === 'edge');
	const isLocal = $derived(modeStore.mode === 'local');
	const supportsDNS = $derived(isVPS || isEdge);

	function isLocalOnlyDomain(dom: string): boolean {
		const clean = dom.trim().replace(/\.$/, '').toLowerCase();
		return /\.(test|local|localhost|invalid|example|arpa|internal)$/.test(clean);
	}

	const pdnsZones = $derived.by(() =>
		Array.from(
			new Set(
				zones
					.map((z) => (z.name || '').replace(/\.$/, '').trim().toLowerCase())
					.filter((name) => Boolean(name) && !name.endsWith('.arpa'))
			)
		).sort()
	);

	const parentDomains = $derived.by(() => {
		const seen = new Set(pdnsZones);
		const fromSites = Array.from(
			new Set(
				sites
					.map((s) => (s.domain || '').replace(/\.$/, '').trim().toLowerCase())
					.filter(
						(name) =>
							Boolean(name) &&
							!name.startsWith('www.') &&
							!isLocalOnlyDomain(name) &&
							!seen.has(name)
					)
			)
		).sort();
		return [...pdnsZones, ...fromSites];
	});

	async function refreshZonesQuiet() {
		if (!supportsDNS) {
			zones = [];
			return;
		}
		try {
			zones = (await api.listPDNSZones()) ?? [];
		} catch {
			// PowerDNS belum terhubung / opsional
		}
	}

	function isManagedApp(site: SiteRecord): boolean {
		return site.framework === 'nodejs' || site.framework === 'go' || site.framework === 'rust';
	}

	function frameworkBadgeLabel(fw?: string): string {
		const normalized = (fw || '').trim().toLowerCase();
		if (normalized === 'nodejs') return 'NODE.JS';
		if (normalized === 'go') return 'GO';
		if (normalized === 'rust') return 'RUST';
		if (!normalized) return 'GENERIC';
		return normalized.toUpperCase();
	}

	function appRuntimeTone(site: SiteRecord): 'success' | 'info' | 'destructive' | 'neutral' {
		const st = site.app_status?.active_state;
		if (st === 'active') return 'success';
		if (st === 'activating' || st === 'reloading') return 'info';
		if (st === 'failed') return 'destructive';
		return 'neutral';
	}

	function appRuntimeLabel(site: SiteRecord): string {
		const st = site.app_status?.active_state || 'inactive';
		if (st === 'active') return t('webstack.page.appStatus.running');
		if (st === 'activating') return t('webstack.page.appStatus.starting');
		if (st === 'failed') return t('webstack.page.appStatus.failed');
		return t('webstack.page.appStatus.stopped');
	}

	function isAppRunning(site: SiteRecord): boolean {
		const st = site.app_status?.active_state;
		return st === 'active' || st === 'activating';
	}

	function formatMemoryBytes(bytes?: number): string {
		if (!bytes || bytes <= 0) return '';
		if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`;
		if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
		return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
	}

	const filteredSites = $derived.by(() => {
		const q = searchQuery.trim().toLowerCase();
		return sites.filter((s) => {
			if (categoryFilter === 'app' && !isManagedApp(s)) return false;
			if (categoryFilter === 'proxy' && s.framework !== 'proxy') return false;
			if (categoryFilter === 'web' && (isManagedApp(s) || s.framework === 'proxy')) return false;
			if (!q) return true;
			return (
				s.domain.toLowerCase().includes(q) ||
				s.framework.toLowerCase().includes(q) ||
				(s.root_path || '').toLowerCase().includes(q) ||
				(s.exposed_to || '').toLowerCase().includes(q) ||
				(s.app?.service_name || '').toLowerCase().includes(q)
			);
		});
	});

	// Pagination (Max 10 data per page)
	const PAGE_SIZE = 10;
	let sitePage = $state(1);
	const paginatedSites = $derived(
		filteredSites.slice((sitePage - 1) * PAGE_SIZE, sitePage * PAGE_SIZE)
	);

	$effect(() => {
		const _ = searchQuery;
		const __ = categoryFilter;
		sitePage = 1;
	});

	async function loadSites(opts?: { silent?: boolean }) {
		const silent = Boolean(opts?.silent);
		if (!silent) loading = true;
		try {
			const [siteRes, zoneRes] = await Promise.allSettled([
				api.listSites(),
				supportsDNS ? api.listPDNSZones() : Promise.resolve([] as ZoneInfo[])
			]);
			if (siteRes.status === 'fulfilled') {
				sites = siteRes.value ?? [];
			} else if (!silent) {
				toast.error(
					siteRes.reason instanceof Error
						? siteRes.reason.message
						: t('webstack.page.toast.loadFailed')
				);
			}
			if (zoneRes.status === 'fulfilled') {
				zones = zoneRes.value ?? [];
			} else if (!supportsDNS) {
				zones = [];
			}
		} finally {
			if (!silent) loading = false;
		}
	}

	onMount(() => {
		void loadSites();
		const timer = setInterval(() => {
			if (
				typeof document !== 'undefined' &&
				!document.hidden &&
				!loading &&
				!createOpen &&
				!deleteOpen &&
				sites.some((s) => isManagedApp(s) && isAppRunning(s))
			) {
				void loadSites({ silent: true });
			}
		}, 8000);
		return () => clearInterval(timer);
	});

	$effect(() => {
		if (cp.refreshTick > 0) loadSites();
	});

	$effect(() => {
		const currentMode = modeStore.mode;
		if (lastLoadedMode !== null && lastLoadedMode !== currentMode) {
			void loadSites();
		}
		lastLoadedMode = currentMode;
	});

	function openCreate() {
		editingSite = null;
		createOpen = true;
	}

	function openEdit(site: SiteRecord) {
		editingSite = site;
		createOpen = true;
	}

	function openManageApp(site: SiteRecord) {
		manageAppDomain = site.domain;
		manageAppOpen = true;
	}

	async function runQuickAppAction(site: SiteRecord, action: 'start' | 'stop' | 'restart') {
		appActionPending = { ...appActionPending, [site.domain]: true };
		try {
			const res = await cp.runWithSudoRetry(
				t('webstack.page.sudo.appActionPrompt', { domain: site.domain }),
				(sudoPass) => api.siteAppAction(site.domain, action, sudoPass)
			);
			if (!res) return;
			const verb =
				action === 'start'
					? t('webstack.page.toast.actionVerbStart')
					: action === 'stop'
						? t('webstack.page.toast.actionVerbStop')
						: t('webstack.page.toast.actionVerbRestart');
			toast.success(t('webstack.page.toast.appActionDone', { domain: site.domain, verb }));
			await loadSites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('webstack.page.toast.appActionFailed', { action }));
		} finally {
			const next = { ...appActionPending };
			delete next[site.domain];
			appActionPending = next;
		}
	}

	async function triggerQuickAppBuild(site: SiteRecord) {
		appActionPending = { ...appActionPending, [site.domain]: true };
		try {
			const res = await cp.runWithSudoRetry(
				t('webstack.page.sudo.appBuildPrompt', { domain: site.domain }),
				(sudoPass) => api.buildSiteApp(site.domain, true, sudoPass)
			);
			if (!res) return;
			cp.openTerminal(
				t('webstack.page.buildTerminalTitle', { domain: site.domain, runtime: frameworkBadgeLabel(site.framework) })
			);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('webstack.page.toast.buildFailed'));
		} finally {
			const next = { ...appActionPending };
			delete next[site.domain];
			appActionPending = next;
		}
	}

	async function triggerLetsEncrypt(siteDomain: string, skipConfirm = false) {
		const clean = siteDomain.trim().replace(/\.$/, '').toLowerCase();
		if (isLocalOnlyDomain(clean)) {
			toast.warning(t('webstack.page.toast.localLeBlocked', { domain: clean }));
			return;
		}
		if (!skipConfirm) {
			const confirmed = await cp.confirm({
				title: t('webstack.page.toast.leConfirmTitle'),
				description: t('webstack.page.toast.leConfirmDesc', { domain: clean }),
				confirmText: t('webstack.page.toast.leConfirmBtn')
			});
			if (!confirmed) return;
		}

		sslPending = { ...sslPending, [clean]: true };
		try {
			const res = await cp.runWithSudoRetry(
				t('webstack.page.sudo.letsEncryptPrompt', { domain: clean }),
				(sudoPass) => api.enableSiteSSL(clean, '', sudoPass)
			);
			if (res) {
				cp.openTerminal(t('webstack.page.leTerminalTitle', { domain: clean }));
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('webstack.page.toast.leStartFailed'));
		} finally {
			const next = { ...sslPending };
			delete next[clean];
			sslPending = next;
		}
	}

	function openExposeModal(site: SiteRecord) {
		exposeLocalDomain = site.domain;
		if (site.exposed_to) {
			exposePublicDomain = site.exposed_to;
		} else if (!isLocalOnlyDomain(site.domain)) {
			exposePublicDomain = site.domain;
		} else if (pdnsZones.length > 0) {
			const prefix = site.domain.split('.')[0] || 'app';
			exposePublicDomain = `${prefix}.${pdnsZones[0]}`;
		} else if (parentDomains.length > 0) {
			const prefix = site.domain.split('.')[0] || 'app';
			exposePublicDomain = `${prefix}.${parentDomains[0]}`;
		} else {
			exposePublicDomain = '';
		}
		exposeUseSSL = true;
		exposeOpen = true;
		if (zones.length === 0) void refreshZonesQuiet();
	}

	async function exposeOrRenewEdge(localDomain: string, publicDomain: string, useSSL: boolean) {
		const cleanLocal = localDomain.trim().replace(/\.$/, '').toLowerCase();
		const cleanPub = publicDomain.trim().replace(/\.$/, '').toLowerCase();
		if (!cleanPub || isLocalOnlyDomain(cleanPub)) {
			toast.warning(t('webstack.page.toast.invalidEdgeDomain'));
			return false;
		}
		sslPending = { ...sslPending, [cleanLocal]: true };
		try {
			await api.exposePDNS({
				mode: 'site',
				local_domain: cleanLocal,
				public_domain: cleanPub,
				use_ssl: useSSL,
				websocket: true
			});
			toast.success(
				useSSL
					? t('webstack.page.toast.exposedWithSsl', { domain: cleanPub })
					: t('webstack.page.toast.exposedNoSsl', { domain: cleanPub })
			);
			await loadSites();
			return true;
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('webstack.page.toast.exposeFailed'));
			return false;
		} finally {
			const next = { ...sslPending };
			delete next[cleanLocal];
			sslPending = next;
		}
	}

	async function submitExposeModal(e: SubmitEvent) {
		e.preventDefault();
		exposing = true;
		try {
			const ok = await exposeOrRenewEdge(exposeLocalDomain, exposePublicDomain, exposeUseSSL);
			if (ok) exposeOpen = false;
		} finally {
			exposing = false;
		}
	}

	function openUnexposeModal(site: SiteRecord) {
		unexposeTarget = site;
		unexposeDeleteDNS = false;
		unexposeOpen = true;
	}

	async function confirmUnexpose() {
		const target = unexposeTarget;
		if (!target?.exposed_to) return;
		unexposing = true;
		sslPending = { ...sslPending, [target.domain]: true };
		try {
			await api.unexposePDNS(target.exposed_to, !unexposeDeleteDNS);
			toast.success(t('webstack.page.toast.unexposed', { domain: target.exposed_to }));
			unexposeOpen = false;
			unexposeTarget = null;
			await loadSites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('webstack.page.toast.unexposeFailed'));
		} finally {
			unexposing = false;
			const next = { ...sslPending };
			delete next[target.domain];
			sslPending = next;
		}
	}

	async function handleVhostCreated({ domain: createdDom, wantVPSLetsEncrypt }: { domain: string; wantVPSLetsEncrypt: boolean }) {
		await loadSites();
		if (wantVPSLetsEncrypt) {
			await triggerLetsEncrypt(createdDom, true);
		}
	}

	function formatSSLExpiry(expiresAt?: string, daysLeft?: number): {
		text: string;
		fullTitle: string;
		expired: boolean;
		expiringSoon: boolean;
	} | null {
		if (!expiresAt) return null;
		const dt = new Date(expiresAt);
		if (Number.isNaN(dt.getTime())) return null;
		const dateLabel = i18n.formatDate(dt, {
			day: 'numeric',
			month: 'short',
			year: 'numeric'
		});
		const fullTitle = i18n.formatDateTime(dt, {
			dateStyle: 'full',
			timeStyle: 'short'
		});
		const expired = typeof daysLeft === 'number' ? daysLeft < 0 : dt.getTime() < Date.now();
		const expiringSoon = !expired && typeof daysLeft === 'number' && daysLeft <= 14;
		let text = t('webstack.page.validUntilPrefix', { date: dateLabel });
		if (expired) {
			text = t('webstack.page.expiredPrefix', { date: dateLabel });
		} else if (typeof daysLeft === 'number') {
			text =
				daysLeft === 0
					? t('webstack.page.expiringLt1Day', { date: dateLabel })
					: t('webstack.page.expiringDays', { date: dateLabel, days: daysLeft });
		}
		return { text, fullTitle, expired, expiringSoon };
	}

	async function executeDeleteSite(
		siteDomain: string,
		opts?: { unexpose?: boolean; delete_dns?: boolean }
	) {
		siteDeleting = { ...siteDeleting, [siteDomain]: true };
		try {
			const res = await cp.runWithSudoRetry(
				t('webstack.page.sudo.deleteSitePrompt', { domain: siteDomain }),
				(sudoPass) =>
					api.deleteSite(siteDomain, {
						...opts,
						...(sudoPass ? { sudo_pass: sudoPass } : {})
					})
			);
			if (!res) return;
			if (res?.unexpose_error) {
				toast.warning(
					t('webstack.page.toast.deleteWarningUnexpose', { domain: siteDomain, error: res.unexpose_error })
				);
			} else if (res?.unexposed) {
				toast.success(
					res.dns_deleted
						? t('webstack.page.toast.deletedUnexposedAndDns', { domain: siteDomain })
						: t('webstack.page.toast.deletedUnexposed', { domain: siteDomain })
				);
			} else if (res?.dns_deleted) {
				toast.success(t('webstack.page.toast.deletedWithDns', { domain: siteDomain }));
			} else {
				toast.success(t('webstack.page.toast.deletedSafe', { domain: siteDomain }));
			}
			deleteOpen = false;
			deleteTarget = null;
			await loadSites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('webstack.page.toast.deleteFailed'));
		} finally {
			const next = { ...siteDeleting };
			delete next[siteDomain];
			siteDeleting = next;
		}
	}

	async function deleteSite(site: SiteRecord) {
		const hasEdgeExposure = isEdge && Boolean(site.exposed_to);
		const hasVPSPublicDNS = isVPS && !isLocalOnlyDomain(site.domain);
		if (hasEdgeExposure || hasVPSPublicDNS || isManagedApp(site)) {
			deleteTarget = site;
			deleteUnexpose = hasEdgeExposure;
			deleteDNSRecord = false;
			deleteOpen = true;
			return;
		}

		const confirmed = await cp.confirm({
			title: t('webstack.page.toast.deleteSimpleTitle'),
			description: t('webstack.page.toast.deleteSimpleDesc', {
				domain: site.domain,
				path: site.root_path || '/var/www/' + site.domain
			}),
			confirmText: t('webstack.page.toast.deleteSimpleBtn'),
			destructive: true
		});
		if (!confirmed) return;

		await executeDeleteSite(site.domain);
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">
				{isLocal ? t('webstack.page.titleLocal') : t('webstack.page.titlePublic')}
			</h1>
			<p class="text-sm text-muted-foreground">
				{#if isVPS}
					{t('webstack.page.subtitleVps1')}<code>wdev site</code>{t('webstack.page.subtitleVps2')}
				{:else if isEdge}
					{t('webstack.page.subtitleEdge')}
				{:else}
					{t('webstack.page.subtitleLocal1')}<code>wdev site</code>{t('webstack.page.subtitleLocal2')}<code>/etc/hosts</code>{t('webstack.page.subtitleLocal3')}<code>mkcert</code>{t('webstack.page.subtitleLocal4')}
				{/if}
			</p>
		</div>
		<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
			{#if !isVPS}
				<Button variant="outline" size="sm" class="min-h-10 sm:min-h-0" onclick={() => cp.triggerInstall('sync-hosts')}>
					<RefreshCwIcon data-icon="inline-start" />
					{t('webstack.page.syncHostsSsl')}
				</Button>
			{/if}
			<Button size="sm" class={cn('min-h-10 sm:min-h-0', isVPS && 'col-span-2 sm:col-span-1')} onclick={openCreate}>
				<PlusIcon data-icon="inline-start" />
				{t('webstack.page.addVhostBtn')}
			</Button>
		</div>
	</div>

	{#if isEdge && !modeStore.reachable}
		<Alert.Root variant="destructive">
			<TriangleAlertIcon />
			<Alert.Title>{t('webstack.page.edgeOfflineTitle')}</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					{t('webstack.page.edgeOfflineDesc')}
				</span>
				<Button size="xs" variant="outline" href="/powerdns">
					<NetworkIcon data-icon="inline-start" />
					{t('webstack.page.openPdnsEdge')}
				</Button>
			</Alert.Description>
		</Alert.Root>
	{/if}

	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<search class="w-full sm:max-w-sm">
			<InputGroup.Root>
				<InputGroup.Addon align="inline-start">
					<SearchIcon />
				</InputGroup.Addon>
				<InputGroup.Input
					type="search"
					placeholder={t('webstack.page.searchPlaceholder')}
					aria-label={t('webstack.page.searchAria')}
					bind:value={searchQuery}
				/>
			</InputGroup.Root>
		</search>

		<ToggleGroup.Root
			type="single"
			value={categoryFilter}
			onValueChange={(v) => {
				if (v === 'all' || v === 'web' || v === 'app' || v === 'proxy') categoryFilter = v;
			}}
			variant="outline"
			spacing={1}
			class="grid w-full grid-cols-2 sm:inline-flex sm:w-auto"
			aria-label={t('webstack.page.filterAria')}
		>
			<ToggleGroup.Item value="all" class="min-h-9 text-xs sm:min-h-8 sm:px-3">{t('webstack.page.filterAll')}</ToggleGroup.Item>
			<ToggleGroup.Item value="web" class="min-h-9 text-xs sm:min-h-8 sm:px-3">{t('webstack.page.filterWebPhp')}</ToggleGroup.Item>
			<ToggleGroup.Item value="app" class="min-h-9 text-xs sm:min-h-8 sm:px-3">{t('webstack.page.filterManagedApp')}</ToggleGroup.Item>
			<ToggleGroup.Item value="proxy" class="min-h-9 text-xs sm:min-h-8 sm:px-3">{t('webstack.page.filterProxy')}</ToggleGroup.Item>
		</ToggleGroup.Root>
	</div>

	{#if loading}
		<div class="flex flex-col gap-2">
			<Skeleton class="h-10 w-full" />
			<Skeleton class="h-16 w-full" />
			<Skeleton class="h-16 w-full" />
		</div>
	{:else if filteredSites.length === 0}
		<Empty.Root class="border">
			<Empty.Header>
				<Empty.Media variant="icon">
					<GlobeIcon />
				</Empty.Media>
				<Empty.Title>
					{sites.length === 0 ? t('webstack.page.emptyTitleNone') : t('webstack.page.emptyTitleNoMatch')}
				</Empty.Title>
				<Empty.Description>
					{sites.length === 0
						? t('webstack.page.emptyDescNone')
						: t('webstack.page.emptyDescNoMatch')}
				</Empty.Description>
			</Empty.Header>
			{#if sites.length === 0}
				<Empty.Content>
					<Button onclick={openCreate}>
						<PlusIcon data-icon="inline-start" />
						{t('webstack.page.addVirtualHostBtn')}
					</Button>
				</Empty.Content>
			{/if}
		</Empty.Root>
	{:else}
		<div class="flex flex-col sm:overflow-hidden sm:rounded-lg sm:border sm:bg-card">
			<ResponsiveTableWrapper label={t('webstack.page.tableAria')} class="rounded-none border-0 bg-transparent">
				<Table.Root class="sm:[&_th]:px-4 sm:[&_td]:px-4 sm:[&_td]:py-3">
					<Table.Header>
						<Table.Row>
							<Table.Head scope="col" class="min-w-[160px] sm:w-[22%] text-xs font-semibold text-muted-foreground">
								{isLocal ? t('webstack.page.colDomainLocal') : t('webstack.page.colDomain')}
							</Table.Head>
							<Table.Head scope="col" class="min-w-[130px] sm:w-[16%] text-xs font-semibold text-muted-foreground">{t('webstack.page.colFramework')}</Table.Head>
							<Table.Head scope="col" class="min-w-[150px] sm:w-[22%] text-xs font-semibold text-muted-foreground">{t('webstack.page.colTargetRuntime')}</Table.Head>
							<Table.Head scope="col" class="w-[1%] whitespace-nowrap sm:text-center text-xs font-semibold text-muted-foreground">{t('webstack.page.colDocRoot')}</Table.Head>
							<Table.Head scope="col" class="min-w-[140px] sm:w-[16%] text-xs font-semibold text-muted-foreground">{t('webstack.page.colSsl')}</Table.Head>
							{#if isEdge}
								<Table.Head scope="col" class="min-w-[150px] text-xs font-semibold text-muted-foreground">{t('webstack.page.colEdgeTunnel')}</Table.Head>
							{/if}
							<Table.Head scope="col" class="w-[1%] whitespace-nowrap text-right text-xs font-semibold text-muted-foreground">{t('webstack.page.colActions')}</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each paginatedSites as site (site.domain)}
							{@const proto = site.ssl_enabled ? 'https://' : 'http://'}
							{@const hasLetsEncrypt = site.ssl_enabled && site.ssl_provider === 'letsencrypt'}
							{@const sslExpiry = formatSSLExpiry(site.ssl_expires_at, site.ssl_days_left)}
							{@const managed = isManagedApp(site)}
							{@const primaryHref =
								isEdge && site.exposed_to && site.domain === site.exposed_to
									? `https://${site.exposed_to}`
									: `${proto}${site.domain}`}
							<Table.Row>
								<Table.Cell data-label={t('webstack.page.dataDomain')}>
									<div class="flex flex-col gap-0.5">
										<a
											href={primaryHref}
											target="_blank"
											rel="noopener noreferrer"
											class="inline-flex items-center gap-1.5 font-semibold text-primary hover:underline"
										>
											<span class="break-all sm:break-normal">{site.domain}</span>
											<ExternalLinkIcon class="size-3.5 shrink-0 opacity-70" />
										</a>
										{#if isEdge && site.exposed_to && site.exposed_to !== site.domain}
											<div class="text-[11px] text-muted-foreground">
												{t('webstack.page.publicLabel')} <span class="font-mono">{site.exposed_to}</span>
											</div>
										{/if}
									</div>
								</Table.Cell>
								<Table.Cell data-label={t('webstack.page.dataFramework')}>
									<div class="flex flex-col items-start gap-1">
										<Badge variant="secondary" class="uppercase tracking-wide font-medium">
											{frameworkBadgeLabel(site.framework)}
										</Badge>
										{#if managed}
											{@const ramLabel = isAppRunning(site) ? formatMemoryBytes(site.app_status?.memory_bytes) : ''}
											<div class="flex items-center gap-1.5 whitespace-nowrap">
												<StatusBadge tone={appRuntimeTone(site)} label={appRuntimeLabel(site)} />
												{#if ramLabel}
													<span class="whitespace-nowrap font-mono text-[11px] font-medium tabular-nums text-muted-foreground">
														{t('webstack.page.ramBadge', { ram: ramLabel })}
													</span>
												{/if}
											</div>
										{/if}
									</div>
								</Table.Cell>
								<Table.Cell data-label={t('webstack.page.dataTarget')} class="font-mono text-xs">
									{#if isManagedApp(site)}
										<div class="flex flex-col gap-0.5">
											<span class="font-medium text-foreground">127.0.0.1:{site.proxy_port}</span>
											{#if site.app?.service_name}
												<span class="truncate text-[11px] text-muted-foreground max-w-[200px] sm:max-w-xs" title={`${site.app.service_name}.service`}>
													{site.app.service_name}{isAppRunning(site) && site.app_status?.main_pid ? ` · ${t('webstack.page.pidPrefix', { pid: site.app_status.main_pid })}` : ''}
												</span>
											{/if}
										</div>
									{:else if site.framework === 'proxy'}
										<span class="text-foreground">{t('webstack.page.forwardPrefix', { port: site.proxy_port })}</span>
									{:else if site.framework === 'static'}
										<span class="text-foreground">{t('webstack.page.staticHtml')}</span>
									{:else}
										<span class="text-foreground">PHP {site.php_version || '8.4'}</span>
									{/if}
								</Table.Cell>
								<Table.Cell data-label={t('webstack.page.dataRoot')} class="sm:text-center">
									{#if site.root_path && site.framework !== 'proxy'}
										<Button
											href={`/files?path=${encodeURIComponent(site.root_path)}`}
											variant="outline"
											size="icon-sm"
											title={t('webstack.page.openInFilesTitle', { path: site.root_path })}
											aria-label={t('webstack.page.openInFilesTitle', { path: site.root_path })}
											class="text-primary hover:border-primary/40 hover:bg-primary/5"
										>
											<FolderOpenIcon class="size-3.5" />
										</Button>
									{:else}
										<span class="text-xs text-muted-foreground/50 select-none">—</span>
									{/if}
								</Table.Cell>
								<Table.Cell data-label={t('webstack.page.dataSsl')}>
									{#if isEdge && site.exposed_to}
										<div class="flex flex-col items-start gap-1">
											<StatusBadge tone="success" label={t('webstack.page.edgeHttpsBadge')} />
											<span class="text-[11px] leading-tight text-muted-foreground">
												{t('webstack.page.sslTerminatedVps')}
											</span>
										</div>
									{:else if site.ssl_enabled}
										<div class="flex flex-col items-start gap-1">
											{#if sslExpiry?.expired}
												<StatusBadge tone="destructive" label={hasLetsEncrypt ? "Let's Encrypt" : 'HTTPS'} />
											{:else if sslExpiry?.expiringSoon}
												<StatusBadge tone="warning" label={hasLetsEncrypt ? "Let's Encrypt" : 'HTTPS'} />
											{:else if site.ssl_provider === 'letsencrypt'}
												<StatusBadge tone="success" label="Let's Encrypt" />
											{:else if isVPS && site.ssl_provider === 'local'}
												<StatusBadge tone="warning" label={t('webstack.page.sslLocalBadge')} />
											{:else}
												<StatusBadge tone="success" label="HTTPS" />
											{/if}
											{#if sslExpiry}
												<span
													class="text-[11px] leading-tight whitespace-nowrap {sslExpiry.expired
														? 'font-medium text-destructive'
														: sslExpiry.expiringSoon
														? 'font-medium text-warning'
														: 'text-muted-foreground'}"
													title={t('webstack.page.expiresTitle', { date: sslExpiry.fullTitle })}
												>
													{sslExpiry.text}
												</span>
											{/if}
										</div>
									{:else}
										<StatusBadge
											tone="neutral"
											label={isEdge ? t('webstack.page.httpInternal') : t('webstack.page.httpLabel')}
											showIcon={false}
										/>
									{/if}
								</Table.Cell>
								{#if isEdge}
									<Table.Cell data-label={t('webstack.page.dataEdgeTunnel')}>
										{#if site.exposed_to}
											<div class="flex flex-col items-start gap-1">
												<a
													href={`https://${site.exposed_to}`}
													target="_blank"
													rel="noopener noreferrer"
													class="inline-flex items-center gap-1 text-xs font-medium text-primary hover:underline"
												>
													<StatusBadge tone="success" label={site.exposed_to} />
												</a>
												<span class="font-mono text-[11px] text-muted-foreground">
													{modeStore.publicIP ? `VPS ${modeStore.publicIP} ⇄ ` : ''}wt0 :80
												</span>
											</div>
										{:else}
											<Button
												variant="outline"
												size="xs"
												disabled={Boolean(sslPending[site.domain])}
												onclick={() => openExposeModal(site)}
											>
												<CloudUploadIcon data-icon="inline-start" />
												{t('webstack.page.exposeTunnelBtn')}
											</Button>
										{/if}
									</Table.Cell>
								{/if}
								<Table.Cell data-label={t('webstack.page.dataActions')} class="w-[1%] whitespace-nowrap text-right">
									{@const busySite =
										Boolean(sslPending[site.domain]) ||
										Boolean(siteDeleting[site.domain]) ||
										Boolean(appActionPending[site.domain])}
									{@const appRunning = site.app_status?.active_state === 'active'}
									<div class="flex items-center justify-end gap-1.5 whitespace-nowrap">
										{#if managed}
											<Button
												variant="outline"
												size="xs"
												disabled={busySite}
												onclick={() => openManageApp(site)}
											>
												<SlidersHorizontalIcon data-icon="inline-start" />
												{t('webstack.page.manageBtn')}
											</Button>
										{/if}
									<DropdownMenu.Root>
										<DropdownMenu.Trigger>
											{#snippet child({ props })}
												<Button variant="outline" size="xs" disabled={busySite} {...props}>
													{#if sslPending[site.domain]}
														<Spinner data-icon="inline-start" />
														{t('webstack.page.busyProcessing')}
													{:else if siteDeleting[site.domain]}
														<Spinner data-icon="inline-start" />
														{t('webstack.page.busyDeleting')}
													{:else if appActionPending[site.domain]}
														<Spinner data-icon="inline-start" />
														{t('webstack.page.busyService')}
													{:else}
														{t('webstack.page.actionsBtn')}
														<ChevronDownIcon data-icon="inline-end" />
													{/if}
												</Button>
											{/snippet}
										</DropdownMenu.Trigger>
										<DropdownMenu.Content align="end" class="w-56">
											<DropdownMenu.Group>
												<DropdownMenu.Label class="truncate text-xs text-muted-foreground">
													{site.domain}
												</DropdownMenu.Label>
												<DropdownMenu.Separator />
												<DropdownMenu.Item>
													{#snippet child({ props })}
														<a href={primaryHref} target="_blank" rel="noopener noreferrer" {...props}>
															<ExternalLinkIcon />
															<span>{isEdge && site.exposed_to ? t('webstack.page.menuOpenPublicUrl') : t('webstack.page.menuOpenSite')}</span>
														</a>
													{/snippet}
												</DropdownMenu.Item>
												{#if managed}
													<DropdownMenu.Item disabled={busySite} onclick={() => openManageApp(site)}>
														<CpuIcon />
														<span>{t('webstack.page.menuManageApp')}</span>
													</DropdownMenu.Item>
													{#if site.app?.build_command}
														<DropdownMenu.Item disabled={busySite} onclick={() => triggerQuickAppBuild(site)}>
															<HammerIcon />
															<span>{t('webstack.page.menuRebuildRestart')}</span>
														</DropdownMenu.Item>
													{/if}
													{#if appRunning}
														<DropdownMenu.Item disabled={busySite} onclick={() => runQuickAppAction(site, 'restart')}>
															<RotateCwIcon />
															<span>{t('webstack.page.menuRestartService')}</span>
														</DropdownMenu.Item>
														<DropdownMenu.Item disabled={busySite} onclick={() => runQuickAppAction(site, 'stop')}>
															<SquareIcon />
															<span>{t('webstack.page.menuStopService')}</span>
														</DropdownMenu.Item>
													{:else}
														<DropdownMenu.Item disabled={busySite} onclick={() => runQuickAppAction(site, 'start')}>
															<PlayIcon />
															<span>{t('webstack.page.menuStartService')}</span>
														</DropdownMenu.Item>
													{/if}
													<DropdownMenu.Separator />
												{/if}
												<DropdownMenu.Item disabled={busySite} onclick={() => openEdit(site)}>
													<PencilIcon />
													<span>{t('webstack.page.menuEditVhost')}</span>
												</DropdownMenu.Item>
												{#if site.root_path && site.framework !== 'proxy'}
													<DropdownMenu.Item>
														{#snippet child({ props })}
															<a href={`/files?path=${encodeURIComponent(site.root_path)}`} {...props}>
																<FolderOpenIcon />
																<span>{t('webstack.page.menuOpenFiles')}</span>
															</a>
														{/snippet}
													</DropdownMenu.Item>
												{/if}
												{#if isVPS}
													<DropdownMenu.Item disabled={busySite} onclick={() => triggerLetsEncrypt(site.domain)}>
														<ShieldCheckIcon />
														<span>{hasLetsEncrypt ? t('webstack.page.menuRenewSsl') : t('webstack.page.menuGenerateLe')}</span>
													</DropdownMenu.Item>
												{/if}
												{#if isEdge}
													{#if site.exposed_to}
														<DropdownMenu.Item
															disabled={busySite}
															onclick={() => exposeOrRenewEdge(site.domain, site.exposed_to, true)}
														>
															<ShieldCheckIcon />
															<span>{t('webstack.page.menuRenewLeVps')}</span>
														</DropdownMenu.Item>
														<DropdownMenu.Item
															disabled={busySite}
															onclick={() => openUnexposeModal(site)}
														>
															<LinkIcon />
															<span>{t('webstack.page.menuUnexposeTunnel')}</span>
														</DropdownMenu.Item>
													{:else}
														<DropdownMenu.Item disabled={busySite} onclick={() => openExposeModal(site)}>
															<CloudUploadIcon />
															<span>{t('webstack.page.menuExposeSslTunnel')}</span>
														</DropdownMenu.Item>
													{/if}
												{/if}
												<DropdownMenu.Separator />
												<DropdownMenu.Item
													variant="destructive"
													disabled={busySite}
													onclick={() => deleteSite(site)}
												>
													<Trash2Icon />
													<span>{t('webstack.page.menuDeleteSite')}</span>
												</DropdownMenu.Item>
											</DropdownMenu.Group>
										</DropdownMenu.Content>
									</DropdownMenu.Root>
								</div>
							</Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</ResponsiveTableWrapper>
		<PaginationBar
			bind:page={sitePage}
			totalItems={filteredSites.length}
			pageSize={PAGE_SIZE}
			itemLabel={t('webstack.page.paginationItemLabel')}
			class="rounded-lg border bg-card sm:rounded-none sm:border-0 sm:border-t mt-3 sm:mt-0"
		/>
	</div>
	{/if}
</div>

<CreateVhostModal
	bind:open={createOpen}
	siteToEdit={editingSite}
	{pdnsZones}
	{parentDomains}
	onRefreshZones={refreshZonesQuiet}
	onCreated={handleVhostCreated}
	onUpdated={async () => {
		await loadSites();
	}}
/>

<ManageSiteAppModal
	bind:open={manageAppOpen}
	site={manageAppSite}
	onUpdated={loadSites}
	onEditVhost={(site) => {
		openEdit(site);
	}}
/>

<!-- Modal Ekspos VHost ke VPS Edge Tunnel -->
<Dialog.Root bind:open={exposeOpen}>
	<Dialog.Content class="max-h-[90dvh] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] overflow-y-auto overscroll-contain sm:w-full sm:max-w-md">
		<Dialog.Header class="pr-8">
			<Dialog.Title>{t('webstack.page.exposeModal.title')}</Dialog.Title>
			<Dialog.Description class="break-words">
				{t('webstack.page.exposeModal.desc1')}<code class="break-all font-mono">{exposeLocalDomain}</code>{t('webstack.page.exposeModal.desc2')}
			</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={submitExposeModal} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="expose-pub-domain">{t('webstack.page.exposeModal.domainLabel')}</Field.FieldLabel>
					<Input
						id="expose-pub-domain"
						class="h-11 text-base sm:h-8 sm:text-sm"
						placeholder={parentDomains[0] ? `app.${parentDomains[0]}` : t('webstack.page.exposeModal.domainPlaceholderDefault')}
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						enterkeyhint="done"
						bind:value={exposePublicDomain}
						required
					/>
					<Field.FieldDescription>
						{modeStore.netbirdIP
							? t('webstack.page.exposeModal.descWithIp', { ip: modeStore.netbirdIP })
							: t('webstack.page.exposeModal.descNoIp')}
					</Field.FieldDescription>
				</Field.Field>

				<Field.Field orientation="horizontal" class="fm-touch-check items-start gap-2.5 sm:items-center">
					<Checkbox id="expose-use-ssl" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={exposeUseSSL} />
					<Field.FieldLabel for="expose-use-ssl" class="cursor-pointer py-0.5 font-normal leading-snug sm:py-0">
						{t('webstack.page.exposeModal.sslCheckbox')}
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>

			<Dialog.Footer class="grid grid-cols-2 gap-2 sm:flex sm:justify-end">
				<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (exposeOpen = false)}>{t('webstack.page.exposeModal.cancel')}</Button>
				<Button type="submit" class="min-h-10 sm:min-h-0" disabled={exposing}>
					{#if exposing}
						<Spinner data-icon="inline-start" />
						{t('webstack.page.exposeModal.exposing')}
					{:else}
						<CloudUploadIcon data-icon="inline-start" />
						<span class="truncate">{t('webstack.page.exposeModal.submit')}</span>
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<!-- Modal Cabut Eksposur Tunnel -->
<Dialog.Root bind:open={unexposeOpen}>
	<Dialog.Content class="max-h-[90dvh] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] overflow-y-auto overscroll-contain sm:w-full sm:max-w-md">
		<Dialog.Header class="pr-8">
			<Dialog.Title>{t('webstack.page.unexposeModal.title')}</Dialog.Title>
			<Dialog.Description class="break-words">
				{t('webstack.page.unexposeModal.desc1')}<strong class="break-all font-mono">{unexposeTarget?.exposed_to}</strong>{t('webstack.page.unexposeModal.desc2')}<strong class="break-all font-mono">{unexposeTarget?.domain}</strong>{t('webstack.page.unexposeModal.desc3')}
			</Dialog.Description>
		</Dialog.Header>

		<div class="py-1">
			<Field.Field orientation="horizontal" class="fm-touch-check items-start gap-2.5 sm:items-center">
				<Checkbox id="unexpose-del-dns" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={unexposeDeleteDNS} />
				<Field.FieldLabel for="unexpose-del-dns" class="cursor-pointer py-0.5 font-normal leading-snug sm:py-0">
					{t('webstack.page.unexposeModal.deleteDnsCheckbox')}
				</Field.FieldLabel>
			</Field.Field>
		</div>

		<Dialog.Footer class="grid grid-cols-2 gap-2 sm:flex sm:justify-end">
			<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (unexposeOpen = false)}>{t('webstack.page.unexposeModal.cancel')}</Button>
			<Button variant="destructive" class="min-h-10 sm:min-h-0" disabled={unexposing} onclick={confirmUnexpose}>
				{#if unexposing}
					<Spinner data-icon="inline-start" />
					{t('webstack.page.unexposeModal.unexposing')}
				{:else}
					<span class="truncate">{t('webstack.page.unexposeModal.submit')}</span>
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Modal Konfirmasi Hapus Situs (Mode Edge & VPS & Managed App) -->
<Dialog.Root bind:open={deleteOpen}>
	<Dialog.Content class="max-h-[90dvh] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] overflow-y-auto overscroll-contain sm:w-full sm:max-w-md">
		<Dialog.Header class="pr-8">
			<Dialog.Title>{t('webstack.page.deleteModal.title')}</Dialog.Title>
			<Dialog.Description class="break-words">
				{t('webstack.page.deleteModal.desc1')}<strong class="break-all font-mono">{deleteTarget?.domain}</strong>{t('webstack.page.deleteModal.desc2')}
			</Dialog.Description>
		</Dialog.Header>

		<div class="flex flex-col gap-3 py-1">
			{#if deleteTarget && isManagedApp(deleteTarget)}
				<Alert.Root>
					<CpuIcon />
					<Alert.Title>{t('webstack.page.deleteModal.systemdTitle')}</Alert.Title>
					<Alert.Description class="min-w-0 text-xs">
						{t('webstack.page.deleteModal.systemdDesc1')}<code class="break-all font-mono">{deleteTarget.app?.service_name || `wdev-app-${deleteTarget.domain}`}.service</code>{t('webstack.page.deleteModal.systemdDesc2')}<code>.env</code>{t('webstack.page.deleteModal.systemdDesc3')}<code class="font-mono break-all">{deleteTarget.root_path}</code>{t('webstack.page.deleteModal.systemdDesc4')}
					</Alert.Description>
				</Alert.Root>
			{/if}

			{#if isEdge && deleteTarget?.exposed_to}
				<Field.Field orientation="horizontal" class="fm-touch-check items-start gap-2.5 sm:items-center">
					<Checkbox id="del-unexpose" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={deleteUnexpose} />
					<Field.FieldLabel for="del-unexpose" class="cursor-pointer py-0.5 font-normal leading-snug sm:py-0">
						{t('webstack.page.deleteModal.unexposeCheckbox')}<code class="break-all font-mono">{deleteTarget.exposed_to}</code>
					</Field.FieldLabel>
				</Field.Field>
			{/if}

			{#if (isEdge && deleteTarget?.exposed_to && deleteUnexpose) || (isVPS && deleteTarget && !isLocalOnlyDomain(deleteTarget.domain))}
				<Field.Field orientation="horizontal" class="fm-touch-check items-start gap-2.5 sm:items-center">
					<Checkbox id="del-dns" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={deleteDNSRecord} />
					<Field.FieldLabel for="del-dns" class="cursor-pointer py-0.5 font-normal leading-snug sm:py-0">
						{t('webstack.page.deleteModal.deleteDnsCheckbox')}
					</Field.FieldLabel>
				</Field.Field>
			{/if}
		</div>

		<Dialog.Footer class="grid grid-cols-2 gap-2 sm:flex sm:justify-end">
			<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (deleteOpen = false)}>{t('webstack.page.deleteModal.cancel')}</Button>
			<Button
				variant="destructive"
				class="min-h-10 sm:min-h-0"
				disabled={Boolean(deleteTarget && siteDeleting[deleteTarget.domain])}
				onclick={() =>
					deleteTarget &&
					executeDeleteSite(deleteTarget.domain, {
						unexpose: isEdge && Boolean(deleteTarget.exposed_to) && deleteUnexpose,
						delete_dns: deleteDNSRecord
					})}
			>
				{#if deleteTarget && siteDeleting[deleteTarget.domain]}
					<Spinner data-icon="inline-start" />
					{t('webstack.page.deleteModal.deleting')}
				{:else}
					<Trash2Icon data-icon="inline-start" />
					<span class="truncate sm:hidden">{t('webstack.page.deleteModal.deleteShort')}</span>
					<span class="hidden sm:inline">{t('webstack.page.deleteModal.deleteFull')}</span>
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
