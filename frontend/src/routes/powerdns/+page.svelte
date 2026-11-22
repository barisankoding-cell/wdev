<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PasswordInput from '#lib/components/shared/PasswordInput.svelte';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import { cn } from '#lib/utils.js';
	import type {
		EdgeConfig,
		ProxyExposure,
		RRSet,
		SiteRecord,
		ZoneDoctorReport,
		ZoneInfo
	} from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import ServerIcon from '@lucide/svelte/icons/server';
	import LinkIcon from '@lucide/svelte/icons/link';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import ListIcon from '@lucide/svelte/icons/list';
	import StethoscopeIcon from '@lucide/svelte/icons/stethoscope';
	import WrenchIcon from '@lucide/svelte/icons/wrench';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import CloudUploadIcon from '@lucide/svelte/icons/cloud-upload';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import InfoIcon from '@lucide/svelte/icons/info';
	import BookOpenIcon from '@lucide/svelte/icons/book-open';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import SearchIcon from '@lucide/svelte/icons/search';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import ArrowLeftRightIcon from '@lucide/svelte/icons/arrow-left-right';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import ChevronsLeftIcon from '@lucide/svelte/icons/chevrons-left';
	import ChevronsRightIcon from '@lucide/svelte/icons/chevrons-right';
	import UnplugIcon from '@lucide/svelte/icons/unplug';

	type ActiveTab = 'zones' | 'expose' | 'proxy';
	type ExposeFilter = 'all' | 'exposed' | 'local';
	type ProxyFilter = 'all' | 'vhost' | 'service';

	let edge = $state<EdgeConfig | null>(null);
	let zones = $state<ZoneInfo[]>([]);
	let sites = $state<SiteRecord[]>([]);
	let exposures = $state<ProxyExposure[]>([]);
	let initialLoading = $state(true);
	let zonesError = $state('');
	let exposuresError = $state('');
	let glueGuideOpen = $state(false);
	let refreshingAll = $state(false);
	let refreshingExposures = $state(false);
	let startingPDNS = $state(false);
	let lastLoadedMode = $state<string | null>(null);
	let zonePending = $state<Record<string, 'doctor' | 'repair' | 'delete'>>({});
	let unexposingPending = $state<Record<string, boolean>>({});
	let recordPending = $state<Record<string, boolean>>({});

	// Tabs & Filters
	let activeTab = $state<ActiveTab>('zones');
	let zoneTabRef = $state<HTMLButtonElement | null>(null);
	let exposeTabRef = $state<HTMLButtonElement | null>(null);
	let proxyTabRef = $state<HTMLButtonElement | null>(null);
	let zoneSearch = $state('');
	let exposeSearch = $state('');
	let exposeFilter = $state<ExposeFilter>('all');
	let proxySearch = $state('');
	let proxyFilter = $state<ProxyFilter>('all');

	// Add Zone Dialog
	let addZoneOpen = $state(false);
	let newZoneName = $state('');
	let createDefaultVhost = $state(true);
	let addingZone = $state(false);

	// Doctor / Repair Report Dialog
	let reportOpen = $state(false);
	let reportTitle = $state('');
	let reportData = $state<ZoneDoctorReport | null>(null);
	let reportLoading = $state(false);
	let reportLoadingMessage = $state('');
	let reportError = $state('');
	let reportRequestSeq = 0;

	// DNS Records Dialog
	let recordsOpen = $state(false);
	let currentZone = $state('');
	let rrsets = $state<RRSet[]>([]);
	let recordsLoading = $state(false);
	let recName = $state('');
	let recType = $state('A');
	let recContent = $state('');
	let recTTLStr = $state('300');
	let savingRecord = $state(false);

	// Expose Modal
	let exposeOpen = $state(false);
	let exposeMode = $state<'site' | 'port'>('site');
	let exposeSiteDomain = $state('');
	let exposeSitePublic = $state('');
	let exposePortPublic = $state('');
	let exposePortTargetStr = $state('3000');
	let exposeSSL = $state(true);
	let exposeWS = $state(true);
	let exposing = $state(false);

	// Deploy VPS Modal
	let deployOpen = $state(false);
	let deployHost = $state('');
	let deployUser = $state('root');
	let deployPortStr = $state('22');
	let deployAuthMethod = $state<'password' | 'key'>('password');
	let deployPassword = $state('');
	let deployKeyPath = $state('');
	let deploying = $state(false);

	// Connect Manual Modal
	let connectOpen = $state(false);
	let connNetbirdIP = $state('');
	let connPublicIP = $state('');
	let connAPIKey = $state('');
	let connPDNSPort = $state('8081');
	let connAgentPort = $state('8082');
	let connecting = $state(false);

	const isEdge = $derived(modeStore.mode === 'edge');
	const isVPS = $derived(modeStore.mode === 'vps');
	const isLocal = $derived(modeStore.mode === 'local');

	const selectedSiteObj = $derived(sites.find((s) => s.domain === exposeSiteDomain) ?? null);
	const defaultZoneName = $derived(
		zones.length > 0 ? zones[0].name.replace(/\.$/, '') : 'example.com'
	);

	const exposedSitesCount = $derived(sites.filter((s) => Boolean(s.exposed_to)).length);
	const localOnlySitesCount = $derived(sites.length - exposedSitesCount);
	const vhostExposuresCount = $derived(exposures.filter((e) => Boolean(e.local_site)).length);
	const serviceExposuresCount = $derived(exposures.length - vhostExposuresCount);

	const filteredZones = $derived.by(() => {
		const q = zoneSearch.trim().toLowerCase();
		if (!q) return zones;
		return zones.filter(
			(z) => z.name.toLowerCase().includes(q) || z.kind.toLowerCase().includes(q)
		);
	});

	const filteredSites = $derived.by(() => {
		const q = exposeSearch.trim().toLowerCase();
		return sites.filter((s) => {
			if (exposeFilter === 'exposed' && !s.exposed_to) return false;
			if (exposeFilter === 'local' && s.exposed_to) return false;
			if (!q) return true;
			return (
				s.domain.toLowerCase().includes(q) ||
				(s.root_path || '').toLowerCase().includes(q) ||
				(s.exposed_to || '').toLowerCase().includes(q) ||
				(s.framework || '').toLowerCase().includes(q)
			);
		});
	});

	const filteredExposures = $derived.by(() => {
		const q = proxySearch.trim().toLowerCase();
		return exposures.filter((exp) => {
			if (proxyFilter === 'vhost' && !exp.local_site) return false;
			if (proxyFilter === 'service' && exp.local_site) return false;
			if (!q) return true;
			return (
				exp.domain.toLowerCase().includes(q) ||
				(exp.local_site || '').toLowerCase().includes(q) ||
				(exp.target || '').toLowerCase().includes(q) ||
				(exp.config_file || '').toLowerCase().includes(q)
			);
		});
	});

	// Pagination (Maximal 10 data per page)
	const PAGE_SIZE = 10;
	let zonePage = $state(1);
	let exposePage = $state(1);
	let proxyPage = $state(1);

	const totalZonePages = $derived(Math.max(1, Math.ceil(filteredZones.length / PAGE_SIZE)));
	const paginatedZones = $derived(
		filteredZones.slice((zonePage - 1) * PAGE_SIZE, zonePage * PAGE_SIZE)
	);

	const totalExposePages = $derived(Math.max(1, Math.ceil(filteredSites.length / PAGE_SIZE)));
	const paginatedSites = $derived(
		filteredSites.slice((exposePage - 1) * PAGE_SIZE, exposePage * PAGE_SIZE)
	);

	const totalProxyPages = $derived(Math.max(1, Math.ceil(filteredExposures.length / PAGE_SIZE)));
	const paginatedExposures = $derived(
		filteredExposures.slice((proxyPage - 1) * PAGE_SIZE, proxyPage * PAGE_SIZE)
	);

	$effect(() => {
		const _ = zoneSearch;
		zonePage = 1;
	});

	$effect(() => {
		const _ = `${exposeSearch}:${exposeFilter}`;
		exposePage = 1;
	});

	$effect(() => {
		const _ = `${proxySearch}:${proxyFilter}`;
		proxyPage = 1;
	});

	$effect(() => {
		if (zonePage > totalZonePages) {
			zonePage = totalZonePages;
		}
	});

	$effect(() => {
		if (exposePage > totalExposePages) {
			exposePage = totalExposePages;
		}
	});

	$effect(() => {
		if (proxyPage > totalProxyPages) {
			proxyPage = totalProxyPages;
		}
	});

	async function copyText(val: string, label: string) {
		if (!val || val === '-') return;
		try {
			await navigator.clipboard.writeText(val);
			toast.success(`${label} (${val}) disalin.`);
		} catch {
			toast.error(`Gagal menyalin ${label}.`);
		}
	}

	function handleTabChange(val: string) {
		if (val !== 'zones' && val !== 'expose' && val !== 'proxy') return;
		activeTab = val;
		const target = val === 'zones' ? zoneTabRef : val === 'expose' ? exposeTabRef : proxyTabRef;
		target?.scrollIntoView({
			block: 'nearest',
			inline: 'nearest',
			container: 'nearest'
		} as ScrollIntoViewOptions);
	}

	async function loadAll(showProgress = false) {
		if (showProgress) refreshingAll = true;
		try {
			const st = await api.getPDNSStatus();
			edge = st;
			if (!deployHost && st.vps_netbird_ip) deployHost = st.vps_netbird_ip;
			if (!connNetbirdIP && st.vps_netbird_ip) connNetbirdIP = st.vps_netbird_ip;
			if (!connPublicIP && st.vps_public_ip) connPublicIP = st.vps_public_ip;
			if (!connAPIKey && st.api_key) connAPIKey = st.api_key;
			if (st.pdns_port) connPDNSPort = st.pdns_port;
			if (st.agent_port) connAgentPort = st.agent_port;
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memuat status PowerDNS');
		}

		await Promise.all([
			loadZones(),
			modeStore.mode === 'edge' ? loadExposures() : Promise.resolve((exposures = [])),
			modeStore.mode === 'edge' ? loadLocalSites() : Promise.resolve((sites = []))
		]);

		refreshingAll = false;
		initialLoading = false;
	}

	async function loadZones() {
		zonesError = '';
		try {
			zones = (await api.listPDNSZones()) ?? [];
		} catch (err) {
			zones = [];
			zonesError = err instanceof Error ? err.message : 'Gagal memuat zone DNS';
		}
	}

	async function loadExposures(showProgress = false) {
		if (showProgress) refreshingExposures = true;
		exposuresError = '';
		try {
			exposures = (await api.listPDNSExposures()) ?? [];
		} catch (err) {
			exposures = [];
			exposuresError = err instanceof Error ? err.message : 'Edge VPS tidak dapat dihubungi';
		} finally {
			refreshingExposures = false;
		}
	}

	async function refreshActiveTab() {
		if (activeTab === 'zones') {
			await loadZones();
			return;
		}
		if (activeTab === 'proxy') {
			await loadExposures(true);
			return;
		}
		refreshingExposures = true;
		try {
			await Promise.all([loadLocalSites(), loadExposures(false)]);
		} finally {
			refreshingExposures = false;
		}
	}

	async function startLocalPDNS() {
		startingPDNS = true;
		try {
			await api.controlService('pdns', 'restart');
			toast.success('Layanan PowerDNS dijalankan.');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menjalankan PowerDNS');
		} finally {
			startingPDNS = false;
		}
		await loadAll();
	}

	async function loadLocalSites() {
		try {
			sites = (await api.listSites()) ?? [];
		} catch {
			sites = [];
		}
	}

	onMount(async () => {
		const tabParam = page.url.searchParams.get('tab');
		if (tabParam === 'zones' || tabParam === 'proxy' || tabParam === 'expose') {
			activeTab = tabParam;
		}
		await loadAll();
		const preExpose = page.url.searchParams.get('expose');
		if (preExpose) {
			activeTab = 'expose';
			openExposeModal('site', preExpose);
		}
	});

	$effect(() => {
		if (!isEdge && (activeTab === 'expose' || activeTab === 'proxy')) {
			activeTab = 'zones';
		}
	});

	$effect(() => {
		if (cp.refreshTick > 0) loadAll();
	});

	$effect(() => {
		const currentMode = modeStore.mode;
		if (lastLoadedMode !== null && lastLoadedMode !== currentMode) {
			void loadAll();
		}
		lastLoadedMode = currentMode;
	});

	function suggestPublicDomain(localDomain: string) {
		const found = sites.find((s) => s.domain === localDomain);
		if (found?.exposed_to) {
			exposeSitePublic = found.exposed_to;
			return;
		}
		const cleanPrefix = localDomain.replace(/\.(test|local)$/, '');
		exposeSitePublic = `${cleanPrefix}.${defaultZoneName}`;
	}

	function openExposeModal(mode: 'site' | 'port' = 'site', preselectedDomain = '') {
		exposeMode = mode;
		if (sites.length > 0) {
			const target =
				preselectedDomain && sites.some((s) => s.domain === preselectedDomain)
					? preselectedDomain
					: sites[0].domain;
			exposeSiteDomain = target;
			suggestPublicDomain(target);
		} else {
			exposeSiteDomain = '';
			exposeSitePublic = '';
		}
		exposeSSL = true;
		exposeWS = true;
		exposeOpen = true;
	}

	async function submitExpose(e: SubmitEvent) {
		e.preventDefault();
		let pubDomain = '';
		let targetPort = 0;

		if (exposeMode === 'site') {
			if (!exposeSiteDomain) {
				toast.warning('Pilih vhost lokal yang ingin diekspos.');
				return;
			}
			pubDomain = exposeSitePublic.trim();
			if (!pubDomain) {
				toast.warning('Domain publik VPS wajib diisi.');
				return;
			}
		} else {
			pubDomain = exposePortPublic.trim();
			targetPort = parseInt(exposePortTargetStr, 10) || 0;
			if (!pubDomain) {
				toast.warning('Domain publik VPS wajib diisi.');
				return;
			}
			if (targetPort < 1 || targetPort > 65535) {
				toast.warning('Port target lokal tidak valid (1-65535).');
				return;
			}
		}

		exposeOpen = false;
		const ok = await cp.confirm({
			title: 'Konfirmasi Ekspos VPS',
			description: `Ekspos '${pubDomain}' ke internet via VPS Edge Gateway?`,
			confirmText: 'Ekspos Sekarang'
		});
		if (!ok) {
			exposeOpen = true;
			return;
		}

		exposing = true;
		try {
			await api.exposePDNS({
				mode: exposeMode,
				local_domain: exposeMode === 'site' ? exposeSiteDomain : undefined,
				public_domain: pubDomain,
				target_port: exposeMode === 'port' ? targetPort : undefined,
				use_ssl: exposeSSL,
				websocket: exposeWS
			});
			toast.success(`Domain '${pubDomain}' berhasil diekspos ke internet!`);
			await loadAll();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal ekspos ke VPS');
			exposeOpen = true;
		} finally {
			exposing = false;
		}
	}

	async function unexposeDomain(domain: string) {
		const ok = await cp.confirm({
			title: 'Hapus Eksposur VPS',
			description: `Hapus ekspos domain '${domain}' dari VPS Edge Gateway? Konfigurasi proxy dan record DNS subdomain akan dibersihkan.`,
			confirmText: 'Hapus Ekspos',
			destructive: true
		});
		if (!ok) return;
		unexposingPending = { ...unexposingPending, [domain]: true };
		try {
			await api.unexposePDNS(domain, false);
			toast.success(`Ekspos untuk '${domain}' berhasil dihapus.`);
			await loadAll();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus ekspos');
		} finally {
			const next = { ...unexposingPending };
			delete next[domain];
			unexposingPending = next;
		}
	}

	async function submitAddZone(e: SubmitEvent) {
		e.preventDefault();
		const clean = newZoneName.trim().replace(/\.$/, '').toLowerCase();
		if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(clean)) {
			toast.warning('Nama zone tidak valid. Gunakan FQDN tanpa protokol, contoh: domainanda.com');
			return;
		}
		addingZone = true;
		try {
			const res = await api.addPDNSZone(clean, createDefaultVhost);
			addZoneOpen = false;
			newZoneName = '';
			createDefaultVhost = true;
			if (res.vhost_created) {
				toast.success(
					`Zone DNS ${clean} & vhost static HTML berhasil dibuat (${res.vhost_root || `/var/www/${clean}`})!`
				);
			} else if (res.vhost_status === 'exists') {
				toast.success(`Zone DNS ${clean} ditambahkan (vhost yang sudah ada dipertahankan).`);
			} else {
				toast.success(`Zone DNS ${clean} berhasil ditambahkan!`);
				if (res.vhost_message && (res.vhost_status === 'error' || res.vhost_status === 'skipped')) {
					toast.warning(res.vhost_message);
				}
			}
			if (modeStore.mode === 'edge') {
				await Promise.all([loadZones(), loadLocalSites(), loadExposures()]);
			} else {
				await loadZones();
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menambahkan zone');
		} finally {
			addingZone = false;
		}
	}

	async function runZoneDoctor(zoneName: string) {
		const reqId = ++reportRequestSeq;
		zonePending = { ...zonePending, [zoneName]: 'doctor' };
		reportTitle = `Doctor Zone — ${zoneName}`;
		reportData = null;
		reportError = '';
		reportLoadingMessage = `Memeriksa NS, SOA, glue record, dan resolusi DNS untuk ${zoneName}...`;
		reportLoading = true;
		reportOpen = true;
		try {
			const res = await api.doctorPDNSZone(zoneName);
			if (reqId !== reportRequestSeq) return;
			reportData = res;
		} catch (err) {
			if (reqId !== reportRequestSeq) return;
			const msg = err instanceof Error ? err.message : 'Gagal menjalankan doctor zone';
			reportError = msg;
			toast.error(msg);
		} finally {
			const next = { ...zonePending };
			delete next[zoneName];
			zonePending = next;
			if (reqId === reportRequestSeq) {
				reportLoading = false;
			}
		}
	}

	async function runZoneRepair(zoneName: string) {
		const ok = await cp.confirm({
			title: 'Repair DNS Zone',
			description: `Perbaiki zone ${zoneName}? Tindakan ini menulis ulang SOA/NS, glue ns1/ns2, dan mengaktifkan SOA-EDIT-API. Record lain tidak diubah.`,
			confirmText: 'Repair Zone'
		});
		if (!ok) return;
		const reqId = ++reportRequestSeq;
		zonePending = { ...zonePending, [zoneName]: 'repair' };
		reportTitle = `Repair Zone — ${zoneName}`;
		reportData = null;
		reportError = '';
		reportLoadingMessage = `Memperbaiki SOA/NS, glue ns1/ns2, dan konfigurasi zone ${zoneName}...`;
		reportLoading = true;
		reportOpen = true;
		try {
			const res = await api.repairPDNSZone(zoneName);
			if (reqId !== reportRequestSeq) return;
			reportData = res.report ?? res;
			await loadZones();
		} catch (err) {
			if (reqId !== reportRequestSeq) return;
			const msg = err instanceof Error ? err.message : 'Gagal memperbaiki zone';
			reportError = msg;
			toast.error(msg);
		} finally {
			const next = { ...zonePending };
			delete next[zoneName];
			zonePending = next;
			if (reqId === reportRequestSeq) {
				reportLoading = false;
			}
		}
	}

	async function deleteZone(zoneName: string) {
		const ok = await cp.confirm({
			title: 'Hapus Authoritative Zone',
			description: `Hapus authoritative zone '${zoneName}' beserta seluruh record di dalamnya dari PowerDNS VPS?`,
			confirmText: 'Hapus Zone',
			destructive: true
		});
		if (!ok) return;
		zonePending = { ...zonePending, [zoneName]: 'delete' };
		try {
			await api.deletePDNSZone(zoneName);
			toast.success(`Zone DNS '${zoneName}' berhasil dihapus.`);
			await loadZones();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus zone');
		} finally {
			const next = { ...zonePending };
			delete next[zoneName];
			zonePending = next;
		}
	}

	async function openRecordsModal(zoneName: string) {
		currentZone = zoneName;
		recName = '';
		recContent = '';
		recType = 'A';
		recTTLStr = '300';
		recordsOpen = true;
		await loadRecords(zoneName);
	}

	async function loadRecords(zoneName: string) {
		recordsLoading = true;
		try {
			rrsets = (await api.listPDNSRecords(zoneName)) ?? [];
		} catch (err) {
			rrsets = [];
			toast.error(err instanceof Error ? err.message : 'Gagal memuat record DNS');
		} finally {
			recordsLoading = false;
		}
	}

	async function submitAddRecord(e: SubmitEvent) {
		e.preventDefault();
		if (!recContent.trim()) {
			toast.warning('Konten / nilai target record tidak boleh kosong.');
			return;
		}
		savingRecord = true;
		try {
			await api.addPDNSRecord(
				currentZone,
				recName.trim() || '@',
				recType,
				recContent.trim(),
				parseInt(recTTLStr, 10) || 300
			);
			recName = '';
			recContent = '';
			toast.success('Record DNS disimpan.');
			await loadRecords(currentZone);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menambahkan record');
		} finally {
			savingRecord = false;
		}
	}

	async function deleteRecord(name: string, type: string) {
		recordsOpen = false;
		const ok = await cp.confirm({
			title: 'Hapus Record DNS',
			description: `Hapus record DNS ${type} untuk '${name}'?`,
			confirmText: 'Hapus',
			destructive: true
		});
		recordsOpen = true;
		if (!ok) return;
		const key = `${name}:${type}`;
		recordPending = { ...recordPending, [key]: true };
		try {
			await api.deletePDNSRecord(currentZone, name, type);
			toast.success(`Record ${type} untuk ${name} dihapus.`);
			await loadRecords(currentZone);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus record');
		} finally {
			const next = { ...recordPending };
			delete next[key];
			recordPending = next;
		}
	}

	async function submitDeployVPS(e: SubmitEvent) {
		e.preventDefault();
		if (!deployHost.trim()) {
			toast.warning('Host / IP VPS wajib diisi.');
			return;
		}
		if (deployAuthMethod === 'password' && !deployPassword) {
			toast.warning('Password SSH wajib diisi untuk login ke VPS.');
			return;
		}
		const port = parseInt(deployPortStr, 10) || 22;
		const user = deployUser.trim() || 'root';
		deployOpen = false;
		const ok = await cp.confirm({
			title: 'Konfirmasi Deploy VPS',
			description: `Mulai deployment otomatis ke VPS ${user}@${deployHost.trim()}:${port}? Skrip instalasi PowerDNS, Nginx, dan Edge Gateway akan dijalankan di VPS via SSH.`,
			confirmText: 'Mulai Deploy'
		});
		if (!ok) {
			deployOpen = true;
			return;
		}

		deploying = true;
		try {
			await api.deployVPS({
				host: deployHost.trim(),
				port,
				user,
				auth_method: deployAuthMethod,
				password: deployPassword,
				key_path: deployKeyPath.trim()
			});
			cp.openTerminal('Deploy VPS Edge Gateway');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memulai deploy VPS');
			deployOpen = true;
		} finally {
			deploying = false;
		}
	}

	async function submitConnectVPS(e: SubmitEvent) {
		e.preventDefault();
		if (!connNetbirdIP.trim() || !connAPIKey.trim()) {
			toast.warning('IP NetBird VPS dan PowerDNS API Key wajib diisi.');
			return;
		}
		connecting = true;
		try {
			await api.connectVPS({
				vps_netbird_ip: connNetbirdIP.trim(),
				vps_public_ip: connPublicIP.trim(),
				api_key: connAPIKey.trim(),
				pdns_port: connPDNSPort.trim() || '8081',
				agent_port: connAgentPort.trim() || '8082'
			});
			connectOpen = false;
			toast.success('Berhasil terhubung ke VPS Edge Gateway!');
			await loadAll();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghubungkan ke VPS');
		} finally {
			connecting = false;
		}
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<!-- Page Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div class="min-w-0 max-w-2xl">
			{#if isVPS}
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Domain &amp; DNS Manager</h1>
				<p class="text-sm text-muted-foreground">
					Authoritative DNS server dan manajemen domain langsung di Cloud VPS.
				</p>
			{:else if isLocal}
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Domain &amp; DNS Manager (Lokal)</h1>
				<p class="text-sm text-muted-foreground">
					PowerDNS Authoritative mandiri di 127.0.0.1:8081 &amp; port 53 tanpa NetBird maupun VPS Edge.
				</p>
			{:else}
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">PowerDNS &amp; VPS Edge Gateway</h1>
				<p class="text-sm text-muted-foreground">
					Authoritative DNS mandiri dan reverse-proxy SSL Let's Encrypt otomatis di VPS Edge via NetBird.
				</p>
			{/if}
		</div>

		<div class="flex shrink-0 flex-wrap items-center gap-2">
			{#if !isEdge}
				<StatusBadge
					tone={edge?.reachable ? 'success' : edge?.has_local_pdns ? 'warning' : 'destructive'}
					label={edge?.reachable
						? 'PowerDNS Aktif'
						: edge?.has_local_pdns
							? 'PowerDNS Nonaktif'
							: 'Belum Terpasang'}
				/>
				{#if !edge?.reachable && !edge?.has_local_pdns}
					<Button size="sm" onclick={() => cp.triggerInstall('pdns')}>
						Install PowerDNS
					</Button>
				{/if}
				{#if isVPS}
					<Button
						variant="outline"
						size="sm"
						aria-expanded={glueGuideOpen}
						onclick={() => (glueGuideOpen = !glueGuideOpen)}
					>
						<BookOpenIcon data-icon="inline-start" />
						Panduan Nameserver
					</Button>
				{/if}
				<Button size="sm" variant={edge?.reachable ? 'default' : 'outline'} onclick={() => (addZoneOpen = true)}>
					<PlusIcon data-icon="inline-start" />
					Tambah Zone
				</Button>
			{:else}
				{#if edge}
					<StatusBadge
						tone={edge.reachable ? 'success' : 'destructive'}
						label={edge.reachable ? 'Terhubung ke Edge VPS' : 'Edge VPS Terputus'}
					/>
				{/if}
				<Button variant={edge?.reachable ? 'outline' : 'default'} size="sm" onclick={() => (deployOpen = true)}>
					<ServerIcon data-icon="inline-start" />
					Deploy ke VPS
				</Button>
				<Button variant="outline" size="sm" onclick={() => (connectOpen = true)}>
					<LinkIcon data-icon="inline-start" />
					Hubungkan Manual
				</Button>
			{/if}
		</div>
	</div>

	<!-- Contextual Alerts -->
	{#if isLocal}
		<Alert.Root class="border-muted-foreground/30 bg-muted/40">
			<InfoIcon />
			<Alert.Title>Mode Lokal Standalone Aktif</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					Di mode <strong>Lokal Standalone</strong>, PowerDNS disembunyikan dari menu sidebar karena pengembangan lokal umumnya memakai <code>/etc/hosts</code>. Anda tetap dapat menggunakannya secara lokal atau beralih ke mode VPS / Edge Gateway.
				</span>
				<Button size="xs" variant="outline" class="shrink-0" onclick={() => modeStore.openModal()}>
					Ganti Mode Server
				</Button>
			</Alert.Description>
		</Alert.Root>
	{/if}

	{#if !isEdge && edge && !edge.reachable}
		<Alert.Root variant="destructive">
			<TriangleAlertIcon />
			<Alert.Title>
				{edge.has_local_pdns
					? 'Layanan PowerDNS Lokal Belum Aktif'
					: 'PowerDNS Authoritative Belum Terpasang'}
			</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					{edge.has_local_pdns
						? 'Konfigurasi /etc/powerdns/pdns.conf sudah tersedia tetapi API 127.0.0.1:8081 tidak merespons. Jalankan ulang instalasi atau nyalakan service pdns.'
						: 'PowerDNS lokal di port 53 & port 8081 belum terpasang. Pasang PowerDNS standalone sekarang tanpa NetBird.'}
				</span>
				<div class="flex flex-wrap gap-2">
					{#if edge.has_local_pdns}
						<Button size="xs" disabled={startingPDNS} onclick={startLocalPDNS}>
							{#if startingPDNS}
								<Spinner data-icon="inline-start" />
								Menjalankan...
							{:else}
								Nyalakan PowerDNS
							{/if}
						</Button>
					{/if}
					<Button size="xs" variant={edge.has_local_pdns ? 'outline' : 'default'} onclick={() => cp.triggerInstall('pdns')}>
						{edge.has_local_pdns ? 'Konfigurasi Ulang PowerDNS' : 'Install PowerDNS Sekarang'}
					</Button>
				</div>
			</Alert.Description>
		</Alert.Root>
	{:else if isEdge && edge && !edge.reachable}
		<Alert.Root variant="destructive">
			<TriangleAlertIcon />
			<Alert.Title>VPS Edge Gateway Belum Terhubung</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					Mesin lokal belum terhubung ke PowerDNS &amp; Nginx di VPS. Gunakan Deploy otomatis via SSH atau Hubungkan Manual.
				</span>
				<div class="flex flex-wrap gap-2">
					<Button size="xs" onclick={() => (deployOpen = true)}>Mulai Deploy ke VPS</Button>
					<Button variant="outline" size="xs" onclick={() => (connectOpen = true)}>Hubungkan Manual</Button>
				</div>
			</Alert.Description>
		</Alert.Root>
	{/if}

	{#if isVPS && glueGuideOpen}
		<Alert.Root class="border-primary/40 bg-primary/5">
			<InfoIcon class="text-primary" />
			<Alert.Title class="font-semibold text-primary">Petunjuk Integrasi Domain (Child Nameserver)</Alert.Title>
			<Alert.Description class="mt-2 space-y-2 text-xs leading-relaxed text-muted-foreground">
				<p>
					Agar seluruh domain utama dan subdomain mengarah langsung ke VPS ini:
				</p>
				<ol class="list-inside list-decimal space-y-1.5">
					<li>Buka panel registrar domain Anda (Niagahoster, Rumahweb, Namecheap, Cloudflare, dll).</li>
					<li>Cari menu <strong>Child Nameserver / Host Records / Glue Records</strong> pada domain utama Anda.</li>
					<li>
						Daftarkan pasangan glue record berikut:
						<div class="mt-1.5 flex flex-col gap-1.5 sm:flex-row sm:flex-wrap">
							<span class="inline-flex items-center gap-1.5 rounded-md border bg-background px-2 py-1 font-mono text-foreground">
								ns1.{defaultZoneName} &rarr; {edge?.vps_public_ip || 'IP_PUBLIK_VPS'}
								{#if edge?.vps_public_ip}
									<Button
										variant="ghost"
										size="icon-xs"
										aria-label="Salin ns1"
										onclick={() => copyText(`ns1.${defaultZoneName}`, 'Nameserver 1')}
									>
										<CopyIcon />
									</Button>
								{/if}
							</span>
							<span class="inline-flex items-center gap-1.5 rounded-md border bg-background px-2 py-1 font-mono text-foreground">
								ns2.{defaultZoneName} &rarr; {edge?.vps_public_ip || 'IP_PUBLIK_VPS'}
								{#if edge?.vps_public_ip}
									<Button
										variant="ghost"
										size="icon-xs"
										aria-label="Salin ns2"
										onclick={() => copyText(`ns2.${defaultZoneName}`, 'Nameserver 2')}
									>
										<CopyIcon />
									</Button>
								{/if}
							</span>
						</div>
					</li>
					<li>Ubah <strong>Nameserver (DNS)</strong> domain Anda menjadi <code class="font-mono">ns1.{defaultZoneName}</code> dan <code class="font-mono">ns2.{defaultZoneName}</code>.</li>
					<li>Tambahkan zone domain di tabel di bawah. wdev akan mengelola seluruh record (A, CNAME, MX, TXT) secara otomatis.</li>
				</ol>
			</Alert.Description>
		</Alert.Root>
	{/if}

	<!-- Unified Network & DNS Topology Strip -->
	<Card.Root class="gap-0 py-0">
		<div class="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/30 px-4 py-2.5">
			<div class="flex items-center gap-2">
				<NetworkIcon class="size-4 text-primary" aria-hidden="true" />
				<span class="text-sm font-semibold">Topologi &amp; Endpoint Layanan</span>
				<Badge variant="outline" class="font-normal">
					{isEdge ? 'Edge Gateway (NetBird)' : isVPS ? 'Full VPS Authoritative' : 'Lokal Standalone'}
				</Badge>
			</div>
			<div class="flex flex-wrap items-center gap-3 text-xs tabular-nums text-muted-foreground">
				<span><strong class="font-semibold text-foreground">{zones.length}</strong> Zone DNS</span>
				{#if isEdge}
					<span><strong class="font-semibold text-foreground">{exposedSitesCount}/{sites.length}</strong> VHost Terekspos</span>
					<span><strong class="font-semibold text-foreground">{exposures.length}</strong> Proxy Aktif</span>
				{/if}
			</div>
		</div>

		{#if initialLoading && !edge}
			<div class="grid divide-y md:grid-cols-3 md:divide-x md:divide-y-0">
				{#each [0, 1, 2] as idx (idx)}
					<div class="flex flex-col gap-2 p-4">
						<Skeleton class="h-3.5 w-28" />
						<Skeleton class="h-5 w-40" />
						<Skeleton class="h-3 w-48" />
					</div>
				{/each}
			</div>
		{:else if isVPS}
			<div class="grid divide-y md:grid-cols-3 md:divide-x md:divide-y-0">
				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">IP Publik VPS (Target Record A)</span>
					<div class="flex items-center gap-1.5">
						<span class="truncate font-mono text-base font-semibold tabular-nums">
							{edge?.vps_public_ip || '-'}
						</span>
						{#if edge?.vps_public_ip}
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label="Salin IP Publik VPS"
								onclick={() => copyText(edge?.vps_public_ip || '', 'IP Publik VPS')}
							>
								<CopyIcon />
							</Button>
						{/if}
					</div>
					<span class="text-xs text-muted-foreground">Alamat IP tujuan untuk domain &amp; glue record.</span>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">Authoritative Nameserver</span>
					<div class="flex flex-col gap-1 font-mono text-xs sm:text-sm">
						<div class="flex items-center gap-1.5">
							<span class="truncate font-semibold">ns1.{defaultZoneName}</span>
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label={`Salin ns1.${defaultZoneName}`}
								onclick={() => copyText(`ns1.${defaultZoneName}`, 'NS1')}
							>
								<CopyIcon />
							</Button>
						</div>
						<div class="flex items-center gap-1.5">
							<span class="truncate font-semibold">ns2.{defaultZoneName}</span>
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label={`Salin ns2.${defaultZoneName}`}
								onclick={() => copyText(`ns2.${defaultZoneName}`, 'NS2')}
							>
								<CopyIcon />
							</Button>
						</div>
					</div>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">Endpoint &amp; Port Engine</span>
					<span class="font-mono text-base font-semibold tabular-nums">
						127.0.0.1:{edge?.pdns_port || '8081'}
					</span>
					<span class="text-xs text-muted-foreground">
						Port DNS publik 53 (UDP/TCP) dengan SOA-EDIT-API aktif.
					</span>
				</div>
			</div>
		{:else if isLocal}
			<div class="grid divide-y md:grid-cols-3 md:divide-x md:divide-y-0">
				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">Endpoint PowerDNS API (Lokal)</span>
					<div class="flex items-center gap-1.5">
						<span class="truncate font-mono text-base font-semibold tabular-nums">
							{edge?.vps_netbird_ip || '127.0.0.1'}:{edge?.pdns_port || '8081'}
						</span>
						<Button
							variant="ghost"
							size="icon-xs"
							aria-label="Salin endpoint PowerDNS lokal"
							onclick={() =>
								copyText(
									`${edge?.vps_netbird_ip || '127.0.0.1'}:${edge?.pdns_port || '8081'}`,
									'Endpoint PowerDNS'
								)}
						>
							<CopyIcon />
						</Button>
					</div>
					<span class="text-xs text-muted-foreground">Authoritative DNS port 53 lokal.</span>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">IP Host / LAN (Target Record A)</span>
					<div class="flex items-center gap-1.5">
						<span class="truncate font-mono text-base font-semibold tabular-nums">
							{edge?.vps_public_ip || '127.0.0.1'}
						</span>
						<Button
							variant="ghost"
							size="icon-xs"
							aria-label="Salin IP Target Record A"
							onclick={() => copyText(edge?.vps_public_ip || '127.0.0.1', 'IP Host')}
						>
							<CopyIcon />
						</Button>
					</div>
					<span class="text-xs text-muted-foreground">Target default resolusi record A lokal.</span>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">Mode Operasional DNS</span>
					<span class="text-base font-semibold">Lokal Standalone</span>
					<span class="text-xs text-muted-foreground">Berjalan mandiri tanpa NetBird maupun Edge VPS.</span>
				</div>
			</div>
		{:else}
			<div class="grid divide-y md:grid-cols-3 md:divide-x md:divide-y-0">
				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">IP Publik Edge Gateway</span>
					<div class="flex items-center gap-1.5">
						<span class="truncate font-mono text-base font-semibold tabular-nums">
							{edge?.vps_public_ip || '-'}
						</span>
						{#if edge?.vps_public_ip}
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label="Salin IP Publik Edge Gateway"
								onclick={() => copyText(edge?.vps_public_ip || '', 'IP Publik Edge')}
							>
								<CopyIcon />
							</Button>
						{/if}
					</div>
					<span class="text-xs text-muted-foreground">IP tujuan untuk record DNS A &amp; trafik publik HTTPS.</span>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">Terowongan NetBird Mesh (wt0)</span>
					<div class="flex flex-wrap items-center gap-1.5 font-mono text-sm font-semibold tabular-nums">
						<span>{edge?.local_netbird_ip || 'Lokal wt0'}</span>
						<ArrowLeftRightIcon class="size-3.5 text-info" aria-hidden="true" />
						<span>{edge?.vps_netbird_ip || '-'}</span>
						{#if edge?.vps_netbird_ip}
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label="Salin IP NetBird VPS"
								onclick={() => copyText(edge?.vps_netbird_ip || '', 'IP NetBird VPS')}
							>
								<CopyIcon />
							</Button>
						{/if}
					</div>
					<span class="font-mono text-xs tabular-nums text-muted-foreground">
						PDNS :{edge?.pdns_port || '8081'} · Edge Agent :{edge?.agent_port || '8082'}
					</span>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">Ringkasan Eksposur Edge</span>
					<div class="flex items-baseline gap-2">
						<span class="text-base font-semibold tabular-nums">
							{exposedSitesCount}/{sites.length} VHost
						</span>
						<span class="text-xs text-muted-foreground tabular-nums">
							· {exposures.length} proxy aktif
						</span>
					</div>
					<span class="text-xs text-muted-foreground">
						Otomatisasi record DNS dan sertifikat SSL Let's Encrypt di Edge VPS.
					</span>
				</div>
			</div>
		{/if}
	</Card.Root>

	{#snippet paginationControls(
		currentPage: number,
		totalPages: number,
		totalItems: number,
		onPageChange: (p: number) => void
	)}
		{#if totalItems > 0}
			{@const startItem = (currentPage - 1) * PAGE_SIZE + 1}
			{@const endItem = Math.min(currentPage * PAGE_SIZE, totalItems)}
			<div class="flex flex-col items-center justify-between gap-3 border-t bg-muted/10 px-4 py-2.5 sm:flex-row">
				<div class="text-xs text-muted-foreground">
					Menampilkan <span class="font-medium text-foreground">{startItem}</span>–<span class="font-medium text-foreground">{endItem}</span> dari <span class="font-medium text-foreground">{totalItems}</span> data
				</div>
				{#if totalPages > 1}
					<div class="flex items-center gap-1.5">
						<Button
							variant="outline"
							size="xs"
							disabled={currentPage <= 1}
							onclick={() => onPageChange(1)}
							aria-label="Halaman pertama"
						>
							<ChevronsLeftIcon class="size-3.5" />
						</Button>
						<Button
							variant="outline"
							size="xs"
							disabled={currentPage <= 1}
							onclick={() => onPageChange(currentPage - 1)}
							aria-label="Halaman sebelumnya"
						>
							<ChevronLeftIcon class="size-3.5" />
							<span class="hidden sm:inline">Sebelumnya</span>
						</Button>

						<div class="flex items-center gap-1 px-1.5 text-xs text-muted-foreground">
							<span>Hal.</span>
							<span class="font-semibold text-foreground">{currentPage}</span>
							<span>/</span>
							<span class="font-semibold text-foreground">{totalPages}</span>
						</div>

						<Button
							variant="outline"
							size="xs"
							disabled={currentPage >= totalPages}
							onclick={() => onPageChange(currentPage + 1)}
							aria-label="Halaman berikutnya"
						>
							<span class="hidden sm:inline">Berikutnya</span>
							<ChevronRightIcon class="size-3.5" />
						</Button>
						<Button
							variant="outline"
							size="xs"
							disabled={currentPage >= totalPages}
							onclick={() => onPageChange(totalPages)}
							aria-label="Halaman terakhir"
						>
							<ChevronsRightIcon class="size-3.5" />
						</Button>
					</div>
				{/if}
			</div>
		{/if}
	{/snippet}

	<!-- Pengelolaan DNS & Edge Gateway (Tabbed) -->
	<Card.Root class="gap-0 py-0">
		<Tabs.Root value={activeTab} onValueChange={handleTabChange} class="gap-0">
			<div class="flex flex-col border-b">
				<div class="flex flex-col gap-3 px-4 pt-3.5 pb-2.5 sm:flex-row sm:items-center sm:justify-between">
					<div>
						{#if activeTab === 'zones'}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">Zone Authoritative DNS</h2>
								<Badge variant="secondary" class="tabular-nums">{zones.length}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								Kelola domain authoritative, record DNS (A, CNAME, MX, TXT), serta diagnostik SOA/NS.
							</p>
						{:else if activeTab === 'expose'}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">Status Ekspos Virtual Host</h2>
								<Badge variant="secondary" class="tabular-nums">{exposedSitesCount}/{sites.length}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								Pantau pemetaan domain virtual host lokal dan status ekspos ke internet melalui VPS Edge.
							</p>
						{:else}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">Daftar Proxy VPS Edge</h2>
								<Badge variant="secondary" class="tabular-nums">{exposures.length}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								Kelola reverse-proxy aktif dan konfigurasi SSL yang terpasang di VPS Edge Gateway.
							</p>
						{/if}
					</div>

					<div class="flex items-center gap-2">
						<Button
							variant="outline"
							size="sm"
							disabled={activeTab === 'zones' ? refreshingAll : (refreshingExposures || refreshingAll)}
							onclick={refreshActiveTab}
						>
							{#if (activeTab === 'zones' && refreshingAll) || (activeTab !== 'zones' && refreshingExposures)}
								<Spinner data-icon="inline-start" />
								Menyegarkan...
							{:else}
								<RefreshCwIcon data-icon="inline-start" />
								Segarkan
							{/if}
						</Button>
						{#if activeTab === 'zones'}
							<Button size="sm" onclick={() => (addZoneOpen = true)}>
								<PlusIcon data-icon="inline-start" />
								Tambah Zone
							</Button>
						{:else}
							<Button
								size="sm"
								onclick={() => openExposeModal(sites.length > 0 ? 'site' : 'port')}
							>
								<PlusIcon data-icon="inline-start" />
								Tambah Ekspos
							</Button>
						{/if}
					</div>
				</div>

				<div class="px-4">
					<Tabs.List
						variant="line"
						aria-label="Tab Pengelolaan PowerDNS"
						class="h-11 group-data-horizontal/tabs:h-11 w-full flex-nowrap items-stretch justify-start gap-1.5 overflow-x-auto overflow-y-hidden rounded-none bg-transparent p-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden sm:gap-2"
					>
						<Tabs.Trigger
							value="zones"
							bind:ref={zoneTabRef}
							class="h-full flex-none shrink-0 whitespace-nowrap rounded-none border-0 px-2.5 py-0 text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 focus-visible:ring-0 data-active:text-foreground group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-0 group-data-horizontal/tabs:after:h-0 after:border-b-2 after:border-primary after:bg-transparent sm:px-3"
						>
							<GlobeIcon aria-hidden="true" class="size-3.5" />
							<span>Zone Authoritative</span>
							<span class="rounded-full bg-muted px-1.5 py-0.5 text-[11px] leading-none font-medium tabular-nums text-muted-foreground">
								{zones.length}
							</span>
						</Tabs.Trigger>

						{#if isEdge}
							<Tabs.Trigger
								value="expose"
								bind:ref={exposeTabRef}
								class="h-full flex-none shrink-0 whitespace-nowrap rounded-none border-0 px-2.5 py-0 text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 focus-visible:ring-0 data-active:text-foreground group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-0 group-data-horizontal/tabs:after:h-0 after:border-b-2 after:border-primary after:bg-transparent sm:px-3"
							>
								<ArrowLeftRightIcon aria-hidden="true" class="size-3.5" />
								<span>Status Ekspos</span>
								<span class="rounded-full bg-muted px-1.5 py-0.5 text-[11px] leading-none font-medium tabular-nums text-muted-foreground">
									{exposedSitesCount}/{sites.length}
								</span>
							</Tabs.Trigger>
							<Tabs.Trigger
								value="proxy"
								bind:ref={proxyTabRef}
								class="h-full flex-none shrink-0 whitespace-nowrap rounded-none border-0 px-2.5 py-0 text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 focus-visible:ring-0 data-active:text-foreground group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-0 group-data-horizontal/tabs:after:h-0 after:border-b-2 after:border-primary after:bg-transparent sm:px-3"
							>
								<NetworkIcon aria-hidden="true" class="size-3.5" />
								<span>Daftar Proxy</span>
								<span class="rounded-full bg-muted px-1.5 py-0.5 text-[11px] leading-none font-medium tabular-nums text-muted-foreground">
									{exposures.length}
								</span>
							</Tabs.Trigger>
						{/if}
					</Tabs.List>
				</div>
			</div>

			<!-- Tab 1: Zone Authoritative DNS -->
			<Tabs.Content value="zones" class="mt-0">
				<div>
					<div class="flex flex-col gap-2.5 border-b bg-muted/20 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
						<search class="w-full sm:max-w-xs">
							<InputGroup.Root class="h-8 bg-background">
								<InputGroup.Addon align="inline-start">
									<SearchIcon />
								</InputGroup.Addon>
								<InputGroup.Input
									type="search"
									placeholder="Cari zone..."
									aria-label="Cari zone authoritative DNS"
									bind:value={zoneSearch}
								/>
							</InputGroup.Root>
						</search>

						<div class="flex items-center gap-2">
							<span class="text-xs text-muted-foreground">
								{filteredZones.length} dari {zones.length} zone
							</span>
						</div>
					</div>

		{#if initialLoading && zones.length === 0}
			<div class="flex flex-col gap-2 p-4">
				<Skeleton class="h-9 w-full" />
				<Skeleton class="h-12 w-full" />
				<Skeleton class="h-12 w-full" />
			</div>
		{:else if zonesError}
			<div class="flex flex-col items-center justify-center gap-3 px-4 py-10 text-center">
				<p class="text-sm text-muted-foreground">{zonesError}</p>
				<Button variant="outline" size="sm" onclick={() => loadZones()}>
					<RefreshCwIcon data-icon="inline-start" />
					Coba Lagi
				</Button>
			</div>
		{:else if zones.length === 0}
			<div class="p-4">
				<Empty.Root class="border-0 py-8">
					<Empty.Header>
						<Empty.Media variant="icon">
							<GlobeIcon />
						</Empty.Media>
						<Empty.Title>Belum ada zone DNS</Empty.Title>
						<Empty.Description>
							Tambahkan domain authoritative pertama untuk mengelola record DNS dan subdomain secara otomatis.
						</Empty.Description>
					</Empty.Header>
					<Empty.Content>
						<Button size="sm" onclick={() => (addZoneOpen = true)}>
							<PlusIcon data-icon="inline-start" />
							Tambah Zone Pertama
						</Button>
					</Empty.Content>
				</Empty.Root>
			</div>
		{:else if filteredZones.length === 0}
			<div class="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center">
				<p class="text-sm text-muted-foreground">Tidak ada zone yang cocok dengan "{zoneSearch}".</p>
				<Button variant="outline" size="xs" onclick={() => (zoneSearch = '')}>
					Reset Pencarian
				</Button>
			</div>
		{:else}
			<div class="p-3 sm:p-0">
				<ResponsiveTableWrapper label="Tabel Zone Authoritative DNS" class="rounded-none border-0 bg-transparent">
					<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
						<Table.Header>
							<Table.Row>
								<Table.Head scope="col">Nama Zone</Table.Head>
								<Table.Head scope="col">Tipe</Table.Head>
								<Table.Head scope="col">Serial</Table.Head>
								<Table.Head scope="col">Status</Table.Head>
								<Table.Head scope="col" class="text-right">Aksi</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each paginatedZones as z (z.name)}
								{@const zPending = zonePending[z.name]}
								<Table.Row>
									<Table.Cell data-label="Zone">
										<button
											type="button"
											class="text-left font-semibold text-foreground hover:text-primary hover:underline focus-visible:rounded-sm focus-visible:outline-2 focus-visible:outline-ring"
											onclick={() => openRecordsModal(z.name)}
										>
											{z.name}
										</button>
									</Table.Cell>
									<Table.Cell data-label="Tipe">
										<Badge variant="secondary">{z.kind}</Badge>
									</Table.Cell>
									<Table.Cell data-label="Serial" class="font-mono text-xs tabular-nums text-muted-foreground">
										{z.serial}
									</Table.Cell>
									<Table.Cell data-label="Status">
										<StatusBadge tone="success" label="Aktif" />
									</Table.Cell>
									<Table.Cell data-label="Aksi" class="text-right">
										<div class="flex items-center justify-end gap-1.5">
											<Button
												variant="outline"
												size="xs"
												disabled={Boolean(zPending)}
												onclick={() => openRecordsModal(z.name)}
											>
												<ListIcon data-icon="inline-start" />
												Record<span class="sr-only"> {z.name}</span>
											</Button>
											<DropdownMenu.Root>
												<DropdownMenu.Trigger>
													{#snippet child({ props })}
														<Button variant="outline" size="xs" disabled={Boolean(zPending)} {...props}>
															{#if zPending === 'doctor'}
																<Spinner data-icon="inline-start" />
																Memeriksa...
															{:else if zPending === 'repair'}
																<Spinner data-icon="inline-start" />
																Memperbaiki...
															{:else if zPending === 'delete'}
																<Spinner data-icon="inline-start" />
																Menghapus...
															{:else}
																Aksi<span class="sr-only"> untuk {z.name}</span>
																<ChevronDownIcon data-icon="inline-end" />
															{/if}
														</Button>
													{/snippet}
												</DropdownMenu.Trigger>
												<DropdownMenu.Content align="end" class="w-48">
													<DropdownMenu.Group>
														<DropdownMenu.Label class="truncate text-xs text-muted-foreground">
															{z.name}
														</DropdownMenu.Label>
														<DropdownMenu.Separator />
														<DropdownMenu.Item disabled={Boolean(zPending)} onclick={() => openRecordsModal(z.name)}>
															<ListIcon />
															<span>Kelola Record</span>
														</DropdownMenu.Item>
														<DropdownMenu.Item disabled={Boolean(zPending)} onclick={() => runZoneDoctor(z.name)}>
															<StethoscopeIcon />
															<span>Doctor Zone</span>
														</DropdownMenu.Item>
														<DropdownMenu.Item disabled={Boolean(zPending)} onclick={() => runZoneRepair(z.name)}>
															<WrenchIcon />
															<span>Repair Zone</span>
														</DropdownMenu.Item>
														<DropdownMenu.Separator />
														<DropdownMenu.Item
															variant="destructive"
															disabled={Boolean(zPending)}
															onclick={() => deleteZone(z.name)}
														>
															<Trash2Icon />
															<span>Hapus Zone</span>
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
			</div>
			{@render paginationControls(zonePage, totalZonePages, filteredZones.length, (p) => (zonePage = p))}
		{/if}
				</div>
			</Tabs.Content>

			{#if isEdge}
				<!-- Tab 2: Status Ekspos Virtual Host Lokal -->
				<Tabs.Content value="expose" class="mt-0">
					<div>
						<div class="flex flex-col gap-2.5 border-b bg-muted/20 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
							<search class="w-full sm:max-w-xs">
								<InputGroup.Root class="h-8 bg-background">
									<InputGroup.Addon align="inline-start">
										<SearchIcon />
									</InputGroup.Addon>
									<InputGroup.Input
										type="search"
										placeholder="Cari vhost lokal atau domain publik..."
										aria-label="Cari status ekspos virtual host"
										bind:value={exposeSearch}
									/>
								</InputGroup.Root>
							</search>

							<div class="flex flex-wrap items-center gap-1.5" role="group" aria-label="Filter status ekspos">
								<Button
									variant={exposeFilter === 'all' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={exposeFilter === 'all'}
									onclick={() => (exposeFilter = 'all')}
								>
									Semua ({sites.length})
								</Button>
								<Button
									variant={exposeFilter === 'exposed' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={exposeFilter === 'exposed'}
									onclick={() => (exposeFilter = 'exposed')}
								>
									Terekspos ({exposedSitesCount})
								</Button>
								<Button
									variant={exposeFilter === 'local' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={exposeFilter === 'local'}
									onclick={() => (exposeFilter = 'local')}
								>
									Hanya Lokal ({localOnlySitesCount})
								</Button>
							</div>
						</div>

						{#if initialLoading && sites.length === 0}
							<div class="flex flex-col gap-2 p-4">
								<Skeleton class="h-9 w-full" />
								<Skeleton class="h-12 w-full" />
								<Skeleton class="h-12 w-full" />
							</div>
						{:else if sites.length === 0}
							<div class="p-4">
								<Empty.Root class="border-0 py-8">
									<Empty.Header>
										<Empty.Media variant="icon">
											<GlobeIcon />
										</Empty.Media>
										<Empty.Title>Belum ada virtual host lokal</Empty.Title>
										<Empty.Description>
											Buat virtual host di menu Web Stack terlebih dahulu, atau langsung ekspos port aplikasi lokal ke VPS Edge.
										</Empty.Description>
									</Empty.Header>
									<Empty.Content class="flex flex-wrap justify-center gap-2">
										<Button variant="outline" size="sm" href="/webstack">
											Buka Web Stack
										</Button>
										<Button size="sm" onclick={() => openExposeModal('port')}>
											<CloudUploadIcon data-icon="inline-start" />
											Ekspos Port Aplikasi
										</Button>
									</Empty.Content>
								</Empty.Root>
							</div>
						{:else if filteredSites.length === 0}
							<div class="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center">
								<p class="text-sm text-muted-foreground">
									Tidak ada virtual host yang sesuai dengan filter atau pencarian saat ini.
								</p>
								<Button
									variant="outline"
									size="xs"
									onclick={() => {
										exposeSearch = '';
										exposeFilter = 'all';
									}}
								>
									Reset Filter
								</Button>
							</div>
						{:else}
							<div class="p-3 sm:p-0">
								<ResponsiveTableWrapper
									label="Tabel Status Ekspos Virtual Host Lokal"
									class="rounded-none border-0 bg-transparent"
								>
									<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
										<Table.Header>
											<Table.Row>
												<Table.Head scope="col">Virtual Host Lokal</Table.Head>
												<Table.Head scope="col">Framework</Table.Head>
												<Table.Head scope="col">Status Ekspos</Table.Head>
												<Table.Head scope="col">Domain Publik di VPS</Table.Head>
												<Table.Head scope="col">Target NetBird</Table.Head>
												<Table.Head scope="col" class="text-right">Aksi Cepat</Table.Head>
											</Table.Row>
										</Table.Header>
										<Table.Body>
											{#each paginatedSites as s (s.domain)}
												{@const targetNB = edge?.local_netbird_ip
													? `http://${edge.local_netbird_ip}:80`
													: 'Port 80 (wt0)'}
												<Table.Row>
													<Table.Cell data-label="VHost">
														<div class="font-semibold">{s.domain}</div>
														<div class="break-all font-mono text-xs text-muted-foreground sm:max-w-44 sm:truncate">
															{s.root_path || '-'}
														</div>
													</Table.Cell>
													<Table.Cell data-label="Framework">
														<Badge variant="secondary" class="uppercase">{s.framework || 'PHP'}</Badge>
														<span class="ml-1 text-xs text-muted-foreground tabular-nums">
															{s.framework === 'static'
																? 'Static HTML'
																: s.framework === 'proxy'
																	? `:${s.proxy_port}`
																	: `PHP ${s.php_version || '8.4'}`}
														</span>
													</Table.Cell>
													<Table.Cell data-label="Status">
														{#if s.exposed_to}
															<StatusBadge tone="success" label="Terekspos" />
														{:else}
															<StatusBadge tone="neutral" label="Hanya Lokal" />
														{/if}
													</Table.Cell>
													<Table.Cell data-label="Domain Publik">
														{#if s.exposed_to}
															<div class="flex items-center gap-1">
																<a
																	href={`https://${s.exposed_to}`}
																	target="_blank"
																	rel="noopener noreferrer"
																	class="inline-flex items-center gap-1 break-all font-medium text-primary hover:underline"
																>
																	<span>https://{s.exposed_to}</span>
																	<ExternalLinkIcon class="size-3.5 shrink-0" />
																</a>
																<Button
																	variant="ghost"
																	size="icon-xs"
																	aria-label={`Salin URL https://${s.exposed_to}`}
																	onclick={() => copyText(`https://${s.exposed_to}`, 'URL Publik')}
																>
																	<CopyIcon />
																</Button>
															</div>
														{:else}
															<span class="text-xs text-muted-foreground">Belum diekspos</span>
														{/if}
													</Table.Cell>
													<Table.Cell data-label="Target NetBird" class="font-mono text-xs tabular-nums">
														{targetNB}
													</Table.Cell>
													<Table.Cell data-label="Aksi" class="text-right">
														<div class="flex items-center justify-end gap-1.5">
															{#if !s.exposed_to}
																<Button
																	variant="outline"
																	size="xs"
																	onclick={() => openExposeModal('site', s.domain)}
																>
																	<CloudUploadIcon data-icon="inline-start" />
																	Ekspos<span class="sr-only"> {s.domain}</span>
																</Button>
															{:else}
																<DropdownMenu.Root>
																	<DropdownMenu.Trigger>
																		{#snippet child({ props })}
																			<Button
																				variant="outline"
																				size="xs"
																				disabled={Boolean(s.exposed_to && unexposingPending[s.exposed_to])}
																				{...props}
																			>
																				{#if s.exposed_to && unexposingPending[s.exposed_to]}
																					<Spinner data-icon="inline-start" />
																					Memutus...
																				{:else}
																					Aksi<span class="sr-only"> untuk {s.domain}</span>
																					<ChevronDownIcon data-icon="inline-end" />
																				{/if}
																			</Button>
																		{/snippet}
																	</DropdownMenu.Trigger>
																	<DropdownMenu.Content align="end" class="w-48">
																		<DropdownMenu.Group>
																			<DropdownMenu.Label class="truncate text-xs text-muted-foreground">
																				{s.domain}
																			</DropdownMenu.Label>
																			<DropdownMenu.Separator />
																			<DropdownMenu.Item>
																				{#snippet child({ props })}
																					<a href={`https://${s.exposed_to}`} target="_blank" rel="noopener noreferrer" {...props}>
																						<ExternalLinkIcon />
																						<span>Buka Domain</span>
																					</a>
																				{/snippet}
																			</DropdownMenu.Item>
																			<DropdownMenu.Item onclick={() => openExposeModal('site', s.domain)}>
																				<CloudUploadIcon />
																				<span>Perbarui Ekspos</span>
																			</DropdownMenu.Item>
																			<DropdownMenu.Separator />
																			<DropdownMenu.Item
																				variant="destructive"
																				disabled={Boolean(unexposingPending[s.exposed_to])}
																				onclick={() => unexposeDomain(s.exposed_to)}
																			>
																				<UnplugIcon />
																				<span>Putus Ekspos</span>
																			</DropdownMenu.Item>
																		</DropdownMenu.Group>
																	</DropdownMenu.Content>
																</DropdownMenu.Root>
															{/if}
														</div>
													</Table.Cell>
												</Table.Row>
											{/each}
										</Table.Body>
									</Table.Root>
								</ResponsiveTableWrapper>
							</div>
							{@render paginationControls(exposePage, totalExposePages, filteredSites.length, (p) => (exposePage = p))}
						{/if}
					</div>
				</Tabs.Content>

				<!-- Tab 2: Daftar Proxy Domain VPS -->
				<Tabs.Content value="proxy" class="mt-0">
					<div>
						<div class="flex flex-col gap-2.5 border-b bg-muted/20 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
							<search class="w-full sm:max-w-xs">
								<InputGroup.Root class="h-8 bg-background">
									<InputGroup.Addon align="inline-start">
										<SearchIcon />
									</InputGroup.Addon>
									<InputGroup.Input
										type="search"
										placeholder="Cari domain proxy, target, atau file..."
										aria-label="Cari daftar proxy VPS"
										bind:value={proxySearch}
									/>
								</InputGroup.Root>
							</search>

							<div class="flex flex-wrap items-center gap-1.5" role="group" aria-label="Filter tipe proxy">
								<Button
									variant={proxyFilter === 'all' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={proxyFilter === 'all'}
									onclick={() => (proxyFilter = 'all')}
								>
									Semua ({exposures.length})
								</Button>
								<Button
									variant={proxyFilter === 'vhost' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={proxyFilter === 'vhost'}
									onclick={() => (proxyFilter = 'vhost')}
								>
									VHost ({vhostExposuresCount})
								</Button>
								<Button
									variant={proxyFilter === 'service' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={proxyFilter === 'service'}
									onclick={() => (proxyFilter = 'service')}
								>
									Service ({serviceExposuresCount})
								</Button>
							</div>
						</div>

						{#if initialLoading && exposures.length === 0}
							<div class="flex flex-col gap-2 p-4">
								<Skeleton class="h-9 w-full" />
								<Skeleton class="h-12 w-full" />
								<Skeleton class="h-12 w-full" />
							</div>
						{:else if exposuresError}
							<div class="flex flex-col items-center justify-center gap-3 px-4 py-10 text-center">
								<p class="text-sm text-muted-foreground">{exposuresError}</p>
								<Button variant="outline" size="sm" onclick={() => loadExposures(true)}>
									<RefreshCwIcon data-icon="inline-start" />
									Coba Hubungkan Ulang
								</Button>
							</div>
						{:else if exposures.length === 0}
							<div class="p-4">
								<Empty.Root class="border-0 py-8">
									<Empty.Header>
										<Empty.Media variant="icon">
											<NetworkIcon />
										</Empty.Media>
										<Empty.Title>Belum ada proxy aktif di VPS Edge</Empty.Title>
										<Empty.Description>
											Ekspos virtual host lokal atau port service ke domain publik agar reverse-proxy dan SSL Let's Encrypt otomatis disiapkan di VPS.
										</Empty.Description>
									</Empty.Header>
									<Empty.Content>
										<Button size="sm" onclick={() => openExposeModal(sites.length > 0 ? 'site' : 'port')}>
											<PlusIcon data-icon="inline-start" />
											Tambah Ekspos Pertama
										</Button>
									</Empty.Content>
								</Empty.Root>
							</div>
						{:else if filteredExposures.length === 0}
							<div class="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center">
								<p class="text-sm text-muted-foreground">
									Tidak ada proxy yang sesuai dengan filter atau pencarian saat ini.
								</p>
								<Button
									variant="outline"
									size="xs"
									onclick={() => {
										proxySearch = '';
										proxyFilter = 'all';
									}}
								>
									Reset Filter
								</Button>
							</div>
						{:else}
							<div class="p-3 sm:p-0">
								<ResponsiveTableWrapper
									label="Tabel Proxy Domain VPS"
									class="rounded-none border-0 bg-transparent"
								>
									<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
										<Table.Header>
											<Table.Row>
												<Table.Head scope="col">Domain Publik VPS</Table.Head>
												<Table.Head scope="col">Tipe &amp; Rute Target</Table.Head>
												<Table.Head scope="col">Status Proxy</Table.Head>
												<Table.Head scope="col">File Konfigurasi</Table.Head>
												<Table.Head scope="col" class="text-right">Aksi</Table.Head>
											</Table.Row>
										</Table.Header>
										<Table.Body>
											{#each paginatedExposures as exp (exp.domain)}
												<Table.Row>
													<Table.Cell data-label="Domain">
														<div class="flex items-center gap-1">
															<a
																href={`https://${exp.domain}`}
																target="_blank"
																rel="noopener noreferrer"
																class="inline-flex items-center gap-1 break-all font-semibold text-primary hover:underline"
															>
																<span>{exp.domain}</span>
																<ExternalLinkIcon class="size-3.5 shrink-0" />
															</a>
															<Button
																variant="ghost"
																size="icon-xs"
																aria-label={`Salin URL https://${exp.domain}`}
																onclick={() => copyText(`https://${exp.domain}`, 'URL Proxy')}
															>
																<CopyIcon />
															</Button>
														</div>
													</Table.Cell>
													<Table.Cell data-label="Target">
														{#if exp.local_site}
															<Badge variant="secondary" class="mr-1.5">VHost</Badge>
															<span class="font-medium">{exp.local_site}</span>
														{:else}
															<Badge variant="outline" class="mr-1.5">Service</Badge>
															<span class="font-mono text-xs">{exp.target || 'Proxy VPS'}</span>
														{/if}
													</Table.Cell>
													<Table.Cell data-label="Status">
														<StatusBadge
															tone={exp.enabled ? 'success' : 'neutral'}
															label={exp.enabled ? 'Aktif (HTTPS)' : 'Nonaktif'}
														/>
													</Table.Cell>
													<Table.Cell data-label="Konfigurasi" class="sm:max-w-52">
														<code class="block break-all text-xs text-muted-foreground sm:truncate" title={exp.config_file || '-'}>
															{exp.config_file || '-'}
														</code>
													</Table.Cell>
													<Table.Cell data-label="Aksi" class="text-right">
														<div class="flex justify-end">
															<DropdownMenu.Root>
																<DropdownMenu.Trigger>
																	{#snippet child({ props })}
																		<Button
																			variant="outline"
																			size="xs"
																			disabled={Boolean(unexposingPending[exp.domain])}
																			{...props}
																		>
																			{#if unexposingPending[exp.domain]}
																				<Spinner data-icon="inline-start" />
																				Menghapus...
																			{:else}
																				Aksi<span class="sr-only"> untuk {exp.domain}</span>
																				<ChevronDownIcon data-icon="inline-end" />
																			{/if}
																		</Button>
																	{/snippet}
																</DropdownMenu.Trigger>
																<DropdownMenu.Content align="end" class="w-48">
																	<DropdownMenu.Group>
																		<DropdownMenu.Label class="truncate text-xs text-muted-foreground">
																			{exp.domain}
																		</DropdownMenu.Label>
																		<DropdownMenu.Separator />
																		<DropdownMenu.Item>
																			{#snippet child({ props })}
																				<a href={`https://${exp.domain}`} target="_blank" rel="noopener noreferrer" {...props}>
																					<ExternalLinkIcon />
																					<span>Buka Domain</span>
																				</a>
																			{/snippet}
																		</DropdownMenu.Item>
																		<DropdownMenu.Item onclick={() => copyText(`https://${exp.domain}`, 'URL Proxy')}>
																			<CopyIcon />
																			<span>Salin URL Publik</span>
																		</DropdownMenu.Item>
																		<DropdownMenu.Separator />
																		<DropdownMenu.Item
																			variant="destructive"
																			disabled={Boolean(unexposingPending[exp.domain])}
																			onclick={() => unexposeDomain(exp.domain)}
																		>
																			<Trash2Icon />
																			<span>Hapus Ekspos</span>
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
							</div>
							{@render paginationControls(proxyPage, totalProxyPages, filteredExposures.length, (p) => (proxyPage = p))}
						{/if}
					</div>
				</Tabs.Content>
			{/if}
		</Tabs.Root>
	</Card.Root>
</div>

<!-- Add Zone Dialog -->
<Dialog.Root bind:open={addZoneOpen}>
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-md">
		<Dialog.Header class="pr-8">
			<Dialog.Title>Tambah Authoritative DNS Zone</Dialog.Title>
			<Dialog.Description>
				Masukkan nama domain FQDN tanpa protokol (contoh: domainanda.com). Record DNS (@, www, ns1, ns2) akan dibuat otomatis.
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={submitAddZone} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="pdns-new-zone">Nama Zone Domain</Field.FieldLabel>
					<Input
						id="pdns-new-zone"
						placeholder="domainanda.com"
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						bind:value={newZoneName}
						required
					/>
				</Field.Field>
				<Field.Field orientation="horizontal">
					<Checkbox id="pdns-create-vhost" bind:checked={createDefaultVhost} />
					<Field.FieldLabel for="pdns-create-vhost" class="font-normal leading-snug">
						Buatkan virtual host Nginx &amp; template HTML statis default agar langsung dapat diakses
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (addZoneOpen = false)}>Batal</Button>
				<Button type="submit" disabled={addingZone}>
					{#if addingZone}
						<Spinner data-icon="inline-start" />
						Menambahkan...
					{:else}
						Tambah Zone
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<!-- Zone Doctor / Repair Report Dialog -->
<Dialog.Root bind:open={reportOpen}>
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header class="pr-8">
			<Dialog.Title>{reportTitle}</Dialog.Title>
			{#if reportData?.nameservers && reportData.nameservers.length > 0}
				<Dialog.Description>
					Nameservers: {reportData.nameservers.join(', ')}
				</Dialog.Description>
			{/if}
		</Dialog.Header>
		{#if reportLoading}
			<div class="flex flex-col gap-3 py-1" aria-busy="true">
				<div class="flex items-center gap-2 text-xs text-muted-foreground">
					<Spinner />
					<span>{reportLoadingMessage}</span>
				</div>
				<div
					role="progressbar"
					aria-label="Memeriksa kesehatan zone DNS"
					aria-valuetext={reportLoadingMessage}
					class="h-1.5 w-full overflow-hidden rounded-full bg-muted"
				>
					<div class="h-full w-2/3 animate-pulse rounded-full bg-primary"></div>
				</div>
				<div class="flex flex-col gap-2">
					{#each [0, 1, 2] as idx (idx)}
						<div class="flex items-center justify-between gap-3 rounded-md border p-2.5">
							<div class="flex flex-1 flex-col gap-1.5">
								<Skeleton class="h-3.5 w-28" />
								<Skeleton class="h-3 w-full max-w-xs" />
							</div>
							<Skeleton class="h-5 w-12 rounded-full" />
						</div>
					{/each}
				</div>
			</div>
		{:else if reportError}
			<Alert.Root variant="destructive">
				<Alert.Description>{reportError}</Alert.Description>
			</Alert.Root>
		{:else if reportData}
			<div class="flex max-h-[55dvh] flex-col gap-2 overflow-y-auto overscroll-contain">
				{#each reportData.checks || [] as chk, idx (idx)}
					<div class="flex items-start justify-between gap-3 rounded-md border p-2.5 text-xs">
						<div class="flex flex-col gap-0.5">
							<span class="font-semibold">{chk.name}</span>
							<span class="text-muted-foreground">{chk.detail}</span>
						</div>
						<StatusBadge
							tone={chk.status === 'ok' ? 'success' : chk.status === 'warn' ? 'warning' : 'destructive'}
							label={chk.status.toUpperCase()}
						/>
					</div>
				{/each}
			</div>
		{/if}
		<Dialog.Footer>
			<Button type="button" onclick={() => (reportOpen = false)}>Tutup</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Manage DNS Records Dialog -->
<Dialog.Root bind:open={recordsOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-4xl">
		<Dialog.Header class="pr-8">
			<Dialog.Title>Record DNS — {currentZone}</Dialog.Title>
			<Dialog.Description>Tambah atau hapus record DNS authoritative pada zone ini.</Dialog.Description>
		</Dialog.Header>

		<div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain pr-1">
			<form onsubmit={submitAddRecord} class="rounded-lg border p-3.5">
				<Field.FieldGroup class="grid gap-3 sm:grid-cols-2 lg:grid-cols-[minmax(140px,1fr)_100px_minmax(220px,1.8fr)_85px_auto]">
					<Field.Field>
						<Field.FieldLabel for="pdns-rec-name">Nama / Host</Field.FieldLabel>
						<Input id="pdns-rec-name" placeholder="@ atau sub" bind:value={recName} />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="pdns-rec-type">Tipe</Field.FieldLabel>
						<Select.Root type="single" bind:value={recType}>
							<Select.Trigger id="pdns-rec-type" class="w-full">{recType}</Select.Trigger>
							<Select.Content>
								<Select.Group>
									{#each ['A', 'CNAME', 'TXT', 'MX', 'AAAA'] as t (t)}
										<Select.Item value={t}>{t}</Select.Item>
									{/each}
								</Select.Group>
							</Select.Content>
						</Select.Root>
					</Field.Field>
					<Field.Field class="sm:col-span-2 lg:col-span-1">
						<Field.FieldLabel for="pdns-rec-content">Konten / Target</Field.FieldLabel>
						<Input id="pdns-rec-content" placeholder="1.2.3.4 atau target.com." bind:value={recContent} required />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="pdns-rec-ttl">TTL</Field.FieldLabel>
						<Input id="pdns-rec-ttl" inputmode="numeric" pattern="[0-9]+" bind:value={recTTLStr} />
					</Field.Field>
					<div class="flex items-end">
						<Button type="submit" disabled={savingRecord} class="w-full">
							{#if savingRecord}
								<Spinner data-icon="inline-start" />
							{:else}
								<PlusIcon data-icon="inline-start" />
							{/if}
							Simpan
						</Button>
					</div>
				</Field.FieldGroup>
			</form>

			<ResponsiveTableWrapper label="Tabel Record DNS PowerDNS">
				<Table.Root class="sm:table-fixed">
					<Table.Header>
						<Table.Row>
							<Table.Head scope="col" class="sm:w-48 lg:w-52">Nama Record</Table.Head>
							<Table.Head scope="col" class="sm:w-20">Tipe</Table.Head>
							<Table.Head scope="col" class="sm:w-16">TTL</Table.Head>
							<Table.Head scope="col">Konten</Table.Head>
							<Table.Head scope="col" class="text-right sm:w-32">Aksi</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#if recordsLoading}
							<Table.Row>
								<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
									<div class="inline-flex items-center justify-center gap-2">
										<Spinner />
										<span>Memuat record DNS...</span>
									</div>
								</Table.Cell>
							</Table.Row>
						{:else if rrsets.length === 0}
							<Table.Row>
								<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
									Belum ada record pada zone ini.
								</Table.Cell>
							</Table.Row>
						{:else}
							{#each rrsets as rr (rr.name + ':' + rr.type)}
								{@const isProtected = rr.type === 'SOA' || rr.type === 'NS'}
								{@const cleanName = rr.name.replace(/\.$/, '')}
								{@const isEdgeProxy = exposures.some((p) => p.domain === cleanName)}
								{#each rr.records || [] as rec, idx (idx)}
									<Table.Row>
										<Table.Cell data-label="Nama" class="whitespace-normal wrap-anywhere font-semibold">
											<div class="flex flex-wrap items-center gap-1.5">
												<span>{rr.name}</span>
												{#if isEdgeProxy}
													<Badge variant="secondary" class="text-[10px]">Edge Proxy</Badge>
												{/if}
											</div>
										</Table.Cell>
										<Table.Cell data-label="Tipe">
											<Badge variant="outline">{rr.type}</Badge>
										</Table.Cell>
										<Table.Cell data-label="TTL" class="tabular-nums text-muted-foreground">
											{rr.ttl}
										</Table.Cell>
										<Table.Cell
											data-label="Konten"
											class="whitespace-normal break-normal wrap-anywhere font-mono text-xs leading-relaxed"
										>
											{rec.content}
										</Table.Cell>
										<Table.Cell data-label="Aksi" class="text-right whitespace-nowrap">
											{#if isProtected}
												<Badge variant="secondary" class="font-normal text-muted-foreground">Sistem</Badge>
											{:else}
												<div class="flex justify-end">
													<DropdownMenu.Root>
														<DropdownMenu.Trigger>
															{#snippet child({ props })}
																<Button
																	variant="outline"
																	size="xs"
																	disabled={Boolean(recordPending[`${rr.name}:${rr.type}`])}
																	{...props}
																>
																	{#if recordPending[`${rr.name}:${rr.type}`]}
																		<Spinner data-icon="inline-start" />
																		Menghapus...
																	{:else}
																		Aksi<span class="sr-only"> {rr.type} {rr.name}</span>
																		<ChevronDownIcon data-icon="inline-end" />
																	{/if}
																</Button>
															{/snippet}
														</DropdownMenu.Trigger>
														<DropdownMenu.Content align="end" class="w-44">
															<DropdownMenu.Group>
																<DropdownMenu.Item
																	variant="destructive"
																	disabled={Boolean(recordPending[`${rr.name}:${rr.type}`])}
																	onclick={() => deleteRecord(rr.name, rr.type)}
																>
																	<Trash2Icon />
																	<span>Hapus Record</span>
																</DropdownMenu.Item>
															</DropdownMenu.Group>
														</DropdownMenu.Content>
													</DropdownMenu.Root>
												</div>
											{/if}
										</Table.Cell>
									</Table.Row>
								{/each}
							{/each}
						{/if}
					</Table.Body>
				</Table.Root>
			</ResponsiveTableWrapper>
		</div>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (recordsOpen = false)}>Tutup</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Expose to VPS Modal -->
<Dialog.Root bind:open={exposeOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-lg">
		<Dialog.Header class="pr-8">
			<Dialog.Title>Ekspos ke VPS Edge Gateway</Dialog.Title>
			<Dialog.Description>
				Ekspos vhost lokal atau port aplikasi lokal ke internet melalui VPS Edge Gateway dengan otomatisasi DNS &amp; SSL Let's Encrypt.
			</Dialog.Description>
		</Dialog.Header>

		<form id="pdns-expose-form" onsubmit={submitExpose} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain pr-1">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldTitle id="pdns-expose-mode-label">Mode Ekspos</Field.FieldTitle>
					<ToggleGroup.Root
						type="single"
						bind:value={exposeMode}
						variant="outline"
						spacing={2}
						class="flex w-full flex-wrap"
						aria-labelledby="pdns-expose-mode-label"
						onValueChange={(v) => {
							if (v === 'site' || v === 'port') exposeMode = v;
						}}
					>
						<ToggleGroup.Item value="site" class="min-w-[8rem] flex-1">Virtual Host (VHost)</ToggleGroup.Item>
						<ToggleGroup.Item value="port" class="min-w-[8rem] flex-1">Port Aplikasi</ToggleGroup.Item>
					</ToggleGroup.Root>
				</Field.Field>
			</Field.FieldGroup>

			{#if exposeMode === 'site'}
				<Field.FieldGroup>
					<Field.Field>
						<Field.FieldLabel for="pdns-expose-site-select">Pilih Virtual Host Lokal</Field.FieldLabel>
						<Select.Root
							type="single"
							value={exposeSiteDomain}
							onValueChange={(v) => {
								if (v) {
									exposeSiteDomain = v;
									suggestPublicDomain(v);
								}
							}}
						>
							<Select.Trigger id="pdns-expose-site-select" class="w-full">
								{exposeSiteDomain || '(Belum ada vhost lokal dibuat)'}
							</Select.Trigger>
							<Select.Content>
								<Select.Group>
									{#each sites as s (s.domain)}
										<Select.Item value={s.domain}>
											{s.domain} {s.exposed_to ? `(Sudah -> ${s.exposed_to})` : '(Belum diekspos)'}
										</Select.Item>
									{/each}
								</Select.Group>
							</Select.Content>
						</Select.Root>
					</Field.Field>

					<Field.Field>
						<Field.FieldLabel for="pdns-expose-site-pub">Domain Publik VPS</Field.FieldLabel>
						<Input
							id="pdns-expose-site-pub"
							placeholder={`portal.${defaultZoneName}`}
							bind:value={exposeSitePublic}
							required
						/>
					</Field.Field>

					{#if selectedSiteObj}
						<Alert.Root>
							<InfoIcon />
							<Alert.Title>
								{selectedSiteObj.exposed_to ? 'Sudah Diekspos' : 'Rekomendasi Ekspos'}
							</Alert.Title>
							<Alert.Description>
								{#if selectedSiteObj.exposed_to}
									Virtual host <strong>{selectedSiteObj.domain}</strong> sudah diekspos ke <strong>https://{selectedSiteObj.exposed_to}</strong>. Mengekspos ulang akan memperbarui konfigurasi Nginx dan SSL.
								{:else}
									Rekomendasi domain publik: <code>{exposeSitePublic}</code>.
								{/if}
							</Alert.Description>
						</Alert.Root>
					{/if}
				</Field.FieldGroup>
			{:else}
				<Field.FieldGroup>
					<Field.Field>
						<Field.FieldLabel for="pdns-expose-port-pub">Domain Publik VPS</Field.FieldLabel>
						<Input
							id="pdns-expose-port-pub"
							placeholder={`api.${defaultZoneName}`}
							bind:value={exposePortPublic}
							required
						/>
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="pdns-expose-port-target">Port Target Mesin Lokal</Field.FieldLabel>
						<Input
							id="pdns-expose-port-target"
							inputmode="numeric"
							pattern="[0-9]+"
							placeholder="3000"
							bind:value={exposePortTargetStr}
							required
						/>
					</Field.Field>
				</Field.FieldGroup>
			{/if}

			<Field.FieldGroup>
				<Field.Field orientation="horizontal">
					<Checkbox id="pdns-expose-ssl" bind:checked={exposeSSL} />
					<Field.FieldLabel for="pdns-expose-ssl" class="font-normal">
						Otomatis Pasang SSL Let's Encrypt (HTTPS)
					</Field.FieldLabel>
				</Field.Field>
				<Field.Field orientation="horizontal">
					<Checkbox id="pdns-expose-ws" bind:checked={exposeWS} />
					<Field.FieldLabel for="pdns-expose-ws" class="font-normal">
						Dukungan WebSocket (WSS / Upgrade)
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>
		</form>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (exposeOpen = false)}>Batal</Button>
			<Button type="submit" form="pdns-expose-form" disabled={exposing}>
				{#if exposing}
					<Spinner data-icon="inline-start" />
					Mengekspos...
				{:else}
					<CloudUploadIcon data-icon="inline-start" />
					Ekspos Sekarang
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Deploy VPS Modal -->
<Dialog.Root bind:open={deployOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-lg">
		<Dialog.Header class="pr-8">
			<Dialog.Title>Deploy &amp; Install VPS Edge Gateway</Dialog.Title>
			<Dialog.Description>
				Instalasi otomatis PowerDNS Authoritative, SQLite3, Nginx Reverse Proxy, dan Edge Agent ke VPS via SSH.
			</Dialog.Description>
		</Dialog.Header>

		<form id="pdns-deploy-form" onsubmit={submitDeployVPS} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain pr-1">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="vps-deploy-host">Host / IP VPS (NetBird atau Publik)</Field.FieldLabel>
					<Input
						id="vps-deploy-host"
						placeholder="100.68.66.249 atau 103.150.20.50"
						bind:value={deployHost}
						required
					/>
					<Field.FieldDescription>Disarankan menggunakan IP NetBird mesh.</Field.FieldDescription>
				</Field.Field>

				<div class="grid gap-3 sm:grid-cols-2">
					<Field.Field>
						<Field.FieldLabel for="vps-deploy-user">Username SSH</Field.FieldLabel>
						<Input id="vps-deploy-user" bind:value={deployUser} required />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="vps-deploy-port">Port SSH</Field.FieldLabel>
						<Input id="vps-deploy-port" inputmode="numeric" pattern="[0-9]+" bind:value={deployPortStr} required />
					</Field.Field>
				</div>

				<Field.Field>
					<Field.FieldTitle id="vps-auth-label">Metode Autentikasi SSH</Field.FieldTitle>
					<ToggleGroup.Root
						type="single"
						bind:value={deployAuthMethod}
						variant="outline"
						spacing={2}
						class="flex w-full flex-wrap"
						aria-labelledby="vps-auth-label"
						onValueChange={(v) => {
							if (v === 'password' || v === 'key') deployAuthMethod = v;
						}}
					>
						<ToggleGroup.Item value="password" class="min-w-[8rem] flex-1">Password SSH</ToggleGroup.Item>
						<ToggleGroup.Item value="key" class="min-w-[8rem] flex-1">SSH Private Key</ToggleGroup.Item>
					</ToggleGroup.Root>
				</Field.Field>

				{#if deployAuthMethod === 'password'}
					<Field.Field>
						<Field.FieldLabel for="vps-deploy-password">Password Login SSH</Field.FieldLabel>
						<PasswordInput
							id="vps-deploy-password"
							canGenerate={false}
							autocomplete="current-password"
							placeholder="Masukkan password SSH user VPS"
							bind:value={deployPassword}
							required
						/>
					</Field.Field>
				{:else}
					<Field.Field>
						<Field.FieldLabel for="vps-deploy-keypath">Path Private Key SSH di Mesin Lokal</Field.FieldLabel>
						<Input
							id="vps-deploy-keypath"
							placeholder="~/.ssh/id_ed25519"
							bind:value={deployKeyPath}
						/>
					</Field.Field>
				{/if}
			</Field.FieldGroup>
		</form>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (deployOpen = false)}>Batal</Button>
			<Button type="submit" form="pdns-deploy-form" disabled={deploying}>
				{#if deploying}
					<Spinner data-icon="inline-start" />
					Memulai...
				{:else}
					Mulai Deploy &amp; Setup VPS
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Connect Manual VPS Modal -->
<Dialog.Root bind:open={connectOpen}>
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-lg">
		<Dialog.Header class="pr-8">
			<Dialog.Title>Hubungkan Manual ke VPS Edge</Dialog.Title>
			<Dialog.Description>
				Masukkan kredensial dari <code>/etc/wdev-edge/edge.conf</code> di VPS jika PowerDNS &amp; Edge Agent sudah terpasang.
			</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={submitConnectVPS} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="vps-conn-netbird-ip">IP NetBird VPS (wt0)</Field.FieldLabel>
					<Input id="vps-conn-netbird-ip" placeholder="100.68.66.249" bind:value={connNetbirdIP} required />
				</Field.Field>
				<Field.Field>
					<Field.FieldLabel for="vps-conn-public-ip">IP Publik Edge Gateway (Opsional)</Field.FieldLabel>
					<Input id="vps-conn-public-ip" placeholder="174.128.205.124" bind:value={connPublicIP} />
				</Field.Field>
				<Field.Field>
					<Field.FieldLabel for="vps-conn-api-key">API Key PowerDNS</Field.FieldLabel>
					<Input id="vps-conn-api-key" placeholder="API Key dari edge.conf" bind:value={connAPIKey} required />
				</Field.Field>
				<div class="grid gap-3 sm:grid-cols-2">
					<Field.Field>
						<Field.FieldLabel for="vps-conn-pdns-port">Port PowerDNS API</Field.FieldLabel>
						<Input id="vps-conn-pdns-port" inputmode="numeric" pattern="[0-9]+" bind:value={connPDNSPort} />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="vps-conn-agent-port">Port Edge Agent</Field.FieldLabel>
						<Input id="vps-conn-agent-port" inputmode="numeric" pattern="[0-9]+" bind:value={connAgentPort} />
					</Field.Field>
				</div>
			</Field.FieldGroup>

			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (connectOpen = false)}>Batal</Button>
				<Button type="submit" disabled={connecting}>
					{#if connecting}
						<Spinner data-icon="inline-start" />
						Menyimpan...
					{:else}
						Simpan &amp; Hubungkan
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
