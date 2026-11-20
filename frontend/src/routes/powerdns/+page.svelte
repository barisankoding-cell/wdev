<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
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

	let edge = $state<EdgeConfig | null>(null);
	let zones = $state<ZoneInfo[]>([]);
	let sites = $state<SiteRecord[]>([]);
	let exposures = $state<ProxyExposure[]>([]);
	let zonesError = $state('');
	let exposuresError = $state('');

	// Add Zone Dialog
	let addZoneOpen = $state(false);
	let newZoneName = $state('');
	let addingZone = $state(false);

	// Doctor / Repair Report Dialog
	let reportOpen = $state(false);
	let reportTitle = $state('');
	let reportData = $state<ZoneDoctorReport | null>(null);

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

	const selectedSiteObj = $derived(sites.find((s) => s.domain === exposeSiteDomain) ?? null);
	const defaultZoneName = $derived(
		zones.length > 0 ? zones[0].name.replace(/\.$/, '') : 'example.com'
	);

	async function loadAll() {
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

		await Promise.all([loadZones(), loadExposures(), loadLocalSites()]);
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

	async function loadExposures() {
		exposuresError = '';
		try {
			exposures = (await api.listPDNSExposures()) ?? [];
		} catch (err) {
			exposures = [];
			exposuresError = err instanceof Error ? err.message : 'Edge VPS tidak dapat dihubungi';
		}
	}

	async function loadLocalSites() {
		try {
			sites = (await api.listSites()) ?? [];
		} catch {
			sites = [];
		}
	}

	onMount(async () => {
		await loadAll();
		const preExpose = page.url.searchParams.get('expose');
		if (preExpose) {
			openExposeModal('site', preExpose);
		}
	});

	$effect(() => {
		if (cp.refreshTick > 0) loadAll();
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

		const ok = await cp.confirm({
			title: 'Konfirmasi Ekspos VPS',
			description: `Ekspos '${pubDomain}' ke internet via VPS Edge Gateway?`,
			confirmText: 'Ekspos Sekarang'
		});
		if (!ok) return;

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
			exposeOpen = false;
			toast.success(`Domain '${pubDomain}' berhasil diekspos ke internet!`);
			await loadAll();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal ekspos ke VPS');
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
		try {
			await api.unexposePDNS(domain, false);
			toast.success(`Ekspos untuk '${domain}' berhasil dihapus.`);
			await loadAll();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus ekspos');
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
			await api.addPDNSZone(clean);
			addZoneOpen = false;
			newZoneName = '';
			toast.success(`Zone DNS ${clean} berhasil ditambahkan!`);
			await loadZones();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menambahkan zone');
		} finally {
			addingZone = false;
		}
	}

	async function runZoneDoctor(zoneName: string) {
		try {
			const res = await api.doctorPDNSZone(zoneName);
			reportTitle = `Doctor Zone — ${zoneName}`;
			reportData = res;
			reportOpen = true;
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menjalankan doctor zone');
		}
	}

	async function runZoneRepair(zoneName: string) {
		const ok = await cp.confirm({
			title: 'Repair DNS Zone',
			description: `Perbaiki zone ${zoneName}? Tindakan ini menulis ulang SOA/NS, glue ns1/ns2, dan mengaktifkan SOA-EDIT-API. Record lain tidak diubah.`,
			confirmText: 'Repair Zone'
		});
		if (!ok) return;
		try {
			const res = await api.repairPDNSZone(zoneName);
			reportTitle = `Repair Zone — ${zoneName}`;
			reportData = res.report ?? res;
			reportOpen = true;
			await loadZones();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memperbaiki zone');
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
		try {
			await api.deletePDNSZone(zoneName);
			toast.success(`Zone DNS '${zoneName}' berhasil dihapus.`);
			await loadZones();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus zone');
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
		const ok = await cp.confirm({
			title: 'Hapus Record DNS',
			description: `Hapus record DNS ${type} untuk '${name}'?`,
			confirmText: 'Hapus',
			destructive: true
		});
		if (!ok) return;
		try {
			await api.deletePDNSRecord(currentZone, name, type);
			toast.success(`Record ${type} untuk ${name} dihapus.`);
			await loadRecords(currentZone);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus record');
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
		const ok = await cp.confirm({
			title: 'Konfirmasi Deploy VPS',
			description: `Mulai deployment otomatis ke VPS ${user}@${deployHost.trim()}:${port}? Skrip instalasi PowerDNS, Nginx, dan Edge Gateway akan dijalankan di VPS via SSH.`,
			confirmText: 'Mulai Deploy'
		});
		if (!ok) return;

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
			deployOpen = false;
			cp.openTerminal('Deploy VPS Edge Gateway');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memulai deploy VPS');
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
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">PowerDNS &amp; VPS Edge Gateway</h1>
			<p class="text-sm text-muted-foreground">
				Authoritative DNS mandiri &amp; reverse-proxy SSL Let's Encrypt otomatis di VPS Edge via NetBird.
			</p>
		</div>
		<div class="flex flex-wrap items-center gap-2">
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
		</div>
	</div>

	{#if edge && !edge.reachable}
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

	<div class="grid gap-3 sm:grid-cols-3">
		<Card.Root>
			<Card.Header>
				<Card.Description>IP VPS (NetBird Mesh)</Card.Description>
				<Card.Title class="truncate font-mono text-base">{edge?.vps_netbird_ip || '-'}</Card.Title>
			</Card.Header>
		</Card.Root>
		<Card.Root>
			<Card.Header>
				<Card.Description>IP Publik Edge Gateway</Card.Description>
				<Card.Title class="truncate font-mono text-base">{edge?.vps_public_ip || '-'}</Card.Title>
			</Card.Header>
		</Card.Root>
		<Card.Root>
			<Card.Header>
				<Card.Description>IP NetBird Mesin Lokal (wt0)</Card.Description>
				<Card.Title class="truncate font-mono text-base">{edge?.local_netbird_ip || '-'}</Card.Title>
			</Card.Header>
		</Card.Root>
	</div>

	<!-- Authoritative DNS Zones -->
	<section class="flex flex-col gap-3">
		<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
			<h2 class="text-base font-semibold tracking-tight sm:text-lg">Zone Authoritative DNS</h2>
			<Button size="sm" class="w-full sm:w-auto" onclick={() => (addZoneOpen = true)}>
				<PlusIcon data-icon="inline-start" />
				Tambah Zone
			</Button>
		</div>

		<ResponsiveTableWrapper label="Tabel Zone Authoritative DNS">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Nama Zone</Table.Head>
						<Table.Head>Tipe</Table.Head>
						<Table.Head>Serial</Table.Head>
						<Table.Head>Status</Table.Head>
						<Table.Head class="text-right">Aksi</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#if zonesError}
						<Table.Row>
							<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
								{zonesError}
							</Table.Cell>
						</Table.Row>
					{:else if zones.length === 0}
						<Table.Row>
							<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
								Belum ada zone DNS.
							</Table.Cell>
						</Table.Row>
					{:else}
						{#each zones as z (z.name)}
							<Table.Row>
								<Table.Cell data-label="Zone" class="font-semibold">{z.name}</Table.Cell>
								<Table.Cell data-label="Tipe">{z.kind}</Table.Cell>
								<Table.Cell data-label="Serial" class="font-mono">{z.serial}</Table.Cell>
								<Table.Cell data-label="Status">
									<StatusBadge tone="success" label="Aktif" />
								</Table.Cell>
								<Table.Cell data-label="Aksi">
									<div class="flex flex-wrap justify-end gap-1.5">
										<Button variant="outline" size="xs" onclick={() => openRecordsModal(z.name)}>
											<ListIcon data-icon="inline-start" />
											Record
										</Button>
										<Button variant="outline" size="xs" onclick={() => runZoneDoctor(z.name)}>
											<StethoscopeIcon data-icon="inline-start" />
											Doctor
										</Button>
										<Button variant="outline" size="xs" onclick={() => runZoneRepair(z.name)}>
											<WrenchIcon data-icon="inline-start" />
											Repair
										</Button>
										<Button variant="destructive" size="xs" onclick={() => deleteZone(z.name)}>
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

	<!-- Local Virtual Hosts Exposure Status -->
	<section class="flex flex-col gap-3">
		<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<h2 class="text-base font-semibold tracking-tight sm:text-lg">Status Ekspos Virtual Host Lokal</h2>
				<p class="text-xs text-muted-foreground">
					Pemetaan virtual host lokal ke domain publik VPS Edge Gateway.
				</p>
			</div>
			<Button variant="outline" size="sm" class="w-full sm:w-auto" onclick={loadAll}>
				<RefreshCwIcon data-icon="inline-start" />
				Perbarui Status
			</Button>
		</div>

		<ResponsiveTableWrapper label="Tabel Status Ekspos Virtual Host Lokal">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Virtual Host Lokal</Table.Head>
						<Table.Head>Framework</Table.Head>
						<Table.Head>Status Ekspos</Table.Head>
						<Table.Head>Domain Publik di VPS</Table.Head>
						<Table.Head>Target NetBird</Table.Head>
						<Table.Head class="text-right">Aksi Cepat</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#if sites.length === 0}
						<Table.Row>
							<Table.Cell colspan={6} class="py-6 text-center text-muted-foreground">
								Belum ada virtual host lokal. Buat virtual host di menu Web Stack terlebih dahulu.
							</Table.Cell>
						</Table.Row>
					{:else}
						{#each sites as s (s.domain)}
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
									<span class="ml-1 text-xs text-muted-foreground">PHP {s.php_version || '8.4'}</span>
								</Table.Cell>
								<Table.Cell data-label="Status">
									{#if s.exposed_to}
										<StatusBadge tone="success" label="Terhubung ke Internet" />
									{:else}
										<StatusBadge tone="neutral" label="Hanya Lokal" />
									{/if}
								</Table.Cell>
								<Table.Cell data-label="Domain Publik">
									{#if s.exposed_to}
										<a
											href={`https://${s.exposed_to}`}
											target="_blank"
											rel="noopener noreferrer"
											class="inline-flex items-center gap-1 break-all font-medium text-primary hover:underline"
										>
											https://{s.exposed_to}
											<ExternalLinkIcon class="size-3.5 shrink-0" />
										</a>
									{:else}
										<span class="text-xs italic text-muted-foreground">Belum diekspos</span>
									{/if}
								</Table.Cell>
								<Table.Cell data-label="Target NetBird" class="font-mono text-xs">{targetNB}</Table.Cell>
								<Table.Cell data-label="Aksi">
									<div class="flex flex-wrap justify-end gap-1.5">
										{#if s.exposed_to}
											<Button
												variant="outline"
												size="xs"
												href={`https://${s.exposed_to}`}
												target="_blank"
												rel="noopener noreferrer"
											>
												<ExternalLinkIcon data-icon="inline-start" />
												Buka
											</Button>
											<Button
												variant="destructive"
												size="xs"
												onclick={() => unexposeDomain(s.exposed_to)}
											>
												Putus Ekspos
											</Button>
										{:else}
											<Button size="xs" onclick={() => openExposeModal('site', s.domain)}>
												<CloudUploadIcon data-icon="inline-start" />
												Ekspos ke VPS
											</Button>
										{/if}
									</div>
								</Table.Cell>
							</Table.Row>
						{/each}
					{/if}
				</Table.Body>
			</Table.Root>
		</ResponsiveTableWrapper>
	</section>

	<!-- Active VPS Proxy Domains -->
	<section class="flex flex-col gap-3">
		<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<h2 class="text-base font-semibold tracking-tight sm:text-lg">Daftar Proxy Domain yang Diekspos di VPS</h2>
				<p class="text-xs text-muted-foreground">
					Seluruh reverse-proxy aktif di VPS Edge Gateway.
				</p>
			</div>
			<div class="grid grid-cols-2 gap-2 sm:flex sm:items-center">
				<Button variant="outline" size="sm" onclick={loadExposures}>
					<RefreshCwIcon data-icon="inline-start" />
					Segarkan
				</Button>
				<Button size="sm" onclick={() => openExposeModal('site')}>
					<PlusIcon data-icon="inline-start" />
					Tambah Ekspos
				</Button>
			</div>
		</div>

		<ResponsiveTableWrapper label="Tabel Proxy Domain VPS">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Domain Publik VPS</Table.Head>
						<Table.Head>Tipe &amp; Rute Target</Table.Head>
						<Table.Head>Status Proxy</Table.Head>
						<Table.Head>File Konfigurasi</Table.Head>
						<Table.Head class="text-right">Aksi</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#if exposuresError}
						<Table.Row>
							<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
								{exposuresError}
							</Table.Cell>
						</Table.Row>
					{:else if exposures.length === 0}
						<Table.Row>
							<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
								Belum ada proxy atau aplikasi yang diekspos di VPS Edge.
							</Table.Cell>
						</Table.Row>
					{:else}
						{#each exposures as exp (exp.domain)}
							<Table.Row>
								<Table.Cell data-label="Domain">
									<a
										href={`https://${exp.domain}`}
										target="_blank"
										rel="noopener noreferrer"
										class="inline-flex items-center gap-1 break-all font-semibold text-primary hover:underline"
									>
										{exp.domain}
										<ExternalLinkIcon class="size-3.5 shrink-0" />
									</a>
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
								<Table.Cell data-label="Konfigurasi" class="sm:max-w-48">
									<code class="block break-all text-xs text-muted-foreground sm:truncate">{exp.config_file || '-'}</code>
								</Table.Cell>
								<Table.Cell data-label="Aksi" class="text-right">
									<Button variant="destructive" size="xs" onclick={() => unexposeDomain(exp.domain)}>
										<Trash2Icon data-icon="inline-start" />
										Hapus Ekspos
									</Button>
								</Table.Cell>
							</Table.Row>
						{/each}
					{/if}
				</Table.Body>
			</Table.Root>
		</ResponsiveTableWrapper>
	</section>
</div>

<!-- Add Zone Dialog -->
<Dialog.Root bind:open={addZoneOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Tambah Authoritative DNS Zone</Dialog.Title>
			<Dialog.Description>Masukkan nama domain FQDN tanpa protokol (contoh: domainanda.com).</Dialog.Description>
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
		<Dialog.Header>
			<Dialog.Title>{reportTitle}</Dialog.Title>
			{#if reportData?.nameservers && reportData.nameservers.length > 0}
				<Dialog.Description>
					Nameservers: {reportData.nameservers.join(', ')}
				</Dialog.Description>
			{/if}
		</Dialog.Header>
		{#if reportData}
			<div class="flex max-h-[55dvh] flex-col gap-2 overflow-y-auto">
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
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 sm:max-w-3xl">
		<Dialog.Header>
			<Dialog.Title>Record DNS — {currentZone}</Dialog.Title>
			<Dialog.Description>Tambah atau hapus record DNS authoritative pada zone ini.</Dialog.Description>
		</Dialog.Header>

		<div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
			<form onsubmit={submitAddRecord} class="rounded-lg border p-3.5">
				<Field.FieldGroup class="grid gap-3 sm:grid-cols-2 lg:grid-cols-[1fr_110px_1.6fr_90px_auto]">
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
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>Nama Record</Table.Head>
							<Table.Head>Tipe</Table.Head>
							<Table.Head>TTL</Table.Head>
							<Table.Head>Konten</Table.Head>
							<Table.Head class="text-right">Aksi</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#if recordsLoading}
							<Table.Row>
								<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
									Memuat record DNS...
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
										<Table.Cell data-label="Nama" class="font-semibold">
											<span>{rr.name}</span>
											{#if isEdgeProxy}
												<Badge variant="secondary" class="ml-1.5 text-[10px]">Edge Proxy</Badge>
											{/if}
										</Table.Cell>
										<Table.Cell data-label="Tipe">
											<Badge variant="outline">{rr.type}</Badge>
										</Table.Cell>
										<Table.Cell data-label="TTL">{rr.ttl}</Table.Cell>
										<Table.Cell data-label="Konten" class="break-all font-mono text-xs sm:max-w-64">
											{rec.content}
										</Table.Cell>
										<Table.Cell data-label="Aksi" class="text-right">
											{#if isProtected}
												<span class="text-xs text-muted-foreground">Sistem</span>
											{:else}
												<Button
													variant="destructive"
													size="xs"
													onclick={() => deleteRecord(rr.name, rr.type)}
												>
													<Trash2Icon data-icon="inline-start" />
													Hapus
												</Button>
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
		<Dialog.Header>
			<Dialog.Title>Ekspos ke VPS Edge Gateway</Dialog.Title>
			<Dialog.Description>
				Ekspos vhost lokal atau port aplikasi lokal ke internet melalui VPS Edge Gateway dengan otomatisasi DNS &amp; SSL Let's Encrypt.
			</Dialog.Description>
		</Dialog.Header>

		<form id="pdns-expose-form" onsubmit={submitExpose} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
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
		<Dialog.Header>
			<Dialog.Title>Deploy &amp; Install VPS Edge Gateway</Dialog.Title>
			<Dialog.Description>
				Instalasi otomatis PowerDNS Authoritative, SQLite3, Nginx Reverse Proxy, dan Edge Agent ke VPS via SSH.
			</Dialog.Description>
		</Dialog.Header>

		<form id="pdns-deploy-form" onsubmit={submitDeployVPS} class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pr-1">
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
						<Input
							id="vps-deploy-password"
							type="password"
							autocomplete="new-password"
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
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
		<Dialog.Header>
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
