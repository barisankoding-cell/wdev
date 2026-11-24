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
				toast.error(stRes.reason instanceof Error ? stRes.reason.message : 'Gagal memuat status mail');
			}
			if (accRes.status === 'fulfilled') {
				domainGroups = accRes.value ?? [];
				accountsError = '';
			} else {
				domainGroups = [];
				accountsError = accRes.reason instanceof Error ? accRes.reason.message : 'Gagal memuat akun';
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
			toast.warning('Format email tidak valid. Gunakan bentuk user@domain.tld.');
			return;
		}
		if (!newEmailPass) {
			toast.warning('Password wajib diisi.');
			return;
		}
		addingAcc = true;
		try {
			await api.addMailAccount(em, newEmailPass);
			toast.success(`Akun email ${em} berhasil dibuat di Stalwart!`);
			addAccOpen = false;
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menambahkan akun email');
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
			toast.warning('Password baru wajib diisi.');
			return;
		}
		if (editPassValue.trim().length < 8) {
			toast.warning('Password baru minimal 8 karakter.');
			return;
		}
		editingPass = true;
		try {
			const res = await api.changeMailAccountPassword(em, editPassValue.trim());
			toast.success(res.message || `Password akun ${em} berhasil diperbarui.`);
			editPassOpen = false;
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal mengubah password akun email');
		} finally {
			editingPass = false;
		}
	}

	async function deleteAccount(email: string) {
		const ok = await cp.confirm({
			title: 'Hapus Akun Email',
			description: `Hapus akun email ${email}? Semua email di kotak pos tersebut akan ikut terhapus.`,
			confirmText: 'Hapus Akun',
			destructive: true
		});
		if (!ok) return;
		accountPending = { ...accountPending, [email]: true };
		try {
			const res = await api.deleteMailAccount(email);
			toast.success(res.message || `Akun ${email} dihapus.`);
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus akun email');
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
			toast.warning('Ketik nama domain persis sama untuk konfirmasi penghapusan.');
			return;
		}
		deletingDom = true;
		try {
			const report = await api.deleteMailDomain(deleteDomTarget, deleteDomDNS);
			deleteDomOpen = false;
			const summary = (report.checks || []).map((c) => `${c.name}: ${c.detail}`).join(' | ');
			toast.success(`Domain ${deleteDomTarget} dihapus.${summary ? ` (${summary})` : ''}`);
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus domain mail');
		} finally {
			deletingDom = false;
		}
	}

	async function repairDomain(domain: string) {
		const ok = await cp.confirm({
			title: 'Perbaiki Domain Mail',
			description: `Perbaiki domain mail ${domain}? Tindakan: perbaiki NS/SOA & glue zone, pastikan record A/MX/SPF/DMARC, segarkan vhost VPS, dan rapikan routing relay outbound.`,
			confirmText: 'Perbaiki Sekarang'
		});
		if (!ok) return;
		domainPending = { ...domainPending, [domain]: 'repair' };
		try {
			await api.repairMailDomain(domain);
			cp.openTerminal(`Perbaiki Domain Mail: ${domain}`);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memulai perbaikan domain');
		} finally {
			const next = { ...domainPending };
			delete next[domain];
			domainPending = next;
		}
	}

	async function resetAdmin(domain: string) {
		const ok = await cp.confirm({
			title: 'Reset Password Admin',
			description: `Reset password admin Stalwart untuk domain '${domain}'? Password baru akan dibuat otomatis dan disimpan di /etc/wdev/mail-admin.cred.`,
			confirmText: 'Reset Password'
		});
		if (!ok) return;
		domainPending = { ...domainPending, [domain]: 'reset-admin' };
		try {
			await api.resetMailAdmin(domain);
			toast.success(`Password admin untuk ${domain} berhasil direset.`);
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal mereset password admin');
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
		if (dns.removed?.length) parts.push(`${dns.removed.length} record lama dihapus`);
		if (dns.added?.length) parts.push(`${dns.added.length} record baru dibuat`);
		if (dns.kept?.length) parts.push(`${dns.kept.length} record terpasang`);
		if (dns.verification_triggered) parts.push('verifikasi Resend dipicu');
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
			if (rep?.added?.length) parts.push(`${rep.added.length} record DNS dibuat di PowerDNS`);
			if (rep?.kept?.length) parts.push(`${rep.kept.length} record sudah sesuai`);
			if (rep?.removed?.length) parts.push(`${rep.removed.length} record usang dibersihkan`);
			if (res.verification_triggered) parts.push('verifikasi domain Resend dipicu');
			const msg = parts.join(', ') || 'Sinkronisasi Resend selesai';
			const warnNote = rep?.manual?.find((m) => m.toLowerCase().includes('gagal') || m.toLowerCase().includes('menunggu'));
			relaySyncNotice = {
				tone: warnNote ? 'warn' : 'info',
				text: warnNote ? `${msg}. (${warnNote})` : `${msg} (status Resend: ${res.domain_status || 'pending'}).`
			};
			toast.success(`DNS Resend ${relayTargetDomain}: ${msg}`);
			await loadMail();
		} catch (err) {
			const text = err instanceof Error ? err.message : 'Gagal menyinkronkan DNS Resend';
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
				toast.warning(`API / SMTP Key untuk ${relayProvider.toUpperCase()} wajib diisi.`);
				return;
			}
			if (typed) {
				payload.password = typed;
			}
		} else {
			if (!relayHost.trim()) {
				toast.warning('Host SMTP wajib diisi untuk custom relay.');
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
						throw new Error(`Format baris DNS tidak valid: ${line}`);
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
					? `Outbound ${relayTargetDomain === 'global' ? 'default' : relayTargetDomain} diarahkan ke SMTP lokal Stalwart.${dnsNote ? ` (${dnsNote})` : ''}`
					: `Relay domain ${relayTargetDomain} berhasil disimpan.${dnsNote ? ` (${dnsNote})` : ''}`
			);
			if (res.dns?.manual?.some((m) => m.includes('API Key Resend tidak diizinkan') || m.includes('gagal terhubung'))) {
				toast.warning(res.dns.manual[0]);
			}
			relayOpen = false;
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menyetel relay');
		} finally {
			savingRelay = false;
		}
	}

	async function removeRelayOverride(domain: string) {
		const fallbackDesc = mailStatus?.global_relay?.host
			? `relay default (${mailStatus.global_relay.provider.toUpperCase()})`
			: 'SMTP lokal bawaan Stalwart (direct MX)';
		const ok = await cp.confirm({
			title: 'Hapus Override Outbound',
			description: `Hapus override outbound untuk ${domain}? Domain akan kembali memakai ${fallbackDesc}.`,
			confirmText: 'Hapus Override',
			destructive: true
		});
		if (!ok) return;
		domainPending = { ...domainPending, [domain]: 'unset-relay' };
		try {
			await api.removeMailRelay(domain);
			toast.success(`Override relay ${domain} dihapus.`);
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus override relay');
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
			toast.warning('Pilih atau masukkan domain publik yang valid.');
			return;
		}
		let relayHost = '';
		let relayPort = 587;
		let relayUser = '';
		let relayPass = '';

		if (setupRelayProv === 'resend') {
			relayPass = setupResendKey.trim();
			if (!relayPass) {
				toast.warning('API Key Resend wajib diisi.');
				return;
			}
		} else if (setupRelayProv === 'brevo') {
			relayUser = setupBrevoUser.trim();
			relayPass = setupBrevoKey.trim();
			if (!relayPass) {
				toast.warning('SMTP Key Brevo wajib diisi.');
				return;
			}
		} else if (setupRelayProv === 'custom') {
			relayHost = setupCustomHost.trim();
			relayPort = parseInt(setupCustomPortStr, 10) || 587;
			relayUser = setupCustomUser.trim();
			relayPass = setupCustomPass;
			if (!relayHost) {
				toast.warning('SMTP Host wajib diisi untuk Custom Relay.');
				return;
			}
		}

		setupOpen = false;
		const ok = await cp.confirm({
			title: 'Konfirmasi Setup Mail Server',
			description: `Mulai setup otomatis Mail Server & DNS untuk domain "${domain}"?`,
			confirmText: 'Mulai Setup'
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
			cp.openTerminal(`Setup Mail Server: ${domain}`);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memulai setup mail');
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
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Mail Server &amp; Webmail</h1>
				<p class="text-sm text-muted-foreground">
					Mail Server Stalwart standalone &amp; Webmail SnappyMail langsung melayani publik (aaPanel Style).
				</p>
			{:else if modeStore.mode === 'local'}
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Mail Server Standalone (Lokal)</h1>
				<p class="text-sm text-muted-foreground">
					Stalwart Mail Server &amp; Webmail SnappyMail mandiri tanpa NetBird via /etc/hosts atau PowerDNS lokal.
				</p>
			{:else}
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Mail Server &amp; Outbound SMTP</h1>
				<p class="text-sm text-muted-foreground">
					Manajemen kotak pos Stalwart, webmail SnappyMail, serta pengiriman SMTP lokal atau relay eksternal per domain.
				</p>
			{/if}
		</div>
		<div class="flex shrink-0 flex-wrap items-center gap-2">
			<StatusBadge
				tone={mailStatus?.stalwart_active ? 'success' : 'warning'}
				label={mailStatus?.stalwart_active ? 'Mail Stack Aktif' : 'Belum Aktif'}
			/>
			<Button
				variant="outline"
				size="sm"
				disabled={refreshing}
				onclick={() => loadMail(true)}
			>
				{#if refreshing}
					<Spinner data-icon="inline-start" />
					Menyegarkan...
				{:else}
					<RefreshCwIcon data-icon="inline-start" />
					Segarkan
				{/if}
			</Button>
			<Button variant="outline" size="sm" onclick={() => openRelayModal('global')}>
				<SettingsIcon data-icon="inline-start" />
				Setel Relay
			</Button>
			<Button size="sm" onclick={openSetupModal}>
				<MailPlusIcon data-icon="inline-start" />
				Setup Domain
			</Button>
		</div>
	</div>

	<!-- Contextual Alerts -->
	{#if modeStore.mode === 'local'}
		<Alert.Root class="border-muted-foreground/30 bg-muted/40">
			<InfoIcon />
			<Alert.Title>Mode Lokal Standalone Aktif</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					Di mode <strong>Lokal Standalone</strong>, menu Mail Server disembunyikan dari sidebar utama. Anda tetap bisa menguji Mail Server lokal di halaman ini atau beralih ke mode Edge Gateway / Full VPS.
				</span>
				<Button size="xs" variant="outline" class="shrink-0" onclick={() => modeStore.openModal()}>
					Ganti Mode Server
				</Button>
			</Alert.Description>
		</Alert.Root>
	{/if}

	{#if mailStatus && (!mailStatus.stalwart_active || !mailStatus.snappymail_active || mailStatus.domains.length === 0)}
		<Alert.Root>
			<InfoIcon />
			<Alert.Title>
				{#if mailStatus.domains.length > 0 && (!mailStatus.stalwart_active || !mailStatus.snappymail_active)}
					Mail Server Stack Belum Aktif / Belum Terpasang
				{:else}
					Setup Domain &amp; Mail Server
				{/if}
			</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					{#if mailStatus.domains.length > 0 && (!mailStatus.stalwart_active || !mailStatus.snappymail_active)}
						Domain mail sudah tercatat, namun engine Stalwart dan/atau Webmail SnappyMail belum terpasang atau sedang nonaktif. Klik Pasang Mail Stack lalu Perbaiki domain terkait.
					{:else if modeStore.mode === 'local'}
						Inisialisasi engine Stalwart lokal, Webmail SnappyMail, entri /etc/hosts, serta SSL lokal (mkcert/OpenSSL) tanpa NetBird.
					{:else}
						Inisialisasi engine Stalwart Mail Server, Webmail SnappyMail, record DNS (MX, SPF, DMARC), dan SSL Let's Encrypt.
					{/if}
				</span>
				<div class="flex flex-wrap gap-2">
					{#if !mailStatus.stalwart_active || !mailStatus.snappymail_active}
						<Button size="xs" variant="outline" onclick={() => cp.triggerInstall('mail-stack')}>
							Pasang Mail Stack
						</Button>
					{/if}
					<Button size="xs" onclick={openSetupModal}>Mulai Setup Domain</Button>
				</div>
			</Alert.Description>
		</Alert.Root>
	{/if}

	<!-- Unified Mail Server Topology & Status Strip (PowerDNS Style) -->
	<Card.Root class="gap-0 py-0">
		<div class="flex flex-wrap items-center justify-between gap-2 border-b bg-muted/30 px-4 py-2.5">
			<div class="flex items-center gap-2">
				<MailIcon class="size-4 text-primary" aria-hidden="true" />
				<span class="text-sm font-semibold">Topologi &amp; Status Layanan Mail</span>
				<Badge variant="outline" class="font-normal">
					{(mailStatus?.mode || modeStore.mode) === 'vps'
						? 'Full VPS Mail'
						: (mailStatus?.mode || modeStore.mode) === 'local'
							? 'Lokal Standalone'
							: 'Edge Gateway Mail'}
				</Badge>
			</div>
			<div class="flex flex-wrap items-center gap-3 text-xs tabular-nums text-muted-foreground">
				<span><strong class="font-semibold text-foreground">{allMailDomains.length}</strong> Domain</span>
				<span><strong class="font-semibold text-foreground">{totalAccounts}</strong> Akun Email</span>
				<span><strong class="font-semibold text-foreground">{openPortCount}/{stalwartPorts.length}</strong> Port Aktif</span>
			</div>
		</div>

		<div class="grid divide-y sm:grid-cols-2 lg:grid-cols-4 sm:divide-x sm:divide-y-0">
			<!-- Col 1: Stalwart Engine & Ports -->
			<div class="flex flex-col gap-2 p-4">
				<div class="flex items-center justify-between gap-2">
					<span class="text-xs text-muted-foreground">Stalwart Mail Engine</span>
					<StatusBadge
						tone={mailStatus?.stalwart_active ? 'success' : 'warning'}
						label={mailStatus?.stalwart_active ? 'Aktif (Running)' : 'Nonaktif'}
					/>
				</div>
				<div class="flex items-center justify-between gap-1 text-[11px] text-muted-foreground">
					<span>Listener Port</span>
					<span class="font-mono tabular-nums">
						{#if !mailStatus}
							...
						{:else if (mailStatus.mode || modeStore.mode) === 'local'}
							{openPortCount}/{stalwartPorts.length} lokal
						{:else}
							{publicOpenCount}/{publicMailPorts.length} publik · {openPortCount}/{stalwartPorts.length} listen
						{/if}
					</span>
				</div>
				<div class="flex flex-wrap gap-1" aria-label="Status port Stalwart">
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
								? `${item.label} (Port ${item.port}): Memeriksa status port...`
								: !item.open
									? `${item.label} (Port ${item.port}): Mati — service tidak listen di dalam`
									: isInternal
										? `${item.label} (Port ${item.port}): Internal lokal (127.0.0.1:8080 diproxy Nginx)`
										: isLocalMode
											? `${item.label} (Port ${item.port}): Aktif lokal`
											: isOutsideOpen
												? `${item.label} (Port ${item.port}): Terbuka penuh — listen lokal & IP publik terjangkau dari luar`
												: isOutsideBlocked
													? `${item.label} (Port ${item.port}): Kuning — listen lokal berhasil, namun terblokir dari luar (cek Firewall VPS / ISP)`
													: `${item.label} (Port ${item.port}): Kuning — listen lokal berhasil, namun uji koneksi luar belum terjangkau`}
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
						<span class="text-xs text-muted-foreground">SnappyMail Webmail</span>
						<StatusBadge
							tone={mailStatus?.snappymail_active ? 'success' : 'warning'}
							label={mailStatus?.snappymail_active ? 'Siap / Aktif' : 'Belum Terpasang'}
						/>
					</div>
					<p class="truncate font-mono text-xs text-muted-foreground">/var/www/snappymail</p>
				</div>
				<p class="text-[11px] text-muted-foreground">
					Webmail modern berbasis PHP untuk akses kotak pos via browser.
				</p>
			</div>

			<!-- Col 3: Storage -->
			<div class="flex flex-col justify-between gap-2 p-4">
				<div class="flex flex-col gap-1">
					<span class="text-xs text-muted-foreground">Kapasitas Disk Bebas</span>
					<div class="flex items-baseline gap-1.5">
						<span class="font-mono text-base font-semibold tabular-nums">
							{mailStatus?.storage_free ? `${mailStatus.storage_free} Bebas` : '-'}
						</span>
					</div>
				</div>
				<p class="text-[11px] text-muted-foreground">Penyimpanan kotak pos Maildir &amp; RocksDB</p>
			</div>

			<!-- Col 4: Relay Outbound Default -->
			<div class="flex flex-col justify-between gap-2 p-4">
				<div class="flex flex-col gap-1">
					<div class="flex items-center justify-between gap-2">
						<span class="text-xs text-muted-foreground">Relay Outbound Default</span>
						<StatusBadge
							tone={mailStatus?.global_relay?.host ? 'info' : 'success'}
							label={mailStatus?.global_relay?.host
								? (mailStatus.global_relay.provider?.toUpperCase() || 'RELAY AKTIF')
								: 'SMTP LOKAL'}
						/>
					</div>
					<p class="truncate font-mono text-xs text-muted-foreground">
						{#if mailStatus?.global_relay?.host}
							{mailStatus.global_relay.host}:{mailStatus.global_relay.port}
						{:else}
							Stalwart SMTP (Direct MX)
						{/if}
					</p>
				</div>
				<div class="flex items-center justify-between text-[11px] text-muted-foreground">
					<span>Override: {Object.keys(mailStatus?.domain_relays || {}).length} domain</span>
					<Button
						variant="ghost"
						size="xs"
						class="h-6 px-1.5 text-[11px]"
						onclick={() => openRelayModal('global')}
					>
						Ubah
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
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">Domain &amp; Outbound SMTP</h2>
								<Badge variant="secondary" class="tabular-nums">{allMailDomains.length}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								Status domain mail, kredensial admin, webmail, dan rute pengiriman SMTP lokal / relay pihak ketiga.
							</p>
						{:else if activeTab === 'accounts'}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">Daftar Akun Email</h2>
								<Badge variant="secondary" class="tabular-nums">{flatAccounts.length}</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								Kelola akun kotak pos user, hak akses administrator, dan kata sandi di server Stalwart.
							</p>
						{:else}
							<div class="flex items-center gap-2">
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">Status Port &amp; Protokol Mail</h2>
								<Badge variant="secondary" class="tabular-nums">{openPortCount}/{stalwartPorts.length} Aktif</Badge>
							</div>
							<p class="text-xs text-muted-foreground">
								Diagnostik listener port lokal dan uji keterjangkauan dari internet (Firewall / ISP check).
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
								Menyegarkan...
							{:else}
								<RefreshCwIcon data-icon="inline-start" />
								Segarkan
							{/if}
						</Button>
						{#if activeTab === 'domains'}
							<Button variant="outline" size="sm" onclick={() => openRelayModal('global')}>
								<SettingsIcon data-icon="inline-start" />
								Setel Relay
							</Button>
							<Button size="sm" onclick={openSetupModal}>
								<MailPlusIcon data-icon="inline-start" />
								Setup Domain
							</Button>
						{:else if activeTab === 'accounts'}
							<Button size="sm" onclick={() => openAddAccount(domainFilter)}>
								<UserPlusIcon data-icon="inline-start" />
								Tambah Email
							</Button>
						{/if}
					</div>
				</div>

				<div class="px-4">
					<Tabs.List
						variant="line"
						aria-label="Tab Pengelolaan Mail Server"
						class="h-11 group-data-horizontal/tabs:h-11 w-full flex-nowrap items-stretch justify-start gap-1.5 overflow-x-auto overflow-y-hidden rounded-none bg-transparent p-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden sm:gap-2"
					>
						<Tabs.Trigger
							value="domains"
							class="h-full flex-none shrink-0 whitespace-nowrap rounded-none border-0 px-2.5 py-0 text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 focus-visible:ring-0 data-active:text-foreground group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-0 group-data-horizontal/tabs:after:h-0 after:border-b-2 after:border-primary after:bg-transparent sm:px-3"
						>
							<GlobeIcon aria-hidden="true" class="size-3.5" />
							<span>Domain &amp; Relay</span>
							<span class="rounded-full bg-muted px-1.5 py-0.5 text-[11px] leading-none font-medium tabular-nums text-muted-foreground">
								{allMailDomains.length}
							</span>
						</Tabs.Trigger>

						<Tabs.Trigger
							value="accounts"
							class="h-full flex-none shrink-0 whitespace-nowrap rounded-none border-0 px-2.5 py-0 text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 focus-visible:ring-0 data-active:text-foreground group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-0 group-data-horizontal/tabs:after:h-0 after:border-b-2 after:border-primary after:bg-transparent sm:px-3"
						>
							<UsersIcon aria-hidden="true" class="size-3.5" />
							<span>Akun Email</span>
							<span class="rounded-full bg-muted px-1.5 py-0.5 text-[11px] leading-none font-medium tabular-nums text-muted-foreground">
								{flatAccounts.length}
							</span>
						</Tabs.Trigger>

						<Tabs.Trigger
							value="ports"
							class="h-full flex-none shrink-0 whitespace-nowrap rounded-none border-0 px-2.5 py-0 text-sm font-medium focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2 focus-visible:ring-0 data-active:text-foreground group-data-horizontal/tabs:after:inset-x-0 group-data-horizontal/tabs:after:bottom-0 group-data-horizontal/tabs:after:h-0 after:border-b-2 after:border-primary after:bg-transparent sm:px-3"
						>
							<ShieldCheckIcon aria-hidden="true" class="size-3.5" />
							<span>Port &amp; Protokol</span>
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
									placeholder="Cari domain mail..."
									aria-label="Cari domain mail"
									bind:value={domainSearch}
								/>
							</InputGroup.Root>
						</search>

						<div class="flex items-center gap-2">
							<span class="text-xs text-muted-foreground">
								{filteredMailDomains.length} dari {allMailDomains.length} domain
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
									<Empty.Title>Belum ada domain mail</Empty.Title>
									<Empty.Description>
										Daftarkan domain mail pertama untuk mulai mengelola kotak pos Stalwart dan webmail SnappyMail.
									</Empty.Description>
								</Empty.Header>
								<Empty.Content>
									<Button size="sm" onclick={openSetupModal}>
										<MailPlusIcon data-icon="inline-start" />
										Setup Domain Pertama
									</Button>
								</Empty.Content>
							</Empty.Root>
						</div>
					{:else if filteredMailDomains.length === 0}
						<div class="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center">
							<p class="text-sm text-muted-foreground">Tidak ada domain yang cocok dengan "{domainSearch}".</p>
							<Button variant="outline" size="xs" onclick={() => (domainSearch = '')}>
								Reset Pencarian
							</Button>
						</div>
					{:else}
						<div class="p-3 sm:p-0">
							<ResponsiveTableWrapper label="Tabel Status Outbound SMTP per Domain" class="rounded-none border-0 bg-transparent">
								<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
									<Table.Header>
										<Table.Row>
											<Table.Head scope="col">Domain</Table.Head>
											<Table.Head scope="col">Mode / Provider</Table.Head>
											<Table.Head scope="col">SMTP / Relay Host</Table.Head>
											<Table.Head scope="col">Username / Sumber</Table.Head>
											<Table.Head scope="col">Panel Admin</Table.Head>
											<Table.Head scope="col">Webmail</Table.Head>
											<Table.Head scope="col" class="text-right">Aksi</Table.Head>
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
												<Table.Cell data-label="Domain" class="font-semibold">{dom.domain}</Table.Cell>
												<Table.Cell data-label="Provider">
													{#if isLocalOverride}
														<StatusBadge tone="success" label="SMTP LOKAL (DIRECT)" />
													{:else if activeRelay?.host}
														<StatusBadge
															tone={activeRelay.provider === 'resend'
																? 'info'
																: activeRelay.provider === 'brevo'
																	? 'success'
																	: 'warning'}
															label={`${isOverride ? '' : 'DEFAULT · '}${activeRelay.provider.toUpperCase()}`}
														/>
													{:else}
														<StatusBadge tone="success" label="DEFAULT · SMTP LOKAL" />
													{/if}
												</Table.Cell>
												<Table.Cell data-label="Host:Port" class="font-mono text-xs">
													{#if isLocalOverride}
														Stalwart SMTP (Direct MX)
													{:else if activeRelay?.host}
														{activeRelay.host}:{activeRelay.port}
													{:else}
														Stalwart SMTP (Direct MX)
													{/if}
												</Table.Cell>
												<Table.Cell data-label="Username">
													<div class="text-xs">{activeRelay?.username || '-'}</div>
													<div class="text-[11px] text-muted-foreground">
														{#if isLocalOverride}
															override SMTP lokal
														{:else if domRelay?.host}
															override domain
														{:else if activeRelay?.host}
															ikut relay default
														{:else}
															SMTP lokal bawaan
														{/if}
													</div>
												</Table.Cell>
												<Table.Cell data-label="Admin">
													<Button
														variant="outline"
														size="xs"
														href={dom.admin_url}
														target="_blank"
														rel="noopener noreferrer"
													>
														Panel
														<ExternalLinkIcon data-icon="inline-end" />
													</Button>
												</Table.Cell>
												<Table.Cell data-label="Webmail">
													<Button
														variant="outline"
														size="xs"
														href={dom.webmail}
														target="_blank"
														rel="noopener noreferrer"
													>
														Webmail
														<ExternalLinkIcon data-icon="inline-end" />
													</Button>
												</Table.Cell>
												<Table.Cell data-label="Aksi" class="text-right">
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
																			Memperbaiki...
																		{:else if pendingAction === 'reset-admin'}
																			<Spinner data-icon="inline-start" />
																			Mereset...
																		{:else if pendingAction === 'unset-relay'}
																			<Spinner data-icon="inline-start" />
																			Menghapus...
																		{:else}
																			Aksi
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
																		<span>{isOverride ? 'Ubah Relay' : 'Setel Relay'}</span>
																	</DropdownMenu.Item>
																	{#if isOverride}
																		<DropdownMenu.Item
																				disabled={Boolean(pendingAction)}
																onclick={() => removeRelayOverride(dom.domain)}
																		>
																			<RefreshCwIcon />
																			<span>Hapus Override</span>
																		</DropdownMenu.Item>
																	{/if}
																	<DropdownMenu.Item
																		disabled={Boolean(pendingAction)}
														onclick={() => resetAdmin(dom.domain)}
																	>
																		<KeyRoundIcon />
																		<span>Reset Admin</span>
																	</DropdownMenu.Item>
																	<DropdownMenu.Item
																		disabled={Boolean(pendingAction)}
														onclick={() => repairDomain(dom.domain)}
																	>
																		<WrenchIcon />
																		<span>Perbaiki</span>
																	</DropdownMenu.Item>
																	<DropdownMenu.Separator />
																	<DropdownMenu.Item
																		variant="destructive"
															disabled={Boolean(pendingAction)}
														onclick={() => openDeleteDomainModal(dom.domain)}
																	>
																		<Trash2Icon />
																		<span>Hapus Domain</span>
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
						itemLabel="domain email"
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
										placeholder="Cari email / nama..."
										aria-label="Cari akun email"
										bind:value={accountSearch}
									/>
								</InputGroup.Root>
							</search>

							<Select.Root type="single" bind:value={domainFilter}>
								<Select.Trigger class="h-8 w-full sm:w-44 text-xs" aria-label="Filter domain email">
									{domainFilter || 'Semua Domain'}
								</Select.Trigger>
								<Select.Content>
									<Select.Group>
										<Select.Item value="">Semua Domain</Select.Item>
										{#each mailStatus?.domains || [] as dom (dom.domain)}
											<Select.Item value={dom.domain}>{dom.domain}</Select.Item>
										{/each}
									</Select.Group>
								</Select.Content>
							</Select.Root>
						</div>

						<div class="flex items-center gap-2">
							<span class="text-xs text-muted-foreground">
								{filteredAccounts.length} dari {flatAccounts.length} akun
							</span>
						</div>
					</div>

					{#if accountsError}
						<div class="flex flex-col items-center justify-center gap-3 px-4 py-10 text-center">
							<p class="text-sm text-destructive">{accountsError}</p>
							<Button variant="outline" size="sm" onclick={() => loadMail(true)}>
								<RefreshCwIcon data-icon="inline-start" />
								Coba Lagi
							</Button>
						</div>
					{:else if flatAccounts.length === 0}
						<div class="p-4">
							<Empty.Root class="border-0 py-8">
								<Empty.Header>
									<Empty.Media variant="icon">
										<UsersIcon />
									</Empty.Media>
									<Empty.Title>Belum ada akun email</Empty.Title>
									<Empty.Description>
										Tambahkan kotak pos user pertama Anda untuk mulai mengirim dan menerima pesan.
									</Empty.Description>
								</Empty.Header>
								<Empty.Content>
									<Button size="sm" onclick={() => openAddAccount(domainFilter)}>
										<UserPlusIcon data-icon="inline-start" />
										Tambah Email Sekarang
									</Button>
								</Empty.Content>
							</Empty.Root>
						</div>
					{:else if filteredAccounts.length === 0}
						<div class="flex flex-col items-center justify-center gap-2 px-4 py-10 text-center">
							<p class="text-sm text-muted-foreground">Tidak ada akun email yang cocok dengan kriteria filter.</p>
							<Button
								variant="outline"
								size="xs"
								onclick={() => {
									accountSearch = '';
									domainFilter = '';
								}}
							>
								Reset Filter &amp; Pencarian
							</Button>
						</div>
					{:else}
						<div class="p-3 sm:p-0">
							<ResponsiveTableWrapper label="Tabel Akun Email" class="rounded-none border-0 bg-transparent">
								<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
									<Table.Header>
										<Table.Row>
											<Table.Head scope="col">Domain</Table.Head>
											<Table.Head scope="col">Alamat Email</Table.Head>
											<Table.Head scope="col">Nama / ID Akun</Table.Head>
											<Table.Head scope="col">Tipe / Role</Table.Head>
											<Table.Head scope="col">Outbound Relay</Table.Head>
											<Table.Head scope="col" class="text-right">Aksi</Table.Head>
										</Table.Row>
									</Table.Header>
									<Table.Body>
										{#each paginatedAccounts as item (item.account.id)}
											{@const grp = item}
											{@const acc = item.account}
											{@const isPending = Boolean(accountPending[acc.email])}
											<Table.Row>
												<Table.Cell data-label="Domain" class="font-semibold">{grp.domain}</Table.Cell>
												<Table.Cell data-label="Email" class="font-medium">{acc.email}</Table.Cell>
												<Table.Cell data-label="ID" class="font-mono text-xs text-muted-foreground">
													{acc.name || acc.id}
												</Table.Cell>
												<Table.Cell data-label="Role">
													<StatusBadge
														tone={acc.is_admin ? 'warning' : 'success'}
														label={acc.is_admin ? 'Administrator' : 'Kotak Pos User'}
													/>
												</Table.Cell>
												<Table.Cell data-label="Relay">
													{#if grp.relay?.host}
														<Badge variant="secondary">
															{grp.relay.provider.toUpperCase()} ({grp.relay.host})
														</Badge>
													{:else if mailStatus?.outbound_routes?.[grp.domain] === 'mx'}
														<Badge variant="outline">SMTP Lokal (Direct)</Badge>
													{:else if mailStatus?.global_relay?.host}
														<Badge variant="secondary">
															DEFAULT ({mailStatus.global_relay.provider.toUpperCase()})
														</Badge>
													{:else}
														<Badge variant="outline">Default (SMTP Lokal)</Badge>
													{/if}
												</Table.Cell>
												<Table.Cell data-label="Aksi" class="text-right">
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
																			Menghapus...
																		{:else}
																			Aksi
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
																<span>Buka Webmail</span>
															</a>
														{/snippet}
													</DropdownMenu.Item>
													<DropdownMenu.Item
														disabled={isPending}
													onclick={() => openEditAccountPass(acc.email)}
													>
														<KeyRoundIcon />
														<span>Ubah Password</span>
													</DropdownMenu.Item>
													{#if !acc.is_admin}
														<DropdownMenu.Separator />
														<DropdownMenu.Item
															variant="destructive"
															disabled={isPending}
															onclick={() => deleteAccount(acc.email)}
														>
															<Trash2Icon />
															<span>Hapus Akun</span>
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
						itemLabel="akun email"
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
								Diagnostik Port Stalwart Mail Server &amp; Akses Internet
							</span>
						</div>
						<span class="text-xs text-muted-foreground">
							{openPortCount} dari {stalwartPorts.length} port beroperasi
						</span>
					</div>

					<div class="p-3 sm:p-0">
						<ResponsiveTableWrapper label="Tabel Status Port & Protokol Mail" class="rounded-none border-0 bg-transparent">
							<Table.Root class="sm:[&_th:first-child]:pl-4 sm:[&_td:first-child]:pl-4 sm:[&_th:last-child]:pr-4 sm:[&_td:last-child]:pr-4">
								<Table.Header>
									<Table.Row>
										<Table.Head scope="col">Port</Table.Head>
										<Table.Head scope="col">Protokol / Layanan</Table.Head>
										<Table.Head scope="col">Listener Lokal</Table.Head>
										<Table.Head scope="col">Akses Internet / Publik</Table.Head>
										<Table.Head scope="col">Keterangan / Rekomendasi</Table.Head>
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
											<Table.Cell data-label="Port" class="font-mono text-sm font-bold">
												{item.port}
											</Table.Cell>
											<Table.Cell data-label="Layanan">
												<div class="font-semibold">{item.label}</div>
												<div class="font-mono text-xs text-muted-foreground">
													{item.port === 25
														? 'SMTP Inbound (Server-to-Server)'
														: item.port === 465
															? 'SMTPS (Implicit TLS Client Submission)'
															: item.port === 587
																? 'Submission (STARTTLS Client Auth)'
																: item.port === 993
																	? 'IMAPS (Secure Mailbox Fetch)'
																	: 'Stalwart Web Management UI'}
												</div>
											</Table.Cell>
											<Table.Cell data-label="Lokal">
												{#if isInsideOpen}
													<StatusBadge tone="success" label="Listening (Aktif)" />
												{:else}
													<StatusBadge tone="destructive" label="Mati (Nonaktif)" />
												{/if}
											</Table.Cell>
											<Table.Cell data-label="Publik">
												{#if isInternal}
													<StatusBadge tone="neutral" label="Internal Saja (Nginx Proxy)" />
												{:else if isLocalMode}
													<StatusBadge tone="info" label="Mode Lokal (Non-Publik)" />
												{:else if isOutsideOpen}
													<StatusBadge tone="success" label="Terbuka Penuh" />
												{:else if isOutsideBlocked}
													<StatusBadge tone="warning" label="Terblokir Firewall / ISP" />
												{:else if isLocalUnverified}
													<StatusBadge tone="warning" label="Belum Terverifikasi" />
												{:else}
													<StatusBadge tone="destructive" label="Tertutup" />
												{/if}
											</Table.Cell>
											<Table.Cell data-label="Keterangan" class="text-xs text-muted-foreground">
												{#if item.port === 25}
													{#if isOutsideBlocked}
														Port 25 sering diblokir oleh provider internet / cloud untuk mencegah spam. Gunakan <strong>Relay Outbound SMTP</strong> (Resend / Brevo) untuk pengiriman email keluar.
													{:else}
														Wajib terbuka dua arah agar mail server lain dapat mengirim email masuk (inbound) ke domain Anda.
													{/if}
												{:else if item.port === 465 || item.port === 587}
													Digunakan oleh aplikasi email (Outlook, Thunderbird, Apple Mail, dsb) untuk otentikasi kirim pesan.
												{:else if item.port === 993}
													Digunakan oleh SnappyMail Webmail dan aplikasi email untuk sinkronisasi folder inbox (IMAP over SSL).
												{:else}
													Panel admin Stalwart diakses aman melalui reverse proxy Nginx pada port 8080 internal.
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
										Port Terblokir dari Luar ({blockedOutsidePorts.map((p) => p.port).join(', ')})
									</Alert.Title>
									<Alert.Description class="text-xs leading-relaxed text-muted-foreground">
										Port berhasil listen di server lokal, tetapi koneksi dari internet terblokir. Pastikan port forwarding pada router/firewall VPS dibuka (Security Group AWS/GCP/Alibaba/DigitalOcean). Bila Port 25 terblokir oleh ISP, Anda dapat menggunakan tombol <strong>Setel Relay</strong> di atas untuk merutekan outbound via Resend atau Brevo.
									</Alert.Description>
								</Alert.Root>
							{:else}
								<div class="flex items-center gap-2 text-xs text-muted-foreground">
									<CheckIcon class="size-4 text-success" />
									<span>Seluruh pengujian port listener &amp; konektivitas mail server berjalan optimal.</span>
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
			<Dialog.Title>Tambah Akun Email Baru</Dialog.Title>
			<Dialog.Description>Domain email harus sudah terdaftar di Stalwart.</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={submitAddAccount} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-new-email">Alamat Email Lengkap</Field.FieldLabel>
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
					<Field.FieldLabel for="mail-new-password">Password Kotak Pos</Field.FieldLabel>
					<PasswordInput
						id="mail-new-password"
						autocomplete="new-password"
						placeholder="Minimal 8 karakter..."
						bind:value={newEmailPass}
						required
					/>
					<Field.FieldDescription>Isi manual atau klik Generate untuk membuat password kuat acak.</Field.FieldDescription>
				</Field.Field>
			</Field.FieldGroup>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (addAccOpen = false)}>Batal</Button>
				<Button type="submit" disabled={addingAcc}>
					{#if addingAcc}
						<Spinner data-icon="inline-start" />
						Membuat Akun...
					{:else}
						Buat Akun Email
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
			<Dialog.Title>Ubah Password Akun Email</Dialog.Title>
			<Dialog.Description>
				Perbarui kata sandi kotak pos untuk <strong class="font-mono">{editPassEmail}</strong>.
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={submitEditAccountPass} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-edit-password">Password Baru</Field.FieldLabel>
					<PasswordInput
						id="mail-edit-password"
						autocomplete="new-password"
						placeholder="Minimal 8 karakter..."
						bind:value={editPassValue}
						required
					/>
					<Field.FieldDescription>Isi manual atau klik Generate untuk membuat password kuat acak.</Field.FieldDescription>
				</Field.Field>
			</Field.FieldGroup>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (editPassOpen = false)}>Batal</Button>
				<Button type="submit" disabled={editingPass}>
					{#if editingPass}
						<Spinner data-icon="inline-start" />
						Menyimpan...
					{:else}
						Simpan Password
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
			<Dialog.Title>Hapus Domain Mail</Dialog.Title>
			<Dialog.Description>
				Domain <strong>{deleteDomTarget}</strong> akan dihapus dari Stalwart (termasuk seluruh akun email), vhost webmail/admin di VPS, relay Postfix, dan stream mail.
			</Dialog.Description>
		</Dialog.Header>
		<form onsubmit={submitDeleteDomain} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-del-confirm">Ketik nama domain untuk konfirmasi</Field.FieldLabel>
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
						Hapus juga record DNS milik domain ini (A mail/mx/webmail/mailadmin, MX, DMARC, SPF bawaan)
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (deleteDomOpen = false)}>Batal</Button>
				<Button type="submit" variant="destructive" disabled={deletingDom}>
					{#if deletingDom}
						<Spinner data-icon="inline-start" />
						Menghapus...
					{:else}
						<Trash2Icon data-icon="inline-start" />
						Hapus Domain
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
			<Dialog.Title>Konfigurasi Outbound SMTP</Dialog.Title>
			<Dialog.Description>
				Gunakan SMTP lokal bawaan Stalwart (direct MX) atau arahkan pengiriman keluar lewat relay SMTP pihak ketiga.
			</Dialog.Description>
		</Dialog.Header>

		<form id="mail-relay-form" onsubmit={submitRelay} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-relay-domain">Domain Target</Field.FieldLabel>
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
								? 'Semua Domain (default)'
								: relayTargetDomain}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="global">Semua Domain (default)</Select.Item>
								{#each mailStatus?.domains || [] as dom (dom.domain)}
									<Select.Item value={dom.domain}>{dom.domain}</Select.Item>
								{/each}
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="mail-relay-provider">Mode / Provider Outbound</Field.FieldLabel>
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
								? 'SMTP Lokal Stalwart (Direct MX / Tanpa Relay)'
								: relayProvider === 'resend'
									? 'Resend (smtp.resend.com:465 - Implicit TLS)'
									: relayProvider === 'brevo'
										? 'Brevo / Sendinblue (smtp-relay.brevo.com:587)'
										: 'Custom SMTP Relay (Server / Port Kustom)'}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="local">SMTP Lokal Stalwart (Direct MX / Tanpa Relay)</Select.Item>
								<Select.Item value="resend">Resend (smtp.resend.com:465 - Implicit TLS)</Select.Item>
								<Select.Item value="brevo">Brevo / Sendinblue (smtp-relay.brevo.com:587)</Select.Item>
								<Select.Item value="custom">Custom SMTP Relay (Server / Port Kustom)</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				{#if relayProvider === 'local'}
					<Alert.Root>
						<InfoIcon />
						<Alert.Title>SMTP Lokal Bawaan Stalwart</Alert.Title>
						<Alert.Description>
							{#if relayTargetDomain === 'global'}
								Semua domain tanpa override akan mengirim email langsung menggunakan SMTP lokal Stalwart (direct MX) tanpa relay pihak ketiga.
							{:else}
								Domain <strong>{relayTargetDomain}</strong> akan mengirim email langsung melalui SMTP lokal Stalwart (direct MX).
							{/if}
						</Alert.Description>
					</Alert.Root>
				{:else if relayProvider === 'resend' || relayProvider === 'brevo'}
					<Field.Field>
						<Field.FieldLabel for="mail-relay-secret">
							{relayProvider === 'resend' ? 'API Key Resend (re_...)' : 'Master SMTP Key Brevo (xkeysib-...)'}
						</Field.FieldLabel>
						<PasswordInput
							id="mail-relay-secret"
							canGenerate={false}
							autocomplete="current-password"
							placeholder={hasStoredRelayKey
								? 'Kosongkan untuk tetap memakai key tersimpan'
								: relayProvider === 'resend'
									? 're_123456789...'
									: 'xkeysib-123456789...'}
							bind:value={relaySecret}
						/>
						<Field.FieldDescription>
							{relayProvider === 'resend'
								? 'Port 465 (Implicit TLS). Username otomatis disetel ke "resend". Dengan API Key Resend Full Access, record DNS DKIM/SPF/MX otomatis dibangun di PowerDNS saat disimpan.'
								: 'Port 587 (STARTTLS). Username otomatis diambil dari akun Brevo Anda.'}
						</Field.FieldDescription>
					</Field.Field>
				{:else}
					<div class="grid gap-3 sm:grid-cols-[2fr_1fr]">
						<Field.Field>
							<Field.FieldLabel for="mail-relay-host">Host SMTP</Field.FieldLabel>
							<Input id="mail-relay-host" placeholder="smtp.mailgun.org" bind:value={relayHost} required />
						</Field.Field>
						<Field.Field>
							<Field.FieldLabel for="mail-relay-port">Port</Field.FieldLabel>
							<Input id="mail-relay-port" inputmode="numeric" pattern="[0-9]+" bind:value={relayPortStr} />
						</Field.Field>
					</div>
					<Field.Field>
						<Field.FieldLabel for="mail-relay-user">Username SMTP</Field.FieldLabel>
						<Input id="mail-relay-user" placeholder="postmaster@domain.com" bind:value={relayUser} />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="mail-relay-pass">Password SMTP</Field.FieldLabel>
						<PasswordInput id="mail-relay-pass" canGenerate={false} autocomplete="current-password" placeholder="Password SMTP..." bind:value={relayPass} />
					</Field.Field>
				{/if}

				{#if relayProvider !== 'local'}
					<Field.Field>
						<div class="flex flex-wrap items-center justify-between gap-2">
							<Field.FieldLabel for="mail-relay-dns">Record DNS Relay (dikelola wdev)</Field.FieldLabel>
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
										Menyinkronkan...
									{:else}
										<RefreshCwIcon data-icon="inline-start" />
										Sinkronkan DNS Resend
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
								? 'Terisi otomatis dari Resend API saat Simpan / Sinkronkan DNS (TXT resend._domainkey, MX send, TXT send)'
								: 'CNAME brevo1._domainkey.domain.com b1.domain.com.dkim.brevo.com'}
							bind:value={relayDNSText}
						/>
						<Field.FieldDescription>
							Satu record per baris: <code>TYPE NAMA NILAI</code>. Otomatis disinkronkan dari Resend API atau dibersihkan bila provider diganti.
						</Field.FieldDescription>
					</Field.Field>
				{/if}
			</Field.FieldGroup>
		</form>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (relayOpen = false)}>Batal</Button>
			<Button type="submit" form="mail-relay-form" disabled={savingRelay}>
				{#if savingRelay}
					<Spinner data-icon="inline-start" />
					Menyimpan...
				{:else}
					Simpan Konfigurasi
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- Setup Domain & Mail Server Dialog -->
<Dialog.Root bind:open={setupOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>Setup Domain &amp; Mail Server Baru</Dialog.Title>
			<Dialog.Description>
				{#if modeStore.mode === 'vps'}
					Mengonfigurasi otomatis Stalwart Mail Server, SnappyMail, DNS record (A, MX, SPF, DMARC) di PowerDNS lokal, Nginx, dan SSL Let's Encrypt.
				{:else if modeStore.mode === 'local'}
					Mengonfigurasi otomatis Stalwart, SnappyMail, vhost Nginx, /etc/hosts lokal, DNS (bila PowerDNS tersedia), serta SSL lokal tanpa NetBird.
				{:else}
					Mengonfigurasi otomatis Stalwart lokal, SnappyMail, DNS record (A, MX, SPF, DMARC) di VPS Edge, serta SSL Let's Encrypt.
				{/if}
			</Dialog.Description>
		</Dialog.Header>

		<form id="mail-setup-form" onsubmit={submitSetupMail} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
			{#if preflightLoading}
				<div class="flex items-center gap-2 text-xs text-muted-foreground">
					<Spinner />
					<span>Memeriksa prasyarat mail...</span>
				</div>
			{:else if preflight}
				{#if preflight.blockers?.length}
					<Alert.Root variant="destructive">
						<Alert.Title>Blocker (Setup akan gagal)</Alert.Title>
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
						<Alert.Title>Peringatan Prasyarat</Alert.Title>
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
						<Alert.Title>Prasyarat Lengkap</Alert.Title>
						<Alert.Description>
							{#if (preflight.mode || modeStore.mode) === 'local'}
								Stalwart dan stalwart-cli siap digunakan dalam Mode Lokal Standalone (tanpa NetBird).
							{:else if (preflight.mode || modeStore.mode) === 'vps'}
								Stalwart, stalwart-cli, dan PowerDNS lokal siap digunakan (Mode VPS Standalone tanpa NetBird).
							{:else}
								Stalwart, stalwart-cli, NetBird, dan Edge Gateway siap digunakan.
							{/if}
						</Alert.Description>
					</Alert.Root>
				{/if}
			{/if}

			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-setup-domain-select">
						Domain Mail {modeStore.mode === 'edge' ? '(PowerDNS Edge)' : modeStore.mode === 'vps' ? '(PowerDNS VPS)' : '(Lokal / PowerDNS)'}
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
								? '+ Masukkan Domain Manual...'
								: setupDomainChoice || 'Pilih domain'}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								{#each zoneDomains as dom (dom)}
									<Select.Item value={dom}>{dom}</Select.Item>
								{/each}
								<Select.Item value="__custom__">+ Masukkan Domain Kustom / Manual...</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				{#if setupDomainChoice === '__custom__'}
					<Field.Field>
						<Field.FieldLabel for="mail-setup-custom-dom">Masukkan Domain Manual</Field.FieldLabel>
						<Input
							id="mail-setup-custom-dom"
							placeholder="contoh: domainanda.com"
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
							Aktifkan SSL Lokal Otomatis (mkcert / self-signed untuk webmail &amp; mailadmin)
						{:else}
							Aktifkan SSL Let's Encrypt Otomatis (webmail &amp; mailadmin)
						{/if}
					</Field.FieldLabel>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="mail-setup-relay-prov">Pengiriman Keluar (Outbound SMTP)</Field.FieldLabel>
					<Select.Root type="single" bind:value={setupRelayProv}>
						<Select.Trigger id="mail-setup-relay-prov" class="w-full">
							{setupRelayProv === ''
								? mailStatus?.global_relay?.host
									? `Gunakan default saat ini (${mailStatus.global_relay.provider.toUpperCase()})`
									: 'SMTP Lokal Stalwart (Default - Tanpa Relay)'
								: setupRelayProv === 'local'
									? 'SMTP Lokal Stalwart (Direct MX - Tanpa Relay)'
									: setupRelayProv === 'resend'
										? 'Resend (smtp.resend.com:465)'
										: setupRelayProv === 'brevo'
											? 'Brevo (smtp-relay.brevo.com:587)'
											: 'Custom SMTP Relay Server'}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="">
									{mailStatus?.global_relay?.host
										? `Gunakan default saat ini (${mailStatus.global_relay.provider.toUpperCase()})`
										: 'SMTP Lokal Stalwart (Default - Tanpa Relay)'}
								</Select.Item>
								{#if mailStatus?.global_relay?.host}
									<Select.Item value="local">SMTP Lokal Stalwart (Direct MX - Tanpa Relay)</Select.Item>
								{/if}
								<Select.Item value="resend">Resend (smtp.resend.com:465)</Select.Item>
								<Select.Item value="brevo">Brevo (smtp-relay.brevo.com:587)</Select.Item>
								<Select.Item value="custom">Custom SMTP Relay Server</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
					<Field.FieldDescription>
						{#if modeStore.mode === 'local'}
							Default memakai SMTP lokal Stalwart tanpa relay; pilih Resend/Brevo/Custom bila ingin mengirim ke internet via relay.
						{:else}
							Opsional: biarkan default untuk langsung memakai SMTP lokal Stalwart (direct MX), atau pilih Resend/Brevo/Custom SMTP Relay.
						{/if}
					</Field.FieldDescription>
				</Field.Field>

				{#if setupRelayProv === 'resend'}
					<Field.Field>
						<Field.FieldLabel for="mail-setup-resend-key">API Key Resend (re_...)</Field.FieldLabel>
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
						<Field.FieldLabel for="mail-setup-brevo-user">Brevo SMTP Login (Email)</Field.FieldLabel>
						<Input id="mail-setup-brevo-user" placeholder="akun@domain.com" bind:value={setupBrevoUser} />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="mail-setup-brevo-key">Brevo SMTP Key (xsmtpsib-...)</Field.FieldLabel>
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
							<Field.FieldLabel for="mail-setup-custom-host">SMTP Host</Field.FieldLabel>
							<Input id="mail-setup-custom-host" placeholder="mail.smtp.com" bind:value={setupCustomHost} required />
						</Field.Field>
						<Field.Field>
							<Field.FieldLabel for="mail-setup-custom-port">Port</Field.FieldLabel>
							<Input
								id="mail-setup-custom-port"
								inputmode="numeric"
								pattern="[0-9]+"
								bind:value={setupCustomPortStr}
							/>
						</Field.Field>
					</div>
					<Field.Field>
						<Field.FieldLabel for="mail-setup-custom-user">Username</Field.FieldLabel>
						<Input id="mail-setup-custom-user" placeholder="smtp-user" bind:value={setupCustomUser} />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="mail-setup-custom-pass">Password</Field.FieldLabel>
						<PasswordInput id="mail-setup-custom-pass" canGenerate={false} autocomplete="current-password" placeholder="Password SMTP..." bind:value={setupCustomPass} />
					</Field.Field>
				{/if}
			</Field.FieldGroup>
		</form>

		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (setupOpen = false)}>Batal</Button>
			<Button type="submit" form="mail-setup-form" disabled={settingUp}>
				{#if settingUp}
					<Spinner data-icon="inline-start" />
					Memulai Setup...
				{:else}
					Mulai Setup Domain
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
