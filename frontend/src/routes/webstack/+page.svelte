<script lang="ts">
	import { onMount } from 'svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import FolderPickerDialog from '#lib/components/modals/FolderPickerDialog.svelte';
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

	let sites = $state<SiteRecord[]>([]);
	let zones = $state<ZoneInfo[]>([]);
	let loading = $state(true);
	let searchQuery = $state('');
	let siteDeleting = $state<Record<string, boolean>>({});
	let sslPending = $state<Record<string, boolean>>({});
	let lastLoadedMode = $state<string | null>(null);

	// Modal Tambah VHost
	let createOpen = $state(false);
	let folderPickerOpen = $state(false);
	let creating = $state(false);
	let domainMode = $state<'custom' | 'subdomain'>('custom');
	let domain = $state('');
	let subdomainPrefix = $state('');
	let parentDomain = $state('');
	let domainTouched = $state(false);
	let framework = $state('laravel');
	let phpVersion = $state('8.4');
	let rootPath = $state('');
	let proxyPortStr = $state('3000');
	let sslEnabled = $state(true);
	let edgeExposeEnabled = $state(true);
	let edgeLocalExposeEnabled = $state(false);
	let edgePublicDomain = $state('');
	let edgeSSLEnabled = $state(true);
	let addLocalHosts = $state(false);

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

	const frameworks = [
		{ value: 'laravel', label: 'Laravel (root di /public)' },
		{ value: 'wordpress', label: 'WordPress' },
		{ value: 'ci4', label: 'CodeIgniter 4 (root di /public)' },
		{ value: 'ci3', label: 'CodeIgniter 3' },
		{ value: 'symfony', label: 'Symfony (root di /public)' },
		{ value: 'generic', label: 'Generic PHP' },
		{ value: 'static', label: 'Static HTML (index.html)' },
		{ value: 'proxy', label: 'Reverse Proxy (Node/Go/Python/Docker)' }
	];

	const phpVersions = ['8.4', '8.3', '8.2', '8.1'];
	const isProxy = $derived(framework === 'proxy');
	const isStatic = $derived(framework === 'static');
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

	const domainChoices = $derived(isEdge ? pdnsZones : parentDomains);

	const cleanSubPrefix = $derived(
		subdomainPrefix.trim().replace(/^\.+|\.+$/g, '').toLowerCase()
	);
	const effectiveDomain = $derived(
		supportsDNS && domainMode === 'subdomain'
			? !parentDomain
				? ''
				: cleanSubPrefix
					? `${cleanSubPrefix}.${parentDomain}`
					: isEdge
						? parentDomain
						: ''
			: domain.trim().replace(/^\.+|\.+$/g, '').toLowerCase()
	);
	const effectiveIsLocalDomain = $derived(
		effectiveDomain.length > 0 && isLocalOnlyDomain(effectiveDomain)
	);
	const domainInvalid = $derived(
		domainTouched &&
			(supportsDNS && domainMode === 'subdomain'
				? !parentDomain || (!isEdge && cleanSubPrefix.length === 0)
				: effectiveDomain.length === 0)
	);

	const filteredSites = $derived.by(() => {
		const q = searchQuery.trim().toLowerCase();
		if (!q) return sites;
		return sites.filter(
			(s) =>
				s.domain.toLowerCase().includes(q) ||
				s.framework.toLowerCase().includes(q) ||
				(s.root_path || '').toLowerCase().includes(q) ||
				(s.exposed_to || '').toLowerCase().includes(q)
		);
	});

	async function refreshZonesQuiet() {
		if (!supportsDNS) {
			zones = [];
			return;
		}
		try {
			zones = (await api.listPDNSZones()) ?? [];
			if (!parentDomain && domainChoices.length > 0) {
				parentDomain = domainChoices[0];
			}
			if (
				createOpen &&
				isEdge &&
				domainMode === 'custom' &&
				!domain.trim() &&
				!subdomainPrefix.trim() &&
				pdnsZones.length > 0
			) {
				domainMode = 'subdomain';
				parentDomain = pdnsZones[0];
			}
		} catch {
			// PowerDNS belum terhubung / opsional
		}
	}

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

	$effect(() => {
		if (domainChoices.length > 0 && (!parentDomain || !domainChoices.includes(parentDomain))) {
			parentDomain = domainChoices[0];
		}
	});

	function openCreate() {
		domainMode = isEdge && pdnsZones.length > 0 ? 'subdomain' : 'custom';
		domain = '';
		subdomainPrefix = '';
		parentDomain = domainChoices[0] ?? '';
		domainTouched = false;
		framework = 'laravel';
		phpVersion = '8.4';
		rootPath = '';
		proxyPortStr = '3000';
		sslEnabled = isLocal;
		edgeExposeEnabled = isEdge;
		edgeLocalExposeEnabled = false;
		edgePublicDomain = '';
		edgeSSLEnabled = true;
		addLocalHosts = false;
		createOpen = true;
		if (supportsDNS) {
			void refreshZonesQuiet();
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

	async function submitCreate(e: SubmitEvent) {
		e.preventDefault();
		domainTouched = true;
		if (supportsDNS && domainMode === 'subdomain') {
			if (!parentDomain) {
				toast.warning(
					isEdge
						? 'Pilih domain PowerDNS terlebih dahulu atau gunakan Input Manual.'
						: 'Pilih domain induk terlebih dahulu atau gunakan mode Domain Utama.'
				);
				return;
			}
			if (!isEdge && !cleanSubPrefix) return;
			if (
				cleanSubPrefix &&
				!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*$/.test(cleanSubPrefix)
			) {
				toast.warning('Nama subdomain hanya boleh berisi huruf, angka, tanda hubung (-), atau titik.');
				return;
			}
		}
		const cleanDomain = effectiveDomain;
		if (!cleanDomain) return;
		const proxyPort = parseInt(proxyPortStr, 10) || 0;
		if (isProxy && (proxyPort < 1 || proxyPort > 65535)) {
			toast.error('Port aplikasi proxy harus antara 1 sampai 65535.');
			return;
		}

		const cleanIsLocal = isLocalOnlyDomain(cleanDomain);
		const wantVPSLetsEncrypt = isVPS && sslEnabled && !cleanIsLocal;
		const wantEdgeTunnel = isEdge && (cleanIsLocal ? edgeLocalExposeEnabled : edgeExposeEnabled);
		const resolvedEdgeDomain = cleanIsLocal
			? edgePublicDomain.trim().replace(/\.$/, '').toLowerCase()
			: cleanDomain;
		if (wantEdgeTunnel && (!resolvedEdgeDomain || isLocalOnlyDomain(resolvedEdgeDomain))) {
			toast.warning('Untuk mengekspos via VPS Edge, masukkan domain publik FQDN yang valid (bukan .test/.local).');
			return;
		}

		creating = true;
		try {
			const res = await api.createSite({
				domain: cleanDomain,
				framework,
				php_version: isProxy || isStatic ? '' : phpVersion,
				root_path: rootPath.trim(),
				proxy_port: proxyPort,
				ssl_enabled: wantVPSLetsEncrypt || wantEdgeTunnel ? false : sslEnabled,
				...(isEdge && !cleanIsLocal ? { add_hosts: addLocalHosts } : {}),
				...(wantEdgeTunnel
					? {
							expose_edge: true,
							edge_domain: resolvedEdgeDomain,
							edge_ssl: edgeSSLEnabled
						}
					: {})
			});

			if (res?.edge_error) {
				toast.warning(
					`Virtual host ${cleanDomain} berhasil dibuat, namun eksposur VPS Edge gagal: ${res.edge_error}`
				);
			} else if (res?.edge_exposed) {
				toast.success(
					`Virtual host ${cleanDomain} dibuat & terekspos ke ${res.edge_domain || resolvedEdgeDomain}${res.dns_synced ? ' (DNS A & Proxy VPS aktif)' : ''}.`
				);
			} else if (res?.dns_synced && res?.dns_message) {
				toast.success(`Virtual host ${cleanDomain} berhasil dibuat (${res.dns_message}).`);
			} else {
				toast.success(`Virtual host ${cleanDomain} berhasil dibuat!`);
			}

			createOpen = false;
			await loadSites();
			if (wantVPSLetsEncrypt) {
				await triggerLetsEncrypt(cleanDomain, true);
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal membuat virtual host');
		} finally {
			creating = false;
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
			const res = await api.deleteSite(siteDomain, opts);
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
				toast.success(`Virtual host ${siteDomain} berhasil dihapus.`);
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
		if (hasEdgeExposure || hasVPSPublicDNS) {
			deleteTarget = site;
			deleteUnexpose = hasEdgeExposure;
			deleteDNSRecord = false;
			deleteOpen = true;
			return;
		}

		const confirmed = await cp.confirm({
			title: 'Hapus Virtual Host',
			description: `Hapus virtual host ${site.domain}? Tindakan ini menghapus konfigurasi Nginx dan entri /etc/hosts lokal.`,
			confirmText: 'Hapus',
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
				<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('sync-hosts')}>
					<RefreshCwIcon data-icon="inline-start" />
					Sync Hosts &amp; SSL
				</Button>
			{/if}
			<Button size="sm" class={isVPS ? 'col-span-2 sm:col-span-1' : ''} onclick={openCreate}>
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

	<search class="w-full sm:max-w-sm">
		<InputGroup.Root>
			<InputGroup.Addon align="inline-start">
				<SearchIcon />
			</InputGroup.Addon>
			<InputGroup.Input
				type="search"
				placeholder="Cari domain, framework, atau path..."
				aria-label="Cari virtual host"
				bind:value={searchQuery}
			/>
		</InputGroup.Root>
	</search>

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
		<ResponsiveTableWrapper label="Daftar Virtual Host">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>{isLocal ? 'Domain Lokal' : 'Domain'}</Table.Head>
						<Table.Head>Framework</Table.Head>
						<Table.Head>Target Runtime</Table.Head>
						<Table.Head>Document Root</Table.Head>
						<Table.Head>SSL</Table.Head>
						{#if isEdge}
							<Table.Head>Tunnel VPS Edge</Table.Head>
						{/if}
						<Table.Head class="text-right">Aksi</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each filteredSites as site (site.domain)}
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
								<Badge variant="secondary" class="uppercase">{site.framework}</Badge>
							</Table.Cell>
							<Table.Cell data-label="Target" class="font-mono text-xs">
								{site.framework === 'proxy'
									? `Forward :${site.proxy_port}`
									: site.framework === 'static'
										? 'Static HTML'
										: `PHP ${site.php_version || '8.4'}`}
							</Table.Cell>
							<Table.Cell data-label="Root" class="sm:max-w-52">
								{#if site.root_path && site.framework !== 'proxy'}
									<a
										href={`/files?path=${encodeURIComponent(site.root_path)}`}
										title={`Buka ${site.root_path} di File Manager`}
										class="inline-flex max-w-full items-center gap-1.5 font-mono text-xs text-primary hover:underline"
									>
										<FolderOpenIcon class="size-3.5 shrink-0" />
										<span class="break-all sm:truncate">{site.root_path}</span>
									</a>
								{:else}
									<code class="block break-all sm:truncate text-xs text-muted-foreground" title={site.root_path || '-'}>
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
								{@const busySite = Boolean(sslPending[site.domain]) || Boolean(siteDeleting[site.domain])}
								<div class="flex justify-end">
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
	{/if}
</div>

<Dialog.Root bind:open={createOpen}>
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>Buat Virtual Host Baru</Dialog.Title>
			<Dialog.Description>
				{#if isVPS}
					Membuat konfigurasi Nginx (<code>wdev site create</code>) langsung aktif di VPS publik.
				{:else if isEdge}
					Membuat virtual host Nginx lokal dan (untuk domain publik) mengeksposnya via NetBird Tunnel beserta DNS PowerDNS &amp; SSL Let's Encrypt di VPS Edge.
				{:else}
					Membuat konfigurasi Nginx (<code>wdev site create</code>), entri <code>/etc/hosts</code>, dan sertifikat SSL lokal.
				{/if}
			</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={submitCreate} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field data-invalid={domainInvalid ? 'true' : undefined}>
					<div class="flex flex-wrap items-center justify-between gap-2">
						<Field.FieldLabel for={supportsDNS && domainMode === 'subdomain' ? 'site-sub-prefix' : 'site-domain'}>
							{isLocal ? 'Domain Lokal' : 'Domain'}
						</Field.FieldLabel>
						{#if supportsDNS}
							<div class="inline-flex rounded-lg border bg-muted p-0.5 text-xs" role="group" aria-label="Mode input domain">
								{#if isEdge}
									<button
										type="button"
										class={domainMode === 'subdomain'
											? 'rounded-md bg-background px-2.5 py-1 font-medium text-foreground shadow-xs'
											: 'rounded-md px-2.5 py-1 text-muted-foreground hover:text-foreground'}
										onclick={() => {
											domainMode = 'subdomain';
											domainTouched = false;
											if (!parentDomain && domainChoices.length > 0) {
												parentDomain = domainChoices[0];
											}
											void refreshZonesQuiet();
										}}
									>
										PowerDNS ({domainChoices.length})
									</button>
									<button
										type="button"
										class={domainMode === 'custom'
											? 'rounded-md bg-background px-2.5 py-1 font-medium text-foreground shadow-xs'
											: 'rounded-md px-2.5 py-1 text-muted-foreground hover:text-foreground'}
										onclick={() => {
											domainMode = 'custom';
											domainTouched = false;
										}}
									>
										Manual / .test
									</button>
								{:else}
									<button
										type="button"
										class={domainMode === 'custom'
											? 'rounded-md bg-background px-2.5 py-1 font-medium text-foreground shadow-xs'
											: 'rounded-md px-2.5 py-1 text-muted-foreground hover:text-foreground'}
										onclick={() => {
											domainMode = 'custom';
											domainTouched = false;
										}}
									>
										Domain Utama
									</button>
									<button
										type="button"
										class={domainMode === 'subdomain'
											? 'rounded-md bg-background px-2.5 py-1 font-medium text-foreground shadow-xs'
											: 'rounded-md px-2.5 py-1 text-muted-foreground hover:text-foreground'}
										onclick={() => {
											domainMode = 'subdomain';
											domainTouched = false;
											if (!parentDomain && domainChoices.length > 0) {
												parentDomain = domainChoices[0];
											}
											void refreshZonesQuiet();
										}}
									>
										Subdomain ({domainChoices.length})
									</button>
								{/if}
							</div>
						{/if}
					</div>

					{#if supportsDNS && domainMode === 'subdomain'}
						{#if domainChoices.length === 0}
							<div class="rounded-md border border-dashed bg-muted/40 px-3 py-2.5 text-xs text-muted-foreground">
								{isEdge
									? 'Belum ada zone di PowerDNS VPS Edge. Tambahkan zone di menu PowerDNS & Edge terlebih dahulu, atau pilih Manual / .test.'
									: 'Belum ada domain atau zone PowerDNS yang tersedia sebagai induk subdomain. Tambahkan zone di menu PowerDNS terlebih dahulu atau pilih Domain Utama.'}
							</div>
						{:else}
							<div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)]">
								<InputGroup.Root>
									<InputGroup.Input
										id="site-sub-prefix"
										name="subdomain_prefix"
										placeholder={isEdge ? 'app / api (kosong = utama)' : 'app / api / admin'}
										autocorrect="off"
										autocapitalize="none"
										spellcheck={false}
										aria-invalid={domainInvalid ? 'true' : undefined}
										bind:value={subdomainPrefix}
										onblur={() => (domainTouched = true)}
										oninput={() => {
											if (subdomainPrefix.trim()) domainTouched = false;
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
									}}
								>
									<Select.Trigger id="site-parent-domain" class="w-full" aria-label="Pilih domain PowerDNS">
										{parentDomain || 'Pilih domain PowerDNS'}
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
								<span>Domain hasil:</span>
								<code class="rounded bg-muted px-1.5 py-0.5 font-mono text-foreground">
									{effectiveDomain || `<subdomain>.${parentDomain}`}
								</code>
							</div>
						{/if}
					{:else}
						<Input
							id="site-domain"
							name="domain"
							placeholder={isLocal ? 'proyekanda.test' : 'domainanda.com'}
							autocorrect="off"
							autocapitalize="none"
							spellcheck={false}
							aria-invalid={domainInvalid ? 'true' : undefined}
							bind:value={domain}
							onblur={() => (domainTouched = true)}
							oninput={() => {
								if (domain.trim()) domainTouched = false;
							}}
							required
						/>
					{/if}

					<Field.FieldDescription>
						{#if supportsDNS && domainMode === 'subdomain'}
							{#if isVPS}
								Pilih domain induk dan isi awalan subdomain. Record DNS A otomatis dibuat bila zone dikelola PowerDNS lokal.
							{:else}
								Pilih zone PowerDNS lalu isi awalan subdomain (atau kosongkan awalan untuk memakai domain utama zone). Otomatis diekspos via VPS Edge Tunnel beserta SSL Let's Encrypt.
							{/if}
						{:else if isVPS}
							Masukkan domain FQDN yang mengarah ke IP publik VPS ini.
						{:else if isEdge}
							Masukkan domain publik (mis. <code>app.domainanda.com</code>) untuk langsung diekspos lewat VPS Edge, atau akhiran <code>.test</code> untuk pengembangan lokal saja.
						{:else}
							Akhiran <code>.test</code> disarankan untuk pengembangan lokal.
						{/if}
					</Field.FieldDescription>
					{#if domainInvalid}
						<Field.FieldError
							errors={[{ message: supportsDNS && domainMode === 'subdomain' ? (isEdge ? 'Pilih zone PowerDNS terlebih dahulu.' : 'Awalan subdomain dan domain induk wajib diisi.') : 'Domain wajib diisi.' }]}
						/>
					{/if}
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="site-framework">Framework / Pola Aplikasi</Field.FieldLabel>
					<Select.Root type="single" bind:value={framework}>
						<Select.Trigger id="site-framework" class="w-full">
							{frameworks.find((f) => f.value === framework)?.label ?? framework}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								{#each frameworks as fw (fw.value)}
									<Select.Item value={fw.value}>{fw.label}</Select.Item>
								{/each}
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				{#if isProxy}
					<Field.Field>
						<Field.FieldLabel for="site-proxy-port">Port Aplikasi Lokal</Field.FieldLabel>
						<Input
							id="site-proxy-port"
							name="proxy_port"
							inputmode="numeric"
							pattern="[0-9]+"
							placeholder="3000"
							bind:value={proxyPortStr}
							required
						/>
						<Field.FieldDescription>Contoh: 3000 (Node/Vite) atau 8080 (Go/Docker).</Field.FieldDescription>
					</Field.Field>
				{:else}
					{#if !isStatic}
						<Field.Field>
							<Field.FieldLabel for="site-php-ver">Versi PHP Target</Field.FieldLabel>
							<Select.Root type="single" bind:value={phpVersion}>
								<Select.Trigger id="site-php-ver" class="w-full">PHP {phpVersion}</Select.Trigger>
								<Select.Content>
									<Select.Group>
										{#each phpVersions as ver (ver)}
											<Select.Item value={ver}>PHP {ver}</Select.Item>
										{/each}
									</Select.Group>
								</Select.Content>
							</Select.Root>
						</Field.Field>
					{/if}

					<Field.Field>
						<Field.FieldLabel for="site-root-path">Document Root (Opsional)</Field.FieldLabel>
						<div class="flex gap-2">
							<Input
								id="site-root-path"
								name="root_path"
								class="min-w-0 flex-1"
								placeholder={effectiveDomain ? `/var/www/${effectiveDomain}` : (isLocal ? '/var/www/proyekanda.test' : '/var/www/domainanda.com')}
								autocorrect="off"
								autocapitalize="none"
								spellcheck={false}
								bind:value={rootPath}
							/>
							<Button
								type="button"
								variant="outline"
								class="shrink-0"
								onclick={() => (folderPickerOpen = true)}
							>
								<FolderOpenIcon data-icon="inline-start" />
								Cari Folder
							</Button>
						</div>
						<Field.FieldDescription>
							Kosongkan untuk otomatis di <code>/var/www/&lt;domain&gt;</code>, atau klik <strong>Cari Folder</strong> untuk memilih direktori yang sudah ada.
							{#if !isStatic}
								Untuk Laravel/CI4/Symfony pilih folder root proyek (<code>/public</code> ditambahkan otomatis).
							{/if}
						</Field.FieldDescription>
					</Field.Field>
				{/if}

				{#if isEdge}
					{#if effectiveIsLocalDomain}
						<div class="flex flex-col gap-3 rounded-lg border bg-muted/30 p-3">
							<Field.Field orientation="horizontal">
								<Checkbox id="site-ssl" bind:checked={sslEnabled} />
								<Field.FieldLabel for="site-ssl" class="font-normal">
									Aktifkan Sertifikat SSL Lokal (HTTPS mkcert)
								</Field.FieldLabel>
							</Field.Field>

							<Field.Field orientation="horizontal">
								<Checkbox id="site-edge-local-expose" bind:checked={edgeLocalExposeEnabled} />
								<Field.FieldLabel for="site-edge-local-expose" class="font-normal">
									Sekalian ekspos domain lokal ini ke domain publik via VPS Edge Tunnel
								</Field.FieldLabel>
							</Field.Field>

							{#if edgeLocalExposeEnabled}
								<div class="flex flex-col gap-3 pl-6">
									<Field.Field>
										<Field.FieldLabel for="site-edge-pub-dom">Domain Publik Target</Field.FieldLabel>
										<Input
											id="site-edge-pub-dom"
											placeholder={parentDomains[0] ? `app.${parentDomains[0]}` : 'app.domainanda.com'}
											autocorrect="off"
											autocapitalize="none"
											spellcheck={false}
											bind:value={edgePublicDomain}
											required
										/>
									</Field.Field>
									<Field.Field orientation="horizontal">
										<Checkbox id="site-edge-ssl" bind:checked={edgeSSLEnabled} />
										<Field.FieldLabel for="site-edge-ssl" class="font-normal">
											Terbitkan SSL Let's Encrypt (HTTPS) di VPS Edge
										</Field.FieldLabel>
									</Field.Field>
								</div>
							{/if}
						</div>
					{:else}
						<div class="flex flex-col gap-3 rounded-lg border bg-muted/30 p-3">
							<Field.Field orientation="horizontal">
								<Checkbox id="site-edge-expose" bind:checked={edgeExposeEnabled} />
								<Field.FieldLabel for="site-edge-expose" class="font-normal">
									Ekspos ke internet &amp; sinkronkan DNS via VPS Edge Tunnel (NetBird{modeStore.publicIP ? ` → ${modeStore.publicIP}` : ''})
								</Field.FieldLabel>
							</Field.Field>

							{#if edgeExposeEnabled}
								<div class="flex flex-col gap-2.5 pl-6">
									<Field.Field orientation="horizontal">
										<Checkbox id="site-edge-ssl" bind:checked={edgeSSLEnabled} />
										<Field.FieldLabel for="site-edge-ssl" class="font-normal">
											Terbitkan SSL Let's Encrypt (HTTPS) di VPS Edge Gateway
										</Field.FieldLabel>
									</Field.Field>
									<Field.Field orientation="horizontal">
										<Checkbox id="site-add-hosts" bind:checked={addLocalHosts} />
										<Field.FieldLabel for="site-add-hosts" class="font-normal">
											Tambahkan juga ke <code>/etc/hosts</code> lokal (opsional, untuk akses langsung 127.0.0.1)
										</Field.FieldLabel>
									</Field.Field>
								</div>
							{:else}
								<Field.Field orientation="horizontal">
									<Checkbox id="site-ssl" bind:checked={sslEnabled} />
									<Field.FieldLabel for="site-ssl" class="font-normal">
										Aktifkan Sertifikat SSL Lokal (HTTPS mkcert)
									</Field.FieldLabel>
								</Field.Field>
							{/if}
						</div>
					{/if}
				{:else if isVPS}
					<Field.Field orientation="horizontal">
						<Checkbox id="site-ssl" bind:checked={sslEnabled} />
						<Field.FieldLabel for="site-ssl" class="font-normal">
							Terbitkan SSL Let's Encrypt (HTTPS) setelah virtual host dibuat
						</Field.FieldLabel>
					</Field.Field>
				{:else}
					<Field.Field orientation="horizontal">
						<Checkbox id="site-ssl" bind:checked={sslEnabled} />
						<Field.FieldLabel for="site-ssl" class="font-normal">
							Aktifkan Sertifikat SSL Otomatis (HTTPS mkcert)
						</Field.FieldLabel>
					</Field.Field>
				{/if}
			</Field.FieldGroup>

			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (createOpen = false)}>Batal</Button>
				<Button type="submit" disabled={creating}>
					{#if creating}
						<Spinner data-icon="inline-start" />
						Membuat...
					{:else}
						<PlusIcon data-icon="inline-start" />
						Buat Virtual Host
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<!-- Modal Ekspos VHost ke VPS Edge Tunnel -->
<Dialog.Root bind:open={exposeOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
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
						placeholder={parentDomains[0] ? `app.${parentDomains[0]}` : 'app.domainanda.com'}
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						bind:value={exposePublicDomain}
						required
					/>
					<Field.FieldDescription>
						{modeStore.netbirdIP
							? `Trafik VPS akan diteruskan ke http://${modeStore.netbirdIP}:80.`
							: 'Trafik VPS akan diteruskan ke IP NetBird mesin ini (port 80).'}
					</Field.FieldDescription>
				</Field.Field>

				<Field.Field orientation="horizontal">
					<Checkbox id="expose-use-ssl" bind:checked={exposeUseSSL} />
					<Field.FieldLabel for="expose-use-ssl" class="font-normal">
						Terbitkan Sertifikat SSL Let's Encrypt (HTTPS) di VPS Edge
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>

			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (exposeOpen = false)}>Batal</Button>
				<Button type="submit" disabled={exposing}>
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
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Cabut Eksposur Tunnel</Dialog.Title>
			<Dialog.Description>
				Lepaskan reverse proxy VPS Edge untuk <strong class="font-mono">{unexposeTarget?.exposed_to}</strong>. Virtual host lokal <strong class="font-mono">{unexposeTarget?.domain}</strong> akan tetap dipertahankan.
			</Dialog.Description>
		</Dialog.Header>

		<div class="py-1">
			<Field.Field orientation="horizontal">
				<Checkbox id="unexpose-del-dns" bind:checked={unexposeDeleteDNS} />
				<Field.FieldLabel for="unexpose-del-dns" class="font-normal">
					Hapus juga record DNS A / CNAME dari PowerDNS (bila subdomain)
				</Field.FieldLabel>
			</Field.Field>
		</div>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (unexposeOpen = false)}>Batal</Button>
			<Button variant="destructive" disabled={unexposing} onclick={confirmUnexpose}>
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
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Hapus Virtual Host</Dialog.Title>
			<Dialog.Description>
				Hapus konfigurasi Nginx lokal dan data situs untuk <strong class="font-mono">{deleteTarget?.domain}</strong>. Berkas web di document root tetap aman.
			</Dialog.Description>
		</Dialog.Header>

		<div class="flex flex-col gap-3 py-1">
			{#if isEdge && deleteTarget?.exposed_to}
				<Field.Field orientation="horizontal">
					<Checkbox id="del-unexpose" bind:checked={deleteUnexpose} />
					<Field.FieldLabel for="del-unexpose" class="font-normal">
						Cabut juga reverse proxy VPS Edge untuk <code class="font-mono">{deleteTarget.exposed_to}</code>
					</Field.FieldLabel>
				</Field.Field>
			{/if}

			{#if (isEdge && deleteTarget?.exposed_to && deleteUnexpose) || (isVPS && deleteTarget && !isLocalOnlyDomain(deleteTarget.domain))}
				<Field.Field orientation="horizontal">
					<Checkbox id="del-dns" bind:checked={deleteDNSRecord} />
					<Field.FieldLabel for="del-dns" class="font-normal">
						Hapus juga record DNS A dari PowerDNS (jika dikelola lokal/Edge)
					</Field.FieldLabel>
				</Field.Field>
			{/if}
		</div>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (deleteOpen = false)}>Batal</Button>
			<Button
				variant="destructive"
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

<FolderPickerDialog
	bind:open={folderPickerOpen}
	initialPath={rootPath.trim()}
	onSelect={(picked) => {
		rootPath = picked;
	}}
/>
