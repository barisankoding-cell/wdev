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

	function frameworkBadgeLabel(fw: string): string {
		if (fw === 'nodejs') return 'NODE.JS';
		if (fw === 'go') return 'GO';
		if (fw === 'rust') return 'RUST';
		return fw.toUpperCase();
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
		if (st === 'active') return 'Running';
		if (st === 'activating') return 'Starting';
		if (st === 'failed') return 'Failed';
		return 'Stopped';
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

	async function loadSites() {
		loading = true;
		try {
			const [siteRes, zoneRes] = await Promise.allSettled([
				api.listSites(),
				supportsDNS ? api.listPDNSZones() : Promise.resolve([] as ZoneInfo[])
			]);
			if (siteRes.status === 'fulfilled') {
				sites = siteRes.value ?? [];
			} else {
				toast.error(
					siteRes.reason instanceof Error
						? siteRes.reason.message
						: 'Gagal memuat daftar virtual host'
				);
			}
			if (zoneRes.status === 'fulfilled') {
				zones = zoneRes.value ?? [];
			} else if (!supportsDNS) {
				zones = [];
			}
		} finally {
			loading = false;
		}
	}

	onMount(loadSites);

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
				`Mengontrol service systemd untuk ${site.domain} memerlukan hak akses administrator (sudo).`,
				(sudoPass) => api.siteAppAction(site.domain, action, sudoPass)
			);
			if (!res) return;
			const verb = action === 'start' ? 'dijalankan' : action === 'stop' ? 'dihentikan' : 'di-restart';
			toast.success(`Service ${site.domain} berhasil ${verb}.`);
			await loadSites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : `Gagal menjalankan ${action}`);
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
				`Menjalankan build & restart service ${site.domain} memerlukan hak akses administrator (sudo).`,
				(sudoPass) => api.buildSiteApp(site.domain, true, sudoPass)
			);
			if (!res) return;
			cp.openTerminal(`Build ${site.domain} (${frameworkBadgeLabel(site.framework)})`);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memulai proses build');
		} finally {
			const next = { ...appActionPending };
			delete next[site.domain];
			appActionPending = next;
		}
	}

	async function triggerLetsEncrypt(siteDomain: string, skipConfirm = false) {
		const clean = siteDomain.trim().replace(/\.$/, '').toLowerCase();
		if (isLocalOnlyDomain(clean)) {
			toast.warning(`Domain lokal (${clean}) tidak dapat diterbitkan sertifikat Let's Encrypt.`);
			return;
		}
		if (!skipConfirm) {
			const confirmed = await cp.confirm({
				title: "Generate SSL Let's Encrypt",
				description: `Terbitkan dan pasang sertifikat HTTPS Let's Encrypt (certbot) untuk ${clean}? Pastikan DNS domain sudah mengarah ke IP publik VPS dan port 80/443 terbuka.`,
				confirmText: 'Generate SSL'
			});
			if (!confirmed) return;
		}

		sslPending = { ...sslPending, [clean]: true };
		try {
			const res = await cp.runWithSudoRetry(
				`Penerbitan sertifikat Let's Encrypt untuk ${clean} memerlukan hak akses administrator (sudo).`,
				(sudoPass) => api.enableSiteSSL(clean, '', sudoPass)
			);
			if (res) {
				cp.openTerminal(`SSL Let's Encrypt: ${clean}`);
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : "Gagal memulai penerbitan SSL Let's Encrypt");
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
			toast.warning('Masukkan domain publik FQDN yang valid untuk VPS Edge.');
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
					? `${cleanPub} terekspos via VPS Edge Tunnel dengan SSL Let's Encrypt.`
					: `${cleanPub} berhasil diekspos via VPS Edge Tunnel.`
			);
			await loadSites();
			return true;
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal mengekspos situs ke VPS Edge');
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
			toast.success(`Eksposur tunnel untuk ${target.exposed_to} dicabut.`);
			unexposeOpen = false;
			unexposeTarget = null;
			await loadSites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal mencabut eksposur tunnel');
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
		const dateLabel = dt.toLocaleDateString('id-ID', {
			day: 'numeric',
			month: 'short',
			year: 'numeric'
		});
		const fullTitle = dt.toLocaleString('id-ID', {
			dateStyle: 'full',
			timeStyle: 'short'
		});
		const expired = typeof daysLeft === 'number' ? daysLeft < 0 : dt.getTime() < Date.now();
		const expiringSoon = !expired && typeof daysLeft === 'number' && daysLeft <= 14;
		let text = `s/d ${dateLabel}`;
		if (expired) {
			text = `Kedaluwarsa ${dateLabel}`;
		} else if (typeof daysLeft === 'number') {
			text = daysLeft === 0 ? `s/d ${dateLabel} (<1 hari)` : `s/d ${dateLabel} (${daysLeft} hari)`;
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
				`Penghapusan konfigurasi Nginx untuk ${siteDomain} memerlukan hak akses administrator (sudo).`,
				(sudoPass) =>
					api.deleteSite(siteDomain, {
						...opts,
						...(sudoPass ? { sudo_pass: sudoPass } : {})
					})
			);
			if (!res) return;
			if (res?.unexpose_error) {
				toast.warning(
					`Virtual host ${siteDomain} dihapus, tetapi pencabutan proxy VPS Edge gagal: ${res.unexpose_error}`
				);
			} else if (res?.unexposed) {
				toast.success(
					`Virtual host ${siteDomain} & eksposur VPS Edge berhasil dihapus${res.dns_deleted ? ' beserta record DNS' : ''}.`
				);
			} else if (res?.dns_deleted) {
				toast.success(`Virtual host ${siteDomain} & record DNS A berhasil dihapus.`);
			} else {
				toast.success(`Virtual host ${siteDomain} berhasil dihapus (folder proyek tetap aman).`);
			}
			deleteOpen = false;
			deleteTarget = null;
			await loadSites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus virtual host');
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
			title: 'Hapus Virtual Host',
			description: `Hapus konfigurasi Nginx dan entri /etc/hosts untuk ${site.domain}? Berkas dan folder proyek di ${site.root_path || '/var/www/' + site.domain} tidak akan dihapus.`,
			confirmText: 'Hapus VHost',
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
				{isLocal ? 'Web Stack & Domain Lokal' : 'Web Stack & Virtual Host'}
			</h1>
			<p class="text-sm text-muted-foreground">
				{#if isVPS}
					Kelola virtual host Nginx (<code>wdev site</code>), sinkronisasi PowerDNS, dan sertifikat SSL Let's Encrypt langsung di VPS.
				{:else if isEdge}
					Kelola virtual host Nginx publik seperti mode VPS, dengan trafik, DNS, dan Let's Encrypt dialirkan lewat tunnel NetBird ke VPS Edge Gateway.
				{:else}
					Kelola virtual host Nginx (<code>wdev site</code>), entri <code>/etc/hosts</code>, dan sertifikat SSL lokal (<code>mkcert</code>).
				{/if}
			</p>
		</div>
		<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
			{#if !isVPS}
				<Button variant="outline" size="sm" class="min-h-10 sm:min-h-0" onclick={() => cp.triggerInstall('sync-hosts')}>
					<RefreshCwIcon data-icon="inline-start" />
					Sync Hosts &amp; SSL
				</Button>
			{/if}
			<Button size="sm" class={cn('min-h-10 sm:min-h-0', isVPS && 'col-span-2 sm:col-span-1')} onclick={openCreate}>
				<PlusIcon data-icon="inline-start" />
				Tambah VHost
			</Button>
		</div>
	</div>

	{#if isEdge && !modeStore.reachable}
		<Alert.Root variant="destructive">
			<TriangleAlertIcon />
			<Alert.Title>VPS Edge Gateway Belum Terhubung</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					Tunnel ke VPS Edge belum terhubung. Anda tetap bisa membuat Virtual Host lokal, lalu mengeksposnya ke VPS beserta Let's Encrypt setelah terhubung di menu PowerDNS &amp; Edge.
				</span>
				<Button size="xs" variant="outline" href="/powerdns">
					<NetworkIcon data-icon="inline-start" />
					Buka PowerDNS &amp; Edge
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
					placeholder="Cari domain, framework, unit, atau path..."
					aria-label="Cari virtual host"
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
			aria-label="Filter tipe virtual host"
		>
			<ToggleGroup.Item value="all" class="min-h-9 text-xs sm:min-h-8 sm:px-3">Semua</ToggleGroup.Item>
			<ToggleGroup.Item value="web" class="min-h-9 text-xs sm:min-h-8 sm:px-3">Web / PHP</ToggleGroup.Item>
			<ToggleGroup.Item value="app" class="min-h-9 text-xs sm:min-h-8 sm:px-3">Managed App</ToggleGroup.Item>
			<ToggleGroup.Item value="proxy" class="min-h-9 text-xs sm:min-h-8 sm:px-3">Proxy</ToggleGroup.Item>
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
					{sites.length === 0 ? 'Belum ada virtual host' : 'Tidak ada virtual host yang cocok'}
				</Empty.Title>
				<Empty.Description>
					{sites.length === 0
						? 'Buat virtual host pertama untuk situs statis, Laravel, WordPress, atau reverse proxy.'
						: 'Ubah kata kunci pencarian untuk menampilkan virtual host lainnya.'}
				</Empty.Description>
			</Empty.Header>
			{#if sites.length === 0}
				<Empty.Content>
					<Button onclick={openCreate}>
						<PlusIcon data-icon="inline-start" />
						Tambah Virtual Host
					</Button>
				</Empty.Content>
			{/if}
		</Empty.Root>
	{:else}
		<div class="flex flex-col sm:overflow-hidden sm:rounded-lg sm:border sm:bg-card">
			<ResponsiveTableWrapper label="Daftar Virtual Host" class="rounded-none border-0 bg-transparent">
				<Table.Root class="sm:[&_th]:px-3 sm:[&_td]:px-3 sm:[&_td]:py-3">
					<Table.Header>
						<Table.Row>
							<Table.Head scope="col" class="text-xs font-semibold text-muted-foreground">
								{isLocal ? 'Domain Lokal' : 'Domain'}
							</Table.Head>
							<Table.Head scope="col" class="text-xs font-semibold text-muted-foreground">Framework</Table.Head>
							<Table.Head scope="col" class="text-xs font-semibold text-muted-foreground">Target Runtime</Table.Head>
							<Table.Head scope="col" class="text-xs font-semibold text-muted-foreground">Document Root</Table.Head>
							<Table.Head scope="col" class="text-xs font-semibold text-muted-foreground">SSL</Table.Head>
							{#if isEdge}
								<Table.Head scope="col" class="text-xs font-semibold text-muted-foreground">Tunnel VPS Edge</Table.Head>
							{/if}
							<Table.Head scope="col" class="text-right text-xs font-semibold text-muted-foreground">Aksi</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each paginatedSites as site (site.domain)}
						{@const proto = site.ssl_enabled ? 'https://' : 'http://'}
						{@const hasLetsEncrypt = site.ssl_enabled && site.ssl_provider === 'letsencrypt'}
						{@const sslExpiry = formatSSLExpiry(site.ssl_expires_at, site.ssl_days_left)}
						{@const primaryHref =
							isEdge && site.exposed_to && site.domain === site.exposed_to
								? `https://${site.exposed_to}`
								: `${proto}${site.domain}`}
						<Table.Row>
							<Table.Cell data-label="Domain">
								<a
									href={primaryHref}
									target="_blank"
									rel="noopener noreferrer"
									class="inline-flex items-center gap-1 break-all font-semibold text-primary hover:underline"
								>
									<span>{site.domain}</span>
									<ExternalLinkIcon class="size-3.5 shrink-0" />
								</a>
								{#if isEdge && site.exposed_to && site.exposed_to !== site.domain}
									<div class="text-xs text-muted-foreground">
										Publik: <span class="font-mono">{site.exposed_to}</span>
									</div>
								{/if}
							</Table.Cell>
							<Table.Cell data-label="Framework">
								<div class="flex flex-wrap items-center gap-1.5">
									<Badge variant={isManagedApp(site) ? 'default' : 'secondary'} class="uppercase">
										{frameworkBadgeLabel(site.framework)}
									</Badge>
									{#if isManagedApp(site)}
										<StatusBadge tone={appRuntimeTone(site)} label={appRuntimeLabel(site)} />
									{/if}
								</div>
							</Table.Cell>
							<Table.Cell data-label="Target" class="font-mono text-xs">
								{#if isManagedApp(site)}
									<div class="flex flex-col gap-0.5">
										<span>127.0.0.1:{site.proxy_port}</span>
										{#if site.app?.service_name}
											<span class="truncate text-[11px] text-muted-foreground" title={`${site.app.service_name}.service`}>
												{site.app.service_name}
											</span>
										{/if}
									</div>
								{:else if site.framework === 'proxy'}
									Forward :{site.proxy_port}
								{:else if site.framework === 'static'}
									Static HTML
								{:else}
									PHP {site.php_version || '8.4'}
								{/if}
							</Table.Cell>
							<Table.Cell data-label="Root">
								{#if site.root_path && site.framework !== 'proxy'}
									<a
										href={`/files?path=${encodeURIComponent(site.root_path)}`}
										title={`Buka ${site.root_path} di File Manager`}
										class="inline-flex max-w-full sm:max-w-72 items-center gap-1.5 font-mono text-xs text-primary hover:underline"
									>
										<FolderOpenIcon class="size-3.5 shrink-0" />
										<span class="break-all sm:truncate">{site.root_path}</span>
									</a>
								{:else}
									<code class="block max-w-full sm:max-w-72 break-all sm:truncate text-xs text-muted-foreground" title={site.root_path || '-'}>
										{site.root_path || '-'}
									</code>
								{/if}
							</Table.Cell>
							<Table.Cell data-label="SSL">
								{#if isEdge && site.exposed_to}
									<div class="flex flex-col items-start gap-1">
										<StatusBadge tone="success" label="VPS Edge HTTPS" />
										<span class="text-[11px] leading-tight text-muted-foreground">
											Terminasi SSL di VPS Gateway
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
											<StatusBadge tone="warning" label="SSL Lokal" />
										{:else}
											<StatusBadge tone="success" label="HTTPS" />
										{/if}
										{#if sslExpiry}
											<span
												class="text-[11px] leading-tight {sslExpiry.expired
													? 'font-medium text-destructive'
													: sslExpiry.expiringSoon
														? 'font-medium text-warning'
														: 'text-muted-foreground'}"
												title={`Berakhir pada ${sslExpiry.fullTitle}`}
											>
												{sslExpiry.text}
											</span>
										{/if}
									</div>
								{:else}
									<StatusBadge
										tone="neutral"
										label={isEdge ? 'HTTP Internal' : 'HTTP'}
										showIcon={false}
									/>
								{/if}
							</Table.Cell>
							{#if isEdge}
								<Table.Cell data-label="Tunnel VPS Edge">
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
											Ekspos Tunnel
										</Button>
									{/if}
								</Table.Cell>
							{/if}
							<Table.Cell data-label="Aksi" class="text-right">
								{@const busySite =
									Boolean(sslPending[site.domain]) ||
									Boolean(siteDeleting[site.domain]) ||
									Boolean(appActionPending[site.domain])}
								{@const managed = isManagedApp(site)}
								{@const appRunning = site.app_status?.active_state === 'active'}
								<div class="flex flex-wrap items-center justify-end gap-1.5">
									{#if managed}
										<Button
											variant="outline"
											size="xs"
											disabled={busySite}
											onclick={() => openManageApp(site)}
										>
											<SlidersHorizontalIcon data-icon="inline-start" />
											Kelola
										</Button>
									{/if}
									<DropdownMenu.Root>
										<DropdownMenu.Trigger>
											{#snippet child({ props })}
												<Button variant="outline" size="xs" disabled={busySite} {...props}>
													{#if sslPending[site.domain]}
														<Spinner data-icon="inline-start" />
														Memproses...
													{:else if siteDeleting[site.domain]}
														<Spinner data-icon="inline-start" />
														Menghapus...
													{:else if appActionPending[site.domain]}
														<Spinner data-icon="inline-start" />
														Service...
													{:else}
														Aksi
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
															<span>{isEdge && site.exposed_to ? 'Buka URL Publik' : 'Buka Situs'}</span>
														</a>
													{/snippet}
												</DropdownMenu.Item>
												{#if managed}
													<DropdownMenu.Item disabled={busySite} onclick={() => openManageApp(site)}>
														<CpuIcon />
														<span>Kelola App, .env &amp; Logs</span>
													</DropdownMenu.Item>
													{#if site.app?.build_command}
														<DropdownMenu.Item disabled={busySite} onclick={() => triggerQuickAppBuild(site)}>
															<HammerIcon />
															<span>Rebuild &amp; Restart</span>
														</DropdownMenu.Item>
													{/if}
													{#if appRunning}
														<DropdownMenu.Item disabled={busySite} onclick={() => runQuickAppAction(site, 'restart')}>
															<RotateCwIcon />
															<span>Restart Service</span>
														</DropdownMenu.Item>
														<DropdownMenu.Item disabled={busySite} onclick={() => runQuickAppAction(site, 'stop')}>
															<SquareIcon />
															<span>Stop Service</span>
														</DropdownMenu.Item>
													{:else}
														<DropdownMenu.Item disabled={busySite} onclick={() => runQuickAppAction(site, 'start')}>
															<PlayIcon />
															<span>Start Service</span>
														</DropdownMenu.Item>
													{/if}
													<DropdownMenu.Separator />
												{/if}
												<DropdownMenu.Item disabled={busySite} onclick={() => openEdit(site)}>
													<PencilIcon />
													<span>Ubah Virtual Host</span>
												</DropdownMenu.Item>
												{#if site.root_path && site.framework !== 'proxy'}
													<DropdownMenu.Item>
														{#snippet child({ props })}
															<a href={`/files?path=${encodeURIComponent(site.root_path)}`} {...props}>
																<FolderOpenIcon />
																<span>Buka File Manager</span>
															</a>
														{/snippet}
													</DropdownMenu.Item>
												{/if}
												{#if isVPS}
													<DropdownMenu.Item disabled={busySite} onclick={() => triggerLetsEncrypt(site.domain)}>
														<ShieldCheckIcon />
														<span>{hasLetsEncrypt ? 'Perbarui SSL' : "Generate Let's Encrypt"}</span>
													</DropdownMenu.Item>
												{/if}
												{#if isEdge}
													{#if site.exposed_to}
														<DropdownMenu.Item
															disabled={busySite}
															onclick={() => exposeOrRenewEdge(site.domain, site.exposed_to, true)}
														>
															<ShieldCheckIcon />
															<span>Perbarui Let's Encrypt (VPS)</span>
														</DropdownMenu.Item>
														<DropdownMenu.Item
															disabled={busySite}
															onclick={() => openUnexposeModal(site)}
														>
															<LinkIcon />
															<span>Cabut Eksposur Tunnel</span>
														</DropdownMenu.Item>
													{:else}
														<DropdownMenu.Item disabled={busySite} onclick={() => openExposeModal(site)}>
															<CloudUploadIcon />
															<span>Ekspos &amp; SSL via Tunnel</span>
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
													<span>Hapus Situs</span>
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
			itemLabel="virtual host"
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
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-md">
		<Dialog.Header class="pr-8">
			<Dialog.Title>Ekspos ke VPS Edge Gateway</Dialog.Title>
			<Dialog.Description>
				Memetakan vhost lokal <code class="font-mono">{exposeLocalDomain}</code> ke VPS Edge lewat NetBird Tunnel, menambah record DNS A (jika zone ada di PowerDNS), dan menerbitkan Let's Encrypt di VPS.
			</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={submitExposeModal} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="expose-pub-domain">Domain Publik di VPS Edge</Field.FieldLabel>
					<Input
						id="expose-pub-domain"
						class="h-11 text-base sm:h-8 sm:text-sm"
						placeholder={parentDomains[0] ? `app.${parentDomains[0]}` : 'app.domainanda.com'}
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						enterkeyhint="done"
						bind:value={exposePublicDomain}
						required
					/>
					<Field.FieldDescription>
						{modeStore.netbirdIP
							? `Trafik VPS akan diteruskan ke http://${modeStore.netbirdIP}:80.`
							: 'Trafik VPS akan diteruskan ke IP NetBird mesin ini (port 80).'}
					</Field.FieldDescription>
				</Field.Field>

				<Field.Field orientation="horizontal" class="fm-touch-check items-start gap-2.5 sm:items-center">
					<Checkbox id="expose-use-ssl" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={exposeUseSSL} />
					<Field.FieldLabel for="expose-use-ssl" class="cursor-pointer py-0.5 font-normal leading-snug sm:py-0">
						Terbitkan Sertifikat SSL Let's Encrypt (HTTPS) di VPS Edge
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>

			<Dialog.Footer>
				<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (exposeOpen = false)}>Batal</Button>
				<Button type="submit" class="min-h-10 sm:min-h-0" disabled={exposing}>
					{#if exposing}
						<Spinner data-icon="inline-start" />
						Mengekspos...
					{:else}
						<CloudUploadIcon data-icon="inline-start" />
						Ekspos Sekarang
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<!-- Modal Cabut Eksposur Tunnel -->
<Dialog.Root bind:open={unexposeOpen}>
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-md">
		<Dialog.Header class="pr-8">
			<Dialog.Title>Cabut Eksposur Tunnel</Dialog.Title>
			<Dialog.Description>
				Lepaskan reverse proxy VPS Edge untuk <strong class="font-mono">{unexposeTarget?.exposed_to}</strong>. Virtual host lokal <strong class="font-mono">{unexposeTarget?.domain}</strong> akan tetap dipertahankan.
			</Dialog.Description>
		</Dialog.Header>

		<div class="py-1">
			<Field.Field orientation="horizontal" class="fm-touch-check items-start gap-2.5 sm:items-center">
				<Checkbox id="unexpose-del-dns" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={unexposeDeleteDNS} />
				<Field.FieldLabel for="unexpose-del-dns" class="cursor-pointer py-0.5 font-normal leading-snug sm:py-0">
					Hapus juga record DNS A / CNAME dari PowerDNS (bila subdomain)
				</Field.FieldLabel>
			</Field.Field>
		</div>

		<Dialog.Footer>
			<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (unexposeOpen = false)}>Batal</Button>
			<Button variant="destructive" class="min-h-10 sm:min-h-0" disabled={unexposing} onclick={confirmUnexpose}>
				{#if unexposing}
					<Spinner data-icon="inline-start" />
					Mencabut...
				{:else}
					Cabut Eksposur
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Modal Konfirmasi Hapus Situs (Mode Edge & VPS) -->
<Dialog.Root bind:open={deleteOpen}>
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-md">
		<Dialog.Header class="pr-8">
			<Dialog.Title>Hapus Virtual Host</Dialog.Title>
			<Dialog.Description>
				Hapus konfigurasi Nginx lokal dan data situs untuk <strong class="font-mono">{deleteTarget?.domain}</strong>. Berkas web di document root tetap aman.
			</Dialog.Description>
		</Dialog.Header>

		<div class="flex flex-col gap-3 py-1">
			{#if deleteTarget && isManagedApp(deleteTarget)}
				<Alert.Root>
					<CpuIcon />
					<Alert.Title>Pembersihan Service Systemd</Alert.Title>
					<Alert.Description class="text-xs">
						Unit <code class="font-mono">{deleteTarget.app?.service_name || `wdev-app-${deleteTarget.domain}`}.service</code> dan file environment terkelola akan dihentikan serta dihapus permanen. Folder proyek di <code class="font-mono break-all">{deleteTarget.root_path}</code> tetap aman di disk.
					</Alert.Description>
				</Alert.Root>
			{/if}

			{#if isEdge && deleteTarget?.exposed_to}
				<Field.Field orientation="horizontal" class="fm-touch-check items-start gap-2.5 sm:items-center">
					<Checkbox id="del-unexpose" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={deleteUnexpose} />
					<Field.FieldLabel for="del-unexpose" class="cursor-pointer py-0.5 font-normal leading-snug sm:py-0">
						Cabut juga reverse proxy VPS Edge untuk <code class="font-mono">{deleteTarget.exposed_to}</code>
					</Field.FieldLabel>
				</Field.Field>
			{/if}

			{#if (isEdge && deleteTarget?.exposed_to && deleteUnexpose) || (isVPS && deleteTarget && !isLocalOnlyDomain(deleteTarget.domain))}
				<Field.Field orientation="horizontal" class="fm-touch-check items-start gap-2.5 sm:items-center">
					<Checkbox id="del-dns" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={deleteDNSRecord} />
					<Field.FieldLabel for="del-dns" class="cursor-pointer py-0.5 font-normal leading-snug sm:py-0">
						Hapus juga record DNS A dari PowerDNS (jika dikelola lokal/Edge)
					</Field.FieldLabel>
				</Field.Field>
			{/if}
		</div>

		<Dialog.Footer>
			<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (deleteOpen = false)}>Batal</Button>
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
					Menghapus...
				{:else}
					<Trash2Icon data-icon="inline-start" />
					Hapus Virtual Host
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
