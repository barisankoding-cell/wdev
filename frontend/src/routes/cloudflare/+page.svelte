<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
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
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { cn } from '#lib/utils.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PaginationBar from '#lib/components/shared/PaginationBar.svelte';
	import { api, ApiError } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type {
		CFHostCheckResult,
		CFLoginSession,
		CFProfileInfo,
		CFZoneInfo,
		Exposure,
		SiteRecord,
		TunnelStatus
	} from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import CloudUploadIcon from '@lucide/svelte/icons/cloud-upload';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import PlayIcon from '@lucide/svelte/icons/play';
	import SquareIcon from '@lucide/svelte/icons/square';
	import RotateCwIcon from '@lucide/svelte/icons/rotate-cw';
	import FileTextIcon from '@lucide/svelte/icons/file-text';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import InfoIcon from '@lucide/svelte/icons/info';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import PencilIcon from '@lucide/svelte/icons/pencil';

	let cfStatus = $state<TunnelStatus | null>(null);
	let localSites = $state<SiteRecord[]>([]);
	let localDomain = $state('');
	let localDomainMode = $state<'select' | 'custom'>('select');
	let profile = $state('');
	let serviceUrl = $state('');
	let showAdvancedService = $state(false);
	let submitting = $state(false);
	let exposureDeleting = $state<Record<string, boolean>>({});
	let profilePending = $state<Record<string, boolean>>({});

	// Modal Edit Domain Lokal pada Daftar Domain Publik Aktif
	let editExposureModalOpen = $state(false);
	let editTargetExposure = $state<Exposure | null>(null);
	let editLocalDomain = $state('');
	let editLocalDomainMode = $state<'select' | 'custom'>('select');
	let editServiceUrl = $state('');
	let editShowAdvancedService = $state(false);
	let savingExposureEdit = $state(false);

	// Modal Tambah Profil Multi-Akun
	let addModalOpen = $state(false);
	let addTab = $state<'token' | 'login' | 'cert'>('token');

	// Form Token Zero Trust
	let tokenName = $state('');
	let tokenValue = $state('');
	let tokenTunnelName = $state('');
	let tokenApiToken = $state('');
	let tokenAutoStart = $state(true);
	let savingToken = $state(false);

	// Form Browser OAuth Login
	let loginName = $state('');
	let loginTunnelName = $state('');
	let loginApiToken = $state('');
	let loginSession = $state<CFLoginSession | null>(null);
	let startingLogin = $state(false);
	let loginPollTimer: ReturnType<typeof setInterval> | null = null;

	// Form Cert.pem + Credentials JSON
	let certName = $state('');
	let certPEM = $state('');
	let selectedLocalCert = $state('');
	let certCredJSON = $state('');
	let certTunnelName = $state('');
	let certApiToken = $state('');
	let certAutoStart = $state(true);
	let savingCert = $state(false);

	// Modal API Token per Profil
	let apiTokenModalOpen = $state(false);
	let apiTokenTargetProfile = $state<CFProfileInfo | null>(null);
	let apiTokenInput = $state('');
	let savingApiToken = $state(false);

	// Modal Hapus Profil (dengan opsi hapus remote tunnel & DNS CNAME)
	let deleteModalOpen = $state(false);
	let deleteTargetProfile = $state<CFProfileInfo | null>(null);
	let deleteRemote = $state(false);
	let deleteDNS = $state(false);
	let deletingProfile = $state(false);

	// Auto-fetch Domain (Zones) Cloudflare per Profil & Pre-Check Subdomain
	let cfZones = $state<CFZoneInfo[]>([]);
	let zonesLoading = $state(false);
	let zonesError = $state('');
	let selectedZoneName = $state('');
	let zoneSubdomain = $state('');
	let allowOverwrite = $state(false);
	let hostCheck = $state<CFHostCheckResult | null>(null);
	let hostChecking = $state(false);
	let lastZoneKey = '';
	let hostCheckTimer: ReturnType<typeof setTimeout> | null = null;
	let hostCheckSeq = 0;

	const dnsLabelRe = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

	function normalizeSubdomain(raw: string, zoneName: string): string {
		let val = raw.trim().toLowerCase();
		val = val.replace(/^https?:\/\//, '').split('/')[0] ?? '';
		val = val.replace(/\s+/g, '').replace(/^\.+|\.+$/g, '');
		const z = zoneName.trim().toLowerCase();
		if (z) {
			if (val === z) return '';
			if (val.endsWith('.' + z)) {
				val = val.slice(0, -(z.length + 1)).replace(/\.+$/g, '');
			}
		}
		return val;
	}

	const normalizedSubdomain = $derived(normalizeSubdomain(zoneSubdomain, selectedZoneName));

	const subdomainError = $derived.by(() => {
		const raw = zoneSubdomain.trim();
		if (!raw) return '';
		const sub = normalizedSubdomain;
		if (!sub) return '';
		if (sub.includes('..')) {
			return t('cloudflare.validation.subDoubleDot');
		}
		const labels = sub.split('.');
		for (const lbl of labels) {
			if (!dnsLabelRe.test(lbl)) {
				return t('cloudflare.validation.subInvalidChars');
			}
		}
		const z = selectedZoneName.trim().toLowerCase();
		if (z && `${sub}.${z}`.length > 253) {
			return t('cloudflare.validation.subTooLong');
		}
		return '';
	});

	const publicHost = $derived.by(() => {
		const cleanZone = selectedZoneName.trim().toLowerCase();
		if (!cleanZone || subdomainError) return '';
		return normalizedSubdomain ? `${normalizedSubdomain}.${cleanZone}` : cleanZone;
	});

	// Modal Log Profil
	let logsModalOpen = $state(false);
	let logsProfileName = $state('');
	let logsText = $state('');
	let logsLoading = $state(false);

	// Pagination (Max 10 data per page)
	const PAGE_SIZE = 10;
	let exposurePage = $state(1);

	const profileDetails = $derived<CFProfileInfo[]>(cfStatus?.profile_details ?? []);
	const apiProfiles = $derived(profileDetails.filter((p) => p.has_api_token));
	const selectedProfileInfo = $derived(apiProfiles.find((p) => p.name === profile) ?? null);
	const canDeleteRemote = $derived(
		Boolean(
			deleteTargetProfile &&
				(deleteTargetProfile.auth_mode !== 'token'
					? deleteTargetProfile.has_cert ||
						(deleteTargetProfile.has_api_token &&
							deleteTargetProfile.account_tag &&
							deleteTargetProfile.tunnel_id)
					: deleteTargetProfile.has_api_token &&
						deleteTargetProfile.account_tag &&
						deleteTargetProfile.tunnel_id)
		)
	);
	const canDeleteDNS = $derived(
		Boolean(deleteTargetProfile && deleteTargetProfile.has_api_token && deleteTargetProfile.route_count > 0)
	);
	const allExposures = $derived(cfStatus?.exposures || []);
	const paginatedExposures = $derived(
		allExposures.slice((exposurePage - 1) * PAGE_SIZE, exposurePage * PAGE_SIZE)
	);
	const exposureByLocalDomain = $derived.by(() => {
		const map: Record<string, Exposure> = {};
		for (const exp of allExposures) {
			map[exp.local_domain.toLowerCase()] = exp;
		}
		return map;
	});
	const editProfileInfo = $derived(
		editTargetExposure
			? (profileDetails.find((p) => p.name === editTargetExposure?.profile) ?? null)
			: null
	);
	const editMissingApiToken = $derived(
		Boolean(editProfileInfo && editProfileInfo.auth_mode === 'token' && !editProfileInfo.has_api_token)
	);
	const editNormalizedLocal = $derived(editLocalDomain.trim().toLowerCase());
	const editReassignedLocalConflict = $derived.by(() => {
		if (!editTargetExposure || !editNormalizedLocal) return null;
		const curHost = editTargetExposure.public_host.toLowerCase();
		return (
			allExposures.find(
				(e) =>
					e.local_domain.toLowerCase() === editNormalizedLocal &&
					e.public_host.toLowerCase() !== curHost
			) ?? null
		);
	});

	$effect(() => {
		if (allExposures.length === 0) {
			exposurePage = 1;
		}
	});

	$effect(() => {
		if (apiProfiles.length === 0) {
			if (profile !== '') profile = '';
			return;
		}
		if (!apiProfiles.some((p) => p.name === profile)) {
			profile = apiProfiles[0].name;
		}
	});

	$effect(() => {
		const info = selectedProfileInfo;
		const key = info ? `${info.name}:${info.has_api_token ? '1' : '0'}` : '';
		if (key === lastZoneKey) return;
		lastZoneKey = key;
		if (info?.has_api_token) {
			void loadProfileZones(info.name);
		} else {
			cfZones = [];
			zonesError = '';
			selectedZoneName = '';
		}
	});

	function stopLoginPoll() {
		if (loginPollTimer) {
			clearInterval(loginPollTimer);
			loginPollTimer = null;
		}
	}

	onDestroy(() => {
		stopLoginPoll();
		if (hostCheckTimer) {
			clearTimeout(hostCheckTimer);
			hostCheckTimer = null;
		}
	});

	$effect(() => {
		const profName = selectedProfileInfo?.name ?? '';
		const host = publicHost;
		const local = localDomain.trim().toLowerCase();
		const invalidSub = Boolean(subdomainError);

		allowOverwrite = false;
		if (hostCheckTimer) {
			clearTimeout(hostCheckTimer);
			hostCheckTimer = null;
		}
		if (!profName || !host || invalidSub) {
			hostCheckSeq += 1;
			hostCheck = null;
			hostChecking = false;
			return;
		}

		const seq = ++hostCheckSeq;
		hostChecking = true;
		hostCheckTimer = setTimeout(async () => {
			try {
				const res = await api.checkCFHost(profName, host, local);
				if (seq !== hostCheckSeq) return;
				hostCheck = res;
			} catch (err) {
				if (seq !== hostCheckSeq) return;
				hostCheck = {
					profile: profName,
					public_host: host,
					local_domain: local,
					available: true,
					check_error: err instanceof Error ? err.message : t('cloudflare.validation.checkErrorFallback')
				};
			} finally {
				if (seq === hostCheckSeq) {
					hostChecking = false;
				}
			}
		}, 300);
	});

	async function loadCloudflare() {
		try {
			const [st, sites] = await Promise.all([
				api.getCFStatus(),
				api.listSites().catch(() => [] as SiteRecord[])
			]);
			cfStatus = st;
			localSites = sites ?? [];
			if (localSites.length === 0) {
				localDomainMode = 'custom';
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('cloudflare.toasts.statusLoadError'));
		}
	}

	onMount(loadCloudflare);

	$effect(() => {
		if (cp.refreshTick > 0) void loadCloudflare();
	});

	async function loadProfileZones(profName: string) {
		if (!profName) return;
		zonesLoading = true;
		zonesError = '';
		try {
			const res = await api.listCFProfileZones(profName);
			if (selectedProfileInfo?.name !== profName) return;
			cfZones = res.zones ?? [];
			if (cfZones.length > 0) {
				if (!cfZones.some((z) => z.name === selectedZoneName)) {
					selectedZoneName = cfZones[0].name;
				}
			} else {
				selectedZoneName = '';
			}
		} catch (err) {
			if (selectedProfileInfo?.name !== profName) return;
			cfZones = [];
			selectedZoneName = '';
			zonesError = err instanceof ApiError ? err.message : t('cloudflare.toasts.zonesLoadError');
		} finally {
			zonesLoading = false;
		}
	}

	function openAddProfileModal(tab: 'token' | 'login' | 'cert' = 'token') {
		addTab = tab;
		tokenName = '';
		loginName = '';
		certName = '';
		tokenApiToken = '';
		loginApiToken = '';
		certApiToken = '';
		selectedLocalCert = cfStatus?.detected_local_certs?.[0] || '';
		addModalOpen = true;
	}

	function openApiTokenModal(prof: CFProfileInfo) {
		apiTokenTargetProfile = prof;
		apiTokenInput = '';
		apiTokenModalOpen = true;
	}

	async function submitProfileApiToken(e: SubmitEvent) {
		e.preventDefault();
		if (!apiTokenTargetProfile) return;
		if (!apiTokenInput.trim()) {
			toast.warning(t('cloudflare.toasts.enterApiToken'));
			return;
		}
		savingApiToken = true;
		try {
			const targetName = apiTokenTargetProfile.name;
			await api.setCFProfileAPIToken(targetName, apiTokenInput.trim());
			toast.success(t('cloudflare.toasts.apiTokenSaved', { name: targetName }));
			apiTokenModalOpen = false;
			apiTokenInput = '';
			lastZoneKey = '';
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : t('cloudflare.toasts.apiTokenSaveError'));
		} finally {
			savingApiToken = false;
		}
	}

	async function clearProfileApiToken() {
		if (!apiTokenTargetProfile) return;
		savingApiToken = true;
		try {
			const targetName = apiTokenTargetProfile.name;
			await api.setCFProfileAPIToken(targetName, '');
			toast.success(t('cloudflare.toasts.apiTokenCleared', { name: targetName }));
			apiTokenModalOpen = false;
			apiTokenInput = '';
			lastZoneKey = '';
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : t('cloudflare.toasts.apiTokenClearError'));
		} finally {
			savingApiToken = false;
		}
	}

	async function copyText(val: string, label: string) {
		if (!val) return;
		try {
			await navigator.clipboard.writeText(val);
			toast.success(t('cloudflare.toasts.copied', { label }));
		} catch {
			toast.error(t('cloudflare.toasts.copyFailed', { label }));
		}
	}

	async function submitTokenProfile(e: SubmitEvent) {
		e.preventDefault();
		if (!tokenValue.trim()) {
			toast.warning(t('cloudflare.toasts.tunnelTokenRequired'));
			return;
		}
		savingToken = true;
		try {
			const res = await api.registerCFTokenProfile({
				name: tokenName.trim() || undefined,
				token: tokenValue.trim(),
				tunnel_name: tokenTunnelName.trim() || undefined,
				api_token: tokenApiToken.trim() || undefined,
				auto_start: tokenAutoStart
			});
			toast.success(t('cloudflare.toasts.tokenRegistered', { name: res.profile.name }));
			tokenName = '';
			tokenValue = '';
			tokenTunnelName = '';
			tokenApiToken = '';
			addModalOpen = false;
			profile = res.profile.name;
			lastZoneKey = '';
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : t('cloudflare.toasts.tokenRegisterError'));
		} finally {
			savingToken = false;
		}
	}

	async function startBrowserLoginFlow(e: SubmitEvent) {
		e.preventDefault();
		stopLoginPoll();
		startingLogin = true;
		try {
			const sess = await api.startCFLogin(
				loginName.trim(),
				loginTunnelName.trim(),
				loginApiToken.trim()
			);
			loginSession = sess;
			if (sess.status === 'completed') {
				const resolvedName = sess.result?.name || sess.profile;
				toast.success(sess.message || t('cloudflare.toasts.profileRegistered', { name: resolvedName }));
				profile = resolvedName;
				addModalOpen = false;
				loginSession = null;
				loginName = '';
				loginTunnelName = '';
				loginApiToken = '';
				lastZoneKey = '';
				await loadCloudflare();
			} else if (sess.status === 'waiting_browser' || sess.status === 'starting') {
				loginPollTimer = setInterval(async () => {
					if (!loginSession?.session_id) {
						stopLoginPoll();
						return;
					}
					try {
						const cur = await api.getCFLoginStatus(loginSession.session_id);
						loginSession = cur;
						if (cur.status === 'completed') {
							stopLoginPoll();
							const resolvedName = cur.result?.name || cur.profile;
							toast.success(cur.message || t('cloudflare.toasts.profileRegistered', { name: resolvedName }));
							profile = resolvedName;
							addModalOpen = false;
							loginSession = null;
							loginName = '';
							loginTunnelName = '';
							loginApiToken = '';
							lastZoneKey = '';
							await loadCloudflare();
						} else if (cur.status === 'failed' || cur.status === 'cancelled') {
							stopLoginPoll();
						}
					} catch {
						// biarkan polling mencoba kembali sementara sesi aktif
					}
				}, 1800);
			}
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : t('cloudflare.toasts.browserLoginError'));
		} finally {
			startingLogin = false;
		}
	}

	async function cancelLoginFlow() {
		stopLoginPoll();
		if (loginSession?.session_id) {
			await api.cancelCFLogin(loginSession.session_id).catch(() => {});
		}
		loginSession = null;
	}

	async function submitCertProfile(e: SubmitEvent) {
		e.preventDefault();
		if (!certPEM.trim() && !selectedLocalCert && !certCredJSON.trim()) {
			toast.warning(t('cloudflare.toasts.certInputRequired'));
			return;
		}
		savingCert = true;
		try {
			const res = await api.registerCFCertProfile({
				name: certName.trim() || undefined,
				cert_pem: certPEM.trim() || undefined,
				local_cert_path: !certPEM.trim() && selectedLocalCert ? selectedLocalCert : undefined,
				credentials_json: certCredJSON.trim() || undefined,
				tunnel_name: certTunnelName.trim() || undefined,
				api_token: certApiToken.trim() || undefined,
				auto_start: certAutoStart
			});
			toast.success(t('cloudflare.toasts.certRegistered', { name: res.profile.name }));
			certName = '';
			certPEM = '';
			certCredJSON = '';
			certTunnelName = '';
			certApiToken = '';
			addModalOpen = false;
			profile = res.profile.name;
			lastZoneKey = '';
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : t('cloudflare.toasts.certRegisterError'));
		} finally {
			savingCert = false;
		}
	}

	async function controlProfile(name: string, action: 'start' | 'stop' | 'restart') {
		const successKeys = {
			start: 'cloudflare.toasts.controlSuccess.start',
			stop: 'cloudflare.toasts.controlSuccess.stop',
			restart: 'cloudflare.toasts.controlSuccess.restart'
		} as const;
		const errorKeys = {
			start: 'cloudflare.toasts.controlError.start',
			stop: 'cloudflare.toasts.controlError.stop',
			restart: 'cloudflare.toasts.controlError.restart'
		} as const;
		profilePending = { ...profilePending, [name]: true };
		try {
			await api.controlCFProfile(name, action);
			toast.success(t(successKeys[action], { name }));
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : t(errorKeys[action], { name }));
		} finally {
			const next = { ...profilePending };
			delete next[name];
			profilePending = next;
		}
	}

	function openDeleteProfileModal(prof: CFProfileInfo) {
		deleteTargetProfile = prof;
		deleteRemote = false;
		deleteDNS = false;
		deleteModalOpen = true;
	}

	async function confirmDeleteProfile(e: SubmitEvent) {
		e.preventDefault();
		if (!deleteTargetProfile) return;
		const name = deleteTargetProfile.name;
		deletingProfile = true;
		profilePending = { ...profilePending, [name]: true };
		try {
			const res = await api.deleteCFProfile(name, {
				delete_remote: deleteRemote,
				delete_dns: deleteDNS
			});
			const details: string[] = [];
			if (res.remote_deleted) details.push(t('cloudflare.toasts.remoteTunnelDeleted'));
			if ((res.dns_deleted_count ?? 0) > 0) {
				details.push(t('cloudflare.toasts.dnsRecordsDeleted', { count: res.dns_deleted_count ?? 0 }));
			}
			toast.success(t('cloudflare.toasts.profileDeleted', { name }), {
				description: details.length > 0 ? details.join(' · ') : undefined
			});
			if (res.warnings && res.warnings.length > 0) {
				toast.warning(t('cloudflare.toasts.deleteWarnings'), { description: res.warnings.join(' | ') });
			}
			deleteModalOpen = false;
			deleteTargetProfile = null;
			if (profile === name) {
				profile = '';
				lastZoneKey = '';
			}
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : t('cloudflare.toasts.deleteProfileError', { name }));
		} finally {
			deletingProfile = false;
			const next = { ...profilePending };
			delete next[name];
			profilePending = next;
		}
	}

	async function openProfileLogs(name: string) {
		logsProfileName = name;
		logsText = '';
		logsModalOpen = true;
		await refreshProfileLogs();
	}

	async function refreshProfileLogs() {
		if (!logsProfileName) return;
		logsLoading = true;
		try {
			const res = await api.getCFProfileLogs(logsProfileName, 60);
			logsText = res.logs || t('cloudflare.toasts.logsEmpty');
		} catch (err) {
			logsText = err instanceof ApiError ? err.message : t('cloudflare.toasts.logsError');
		} finally {
			logsLoading = false;
		}
	}

	async function submitExpose(e: SubmitEvent) {
		e.preventDefault();
		if (subdomainError) {
			toast.warning(subdomainError);
			return;
		}
		if (!localDomain.trim() || !publicHost.trim()) {
			toast.warning(t('cloudflare.toasts.domainsRequired'));
			return;
		}
		if (hostCheck && !hostCheck.available && !allowOverwrite) {
			toast.warning(hostCheck.message || t('cloudflare.toasts.subdomainTaken'));
			return;
		}
		submitting = true;
		try {
			const res = await api.exposeCF(
				localDomain.trim(),
				publicHost.trim(),
				profile.trim(),
				serviceUrl.trim(),
				undefined,
				allowOverwrite
			);
			toast.success(
				t('cloudflare.toasts.exposeSuccess', { local: localDomain.trim(), public: res.public_host }),
				{
					description: t('cloudflare.toasts.exposeSuccessDesc', {
						profile: res.profile,
						origin: res.service_url
					})
				}
			);
			if (res.dns_warning) {
				toast.warning(t('cloudflare.toasts.cfDnsNote'), { description: res.dns_warning });
			}
			localDomain = '';
			localDomainMode = localSites.length > 0 ? 'select' : 'custom';
			zoneSubdomain = '';
			allowOverwrite = false;
			hostCheck = null;
			serviceUrl = '';
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('cloudflare.toasts.exposeError'));
		} finally {
			submitting = false;
		}
	}

	async function unexpose(expProfile: string, domain: string) {
		const ok = await cp.confirm({
			title: t('cloudflare.confirm.unexposeTitle'),
			description: t('cloudflare.confirm.unexposeDesc', { domain, profile: expProfile }),
			confirmText: t('cloudflare.confirm.unexposeBtn'),
			destructive: true
		});
		if (!ok) return;
		exposureDeleting = { ...exposureDeleting, [domain]: true };
		try {
			const res = await api.unexposeCF(domain, expProfile);
			toast.success(t('cloudflare.toasts.unexposeSuccess', { domain }), {
				description: res.dns_deleted
					? t('cloudflare.toasts.unexposeDnsCleaned', { host: res.public_host || '' })
					: undefined
			});
			if (res.dns_warning) {
				toast.warning(t('cloudflare.toasts.cfDnsNote'), { description: res.dns_warning });
			}
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('cloudflare.toasts.unexposeError'));
		} finally {
			const next = { ...exposureDeleting };
			delete next[domain];
			exposureDeleting = next;
		}
	}

	function isDefaultNginxServiceUrl(url?: string): boolean {
		const clean = (url || '').trim().toLowerCase();
		return !clean || clean === 'http://127.0.0.1:80' || clean === 'https://127.0.0.1:443';
	}

	function openEditExposureModal(exp: Exposure) {
		editTargetExposure = exp;
		const hasInList = localSites.some(
			(st) => st.domain.toLowerCase() === exp.local_domain.toLowerCase()
		);
		editLocalDomain = exp.local_domain;
		editLocalDomainMode = localSites.length > 0 && hasInList ? 'select' : 'custom';
		if (isDefaultNginxServiceUrl(exp.service_url)) {
			editServiceUrl = '';
			editShowAdvancedService = false;
		} else {
			editServiceUrl = exp.service_url || '';
			editShowAdvancedService = true;
		}
		editExposureModalOpen = true;
	}

	async function submitExposureEdit(e: SubmitEvent) {
		e.preventDefault();
		if (!editTargetExposure) return;
		const nextLocal = editLocalDomain.trim().toLowerCase();
		if (!nextLocal) {
			toast.warning(t('cloudflare.toasts.selectEditLocalRequired'));
			return;
		}
		if (editMissingApiToken) {
			toast.warning(t('cloudflare.toasts.editNeedsApiToken'));
			return;
		}
		savingExposureEdit = true;
		try {
			const res = await api.exposeCF(
				nextLocal,
				editTargetExposure.public_host,
				editTargetExposure.profile,
				editServiceUrl.trim(),
				undefined,
				true
			);
			toast.success(t('cloudflare.toasts.editSuccess', { public: res.public_host, local: nextLocal }), {
				description: t('cloudflare.toasts.exposeSuccessDesc', {
					profile: res.profile,
					origin: res.service_url
				})
			});
			if (res.dns_warning) {
				toast.warning(t('cloudflare.toasts.cfDnsNote'), { description: res.dns_warning });
			}
			editExposureModalOpen = false;
			editTargetExposure = null;
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('cloudflare.toasts.editError'));
		} finally {
			savingExposureEdit = false;
		}
	}
</script>

{#snippet statusBadgeTooltip(
	tone: 'warning' | 'success',
	badgeLabel: string,
	tooltipText: string,
	records?: { id?: string; type: string; content: string }[]
)}
	<Tooltip.Root>
		<Tooltip.Trigger>
			{#snippet child({ props })}
				<button
					type="button"
					{...props}
					class="inline-flex cursor-help rounded-4xl focus-visible:outline-none"
				>
					<Badge
						variant="outline"
						class={tone === 'warning'
							? 'border-warning/40 bg-warning/10 text-warning'
							: 'border-success/40 bg-success/10 text-success'}
					>
						{#if tone === 'warning'}
							<AlertCircleIcon data-icon="inline-start" />
						{:else}
							<ShieldCheckIcon data-icon="inline-start" />
						{/if}
						{badgeLabel}
					</Badge>
				</button>
			{/snippet}
		</Tooltip.Trigger>
		<Tooltip.Content class="z-[60] max-w-xs">
			<div class="flex flex-col gap-1 py-0.5 text-left leading-relaxed">
				<p>{tooltipText}</p>
				{#if records && records.length > 0}
					<p class="font-mono text-[11px] opacity-85">
						{records.map((r) => `${r.type} → ${r.content}`).join(' · ')}
					</p>
				{/if}
			</div>
		</Tooltip.Content>
	</Tooltip.Root>
{/snippet}

<div class="flex flex-col gap-5 sm:gap-6">
	<!-- Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('cloudflare.title')}</h1>
			<p class="text-sm text-muted-foreground">
				{t('cloudflare.descPre')} <code class="font-mono">cert.pem</code>{t('cloudflare.descPost')}
			</p>
		</div>
		<div class="flex flex-wrap items-center gap-2">
			{#if cfStatus}
				<StatusBadge
					tone={cfStatus.active ? 'success' : 'neutral'}
					label={cfStatus.active ? t('cloudflare.badges.tunnelRunning') : t('cloudflare.badges.tunnelInactive')}
				/>
			{/if}
			{#if cfStatus && !cfStatus.installed}
				<Button variant="default" size="sm" onclick={() => cp.triggerInstall('cloudflare')}>
					{t('cloudflare.buttons.installCloudflared')}
				</Button>
			{/if}
			<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('cf-doctor')}>
				<ActivityIcon data-icon="inline-start" />
				{t('cloudflare.buttons.doctorDiagnostics')}
			</Button>
			<Button size="sm" onclick={() => openAddProfileModal('token')}>
				<PlusIcon data-icon="inline-start" />
				{t('cloudflare.buttons.addProfile')}
			</Button>
		</div>
	</div>

	<!-- Kartu Multi-Profil Akun -->
	<Card.Root>
		<Card.Header class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<Card.Title class="text-base">{t('cloudflare.profilesCard.title', { count: profileDetails.length })}</Card.Title>
				<Card.Description class="break-all">
					{#if cfStatus?.binary}
						{t('cloudflare.profilesCard.binaryLabel')} <span class="font-mono">{cfStatus.binary}</span> · {t('cloudflare.profilesCard.systemdTemplate')}
						<span class="font-mono">{cfStatus.service_unit_ready ? t('cloudflare.profilesCard.systemdReady') : t('cloudflare.profilesCard.systemdNotInstalled')}</span>
					{:else}
						{t('cloudflare.profilesCard.noBinary')}
					{/if}
				</Card.Description>
			</div>
			{#if profileDetails.length > 0}
				<div class="flex flex-wrap gap-1.5">
					<Button variant="outline" size="xs" onclick={() => openAddProfileModal('token')}>
						<KeyRoundIcon data-icon="inline-start" />
						{t('cloudflare.buttons.addTokenShort')}
					</Button>
					<Button variant="outline" size="xs" onclick={() => openAddProfileModal('login')}>
						<GlobeIcon data-icon="inline-start" />
						{t('cloudflare.buttons.addBrowserShort')}
					</Button>
				</div>
			{/if}
		</Card.Header>
		<Card.Content>
			{#if profileDetails.length === 0}
				<Empty.Root class="border">
					<Empty.Header>
						<Empty.Media variant="icon">
							<GlobeIcon />
						</Empty.Media>
						<Empty.Title>{t('cloudflare.profilesCard.emptyTitle')}</Empty.Title>
						<Empty.Description>
							{t('cloudflare.profilesCard.emptyDesc')}
						</Empty.Description>
					</Empty.Header>
					<Empty.Content>
						<div class="flex flex-wrap justify-center gap-2">
							<Button size="sm" onclick={() => openAddProfileModal('token')}>
								<KeyRoundIcon data-icon="inline-start" />
								{t('cloudflare.profilesCard.emptyBtnToken')}
							</Button>
							<Button variant="outline" size="sm" onclick={() => openAddProfileModal('login')}>
								<GlobeIcon data-icon="inline-start" />
								{t('cloudflare.profilesCard.emptyBtnBrowser')}
							</Button>
							<Button variant="outline" size="sm" onclick={() => openAddProfileModal('cert')}>
								<ShieldCheckIcon data-icon="inline-start" />
								{t('cloudflare.profilesCard.emptyBtnCert')}
							</Button>
						</div>
					</Empty.Content>
				</Empty.Root>
			{:else}
				<ResponsiveTableWrapper label={t('cloudflare.profilesCard.tableAria')} class="rounded-md border">
					<Table.Root>
						<Table.Header>
							<Table.Row>
								<Table.Head>{t('cloudflare.profilesCard.colProfile')}</Table.Head>
								<Table.Head>{t('cloudflare.profilesCard.colAuthMode')}</Table.Head>
								<Table.Head>{t('cloudflare.profilesCard.colTunnelId')}</Table.Head>
								<Table.Head>{t('cloudflare.profilesCard.colStatus')}</Table.Head>
								<Table.Head>{t('cloudflare.profilesCard.colRoutes')}</Table.Head>
								<Table.Head class="text-right">{t('cloudflare.profilesCard.colControls')}</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each profileDetails as prof (prof.name)}
								<Table.Row>
									<Table.Cell data-label={t('cloudflare.profilesCard.colProfile')} class="font-semibold">
										<div class="font-mono text-sm">{prof.name}</div>
										<div class="font-mono text-[11px] text-muted-foreground">{prof.service_unit}</div>
									</Table.Cell>
									<Table.Cell data-label={t('cloudflare.profilesCard.colModeLabel')}>
										<div class="flex flex-wrap items-center gap-1.5">
											{#if prof.auth_mode === 'token'}
												<Badge variant="outline" class="border-info/40 bg-info/10 text-info">
													{t('cloudflare.badges.tokenZeroTrust')}
												</Badge>
											{:else}
												<Badge variant="outline" class="border-primary/40 bg-primary/10 text-primary">
													{t('cloudflare.badges.localCertConfig')}
												</Badge>
											{/if}
											{#if prof.has_api_token}
												<Badge variant="outline" class="border-success/40 bg-success/10 text-success">
													{t('cloudflare.badges.apiToken')}
												</Badge>
											{/if}
										</div>
									</Table.Cell>
									<Table.Cell data-label={t('cloudflare.profilesCard.colTunnelIdShort')}>
										<div class="font-medium text-xs">{prof.tunnel_name || '-'}</div>
										{#if prof.tunnel_id}
											<button
												type="button"
												class="mt-0.5 inline-flex items-center gap-1 font-mono text-[11px] text-muted-foreground hover:text-foreground"
												onclick={() => copyText(prof.tunnel_id || '', t('cloudflare.copyLabels.tunnelId'))}
												title={t('cloudflare.profilesCard.copyUuidTitle')}
											>
												<span class="truncate">{prof.tunnel_id}</span>
												<CopyIcon class="size-3" />
											</button>
										{/if}
									</Table.Cell>
									<Table.Cell data-label={t('cloudflare.profilesCard.colStatusShort')}>
										<StatusBadge
											tone={prof.active ? 'success' : 'neutral'}
											label={prof.active ? t('cloudflare.badges.activeRunning') : t('cloudflare.badges.inactive')}
										/>
									</Table.Cell>
									<Table.Cell data-label={t('cloudflare.profilesCard.colRoutes')} class="font-mono text-xs tabular-nums">
										{#if prof.auth_mode === 'token' && !prof.has_api_token}
											<span class="text-muted-foreground" title={t('cloudflare.profilesCard.dashboardCfTitle')}>{t('cloudflare.profilesCard.dashboardCf')}</span>
										{:else if prof.auth_mode === 'token'}
											<span>{t('cloudflare.profilesCard.routesApiCount', { count: prof.route_count })}</span>
										{:else}
											{t('cloudflare.profilesCard.routesCount', { count: prof.route_count })}
										{/if}
									</Table.Cell>
									<Table.Cell data-label={t('cloudflare.profilesCard.colControls')} class="text-right">
										<div class="flex justify-end gap-1">
											{#if prof.active}
												<Button
													variant="outline"
													size="icon-xs"
													disabled={Boolean(profilePending[prof.name])}
													onclick={() => controlProfile(prof.name, 'restart')}
													title={t('cloudflare.profilesCard.restartTitle')}
													aria-label={t('cloudflare.profilesCard.restartTitle')}
												>
													<RotateCwIcon />
												</Button>
												<Button
													variant="outline"
													size="icon-xs"
													disabled={Boolean(profilePending[prof.name])}
													onclick={() => controlProfile(prof.name, 'stop')}
													title={t('cloudflare.profilesCard.stopTitle')}
													aria-label={t('cloudflare.profilesCard.stopTitle')}
												>
													<SquareIcon />
												</Button>
											{:else}
												<Button
													variant="outline"
													size="icon-xs"
													disabled={Boolean(profilePending[prof.name])}
													onclick={() => controlProfile(prof.name, 'start')}
													title={t('cloudflare.profilesCard.startTitle')}
													aria-label={t('cloudflare.profilesCard.startTitle')}
												>
													<PlayIcon class="text-success" />
												</Button>
											{/if}
											<Button
												variant="outline"
												size="icon-xs"
												onclick={() => openApiTokenModal(prof)}
												title={prof.has_api_token ? t('cloudflare.profilesCard.apiTokenManageInstalled') : t('cloudflare.profilesCard.apiTokenManageAttach')}
												aria-label={t('cloudflare.profilesCard.apiTokenManageAria')}
											>
												<KeyRoundIcon class={cn(prof.has_api_token && 'text-success')} />
											</Button>
											<Button
												variant="outline"
												size="icon-xs"
												onclick={() => openProfileLogs(prof.name)}
												title={t('cloudflare.profilesCard.viewLogsTitle')}
												aria-label={t('cloudflare.profilesCard.viewLogsTitle')}
											>
												<FileTextIcon />
											</Button>
											<Button
												variant="outline"
												size="icon-xs"
												disabled={Boolean(profilePending[prof.name])}
												onclick={() => openDeleteProfileModal(prof)}
												title={t('cloudflare.profilesCard.deleteProfileTitle')}
												aria-label={t('cloudflare.profilesCard.deleteProfileTitle')}
											>
												<Trash2Icon class="text-destructive" />
											</Button>
										</div>
									</Table.Cell>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				</ResponsiveTableWrapper>
			{/if}
		</Card.Content>
	</Card.Root>

	<!-- Form Ekspos Vhost Lokal (Khusus Profil yang Memiliki Domain + Cloudflare API Token) -->
	<Card.Root>
		<Card.Header>
			<Card.Title class="text-base">{t('cloudflare.exposeCard.title')}</Card.Title>
			<Card.Description>
				{t('cloudflare.exposeCard.desc')}
			</Card.Description>
		</Card.Header>
		<Card.Content class="flex flex-col gap-4">
			{#if apiProfiles.length === 0}
				<Alert.Root class="border-warning/40 bg-warning/10">
					<InfoIcon class="text-warning" />
					<Alert.Title class="text-warning">
						{t('cloudflare.exposeCard.noApiTitle')}
					</Alert.Title>
					<Alert.Description class="flex flex-col gap-2.5 text-xs leading-relaxed text-muted-foreground">
						<p>
							{t('cloudflare.exposeCard.noApiPre')} <strong>{t('cloudflare.exposeCard.noApiBold')}</strong> {t('cloudflare.exposeCard.noApiPost')}
						</p>
						<div class="flex flex-wrap gap-2">
							{#if profileDetails.length > 0}
								{#each profileDetails as p (p.name)}
									<Button type="button" variant="outline" size="xs" onclick={() => openApiTokenModal(p)}>
										<KeyRoundIcon data-icon="inline-start" />
										{t('cloudflare.exposeCard.attachApiBtn', { name: p.name })}
									</Button>
								{/each}
							{:else}
								<Button type="button" size="xs" onclick={() => openAddProfileModal('login')}>
									<PlusIcon data-icon="inline-start" />
									{t('cloudflare.exposeCard.registerWithApiBtn')}
								</Button>
							{/if}
						</div>
					</Alert.Description>
				</Alert.Root>
			{:else}
				{#if selectedProfileInfo?.auth_mode === 'token'}
					<Alert.Root class="border-info/35 bg-info/5">
						<InfoIcon class="text-info" />
						<Alert.Title class="text-info">{t('cloudflare.exposeCard.tokenApiTitle', { name: selectedProfileInfo.name })}</Alert.Title>
						<Alert.Description class="text-xs leading-relaxed text-muted-foreground">
							{t('cloudflare.exposeCard.tokenApiDescPre')}<code class="font-mono">cfd_tunnel/.../configurations</code>{t('cloudflare.exposeCard.tokenApiDescMid')} <code class="font-mono">Cloudflare Tunnel:Edit</code>, <code class="font-mono">Zone:Read</code>, {t('cloudflare.exposeCard.tokenApiDescAnd')} <code class="font-mono">DNS:Edit</code>{t('cloudflare.exposeCard.tokenApiDescEnd')}
						</Alert.Description>
					</Alert.Root>
				{/if}

				<form onsubmit={submitExpose} class="flex flex-col gap-4">
					<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
						<Field.Field>
							<Field.FieldLabel for="cf-profile-select">{t('cloudflare.exposeCard.profileLabel')}</Field.FieldLabel>
							<Select.Root type="single" bind:value={profile}>
								<Select.Trigger id="cf-profile-select" class="w-full">
									<span class="truncate">
										{selectedProfileInfo
											? `${selectedProfileInfo.name} (${selectedProfileInfo.auth_mode === 'token' ? t('cloudflare.exposeCard.modeTokenApi') : t('cloudflare.exposeCard.modeCertApi')})`
											: t('cloudflare.exposeCard.profilePlaceholder')}
									</span>
								</Select.Trigger>
								<Select.Content>
									<Select.Group>
										<Select.Label>{t('cloudflare.exposeCard.profileGroupLabel')}</Select.Label>
										{#each apiProfiles as p (p.name)}
											<Select.Item value={p.name}>
												{p.name} ({p.auth_mode === 'token' ? t('cloudflare.exposeCard.modeTokenApi') : t('cloudflare.exposeCard.modeCertApi')})
											</Select.Item>
										{/each}
									</Select.Group>
								</Select.Content>
							</Select.Root>
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-local-domain">{t('cloudflare.exposeCard.localDomainLabel')}</Field.FieldLabel>
							{#if localSites.length > 0 && localDomainMode === 'select'}
								<Select.Root
									type="single"
									value={localDomain}
									onValueChange={(val) => {
										if (val === '__custom__') {
											localDomainMode = 'custom';
											localDomain = '';
										} else {
											localDomain = val;
										}
									}}
								>
									<Select.Trigger id="cf-local-domain" class="w-full font-mono text-xs">
										<span class="truncate">{localDomain || t('cloudflare.exposeCard.localDomainPlaceholder')}</span>
									</Select.Trigger>
									<Select.Content>
										<Select.Group>
											<Select.Label>{t('cloudflare.exposeCard.localDomainGroupLabel')}</Select.Label>
											{#each localSites as st (st.domain)}
												<Select.Item value={st.domain} class="font-mono text-xs">
													{st.domain}
												</Select.Item>
											{/each}
											<Select.Separator />
											<Select.Item value="__custom__">
												{t('cloudflare.exposeCard.customLocalItem')}
											</Select.Item>
										</Select.Group>
									</Select.Content>
								</Select.Root>
							{:else}
								<InputGroup.Root>
									<InputGroup.Input
										id="cf-local-domain"
										placeholder="myapp.test"
										class="font-mono text-xs"
										bind:value={localDomain}
										required
									/>
									{#if localSites.length > 0}
										<InputGroup.Addon align="inline-end">
											<InputGroup.Button
												size="xs"
												onclick={() => {
													localDomainMode = 'select';
													localDomain = localSites.some((st) => st.domain === localDomain) ? localDomain : '';
												}}
											>
												{t('cloudflare.exposeCard.selectListBtn')}
											</InputGroup.Button>
										</InputGroup.Addon>
									{/if}
								</InputGroup.Root>
							{/if}
						</Field.Field>
					</Field.FieldGroup>

					<div class="flex flex-col gap-3 rounded-md border bg-muted/25 p-3.5">
						<div class="flex flex-wrap items-center justify-between gap-2">
							<span class="text-xs font-medium">
								{t('cloudflare.exposeCard.publicDomainSection', { profile: selectedProfileInfo?.name || profile })}
							</span>
							{#if selectedProfileInfo}
								<Button
									type="button"
									variant="ghost"
									size="xs"
									disabled={zonesLoading}
									onclick={() => loadProfileZones(selectedProfileInfo.name)}
								>
									<RotateCwIcon class={cn(zonesLoading && 'animate-spin')} data-icon="inline-start" />
									{zonesLoading ? t('cloudflare.exposeCard.loadingZonesShort') : t('cloudflare.exposeCard.reloadZones')}
								</Button>
							{/if}
						</div>

						{#if zonesError && selectedProfileInfo}
							<div class="flex flex-wrap items-center justify-between gap-2 text-xs text-destructive">
								<span>{zonesError}</span>
								<Button type="button" variant="outline" size="xs" onclick={() => openApiTokenModal(selectedProfileInfo)}>
									{t('cloudflare.exposeCard.updateApiToken')}
								</Button>
							</div>
						{/if}

						<Field.FieldGroup class="grid gap-3 sm:grid-cols-2 lg:grid-cols-[1fr_180px_1fr]">
							<Field.Field data-disabled={zonesLoading || cfZones.length === 0 ? true : undefined}>
								<Field.FieldLabel for="cf-zone-select" class="text-xs">{t('cloudflare.exposeCard.selectZoneLabel')}</Field.FieldLabel>
								<Select.Root
									type="single"
									value={selectedZoneName}
									disabled={zonesLoading || cfZones.length === 0}
									onValueChange={(val) => {
										selectedZoneName = val;
									}}
								>
									<Select.Trigger id="cf-zone-select" class="w-full font-mono text-xs">
										<span class="truncate">
											{selectedZoneName || (zonesLoading ? t('cloudflare.exposeCard.loadingZonesPlaceholder') : t('cloudflare.exposeCard.noZonesPlaceholder'))}
										</span>
									</Select.Trigger>
									<Select.Content>
										<Select.Group>
											<Select.Label>{t('cloudflare.exposeCard.zoneGroupLabel')}</Select.Label>
											{#each cfZones as z (z.id)}
												<Select.Item value={z.name} class="font-mono text-xs">
													{z.name} ({z.status})
												</Select.Item>
											{/each}
										</Select.Group>
									</Select.Content>
								</Select.Root>
							</Field.Field>

							<Field.Field data-invalid={subdomainError ? true : undefined}>
								<Field.FieldLabel for="cf-zone-sub" class="text-xs">{t('cloudflare.exposeCard.subdomainLabel')}</Field.FieldLabel>
								<Input
									id="cf-zone-sub"
									placeholder={t('cloudflare.exposeCard.subdomainPlaceholder')}
									class="font-mono text-xs"
									aria-invalid={subdomainError ? true : undefined}
									value={zoneSubdomain}
									oninput={(e) => {
										const rawVal = (e.currentTarget as HTMLInputElement).value;
										const norm = normalizeSubdomain(rawVal, selectedZoneName);
										zoneSubdomain = norm !== rawVal.trim().toLowerCase() ? norm : rawVal;
									}}
								/>
								{#if subdomainError}
									<Field.FieldError>{subdomainError}</Field.FieldError>
								{/if}
							</Field.Field>

							<Field.Field>
								<Field.FieldLabel for="cf-public-host" class="text-xs">{t('cloudflare.exposeCard.previewLabel')}</Field.FieldLabel>
								<InputGroup.Root class="bg-muted/50">
									<InputGroup.Addon>
										<InputGroup.Text class="font-mono text-xs">https://</InputGroup.Text>
									</InputGroup.Addon>
									<InputGroup.Input
										id="cf-public-host"
										value={publicHost}
										readonly
										tabindex={-1}
										aria-readonly="true"
										placeholder={t('cloudflare.exposeCard.previewPlaceholder')}
										class="cursor-default font-mono text-xs text-muted-foreground"
									/>
								</InputGroup.Root>
							</Field.Field>
						</Field.FieldGroup>

						{#if hostChecking}
							<div class="flex items-center gap-2 text-xs text-muted-foreground">
								<Spinner />
								<span>{t('cloudflare.exposeCard.checkingSubdomain')}</span>
							</div>
						{:else if hostCheck && !hostCheck.available}
							<div class="flex flex-wrap items-center justify-between gap-2.5">
								{@render statusBadgeTooltip(
									'warning',
									hostCheck.conflict_source === 'dns'
										? t('cloudflare.exposeCard.warnDnsExists')
										: t('cloudflare.exposeCard.warnSubTaken'),
									hostCheck.message || t('cloudflare.exposeCard.subTakenFallback'),
									hostCheck.dns_records
								)}
								<Field.Field orientation="horizontal" class="w-auto">
									<Checkbox id="cf-allow-overwrite" bind:checked={allowOverwrite} />
									<Field.FieldLabel for="cf-allow-overwrite" class="text-xs font-normal text-foreground">
										{t('cloudflare.exposeCard.allowOverwriteLabel')}
									</Field.FieldLabel>
								</Field.Field>
							</div>
						{:else if hostCheck && hostCheck.available}
							<div class="flex flex-wrap items-center gap-2">
								{@render statusBadgeTooltip(
									'success',
									hostCheck.dns_records && hostCheck.dns_records.length > 0
										? t('cloudflare.exposeCard.connectedToTunnel')
										: t('cloudflare.exposeCard.subAvailable'),
									hostCheck.message || t('cloudflare.exposeCard.subReadyFallback', { host: hostCheck.public_host }),
									hostCheck.dns_records
								)}
								{#if hostCheck.check_error}
									{@render statusBadgeTooltip(
										'warning',
										t('cloudflare.exposeCard.warnPartialCheck'),
										hostCheck.check_error
									)}
								{/if}
							</div>
						{/if}
					</div>

					<div class="flex flex-wrap items-center justify-between gap-3">
						<button
							type="button"
							class="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
							onclick={() => (showAdvancedService = !showAdvancedService)}
						>
							{showAdvancedService ? t('cloudflare.exposeCard.hideCustomTarget') : t('cloudflare.exposeCard.showCustomTarget')}
						</button>

						<Button
							type="submit"
							disabled={submitting ||
								!selectedProfileInfo ||
								!publicHost ||
								Boolean(subdomainError) ||
								hostChecking ||
								(hostCheck !== null && !hostCheck.available && !allowOverwrite)}
						>
							{#if submitting}
								<Spinner data-icon="inline-start" />
								{t('cloudflare.exposeCard.submitting')}
							{:else}
								<CloudUploadIcon data-icon="inline-start" />
								{t('cloudflare.exposeCard.submit')}
							{/if}
						</Button>
					</div>

					{#if showAdvancedService}
						<Field.Field class="max-w-md">
							<Field.FieldLabel for="cf-service-url">{t('cloudflare.exposeCard.serviceUrlLabel')}</Field.FieldLabel>
							<Input
								id="cf-service-url"
								placeholder={t('cloudflare.exposeCard.serviceUrlPlaceholder')}
								bind:value={serviceUrl}
							/>
							<Field.FieldDescription class="text-xs">
								{t('cloudflare.exposeCard.serviceUrlHelpPre')} <code class="font-mono">noTLSVerify</code>{t('cloudflare.exposeCard.serviceUrlHelpPost')}
							</Field.FieldDescription>
						</Field.Field>
					{/if}
				</form>
			{/if}
		</Card.Content>
	</Card.Root>

	<!-- Tabel Eksposur Aktif -->
	<section class="flex flex-col gap-3">
		<h2 class="text-lg font-semibold tracking-tight">{t('cloudflare.exposuresTable.heading')}</h2>
		<div class="flex flex-col sm:overflow-hidden sm:rounded-lg sm:border sm:bg-card">
			<ResponsiveTableWrapper label={t('cloudflare.exposuresTable.tableAria')} class="rounded-none border-0 bg-transparent">
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>{t('cloudflare.exposuresTable.colLocalDomain')}</Table.Head>
							<Table.Head>{t('cloudflare.exposuresTable.colPublicDomain')}</Table.Head>
							<Table.Head>{t('cloudflare.exposuresTable.colProfile')}</Table.Head>
							<Table.Head>{t('cloudflare.exposuresTable.colTarget')}</Table.Head>
							<Table.Head class="text-right">{t('cloudflare.exposuresTable.colActions')}</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#if allExposures.length === 0}
							<Table.Row>
								<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
									{t('cloudflare.exposuresTable.empty')}
								</Table.Cell>
							</Table.Row>
						{:else}
							{#each paginatedExposures as exp (`${exp.profile}:${exp.local_domain}:${exp.public_host}`)}
								<Table.Row>
									<Table.Cell data-label={t('cloudflare.exposuresTable.colLocalDomain')} class="font-mono text-xs">{exp.local_domain}</Table.Cell>
									<Table.Cell data-label={t('cloudflare.exposuresTable.colPublicDomain')}>
										<a
											href={`https://${exp.public_host}`}
											target="_blank"
											rel="noopener noreferrer"
											class="inline-flex items-center gap-1 break-all font-semibold text-primary hover:underline"
										>
											{exp.public_host}
											<ExternalLinkIcon class="size-3.5 shrink-0" />
										</a>
									</Table.Cell>
									<Table.Cell data-label={t('cloudflare.exposuresTable.colProfile')}>
										<Badge variant="outline">{exp.profile}</Badge>
									</Table.Cell>
									<Table.Cell data-label={t('cloudflare.exposuresTable.colTargetShort')} class="text-xs text-muted-foreground">
										<div class="font-mono">{exp.service_url || t('cloudflare.exposuresTable.autoTarget')}</div>
										{#if exp.http_host_header}
											<div class="mt-0.5 text-[11px]">
												Host: <span class="font-mono">{exp.http_host_header}</span>
												{#if exp.no_tls_verify}
													· <span class="font-mono">noTLSVerify</span>
												{/if}
											</div>
										{/if}
									</Table.Cell>
									<Table.Cell data-label={t('cloudflare.exposuresTable.colActions')} class="text-right">
										<div class="flex justify-end gap-1.5">
											<Button
												variant="outline"
												size="icon-xs"
												disabled={Boolean(exposureDeleting[exp.local_domain])}
												onclick={() => openEditExposureModal(exp)}
												title={t('cloudflare.exposuresTable.editLocalTitle')}
												aria-label={t('cloudflare.exposuresTable.editLocalTitle')}
											>
												<PencilIcon />
											</Button>
											<DropdownMenu.Root>
												<DropdownMenu.Trigger>
													{#snippet child({ props })}
														<Button
															variant="outline"
															size="xs"
															disabled={Boolean(exposureDeleting[exp.local_domain])}
															{...props}
														>
															{#if exposureDeleting[exp.local_domain]}
																<Spinner data-icon="inline-start" />
																{t('cloudflare.exposuresTable.deleting')}
															{:else}
																{t('cloudflare.exposuresTable.colActions')}
																<ChevronDownIcon data-icon="inline-end" />
															{/if}
														</Button>
													{/snippet}
												</DropdownMenu.Trigger>
												<DropdownMenu.Content align="end" class="w-48">
													<DropdownMenu.Group>
														<DropdownMenu.Label class="truncate text-xs text-muted-foreground">
															{exp.local_domain}
														</DropdownMenu.Label>
														<DropdownMenu.Separator />
														<DropdownMenu.Item>
															{#snippet child({ props })}
																<a href={`https://${exp.public_host}`} target="_blank" rel="noopener noreferrer" {...props}>
																	<ExternalLinkIcon />
																	<span>{t('cloudflare.exposuresTable.openPublicUrl')}</span>
																</a>
															{/snippet}
														</DropdownMenu.Item>
														<DropdownMenu.Item
															disabled={Boolean(exposureDeleting[exp.local_domain])}
															onclick={() => openEditExposureModal(exp)}
														>
															<PencilIcon />
															<span>{t('cloudflare.exposuresTable.editLocalItem')}</span>
														</DropdownMenu.Item>
														<DropdownMenu.Separator />
														<DropdownMenu.Item
															variant="destructive"
															disabled={Boolean(exposureDeleting[exp.local_domain])}
															onclick={() => unexpose(exp.profile, exp.local_domain)}
														>
															<Trash2Icon />
															<span>{t('cloudflare.exposuresTable.removeExposure')}</span>
														</DropdownMenu.Item>
													</DropdownMenu.Group>
												</DropdownMenu.Content>
											</DropdownMenu.Root>
										</div>
									</Table.Cell>
								</Table.Row>
							{/each}
						{/if}
					</Table.Body>
				</Table.Root>
			</ResponsiveTableWrapper>
			<PaginationBar
				bind:page={exposurePage}
				totalItems={allExposures.length}
				pageSize={PAGE_SIZE}
				itemLabel={t('cloudflare.exposuresTable.itemLabel')}
				class="rounded-lg border bg-card sm:rounded-none sm:border-0 sm:border-t mt-3 sm:mt-0"
			/>
		</div>
	</section>
</div>

<!-- Modal Tambah / Daftarkan Profil Multi-Akun Cloudflare -->
<Dialog.Root
	bind:open={addModalOpen}
	onOpenChange={(open) => {
		if (!open) void cancelLoginFlow();
	}}
>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-xl">
		<Dialog.Header>
			<Dialog.Title>{t('cloudflare.addModal.title')}</Dialog.Title>
			<Dialog.Description>
				{t('cloudflare.addModal.descPre')} <code class="font-mono">cloudflared@&lt;profil&gt;</code> {t('cloudflare.addModal.descPost')}
			</Dialog.Description>
		</Dialog.Header>

		<Tabs.Root bind:value={addTab} class="mt-2 flex flex-col gap-4">
			<Tabs.List class="grid w-full grid-cols-3">
				<Tabs.Trigger value="token">{t('cloudflare.addModal.tabToken')}</Tabs.Trigger>
				<Tabs.Trigger value="login">{t('cloudflare.addModal.tabLogin')}</Tabs.Trigger>
				<Tabs.Trigger value="cert">{t('cloudflare.addModal.tabCert')}</Tabs.Trigger>
			</Tabs.List>

			<!-- Tab 1: Tunnel Token -->
			<Tabs.Content value="token">
				<form onsubmit={submitTokenProfile} class="flex flex-col gap-4">
					<Field.FieldGroup>
						<Field.Field>
							<Field.FieldLabel for="cf-tok-name">{t('cloudflare.addModal.profileNameLabel')}</Field.FieldLabel>
							<Input
								id="cf-tok-name"
								placeholder={t('cloudflare.addModal.tokenNamePlaceholder')}
								bind:value={tokenName}
							/>
							<Field.FieldDescription class="text-xs">
								{t('cloudflare.addModal.tokenNameHelpPre')} <code class="font-mono">-</code> {t('cloudflare.addModal.tokenNameHelpOr')} <code class="font-mono">_</code>{t('cloudflare.addModal.tokenNameHelpPost')}
							</Field.FieldDescription>
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-tok-val">{t('cloudflare.addModal.tunnelTokenLabel')}</Field.FieldLabel>
							<Textarea
								id="cf-tok-val"
								rows={3}
								class="font-mono text-xs"
								placeholder={t('cloudflare.addModal.tunnelTokenPlaceholder')}
								bind:value={tokenValue}
								required
							/>
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-tok-tunnel">{t('cloudflare.addModal.tunnelLabelLabel')}</Field.FieldLabel>
							<Input id="cf-tok-tunnel" placeholder={t('cloudflare.addModal.tunnelLabelPlaceholder')} bind:value={tokenTunnelName} />
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-tok-api">{t('cloudflare.addModal.apiTokenOptLabel')}</Field.FieldLabel>
							<Input
								id="cf-tok-api"
								type="password"
								class="font-mono text-xs"
								placeholder={t('cloudflare.addModal.tokenApiPlaceholder')}
								bind:value={tokenApiToken}
							/>
							<Field.FieldDescription class="text-xs">
								{t('cloudflare.addModal.tokenApiHelpPre')} <code class="font-mono">wdev</code> {t('cloudflare.addModal.tokenApiHelpPost')}
							</Field.FieldDescription>
						</Field.Field>

						<Field.Field orientation="horizontal">
							<Checkbox id="cf-tok-start" bind:checked={tokenAutoStart} />
							<Field.FieldLabel for="cf-tok-start" class="font-normal">
								{t('cloudflare.addModal.autoStartLabel')}
							</Field.FieldLabel>
						</Field.Field>
					</Field.FieldGroup>

					<Dialog.Footer>
						<Button type="button" variant="outline" onclick={() => (addModalOpen = false)}>{t('cloudflare.addModal.cancel')}</Button>
						<Button type="submit" disabled={savingToken}>
							{#if savingToken}
								<Spinner data-icon="inline-start" />
								{t('cloudflare.addModal.saving')}
							{:else}
								{t('cloudflare.addModal.saveAndActivate')}
							{/if}
						</Button>
					</Dialog.Footer>
				</form>
			</Tabs.Content>

			<!-- Tab 2: Browser OAuth Login -->
			<Tabs.Content value="login" class="flex flex-col gap-4">
				{#if !loginSession}
					<form onsubmit={startBrowserLoginFlow} class="flex flex-col gap-4">
						<Field.FieldGroup>
							<Field.Field>
								<Field.FieldLabel for="cf-login-name">{t('cloudflare.addModal.profileNameLabel')}</Field.FieldLabel>
								<Input
									id="cf-login-name"
									placeholder={t('cloudflare.addModal.loginNamePlaceholder')}
									bind:value={loginName}
								/>
								<Field.FieldDescription class="text-xs">
									{t('cloudflare.addModal.loginNameHelp')}
								</Field.FieldDescription>
							</Field.Field>
							<Field.Field>
								<Field.FieldLabel for="cf-login-tunnel">{t('cloudflare.addModal.loginTunnelLabel')}</Field.FieldLabel>
								<Input id="cf-login-tunnel" placeholder={t('cloudflare.addModal.loginTunnelPlaceholder')} bind:value={loginTunnelName} />
								<Field.FieldDescription class="text-xs">
									{t('cloudflare.addModal.loginTunnelHelpPre')} <code class="font-mono">wdev</code> {t('cloudflare.addModal.loginTunnelHelpMid')} <code class="font-mono">cert.pem</code> {t('cloudflare.addModal.loginTunnelHelpAnd')} <code class="font-mono">cloudflared tunnel create</code>{t('cloudflare.addModal.loginTunnelHelpPost')}
								</Field.FieldDescription>
							</Field.Field>
							<Field.Field>
								<Field.FieldLabel for="cf-login-api">{t('cloudflare.addModal.apiTokenOptLabel')}</Field.FieldLabel>
								<Input
									id="cf-login-api"
									type="password"
									class="font-mono text-xs"
									placeholder={t('cloudflare.addModal.loginApiPlaceholder')}
									bind:value={loginApiToken}
								/>
							</Field.Field>
						</Field.FieldGroup>

						<Dialog.Footer>
							<Button type="button" variant="outline" onclick={() => (addModalOpen = false)}>{t('cloudflare.addModal.cancel')}</Button>
							<Button type="submit" disabled={startingLogin}>
								{#if startingLogin}
									<Spinner data-icon="inline-start" />
									{t('cloudflare.addModal.preparingUrl')}
								{:else}
									<GlobeIcon data-icon="inline-start" />
									{t('cloudflare.addModal.startLoginBtn')}
								{/if}
							</Button>
						</Dialog.Footer>
					</form>
				{:else}
					<div class="flex flex-col gap-4">
						<Alert.Root>
							<InfoIcon />
							<Alert.Title>{t('cloudflare.addModal.waitingAuthTitle', { profile: loginSession.profile || t('cloudflare.addModal.autoProfileFallback') })}</Alert.Title>
							<Alert.Description class="flex flex-col gap-2 text-xs">
								<p>{loginSession.message}</p>
								{#if loginSession.auth_url}
									<div class="flex flex-wrap items-center gap-2 pt-1">
										<Button
											href={loginSession.auth_url}
											target="_blank"
											rel="noopener noreferrer"
											size="sm"
										>
											<ExternalLinkIcon data-icon="inline-start" />
											{t('cloudflare.addModal.openAuthBtn')}
										</Button>
										<Button
											type="button"
											variant="outline"
											size="sm"
											onclick={() => copyText(loginSession?.auth_url || '', t('cloudflare.copyLabels.authUrl'))}
										>
											<CopyIcon data-icon="inline-start" />
											{t('cloudflare.addModal.copyUrlBtn')}
										</Button>
									</div>
								{/if}
							</Alert.Description>
						</Alert.Root>

						{#if loginSession.status === 'failed' || loginSession.status === 'cancelled'}
							<Alert.Root variant="destructive">
								<AlertCircleIcon />
								<Alert.Title>{t('cloudflare.addModal.loginStoppedTitle')}</Alert.Title>
								<Alert.Description class="font-mono text-xs break-all">
									{loginSession.error || loginSession.message}
								</Alert.Description>
							</Alert.Root>
						{:else}
							<div class="flex items-center gap-2 text-xs text-muted-foreground">
								<Spinner />
								<span>{t('cloudflare.addModal.pollingAuth')}</span>
							</div>
						{/if}

						<Dialog.Footer>
							<Button type="button" variant="outline" onclick={cancelLoginFlow}>
								{t('cloudflare.addModal.closeCancelBtn')}
							</Button>
						</Dialog.Footer>
					</div>
				{/if}
			</Tabs.Content>

			<!-- Tab 3: Paste / Import cert.pem -->
			<Tabs.Content value="cert">
				<form onsubmit={submitCertProfile} class="flex flex-col gap-4">
					<Field.FieldGroup>
						<Field.Field>
							<Field.FieldLabel for="cf-cert-name">{t('cloudflare.addModal.profileNameLabel')}</Field.FieldLabel>
							<Input
								id="cf-cert-name"
								placeholder={t('cloudflare.addModal.tokenNamePlaceholder')}
								bind:value={certName}
							/>
							<Field.FieldDescription class="text-xs">
								{t('cloudflare.addModal.certNameHelpPre')} <code class="font-mono">cert.pem</code> {t('cloudflare.addModal.certNameHelpOr')}
							</Field.FieldDescription>
						</Field.Field>

						{#if (cfStatus?.detected_local_certs?.length ?? 0) > 0}
							<Field.Field>
								<Field.FieldLabel for="cf-local-cert-pick">{t('cloudflare.addModal.localCertLabel')}</Field.FieldLabel>
								<Select.Root type="single" bind:value={selectedLocalCert}>
									<Select.Trigger id="cf-local-cert-pick" class="w-full font-mono text-xs">
										<span class="truncate">{selectedLocalCert || t('cloudflare.addModal.localCertPlaceholder')}</span>
									</Select.Trigger>
									<Select.Content>
										<Select.Group>
											<Select.Label>{t('cloudflare.addModal.localCertGroupLabel')}</Select.Label>
											{#each cfStatus?.detected_local_certs ?? [] as certPath (certPath)}
												<Select.Item value={certPath} class="font-mono text-xs">{certPath}</Select.Item>
											{/each}
										</Select.Group>
									</Select.Content>
								</Select.Root>
							</Field.Field>
						{/if}

						<Field.Field>
							<Field.FieldLabel for="cf-cert-pem">{t('cloudflare.addModal.certPemLabel')}</Field.FieldLabel>
							<Textarea
								id="cf-cert-pem"
								rows={4}
								class="font-mono text-xs"
								placeholder="-----BEGIN ARGO TUNNEL TOKEN-----&#10;...&#10;-----END ARGO TUNNEL TOKEN-----"
								bind:value={certPEM}
							/>
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-cert-tun">{t('cloudflare.addModal.certTunLabel')}</Field.FieldLabel>
							<Input id="cf-cert-tun" placeholder={t('cloudflare.addModal.loginTunnelPlaceholder')} bind:value={certTunnelName} />
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-cert-cred">{t('cloudflare.addModal.certCredLabel')}</Field.FieldLabel>
							<Textarea
								id="cf-cert-cred"
								rows={3}
								class="font-mono text-xs"
								placeholder={'{"AccountTag":"...","TunnelSecret":"...","TunnelID":"..."}'}
								bind:value={certCredJSON}
							/>
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-cert-api">{t('cloudflare.addModal.apiTokenOptLabel')}</Field.FieldLabel>
							<Input
								id="cf-cert-api"
								type="password"
								class="font-mono text-xs"
								placeholder={t('cloudflare.addModal.loginApiPlaceholder')}
								bind:value={certApiToken}
							/>
						</Field.Field>

						<Field.Field orientation="horizontal">
							<Checkbox id="cf-cert-start" bind:checked={certAutoStart} />
							<Field.FieldLabel for="cf-cert-start" class="font-normal">
								{t('cloudflare.addModal.autoStartLabel')}
							</Field.FieldLabel>
						</Field.Field>
					</Field.FieldGroup>

					<Dialog.Footer>
						<Button type="button" variant="outline" onclick={() => (addModalOpen = false)}>{t('cloudflare.addModal.cancel')}</Button>
						<Button type="submit" disabled={savingCert}>
							{#if savingCert}
								<Spinner data-icon="inline-start" />
								{t('cloudflare.addModal.registering')}
							{:else}
								{t('cloudflare.addModal.saveCertBtn')}
							{/if}
						</Button>
					</Dialog.Footer>
				</form>
			</Tabs.Content>
		</Tabs.Root>
	</Dialog.Content>
</Dialog.Root>

<!-- Modal Log Service per Profil -->
<Dialog.Root bind:open={logsModalOpen}>
	<Dialog.Content class="max-h-[85vh] sm:max-w-2xl">
		<Dialog.Header>
			<Dialog.Title>{t('cloudflare.logsModal.title', { name: logsProfileName })}</Dialog.Title>
			<Dialog.Description>{t('cloudflare.logsModal.desc')}</Dialog.Description>
		</Dialog.Header>
		<div class="max-h-[55vh] overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap break-all">
			{#if logsLoading}
				<div class="flex items-center gap-2 text-muted-foreground">
					<Spinner />
					<span>{t('cloudflare.logsModal.loading')}</span>
				</div>
			{:else}
				{logsText}
			{/if}
		</div>
		<Dialog.Footer>
			<Button type="button" variant="outline" size="sm" onclick={refreshProfileLogs} disabled={logsLoading}>
				<RotateCwIcon data-icon="inline-start" />
				{t('cloudflare.logsModal.reload')}
			</Button>
			<Button type="button" size="sm" onclick={() => (logsModalOpen = false)}>{t('cloudflare.logsModal.close')}</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Modal Kelola Cloudflare API Token per Profil -->
<Dialog.Root bind:open={apiTokenModalOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>{t('cloudflare.apiTokenModal.title', { name: apiTokenTargetProfile?.name ?? '' })}</Dialog.Title>
			<Dialog.Description>
				{t('cloudflare.apiTokenModal.descPre')} <code class="font-mono">0600</code> {t('cloudflare.apiTokenModal.descPost')}
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={submitProfileApiToken} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="cf-modal-api-token">{t('cloudflare.apiTokenModal.label')}</Field.FieldLabel>
					<Input
						id="cf-modal-api-token"
						type="password"
						class="font-mono text-xs"
						placeholder={apiTokenTargetProfile?.has_api_token ? t('cloudflare.apiTokenModal.replacePlaceholder') : t('cloudflare.apiTokenModal.pastePlaceholder')}
						bind:value={apiTokenInput}
					/>
					<Field.FieldDescription class="text-xs leading-relaxed">
						{t('cloudflare.apiTokenModal.helpPre')}
						<code class="font-mono">Zone:Read</code> {t('cloudflare.apiTokenModal.permZoneRead')}
						<code class="font-mono">DNS:Edit</code> {t('cloudflare.apiTokenModal.permDnsEdit')}
						<code class="font-mono">Cloudflare Tunnel:Edit</code> {t('cloudflare.apiTokenModal.permTunnelEdit')}
					</Field.FieldDescription>
				</Field.Field>
			</Field.FieldGroup>
			<Dialog.Footer class="flex-wrap gap-2 sm:justify-between">
				<div>
					{#if apiTokenTargetProfile?.has_api_token}
						<Button type="button" variant="destructive" size="sm" disabled={savingApiToken} onclick={clearProfileApiToken}>
							{t('cloudflare.apiTokenModal.removeBtn')}
						</Button>
					{/if}
				</div>
				<div class="flex gap-2">
					<Button type="button" variant="outline" size="sm" onclick={() => (apiTokenModalOpen = false)}>{t('cloudflare.apiTokenModal.cancel')}</Button>
					<Button type="submit" size="sm" disabled={savingApiToken}>
						{#if savingApiToken}
							<Spinner data-icon="inline-start" />
							{t('cloudflare.apiTokenModal.saving')}
						{:else}
							{t('cloudflare.apiTokenModal.saveBtn')}
						{/if}
					</Button>
				</div>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<!-- Modal Konfirmasi Hapus Profil Cloudflare -->
<Dialog.Root bind:open={deleteModalOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>{t('cloudflare.deleteModal.title', { name: deleteTargetProfile?.name ?? '' })}</Dialog.Title>
			<Dialog.Description>
				{t('cloudflare.deleteModal.descPre')} <code class="font-mono">{deleteTargetProfile?.service_unit}</code> {t('cloudflare.deleteModal.descPost')}
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={confirmDeleteProfile} class="flex flex-col gap-4">
			<Field.FieldSet class="rounded-md border bg-muted/25 p-3">
				<Field.FieldLegend variant="label" class="sr-only">{t('cloudflare.deleteModal.legend')}</Field.FieldLegend>
				<Field.FieldGroup class="gap-3">
					<Field.Field orientation="horizontal" data-disabled={!canDeleteRemote || deletingProfile ? true : undefined}>
						<Checkbox
							id="cf-del-remote"
							bind:checked={deleteRemote}
							disabled={!canDeleteRemote || deletingProfile}
						/>
						<Field.FieldContent>
							<Field.FieldLabel for="cf-del-remote" class="text-xs font-medium">
								{t('cloudflare.deleteModal.deleteRemoteLabel')}
							</Field.FieldLabel>
							<Field.FieldDescription class="text-xs leading-relaxed">
								{#if canDeleteRemote}
									{#if deleteTargetProfile?.auth_mode !== 'token' && deleteTargetProfile?.has_cert}
										{t('cloudflare.deleteModal.deleteRemoteCertDescPre')} <code class="font-mono">cloudflared tunnel delete -f</code> {t('cloudflare.deleteModal.deleteRemoteCertDescPost')} <code class="font-mono">cert.pem</code>.
									{:else}
										{t('cloudflare.deleteModal.deleteRemoteApiDescPre')}<code class="font-mono">cfd_tunnel</code>{t('cloudflare.deleteModal.deleteRemoteApiDescPost')}
									{/if}
								{:else if deleteTargetProfile?.auth_mode === 'token'}
									{t('cloudflare.deleteModal.deleteRemoteTokenUnavailablePre')}<code class="font-mono">Cloudflare Tunnel:Edit</code>{t('cloudflare.deleteModal.deleteRemoteTokenUnavailablePost')}
								{:else}
									{t('cloudflare.deleteModal.deleteRemoteMissingUnavailablePre')} <code class="font-mono">cert.pem</code> {t('cloudflare.deleteModal.deleteRemoteMissingUnavailablePost')}
								{/if}
							</Field.FieldDescription>
						</Field.FieldContent>
					</Field.Field>

					<Field.Field orientation="horizontal" data-disabled={!canDeleteDNS || deletingProfile ? true : undefined}>
						<Checkbox
							id="cf-del-dns"
							bind:checked={deleteDNS}
							disabled={!canDeleteDNS || deletingProfile}
						/>
						<Field.FieldContent>
							<Field.FieldLabel for="cf-del-dns" class="text-xs font-medium">
								{t('cloudflare.deleteModal.deleteDnsLabel', { count: deleteTargetProfile?.route_count ?? 0 })}
							</Field.FieldLabel>
							<Field.FieldDescription class="text-xs leading-relaxed">
								{#if canDeleteDNS}
									{t('cloudflare.deleteModal.deleteDnsAvailable')}
								{:else if !deleteTargetProfile?.has_api_token}
									{t('cloudflare.deleteModal.deleteDnsNeedsTokenPre')}<code class="font-mono">DNS:Edit</code>{t('cloudflare.deleteModal.deleteDnsNeedsTokenPost')}
								{:else}
									{t('cloudflare.deleteModal.deleteDnsNoRoutes')}
								{/if}
							</Field.FieldDescription>
						</Field.FieldContent>
					</Field.Field>
				</Field.FieldGroup>
			</Field.FieldSet>

			<Dialog.Footer>
				<Button type="button" variant="outline" size="sm" onclick={() => (deleteModalOpen = false)} disabled={deletingProfile}>
					{t('cloudflare.deleteModal.cancel')}
				</Button>
				<Button type="submit" variant="destructive" size="sm" disabled={deletingProfile}>
					{#if deletingProfile}
						<Spinner data-icon="inline-start" />
						{t('cloudflare.deleteModal.deleting')}
					{:else}
						{t('cloudflare.deleteModal.deleteBtn')}
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<!-- Modal Edit Domain Lokal Eksposur Aktif -->
<Dialog.Root bind:open={editExposureModalOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>{t('cloudflare.editModal.title')}</Dialog.Title>
			<Dialog.Description>
				{t('cloudflare.editModal.descPre')} <code class="font-mono font-semibold text-foreground">{editTargetExposure?.public_host}</code> {t('cloudflare.editModal.descPost')}
			</Dialog.Description>
		</Dialog.Header>

		{#if editTargetExposure}
			<form onsubmit={submitExposureEdit} class="flex flex-col gap-4">
				<div class="flex flex-col gap-2 rounded-md border bg-muted/25 p-3 text-xs">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<span class="text-muted-foreground">{t('cloudflare.editModal.publicDomainLabel')}</span>
						<span class="font-mono font-semibold text-foreground">{editTargetExposure.public_host}</span>
					</div>
					<div class="flex flex-wrap items-center justify-between gap-2">
						<span class="text-muted-foreground">{t('cloudflare.editModal.tunnelProfileLabel')}</span>
						<Badge variant="outline">{editTargetExposure.profile}</Badge>
					</div>
					<div class="flex flex-wrap items-center justify-between gap-2">
						<span class="text-muted-foreground">{t('cloudflare.editModal.previousLocalLabel')}</span>
						<span class="font-mono text-muted-foreground">{editTargetExposure.local_domain}</span>
					</div>
				</div>

				{#if editMissingApiToken && editProfileInfo}
					<Alert.Root class="border-warning/40 bg-warning/10">
						<AlertCircleIcon class="text-warning" />
						<Alert.Title class="text-warning">{t('cloudflare.editModal.missingTokenTitle')}</Alert.Title>
						<Alert.Description class="flex flex-col gap-2 text-xs text-muted-foreground">
							<p>
								{t('cloudflare.editModal.missingTokenPre')} <code class="font-mono">{editProfileInfo.name}</code> {t('cloudflare.editModal.missingTokenPost')}
							</p>
							<div>
								<Button
									type="button"
									variant="outline"
									size="xs"
									onclick={() => {
										const prof = editProfileInfo;
										editExposureModalOpen = false;
										if (prof) openApiTokenModal(prof);
									}}
								>
									<KeyRoundIcon data-icon="inline-start" />
									{t('cloudflare.editModal.attachTokenBtn', { name: editProfileInfo.name })}
								</Button>
							</div>
						</Alert.Description>
					</Alert.Root>
				{/if}

				<Field.FieldGroup>
					<Field.Field>
						<div class="flex flex-wrap items-center justify-between gap-2">
							<Field.FieldLabel for="cf-edit-local-domain">{t('cloudflare.editModal.newLocalDomainLabel')}</Field.FieldLabel>
							{#if editReassignedLocalConflict}
								{@render statusBadgeTooltip(
									'warning',
									t('cloudflare.editModal.reassignedBadge', { host: editReassignedLocalConflict.public_host }),
									t('cloudflare.editModal.reassignedTooltip', {
										local: editReassignedLocalConflict.local_domain,
										host: editReassignedLocalConflict.public_host,
										profile: editReassignedLocalConflict.profile
									})
								)}
							{/if}
						</div>
						{#if localSites.length > 0 && editLocalDomainMode === 'select'}
							<Select.Root
								type="single"
								value={editLocalDomain}
								onValueChange={(val) => {
									if (val === '__custom__') {
										editLocalDomainMode = 'custom';
										editLocalDomain = '';
									} else {
										editLocalDomain = val;
									}
								}}
							>
								<Select.Trigger id="cf-edit-local-domain" class="w-full font-mono text-xs">
									<span class="truncate">{editLocalDomain || t('cloudflare.exposeCard.localDomainPlaceholder')}</span>
								</Select.Trigger>
								<Select.Content>
									<Select.Group>
										<Select.Label>{t('cloudflare.exposeCard.localDomainGroupLabel')}</Select.Label>
										{#each localSites as st (st.domain)}
											{@const bound = exposureByLocalDomain[st.domain.toLowerCase()]}
											<Select.Item value={st.domain} label={st.domain} class="font-mono text-xs">
												<span class="truncate">{st.domain}</span>
												{#if bound && bound.public_host.toLowerCase() === editTargetExposure.public_host.toLowerCase()}
													<Badge variant="secondary" class="font-sans text-[10px]">{t('cloudflare.editModal.badgeCurrent')}</Badge>
												{:else if bound}
													<Badge variant="outline" class="border-warning/40 bg-warning/10 font-sans text-[10px] text-warning">{t('cloudflare.editModal.badgeWarn')}</Badge>
												{/if}
											</Select.Item>
										{/each}
										<Select.Separator />
										<Select.Item value="__custom__">
											{t('cloudflare.exposeCard.customLocalItem')}
										</Select.Item>
									</Select.Group>
								</Select.Content>
							</Select.Root>
						{:else}
							<InputGroup.Root>
								<InputGroup.Input
									id="cf-edit-local-domain"
									placeholder="myapp.test"
									class="font-mono text-xs"
									bind:value={editLocalDomain}
									required
								/>
								{#if localSites.length > 0}
									<InputGroup.Addon align="inline-end">
										<InputGroup.Button
											size="xs"
											onclick={() => {
												editLocalDomainMode = 'select';
												editLocalDomain = localSites.some(
													(st) => st.domain.toLowerCase() === editLocalDomain.trim().toLowerCase()
												)
													? editLocalDomain.trim()
													: '';
											}}
										>
											{t('cloudflare.exposeCard.selectListBtn')}
										</InputGroup.Button>
									</InputGroup.Addon>
								{/if}
							</InputGroup.Root>
						{/if}
					</Field.Field>

					<div class="flex flex-col gap-2">
						<button
							type="button"
							class="w-fit text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
							onclick={() => (editShowAdvancedService = !editShowAdvancedService)}
						>
							{editShowAdvancedService
								? t('cloudflare.exposeCard.hideCustomTarget')
								: t('cloudflare.exposeCard.showCustomTarget')}
						</button>

						{#if editShowAdvancedService}
							<Field.Field>
								<Field.FieldLabel for="cf-edit-service-url">{t('cloudflare.exposeCard.serviceUrlLabel')}</Field.FieldLabel>
								<Input
									id="cf-edit-service-url"
									class="font-mono text-xs"
									placeholder={t('cloudflare.exposeCard.serviceUrlPlaceholder')}
									bind:value={editServiceUrl}
								/>
								<Field.FieldDescription class="text-xs">
									{t('cloudflare.editModal.serviceUrlHelp')}
								</Field.FieldDescription>
							</Field.Field>
						{/if}
					</div>
				</Field.FieldGroup>

				<Dialog.Footer>
					<Button
						type="button"
						variant="outline"
						size="sm"
						onclick={() => (editExposureModalOpen = false)}
						disabled={savingExposureEdit}
					>
						{t('cloudflare.editModal.cancel')}
					</Button>
					<Button
						type="submit"
						size="sm"
						disabled={savingExposureEdit || !editNormalizedLocal || editMissingApiToken}
					>
						{#if savingExposureEdit}
							<Spinner data-icon="inline-start" />
							{t('cloudflare.editModal.saving')}
						{:else}
							{t('cloudflare.editModal.saveBtn')}
						{/if}
					</Button>
				</Dialog.Footer>
			</form>
		{/if}
	</Dialog.Content>
</Dialog.Root>
