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
	import PaginationBar from '#lib/components/shared/PaginationBar.svelte';
	import PasswordInput from '#lib/components/shared/PasswordInput.svelte';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
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
	import UnplugIcon from '@lucide/svelte/icons/unplug';

	type ActiveTab = 'zones' | 'expose' | 'proxy';
	type ExposeFilter = 'all' | 'exposed' | 'local';
	type ProxyFilter = 'all' | 'vhost' | 'service';

	const PAGE_SIZE = 10;

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
	let recordPage = $state(1);
	const paginatedRRSets = $derived(
		rrsets.slice((recordPage - 1) * PAGE_SIZE, recordPage * PAGE_SIZE)
	);
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
			toast.success(t('powerdns.toasts.copied', { label, value: val }));
		} catch {
			toast.error(t('powerdns.toasts.copyFailed', { label }));
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
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.statusLoadError'));
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
			zonesError = err instanceof Error ? err.message : t('powerdns.toasts.zonesLoadError');
		}
	}

	async function loadExposures(showProgress = false) {
		if (showProgress) refreshingExposures = true;
		exposuresError = '';
		try {
			exposures = (await api.listPDNSExposures()) ?? [];
		} catch (err) {
			exposures = [];
			exposuresError = err instanceof Error ? err.message : t('powerdns.toasts.edgeUnreachable');
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
			toast.success(t('powerdns.toasts.pdnsStarted'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.pdnsStartError'));
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
			deployHost = '';
			connNetbirdIP = '';
			connPublicIP = '';
			connAPIKey = '';
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
				toast.warning(t('powerdns.toasts.selectVhost'));
				return;
			}
			pubDomain = exposeSitePublic.trim();
			if (!pubDomain) {
				toast.warning(t('powerdns.toasts.domainRequired'));
				return;
			}
		} else {
			pubDomain = exposePortPublic.trim();
			targetPort = parseInt(exposePortTargetStr, 10) || 0;
			if (!pubDomain) {
				toast.warning(t('powerdns.toasts.domainRequired'));
				return;
			}
			if (targetPort < 1 || targetPort > 65535) {
				toast.warning(t('powerdns.toasts.invalidPort'));
				return;
			}
		}

		exposeOpen = false;
		const ok = await cp.confirm({
			title: t('powerdns.confirm.exposeTitle'),
			description: t('powerdns.confirm.exposeDesc', { domain: pubDomain }),
			confirmText: t('powerdns.confirm.exposeBtn')
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
			toast.success(t('powerdns.toasts.exposeSuccess', { domain: pubDomain }));
			await loadAll();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.exposeError'));
			exposeOpen = true;
		} finally {
			exposing = false;
		}
	}

	async function unexposeDomain(domain: string) {
		const ok = await cp.confirm({
			title: t('powerdns.confirm.unexposeTitle'),
			description: t('powerdns.confirm.unexposeDesc', { domain }),
			confirmText: t('powerdns.confirm.unexposeBtn'),
			destructive: true
		});
		if (!ok) return;
		unexposingPending = { ...unexposingPending, [domain]: true };
		try {
			await api.unexposePDNS(domain, false);
			toast.success(t('powerdns.toasts.unexposeSuccess', { domain }));
			await loadAll();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.unexposeError'));
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
			toast.warning(t('powerdns.toasts.invalidZoneName'));
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
					t('powerdns.toasts.zoneAndVhostCreated', {
						zone: clean,
						root: res.vhost_root || `/var/www/${clean}`
					})
				);
			} else if (res.vhost_status === 'exists') {
				toast.success(t('powerdns.toasts.zoneCreatedExistingVhost', { zone: clean }));
			} else {
				toast.success(t('powerdns.toasts.zoneCreated', { zone: clean }));
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
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.zoneAddError'));
		} finally {
			addingZone = false;
		}
	}

	async function runZoneDoctor(zoneName: string) {
		const reqId = ++reportRequestSeq;
		zonePending = { ...zonePending, [zoneName]: 'doctor' };
		reportTitle = t('powerdns.report.doctorTitle', { zone: zoneName });
		reportData = null;
		reportError = '';
		reportLoadingMessage = t('powerdns.report.doctorLoading', { zone: zoneName });
		reportLoading = true;
		reportOpen = true;
		try {
			const res = await api.doctorPDNSZone(zoneName);
			if (reqId !== reportRequestSeq) return;
			reportData = res;
		} catch (err) {
			if (reqId !== reportRequestSeq) return;
			const msg = err instanceof Error ? err.message : t('powerdns.toasts.zoneDoctorError');
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
			title: t('powerdns.confirm.repairTitle'),
			description: t('powerdns.confirm.repairDesc', { zone: zoneName }),
			confirmText: t('powerdns.confirm.repairBtn')
		});
		if (!ok) return;
		const reqId = ++reportRequestSeq;
		zonePending = { ...zonePending, [zoneName]: 'repair' };
		reportTitle = t('powerdns.report.repairTitle', { zone: zoneName });
		reportData = null;
		reportError = '';
		reportLoadingMessage = t('powerdns.report.repairLoading', { zone: zoneName });
		reportLoading = true;
		reportOpen = true;
		try {
			const res = await api.repairPDNSZone(zoneName);
			if (reqId !== reportRequestSeq) return;
			reportData = res.report ?? res;
			await loadZones();
		} catch (err) {
			if (reqId !== reportRequestSeq) return;
			const msg = err instanceof Error ? err.message : t('powerdns.toasts.zoneRepairError');
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
			title: t('powerdns.confirm.deleteZoneTitle'),
			description: t('powerdns.confirm.deleteZoneDesc', { zone: zoneName }),
			confirmText: t('powerdns.confirm.deleteZoneBtn'),
			destructive: true
		});
		if (!ok) return;
		zonePending = { ...zonePending, [zoneName]: 'delete' };
		try {
			await api.deletePDNSZone(zoneName);
			toast.success(t('powerdns.toasts.zoneDeleteSuccess', { zone: zoneName }));
			await loadZones();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.zoneDeleteError'));
		} finally {
			const next = { ...zonePending };
			delete next[zoneName];
			zonePending = next;
		}
	}

	async function openRecordsModal(zoneName: string) {
		currentZone = zoneName;
		recordPage = 1;
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
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.recordsLoadError'));
		} finally {
			recordsLoading = false;
		}
	}

	async function submitAddRecord(e: SubmitEvent) {
		e.preventDefault();
		if (!recContent.trim()) {
			toast.warning(t('powerdns.toasts.recordContentRequired'));
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
			toast.success(t('powerdns.toasts.recordSaved'));
			await loadRecords(currentZone);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.recordAddError'));
		} finally {
			savingRecord = false;
		}
	}

	async function deleteRecord(name: string, type: string) {
		recordsOpen = false;
		const ok = await cp.confirm({
			title: t('powerdns.confirm.deleteRecordTitle'),
			description: t('powerdns.confirm.deleteRecordDesc', { type, name }),
			confirmText: t('powerdns.confirm.deleteRecordBtn'),
			destructive: true
		});
		recordsOpen = true;
		if (!ok) return;
		const key = `${name}:${type}`;
		recordPending = { ...recordPending, [key]: true };
		try {
			await api.deletePDNSRecord(currentZone, name, type);
			toast.success(t('powerdns.toasts.recordDeleteSuccess', { type, name }));
			await loadRecords(currentZone);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.recordDeleteError'));
		} finally {
			const next = { ...recordPending };
			delete next[key];
			recordPending = next;
		}
	}

	async function submitDeployVPS(e: SubmitEvent) {
		e.preventDefault();
		if (!deployHost.trim()) {
			toast.warning(t('powerdns.toasts.deployHostRequired'));
			return;
		}
		if (deployAuthMethod === 'password' && !deployPassword) {
			toast.warning(t('powerdns.toasts.deployPasswordRequired'));
			return;
		}
		const port = parseInt(deployPortStr, 10) || 22;
		const user = deployUser.trim() || 'root';
		deployOpen = false;
		const ok = await cp.confirm({
			title: t('powerdns.confirm.deployTitle'),
			description: t('powerdns.confirm.deployDesc', { user, host: deployHost.trim(), port }),
			confirmText: t('powerdns.confirm.deployBtn')
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
			cp.openTerminal(t('powerdns.terminal.deployTitle'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.deployError'));
			deployOpen = true;
		} finally {
			deploying = false;
		}
	}

	async function submitConnectVPS(e: SubmitEvent) {
		e.preventDefault();
		if (!connNetbirdIP.trim() || !connAPIKey.trim()) {
			toast.warning(t('powerdns.toasts.connectRequired'));
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
			toast.success(t('powerdns.toasts.connectSuccess'));
			await loadAll();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('powerdns.toasts.connectError'));
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
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('powerdns.title')}</h1>
				<p class="text-sm text-muted-foreground">
					{t('powerdns.descVps')}
				</p>
			{:else if isLocal}
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('powerdns.titleLocal')}</h1>
				<p class="text-sm text-muted-foreground">
					{t('powerdns.descLocal')}
				</p>
			{:else}
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('powerdns.titleEdge')}</h1>
				<p class="text-sm text-muted-foreground">
					{t('powerdns.descEdge')}
				</p>
			{/if}
		</div>

		<div class="flex shrink-0 flex-wrap items-center gap-2">
			{#if !isEdge}
				<StatusBadge
					tone={edge?.reachable ? 'success' : edge?.has_local_pdns ? 'warning' : 'destructive'}
					label={edge?.reachable
						? t('powerdns.badges.pdnsActive')
						: edge?.has_local_pdns
							? t('powerdns.badges.pdnsInactive')
							: t('powerdns.badges.pdnsNotInstalled')}
				/>
				{#if !edge?.reachable && !edge?.has_local_pdns}
					<Button size="sm" onclick={() => cp.triggerInstall('pdns')}>
						{t('powerdns.buttons.installPdns')}
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
						{t('powerdns.buttons.nameserverGuide')}
					</Button>
				{/if}
				<Button size="sm" variant={edge?.reachable ? 'default' : 'outline'} onclick={() => (addZoneOpen = true)}>
					<PlusIcon data-icon="inline-start" />
					{t('powerdns.buttons.addZone')}
				</Button>
			{:else}
				{#if edge}
					<StatusBadge
						tone={edge.reachable ? 'success' : 'destructive'}
						label={edge.reachable ? t('powerdns.badges.edgeConnected') : t('powerdns.badges.edgeDisconnected')}
					/>
				{/if}
				<Button variant={edge?.reachable ? 'outline' : 'default'} size="sm" onclick={() => (deployOpen = true)}>
					<ServerIcon data-icon="inline-start" />
					{t('powerdns.buttons.deployVps')}
				</Button>
				<Button variant="outline" size="sm" onclick={() => (connectOpen = true)}>
					<LinkIcon data-icon="inline-start" />
					{t('powerdns.buttons.connectManual')}
				</Button>
			{/if}
		</div>
	</div>

	<!-- Contextual Alerts -->
	{#if isLocal}
		<Alert.Root class="border-muted-foreground/30 bg-muted/40">
			<InfoIcon />
			<Alert.Title>{t('powerdns.alerts.localTitle')}</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					{t('powerdns.alerts.localDescPre')} <strong>{t('powerdns.alerts.localDescMode')}</strong>{t('powerdns.alerts.localDescMid')} <code>/etc/hosts</code>{t('powerdns.alerts.localDescPost')}
				</span>
				<Button size="xs" variant="outline" class="shrink-0" onclick={() => modeStore.openModal()}>
					{t('powerdns.alerts.switchMode')}
				</Button>
			</Alert.Description>
		</Alert.Root>
	{/if}

	{#if !isEdge && edge && !edge.reachable}
		<Alert.Root variant="destructive">
			<TriangleAlertIcon />
			<Alert.Title>
				{edge.has_local_pdns
					? t('powerdns.alerts.localInactiveTitle')
					: t('powerdns.alerts.localNotInstalledTitle')}
			</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					{edge.has_local_pdns
						? t('powerdns.alerts.localInactiveDesc')
						: t('powerdns.alerts.localNotInstalledDesc')}
				</span>
				<div class="flex flex-wrap gap-2">
					{#if edge.has_local_pdns}
						<Button size="xs" disabled={startingPDNS} onclick={startLocalPDNS}>
							{#if startingPDNS}
								<Spinner data-icon="inline-start" />
								{t('powerdns.alerts.startingPdns')}
							{:else}
								{t('powerdns.alerts.startLocalPdns')}
							{/if}
						</Button>
					{/if}
					<Button size="xs" variant={edge.has_local_pdns ? 'outline' : 'default'} onclick={() => cp.triggerInstall('pdns')}>
						{edge.has_local_pdns ? t('powerdns.alerts.reconfigurePdns') : t('powerdns.alerts.installPdnsNow')}
					</Button>
				</div>
			</Alert.Description>
		</Alert.Root>
	{:else if isEdge && edge && !edge.reachable}
		<Alert.Root variant="destructive">
			<TriangleAlertIcon />
			<Alert.Title>{t('powerdns.alerts.edgeNotConnectedTitle')}</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					{t('powerdns.alerts.edgeNotConnectedDesc')}
				</span>
				<div class="flex flex-wrap gap-2">
					<Button size="xs" onclick={() => (deployOpen = true)}>{t('powerdns.alerts.startDeployVps')}</Button>
					<Button variant="outline" size="xs" onclick={() => (connectOpen = true)}>{t('powerdns.buttons.connectManual')}</Button>
				</div>
			</Alert.Description>
		</Alert.Root>
	{/if}

	{#if isVPS && glueGuideOpen}
		<Alert.Root class="border-primary/40 bg-primary/5">
			<InfoIcon class="text-primary" />
			<Alert.Title class="font-semibold text-primary">{t('powerdns.guide.title')}</Alert.Title>
			<Alert.Description class="mt-2 space-y-2 text-xs leading-relaxed text-muted-foreground">
				<p>
					{t('powerdns.guide.intro')}
				</p>
				<ol class="list-inside list-decimal space-y-1.5">
					<li>{t('powerdns.guide.step1')}</li>
					<li>{t('powerdns.guide.step2Pre')} <strong>{t('powerdns.guide.step2Bold')}</strong> {t('powerdns.guide.step2Post')}</li>
					<li>
						{t('powerdns.guide.step3')}
						<div class="mt-1.5 flex flex-col gap-1.5 sm:flex-row sm:flex-wrap">
							<span class="inline-flex items-center gap-1.5 rounded-md border bg-background px-2 py-1 font-mono text-foreground">
								ns1.{defaultZoneName} &rarr; {edge?.vps_public_ip || t('powerdns.guide.vpsPublicIpPlaceholder')}
								{#if edge?.vps_public_ip}
									<Button
										variant="ghost"
										size="icon-xs"
										aria-label={t('powerdns.guide.copyNs1Aria')}
										onclick={() => copyText(`ns1.${defaultZoneName}`, t('powerdns.copyLabels.ns1'))}
									>
										<CopyIcon />
									</Button>
								{/if}
							</span>
							<span class="inline-flex items-center gap-1.5 rounded-md border bg-background px-2 py-1 font-mono text-foreground">
								ns2.{defaultZoneName} &rarr; {edge?.vps_public_ip || t('powerdns.guide.vpsPublicIpPlaceholder')}
								{#if edge?.vps_public_ip}
									<Button
										variant="ghost"
										size="icon-xs"
										aria-label={t('powerdns.guide.copyNs2Aria')}
										onclick={() => copyText(`ns2.${defaultZoneName}`, t('powerdns.copyLabels.ns2'))}
									>
										<CopyIcon />
									</Button>
								{/if}
							</span>
						</div>
					</li>
					<li>{t('powerdns.guide.step4Pre')} <strong>{t('powerdns.guide.step4Bold')}</strong> {t('powerdns.guide.step4Mid')} <code class="font-mono">ns1.{defaultZoneName}</code> {t('powerdns.guide.step4And')} <code class="font-mono">ns2.{defaultZoneName}</code>{t('powerdns.guide.step4Suffix')}</li>
					<li>{t('powerdns.guide.step5')}</li>
				</ol>
			</Alert.Description>
		</Alert.Root>
	{/if}

	<!-- Unified Network & DNS Topology Strip -->
	<Card.Root class="gap-0 py-0">
		<div class="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/30 px-4 py-2.5">
			<div class="flex items-center gap-2">
				<NetworkIcon class="size-4 text-primary" aria-hidden="true" />
				<span class="text-sm font-semibold">{t('powerdns.topology.sectionTitle')}</span>
				<Badge variant="outline" class="font-normal">
					{isEdge ? t('powerdns.topology.edgeMode') : isVPS ? t('powerdns.topology.fullVpsMode') : t('powerdns.topology.localMode')}
				</Badge>
			</div>
			<div class="flex flex-wrap items-center gap-3 text-xs tabular-nums text-muted-foreground">
				<span><strong class="font-semibold text-foreground">{zones.length}</strong> {t('powerdns.topology.dnsZones')}</span>
				{#if isEdge}
					<span><strong class="font-semibold text-foreground">{exposedSitesCount}/{sites.length}</strong> {t('powerdns.topology.vhostsExposed')}</span>
					<span><strong class="font-semibold text-foreground">{exposures.length}</strong> {t('powerdns.topology.activeProxies')}</span>
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
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.vpsPublicIpTitle')}</span>
					<div class="flex items-center gap-1.5">
						<span class="truncate font-mono text-base font-semibold tabular-nums">
							{edge?.vps_public_ip || '-'}
						</span>
						{#if edge?.vps_public_ip}
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label={t('powerdns.topology.copyVpsPublicIpAria')}
								onclick={() => copyText(edge?.vps_public_ip || '', t('powerdns.copyLabels.vpsPublicIp'))}
							>
								<CopyIcon />
							</Button>
						{/if}
					</div>
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.vpsPublicIpDesc')}</span>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.authNameserverTitle')}</span>
					<div class="flex flex-col gap-1 font-mono text-xs sm:text-sm">
						<div class="flex items-center gap-1.5">
							<span class="truncate font-semibold">ns1.{defaultZoneName}</span>
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label={t('powerdns.topology.copyNsAria', { ns: `ns1.${defaultZoneName}` })}
								onclick={() => copyText(`ns1.${defaultZoneName}`, t('powerdns.copyLabels.ns1Short'))}
							>
								<CopyIcon />
							</Button>
						</div>
						<div class="flex items-center gap-1.5">
							<span class="truncate font-semibold">ns2.{defaultZoneName}</span>
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label={t('powerdns.topology.copyNsAria', { ns: `ns2.${defaultZoneName}` })}
								onclick={() => copyText(`ns2.${defaultZoneName}`, t('powerdns.copyLabels.ns2Short'))}
							>
								<CopyIcon />
							</Button>
						</div>
					</div>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.engineEndpointTitle')}</span>
					<span class="font-mono text-base font-semibold tabular-nums">
						127.0.0.1:{edge?.pdns_port || '8081'}
					</span>
					<span class="text-xs text-muted-foreground">
						{t('powerdns.topology.engineEndpointDesc')}
					</span>
				</div>
			</div>
		{:else if isLocal}
			<div class="grid divide-y md:grid-cols-3 md:divide-x md:divide-y-0">
				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.localApiTitle')}</span>
					<div class="flex items-center gap-1.5">
						<span class="truncate font-mono text-base font-semibold tabular-nums">
							{edge?.vps_netbird_ip || '127.0.0.1'}:{edge?.pdns_port || '8081'}
						</span>
						<Button
							variant="ghost"
							size="icon-xs"
							aria-label={t('powerdns.topology.copyLocalApiAria')}
							onclick={() =>
								copyText(
									`${edge?.vps_netbird_ip || '127.0.0.1'}:${edge?.pdns_port || '8081'}`,
									t('powerdns.copyLabels.pdnsEndpoint')
								)}
						>
							<CopyIcon />
						</Button>
					</div>
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.localApiDesc')}</span>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.localHostTitle')}</span>
					<div class="flex items-center gap-1.5">
						<span class="truncate font-mono text-base font-semibold tabular-nums">
							{edge?.vps_public_ip || '127.0.0.1'}
						</span>
						<Button
							variant="ghost"
							size="icon-xs"
							aria-label={t('powerdns.topology.copyLocalHostAria')}
							onclick={() => copyText(edge?.vps_public_ip || '127.0.0.1', t('powerdns.copyLabels.hostIp'))}
						>
							<CopyIcon />
						</Button>
					</div>
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.localHostDesc')}</span>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.localModeTitle')}</span>
					<span class="text-base font-semibold">{t('powerdns.topology.localMode')}</span>
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.localModeDesc')}</span>
				</div>
			</div>
		{:else}
			<div class="grid divide-y md:grid-cols-3 md:divide-x md:divide-y-0">
				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.edgePublicIpTitle')}</span>
					<div class="flex items-center gap-1.5">
						<span class="truncate font-mono text-base font-semibold tabular-nums">
							{edge?.vps_public_ip || '-'}
						</span>
						{#if edge?.vps_public_ip}
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label={t('powerdns.topology.copyEdgeIpAria')}
								onclick={() => copyText(edge?.vps_public_ip || '', t('powerdns.copyLabels.edgePublicIp'))}
							>
								<CopyIcon />
							</Button>
						{/if}
					</div>
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.edgePublicIpDesc')}</span>
				</div>

				<div class="flex flex-col gap-1 p-4">
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.meshTitle')}</span>
					<div class="flex flex-wrap items-center gap-1.5 font-mono text-sm font-semibold tabular-nums">
						<span>{edge?.local_netbird_ip || t('powerdns.topology.localWt0')}</span>
						<ArrowLeftRightIcon class="size-3.5 text-info" aria-hidden="true" />
						<span>{edge?.vps_netbird_ip || '-'}</span>
						{#if edge?.vps_netbird_ip}
							<Button
								variant="ghost"
								size="icon-xs"
								aria-label={t('powerdns.topology.copyNetbirdIpAria')}
								onclick={() => copyText(edge?.vps_netbird_ip || '', t('powerdns.copyLabels.vpsNetbirdIp'))}
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
					<span class="text-xs text-muted-foreground">{t('powerdns.topology.edgeSummaryTitle')}</span>
					<div class="flex items-baseline gap-2">
						<span class="text-base font-semibold tabular-nums">
							{exposedSitesCount}/{sites.length} {t('powerdns.topology.edgeSummaryVhosts')}
						</span>
						<span class="text-xs text-muted-foreground tabular-nums">
							· {t('powerdns.topology.edgeSummaryProxies', { count: exposures.length })}
						</span>
					</div>
					<span class="text-xs text-muted-foreground">
						{t('powerdns.topology.edgeSummaryDesc')}
					</span>
				</div>
			</div>
		{/if}
	</Card.Root>

	<!-- Pengelolaan DNS & Edge Gateway (Tabbed) -->
	<Card.Root class="gap-0 py-0">
		<Tabs.Root value={activeTab} onValueChange={handleTabChange} class="gap-0">
			<div class="flex flex-col border-b">
				<div class="flex flex-col gap-3 px-4 pt-3.5 pb-2.5 sm:flex-row sm:items-center sm:justify-between">
					<div>
						{#if activeTab === 'zones'}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">{t('powerdns.tabs.zonesHeading')}</h2>
								<Badge variant="secondary" class="tabular-nums">{zones.length}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								{t('powerdns.tabs.zonesSubtitle')}
							</p>
						{:else if activeTab === 'expose'}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">{t('powerdns.tabs.exposeHeading')}</h2>
								<Badge variant="secondary" class="tabular-nums">{exposedSitesCount}/{sites.length}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								{t('powerdns.tabs.exposeSubtitle')}
							</p>
						{:else}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">{t('powerdns.tabs.proxyHeading')}</h2>
								<Badge variant="secondary" class="tabular-nums">{exposures.length}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								{t('powerdns.tabs.proxySubtitle')}
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
								{t('powerdns.buttons.refreshing')}
							{:else}
								<RefreshCwIcon data-icon="inline-start" />
								{t('powerdns.buttons.refresh')}
							{/if}
						</Button>
						{#if activeTab === 'zones'}
							<Button size="sm" onclick={() => (addZoneOpen = true)}>
								<PlusIcon data-icon="inline-start" />
								{t('powerdns.buttons.addZone')}
							</Button>
						{:else}
							<Button
								size="sm"
								onclick={() => openExposeModal(sites.length > 0 ? 'site' : 'port')}
							>
								<PlusIcon data-icon="inline-start" />
								{t('powerdns.buttons.addExposure')}
							</Button>
						{/if}
					</div>
				</div>

				<div class="px-4">
					<Tabs.List
						variant="line"
						aria-label={t('powerdns.tabs.ariaLabel')}
						class="h-11 group-data-horizontal/tabs:h-11 w-full flex-nowrap items-stretch justify-start gap-1.5 overflow-x-auto overflow-y-hidden rounded-none bg-transparent p-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden sm:gap-2"
					>
						<Tabs.Trigger
							value="zones"
							bind:ref={zoneTabRef}
							class="h-full flex-none shrink-0 whitespace-nowrap rounded-none border-0 px-2.5 py-0 text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 focus-visible:ring-0 data-active:text-foreground group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-0 group-data-horizontal/tabs:after:h-0 after:border-b-2 after:border-primary after:bg-transparent sm:px-3"
						>
							<GlobeIcon aria-hidden="true" class="size-3.5" />
							<span>{t('powerdns.tabs.zones')}</span>
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
								<span>{t('powerdns.tabs.expose')}</span>
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
								<span>{t('powerdns.tabs.proxy')}</span>
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
									placeholder={t('powerdns.zonesTab.searchPlaceholder')}
									aria-label={t('powerdns.zonesTab.searchAria')}
									bind:value={zoneSearch}
								/>
							</InputGroup.Root>
						</search>

						<div class="flex items-center gap-2">
							<span class="text-xs text-muted-foreground">
								{t('powerdns.zonesTab.countLabel', { shown: filteredZones.length, total: zones.length })}
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
					{t('powerdns.zonesTab.retry')}
				</Button>
			</div>
		{:else if zones.length === 0}
			<div class="p-4">
				<Empty.Root class="border-0 py-8">
					<Empty.Header>
						<Empty.Media variant="icon">
							<GlobeIcon />
						</Empty.Media>
						<Empty.Title>{t('powerdns.zonesTab.emptyTitle')}</Empty.Title>
						<Empty.Description>
							{t('powerdns.zonesTab.emptyDesc')}
						</Empty.Description>
					</Empty.Header>
					<Empty.Content>
						<Button size="sm" onclick={() => (addZoneOpen = true)}>
							<PlusIcon data-icon="inline-start" />
							{t('powerdns.zonesTab.addFirstBtn')}
						</Button>
					</Empty.Content>
				</Empty.Root>
			</div>
		{:else if filteredZones.length === 0}
			<div class="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center">
				<p class="text-sm text-muted-foreground">{t('powerdns.zonesTab.noMatch', { query: zoneSearch })}</p>
				<Button variant="outline" size="xs" onclick={() => (zoneSearch = '')}>
					{t('powerdns.zonesTab.resetSearch')}
				</Button>
			</div>
		{:else}
			<div class="p-3 sm:p-0">
				<ResponsiveTableWrapper label={t('powerdns.zonesTab.tableAria')} class="rounded-none border-0 bg-transparent">
					<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
						<Table.Header>
							<Table.Row>
								<Table.Head scope="col">{t('powerdns.zonesTab.colName')}</Table.Head>
								<Table.Head scope="col">{t('powerdns.tables.type')}</Table.Head>
								<Table.Head scope="col">{t('powerdns.tables.serial')}</Table.Head>
								<Table.Head scope="col">{t('powerdns.tables.status')}</Table.Head>
								<Table.Head scope="col" class="text-right">{t('powerdns.tables.actions')}</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each paginatedZones as z (z.name)}
								{@const zPending = zonePending[z.name]}
								<Table.Row>
									<Table.Cell data-label={t('powerdns.tables.zone')}>
										<button
											type="button"
											class="text-left font-semibold text-foreground hover:text-primary hover:underline focus-visible:rounded-sm focus-visible:outline-2 focus-visible:outline-ring"
											onclick={() => openRecordsModal(z.name)}
										>
											{z.name}
										</button>
									</Table.Cell>
									<Table.Cell data-label={t('powerdns.tables.type')}>
										<Badge variant="secondary">{z.kind}</Badge>
									</Table.Cell>
									<Table.Cell data-label={t('powerdns.tables.serial')} class="font-mono text-xs tabular-nums text-muted-foreground">
										{z.serial}
									</Table.Cell>
									<Table.Cell data-label={t('powerdns.tables.status')}>
										<StatusBadge tone="success" label={t('powerdns.zonesTab.statusActive')} />
									</Table.Cell>
									<Table.Cell data-label={t('powerdns.tables.actions')} class="text-right">
										<div class="flex items-center justify-end gap-1.5">
											<Button
												variant="outline"
												size="xs"
												disabled={Boolean(zPending)}
												onclick={() => openRecordsModal(z.name)}
											>
												<ListIcon data-icon="inline-start" />
												{t('powerdns.tables.records')}<span class="sr-only"> {z.name}</span>
											</Button>
											<DropdownMenu.Root>
												<DropdownMenu.Trigger>
													{#snippet child({ props })}
														<Button variant="outline" size="xs" disabled={Boolean(zPending)} {...props}>
															{#if zPending === 'doctor'}
																<Spinner data-icon="inline-start" />
																{t('powerdns.zonesTab.checking')}
															{:else if zPending === 'repair'}
																<Spinner data-icon="inline-start" />
																{t('powerdns.zonesTab.repairing')}
															{:else if zPending === 'delete'}
																<Spinner data-icon="inline-start" />
																{t('powerdns.zonesTab.deleting')}
															{:else}
																{t('powerdns.tables.actions')}<span class="sr-only">{t('powerdns.zonesTab.actionsForSr', { name: z.name })}</span>
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
															<span>{t('powerdns.tables.manageRecords')}</span>
														</DropdownMenu.Item>
														<DropdownMenu.Item disabled={Boolean(zPending)} onclick={() => runZoneDoctor(z.name)}>
															<StethoscopeIcon />
															<span>{t('powerdns.tables.zoneDoctor')}</span>
														</DropdownMenu.Item>
														<DropdownMenu.Item disabled={Boolean(zPending)} onclick={() => runZoneRepair(z.name)}>
															<WrenchIcon />
															<span>{t('powerdns.tables.repairZone')}</span>
														</DropdownMenu.Item>
														<DropdownMenu.Separator />
														<DropdownMenu.Item
															variant="destructive"
															disabled={Boolean(zPending)}
															onclick={() => deleteZone(z.name)}
														>
															<Trash2Icon />
															<span>{t('powerdns.tables.deleteZone')}</span>
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
			<PaginationBar
				bind:page={zonePage}
				totalItems={filteredZones.length}
				pageSize={PAGE_SIZE}
				itemLabel={t('powerdns.tables.zoneItemLabel')}
			/>
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
										placeholder={t('powerdns.exposeTab.searchPlaceholder')}
										aria-label={t('powerdns.exposeTab.searchAria')}
										bind:value={exposeSearch}
									/>
								</InputGroup.Root>
							</search>

							<div class="flex flex-wrap items-center gap-1.5" role="group" aria-label={t('powerdns.exposeTab.filterAria')}>
								<Button
									variant={exposeFilter === 'all' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={exposeFilter === 'all'}
									onclick={() => (exposeFilter = 'all')}
								>
									{t('powerdns.exposeTab.filterAll', { count: sites.length })}
								</Button>
								<Button
									variant={exposeFilter === 'exposed' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={exposeFilter === 'exposed'}
									onclick={() => (exposeFilter = 'exposed')}
								>
									{t('powerdns.exposeTab.filterExposed', { count: exposedSitesCount })}
								</Button>
								<Button
									variant={exposeFilter === 'local' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={exposeFilter === 'local'}
									onclick={() => (exposeFilter = 'local')}
								>
									{t('powerdns.exposeTab.filterLocal', { count: localOnlySitesCount })}
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
										<Empty.Title>{t('powerdns.exposeTab.emptyTitle')}</Empty.Title>
										<Empty.Description>
											{t('powerdns.exposeTab.emptyDesc')}
										</Empty.Description>
									</Empty.Header>
									<Empty.Content class="flex flex-wrap justify-center gap-2">
										<Button variant="outline" size="sm" href="/webstack">
											{t('powerdns.exposeTab.openWebstack')}
										</Button>
										<Button size="sm" onclick={() => openExposeModal('port')}>
											<CloudUploadIcon data-icon="inline-start" />
											{t('powerdns.exposeTab.exposePort')}
										</Button>
									</Empty.Content>
								</Empty.Root>
							</div>
						{:else if filteredSites.length === 0}
							<div class="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center">
								<p class="text-sm text-muted-foreground">
									{t('powerdns.exposeTab.noMatch')}
								</p>
								<Button
									variant="outline"
									size="xs"
									onclick={() => {
										exposeSearch = '';
										exposeFilter = 'all';
									}}
								>
									{t('powerdns.exposeTab.resetFilter')}
								</Button>
							</div>
						{:else}
							<div class="p-3 sm:p-0">
								<ResponsiveTableWrapper
									label={t('powerdns.exposeTab.tableAria')}
									class="rounded-none border-0 bg-transparent"
								>
									<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
										<Table.Header>
											<Table.Row>
												<Table.Head scope="col">{t('powerdns.exposeTab.colVhost')}</Table.Head>
												<Table.Head scope="col">{t('powerdns.exposeTab.colFramework')}</Table.Head>
												<Table.Head scope="col">{t('powerdns.exposeTab.colStatus')}</Table.Head>
												<Table.Head scope="col">{t('powerdns.exposeTab.colPublicDomain')}</Table.Head>
												<Table.Head scope="col">{t('powerdns.exposeTab.colTarget')}</Table.Head>
												<Table.Head scope="col" class="text-right">{t('powerdns.exposeTab.colActions')}</Table.Head>
											</Table.Row>
										</Table.Header>
										<Table.Body>
											{#each paginatedSites as s (s.domain)}
												{@const targetNB = edge?.local_netbird_ip
													? `http://${edge.local_netbird_ip}:80`
													: t('powerdns.exposeTab.portFallback')}
												<Table.Row>
													<Table.Cell data-label={t('powerdns.tables.vhost')}>
														<div class="font-semibold">{s.domain}</div>
														<div class="break-all font-mono text-xs text-muted-foreground sm:max-w-44 sm:truncate">
															{s.root_path || '-'}
														</div>
													</Table.Cell>
													<Table.Cell data-label={t('powerdns.tables.framework')}>
														<Badge variant="secondary" class="uppercase">{s.framework || 'PHP'}</Badge>
														<span class="ml-1 text-xs text-muted-foreground tabular-nums">
															{s.framework === 'static'
																? t('powerdns.exposeTab.staticHtml')
																: s.framework === 'proxy'
																	? `:${s.proxy_port}`
																	: `PHP ${s.php_version || '8.4'}`}
														</span>
													</Table.Cell>
													<Table.Cell data-label={t('powerdns.tables.status')}>
														{#if s.exposed_to}
															<StatusBadge tone="success" label={t('powerdns.exposeTab.badgeExposed')} />
														{:else}
															<StatusBadge tone="neutral" label={t('powerdns.exposeTab.badgeLocalOnly')} />
														{/if}
													</Table.Cell>
													<Table.Cell data-label={t('powerdns.tables.publicDomain')}>
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
																	aria-label={t('powerdns.exposeTab.copyUrlAria', { domain: s.exposed_to })}
																	onclick={() => copyText(`https://${s.exposed_to}`, t('powerdns.copyLabels.publicUrl'))}
																>
																	<CopyIcon />
																</Button>
															</div>
														{:else}
															<span class="text-xs text-muted-foreground">{t('powerdns.exposeTab.notExposed')}</span>
														{/if}
													</Table.Cell>
													<Table.Cell data-label={t('powerdns.tables.netbirdTarget')} class="font-mono text-xs tabular-nums">
														{targetNB}
													</Table.Cell>
													<Table.Cell data-label={t('powerdns.tables.actions')} class="text-right">
														<div class="flex items-center justify-end gap-1.5">
															{#if !s.exposed_to}
																<Button
																	variant="outline"
																	size="xs"
																	onclick={() => openExposeModal('site', s.domain)}
																>
																	<CloudUploadIcon data-icon="inline-start" />
																	{t('powerdns.tables.expose')}<span class="sr-only"> {s.domain}</span>
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
																					{t('powerdns.tables.disconnecting')}
																				{:else}
																					{t('powerdns.tables.actions')}<span class="sr-only">{t('powerdns.zonesTab.actionsForSr', { name: s.domain })}</span>
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
																						<span>{t('powerdns.tables.openDomain')}</span>
																					</a>
																				{/snippet}
																			</DropdownMenu.Item>
																			<DropdownMenu.Item onclick={() => openExposeModal('site', s.domain)}>
																				<CloudUploadIcon />
																				<span>{t('powerdns.tables.updateExpose')}</span>
																			</DropdownMenu.Item>
																			<DropdownMenu.Separator />
																			<DropdownMenu.Item
																				variant="destructive"
																				disabled={Boolean(unexposingPending[s.exposed_to])}
																				onclick={() => unexposeDomain(s.exposed_to)}
																			>
																				<UnplugIcon />
																				<span>{t('powerdns.tables.disconnectExpose')}</span>
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
							<PaginationBar
								bind:page={exposePage}
								totalItems={filteredSites.length}
								pageSize={PAGE_SIZE}
								itemLabel={t('powerdns.tables.vhostItemLabel')}
							/>
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
										placeholder={t('powerdns.proxyTab.searchPlaceholder')}
										aria-label={t('powerdns.proxyTab.searchAria')}
										bind:value={proxySearch}
									/>
								</InputGroup.Root>
							</search>

							<div class="flex flex-wrap items-center gap-1.5" role="group" aria-label={t('powerdns.proxyTab.filterAria')}>
								<Button
									variant={proxyFilter === 'all' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={proxyFilter === 'all'}
									onclick={() => (proxyFilter = 'all')}
								>
									{t('powerdns.proxyTab.filterAll', { count: exposures.length })}
								</Button>
								<Button
									variant={proxyFilter === 'vhost' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={proxyFilter === 'vhost'}
									onclick={() => (proxyFilter = 'vhost')}
								>
									{t('powerdns.proxyTab.filterVhost', { count: vhostExposuresCount })}
								</Button>
								<Button
									variant={proxyFilter === 'service' ? 'secondary' : 'ghost'}
									size="xs"
									aria-pressed={proxyFilter === 'service'}
									onclick={() => (proxyFilter = 'service')}
								>
									{t('powerdns.proxyTab.filterService', { count: serviceExposuresCount })}
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
									{t('powerdns.proxyTab.reconnect')}
								</Button>
							</div>
						{:else if exposures.length === 0}
							<div class="p-4">
								<Empty.Root class="border-0 py-8">
									<Empty.Header>
										<Empty.Media variant="icon">
											<NetworkIcon />
										</Empty.Media>
										<Empty.Title>{t('powerdns.proxyTab.emptyTitle')}</Empty.Title>
										<Empty.Description>
											{t('powerdns.proxyTab.emptyDesc')}
										</Empty.Description>
									</Empty.Header>
									<Empty.Content>
										<Button size="sm" onclick={() => openExposeModal(sites.length > 0 ? 'site' : 'port')}>
											<PlusIcon data-icon="inline-start" />
											{t('powerdns.proxyTab.addFirstBtn')}
										</Button>
									</Empty.Content>
								</Empty.Root>
							</div>
						{:else if filteredExposures.length === 0}
							<div class="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center">
								<p class="text-sm text-muted-foreground">
									{t('powerdns.proxyTab.noMatch')}
								</p>
								<Button
									variant="outline"
									size="xs"
									onclick={() => {
										proxySearch = '';
										proxyFilter = 'all';
									}}
								>
									{t('powerdns.proxyTab.resetFilter')}
								</Button>
							</div>
						{:else}
							<div class="p-3 sm:p-0">
								<ResponsiveTableWrapper
									label={t('powerdns.proxyTab.tableAria')}
									class="rounded-none border-0 bg-transparent"
								>
									<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
										<Table.Header>
											<Table.Row>
												<Table.Head scope="col">{t('powerdns.proxyTab.colDomain')}</Table.Head>
												<Table.Head scope="col">{t('powerdns.proxyTab.colTarget')}</Table.Head>
												<Table.Head scope="col">{t('powerdns.proxyTab.colStatus')}</Table.Head>
												<Table.Head scope="col">{t('powerdns.proxyTab.colConfig')}</Table.Head>
												<Table.Head scope="col" class="text-right">{t('powerdns.tables.actions')}</Table.Head>
											</Table.Row>
										</Table.Header>
										<Table.Body>
											{#each paginatedExposures as exp (exp.domain)}
												<Table.Row>
													<Table.Cell data-label={t('powerdns.tables.domain')}>
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
																aria-label={t('powerdns.exposeTab.copyUrlAria', { domain: exp.domain })}
																onclick={() => copyText(`https://${exp.domain}`, t('powerdns.copyLabels.proxyUrl'))}
															>
																<CopyIcon />
															</Button>
														</div>
													</Table.Cell>
													<Table.Cell data-label={t('powerdns.tables.target')}>
														{#if exp.local_site}
															<Badge variant="secondary" class="mr-1.5">{t('powerdns.proxyTab.badgeVhost')}</Badge>
															<span class="font-medium">{exp.local_site}</span>
														{:else}
															<Badge variant="outline" class="mr-1.5">{t('powerdns.proxyTab.badgeService')}</Badge>
															<span class="font-mono text-xs">{exp.target || t('powerdns.proxyTab.vpsProxyFallback')}</span>
														{/if}
													</Table.Cell>
													<Table.Cell data-label={t('powerdns.tables.status')}>
														<StatusBadge
															tone={exp.enabled ? 'success' : 'neutral'}
															label={exp.enabled ? t('powerdns.tables.activeHttps') : t('powerdns.tables.inactive')}
														/>
													</Table.Cell>
													<Table.Cell data-label={t('powerdns.tables.config')} class="sm:max-w-52">
														<code class="block break-all text-xs text-muted-foreground sm:truncate" title={exp.config_file || '-'}>
															{exp.config_file || '-'}
														</code>
													</Table.Cell>
													<Table.Cell data-label={t('powerdns.tables.actions')} class="text-right">
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
																				{t('powerdns.zonesTab.deleting')}
																			{:else}
																				{t('powerdns.tables.actions')}<span class="sr-only">{t('powerdns.zonesTab.actionsForSr', { name: exp.domain })}</span>
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
																					<span>{t('powerdns.tables.openDomain')}</span>
																				</a>
																			{/snippet}
																		</DropdownMenu.Item>
																		<DropdownMenu.Item onclick={() => copyText(`https://${exp.domain}`, t('powerdns.copyLabels.proxyUrl'))}>
																			<CopyIcon />
																			<span>{t('powerdns.tables.copyPublicUrl')}</span>
																		</DropdownMenu.Item>
																		<DropdownMenu.Separator />
																		<DropdownMenu.Item
																			variant="destructive"
																			disabled={Boolean(unexposingPending[exp.domain])}
																			onclick={() => unexposeDomain(exp.domain)}
																		>
																			<Trash2Icon />
																			<span>{t('powerdns.tables.deleteExpose')}</span>
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
							<PaginationBar
								bind:page={proxyPage}
								totalItems={filteredExposures.length}
								pageSize={PAGE_SIZE}
								itemLabel={t('powerdns.tables.proxyItemLabel')}
							/>
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
			<Dialog.Title>{t('powerdns.addZoneModal.title')}</Dialog.Title>
			<Dialog.Description>
				{t('powerdns.addZoneModal.desc')}
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={submitAddZone} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="pdns-new-zone">{t('powerdns.addZoneModal.nameLabel')}</Field.FieldLabel>
					<Input
						id="pdns-new-zone"
						placeholder={t('powerdns.addZoneModal.placeholder')}
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
						{t('powerdns.addZoneModal.createVhost')}
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (addZoneOpen = false)}>{t('powerdns.addZoneModal.cancel')}</Button>
				<Button type="submit" disabled={addingZone}>
					{#if addingZone}
						<Spinner data-icon="inline-start" />
						{t('powerdns.addZoneModal.submitting')}
					{:else}
						{t('powerdns.addZoneModal.submit')}
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
					aria-label={t('powerdns.report.ariaChecking')}
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
			<Button type="button" onclick={() => (reportOpen = false)}>{t('powerdns.report.close')}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Manage DNS Records Dialog -->
<Dialog.Root bind:open={recordsOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-4xl">
		<Dialog.Header class="pr-8">
			<Dialog.Title>{t('powerdns.tables.recordsModalTitle', { zone: currentZone })}</Dialog.Title>
			<Dialog.Description>{t('powerdns.recordsModal.desc')}</Dialog.Description>
		</Dialog.Header>

		<div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain pr-1">
			<form onsubmit={submitAddRecord} class="rounded-lg border p-3.5">
				<Field.FieldGroup class="grid gap-3 sm:grid-cols-2 lg:grid-cols-[minmax(140px,1fr)_100px_minmax(220px,1.8fr)_85px_auto]">
					<Field.Field>
						<Field.FieldLabel for="pdns-rec-name">{t('powerdns.recordsModal.nameLabel')}</Field.FieldLabel>
						<Input id="pdns-rec-name" placeholder={t('powerdns.recordsModal.namePlaceholder')} bind:value={recName} />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="pdns-rec-type">{t('powerdns.recordsModal.typeLabel')}</Field.FieldLabel>
						<Select.Root type="single" bind:value={recType}>
							<Select.Trigger id="pdns-rec-type" class="w-full">{recType}</Select.Trigger>
							<Select.Content>
								<Select.Group>
									{#each ['A', 'CNAME', 'TXT', 'MX', 'AAAA'] as recTypeOpt (recTypeOpt)}
										<Select.Item value={recTypeOpt}>{recTypeOpt}</Select.Item>
									{/each}
								</Select.Group>
							</Select.Content>
						</Select.Root>
					</Field.Field>
					<Field.Field class="sm:col-span-2 lg:col-span-1">
						<Field.FieldLabel for="pdns-rec-content">{t('powerdns.recordsModal.contentLabel')}</Field.FieldLabel>
						<Input id="pdns-rec-content" placeholder={t('powerdns.recordsModal.contentPlaceholder')} bind:value={recContent} required />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="pdns-rec-ttl">{t('powerdns.recordsModal.ttlLabel')}</Field.FieldLabel>
						<Input id="pdns-rec-ttl" inputmode="numeric" pattern="[0-9]+" bind:value={recTTLStr} />
					</Field.Field>
					<div class="flex items-end">
						<Button type="submit" disabled={savingRecord} class="w-full">
							{#if savingRecord}
								<Spinner data-icon="inline-start" />
							{:else}
								<PlusIcon data-icon="inline-start" />
							{/if}
							{t('powerdns.recordsModal.save')}
						</Button>
					</div>
				</Field.FieldGroup>
			</form>

			<div class="flex flex-col sm:overflow-hidden sm:rounded-lg sm:border sm:bg-card">
				<ResponsiveTableWrapper label={t('powerdns.recordsModal.tableAria')} class="rounded-none border-0 bg-transparent">
					<Table.Root class="sm:table-fixed">
						<Table.Header>
							<Table.Row>
								<Table.Head scope="col" class="sm:w-48 lg:w-52">{t('powerdns.recordsModal.colName')}</Table.Head>
								<Table.Head scope="col" class="sm:w-20">{t('powerdns.tables.type')}</Table.Head>
								<Table.Head scope="col" class="sm:w-16">{t('powerdns.tables.ttl')}</Table.Head>
								<Table.Head scope="col">{t('powerdns.tables.content')}</Table.Head>
								<Table.Head scope="col" class="text-right sm:w-32">{t('powerdns.tables.actions')}</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#if recordsLoading}
								<Table.Row>
									<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
										<div class="inline-flex items-center justify-center gap-2">
											<Spinner />
											<span>{t('powerdns.recordsModal.loading')}</span>
										</div>
									</Table.Cell>
								</Table.Row>
							{:else if rrsets.length === 0}
								<Table.Row>
									<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
										{t('powerdns.recordsModal.empty')}
									</Table.Cell>
								</Table.Row>
							{:else}
								{#each paginatedRRSets as rr (rr.name + ':' + rr.type)}
									{@const isProtected = rr.type === 'SOA' || rr.type === 'NS'}
									{@const cleanName = rr.name.replace(/\.$/, '')}
									{@const isEdgeProxy = exposures.some((p) => p.domain === cleanName)}
									{#each rr.records || [] as rec, idx (idx)}
										<Table.Row>
											<Table.Cell data-label={t('powerdns.tables.name')} class="whitespace-normal wrap-anywhere font-semibold">
												<div class="flex flex-wrap items-center gap-1.5">
													<span>{rr.name}</span>
													{#if isEdgeProxy}
														<Badge variant="secondary" class="text-[10px]">{t('powerdns.recordsModal.edgeProxyBadge')}</Badge>
													{/if}
												</div>
											</Table.Cell>
											<Table.Cell data-label={t('powerdns.tables.type')}>
												<Badge variant="outline">{rr.type}</Badge>
											</Table.Cell>
											<Table.Cell data-label={t('powerdns.tables.ttl')} class="tabular-nums text-muted-foreground">
												{rr.ttl}
											</Table.Cell>
											<Table.Cell
												data-label={t('powerdns.tables.content')}
												class="whitespace-normal break-normal wrap-anywhere font-mono text-xs leading-relaxed"
											>
												{rec.content}
											</Table.Cell>
											<Table.Cell data-label={t('powerdns.tables.actions')} class="text-right whitespace-nowrap">
												{#if isProtected}
													<Badge variant="secondary" class="font-normal text-muted-foreground">{t('powerdns.tables.system')}</Badge>
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
																			{t('powerdns.zonesTab.deleting')}
																		{:else}
																			{t('powerdns.tables.actions')}<span class="sr-only"> {rr.type} {rr.name}</span>
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
																		<span>{t('powerdns.tables.deleteRecord')}</span>
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
				<PaginationBar
					bind:page={recordPage}
					totalItems={rrsets.length}
					pageSize={PAGE_SIZE}
					itemLabel={t('powerdns.tables.recordItemLabel')}
					class="rounded-lg border bg-card sm:rounded-none sm:border-0 sm:border-t mt-3 sm:mt-0"
				/>
			</div>
		</div>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (recordsOpen = false)}>{t('powerdns.recordsModal.close')}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Expose to VPS Modal -->
<Dialog.Root bind:open={exposeOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-lg">
		<Dialog.Header class="pr-8">
			<Dialog.Title>{t('powerdns.exposeModal.title')}</Dialog.Title>
			<Dialog.Description>
				{t('powerdns.exposeModal.desc')}
			</Dialog.Description>
		</Dialog.Header>

		<form id="pdns-expose-form" onsubmit={submitExpose} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain pr-1">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldTitle id="pdns-expose-mode-label">{t('powerdns.exposeModal.modeLabel')}</Field.FieldTitle>
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
						<ToggleGroup.Item value="site" class="min-w-[8rem] flex-1">{t('powerdns.exposeModal.modeSite')}</ToggleGroup.Item>
						<ToggleGroup.Item value="port" class="min-w-[8rem] flex-1">{t('powerdns.exposeModal.modePort')}</ToggleGroup.Item>
					</ToggleGroup.Root>
				</Field.Field>
			</Field.FieldGroup>

			{#if exposeMode === 'site'}
				<Field.FieldGroup>
					<Field.Field>
						<Field.FieldLabel for="pdns-expose-site-select">{t('powerdns.exposeModal.selectSiteLabel')}</Field.FieldLabel>
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
								{exposeSiteDomain || t('powerdns.exposeModal.noSitePlaceholder')}
							</Select.Trigger>
							<Select.Content>
								<Select.Group>
									{#each sites as s (s.domain)}
										<Select.Item value={s.domain}>
											{s.domain} {s.exposed_to ? t('powerdns.exposeModal.siteAlreadyExposed', { domain: s.exposed_to }) : t('powerdns.exposeModal.siteNotExposed')}
										</Select.Item>
									{/each}
								</Select.Group>
							</Select.Content>
						</Select.Root>
					</Field.Field>

					<Field.Field>
						<Field.FieldLabel for="pdns-expose-site-pub">{t('powerdns.exposeModal.pubDomainLabel')}</Field.FieldLabel>
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
								{selectedSiteObj.exposed_to ? t('powerdns.exposeModal.alreadyExposedTitle') : t('powerdns.exposeModal.recommendationTitle')}
							</Alert.Title>
							<Alert.Description>
								{#if selectedSiteObj.exposed_to}
									{t('powerdns.exposeModal.alreadyExposedPre')} <strong>{selectedSiteObj.domain}</strong> {t('powerdns.exposeModal.alreadyExposedMid')} <strong>https://{selectedSiteObj.exposed_to}</strong>{t('powerdns.exposeModal.alreadyExposedPost')}
								{:else}
									{t('powerdns.exposeModal.recommendedDomain')} <code>{exposeSitePublic}</code>.
								{/if}
							</Alert.Description>
						</Alert.Root>
					{/if}
				</Field.FieldGroup>
			{:else}
				<Field.FieldGroup>
					<Field.Field>
						<Field.FieldLabel for="pdns-expose-port-pub">{t('powerdns.exposeModal.pubDomainLabel')}</Field.FieldLabel>
						<Input
							id="pdns-expose-port-pub"
							placeholder={`api.${defaultZoneName}`}
							bind:value={exposePortPublic}
							required
						/>
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="pdns-expose-port-target">{t('powerdns.exposeModal.targetPortLabel')}</Field.FieldLabel>
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
						{t('powerdns.exposeModal.sslLabel')}
					</Field.FieldLabel>
				</Field.Field>
				<Field.Field orientation="horizontal">
					<Checkbox id="pdns-expose-ws" bind:checked={exposeWS} />
					<Field.FieldLabel for="pdns-expose-ws" class="font-normal">
						{t('powerdns.exposeModal.wsLabel')}
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>
		</form>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (exposeOpen = false)}>{t('powerdns.exposeModal.cancel')}</Button>
			<Button type="submit" form="pdns-expose-form" disabled={exposing}>
				{#if exposing}
					<Spinner data-icon="inline-start" />
					{t('powerdns.exposeModal.submitting')}
				{:else}
					<CloudUploadIcon data-icon="inline-start" />
					{t('powerdns.exposeModal.submit')}
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Deploy VPS Modal -->
<Dialog.Root bind:open={deployOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-lg">
		<Dialog.Header class="pr-8">
			<Dialog.Title>{t('powerdns.deployModal.title')}</Dialog.Title>
			<Dialog.Description>
				{t('powerdns.deployModal.desc')}
			</Dialog.Description>
		</Dialog.Header>

		<form id="pdns-deploy-form" onsubmit={submitDeployVPS} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain pr-1">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="vps-deploy-host">{t('powerdns.deployModal.hostLabel')}</Field.FieldLabel>
					<Input
						id="vps-deploy-host"
						placeholder={t('powerdns.deployModal.hostPlaceholder')}
						bind:value={deployHost}
						required
					/>
					<Field.FieldDescription>{t('powerdns.deployModal.hostDesc')}</Field.FieldDescription>
				</Field.Field>

				<div class="grid gap-3 sm:grid-cols-2">
					<Field.Field>
						<Field.FieldLabel for="vps-deploy-user">{t('powerdns.deployModal.userLabel')}</Field.FieldLabel>
						<Input id="vps-deploy-user" bind:value={deployUser} required />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="vps-deploy-port">{t('powerdns.deployModal.portLabel')}</Field.FieldLabel>
						<Input id="vps-deploy-port" inputmode="numeric" pattern="[0-9]+" bind:value={deployPortStr} required />
					</Field.Field>
				</div>

				<Field.Field>
					<Field.FieldTitle id="vps-auth-label">{t('powerdns.deployModal.authLabel')}</Field.FieldTitle>
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
						<ToggleGroup.Item value="password" class="min-w-[8rem] flex-1">{t('powerdns.deployModal.authPassword')}</ToggleGroup.Item>
						<ToggleGroup.Item value="key" class="min-w-[8rem] flex-1">{t('powerdns.deployModal.authKey')}</ToggleGroup.Item>
					</ToggleGroup.Root>
				</Field.Field>

				{#if deployAuthMethod === 'password'}
					<Field.Field>
						<Field.FieldLabel for="vps-deploy-password">{t('powerdns.deployModal.passwordLabel')}</Field.FieldLabel>
						<PasswordInput
							id="vps-deploy-password"
							canGenerate={false}
							autocomplete="current-password"
							placeholder={t('powerdns.deployModal.passwordPlaceholder')}
							bind:value={deployPassword}
							required
						/>
					</Field.Field>
				{:else}
					<Field.Field>
						<Field.FieldLabel for="vps-deploy-keypath">{t('powerdns.deployModal.keyPathLabel')}</Field.FieldLabel>
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
			<Button type="button" variant="outline" onclick={() => (deployOpen = false)}>{t('powerdns.deployModal.cancel')}</Button>
			<Button type="submit" form="pdns-deploy-form" disabled={deploying}>
				{#if deploying}
					<Spinner data-icon="inline-start" />
					{t('powerdns.deployModal.submitting')}
				{:else}
					{t('powerdns.deployModal.submit')}
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Connect Manual VPS Modal -->
<Dialog.Root bind:open={connectOpen}>
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-lg">
		<Dialog.Header class="pr-8">
			<Dialog.Title>{t('powerdns.connectModal.title')}</Dialog.Title>
			<Dialog.Description>
				{t('powerdns.connectModal.descPre')} <code>/etc/wdev-edge/edge.conf</code> {t('powerdns.connectModal.descPost')}
			</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={submitConnectVPS} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="vps-conn-netbird-ip">{t('powerdns.connectModal.netbirdIpLabel')}</Field.FieldLabel>
					<Input id="vps-conn-netbird-ip" placeholder="100.68.66.249" bind:value={connNetbirdIP} required />
				</Field.Field>
				<Field.Field>
					<Field.FieldLabel for="vps-conn-public-ip">{t('powerdns.connectModal.publicIpLabel')}</Field.FieldLabel>
					<Input id="vps-conn-public-ip" placeholder="174.128.205.124" bind:value={connPublicIP} />
				</Field.Field>
				<Field.Field>
					<Field.FieldLabel for="vps-conn-api-key">{t('powerdns.connectModal.apiKeyLabel')}</Field.FieldLabel>
					<Input id="vps-conn-api-key" placeholder={t('powerdns.connectModal.apiKeyPlaceholder')} bind:value={connAPIKey} required />
				</Field.Field>
				<div class="grid gap-3 sm:grid-cols-2">
					<Field.Field>
						<Field.FieldLabel for="vps-conn-pdns-port">{t('powerdns.connectModal.pdnsPortLabel')}</Field.FieldLabel>
						<Input id="vps-conn-pdns-port" inputmode="numeric" pattern="[0-9]+" bind:value={connPDNSPort} />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="vps-conn-agent-port">{t('powerdns.connectModal.agentPortLabel')}</Field.FieldLabel>
						<Input id="vps-conn-agent-port" inputmode="numeric" pattern="[0-9]+" bind:value={connAgentPort} />
					</Field.Field>
				</div>
			</Field.FieldGroup>

			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (connectOpen = false)}>{t('powerdns.connectModal.cancel')}</Button>
				<Button type="submit" disabled={connecting}>
					{#if connecting}
						<Spinner data-icon="inline-start" />
						{t('powerdns.connectModal.submitting')}
					{:else}
						{t('powerdns.connectModal.submit')}
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
