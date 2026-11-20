<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
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

	let mailStatus = $state<MailStatus | null>(null);
	let domainGroups = $state<DomainAccounts[]>([]);
	let zones = $state<ZoneInfo[]>([]);
	let domainFilter = $state('');
	let accountsError = $state('');

	// Add Email Account Modal
	let addAccOpen = $state(false);
	let newEmail = $state('');
	let newEmailPass = $state('');
	let addingAcc = $state(false);

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
	let savingRelay = $state(false);

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

	const totalAccounts = $derived(
		domainGroups.reduce((sum, g) => sum + (g.accounts?.length ?? 0), 0)
	);

	const filteredGroups = $derived(
		domainFilter ? domainGroups.filter((g) => g.domain === domainFilter) : domainGroups
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

	async function loadMail() {
		const [stRes, accRes, zoneRes] = await Promise.allSettled([
			api.getMailStatus(),
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
	}

	onMount(loadMail);

	$effect(() => {
		if (cp.refreshTick > 0) loadMail();
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

	async function deleteAccount(email: string) {
		const ok = await cp.confirm({
			title: 'Hapus Akun Email',
			description: `Hapus akun email ${email}? Semua email di kotak pos tersebut akan ikut terhapus.`,
			confirmText: 'Hapus Akun',
			destructive: true
		});
		if (!ok) return;
		try {
			const res = await api.deleteMailAccount(email);
			toast.success(res.message || `Akun ${email} dihapus.`);
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus akun email');
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
		try {
			await api.repairMailDomain(domain);
			cp.openTerminal(`Perbaiki Domain Mail: ${domain}`);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memulai perbaikan domain');
		}
	}

	async function resetAdmin(domain: string) {
		const ok = await cp.confirm({
			title: 'Reset Password Admin',
			description: `Reset password admin Stalwart untuk domain '${domain}'? Password baru akan dibuat otomatis dan disimpan di /etc/wdev/mail-admin.cred.`,
			confirmText: 'Reset Password'
		});
		if (!ok) return;
		try {
			await api.resetMailAdmin(domain);
			toast.success(`Password admin untuk ${domain} berhasil direset.`);
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal mereset password admin');
		}
	}

	async function loadRelayDNSRecords(domain: string, provider: string) {
		if (!domain || domain === 'global') {
			relayDNSText = '';
			return;
		}
		try {
			const list = await api.listMailRelayDNS(domain);
			relayDNSText = (list ?? [])
				.filter((r) => !provider || r.provider.toLowerCase() === provider)
				.map((r) => `${r.type} ${r.name} ${r.content}`)
				.join('\n');
		} catch {
			relayDNSText = '';
		}
	}

	function openRelayModal(domain = 'global') {
		relayTargetDomain = domain || 'global';
		const existing =
			relayTargetDomain === 'global'
				? mailStatus?.global_relay
				: mailStatus?.domain_relays?.[relayTargetDomain] ?? mailStatus?.global_relay;

		relaySecret = '';
		relayPass = '';
		if (existing?.host) {
			const p = existing.provider.toLowerCase();
			relayProvider = p === 'resend' || p === 'brevo' || p === 'custom' ? p : 'custom';
			relayHost = existing.host || '';
			relayPortStr = String(existing.port || 587);
			relayUser = existing.username || '';
		} else {
			relayProvider = 'brevo';
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
		if (dns.added?.length) parts.push(`${dns.added.length} record baru ditambahkan`);
		if (dns.manual?.length) parts.push(`${dns.manual.length} record perlu manual`);
		return parts.join(', ');
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

		if (relayProvider === 'resend' || relayProvider === 'brevo') {
			if (!relaySecret.trim()) {
				toast.warning(`API / SMTP Key untuk ${relayProvider.toUpperCase()} wajib diisi.`);
				return;
			}
			payload.password = relaySecret.trim();
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
			if (lines.length > 0 && relayTargetDomain !== 'global') {
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
				`Relay domain ${relayTargetDomain} berhasil disimpan.${dnsNote ? ` (${dnsNote})` : ''}`
			);
			relayOpen = false;
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menyetel relay');
		} finally {
			savingRelay = false;
		}
	}

	async function removeRelayOverride(domain: string) {
		const ok = await cp.confirm({
			title: 'Hapus Override Relay',
			description: `Hapus override relay untuk ${domain}? Domain akan kembali memakai relay default.`,
			confirmText: 'Hapus Override',
			destructive: true
		});
		if (!ok) return;
		try {
			await api.removeMailRelay(domain);
			toast.success(`Override relay ${domain} dihapus.`);
			await loadMail();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus override relay');
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

		const ok = await cp.confirm({
			title: 'Konfirmasi Setup Mail Server',
			description: `Mulai setup otomatis Mail Server & DNS untuk domain "${domain}"?`,
			confirmText: 'Mulai Setup'
		});
		if (!ok) return;

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
			setupOpen = false;
			cp.openTerminal(`Setup Mail Server: ${domain}`);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memulai setup mail');
		} finally {
			settingUp = false;
		}
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Mail Server &amp; Outbound SMTP Relay</h1>
			<p class="text-sm text-muted-foreground">
				Manajemen kotak pos Stalwart, webmail SnappyMail, serta outbound SMTP relay-only per domain.
			</p>
		</div>
		<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
			<Button variant="outline" size="sm" onclick={loadMail}>
				<RefreshCwIcon data-icon="inline-start" />
				Segarkan
			</Button>
			<Button variant="outline" size="sm" onclick={() => openRelayModal('global')}>
				<SettingsIcon data-icon="inline-start" />
				Setel Relay
			</Button>
			<Button size="sm" class="col-span-2 sm:col-span-1" onclick={openSetupModal}>
				<MailPlusIcon data-icon="inline-start" />
				Setup Domain Mail
			</Button>
		</div>
	</div>

	{#if mailStatus && (!mailStatus.stalwart_active || mailStatus.domains.length === 0)}
		<Alert.Root>
			<InfoIcon />
			<Alert.Title>Setup Domain &amp; Mail Server</Alert.Title>
			<Alert.Description class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<span>
					Inisialisasi engine Stalwart Mail Server lokal, Webmail SnappyMail, record DNS (MX, SPF, DMARC), dan SSL Let's Encrypt.
				</span>
				<Button size="xs" onclick={openSetupModal}>Mulai Setup Domain</Button>
			</Alert.Description>
		</Alert.Root>
	{/if}

	<!-- Metrics -->
	<div class="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
		<Card.Root>
			<Card.Header>
				<Card.Description>Stalwart Mail Engine</Card.Description>
				<div class="pt-1">
					<StatusBadge
						tone={mailStatus?.stalwart_active ? 'success' : 'warning'}
						label={mailStatus?.stalwart_active ? 'Aktif (Running)' : 'Nonaktif'}
					/>
				</div>
			</Card.Header>
			<Card.Content>
				<p class="text-xs text-muted-foreground">Port: 25, 465, 587, 993, 8080</p>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Header>
				<Card.Description>SnappyMail Webmail</Card.Description>
				<div class="pt-1">
					<StatusBadge
						tone={mailStatus?.snappymail_active ? 'success' : 'warning'}
						label={mailStatus?.snappymail_active ? 'Siap / Aktif' : 'Belum Terpasang'}
					/>
				</div>
			</Card.Header>
			<Card.Content>
				<p class="font-mono text-xs text-muted-foreground">/var/www/snappymail</p>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Header>
				<Card.Description>Kapasitas Disk Bebas</Card.Description>
				<Card.Title class="font-mono text-base">
					{mailStatus?.storage_free ? `${mailStatus.storage_free} Bebas` : '-'}
				</Card.Title>
			</Card.Header>
			<Card.Content>
				<p class="text-xs text-muted-foreground">Penyimpanan Maildir / RocksDB</p>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Header>
				<Card.Description>Total Domain &amp; Akun</Card.Description>
				<Card.Title class="text-base">
					{domainGroups.length} Domain, {totalAccounts} Akun
				</Card.Title>
			</Card.Header>
			<Card.Content>
				<p class="text-xs text-muted-foreground">Tersimpan di Stalwart DB</p>
			</Card.Content>
		</Card.Root>
	</div>

	<!-- Domain & Relay Table -->
	<section class="flex flex-col gap-3">
		<div>
			<h2 class="text-lg font-semibold tracking-tight">Status Domain &amp; Outbound SMTP Relay</h2>
			<p class="text-xs text-muted-foreground">
				Relay-only: email keluar selalu lewat relay default atau override per domain (Resend, Brevo, Custom SMTP).
			</p>
		</div>

		<ResponsiveTableWrapper label="Tabel Status Relay per Domain">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Domain</Table.Head>
						<Table.Head>Provider Relay</Table.Head>
						<Table.Head>Relay Host &amp; Port</Table.Head>
						<Table.Head>Username</Table.Head>
						<Table.Head>Panel Admin</Table.Head>
						<Table.Head>Webmail</Table.Head>
						<Table.Head class="text-right">Aksi</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#if !mailStatus?.domains || mailStatus.domains.length === 0}
						<Table.Row>
							<Table.Cell colspan={7} class="py-6 text-center text-muted-foreground">
								Belum ada domain mail terdaftar di sistem.
							</Table.Cell>
						</Table.Row>
					{:else}
						{#each mailStatus.domains as dom (dom.domain)}
							{@const domRelay = mailStatus.domain_relays?.[dom.domain]}
							{@const activeRelay = domRelay?.host ? domRelay : mailStatus.global_relay}
							{@const isOverride = Boolean(domRelay?.host)}
							<Table.Row>
								<Table.Cell data-label="Domain" class="font-semibold">{dom.domain}</Table.Cell>
								<Table.Cell data-label="Provider">
									{#if activeRelay?.host}
										<StatusBadge
											tone={activeRelay.provider === 'resend'
												? 'info'
												: activeRelay.provider === 'brevo'
													? 'success'
													: 'warning'}
											label={`${isOverride ? '' : 'DEFAULT · '}${activeRelay.provider.toUpperCase()}`}
										/>
									{:else}
										<StatusBadge tone="destructive" label="BELUM ADA RELAY" />
									{/if}
								</Table.Cell>
								<Table.Cell data-label="Host:Port" class="font-mono text-xs">
									{activeRelay?.host ? `${activeRelay.host}:${activeRelay.port}` : 'Outbound nonaktif'}
								</Table.Cell>
								<Table.Cell data-label="Username">
									<div class="text-xs">{activeRelay?.username || '-'}</div>
									{#if activeRelay?.host}
										<div class="text-[11px] text-muted-foreground">
											{isOverride ? 'override domain' : 'ikut relay default'}
										</div>
									{/if}
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
								<Table.Cell data-label="Aksi">
									<div class="flex flex-wrap justify-end gap-1.5">
										<Button size="xs" onclick={() => openRelayModal(dom.domain)}>
											{isOverride ? 'Ubah Relay' : 'Setel Relay'}
										</Button>
										{#if isOverride}
											<Button
												variant="outline"
												size="xs"
												onclick={() => removeRelayOverride(dom.domain)}
											>
												Hapus Override
											</Button>
										{/if}
										<Button variant="outline" size="xs" onclick={() => resetAdmin(dom.domain)}>
											<KeyRoundIcon data-icon="inline-start" />
											Reset Admin
										</Button>
										<Button variant="outline" size="xs" onclick={() => repairDomain(dom.domain)}>
											<WrenchIcon data-icon="inline-start" />
											Perbaiki
										</Button>
										<Button
											variant="destructive"
											size="xs"
											onclick={() => openDeleteDomainModal(dom.domain)}
										>
											<Trash2Icon data-icon="inline-start" />
											Hapus
										</Button>
									</div>
								</Table.Cell>
							</Table.Row>
						{/each}
					{/if}
				</Table.Body>
			</Table.Root>
		</ResponsiveTableWrapper>
	</section>

	<!-- Accounts Table -->
	<section class="flex flex-col gap-3">
		<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<h2 class="text-base font-semibold tracking-tight sm:text-lg">Daftar Akun Email per Domain</h2>
				<p class="text-xs text-muted-foreground">Akun kotak pos yang terdaftar di server Stalwart.</p>
			</div>
			<div class="flex flex-col gap-2 sm:flex-row sm:items-center">
				<Select.Root type="single" bind:value={domainFilter}>
					<Select.Trigger class="w-full sm:w-48" aria-label="Filter domain email">
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
				<Button size="sm" class="w-full sm:w-auto" onclick={() => openAddAccount(domainFilter)}>
					<UserPlusIcon data-icon="inline-start" />
					Tambah Email
				</Button>
			</div>
		</div>

		<ResponsiveTableWrapper label="Tabel Akun Email">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Domain</Table.Head>
						<Table.Head>Alamat Email</Table.Head>
						<Table.Head>Nama / ID Akun</Table.Head>
						<Table.Head>Tipe / Role</Table.Head>
						<Table.Head>Outbound Relay</Table.Head>
						<Table.Head class="text-right">Aksi</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#if accountsError}
						<Table.Row>
							<Table.Cell colspan={6} class="py-6 text-center text-muted-foreground">
								{accountsError}
							</Table.Cell>
						</Table.Row>
					{:else if filteredGroups.every((g) => (g.accounts?.length ?? 0) === 0)}
						<Table.Row>
							<Table.Cell colspan={6} class="py-6 text-center text-muted-foreground">
								Tidak ada akun email yang cocok dengan filter.
							</Table.Cell>
						</Table.Row>
					{:else}
						{#each filteredGroups as grp (grp.domain)}
							{#each grp.accounts || [] as acc (acc.id)}
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
										{:else}
											<Badge variant="outline">Relay Default</Badge>
										{/if}
									</Table.Cell>
									<Table.Cell data-label="Aksi">
										<div class="flex justify-end gap-1.5">
											<Button
												variant="outline"
												size="xs"
												href={grp.webmail}
												target="_blank"
												rel="noopener noreferrer"
											>
												Webmail
												<ExternalLinkIcon data-icon="inline-end" />
											</Button>
											{#if !acc.is_admin}
												<Button
													variant="destructive"
													size="xs"
													onclick={() => deleteAccount(acc.email)}
												>
													Hapus
												</Button>
											{/if}
										</div>
									</Table.Cell>
								</Table.Row>
							{/each}
						{/each}
					{/if}
				</Table.Body>
			</Table.Root>
		</ResponsiveTableWrapper>
	</section>
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
					<Input
						id="mail-new-password"
						type="password"
						autocomplete="new-password"
						placeholder="Minimal 8 karakter..."
						bind:value={newEmailPass}
						required
					/>
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
			<Dialog.Title>Konfigurasi Outbound SMTP Relay</Dialog.Title>
			<Dialog.Description>
				Provider pihak ketiga menjaga reputasi pengiriman email keluar tanpa port 25 langsung.
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
								? 'Semua Domain (relay default)'
								: relayTargetDomain}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="global">Semua Domain (relay default)</Select.Item>
								{#each mailStatus?.domains || [] as dom (dom.domain)}
									<Select.Item value={dom.domain}>{dom.domain}</Select.Item>
								{/each}
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="mail-relay-provider">Provider Relay</Field.FieldLabel>
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
							{relayProvider === 'resend'
								? 'Resend (smtp.resend.com:465 - Implicit TLS)'
								: relayProvider === 'brevo'
									? 'Brevo / Sendinblue (smtp-relay.brevo.com:587)'
									: 'Custom SMTP Relay (Server / Port Kustom)'}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="resend">Resend (smtp.resend.com:465 - Implicit TLS)</Select.Item>
								<Select.Item value="brevo">Brevo / Sendinblue (smtp-relay.brevo.com:587)</Select.Item>
								<Select.Item value="custom">Custom SMTP Relay (Server / Port Kustom)</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				{#if relayProvider === 'resend' || relayProvider === 'brevo'}
					<Field.Field>
						<Field.FieldLabel for="mail-relay-secret">
							{relayProvider === 'resend' ? 'API Key Resend (re_...)' : 'Master SMTP Key Brevo (xkeysib-...)'}
						</Field.FieldLabel>
						<Input
							id="mail-relay-secret"
							type="password"
							autocomplete="current-password"
							placeholder={relayProvider === 'resend' ? 're_123456789...' : 'xkeysib-123456789...'}
							bind:value={relaySecret}
						/>
						<Field.FieldDescription>
							{relayProvider === 'resend'
								? 'Port 465 (Implicit TLS). Username otomatis disetel ke "resend".'
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
						<Input id="mail-relay-pass" type="password" bind:value={relayPass} />
					</Field.Field>
				{/if}

				<Field.Field>
					<Field.FieldLabel for="mail-relay-dns">Record DNS Relay (dikelola wdev)</Field.FieldLabel>
					<Textarea
						id="mail-relay-dns"
						rows={4}
						spellcheck={false}
						class="font-mono text-xs"
						placeholder="CNAME brevo1._domainkey.domain.com b1.domain.com.dkim.brevo.com"
						bind:value={relayDNSText}
					/>
					<Field.FieldDescription>
						Satu record per baris: <code>TYPE NAMA NILAI</code>. Otomatis dibersihkan bila provider diganti.
					</Field.FieldDescription>
				</Field.Field>
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
				Mengonfigurasi otomatis Stalwart lokal, SnappyMail, DNS record (A, MX, SPF, DMARC) di VPS Edge, serta SSL Let's Encrypt.
			</Dialog.Description>
		</Dialog.Header>

		<form id="mail-setup-form" onsubmit={submitSetupMail} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
			{#if preflightLoading}
				<p class="text-xs text-muted-foreground">Memeriksa prasyarat mail...</p>
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
							Stalwart, CLI, NetBird, Edge Gateway, dan relay default siap digunakan.
						</Alert.Description>
					</Alert.Root>
				{/if}
			{/if}

			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="mail-setup-domain-select">Domain Publik (PowerDNS Edge)</Field.FieldLabel>
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
						Aktifkan SSL Let's Encrypt Otomatis (webmail &amp; mailadmin)
					</Field.FieldLabel>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="mail-setup-relay-prov">Outbound SMTP Relay</Field.FieldLabel>
					<Select.Root type="single" bind:value={setupRelayProv}>
						<Select.Trigger id="mail-setup-relay-prov" class="w-full">
							{setupRelayProv === ''
								? 'Gunakan relay default yang sudah ada'
								: setupRelayProv === 'resend'
									? 'Resend (smtp.resend.com:465 - Rekomendasi)'
									: setupRelayProv === 'brevo'
										? 'Brevo (smtp-relay.brevo.com:587)'
										: 'Custom SMTP Relay Server'}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="">Gunakan relay default yang sudah ada</Select.Item>
								<Select.Item value="resend">Resend (smtp.resend.com:465 - Rekomendasi)</Select.Item>
								<Select.Item value="brevo">Brevo (smtp-relay.brevo.com:587)</Select.Item>
								<Select.Item value="custom">Custom SMTP Relay Server</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
					<Field.FieldDescription>
						Mode relay-only: email keluar tidak pernah dikirim langsung via port 25.
					</Field.FieldDescription>
				</Field.Field>

				{#if setupRelayProv === 'resend'}
					<Field.Field>
						<Field.FieldLabel for="mail-setup-resend-key">API Key Resend (re_...)</Field.FieldLabel>
						<Input
							id="mail-setup-resend-key"
							type="password"
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
						<Input
							id="mail-setup-brevo-key"
							type="password"
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
						<Input id="mail-setup-custom-pass" type="password" bind:value={setupCustomPass} />
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
