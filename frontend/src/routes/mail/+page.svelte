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
	import { Input } from '#lib/components/ui/input/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PaginationBar from '#lib/components/shared/PaginationBar.svelte';
	import PasswordInput from '#lib/components/shared/PasswordInput.svelte';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import type {
		DomainAccounts,
		MailStatus,
		PreflightStatus,
		RelayDNSReport,
		ZoneInfo
	} from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import MailPlusIcon from '@lucide/svelte/icons/mail-plus';
	import UserPlusIcon from '@lucide/svelte/icons/user-plus';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import WrenchIcon from '@lucide/svelte/icons/wrench';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import InfoIcon from '@lucide/svelte/icons/info';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import UsersIcon from '@lucide/svelte/icons/users';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import MailIcon from '@lucide/svelte/icons/mail';
	import SearchIcon from '@lucide/svelte/icons/search';
	import CheckIcon from '@lucide/svelte/icons/check';

	let mailStatus = $state<MailStatus | null>(null);
	let domainGroups = $state<DomainAccounts[]>([]);
	let zones = $state<ZoneInfo[]>([]);
	let domainFilter = $state('');
	let accountsError = $state('');
	let refreshing = $state(false);
	let domainPending = $state<Record<string, 'unset-relay' | 'reset-admin' | 'repair'>>({});
	let accountPending = $state<Record<string, boolean>>({});

	// Add Email Account Modal
	let addAccOpen = $state(false);
	let newEmail = $state('');
	let newEmailPass = $state('');
	let addingAcc = $state(false);

	// Change Email Account Password Modal
	let editPassOpen = $state(false);
	let editPassEmail = $state('');
	let editPassValue = $state('');
	let editingPass = $state(false);

	// Delete Domain Modal
	let deleteDomOpen = $state(false);
	let deleteDomTarget = $state('');
	let deleteDomConfirm = $state('');
	let deleteDomDNS = $state(false);
	let deletingDom = $state(false);

	// Set Relay Modal
	let relayOpen = $state(false);
	let relayTargetDomain = $state('global');
	let relayProvider = $state('resend');
	let relaySecret = $state('');
	let relayHost = $state('');
	let relayPortStr = $state('587');
	let relayUser = $state('');
	let relayPass = $state('');
	let relayDNSText = $state('');
	let relayDNSBaseline = $state('');
	let savingRelay = $state(false);
	let syncingRelayDNS = $state(false);
	let relaySyncNotice = $state<{ tone: 'info' | 'warn'; text: string } | null>(null);

	const hasStoredRelayKey = $derived.by(() => {
		const existing =
			relayTargetDomain === 'global'
				? mailStatus?.global_relay
				: mailStatus?.domain_relays?.[relayTargetDomain] ?? mailStatus?.global_relay;
		return Boolean(existing?.host && existing.provider?.toLowerCase() === relayProvider);
	});

	// Setup Domain Mail Modal
	let setupOpen = $state(false);
	let setupDomainChoice = $state('');
	let setupCustomDomain = $state('');
	let setupSSL = $state(true);
	let setupRelayProv = $state('');
	let setupResendKey = $state('');
	let setupBrevoUser = $state('');
	let setupBrevoKey = $state('');
	let setupCustomHost = $state('');
	let setupCustomPortStr = $state('587');
	let setupCustomUser = $state('');
	let setupCustomPass = $state('');
	let preflight = $state<PreflightStatus | null>(null);
	let preflightLoading = $state(false);
	let settingUp = $state(false);
	let lastLoadedMode: string | null = null;

	const totalAccounts = $derived(
		domainGroups.reduce((sum, g) => sum + (g.accounts?.length ?? 0), 0)
	);

	const stalwartPorts = $derived(
		mailStatus?.stalwart_ports?.length
			? mailStatus.stalwart_ports
			: [
					{ port: 25, label: 'SMTP', open: false, public: false },
					{ port: 465, label: 'SMTPS', open: false, public: false },
					{ port: 587, label: 'Submission', open: false, public: false },
					{ port: 993, label: 'IMAPS', open: false, public: false },
					{ port: 8080, label: 'Admin HTTP', open: false, public: false, internal_only: true }
				]
	);

	const openPortCount = $derived(stalwartPorts.filter((p) => p.open).length);
	const publicMailPorts = $derived(stalwartPorts.filter((p) => !p.internal_only && p.port !== 8080));
	const publicOpenCount = $derived(
		publicMailPorts.filter((p) => (p.local ?? p.open) && p.external_checked && p.public).length
	);
	const blockedOutsidePorts = $derived(
		publicMailPorts.filter((p) => (p.local ?? p.open) && p.external_checked && !p.public)
	);
	const unverifiedOutsidePorts = $derived(
		publicMailPorts.filter((p) => (p.local ?? p.open) && !p.external_checked)
	);

	const filteredGroups = $derived(
		domainFilter ? domainGroups.filter((g) => g.domain === domainFilter) : domainGroups
	);

	// Tabs & Search
	type ActiveTab = 'domains' | 'accounts' | 'ports';
	let activeTab = $state<ActiveTab>('domains');
	let domainSearch = $state('');
	let accountSearch = $state('');

	function handleTabChange(val: string) {
		activeTab = val as ActiveTab;
	}

	// Pagination (Max 10 data per page)
	const PAGE_SIZE = 10;
	let domainPage = $state(1);
	let accountPage = $state(1);

	$effect(() => {
		void domainSearch;
		domainPage = 1;
	});

	$effect(() => {
		void domainFilter;
		void accountSearch;
		accountPage = 1;
	});

	const allMailDomains = $derived(mailStatus?.domains || []);
	const filteredMailDomains = $derived.by(() => {
		const q = domainSearch.trim().toLowerCase();
		if (!q) return allMailDomains;
		return allMailDomains.filter((d) => d.domain.toLowerCase().includes(q));
	});
	const paginatedMailDomains = $derived(
		filteredMailDomains.slice((domainPage - 1) * PAGE_SIZE, domainPage * PAGE_SIZE)
	);

	type FlatAccountItem = {
		domain: string;
		webmail: string;
		relay: (typeof domainGroups)[number]['relay'];
		account: (typeof domainGroups)[number]['accounts'][number];
	};

	const flatAccounts = $derived.by(() => {
		const list: FlatAccountItem[] = [];
		for (const grp of filteredGroups) {
			for (const acc of grp.accounts || []) {
				list.push({
					domain: grp.domain,
					webmail: grp.webmail,
					relay: grp.relay,
					account: acc
				});
			}
		}
		return list;
	});

	const filteredAccounts = $derived.by(() => {
		const q = accountSearch.trim().toLowerCase();
		if (!q) return flatAccounts;
		return flatAccounts.filter(
			(item) =>
				item.account.email.toLowerCase().includes(q) ||
				(item.account.name && item.account.name.toLowerCase().includes(q)) ||
				item.domain.toLowerCase().includes(q)
		);
	});

	const paginatedAccounts = $derived(
		filteredAccounts.slice((accountPage - 1) * PAGE_SIZE, accountPage * PAGE_SIZE)
	);

	const zoneDomains = $derived(
		Array.from(
			new Set(
				zones
					.map((z) => (z.name || '').replace(/\.$/, '').trim())
					.filter(Boolean)
			)
		)
	);

	const effectiveSetupDomain = $derived(
		setupDomainChoice === '__custom__' ? setupCustomDomain.trim() : setupDomainChoice.trim()
	);

	async function loadMail(showProgress = false) {
		if (showProgress) refreshing = true;
		try {
			const [stRes, accRes, zoneRes] = await Promise.allSettled([
				api.getMailStatus(showProgress),
				api.getMailAccounts(),
				api.listPDNSZones()
			]);
			if (stRes.status === 'fulfilled') {
				mailStatus = stRes.value;
			} else {
				toast.error(stRes.reason instanceof Error ? stRes.reason.message : t('mail.toasts.statusLoadError'));
			}
			if (accRes.status === 'fulfilled') {
				domainGroups = accRes.value ?? [];
				accountsError = '';
			} else {
				domainGroups = [];
				accountsError = accRes.reason instanceof Error ? accRes.reason.message : t('mail.toasts.accountsLoadError');
			}
			if (zoneRes.status === 'fulfilled') {
				zones = zoneRes.value ?? [];
			}
		} finally {
			if (showProgress) refreshing = false;
		}
	}

	onMount(async () => {
		const tabParam = page.url.searchParams.get('tab');
		if (tabParam === 'domains' || tabParam === 'accounts' || tabParam === 'ports') {
			activeTab = tabParam;
		}
		await loadMail();
	});

	$effect(() => {
		if (cp.refreshTick > 0) loadMail();
	});

	$effect(() => {
		const currentMode = modeStore.mode;
		if (lastLoadedMode !== null && lastLoadedMode !== currentMode) {
			void loadMail();
		}
		lastLoadedMode = currentMode;
	});

	function openAddAccount(domain = '') {
		const defDom = domain || mailStatus?.domains?.[0]?.domain || '';
		newEmail = defDom ? `@${defDom}` : '';
		newEmailPass = '';
		addAccOpen = true;
	}

	async function submitAddAccount(e: SubmitEvent) {
		e.preventDefault();
		const em = newEmail.trim();
		if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(em)) {
			toast.warning(t('mail.toasts.invalidEmail'));
			return;
		}
		if (!newEmailPass) {
			toast.warning(t('mail.toasts.passwordRequired'));
			return;
		}
		addingAcc = true;
		try {
			await api.addMailAccount(em, newEmailPass);
			toast.success(t('mail.toasts.accountCreated', { email: em }));
			addAccOpen = false;
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('mail.toasts.accountCreateError'));
		} finally {
			addingAcc = false;
		}
	}

	function openEditAccountPass(email: string) {
		editPassEmail = email;
		editPassValue = '';
		editPassOpen = true;
	}

	async function submitEditAccountPass(e: SubmitEvent) {
		e.preventDefault();
		const em = editPassEmail.trim();
		if (!em) return;
		if (!editPassValue.trim()) {
			toast.warning(t('mail.toasts.newPasswordRequired'));
			return;
		}
		if (editPassValue.trim().length < 8) {
			toast.warning(t('mail.toasts.newPasswordMinLength'));
			return;
		}
		editingPass = true;
		try {
			const res = await api.changeMailAccountPassword(em, editPassValue.trim());
			toast.success(res.message || t('mail.toasts.accountPasswordUpdated', { email: em }));
			editPassOpen = false;
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('mail.toasts.accountPasswordError'));
		} finally {
			editingPass = false;
		}
	}

	async function deleteAccount(email: string) {
		const ok = await cp.confirm({
			title: t('mail.confirm.deleteAccTitle'),
			description: t('mail.confirm.deleteAccDesc', { email }),
			confirmText: t('mail.confirm.deleteAccBtn'),
			destructive: true
		});
		if (!ok) return;
		accountPending = { ...accountPending, [email]: true };
		try {
			const res = await api.deleteMailAccount(email);
			toast.success(res.message || t('mail.toasts.accountDeleted', { email }));
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('mail.toasts.accountDeleteError'));
		} finally {
			const next = { ...accountPending };
			delete next[email];
			accountPending = next;
		}
	}

	function openDeleteDomainModal(domain: string) {
		deleteDomTarget = domain;
		deleteDomConfirm = '';
		deleteDomDNS = false;
		deleteDomOpen = true;
	}

	async function submitDeleteDomain(e: SubmitEvent) {
		e.preventDefault();
		if (deleteDomConfirm.trim() !== deleteDomTarget) {
			toast.warning(t('mail.toasts.confirmDomainMismatch'));
			return;
		}
		deletingDom = true;
		try {
			const report = await api.deleteMailDomain(deleteDomTarget, deleteDomDNS);
			deleteDomOpen = false;
			const summary = (report.checks || []).map((c) => `${c.name}: ${c.detail}`).join(' | ');
			toast.success(`${t('mail.toasts.domainDeleted', { domain: deleteDomTarget })}${summary ? ` (${summary})` : ''}`);
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('mail.toasts.domainDeleteError'));
		} finally {
			deletingDom = false;
		}
	}

	async function repairDomain(domain: string) {
		const ok = await cp.confirm({
			title: t('mail.confirm.repairTitle'),
			description: t('mail.confirm.repairDesc', { domain }),
			confirmText: t('mail.confirm.repairBtn')
		});
		if (!ok) return;
		domainPending = { ...domainPending, [domain]: 'repair' };
		try {
			await api.repairMailDomain(domain);
			cp.openTerminal(t('mail.terminal.repairTitle', { domain }));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('mail.toasts.repairError'));
		} finally {
			const next = { ...domainPending };
			delete next[domain];
			domainPending = next;
		}
	}

	async function resetAdmin(domain: string) {
		const ok = await cp.confirm({
			title: t('mail.confirm.resetAdminTitle'),
			description: t('mail.confirm.resetAdminDesc', { domain }),
			confirmText: t('mail.confirm.resetAdminBtn')
		});
		if (!ok) return;
		domainPending = { ...domainPending, [domain]: 'reset-admin' };
		try {
			await api.resetMailAdmin(domain);
			toast.success(t('mail.toasts.resetAdminSuccess', { domain }));
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('mail.toasts.resetAdminError'));
		} finally {
			const next = { ...domainPending };
			delete next[domain];
			domainPending = next;
		}
	}

	async function loadRelayDNSRecords(domain: string, provider: string) {
		if (!domain || domain === 'global' || provider === 'local') {
			relayDNSText = '';
			relayDNSBaseline = '';
			return;
		}
		try {
			const list = await api.listMailRelayDNS(domain);
			const formatted = (list ?? [])
				.filter((r) => !provider || r.provider.toLowerCase() === provider)
				.map((r) => `${r.type} ${r.name} ${r.content}`)
				.join('\n');
			relayDNSText = formatted;
			relayDNSBaseline = formatted;
		} catch {
			relayDNSText = '';
			relayDNSBaseline = '';
		}
	}

	function openRelayModal(domain = 'global') {
		relayTargetDomain = domain || 'global';
		relaySyncNotice = null;
		const outboundRoute =
			relayTargetDomain === 'global' ? '' : mailStatus?.outbound_routes?.[relayTargetDomain];
		const existing =
			relayTargetDomain === 'global'
				? mailStatus?.global_relay
				: mailStatus?.domain_relays?.[relayTargetDomain] ?? mailStatus?.global_relay;

		relaySecret = '';
		relayPass = '';
		if (relayTargetDomain !== 'global' && outboundRoute === 'mx' && !mailStatus?.domain_relays?.[relayTargetDomain]?.host) {
			relayProvider = 'local';
			relayHost = '';
			relayPortStr = '587';
			relayUser = '';
		} else if (existing?.host) {
			const p = existing.provider.toLowerCase();
			relayProvider = p === 'resend' || p === 'brevo' || p === 'custom' ? p : 'custom';
			relayHost = existing.host || '';
			relayPortStr = String(existing.port || 587);
			relayUser = existing.username || '';
		} else {
			relayProvider = 'local';
			relayHost = '';
			relayPortStr = '587';
			relayUser = '';
		}
		relayOpen = true;
		loadRelayDNSRecords(relayTargetDomain, relayProvider);
	}

	function formatRelayDNSFeedback(dns?: RelayDNSReport): string {
		if (!dns) return '';
		const parts: string[] = [];
		if (dns.removed?.length) parts.push(t('mail.toasts.relayDnsRemoved', { count: dns.removed.length }));
		if (dns.added?.length) parts.push(t('mail.toasts.relayDnsAdded', { count: dns.added.length }));
		if (dns.kept?.length) parts.push(t('mail.toasts.relayDnsKept', { count: dns.kept.length }));
		if (dns.verification_triggered) parts.push(t('mail.toasts.resendVerified'));
		if (dns.manual?.length && !dns.added?.length && !dns.kept?.length) {
			parts.push(dns.manual[0]);
		} else if (dns.manual?.length && dns.manual[0].toLowerCase().includes('api key')) {
			parts.push(dns.manual[0]);
		}
		return parts.join('; ');
	}

	async function handleSyncResendDNS() {
		if (relayTargetDomain === 'global' || relayProvider !== 'resend') return;
		syncingRelayDNS = true;
		relaySyncNotice = null;
		try {
			const res = await api.syncMailRelayDNS(relayTargetDomain, relaySecret.trim(), 'resend');
			await loadRelayDNSRecords(relayTargetDomain, 'resend');
			const rep = res.report;
			const parts: string[] = [];
			if (rep?.added?.length) parts.push(t('mail.toasts.resendAddedDns', { count: rep.added.length }));
			if (rep?.kept?.length) parts.push(t('mail.toasts.resendKeptDns', { count: rep.kept.length }));
			if (rep?.removed?.length) parts.push(t('mail.toasts.resendRemovedDns', { count: rep.removed.length }));
			if (res.verification_triggered) parts.push(t('mail.toasts.resendVerified'));
			const msg = parts.join(', ') || t('mail.toasts.resendSyncDone');
			const warnNote = rep?.manual?.find((m) => m.toLowerCase().includes('gagal') || m.toLowerCase().includes('menunggu'));
			relaySyncNotice = {
				tone: warnNote ? 'warn' : 'info',
				text: warnNote ? `${msg}. (${warnNote})` : t('mail.toasts.resendSyncSuccessText', { msg, status: res.domain_status || 'pending' })
			};
			toast.success(t('mail.toasts.resendSyncToast', { domain: relayTargetDomain, msg }));
			await loadMail();
		} catch (err) {
			const text = err instanceof Error ? err.message : t('mail.toasts.resendSyncError');
			relaySyncNotice = { tone: 'warn', text };
			toast.error(text);
		} finally {
			syncingRelayDNS = false;
		}
	}

	async function submitRelay(e: SubmitEvent) {
		e.preventDefault();
		const payload: {
			domain: string;
			provider: string;
			host?: string;
			port?: number;
			username?: string;
			password?: string;
		} = {
			domain: relayTargetDomain,
			provider: relayProvider
		};

		if (relayProvider === 'local') {
			// Menggunakan SMTP lokal Stalwart (direct MX); tidak butuh host/kredensial relay eksternal.
		} else if (relayProvider === 'resend' || relayProvider === 'brevo') {
			const typed = relaySecret.trim();
			if (!typed && !hasStoredRelayKey) {
				toast.warning(t('mail.toasts.relayKeyRequired', { provider: relayProvider.toUpperCase() }));
				return;
			}
			if (typed) {
				payload.password = typed;
			}
		} else {
			if (!relayHost.trim()) {
				toast.warning(t('mail.toasts.relayHostRequired'));
				return;
			}
			payload.host = relayHost.trim();
			payload.port = parseInt(relayPortStr, 10) || 587;
			payload.username = relayUser.trim();
			payload.password = relayPass;
		}

		savingRelay = true;
		try {
			const res = await api.setMailRelay(payload);
			const lines = relayDNSText
				.split('\n')
				.map((l) => l.trim())
				.filter(Boolean);
			if (
				relayProvider !== 'local' &&
				lines.length > 0 &&
				relayTargetDomain !== 'global' &&
				relayDNSText.trim() !== relayDNSBaseline.trim()
			) {
				const parsed = lines.map((line) => {
					const parts = line.split(/\s+/);
					if (parts.length < 3) {
						throw new Error(t('mail.toasts.relayInvalidLine', { line }));
					}
					return {
						type: parts[0].toUpperCase(),
						name: parts[1],
						content: parts.slice(2).join(' ')
					};
				});
				await api.addMailRelayDNS(relayTargetDomain, relayProvider, parsed);
			}
			const dnsNote = formatRelayDNSFeedback(res.dns);
			toast.success(
				relayProvider === 'local'
					? `${relayTargetDomain === 'global' ? t('mail.toasts.relayLocalDefaultSaved') : t('mail.toasts.relayLocalDomainSaved', { domain: relayTargetDomain })}${dnsNote ? ` (${dnsNote})` : ''}`
					: `${t('mail.toasts.relayDomainSaved', { domain: relayTargetDomain })}${dnsNote ? ` (${dnsNote})` : ''}`
			);
			if (res.dns?.manual?.some((m) => m.includes('API Key Resend tidak diizinkan') || m.includes('gagal terhubung'))) {
				toast.warning(res.dns.manual[0]);
			}
			relayOpen = false;
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('mail.toasts.relaySaveError'));
		} finally {
			savingRelay = false;
		}
	}

	async function removeRelayOverride(domain: string) {
		const fallbackDesc = mailStatus?.global_relay?.host
			? t('mail.confirm.fallbackRelay', { provider: mailStatus.global_relay.provider.toUpperCase() })
			: t('mail.confirm.fallbackLocalSmtp');
		const ok = await cp.confirm({
			title: t('mail.confirm.removeOverrideTitle'),
			description: t('mail.confirm.removeOverrideDesc', { domain, fallback: fallbackDesc }),
			confirmText: t('mail.confirm.removeOverrideBtn'),
			destructive: true
		});
		if (!ok) return;
		domainPending = { ...domainPending, [domain]: 'unset-relay' };
		try {
			await api.removeMailRelay(domain);
			toast.success(t('mail.toasts.overrideRemoved', { domain }));
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('mail.toasts.overrideRemoveError'));
		} finally {
			const next = { ...domainPending };
			delete next[domain];
			domainPending = next;
		}
	}

	async function refreshPreflight(dom: string) {
		preflightLoading = true;
		try {
			preflight = await api.getMailPreflight(dom);
		} catch {
			preflight = null;
		} finally {
			preflightLoading = false;
		}
	}

	async function openSetupModal() {
		setupSSL = true;
		setupRelayProv = '';
		setupResendKey = '';
		setupBrevoUser = '';
		setupBrevoKey = '';
		setupCustomHost = '';
		setupCustomPortStr = '587';
		setupCustomUser = '';
		setupCustomPass = '';
		setupCustomDomain = '';

		if (zoneDomains.length > 0) {
			const configured = new Set((mailStatus?.domains ?? []).map((d) => d.domain));
			const ready = zoneDomains.find((d) => !configured.has(d));
			setupDomainChoice = ready ?? zoneDomains[0];
		} else {
			setupDomainChoice = '__custom__';
		}

		setupOpen = true;
		await refreshPreflight(effectiveSetupDomain);
	}

	async function submitSetupMail(e: SubmitEvent) {
		e.preventDefault();
		const domain = effectiveSetupDomain;
		if (!domain) {
			toast.warning(t('mail.toasts.setupDomainRequired'));
			return;
		}
		let relayHost = '';
		let relayPort = 587;
		let relayUser = '';
		let relayPass = '';

		if (setupRelayProv === 'resend') {
			relayPass = setupResendKey.trim();
			if (!relayPass) {
				toast.warning(t('mail.toasts.setupResendKeyRequired'));
				return;
			}
		} else if (setupRelayProv === 'brevo') {
			relayUser = setupBrevoUser.trim();
			relayPass = setupBrevoKey.trim();
			if (!relayPass) {
				toast.warning(t('mail.toasts.setupBrevoKeyRequired'));
				return;
			}
		} else if (setupRelayProv === 'custom') {
			relayHost = setupCustomHost.trim();
			relayPort = parseInt(setupCustomPortStr, 10) || 587;
			relayUser = setupCustomUser.trim();
			relayPass = setupCustomPass;
			if (!relayHost) {
				toast.warning(t('mail.toasts.setupCustomHostRequired'));
				return;
			}
		}

		setupOpen = false;
		const ok = await cp.confirm({
			title: t('mail.confirm.setupTitle'),
			description: t('mail.confirm.setupDesc', { domain }),
			confirmText: t('mail.confirm.setupBtn')
		});
		if (!ok) {
			setupOpen = true;
			return;
		}

		settingUp = true;
		try {
			await api.setupMailDomain({
				domain,
				ssl: setupSSL,
				relay_provider: setupRelayProv,
				relay_host: relayHost,
				relay_port: relayPort,
				relay_user: relayUser,
				relay_pass: relayPass
			});
			cp.openTerminal(t('mail.terminal.setupTitle', { domain }));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('mail.toasts.setupError'));
			setupOpen = true;
		} finally {
			settingUp = false;
		}
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<!-- Page Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div class="min-w-0 max-w-2xl">
			{#if modeStore.mode === 'vps'}
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('mail.titleVps')}</h1>
				<p class="text-sm text-muted-foreground">
					{t('mail.descVps')}
				</p>
			{:else if modeStore.mode === 'local'}
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('mail.titleLocal')}</h1>
				<p class="text-sm text-muted-foreground">
					{t('mail.descLocal')}
				</p>
			{:else}
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('mail.titleEdge')}</h1>
				<p class="text-sm text-muted-foreground">
					{t('mail.descEdge')}
				</p>
			{/if}
		</div>
		<div class="flex shrink-0 flex-wrap items-center gap-2">
			<StatusBadge
				tone={mailStatus?.stalwart_active ? 'success' : 'warning'}
				label={mailStatus?.stalwart_active ? t('mail.badges.stackActive') : t('mail.badges.stackInactive')}
			/>
			<Button
				variant="outline"
				size="sm"
				disabled={refreshing}
				onclick={() => loadMail(true)}
			>
				{#if refreshing}
					<Spinner data-icon="inline-start" />
					{t('mail.buttons.refreshing')}
				{:else}
					<RefreshCwIcon data-icon="inline-start" />
					{t('mail.buttons.refresh')}
				{/if}
			</Button>
			<Button variant="outline" size="sm" onclick={() => openRelayModal('global')}>
				<SettingsIcon data-icon="inline-start" />
				{t('mail.buttons.setRelay')}
			</Button>
			<Button size="sm" onclick={openSetupModal}>
				<MailPlusIcon data-icon="inline-start" />
				{t('mail.buttons.setupDomain')}
			</Button>
		</div>
	</div>

	<!-- Contextual Alerts -->
	{#if modeStore.mode === 'local'}
		<Alert.Root class="border-muted-foreground/30 bg-muted/40">
			<InfoIcon />
			<Alert.Title>{t('mail.alerts.localTitle')}</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					{t('mail.alerts.localDescPre')} <strong>{t('mail.alerts.localDescBold')}</strong>{t('mail.alerts.localDescPost')}
				</span>
				<Button size="xs" variant="outline" class="shrink-0" onclick={() => modeStore.openModal()}>
					{t('mail.alerts.switchMode')}
				</Button>
			</Alert.Description>
		</Alert.Root>
	{/if}

	{#if mailStatus && (!mailStatus.stalwart_active || !mailStatus.snappymail_active || mailStatus.domains.length === 0)}
		<Alert.Root>
			<InfoIcon />
			<Alert.Title>
				{#if mailStatus.domains.length > 0 && (!mailStatus.stalwart_active || !mailStatus.snappymail_active)}
					{t('mail.alerts.stackIncompleteTitle')}
				{:else}
					{t('mail.alerts.setupPromptTitle')}
				{/if}
			</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					{#if mailStatus.domains.length > 0 && (!mailStatus.stalwart_active || !mailStatus.snappymail_active)}
						{t('mail.alerts.stackIncompleteDesc')}
					{:else if modeStore.mode === 'local'}
						{t('mail.alerts.setupPromptLocalDesc')}
					{:else}
						{t('mail.alerts.setupPromptPublicDesc')}
					{/if}
				</span>
				<div class="flex flex-wrap gap-2">
					{#if !mailStatus.stalwart_active || !mailStatus.snappymail_active}
						<Button size="xs" variant="outline" onclick={() => cp.triggerInstall('mail-stack')}>
							{t('mail.alerts.installMailStack')}
						</Button>
					{/if}
					<Button size="xs" onclick={openSetupModal}>{t('mail.alerts.startSetupDomain')}</Button>
				</div>
			</Alert.Description>
		</Alert.Root>
	{/if}

	<!-- Unified Mail Server Topology & Status Strip (PowerDNS Style) -->
	<Card.Root class="gap-0 py-0">
		<div class="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/30 px-4 py-2.5">
			<div class="flex items-center gap-2">
				<MailIcon class="size-4 text-primary" aria-hidden="true" />
				<span class="text-sm font-semibold">{t('mail.topology.title')}</span>
				<Badge variant="outline" class="font-normal">
					{(mailStatus?.mode || modeStore.mode) === 'vps'
						? t('mail.topology.modeVps')
						: (mailStatus?.mode || modeStore.mode) === 'local'
							? t('mail.topology.modeLocal')
							: t('mail.topology.modeEdge')}
				</Badge>
			</div>
			<div class="flex flex-wrap items-center gap-3 text-xs tabular-nums text-muted-foreground">
				<span><strong class="font-semibold text-foreground">{allMailDomains.length}</strong> {t('mail.topology.domainsLabel')}</span>
				<span><strong class="font-semibold text-foreground">{totalAccounts}</strong> {t('mail.topology.accountsLabel')}</span>
				<span><strong class="font-semibold text-foreground">{openPortCount}/{stalwartPorts.length}</strong> {t('mail.topology.portsActiveLabel')}</span>
			</div>
		</div>

		<div class="grid divide-y sm:grid-cols-2 lg:grid-cols-4 sm:divide-x sm:divide-y-0">
			<!-- Col 1: Stalwart Engine & Ports -->
			<div class="flex flex-col gap-2 p-4">
				<div class="flex items-center justify-between gap-2">
					<span class="text-xs text-muted-foreground">{t('mail.topology.stalwartEngine')}</span>
					<StatusBadge
						tone={mailStatus?.stalwart_active ? 'success' : 'warning'}
						label={mailStatus?.stalwart_active ? t('mail.topology.engineActive') : t('mail.topology.engineInactive')}
					/>
				</div>
				<div class="flex items-center justify-between gap-1 text-[11px] text-muted-foreground">
					<span>{t('mail.topology.listenerPorts')}</span>
					<span class="font-mono tabular-nums">
						{#if !mailStatus}
							...
						{:else if (mailStatus.mode || modeStore.mode) === 'local'}
							{t('mail.topology.portsLocalCount', { open: openPortCount, total: stalwartPorts.length })}
						{:else}
							{t('mail.topology.portsPublicCount', { pub: publicOpenCount, pubTotal: publicMailPorts.length, open: openPortCount, total: stalwartPorts.length })}
						{/if}
					</span>
				</div>
				<div class="flex flex-wrap gap-1" aria-label={t('mail.topology.portsAriaLabel')}>
					{#each stalwartPorts as item (item.port)}
						{@const isLocalMode = (mailStatus?.mode || modeStore.mode) === 'local'}
						{@const isInternal = Boolean(item.internal_only || item.port === 8080)}
						{@const isInsideOpen = Boolean(item.local ?? item.open)}
						{@const isOutsideOpen = isLocalMode
							? isInsideOpen && !isInternal
							: isInsideOpen && !isInternal && Boolean(item.external_checked) && Boolean(item.public)}
						{@const isOutsideBlocked = !isLocalMode && item.open && !isInternal && Boolean(item.external_checked) && !item.public}
						{@const isLocalUnverified = !isLocalMode && item.open && !isInternal && !item.external_checked}
						<span
							title={!mailStatus
								? t('mail.topology.portChecking', { label: item.label, port: item.port })
								: !item.open
									? t('mail.topology.portDead', { label: item.label, port: item.port })
									: isInternal
										? t('mail.topology.portInternal', { label: item.label, port: item.port })
										: isLocalMode
											? t('mail.topology.portLocalActive', { label: item.label, port: item.port })
											: isOutsideOpen
												? t('mail.topology.portOutsideOpen', { label: item.label, port: item.port })
												: isOutsideBlocked
													? t('mail.topology.portOutsideBlocked', { label: item.label, port: item.port })
													: t('mail.topology.portUnverified', { label: item.label, port: item.port })}
							class={!mailStatus
								? 'inline-flex items-center gap-1 rounded-md border border-border bg-secondary px-1.5 py-0.5 font-mono text-[10.5px] font-medium text-secondary-foreground tabular-nums'
								: !item.open
									? 'inline-flex items-center gap-1 rounded-md border border-destructive/35 bg-destructive/15 px-1.5 py-0.5 font-mono text-[10.5px] font-medium text-destructive tabular-nums'
									: isInternal
										? 'inline-flex items-center gap-1 rounded-md border border-border bg-secondary px-1.5 py-0.5 font-mono text-[10.5px] font-medium text-secondary-foreground tabular-nums'
										: isOutsideOpen
											? 'inline-flex items-center gap-1 rounded-md border border-success/35 bg-success/15 px-1.5 py-0.5 font-mono text-[10.5px] font-medium text-success tabular-nums'
											: 'inline-flex items-center gap-1 rounded-md border border-amber-500/35 bg-amber-500/15 px-1.5 py-0.5 font-mono text-[10.5px] font-medium text-amber-700 dark:text-amber-400 tabular-nums'}
						>
							<span
								class={!mailStatus
									? 'size-1.5 shrink-0 rounded-full bg-muted-foreground/60'
									: !item.open
										? 'size-1.5 shrink-0 rounded-full bg-destructive'
										: isInternal
											? 'size-1.5 shrink-0 rounded-full bg-muted-foreground'
											: isOutsideOpen
												? 'size-1.5 shrink-0 rounded-full bg-success'
												: isOutsideBlocked || isLocalUnverified
													? 'size-1.5 shrink-0 rounded-full bg-amber-500'
													: 'size-1.5 shrink-0 rounded-full bg-muted-foreground'}
							></span>
							<span>{item.port}</span>
						</span>
					{/each}
				</div>
			</div>

			<!-- Col 2: SnappyMail Webmail -->
			<div class="flex flex-col justify-between gap-2 p-4">
				<div class="flex flex-col gap-1">
					<div class="flex items-center justify-between gap-2">
						<span class="text-xs text-muted-foreground">{t('mail.topology.snappymailTitle')}</span>
						<StatusBadge
							tone={mailStatus?.snappymail_active ? 'success' : 'warning'}
							label={mailStatus?.snappymail_active ? t('mail.topology.snappymailActive') : t('mail.topology.snappymailInactive')}
						/>
					</div>
					<p class="truncate font-mono text-xs text-muted-foreground">/var/www/snappymail</p>
				</div>
				<p class="text-[11px] text-muted-foreground">
					{t('mail.topology.snappymailDesc')}
				</p>
			</div>

			<!-- Col 3: Storage -->
			<div class="flex flex-col justify-between gap-2 p-4">
				<div class="flex flex-col gap-1">
					<span class="text-xs text-muted-foreground">{t('mail.topology.storageTitle')}</span>
					<div class="flex items-baseline gap-1.5">
						<span class="font-mono text-base font-semibold tabular-nums">
							{mailStatus?.storage_free ? t('mail.topology.storageFreeValue', { free: mailStatus.storage_free }) : '-'}
						</span>
					</div>
				</div>
				<p class="text-[11px] text-muted-foreground">{t('mail.topology.storageDesc')}</p>
			</div>

			<!-- Col 4: Relay Outbound Default -->
			<div class="flex flex-col justify-between gap-2 p-4">
				<div class="flex flex-col gap-1">
					<div class="flex items-center justify-between gap-2">
						<span class="text-xs text-muted-foreground">{t('mail.topology.defaultRelayTitle')}</span>
						<StatusBadge
							tone={mailStatus?.global_relay?.host ? 'info' : 'success'}
							label={mailStatus?.global_relay?.host
								? (mailStatus.global_relay.provider?.toUpperCase() || t('mail.topology.relayActiveFallback'))
								: t('mail.topology.localSmtpBadge')}
						/>
					</div>
					<p class="truncate font-mono text-xs text-muted-foreground">
						{#if mailStatus?.global_relay?.host}
							{mailStatus.global_relay.host}:{mailStatus.global_relay.port}
						{:else}
							{t('mail.topology.localSmtpDirect')}
						{/if}
					</p>
				</div>
				<div class="flex items-center justify-between text-[11px] text-muted-foreground">
					<span>{t('mail.topology.overrideCount', { count: Object.keys(mailStatus?.domain_relays || {}).length })}</span>
					<Button
						variant="ghost"
						size="xs"
						class="h-6 px-1.5 text-[11px]"
						onclick={() => openRelayModal('global')}
					>
						{t('mail.topology.changeRelay')}
					</Button>
				</div>
			</div>
		</div>
	</Card.Root>

	<!-- Pengelolaan Mail Server (Tabbed) -->
	<Card.Root class="gap-0 py-0">
		<Tabs.Root value={activeTab} onValueChange={handleTabChange} class="gap-0">
			<div class="flex flex-col border-b">
				<div class="flex flex-col gap-3 px-4 pt-3.5 pb-2.5 sm:flex-row sm:items-center sm:justify-between">
					<div>
						{#if activeTab === 'domains'}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">{t('mail.tabs.domainsHeading')}</h2>
								<Badge variant="secondary" class="tabular-nums">{allMailDomains.length}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								{t('mail.tabs.domainsSubtitle')}
							</p>
						{:else if activeTab === 'accounts'}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">{t('mail.tabs.accountsHeading')}</h2>
								<Badge variant="secondary" class="tabular-nums">{flatAccounts.length}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								{t('mail.tabs.accountsSubtitle')}
							</p>
						{:else}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">{t('mail.tabs.portsHeading')}</h2>
								<Badge variant="secondary" class="tabular-nums">{openPortCount}/{stalwartPorts.length} {t('powerdns.zonesTab.statusActive')}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								{t('mail.tabs.portsSubtitle')}
							</p>
						{/if}
					</div>

					<div class="flex flex-wrap items-center gap-2">
						<Button
							variant="outline"
							size="sm"
							disabled={refreshing}
							onclick={() => loadMail(true)}
						>
							{#if refreshing}
								<Spinner data-icon="inline-start" />
								{t('mail.buttons.refreshing')}
							{:else}
								<RefreshCwIcon data-icon="inline-start" />
								{t('mail.buttons.refresh')}
							{/if}
						</Button>
						{#if activeTab === 'domains'}
							<Button variant="outline" size="sm" onclick={() => openRelayModal('global')}>
								<SettingsIcon data-icon="inline-start" />
								{t('mail.buttons.setRelay')}
							</Button>
							<Button size="sm" onclick={openSetupModal}>
								<MailPlusIcon data-icon="inline-start" />
								{t('mail.buttons.setupDomain')}
							</Button>
						{:else if activeTab === 'accounts'}
							<Button size="sm" onclick={() => openAddAccount(domainFilter)}>
								<UserPlusIcon data-icon="inline-start" />
								{t('mail.buttons.addEmail')}
							</Button>
						{/if}
					</div>
				</div>

				<div class="px-4">
					<Tabs.List
						variant="line"
						aria-label={t('mail.tabs.ariaLabel')}
						class="h-11 group-data-horizontal/tabs:h-11 w-full flex-nowrap items-stretch justify-start gap-1.5 overflow-x-auto overflow-y-hidden rounded-none bg-transparent p-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden sm:gap-2"
					>
						<Tabs.Trigger
							value="domains"
							class="h-full flex-none shrink-0 whitespace-nowrap rounded-none border-0 px-2.5 py-0 text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 focus-visible:ring-0 data-active:text-foreground group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-0 group-data-horizontal/tabs:after:h-0 after:border-b-2 after:border-primary after:bg-transparent sm:px-3"
						>
							<GlobeIcon aria-hidden="true" class="size-3.5" />
							<span>{t('mail.tabs.tabDomains')}</span>
							<span class="rounded-full bg-muted px-1.5 py-0.5 text-[11px] leading-none font-medium tabular-nums text-muted-foreground">
								{allMailDomains.length}
							</span>
						</Tabs.Trigger>

						<Tabs.Trigger
							value="accounts"
							class="h-full flex-none shrink-0 whitespace-nowrap rounded-none border-0 px-2.5 py-0 text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 focus-visible:ring-0 data-active:text-foreground group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-0 group-data-horizontal/tabs:after:h-0 after:border-b-2 after:border-primary after:bg-transparent sm:px-3"
						>
							<UsersIcon aria-hidden="true" class="size-3.5" />
							<span>{t('mail.tabs.tabAccounts')}</span>
							<span class="rounded-full bg-muted px-1.5 py-0.5 text-[11px] leading-none font-medium tabular-nums text-muted-foreground">
								{flatAccounts.length}
							</span>
						</Tabs.Trigger>

						<Tabs.Trigger
							value="ports"
							class="h-full flex-none shrink-0 whitespace-nowrap rounded-none border-0 px-2.5 py-0 text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 focus-visible:ring-0 data-active:text-foreground group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-0 group-data-horizontal/tabs:after:h-0 after:border-b-2 after:border-primary after:bg-transparent sm:px-3"
						>
							<ShieldCheckIcon aria-hidden="true" class="size-3.5" />
							<span>{t('mail.tabs.tabPorts')}</span>
							<span class="rounded-full bg-muted px-1.5 py-0.5 text-[11px] leading-none font-medium tabular-nums text-muted-foreground">
								{openPortCount}/{stalwartPorts.length}
							</span>
						</Tabs.Trigger>
					</Tabs.List>
				</div>
			</div>

			<!-- Tab 1: Status Domain & Outbound SMTP -->
			<Tabs.Content value="domains" class="mt-0">
				<div>
					<div class="flex flex-col gap-2.5 border-b bg-muted/20 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
						<search class="w-full sm:max-w-xs">
							<InputGroup.Root class="h-8 bg-background">
								<InputGroup.Addon align="inline-start">
									<SearchIcon />
								</InputGroup.Addon>
								<InputGroup.Input
									type="search"
									placeholder={t('mail.domainsTab.searchPlaceholder')}
									aria-label={t('mail.domainsTab.searchAria')}
									bind:value={domainSearch}
								/>
							</InputGroup.Root>
						</search>

						<div class="flex items-center gap-2">
							<span class="text-xs text-muted-foreground">
								{t('mail.domainsTab.countLabel', { shown: filteredMailDomains.length, total: allMailDomains.length })}
							</span>
						</div>
					</div>

					{#if allMailDomains.length === 0}
						<div class="p-4">
							<Empty.Root class="border-0 py-8">
								<Empty.Header>
									<Empty.Media variant="icon">
										<GlobeIcon />
									</Empty.Media>
									<Empty.Title>{t('mail.domainsTab.emptyTitle')}</Empty.Title>
									<Empty.Description>
										{t('mail.domainsTab.emptyDesc')}
									</Empty.Description>
								</Empty.Header>
								<Empty.Content>
									<Button size="sm" onclick={openSetupModal}>
										<MailPlusIcon data-icon="inline-start" />
										{t('mail.domainsTab.emptyBtn')}
									</Button>
								</Empty.Content>
							</Empty.Root>
						</div>
					{:else if filteredMailDomains.length === 0}
						<div class="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center">
							<p class="text-sm text-muted-foreground">{t('mail.domainsTab.noMatch', { query: domainSearch })}</p>
							<Button variant="outline" size="xs" onclick={() => (domainSearch = '')}>
								{t('mail.domainsTab.resetSearch')}
							</Button>
						</div>
					{:else}
						<div class="p-3 sm:p-0">
							<ResponsiveTableWrapper label={t('mail.domainsTab.tableAria')} class="rounded-none border-0 bg-transparent">
								<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
									<Table.Header>
										<Table.Row>
											<Table.Head scope="col">{t('mail.domainsTab.colDomain')}</Table.Head>
											<Table.Head scope="col">{t('mail.domainsTab.colProvider')}</Table.Head>
											<Table.Head scope="col">{t('mail.domainsTab.colHost')}</Table.Head>
											<Table.Head scope="col">{t('mail.domainsTab.colUsername')}</Table.Head>
											<Table.Head scope="col">{t('mail.domainsTab.colAdmin')}</Table.Head>
											<Table.Head scope="col">{t('mail.domainsTab.colWebmail')}</Table.Head>
											<Table.Head scope="col" class="text-right">{t('mail.domainsTab.colActions')}</Table.Head>
										</Table.Row>
									</Table.Header>
									<Table.Body>
										{#each paginatedMailDomains as dom (dom.domain)}
											{@const domRelay = mailStatus?.domain_relays?.[dom.domain]}
											{@const outboundRoute = mailStatus?.outbound_routes?.[dom.domain]}
											{@const isLocalOverride = outboundRoute === 'mx' && !domRelay?.host}
											{@const activeRelay = isLocalOverride ? null : (domRelay?.host ? domRelay : mailStatus?.global_relay)}
											{@const isOverride = Boolean(domRelay?.host) || isLocalOverride}
											{@const pendingAction = domainPending[dom.domain]}
											<Table.Row>
												<Table.Cell data-label={t('mail.domainsTab.colDomain')} class="font-semibold">{dom.domain}</Table.Cell>
												<Table.Cell data-label={t('mail.domainsTab.dataLabelProvider')}>
													{#if isLocalOverride}
														<StatusBadge tone="success" label={t('mail.domainsTab.badgeLocalDirect')} />
													{:else if activeRelay?.host}
														<StatusBadge
															tone={activeRelay.provider === 'resend'
																? 'info'
																: activeRelay.provider === 'brevo'
																	? 'success'
																	: 'warning'}
															label={`${isOverride ? '' : t('mail.domainsTab.badgeDefaultPrefix')}${activeRelay.provider.toUpperCase()}`}
														/>
													{:else}
														<StatusBadge tone="success" label={t('mail.domainsTab.badgeDefaultLocal')} />
													{/if}
												</Table.Cell>
												<Table.Cell data-label={t('mail.domainsTab.dataLabelHostPort')} class="font-mono text-xs">
													{#if isLocalOverride}
														{t('mail.topology.localSmtpDirect')}
													{:else if activeRelay?.host}
														{activeRelay.host}:{activeRelay.port}
													{:else}
														{t('mail.topology.localSmtpDirect')}
													{/if}
												</Table.Cell>
												<Table.Cell data-label={t('mail.domainsTab.dataLabelUsername')}>
													<div class="text-xs">{activeRelay?.username || '-'}</div>
													<div class="text-[11px] text-muted-foreground">
														{#if isLocalOverride}
															{t('mail.domainsTab.subLocalOverride')}
														{:else if domRelay?.host}
															{t('mail.domainsTab.subDomainOverride')}
														{:else if activeRelay?.host}
															{t('mail.domainsTab.subFollowsDefault')}
														{:else}
															{t('mail.domainsTab.subDefaultLocal')}
														{/if}
													</div>
												</Table.Cell>
												<Table.Cell data-label={t('mail.domainsTab.dataLabelAdmin')}>
													<Button
														variant="outline"
														size="xs"
														href={dom.admin_url}
														target="_blank"
														rel="noopener noreferrer"
													>
														{t('mail.domainsTab.btnPanel')}
														<ExternalLinkIcon data-icon="inline-end" />
													</Button>
												</Table.Cell>
												<Table.Cell data-label={t('mail.domainsTab.colWebmail')}>
													<Button
														variant="outline"
														size="xs"
														href={dom.webmail}
														target="_blank"
														rel="noopener noreferrer"
													>
														{t('mail.domainsTab.btnWebmail')}
														<ExternalLinkIcon data-icon="inline-end" />
													</Button>
												</Table.Cell>
												<Table.Cell data-label={t('mail.domainsTab.colActions')} class="text-right">
													<div class="flex justify-end">
														<DropdownMenu.Root>
															<DropdownMenu.Trigger>
																{#snippet child({ props })}
																	<Button
																		variant="outline"
																		size="xs"
																		disabled={Boolean(pendingAction)}
																		{...props}
																	>
																		{#if pendingAction === 'repair'}
																			<Spinner data-icon="inline-start" />
																			{t('mail.domainsTab.pendingRepair')}
																		{:else if pendingAction === 'reset-admin'}
																			<Spinner data-icon="inline-start" />
																			{t('mail.domainsTab.pendingReset')}
																		{:else if pendingAction === 'unset-relay'}
																			<Spinner data-icon="inline-start" />
																			{t('mail.domainsTab.pendingUnset')}
																		{:else}
																			{t('mail.domainsTab.colActions')}
																			<ChevronDownIcon data-icon="inline-end" />
																		{/if}
																	</Button>
																{/snippet}
															</DropdownMenu.Trigger>
															<DropdownMenu.Content align="end" class="w-48">
																<DropdownMenu.Group>
																	<DropdownMenu.Label class="text-xs text-muted-foreground">
																		{dom.domain}
																	</DropdownMenu.Label>
																	<DropdownMenu.Separator />
																	<DropdownMenu.Item onclick={() => openRelayModal(dom.domain)}>
																		<SettingsIcon />
																		<span>{isOverride ? t('mail.domainsTab.actionEditRelay') : t('mail.domainsTab.actionSetRelay')}</span>
																	</DropdownMenu.Item>
																	{#if isOverride}
																		<DropdownMenu.Item
																				disabled={Boolean(pendingAction)}
																onclick={() => removeRelayOverride(dom.domain)}
																		>
																			<RefreshCwIcon />
																			<span>{t('mail.domainsTab.actionRemoveOverride')}</span>
																		</DropdownMenu.Item>
																	{/if}
																	<DropdownMenu.Item
																		disabled={Boolean(pendingAction)}
														onclick={() => resetAdmin(dom.domain)}
																	>
																		<KeyRoundIcon />
																		<span>{t('mail.domainsTab.actionResetAdmin')}</span>
																	</DropdownMenu.Item>
																	<DropdownMenu.Item
																		disabled={Boolean(pendingAction)}
														onclick={() => repairDomain(dom.domain)}
																	>
																		<WrenchIcon />
																		<span>{t('mail.domainsTab.actionRepair')}</span>
																	</DropdownMenu.Item>
																	<DropdownMenu.Separator />
																	<DropdownMenu.Item
																		variant="destructive"
															disabled={Boolean(pendingAction)}
														onclick={() => openDeleteDomainModal(dom.domain)}
																	>
																		<Trash2Icon />
																		<span>{t('mail.domainsTab.actionDeleteDomain')}</span>
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
{/if}

					<PaginationBar
						bind:page={domainPage}
						totalItems={filteredMailDomains.length}
						pageSize={PAGE_SIZE}
						itemLabel={t('mail.domainsTab.itemLabel')}
					/>
				</div>
			</Tabs.Content>

			<!-- Tab 2: Daftar Akun Email -->
			<Tabs.Content value="accounts" class="mt-0">
				<div>
					<div class="flex flex-col gap-2.5 border-b bg-muted/20 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
						<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-2.5 w-full sm:w-auto">
							<search class="w-full sm:w-64">
								<InputGroup.Root class="h-8 bg-background">
									<InputGroup.Addon align="inline-start">
										<SearchIcon />
									</InputGroup.Addon>
									<InputGroup.Input
										type="search"
										placeholder={t('mail.accountsTab.searchPlaceholder')}
										aria-label={t('mail.accountsTab.searchAria')}
										bind:value={accountSearch}
									/>
								</InputGroup.Root>
							</search>

							<Select.Root type="single" bind:value={domainFilter}>
								<Select.Trigger class="h-8 w-full sm:w-44 text-xs" aria-label={t('mail.accountsTab.filterAria')}>
									{domainFilter || t('mail.accountsTab.allDomains')}
								</Select.Trigger>
								<Select.Content>
									<Select.Group>
										<Select.Item value="">{t('mail.accountsTab.allDomains')}</Select.Item>
										{#each mailStatus?.domains || [] as dom (dom.domain)}
											<Select.Item value={dom.domain}>{dom.domain}</Select.Item>
										{/each}
									</Select.Group>
								</Select.Content>
							</Select.Root>
						</div>

						<div class="flex items-center gap-2">
							<span class="text-xs text-muted-foreground">
								{t('mail.accountsTab.countLabel', { shown: filteredAccounts.length, total: flatAccounts.length })}
							</span>
						</div>
					</div>

					{#if accountsError}
						<div class="flex flex-col items-center justify-center gap-3 px-4 py-10 text-center">
							<p class="text-sm text-destructive">{accountsError}</p>
							<Button variant="outline" size="sm" onclick={() => loadMail(true)}>
								<RefreshCwIcon data-icon="inline-start" />
								{t('mail.accountsTab.retry')}
							</Button>
						</div>
					{:else if flatAccounts.length === 0}
						<div class="p-4">
							<Empty.Root class="border-0 py-8">
								<Empty.Header>
									<Empty.Media variant="icon">
										<UsersIcon />
									</Empty.Media>
									<Empty.Title>{t('mail.accountsTab.emptyTitle')}</Empty.Title>
									<Empty.Description>
										{t('mail.accountsTab.emptyDesc')}
									</Empty.Description>
								</Empty.Header>
								<Empty.Content>
									<Button size="sm" onclick={() => openAddAccount(domainFilter)}>
										<UserPlusIcon data-icon="inline-start" />
										{t('mail.accountsTab.emptyBtn')}
									</Button>
								</Empty.Content>
							</Empty.Root>
						</div>
					{:else if filteredAccounts.length === 0}
						<div class="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center">
							<p class="text-sm text-muted-foreground">{t('mail.accountsTab.noMatch')}</p>
							<Button
								variant="outline"
								size="xs"
								onclick={() => {
									accountSearch = '';
									domainFilter = '';
								}}
							>
								{t('mail.accountsTab.resetFilter')}
							</Button>
						</div>
					{:else}
						<div class="p-3 sm:p-0">
							<ResponsiveTableWrapper label={t('mail.accountsTab.tableAria')} class="rounded-none border-0 bg-transparent">
								<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
									<Table.Header>
										<Table.Row>
											<Table.Head scope="col">{t('mail.accountsTab.colDomain')}</Table.Head>
											<Table.Head scope="col">{t('mail.accountsTab.colEmail')}</Table.Head>
											<Table.Head scope="col">{t('mail.accountsTab.colAccountId')}</Table.Head>
											<Table.Head scope="col">{t('mail.accountsTab.colRole')}</Table.Head>
											<Table.Head scope="col">{t('mail.accountsTab.colRelay')}</Table.Head>
											<Table.Head scope="col" class="text-right">{t('mail.accountsTab.colActions')}</Table.Head>
										</Table.Row>
									</Table.Header>
									<Table.Body>
										{#each paginatedAccounts as item (item.account.id)}
											{@const grp = item}
											{@const acc = item.account}
											{@const isPending = Boolean(accountPending[acc.email])}
											<Table.Row>
												<Table.Cell data-label={t('mail.accountsTab.colDomain')} class="font-semibold">{grp.domain}</Table.Cell>
												<Table.Cell data-label={t('mail.accountsTab.dataLabelEmail')} class="font-medium">{acc.email}</Table.Cell>
												<Table.Cell data-label={t('mail.accountsTab.dataLabelId')} class="font-mono text-xs text-muted-foreground">
													{acc.name || acc.id}
												</Table.Cell>
												<Table.Cell data-label={t('mail.accountsTab.dataLabelRole')}>
													<StatusBadge
														tone={acc.is_admin ? 'warning' : 'success'}
														label={acc.is_admin ? t('mail.accountsTab.roleAdmin') : t('mail.accountsTab.roleUser')}
													/>
												</Table.Cell>
												<Table.Cell data-label={t('mail.accountsTab.dataLabelRelay')}>
													{#if grp.relay?.host}
														<Badge variant="secondary">
															{grp.relay.provider.toUpperCase()} ({grp.relay.host})
														</Badge>
													{:else if mailStatus?.outbound_routes?.[grp.domain] === 'mx'}
														<Badge variant="outline">{t('mail.accountsTab.relayLocalDirect')}</Badge>
													{:else if mailStatus?.global_relay?.host}
														<Badge variant="secondary">
															DEFAULT ({mailStatus.global_relay.provider.toUpperCase()})
														</Badge>
													{:else}
														<Badge variant="outline">{t('mail.accountsTab.relayDefaultLocal')}</Badge>
													{/if}
												</Table.Cell>
												<Table.Cell data-label={t('mail.accountsTab.colActions')} class="text-right">
													<div class="flex justify-end">
														<DropdownMenu.Root>
															<DropdownMenu.Trigger>
																{#snippet child({ props })}
																	<Button
																						variant="outline"
																		size="xs"
														disabled={isPending}
														{...props}
																	>
																		{#if isPending}
																			<Spinner data-icon="inline-start" />
																			{t('mail.accountsTab.deleting')}
																		{:else}
																			{t('mail.accountsTab.colActions')}
																			<ChevronDownIcon data-icon="inline-end" />
																		{/if}
																	</Button>
																{/snippet}
															</DropdownMenu.Trigger>
															<DropdownMenu.Content align="end" class="w-48">
																<DropdownMenu.Group>
																	<DropdownMenu.Label class="truncate text-xs text-muted-foreground">
																		{acc.email}
																	</DropdownMenu.Label>
																	<DropdownMenu.Separator />
																	<DropdownMenu.Item>
																		{#snippet child({ props })}
																			<a href={grp.webmail} target="_blank" rel="noopener noreferrer" {...props}>
																<ExternalLinkIcon />
																<span>{t('mail.accountsTab.openWebmail')}</span>
															</a>
														{/snippet}
													</DropdownMenu.Item>
													<DropdownMenu.Item
														disabled={isPending}
													onclick={() => openEditAccountPass(acc.email)}
													>
														<KeyRoundIcon />
														<span>{t('mail.accountsTab.changePassword')}</span>
													</DropdownMenu.Item>
													{#if !acc.is_admin}
														<DropdownMenu.Separator />
														<DropdownMenu.Item
															variant="destructive"
															disabled={isPending}
															onclick={() => deleteAccount(acc.email)}
														>
															<Trash2Icon />
															<span>{t('mail.accountsTab.deleteAccount')}</span>
														</DropdownMenu.Item>
													{/if}
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
{/if}

					<PaginationBar
						bind:page={accountPage}
						totalItems={filteredAccounts.length}
						pageSize={PAGE_SIZE}
						itemLabel={t('mail.accountsTab.itemLabel')}
					/>
				</div>
			</Tabs.Content>

			<!-- Tab 3: Status Port & Protokol Mail -->
			<Tabs.Content value="ports" class="mt-0">
				<div>
					<div class="flex flex-col gap-2.5 border-b bg-muted/20 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
						<div class="flex items-center gap-2">
							<ShieldCheckIcon class="size-4 text-muted-foreground" />
							<span class="text-xs font-medium text-foreground">
								{t('mail.portsTab.bannerTitle')}
							</span>
						</div>
						<span class="text-xs text-muted-foreground">
							{t('mail.portsTab.operatingCount', { open: openPortCount, total: stalwartPorts.length })}
						</span>
					</div>

					<div class="p-3 sm:p-0">
						<ResponsiveTableWrapper label={t('mail.portsTab.tableAria')} class="rounded-none border-0 bg-transparent">
							<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
								<Table.Header>
									<Table.Row>
										<Table.Head scope="col">{t('mail.portsTab.colPort')}</Table.Head>
										<Table.Head scope="col">{t('mail.portsTab.colService')}</Table.Head>
										<Table.Head scope="col">{t('mail.portsTab.colLocal')}</Table.Head>
										<Table.Head scope="col">{t('mail.portsTab.colPublic')}</Table.Head>
										<Table.Head scope="col">{t('mail.portsTab.colNotes')}</Table.Head>
									</Table.Row>
								</Table.Header>
								<Table.Body>
									{#each stalwartPorts as item (item.port)}
										{@const isLocalMode = (mailStatus?.mode || modeStore.mode) === 'local'}
										{@const isInternal = Boolean(item.internal_only || item.port === 8080)}
										{@const isInsideOpen = Boolean(item.local ?? item.open)}
										{@const isOutsideOpen = isLocalMode
											? isInsideOpen && !isInternal
											: isInsideOpen && !isInternal && Boolean(item.external_checked) && Boolean(item.public)}
										{@const isOutsideBlocked = !isLocalMode && item.open && !isInternal && Boolean(item.external_checked) && !item.public}
										{@const isLocalUnverified = !isLocalMode && item.open && !isInternal && !item.external_checked}
										<Table.Row>
											<Table.Cell data-label={t('mail.portsTab.colPort')} class="font-mono text-sm font-bold">
												{item.port}
											</Table.Cell>
											<Table.Cell data-label={t('mail.portsTab.dataLabelService')}>
												<div class="font-semibold">{item.label}</div>
												<div class="font-mono text-xs text-muted-foreground">
													{item.port === 25
														? t('mail.portsTab.desc25')
														: item.port === 465
															? t('mail.portsTab.desc465')
															: item.port === 587
																? t('mail.portsTab.desc587')
																: item.port === 993
																	? t('mail.portsTab.desc993')
																	: t('mail.portsTab.desc8080')}
												</div>
											</Table.Cell>
											<Table.Cell data-label={t('mail.portsTab.dataLabelLocal')}>
												{#if isInsideOpen}
													<StatusBadge tone="success" label={t('mail.portsTab.listeningActive')} />
												{:else}
													<StatusBadge tone="destructive" label={t('mail.portsTab.downInactive')} />
												{/if}
											</Table.Cell>
											<Table.Cell data-label={t('mail.portsTab.dataLabelPublic')}>
												{#if isInternal}
													<StatusBadge tone="neutral" label={t('mail.portsTab.publicInternal')} />
												{:else if isLocalMode}
													<StatusBadge tone="info" label={t('mail.portsTab.publicLocalMode')} />
												{:else if isOutsideOpen}
													<StatusBadge tone="success" label={t('mail.portsTab.publicOpen')} />
												{:else if isOutsideBlocked}
													<StatusBadge tone="warning" label={t('mail.portsTab.publicBlocked')} />
												{:else if isLocalUnverified}
													<StatusBadge tone="warning" label={t('mail.portsTab.publicUnverified')} />
												{:else}
													<StatusBadge tone="destructive" label={t('mail.portsTab.publicClosed')} />
												{/if}
											</Table.Cell>
											<Table.Cell data-label={t('mail.portsTab.dataLabelNotes')} class="text-xs text-muted-foreground">
												{#if item.port === 25}
													{#if isOutsideBlocked}
														{t('mail.portsTab.note25BlockedPre')} <strong>{t('mail.portsTab.note25BlockedBold')}</strong> {t('mail.portsTab.note25BlockedPost')}
													{:else}
														{t('mail.portsTab.note25Open')}
													{/if}
												{:else if item.port === 465 || item.port === 587}
													{t('mail.portsTab.noteSubmission')}
												{:else if item.port === 993}
													{t('mail.portsTab.noteImaps')}
												{:else}
													{t('mail.portsTab.noteAdmin')}
												{/if}
											</Table.Cell>
										</Table.Row>
									{/each}
								</Table.Body>
							</Table.Root>
						</ResponsiveTableWrapper>
					</div>

					{#if (mailStatus?.mode || modeStore.mode) !== 'local'}
						<div class="border-t p-4">
							{#if blockedOutsidePorts.length > 0}
								<Alert.Root class="border-amber-500/30 bg-amber-500/5">
									<InfoIcon class="text-amber-600 dark:text-amber-400" />
									<Alert.Title class="font-semibold text-amber-700 dark:text-amber-400">
										{t('mail.portsTab.blockedAlertTitle', { ports: blockedOutsidePorts.map((p) => p.port).join(', ') })}
									</Alert.Title>
									<Alert.Description class="text-xs leading-relaxed text-muted-foreground">
										{t('mail.portsTab.blockedAlertPre')} <strong>{t('mail.portsTab.blockedAlertBold')}</strong> {t('mail.portsTab.blockedAlertPost')}
									</Alert.Description>
								</Alert.Root>
							{:else}
								<div class="flex items-center gap-2 text-xs text-muted-foreground">
									<CheckIcon class="size-4 text-success" />
									<span>{t('mail.portsTab.allPortsHealthy')}</span>
								</div>
							{/if}
						</div>
					{/if}
				</div>
			</Tabs.Content>
		</Tabs.Root>
	</Card.Root>
</div>

<!-- Add Email Account Dialog -->
<Dialog.Root bind:open={addAccOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>{t('mail.addAccModal.title')}</Dialog.Title>
			<Dialog.Description>{t('mail.addAccModal.desc')}</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={submitAddAccount} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-new-email">{t('mail.addAccModal.emailLabel')}</Field.FieldLabel>
					<Input
						id="mail-new-email"
						autocomplete="username"
						inputmode="email"
						spellcheck={false}
						placeholder="user@domain.tld"
						bind:value={newEmail}
						required
					/>
				</Field.Field>
				<Field.Field>
					<Field.FieldLabel for="mail-new-password">{t('mail.addAccModal.passLabel')}</Field.FieldLabel>
					<PasswordInput
						id="mail-new-password"
						autocomplete="new-password"
						placeholder={t('mail.addAccModal.passPlaceholder')}
						bind:value={newEmailPass}
						required
					/>
					<Field.FieldDescription>{t('mail.addAccModal.passHelp')}</Field.FieldDescription>
				</Field.Field>
			</Field.FieldGroup>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (addAccOpen = false)}>{t('mail.addAccModal.cancel')}</Button>
				<Button type="submit" disabled={addingAcc}>
					{#if addingAcc}
						<Spinner data-icon="inline-start" />
						{t('mail.addAccModal.submitting')}
					{:else}
						{t('mail.addAccModal.submit')}
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<!-- Change Email Account Password Dialog -->
<Dialog.Root bind:open={editPassOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>{t('mail.editPassModal.title')}</Dialog.Title>
			<Dialog.Description>
				{t('mail.editPassModal.descPre')} <strong class="font-mono">{editPassEmail}</strong>{t('mail.editPassModal.descPost')}
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={submitEditAccountPass} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-edit-password">{t('mail.editPassModal.passLabel')}</Field.FieldLabel>
					<PasswordInput
						id="mail-edit-password"
						autocomplete="new-password"
						placeholder={t('mail.editPassModal.passPlaceholder')}
						bind:value={editPassValue}
						required
					/>
					<Field.FieldDescription>{t('mail.editPassModal.passHelp')}</Field.FieldDescription>
				</Field.Field>
			</Field.FieldGroup>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (editPassOpen = false)}>{t('mail.editPassModal.cancel')}</Button>
				<Button type="submit" disabled={editingPass}>
					{#if editingPass}
						<Spinner data-icon="inline-start" />
						{t('mail.editPassModal.submitting')}
					{:else}
						{t('mail.editPassModal.submit')}
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<!-- Delete Domain Dialog -->
<Dialog.Root bind:open={deleteDomOpen}>
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>{t('mail.deleteDomModal.title')}</Dialog.Title>
			<Dialog.Description>
				{t('mail.deleteDomModal.descPre')} <strong>{deleteDomTarget}</strong> {t('mail.deleteDomModal.descPost')}
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={submitDeleteDomain} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-del-confirm">{t('mail.deleteDomModal.confirmLabel')}</Field.FieldLabel>
					<Input
						id="mail-del-confirm"
						placeholder={deleteDomTarget}
						autocomplete="off"
						spellcheck={false}
						bind:value={deleteDomConfirm}
						required
					/>
				</Field.Field>
				<Field.Field orientation="horizontal">
					<Checkbox id="mail-del-dns" bind:checked={deleteDomDNS} />
					<Field.FieldLabel for="mail-del-dns" class="font-normal">
						{t('mail.deleteDomModal.deleteDnsLabel')}
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (deleteDomOpen = false)}>{t('mail.deleteDomModal.cancel')}</Button>
				<Button type="submit" variant="destructive" disabled={deletingDom}>
					{#if deletingDom}
						<Spinner data-icon="inline-start" />
						{t('mail.deleteDomModal.submitting')}
					{:else}
						<Trash2Icon data-icon="inline-start" />
						{t('mail.deleteDomModal.submit')}
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<!-- Set Relay Dialog -->
<Dialog.Root bind:open={relayOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>{t('mail.relayModal.title')}</Dialog.Title>
			<Dialog.Description>
				{t('mail.relayModal.desc')}
			</Dialog.Description>
		</Dialog.Header>

		<form id="mail-relay-form" onsubmit={submitRelay} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-relay-domain">{t('mail.relayModal.targetDomainLabel')}</Field.FieldLabel>
					<Select.Root
						type="single"
						value={relayTargetDomain}
						onValueChange={(v) => {
							if (v) {
								relayTargetDomain = v;
								loadRelayDNSRecords(v, relayProvider);
							}
						}}
					>
						<Select.Trigger id="mail-relay-domain" class="w-full">
							{relayTargetDomain === 'global'
								? t('mail.relayModal.allDomainsDefault')
								: relayTargetDomain}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="global">{t('mail.relayModal.allDomainsDefault')}</Select.Item>
								{#each mailStatus?.domains || [] as dom (dom.domain)}
									<Select.Item value={dom.domain}>{dom.domain}</Select.Item>
								{/each}
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="mail-relay-provider">{t('mail.relayModal.providerLabel')}</Field.FieldLabel>
					<Select.Root
						type="single"
						value={relayProvider}
						onValueChange={(v) => {
							if (v) {
								relayProvider = v;
								loadRelayDNSRecords(relayTargetDomain, v);
							}
						}}
					>
						<Select.Trigger id="mail-relay-provider" class="w-full">
							{relayProvider === 'local'
								? t('mail.relayModal.optLocal')
								: relayProvider === 'resend'
									? t('mail.relayModal.optResend')
									: relayProvider === 'brevo'
										? t('mail.relayModal.optBrevo')
										: t('mail.relayModal.optCustom')}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="local">{t('mail.relayModal.optLocal')}</Select.Item>
								<Select.Item value="resend">{t('mail.relayModal.optResend')}</Select.Item>
								<Select.Item value="brevo">{t('mail.relayModal.optBrevo')}</Select.Item>
								<Select.Item value="custom">{t('mail.relayModal.optCustom')}</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				{#if relayProvider === 'local'}
					<Alert.Root>
						<InfoIcon />
						<Alert.Title>{t('mail.relayModal.localAlertTitle')}</Alert.Title>
						<Alert.Description>
							{#if relayTargetDomain === 'global'}
								{t('mail.relayModal.localAlertGlobalDesc')}
							{:else}
								{t('mail.relayModal.localAlertDomainPre')} <strong>{relayTargetDomain}</strong> {t('mail.relayModal.localAlertDomainPost')}
							{/if}
						</Alert.Description>
					</Alert.Root>
				{:else if relayProvider === 'resend' || relayProvider === 'brevo'}
					<Field.Field>
						<Field.FieldLabel for="mail-relay-secret">
							{relayProvider === 'resend' ? t('mail.relayModal.secretResendLabel') : t('mail.relayModal.secretBrevoLabel')}
						</Field.FieldLabel>
						<PasswordInput
							id="mail-relay-secret"
							canGenerate={false}
							autocomplete="current-password"
							placeholder={hasStoredRelayKey
								? t('mail.relayModal.secretStoredPlaceholder')
								: relayProvider === 'resend'
									? 're_123456789...'
									: 'xkeysib-123456789...'}
							bind:value={relaySecret}
						/>
						<Field.FieldDescription>
							{relayProvider === 'resend'
								? t('mail.relayModal.secretResendHelp')
								: t('mail.relayModal.secretBrevoHelp')}
						</Field.FieldDescription>
					</Field.Field>
				{:else}
					<div class="grid gap-3 sm:grid-cols-[2fr_1fr]">
						<Field.Field>
							<Field.FieldLabel for="mail-relay-host">{t('mail.relayModal.hostLabel')}</Field.FieldLabel>
							<Input id="mail-relay-host" placeholder="smtp.mailgun.org" bind:value={relayHost} required />
						</Field.Field>
						<Field.Field>
							<Field.FieldLabel for="mail-relay-port">{t('mail.relayModal.portLabel')}</Field.FieldLabel>
							<Input id="mail-relay-port" inputmode="numeric" pattern="[0-9]+" bind:value={relayPortStr} />
						</Field.Field>
					</div>
					<Field.Field>
						<Field.FieldLabel for="mail-relay-user">{t('mail.relayModal.userLabel')}</Field.FieldLabel>
						<Input id="mail-relay-user" placeholder="postmaster@domain.com" bind:value={relayUser} />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="mail-relay-pass">{t('mail.relayModal.passLabel')}</Field.FieldLabel>
						<PasswordInput id="mail-relay-pass" canGenerate={false} autocomplete="current-password" placeholder={t('mail.relayModal.passPlaceholder')} bind:value={relayPass} />
					</Field.Field>
				{/if}

				{#if relayProvider !== 'local'}
					<Field.Field>
						<div class="flex flex-wrap items-center justify-between gap-2">
							<Field.FieldLabel for="mail-relay-dns">{t('mail.relayModal.dnsLabel')}</Field.FieldLabel>
							{#if relayProvider === 'resend' && relayTargetDomain !== 'global'}
								<Button
									type="button"
									variant="outline"
									size="xs"
									disabled={syncingRelayDNS || savingRelay}
									onclick={handleSyncResendDNS}
								>
									{#if syncingRelayDNS}
										<Spinner data-icon="inline-start" />
										{t('mail.relayModal.syncingDns')}
									{:else}
										<RefreshCwIcon data-icon="inline-start" />
										{t('mail.relayModal.syncResendDns')}
									{/if}
								</Button>
							{/if}
						</div>
						{#if relaySyncNotice}
							<Alert.Root variant={relaySyncNotice.tone === 'warn' ? 'destructive' : 'default'} class="my-1 py-2">
								<InfoIcon class="size-4" />
								<Alert.Description class="text-xs">{relaySyncNotice.text}</Alert.Description>
							</Alert.Root>
						{/if}
						<Textarea
							id="mail-relay-dns"
							rows={4}
							spellcheck={false}
							class="font-mono text-xs"
							placeholder={relayProvider === 'resend'
								? t('mail.relayModal.dnsResendPlaceholder')
								: 'CNAME brevo1._domainkey.domain.com b1.domain.com.dkim.brevo.com'}
							bind:value={relayDNSText}
						/>
						<Field.FieldDescription>
							{t('mail.relayModal.dnsHelpPre')} <code>TYPE NAMA NILAI</code>. {t('mail.relayModal.dnsHelpPost')}
						</Field.FieldDescription>
					</Field.Field>
				{/if}
			</Field.FieldGroup>
		</form>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (relayOpen = false)}>{t('mail.relayModal.cancel')}</Button>
			<Button type="submit" form="mail-relay-form" disabled={savingRelay}>
				{#if savingRelay}
					<Spinner data-icon="inline-start" />
					{t('mail.relayModal.submitting')}
				{:else}
					{t('mail.relayModal.submit')}
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Setup Domain & Mail Server Dialog -->
<Dialog.Root bind:open={setupOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>{t('mail.setupModal.title')}</Dialog.Title>
			<Dialog.Description>
				{#if modeStore.mode === 'vps'}
					{t('mail.setupModal.descVps')}
				{:else if modeStore.mode === 'local'}
					{t('mail.setupModal.descLocal')}
				{:else}
					{t('mail.setupModal.descEdge')}
				{/if}
			</Dialog.Description>
		</Dialog.Header>

		<form id="mail-setup-form" onsubmit={submitSetupMail} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
			{#if preflightLoading}
				<div class="flex items-center gap-2 text-xs text-muted-foreground">
					<Spinner />
					<span>{t('mail.setupModal.checkingPrereqs')}</span>
				</div>
			{:else if preflight}
				{#if preflight.blockers?.length}
					<Alert.Root variant="destructive">
						<Alert.Title>{t('mail.setupModal.blockerTitle')}</Alert.Title>
						<Alert.Description>
							<ul class="list-disc pl-4">
								{#each preflight.blockers as b (b)}
									<li>{b}</li>
								{/each}
							</ul>
						</Alert.Description>
					</Alert.Root>
				{:else if preflight.warnings?.length}
					<Alert.Root>
						<Alert.Title>{t('mail.setupModal.warningTitle')}</Alert.Title>
						<Alert.Description>
							<ul class="list-disc pl-4">
								{#each preflight.warnings as w (w)}
									<li>{w}</li>
								{/each}
							</ul>
						</Alert.Description>
					</Alert.Root>
				{:else}
					<Alert.Root>
						<Alert.Title>{t('mail.setupModal.readyTitle')}</Alert.Title>
						<Alert.Description>
							{#if (preflight.mode || modeStore.mode) === 'local'}
								{t('mail.setupModal.readyLocal')}
							{:else if (preflight.mode || modeStore.mode) === 'vps'}
								{t('mail.setupModal.readyVps')}
							{:else}
								{t('mail.setupModal.readyEdge')}
							{/if}
						</Alert.Description>
					</Alert.Root>
				{/if}
			{/if}

			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-setup-domain-select">
						{t('mail.setupModal.domainLabelPrefix')} {modeStore.mode === 'edge' ? t('mail.setupModal.scopeEdge') : modeStore.mode === 'vps' ? t('mail.setupModal.scopeVps') : t('mail.setupModal.scopeLocal')}
					</Field.FieldLabel>
					<Select.Root
						type="single"
						value={setupDomainChoice}
						onValueChange={(v) => {
							if (v) {
								setupDomainChoice = v;
								refreshPreflight(v === '__custom__' ? setupCustomDomain : v);
							}
						}}
					>
						<Select.Trigger id="mail-setup-domain-select" class="w-full">
							{setupDomainChoice === '__custom__'
								? t('mail.setupModal.customOptionTrigger')
								: setupDomainChoice || t('mail.setupModal.selectDomainPlaceholder')}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								{#each zoneDomains as dom (dom)}
									<Select.Item value={dom}>{dom}</Select.Item>
								{/each}
								<Select.Item value="__custom__">{t('mail.setupModal.customOptionItem')}</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				{#if setupDomainChoice === '__custom__'}
					<Field.Field>
						<Field.FieldLabel for="mail-setup-custom-dom">{t('mail.setupModal.manualDomainLabel')}</Field.FieldLabel>
						<Input
							id="mail-setup-custom-dom"
							placeholder={t('mail.setupModal.manualDomainPlaceholder')}
							autocapitalize="none"
							spellcheck={false}
							bind:value={setupCustomDomain}
							onblur={() => refreshPreflight(setupCustomDomain)}
							required
						/>
					</Field.Field>
				{/if}

				<Field.Field orientation="horizontal">
					<Checkbox id="mail-setup-ssl" bind:checked={setupSSL} />
					<Field.FieldLabel for="mail-setup-ssl" class="font-normal">
						{#if modeStore.mode === 'local'}
							{t('mail.setupModal.sslLocalLabel')}
						{:else}
							{t('mail.setupModal.sslPublicLabel')}
						{/if}
					</Field.FieldLabel>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="mail-setup-relay-prov">{t('mail.setupModal.outboundLabel')}</Field.FieldLabel>
					<Select.Root type="single" bind:value={setupRelayProv}>
						<Select.Trigger id="mail-setup-relay-prov" class="w-full">
							{setupRelayProv === ''
								? mailStatus?.global_relay?.host
									? t('mail.setupModal.optUseCurrentDefault', { provider: mailStatus.global_relay.provider.toUpperCase() })
									: t('mail.setupModal.optDefaultLocalSmtp')
								: setupRelayProv === 'local'
									? t('mail.setupModal.optDirectLocal')
									: setupRelayProv === 'resend'
										? t('mail.setupModal.optResend')
										: setupRelayProv === 'brevo'
											? t('mail.setupModal.optBrevo')
											: t('mail.setupModal.optCustom')}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="">
									{mailStatus?.global_relay?.host
										? t('mail.setupModal.optUseCurrentDefault', { provider: mailStatus.global_relay.provider.toUpperCase() })
										: t('mail.setupModal.optDefaultLocalSmtp')}
								</Select.Item>
								{#if mailStatus?.global_relay?.host}
									<Select.Item value="local">{t('mail.setupModal.optDirectLocal')}</Select.Item>
								{/if}
								<Select.Item value="resend">{t('mail.setupModal.optResend')}</Select.Item>
								<Select.Item value="brevo">{t('mail.setupModal.optBrevo')}</Select.Item>
								<Select.Item value="custom">{t('mail.setupModal.optCustom')}</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
					<Field.FieldDescription>
						{#if modeStore.mode === 'local'}
							{t('mail.setupModal.outboundHelpLocal')}
						{:else}
							{t('mail.setupModal.outboundHelpPublic')}
						{/if}
					</Field.FieldDescription>
				</Field.Field>

				{#if setupRelayProv === 'resend'}
					<Field.Field>
						<Field.FieldLabel for="mail-setup-resend-key">{t('mail.setupModal.resendKeyLabel')}</Field.FieldLabel>
						<PasswordInput
							id="mail-setup-resend-key"
							canGenerate={false}
							autocomplete="current-password"
							placeholder="re_123456789..."
							bind:value={setupResendKey}
							required
						/>
					</Field.Field>
				{:else if setupRelayProv === 'brevo'}
					<Field.Field>
						<Field.FieldLabel for="mail-setup-brevo-user">{t('mail.setupModal.brevoUserLabel')}</Field.FieldLabel>
						<Input id="mail-setup-brevo-user" placeholder="akun@domain.com" bind:value={setupBrevoUser} />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="mail-setup-brevo-key">{t('mail.setupModal.brevoKeyLabel')}</Field.FieldLabel>
						<PasswordInput
							id="mail-setup-brevo-key"
							canGenerate={false}
							autocomplete="current-password"
							placeholder="xsmtpsib-..."
							bind:value={setupBrevoKey}
							required
						/>
					</Field.Field>
				{:else if setupRelayProv === 'custom'}
					<div class="grid gap-3 sm:grid-cols-[2fr_1fr]">
						<Field.Field>
							<Field.FieldLabel for="mail-setup-custom-host">{t('mail.setupModal.customHostLabel')}</Field.FieldLabel>
							<Input id="mail-setup-custom-host" placeholder="mail.smtp.com" bind:value={setupCustomHost} required />
						</Field.Field>
						<Field.Field>
							<Field.FieldLabel for="mail-setup-custom-port">{t('mail.setupModal.customPortLabel')}</Field.FieldLabel>
							<Input
								id="mail-setup-custom-port"
								inputmode="numeric"
								pattern="[0-9]+"
								bind:value={setupCustomPortStr}
							/>
						</Field.Field>
					</div>
					<Field.Field>
						<Field.FieldLabel for="mail-setup-custom-user">{t('mail.setupModal.customUserLabel')}</Field.FieldLabel>
						<Input id="mail-setup-custom-user" placeholder="smtp-user" bind:value={setupCustomUser} />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="mail-setup-custom-pass">{t('mail.setupModal.customPassLabel')}</Field.FieldLabel>
						<PasswordInput id="mail-setup-custom-pass" canGenerate={false} autocomplete="current-password" placeholder={t('mail.setupModal.customPassPlaceholder')} bind:value={setupCustomPass} />
					</Field.Field>
				{/if}
			</Field.FieldGroup>
		</form>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (setupOpen = false)}>{t('mail.setupModal.cancel')}</Button>
			<Button type="submit" form="mail-setup-form" disabled={settingUp}>
				{#if settingUp}
					<Spinner data-icon="inline-start" />
					{t('mail.setupModal.submitting')}
				{:else}
					{t('mail.setupModal.submit')}
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
