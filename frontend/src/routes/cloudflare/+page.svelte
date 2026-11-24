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
	import { Input } from '#lib/components/ui/input/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PaginationBar from '#lib/components/shared/PaginationBar.svelte';
	import { api, ApiError } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { CFLoginSession, CFProfileInfo, SiteRecord, TunnelStatus } from '#lib/types/api.js';
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

	let cfStatus = $state<TunnelStatus | null>(null);
	let localSites = $state<SiteRecord[]>([]);
	let localDomain = $state('');
	let publicHost = $state('');
	let profile = $state('');
	let serviceUrl = $state('');
	let showAdvancedService = $state(false);
	let submitting = $state(false);
	let exposureDeleting = $state<Record<string, boolean>>({});
	let profilePending = $state<Record<string, boolean>>({});

	// Modal Tambah Profil Multi-Akun
	let addModalOpen = $state(false);
	let addTab = $state<'token' | 'login' | 'cert'>('token');

	// Form Token Zero Trust
	let tokenName = $state('default');
	let tokenValue = $state('');
	let tokenTunnelName = $state('');
	let tokenAutoStart = $state(true);
	let savingToken = $state(false);

	// Form Browser OAuth Login
	let loginName = $state('default');
	let loginTunnelName = $state('');
	let loginSession = $state<CFLoginSession | null>(null);
	let startingLogin = $state(false);
	let loginPollTimer: ReturnType<typeof setInterval> | null = null;

	// Form Cert.pem + Credentials JSON
	let certName = $state('default');
	let certPEM = $state('');
	let selectedLocalCert = $state('');
	let certCredJSON = $state('');
	let certTunnelName = $state('');
	let certAutoStart = $state(true);
	let savingCert = $state(false);

	// Modal Log Profil
	let logsModalOpen = $state(false);
	let logsProfileName = $state('');
	let logsText = $state('');
	let logsLoading = $state(false);

	// Pagination (Max 10 data per page)
	const PAGE_SIZE = 10;
	let exposurePage = $state(1);

	const profileDetails = $derived<CFProfileInfo[]>(cfStatus?.profile_details ?? []);
	const certProfiles = $derived(profileDetails.filter((p) => p.auth_mode !== 'token'));
	const selectedProfileInfo = $derived(profileDetails.find((p) => p.name === profile) ?? null);
	const allExposures = $derived(cfStatus?.exposures || []);
	const paginatedExposures = $derived(
		allExposures.slice((exposurePage - 1) * PAGE_SIZE, exposurePage * PAGE_SIZE)
	);

	$effect(() => {
		if (allExposures.length === 0) {
			exposurePage = 1;
		}
	});

	$effect(() => {
		if (!profile && profileDetails.length > 0) {
			profile = (certProfiles[0] ?? profileDetails[0]).name;
		}
	});

	function stopLoginPoll() {
		if (loginPollTimer) {
			clearInterval(loginPollTimer);
			loginPollTimer = null;
		}
	}

	onDestroy(stopLoginPoll);

	async function loadCloudflare() {
		try {
			const [st, sites] = await Promise.all([
				api.getCFStatus(),
				api.listSites().catch(() => [] as SiteRecord[])
			]);
			cfStatus = st;
			localSites = sites ?? [];
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memuat status Cloudflare Tunnel');
		}
	}

	onMount(loadCloudflare);

	$effect(() => {
		if (cp.refreshTick > 0) void loadCloudflare();
	});

	function openAddProfileModal(tab: 'token' | 'login' | 'cert' = 'token') {
		addTab = tab;
		const suggested = profileDetails.length === 0 ? 'default' : `akun-${profileDetails.length + 1}`;
		tokenName = suggested;
		loginName = suggested;
		certName = suggested;
		selectedLocalCert = cfStatus?.detected_local_certs?.[0] || '';
		addModalOpen = true;
	}

	async function copyText(val: string, label: string) {
		if (!val) return;
		try {
			await navigator.clipboard.writeText(val);
			toast.success(`${label} disalin ke clipboard.`);
		} catch {
			toast.error(`Gagal menyalin ${label}`);
		}
	}

	async function submitTokenProfile(e: SubmitEvent) {
		e.preventDefault();
		if (!tokenName.trim() || !tokenValue.trim()) {
			toast.warning('Nama profil dan tunnel token wajib diisi.');
			return;
		}
		savingToken = true;
		try {
			const res = await api.registerCFTokenProfile({
				name: tokenName.trim(),
				token: tokenValue.trim(),
				tunnel_name: tokenTunnelName.trim() || undefined,
				auto_start: tokenAutoStart
			});
			toast.success(`Profil token '${res.profile.name}' berhasil didaftarkan.`);
			tokenValue = '';
			tokenTunnelName = '';
			addModalOpen = false;
			profile = res.profile.name;
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : 'Gagal mendaftarkan profil token');
		} finally {
			savingToken = false;
		}
	}

	async function startBrowserLoginFlow(e: SubmitEvent) {
		e.preventDefault();
		if (!loginName.trim()) {
			toast.warning('Nama profil wajib diisi.');
			return;
		}
		stopLoginPoll();
		startingLogin = true;
		try {
			const sess = await api.startCFLogin(loginName.trim(), loginTunnelName.trim());
			loginSession = sess;
			if (sess.status === 'waiting_browser' || sess.status === 'starting') {
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
							toast.success(cur.message || `Profil '${cur.profile}' berhasil terdaftar.`);
							profile = cur.profile;
							addModalOpen = false;
							loginSession = null;
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
			toast.error(err instanceof ApiError ? err.message : 'Gagal memulai login browser Cloudflare');
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
		if (!certName.trim()) {
			toast.warning('Nama profil wajib diisi.');
			return;
		}
		if (!certPEM.trim() && !selectedLocalCert && !certCredJSON.trim()) {
			toast.warning('Isi cert.pem, pilih cert lokal yang terdeteksi, atau sertakan credentials JSON.');
			return;
		}
		savingCert = true;
		try {
			const res = await api.registerCFCertProfile({
				name: certName.trim(),
				cert_pem: certPEM.trim() || undefined,
				local_cert_path: !certPEM.trim() && selectedLocalCert ? selectedLocalCert : undefined,
				credentials_json: certCredJSON.trim() || undefined,
				tunnel_name: certTunnelName.trim() || undefined,
				auto_start: certAutoStart
			});
			toast.success(`Profil cert '${res.profile.name}' berhasil didaftarkan.`);
			certPEM = '';
			certCredJSON = '';
			certTunnelName = '';
			addModalOpen = false;
			profile = res.profile.name;
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : 'Gagal mendaftarkan profil cert');
		} finally {
			savingCert = false;
		}
	}

	async function controlProfile(name: string, action: 'start' | 'stop' | 'restart') {
		profilePending = { ...profilePending, [name]: true };
		try {
			await api.controlCFProfile(name, action);
			toast.success(`Profil Cloudflare '${name}' berhasil di-${action}.`);
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : `Gagal ${action} profil ${name}`);
		} finally {
			const next = { ...profilePending };
			delete next[name];
			profilePending = next;
		}
	}

	async function removeProfile(name: string) {
		const ok = await cp.confirm({
			title: 'Hapus Profil Cloudflare',
			description: `Hapus profil '${name}', credentials/token, unit cloudflared@${name}, serta daftar route miliknya?`,
			confirmText: 'Hapus Profil',
			destructive: true
		});
		if (!ok) return;
		profilePending = { ...profilePending, [name]: true };
		try {
			await api.deleteCFProfile(name);
			toast.success(`Profil '${name}' berhasil dihapus.`);
			if (profile === name) profile = '';
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : `Gagal menghapus profil ${name}`);
		} finally {
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
			logsText = res.logs || '(Belum ada log journald untuk unit ini)';
		} catch (err) {
			logsText = err instanceof ApiError ? err.message : 'Gagal membaca log layanan';
		} finally {
			logsLoading = false;
		}
	}

	async function submitExpose(e: SubmitEvent) {
		e.preventDefault();
		if (!localDomain.trim() || !publicHost.trim()) {
			toast.warning('Domain lokal dan domain publik wajib diisi.');
			return;
		}
		submitting = true;
		try {
			const res = await api.exposeCF(
				localDomain.trim(),
				publicHost.trim(),
				profile.trim(),
				serviceUrl.trim()
			);
			toast.success(`Eksposur berhasil: ${localDomain.trim()} -> https://${res.public_host}`, {
				description: `Profil: ${res.profile} · Origin: ${res.service_url}`
			});
			if (res.dns_warning) {
				toast.warning('Catatan DNS Cloudflare', { description: res.dns_warning });
			}
			localDomain = '';
			publicHost = '';
			serviceUrl = '';
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal mengekspos domain Cloudflare');
		} finally {
			submitting = false;
		}
	}

	async function unexpose(expProfile: string, domain: string) {
		const ok = await cp.confirm({
			title: 'Hapus Eksposur Cloudflare',
			description: `Hapus exposure Cloudflare untuk ${domain} (profil ${expProfile})?`,
			confirmText: 'Hapus',
			destructive: true
		});
		if (!ok) return;
		exposureDeleting = { ...exposureDeleting, [domain]: true };
		try {
			await api.unexposeCF(domain, expProfile);
			toast.success(`Eksposur Cloudflare untuk ${domain} berhasil dihapus.`);
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus eksposur');
		} finally {
			const next = { ...exposureDeleting };
			delete next[domain];
			exposureDeleting = next;
		}
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<!-- Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Cloudflare Tunnel &amp; Multi-Profile Manager</h1>
			<p class="text-sm text-muted-foreground">
				Daftarkan beberapa akun/tunnel Cloudflare (via Token Zero Trust, Login Browser, atau <code class="font-mono">cert.pem</code>) dan ekspos vhost lokal ke domain publik.
			</p>
		</div>
		<div class="flex flex-wrap items-center gap-2">
			{#if cfStatus}
				<StatusBadge
					tone={cfStatus.active ? 'success' : 'neutral'}
					label={cfStatus.active ? 'Tunnel Berjalan' : 'Tunnel Nonaktif'}
				/>
			{/if}
			{#if cfStatus && !cfStatus.installed}
				<Button variant="default" size="sm" onclick={() => cp.triggerInstall('cloudflare')}>
					Pasang cloudflared
				</Button>
			{/if}
			<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('cf-doctor')}>
				<ActivityIcon data-icon="inline-start" />
				Diagnostik Doctor
			</Button>
			<Button size="sm" onclick={() => openAddProfileModal('token')}>
				<PlusIcon data-icon="inline-start" />
				Tambah Profil
			</Button>
		</div>
	</div>

	<!-- Kartu Multi-Profil Akun -->
	<Card.Root>
		<Card.Header class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<Card.Title class="text-base">Profil Akun &amp; Tunnel Terdaftar ({profileDetails.length})</Card.Title>
				<Card.Description class="break-all">
					{#if cfStatus?.binary}
						Biner: <span class="font-mono">{cfStatus.binary}</span> · Systemd Template:
						<span class="font-mono">{cfStatus.service_unit_ready ? 'Siap (cloudflared@.service)' : 'Belum terpasang'}</span>
					{:else}
						Belum ada biner cloudflared terdeteksi di PATH server.
					{/if}
				</Card.Description>
			</div>
			{#if profileDetails.length > 0}
				<div class="flex flex-wrap gap-1.5">
					<Button variant="outline" size="xs" onclick={() => openAddProfileModal('token')}>
						<KeyRoundIcon data-icon="inline-start" />
						+ Token
					</Button>
					<Button variant="outline" size="xs" onclick={() => openAddProfileModal('login')}>
						<GlobeIcon data-icon="inline-start" />
						+ Login Browser
					</Button>
				</div>
			{/if}
		</Card.Header>
		<Card.Content>
			{#if profileDetails.length === 0}
				<Alert.Root>
					<InfoIcon />
					<Alert.Title>Belum Ada Profil Akun Cloudflare Terdaftar</Alert.Title>
					<Alert.Description class="space-y-3 text-xs leading-relaxed">
						<p>
							Anda dapat mendaftarkan banyak akun atau tunnel Cloudflare secara terpisah. Pilih metode paling cepat: paste <strong>Tunnel Token</strong> dari Cloudflare Zero Trust Dashboard, atau lakukan <strong>Login Browser OAuth</strong> per akun.
						</p>
						<div class="flex flex-wrap gap-2">
							<Button size="sm" onclick={() => openAddProfileModal('token')}>
								<KeyRoundIcon data-icon="inline-start" />
								Daftar via Tunnel Token
							</Button>
							<Button variant="outline" size="sm" onclick={() => openAddProfileModal('login')}>
								<GlobeIcon data-icon="inline-start" />
								Login Browser Cloudflare
							</Button>
							<Button variant="outline" size="sm" onclick={() => openAddProfileModal('cert')}>
								<ShieldCheckIcon data-icon="inline-start" />
								Upload / Paste cert.pem
							</Button>
						</div>
					</Alert.Description>
				</Alert.Root>
			{:else}
				<ResponsiveTableWrapper label="Daftar Profil Multi-Tunnel Cloudflare" class="rounded-md border">
					<Table.Root>
						<Table.Header>
							<Table.Row>
								<Table.Head>Profil</Table.Head>
								<Table.Head>Mode Autentikasi</Table.Head>
								<Table.Head>Tunnel ID / Nama</Table.Head>
								<Table.Head>Status Service</Table.Head>
								<Table.Head>Rute</Table.Head>
								<Table.Head class="text-right">Kontrol</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each profileDetails as prof (prof.name)}
								<Table.Row>
									<Table.Cell data-label="Profil" class="font-semibold">
										<div class="font-mono text-sm">{prof.name}</div>
										<div class="font-mono text-[11px] text-muted-foreground">{prof.service_unit}</div>
									</Table.Cell>
									<Table.Cell data-label="Mode">
										{#if prof.auth_mode === 'token'}
											<Badge variant="outline" class="border-info/40 bg-info/10 text-info">
												Token Zero Trust
											</Badge>
										{:else}
											<Badge variant="outline" class="border-primary/40 bg-primary/10 text-primary">
												Cert &amp; Config Lokal
											</Badge>
										{/if}
									</Table.Cell>
									<Table.Cell data-label="Tunnel ID">
										<div class="font-medium text-xs">{prof.tunnel_name || '-'}</div>
										{#if prof.tunnel_id}
											<button
												type="button"
												class="mt-0.5 inline-flex items-center gap-1 font-mono text-[11px] text-muted-foreground hover:text-foreground"
												onclick={() => copyText(prof.tunnel_id || '', 'Tunnel ID')}
												title="Salin Tunnel UUID"
											>
												<span>{prof.tunnel_id}</span>
												<CopyIcon class="size-3" />
											</button>
										{/if}
									</Table.Cell>
									<Table.Cell data-label="Status">
										<StatusBadge
											tone={prof.active ? 'success' : 'neutral'}
											label={prof.active ? 'Aktif (Running)' : 'Nonaktif'}
										/>
									</Table.Cell>
									<Table.Cell data-label="Rute" class="font-mono text-xs tabular-nums">
										{#if prof.auth_mode === 'token'}
											<span class="text-muted-foreground" title="Dikelola di Cloudflare Zero Trust Dashboard">Dashboard CF</span>
										{:else}
											{prof.route_count} rute
										{/if}
									</Table.Cell>
									<Table.Cell data-label="Kontrol" class="text-right">
										<div class="flex justify-end gap-1">
											{#if prof.active}
												<Button
													variant="outline"
													size="xs"
													disabled={Boolean(profilePending[prof.name])}
													onclick={() => controlProfile(prof.name, 'restart')}
													title="Restart service tunnel"
												>
													<RotateCwIcon class="size-3.5" />
												</Button>
												<Button
													variant="outline"
													size="xs"
													disabled={Boolean(profilePending[prof.name])}
													onclick={() => controlProfile(prof.name, 'stop')}
													title="Hentikan service tunnel"
												>
													<SquareIcon class="size-3.5" />
												</Button>
											{:else}
												<Button
													variant="outline"
													size="xs"
													disabled={Boolean(profilePending[prof.name])}
													onclick={() => controlProfile(prof.name, 'start')}
													title="Jalankan service tunnel"
												>
													<PlayIcon class="size-3.5 text-success" />
												</Button>
											{/if}
											<Button
												variant="outline"
												size="xs"
												onclick={() => openProfileLogs(prof.name)}
												title="Lihat log cloudflared"
											>
												<FileTextIcon class="size-3.5" />
											</Button>
											<Button
												variant="outline"
												size="xs"
												disabled={Boolean(profilePending[prof.name])}
												onclick={() => removeProfile(prof.name)}
												title="Hapus profil"
											>
												<Trash2Icon class="size-3.5 text-destructive" />
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

	<!-- Form Ekspos Vhost Lokal (Khusus Profil Mode Cert / Config Lokal) -->
	<Card.Root>
		<Card.Header>
			<Card.Title class="text-base">Ekspos Site Lokal ke Cloudflare (wdev cf expose)</Card.Title>
			<Card.Description>
				Petakan domain lokal (mis. <code class="font-mono">myapp.test</code>) ke hostname publik Cloudflare Anda dengan <code class="font-mono">httpHostHeader</code> otomatis untuk Nginx.
			</Card.Description>
		</Card.Header>
		<Card.Content class="space-y-4">
			{#if selectedProfileInfo?.auth_mode === 'token'}
				<Alert.Root class="border-amber-500/35 bg-amber-500/5">
					<InfoIcon class="text-amber-600 dark:text-amber-400" />
					<Alert.Title class="text-amber-700 dark:text-amber-400">
						Profil "{selectedProfileInfo.name}" Menggunakan Mode Token (Remotely-Managed)
					</Alert.Title>
					<Alert.Description class="text-xs leading-relaxed text-muted-foreground">
						Untuk tunnel berbasis Token, Cloudflare mengatur Public Hostname dari <strong>Cloudflare Zero Trust Dashboard → Networks → Tunnels</strong> (arahkan Service ke <code class="font-mono">http://127.0.0.1:80</code> atau <code class="font-mono">https://127.0.0.1:443</code> + setel <em>HTTP Host Header</em> sesuai domain lokal Anda). Jika ingin <code class="font-mono">wdev</code> mengelola rute &amp; DNS otomatis dari sini, daftarkan profil mode <strong>Login Browser / Cert.pem</strong>.
					</Alert.Description>
				</Alert.Root>
			{/if}

			<form onsubmit={submitExpose} class="space-y-3">
				<Field.FieldGroup class="grid gap-4 sm:grid-cols-2 lg:grid-cols-[180px_1fr_1fr_auto] lg:items-end">
					<Field.Field>
						<Field.FieldLabel for="cf-profile-select">Profil Tunnel</Field.FieldLabel>
						{#if profileDetails.length > 0}
							<Select.Root type="single" bind:value={profile}>
								<Select.Trigger id="cf-profile-select" class="w-full">
									<span class="truncate">
										{profile || 'Pilih profil'}
									</span>
								</Select.Trigger>
								<Select.Content>
									{#each profileDetails as p (p.name)}
										<Select.Item value={p.name}>
											{p.name} ({p.auth_mode === 'token' ? 'Token' : 'Cert'})
										</Select.Item>
									{/each}
								</Select.Content>
							</Select.Root>
						{:else}
							<Input id="cf-profile-select" placeholder="default" bind:value={profile} />
						{/if}
					</Field.Field>

					<Field.Field>
						<Field.FieldLabel for="cf-local-domain">Domain Lokal</Field.FieldLabel>
						<Input
							id="cf-local-domain"
							list="wdev-local-sites"
							placeholder="myapp.test"
							bind:value={localDomain}
							required
						/>
						<datalist id="wdev-local-sites">
							{#each localSites as st (st.domain)}
								<option value={st.domain}></option>
							{/each}
						</datalist>
					</Field.Field>

					<Field.Field>
						<Field.FieldLabel for="cf-public-host">Domain Publik Cloudflare</Field.FieldLabel>
						<Input id="cf-public-host" placeholder="myapp.example.com" bind:value={publicHost} required />
					</Field.Field>

					<Button
						type="submit"
						disabled={submitting || selectedProfileInfo?.auth_mode === 'token'}
						class="w-full sm:col-span-2 lg:col-span-1 lg:w-auto"
					>
						{#if submitting}
							<Spinner data-icon="inline-start" />
							Memproses...
						{:else}
							<CloudUploadIcon data-icon="inline-start" />
							Ekspos Sekarang
						{/if}
					</Button>
				</Field.FieldGroup>

				<div class="flex flex-wrap items-center justify-between gap-2">
					<button
						type="button"
						class="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
						onclick={() => (showAdvancedService = !showAdvancedService)}
					>
						{showAdvancedService ? 'Sembunyikan target service kustom' : 'Atur target service / port lokal kustom (opsional)'}
					</button>
				</div>

				{#if showAdvancedService}
					<Field.Field class="max-w-md">
						<Field.FieldLabel for="cf-service-url">Target Service URL (Opsional)</Field.FieldLabel>
						<Input
							id="cf-service-url"
							placeholder="http://127.0.0.1:80 (kosongkan untuk auto Nginx HTTP/HTTPS)"
							bind:value={serviceUrl}
						/>
						<Field.FieldDescription class="text-xs">
							Kosongkan agar otomatis mengikuti Nginx vhost (HTTP :80 atau HTTPS :443 dengan <code class="font-mono">noTLSVerify</code>).
						</Field.FieldDescription>
					</Field.Field>
				{/if}
			</form>
		</Card.Content>
	</Card.Root>

	<!-- Tabel Eksposur Aktif -->
	<section class="flex flex-col gap-3">
		<h2 class="text-lg font-semibold tracking-tight">Daftar Domain Publik Aktif</h2>
		<div class="flex flex-col sm:overflow-hidden sm:rounded-lg sm:border sm:bg-card">
			<ResponsiveTableWrapper label="Tabel Domain Publik Cloudflare" class="rounded-none border-0 bg-transparent">
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>Domain Lokal</Table.Head>
							<Table.Head>Domain Publik</Table.Head>
							<Table.Head>Profil</Table.Head>
							<Table.Head>Target Service &amp; Origin</Table.Head>
							<Table.Head class="text-right">Aksi</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#if allExposures.length === 0}
							<Table.Row>
								<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
									Belum ada exposure Cloudflare Tunnel lokal yang tercatat.
								</Table.Cell>
							</Table.Row>
						{:else}
							{#each paginatedExposures as exp (exp.local_domain)}
								<Table.Row>
									<Table.Cell data-label="Domain Lokal" class="font-mono text-xs">{exp.local_domain}</Table.Cell>
									<Table.Cell data-label="Domain Publik">
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
									<Table.Cell data-label="Profil">
										<Badge variant="outline">{exp.profile}</Badge>
									</Table.Cell>
									<Table.Cell data-label="Target Service" class="text-xs text-muted-foreground">
										<div class="font-mono">{exp.service_url || 'auto (Nginx :80/:443)'}</div>
										{#if exp.http_host_header}
											<div class="mt-0.5 text-[11px]">
												Host: <span class="font-mono">{exp.http_host_header}</span>
												{#if exp.no_tls_verify}
													· <span class="font-mono">noTLSVerify</span>
												{/if}
											</div>
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
															disabled={Boolean(exposureDeleting[exp.local_domain])}
															{...props}
														>
															{#if exposureDeleting[exp.local_domain]}
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
															{exp.local_domain}
														</DropdownMenu.Label>
														<DropdownMenu.Separator />
														<DropdownMenu.Item>
															{#snippet child({ props })}
																<a href={`https://${exp.public_host}`} target="_blank" rel="noopener noreferrer" {...props}>
																	<ExternalLinkIcon />
																	<span>Buka URL Publik</span>
																</a>
															{/snippet}
														</DropdownMenu.Item>
														<DropdownMenu.Separator />
														<DropdownMenu.Item
															variant="destructive"
															disabled={Boolean(exposureDeleting[exp.local_domain])}
															onclick={() => unexpose(exp.profile, exp.local_domain)}
														>
															<Trash2Icon />
															<span>Hapus Exposure</span>
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
				itemLabel="domain publik"
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
			<Dialog.Title>Daftarkan Profil Cloudflare Tunnel</Dialog.Title>
			<Dialog.Description>
				Setiap profil terisolasi dengan kredensial, file konfigurasi, dan service systemd <code class="font-mono">cloudflared@&lt;profil&gt;</code> tersendiri.
			</Dialog.Description>
		</Dialog.Header>

		<Tabs.Root bind:value={addTab} class="mt-2 space-y-4">
			<Tabs.List class="grid w-full grid-cols-3">
				<Tabs.Trigger value="token">Tunnel Token</Tabs.Trigger>
				<Tabs.Trigger value="login">Login Browser</Tabs.Trigger>
				<Tabs.Trigger value="cert">Paste cert.pem</Tabs.Trigger>
			</Tabs.List>

			<!-- Tab 1: Tunnel Token -->
			<Tabs.Content value="token">
				<form onsubmit={submitTokenProfile} class="space-y-4">
					<Field.FieldGroup>
						<Field.Field>
							<Field.FieldLabel for="cf-tok-name">Nama Profil</Field.FieldLabel>
							<Input
								id="cf-tok-name"
								placeholder="contoh: default, client-a, kantor"
								bind:value={tokenName}
								required
							/>
							<Field.FieldDescription class="text-xs">
								Gunakan huruf, angka, <code class="font-mono">-</code> atau <code class="font-mono">_</code>.
							</Field.FieldDescription>
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-tok-val">Tunnel Token Cloudflare Zero Trust</Field.FieldLabel>
							<Textarea
								id="cf-tok-val"
								rows={3}
								class="font-mono text-xs"
								placeholder="eyJhIjoi... atau paste langsung perintah 'cloudflared service install eyJ...'"
								bind:value={tokenValue}
								required
							/>
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-tok-tunnel">Label Nama Tunnel (Opsional)</Field.FieldLabel>
							<Input id="cf-tok-tunnel" placeholder="otomatis jika kosong" bind:value={tokenTunnelName} />
						</Field.Field>

						<div class="flex items-center gap-2">
							<Checkbox id="cf-tok-start" bind:checked={tokenAutoStart} />
							<label for="cf-tok-start" class="text-xs font-medium">
								Aktifkan &amp; jalankan service systemd otomatis setelah disimpan
							</label>
						</div>
					</Field.FieldGroup>

					<Dialog.Footer>
						<Button type="button" variant="outline" onclick={() => (addModalOpen = false)}>Batal</Button>
						<Button type="submit" disabled={savingToken}>
							{#if savingToken}
								<Spinner data-icon="inline-start" />
								Menyimpan...
							{:else}
								Simpan &amp; Aktifkan
							{/if}
						</Button>
					</Dialog.Footer>
				</form>
			</Tabs.Content>

			<!-- Tab 2: Browser OAuth Login -->
			<Tabs.Content value="login" class="space-y-4">
				{#if !loginSession}
					<form onsubmit={startBrowserLoginFlow} class="space-y-4">
						<Field.FieldGroup>
							<Field.Field>
								<Field.FieldLabel for="cf-login-name">Nama Profil</Field.FieldLabel>
								<Input id="cf-login-name" placeholder="contoh: akun-1, proyek-b" bind:value={loginName} required />
							</Field.Field>
							<Field.Field>
								<Field.FieldLabel for="cf-login-tunnel">Nama Tunnel Baru di Akun (Opsional)</Field.FieldLabel>
								<Input id="cf-login-tunnel" placeholder="otomatis: hostname-profil" bind:value={loginTunnelName} />
								<Field.FieldDescription class="text-xs">
									Setelah otorisasi browser selesai, <code class="font-mono">wdev</code> akan menyimpan <code class="font-mono">cert.pem</code> terisolasi dan otomatis menjalankan <code class="font-mono">cloudflared tunnel create</code>.
								</Field.FieldDescription>
							</Field.Field>
						</Field.FieldGroup>

						<Dialog.Footer>
							<Button type="button" variant="outline" onclick={() => (addModalOpen = false)}>Batal</Button>
							<Button type="submit" disabled={startingLogin}>
								{#if startingLogin}
									<Spinner data-icon="inline-start" />
									Menyiapkan URL...
								{:else}
									<GlobeIcon data-icon="inline-start" />
									Mulai Login Cloudflare
								{/if}
							</Button>
						</Dialog.Footer>
					</form>
				{:else}
					<div class="space-y-4">
						<Alert.Root>
							<InfoIcon />
							<Alert.Title>Menunggu Otorisasi Cloudflare ({loginSession.profile})</Alert.Title>
							<Alert.Description class="space-y-2 text-xs">
								<p>{loginSession.message}</p>
								{#if loginSession.auth_url}
									<div class="flex flex-wrap items-center gap-2 pt-1">
										<a
											href={loginSession.auth_url}
											target="_blank"
											rel="noopener noreferrer"
											class="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:opacity-90"
										>
											<ExternalLinkIcon class="size-3.5" />
											Buka Otorisasi Cloudflare
										</a>
										<Button
											type="button"
											variant="outline"
											size="xs"
											onclick={() => copyText(loginSession?.auth_url || '', 'URL Otorisasi')}
										>
											<CopyIcon data-icon="inline-start" />
											Salin URL
										</Button>
									</div>
								{/if}
							</Alert.Description>
						</Alert.Root>

						{#if loginSession.status === 'failed' || loginSession.status === 'cancelled'}
							<Alert.Root variant="destructive">
								<AlertCircleIcon />
								<Alert.Title>Proses Login Terhenti</Alert.Title>
								<Alert.Description class="font-mono text-xs break-all">
									{loginSession.error || loginSession.message}
								</Alert.Description>
							</Alert.Root>
						{:else}
							<div class="flex items-center gap-2 text-xs text-muted-foreground">
								<Spinner class="size-4" />
								<span>Memantau penyelesaian otorisasi dari Cloudflare...</span>
							</div>
						{/if}

						<Dialog.Footer>
							<Button type="button" variant="outline" onclick={cancelLoginFlow}>
								Tutup / Batalkan
							</Button>
						</Dialog.Footer>
					</div>
				{/if}
			</Tabs.Content>

			<!-- Tab 3: Paste / Import cert.pem -->
			<Tabs.Content value="cert">
				<form onsubmit={submitCertProfile} class="space-y-4">
					<Field.FieldGroup>
						<Field.Field>
							<Field.FieldLabel for="cf-cert-name">Nama Profil</Field.FieldLabel>
							<Input id="cf-cert-name" placeholder="contoh: default, client-b" bind:value={certName} required />
						</Field.Field>

						{#if (cfStatus?.detected_local_certs?.length ?? 0) > 0}
							<Field.Field>
								<Field.FieldLabel for="cf-local-cert-pick">Import cert.pem Lokal Terdeteksi (Opsional)</Field.FieldLabel>
								<Select.Root type="single" bind:value={selectedLocalCert}>
									<Select.Trigger id="cf-local-cert-pick" class="w-full font-mono text-xs">
										<span>{selectedLocalCert || '-- Pilih file cert.pem lokal --'}</span>
									</Select.Trigger>
									<Select.Content>
										{#each cfStatus?.detected_local_certs ?? [] as certPath (certPath)}
											<Select.Item value={certPath} class="font-mono text-xs">{certPath}</Select.Item>
										{/each}
									</Select.Content>
								</Select.Root>
							</Field.Field>
						{/if}

						<Field.Field>
							<Field.FieldLabel for="cf-cert-pem">Isi File cert.pem (Kosongkan jika import lokal)</Field.FieldLabel>
							<Textarea
								id="cf-cert-pem"
								rows={4}
								class="font-mono text-xs"
								placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----BEGIN ARGO TUNNEL TOKEN-----"
								bind:value={certPEM}
							/>
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-cert-tun">Nama Tunnel (Untuk auto-create jika Credentials JSON kosong)</Field.FieldLabel>
							<Input id="cf-cert-tun" placeholder="otomatis: hostname-profil" bind:value={certTunnelName} />
						</Field.Field>

						<Field.Field>
							<Field.FieldLabel for="cf-cert-cred">Credentials JSON (&lt;uuid&gt;.json — Opsional)</Field.FieldLabel>
							<Textarea
								id="cf-cert-cred"
								rows={3}
								class="font-mono text-xs"
								placeholder={'{"AccountTag":"...","TunnelSecret":"...","TunnelID":"..."}'}
								bind:value={certCredJSON}
							/>
						</Field.Field>

						<div class="flex items-center gap-2">
							<Checkbox id="cf-cert-start" bind:checked={certAutoStart} />
							<label for="cf-cert-start" class="text-xs font-medium">
								Aktifkan &amp; jalankan service systemd otomatis setelah disimpan
							</label>
						</div>
					</Field.FieldGroup>

					<Dialog.Footer>
						<Button type="button" variant="outline" onclick={() => (addModalOpen = false)}>Batal</Button>
						<Button type="submit" disabled={savingCert}>
							{#if savingCert}
								<Spinner data-icon="inline-start" />
								Mendaftarkan...
							{:else}
								Simpan Profil Cert
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
			<Dialog.Title>Log Layanan cloudflared@{logsProfileName}</Dialog.Title>
			<Dialog.Description>Catatan eksekusi terakhir dari systemd journald.</Dialog.Description>
		</Dialog.Header>
		<div class="max-h-[55vh] overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap break-all">
			{#if logsLoading}
				<div class="flex items-center gap-2 text-muted-foreground">
					<Spinner class="size-4" />
					<span>Memuat log...</span>
				</div>
			{:else}
				{logsText}
			{/if}
		</div>
		<Dialog.Footer>
			<Button type="button" variant="outline" size="sm" onclick={refreshProfileLogs} disabled={logsLoading}>
				<RotateCwIcon data-icon="inline-start" />
				Muat Ulang
			</Button>
			<Button type="button" size="sm" onclick={() => (logsModalOpen = false)}>Tutup</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
