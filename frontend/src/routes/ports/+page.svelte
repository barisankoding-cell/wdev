<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import { api, ApiError } from '#lib/api/client.js';
	import { modeStore, MODE_LABELS } from '#lib/stores/mode.svelte.js';
	import type { PortColorTone, PortEntry, PortScopeDetail, PortStatusReport } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import EthernetPortIcon from '@lucide/svelte/icons/ethernet-port';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import LaptopIcon from '@lucide/svelte/icons/laptop';
	import InfoIcon from '@lucide/svelte/icons/info';
	import CheckIcon from '@lucide/svelte/icons/check';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import CopyIcon from '@lucide/svelte/icons/copy';

	type ColorFilter = 'all' | PortColorTone;

	let report = $state<PortStatusReport | null>(null);
	let loading = $state(true);
	let refreshing = $state(false);
	let loadError = $state('');
	let categoryFilter = $state<string>('all');
	let colorFilter = $state<ColorFilter>('all');
	let showNotInstalled = $state(true);

	const categories = [
		{ id: 'all', label: 'Semua Kategori' },
		{ id: 'system', label: 'Panel & Akses' },
		{ id: 'web', label: 'Web & Proxy' },
		{ id: 'database', label: 'Database & Cache' },
		{ id: 'dns', label: 'DNS & Edge' },
		{ id: 'mail', label: 'Mail Server' }
	];

	function badgeToneFromColor(color: PortColorTone): 'success' | 'warning' | 'destructive' | 'neutral' {
		switch (color) {
			case 'green':
				return 'success';
			case 'yellow':
				return 'warning';
			case 'red':
				return 'destructive';
			default:
				return 'neutral';
		}
	}

	function pillClassFromColor(color: PortColorTone): string {
		switch (color) {
			case 'green':
				return 'border-success/35 bg-success/15 text-success';
			case 'yellow':
				return 'border-amber-500/35 bg-amber-500/15 text-amber-700 dark:text-amber-400';
			case 'red':
				return 'border-destructive/35 bg-destructive/15 text-destructive';
			default:
				return 'border-border bg-secondary text-secondary-foreground';
		}
	}

	function dotClassFromColor(color: PortColorTone): string {
		switch (color) {
			case 'green':
				return 'bg-success';
			case 'yellow':
				return 'bg-amber-500';
			case 'red':
				return 'bg-destructive';
			default:
				return 'bg-muted-foreground';
		}
	}

	function scopeTooltip(port: PortEntry, scopeName: string, detail: PortScopeDetail): string {
		const targetPart = detail.target ? ` (${detail.target})` : '';
		const msgPart = detail.detail ? ` — ${detail.detail}` : '';
		return `${port.name} :${port.port} [${scopeName}]${targetPart}: ${detail.label}${msgPart}`;
	}

	const allPorts = $derived(report?.ports ?? []);

	const pillPorts = $derived.by(() =>
		showNotInstalled ? allPorts : allPorts.filter((p) => p.installed)
	);

	const filteredPorts = $derived.by(() => {
		return allPorts.filter((p) => {
			if (!showNotInstalled && !p.installed) return false;
			if (categoryFilter !== 'all' && p.category !== categoryFilter) return false;
			if (colorFilter !== 'all' && p.overall_color !== colorFilter) return false;
			return true;
		});
	});

	const blockedOrWarningPorts = $derived.by(() =>
		allPorts.filter((p) => p.installed && p.overall_color === 'yellow')
	);

	const closedPorts = $derived.by(() =>
		allPorts.filter((p) => p.installed && p.overall_color === 'red')
	);

	async function copyText(val: string, label: string) {
		if (!val || val === '-') return;
		try {
			await navigator.clipboard.writeText(val);
			toast.success(`${label} (${val}) disalin.`);
		} catch {
			toast.error(`Gagal menyalin ${label}`);
		}
	}

	async function loadPorts(forceRefresh = false) {
		if (forceRefresh) {
			refreshing = true;
		} else if (!report) {
			loading = true;
		}
		loadError = '';
		try {
			report = await api.getPortStatus(forceRefresh);
			if (forceRefresh) {
				toast.success('Pemeriksaan port Lokal, VPN, dan IP VPS diperbarui.');
			}
		} catch (err) {
			const msg = err instanceof ApiError ? err.message : 'Gagal memuat status port wdev';
			loadError = msg;
			if (forceRefresh) {
				toast.error('Gagal Memeriksa Port', { description: msg });
			}
		} finally {
			loading = false;
			refreshing = false;
		}
	}

	onMount(() => {
		void loadPorts(false);
	});
</script>

<div class="space-y-6">
	<!-- Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div class="space-y-1">
			<div class="flex items-center gap-2">
				<EthernetPortIcon class="size-5 text-primary" />
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Cek Port Status &amp; Jaringan</h1>
				{#if report?.network?.mode}
					<Badge variant="outline" class="font-mono text-xs">
						Mode: {MODE_LABELS[report.network.mode] ?? report.network.mode}
					</Badge>
				{/if}
			</div>
			<p class="text-sm text-muted-foreground">
				Inventaris seluruh port yang berkaitan dengan layanan <span class="font-mono font-medium">wdev</span> beserta status keterjangkauan pada jaringan <strong>Lokal</strong>, <strong>VPN (NetBird)</strong>, dan <strong>IP VPS / Publik</strong>.
			</p>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			<Button
				variant="outline"
				size="sm"
				disabled={refreshing || loading}
				onclick={() => loadPorts(true)}
			>
				{#if refreshing}
					<Spinner class="size-3.5" />
				{:else}
					<RefreshCwIcon data-icon="inline-start" />
				{/if}
				<span>{refreshing ? 'Memeriksa Port...' : 'Cek Ulang Port'}</span>
			</Button>
		</div>
	</div>

	{#if loadError}
		<Alert.Root variant="destructive">
			<AlertCircleIcon />
			<Alert.Title>Gagal Memuat Diagnostik Port</Alert.Title>
			<Alert.Description>{loadError}</Alert.Description>
		</Alert.Root>
	{/if}

	<!-- Ringkasan Alamat Jaringan & Firewall (4 Kartu) -->
	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
		<Card.Root>
			<Card.Content class="space-y-2 p-4">
				<div class="flex items-center justify-between">
					<span class="text-xs font-medium text-muted-foreground">Lokal &amp; LAN Host</span>
					<LaptopIcon class="size-4 text-primary" />
				</div>
				<div class="flex items-center justify-between gap-2">
					<span class="font-mono text-base font-bold tabular-nums">
						{report?.network.local_ip || '127.0.0.1'}
					</span>
					<StatusBadge tone="success" label="Loopback" />
				</div>
				<div class="flex items-center justify-between text-xs text-muted-foreground">
					<span>LAN: <span class="font-mono">{report?.network.lan_ip || '-'}</span></span>
					{#if report?.network.lan_ip}
						<button
							type="button"
							class="inline-flex items-center gap-1 hover:text-foreground"
							onclick={() => copyText(report?.network.lan_ip || '', 'IP LAN')}
							title="Salin IP LAN"
						>
							<CopyIcon class="size-3" />
						</button>
					{/if}
				</div>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Content class="space-y-2 p-4">
				<div class="flex items-center justify-between">
					<span class="text-xs font-medium text-muted-foreground">VPN Mesh (NetBird)</span>
					<NetworkIcon class="size-4 text-primary" />
				</div>
				<div class="flex items-center justify-between gap-2">
					<span class="truncate font-mono text-base font-bold tabular-nums">
						{report?.network.vpn_ip || '-'}
					</span>
					{#if report?.network.netbird_active && report?.network.vpn_ip}
						<StatusBadge tone="success" label="Aktif" />
					{:else if report?.network.netbird_active}
						<StatusBadge tone="warning" label="IP N/A" />
					{:else}
						<StatusBadge tone="neutral" label="Nonaktif" />
					{/if}
				</div>
				<div class="flex items-center justify-between text-xs text-muted-foreground">
					{#if report?.network.vps_vpn_ip}
						<span>Edge VPS VPN: <span class="font-mono">{report.network.vps_vpn_ip}</span></span>
					{:else if report?.network.vpn_ip}
						<span>Subnet CGNAT <span class="font-mono">100.64.0.0/10</span></span>
						<button
							type="button"
							class="inline-flex items-center gap-1 hover:text-foreground"
							onclick={() => copyText(report?.network.vpn_ip || '', 'IP NetBird')}
							title="Salin IP NetBird"
						>
							<CopyIcon class="size-3" />
						</button>
					{:else}
						<span>Interface NetBird (wt0) tidak aktif</span>
					{/if}
				</div>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Content class="space-y-2 p-4">
				<div class="flex items-center justify-between">
					<span class="text-xs font-medium text-muted-foreground">IP VPS / Publik</span>
					<GlobeIcon class="size-4 text-primary" />
				</div>
				<div class="flex items-center justify-between gap-2">
					<span class="truncate font-mono text-base font-bold tabular-nums">
						{report?.network.mode === 'local'
							? 'Mode Lokal'
							: report?.network.vps_public_ip || 'Belum Terdeteksi'}
					</span>
					{#if report?.network.mode === 'local'}
						<StatusBadge tone="neutral" label="Non-Publik" />
					{:else if report?.network.vps_public_ip}
						<StatusBadge tone="info" label={report?.network.mode === 'edge' ? 'Edge IP' : 'Publik IP'} />
					{:else}
						<StatusBadge tone="warning" label="N/A" />
					{/if}
				</div>
				<div class="flex items-center justify-between text-xs text-muted-foreground">
					{#if report?.network.mode === 'local'}
						<span>Tidak mengekspos port ke internet publik</span>
					{:else if report?.network.mode === 'edge'}
						<span>Uji koneksi langsung ke IP VPS Edge</span>
					{:else}
						<span>Uji eksternal via ifconfig.co/port</span>
					{/if}
					{#if report?.network.vps_public_ip}
						<button
							type="button"
							class="inline-flex items-center gap-1 hover:text-foreground"
							onclick={() => copyText(report?.network.vps_public_ip || '', 'IP VPS Publik')}
							title="Salin IP VPS Publik"
						>
							<CopyIcon class="size-3" />
						</button>
					{/if}
				</div>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Content class="space-y-2 p-4">
				<div class="flex items-center justify-between">
					<span class="text-xs font-medium text-muted-foreground">Firewall &amp; Ringkasan Port</span>
					<ShieldCheckIcon class="size-4 text-primary" />
				</div>
				<div class="flex flex-wrap items-center gap-1.5">
					<span class="inline-flex items-center gap-1 rounded-md border border-success/35 bg-success/15 px-2 py-0.5 font-mono text-xs font-semibold text-success tabular-nums">
						<span class="size-1.5 rounded-full bg-success"></span>
						{report?.summary.green ?? 0} Terbuka
					</span>
					<span class="inline-flex items-center gap-1 rounded-md border border-amber-500/35 bg-amber-500/15 px-2 py-0.5 font-mono text-xs font-semibold text-amber-700 dark:text-amber-400 tabular-nums">
						<span class="size-1.5 rounded-full bg-amber-500"></span>
						{report?.summary.yellow ?? 0} Terblokir
					</span>
					<span class="inline-flex items-center gap-1 rounded-md border border-destructive/35 bg-destructive/15 px-2 py-0.5 font-mono text-xs font-semibold text-destructive tabular-nums">
						<span class="size-1.5 rounded-full bg-destructive"></span>
						{report?.summary.red ?? 0} Mati
					</span>
				</div>
				<p class="truncate text-xs text-muted-foreground" title={report?.network.firewall.summary}>
					{report?.network.firewall.summary || 'Memeriksa firewall host...'}
				</p>
			</Card.Content>
		</Card.Root>
	</div>

	<!-- Ringkasan Cepat Pill Port (Pola Mail Server: Hijau / Kuning / Merah / Abu) -->
	<Card.Root>
		<Card.Header class="pb-3">
			<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
				<div>
					<Card.Title class="text-base">Peta Cepat Port wdev</Card.Title>
					<Card.Description>
						Klik indikator warna untuk menyaring daftar, atau arahkan kursor ke angka port untuk melihat detail Lokal, VPN, dan IP VPS.
					</Card.Description>
				</div>
				<div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
					<span class="inline-flex items-center gap-1.5">
						<span class="size-2 rounded-full bg-success"></span> Terbuka / Optimal
					</span>
					<span class="inline-flex items-center gap-1.5">
						<span class="size-2 rounded-full bg-amber-500"></span> Terblokir / Perlu Cek
					</span>
					<span class="inline-flex items-center gap-1.5">
						<span class="size-2 rounded-full bg-destructive"></span> Mati / Tertutup
					</span>
					<span class="inline-flex items-center gap-1.5">
						<span class="size-2 rounded-full bg-muted-foreground"></span> Internal / Belum Terpasang
					</span>
				</div>
			</div>
		</Card.Header>
		<Card.Content class="space-y-4">
			{#if loading && !report}
				<div class="flex items-center gap-2 py-4 text-sm text-muted-foreground">
					<Spinner class="size-4" />
					<span>Memeriksa port Lokal, VPN NetBird, Firewall, dan IP VPS...</span>
				</div>
			{:else}
				<div class="flex flex-wrap gap-1.5" aria-label="Ringkasan status port wdev">
					{#each pillPorts as item (item.id)}
						<button
							type="button"
							onclick={() => {
								categoryFilter = item.category;
								colorFilter = 'all';
							}}
							title={`${item.name} (Port ${item.port}/${item.protocol}): ${item.overall_label} — Lokal: ${item.local.label} | VPN: ${item.vpn.label} | VPS: ${item.vps.label}`}
							class={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1 font-mono text-xs font-medium tabular-nums transition-opacity hover:opacity-85 ${pillClassFromColor(item.overall_color)}`}
						>
							<span class={`size-1.5 shrink-0 rounded-full ${dotClassFromColor(item.overall_color)}`}></span>
							<span>{item.port}</span>
							<span class="font-sans text-[11px] opacity-80">{item.service}</span>
						</button>
					{/each}
				</div>

				<!-- Filter Kategori & Warna -->
				<div class="flex flex-col gap-2 border-t pt-3 md:flex-row md:items-center md:justify-between">
					<div class="flex flex-wrap gap-1.5">
						{#each categories as cat (cat.id)}
							<Button
								variant={categoryFilter === cat.id ? 'default' : 'outline'}
								size="sm"
								class="h-7 px-2.5 text-xs"
								onclick={() => (categoryFilter = cat.id)}
							>
								{cat.label}
							</Button>
						{/each}
					</div>

					<div class="flex flex-wrap items-center gap-1.5">
						<Button
							variant={colorFilter === 'all' ? 'secondary' : 'ghost'}
							size="sm"
							class="h-7 px-2 text-xs"
							onclick={() => (colorFilter = 'all')}
						>
							Semua Status
						</Button>
						<Button
							variant={colorFilter === 'green' ? 'secondary' : 'ghost'}
							size="sm"
							class="h-7 px-2 text-xs text-success"
							onclick={() => (colorFilter = 'green')}
						>
							Hijau
						</Button>
						<Button
							variant={colorFilter === 'yellow' ? 'secondary' : 'ghost'}
							size="sm"
							class="h-7 px-2 text-xs text-amber-600 dark:text-amber-400"
							onclick={() => (colorFilter = 'yellow')}
						>
							Kuning
						</Button>
						<Button
							variant={colorFilter === 'red' ? 'secondary' : 'ghost'}
							size="sm"
							class="h-7 px-2 text-xs text-destructive"
							onclick={() => (colorFilter = 'red')}
						>
							Merah
						</Button>
						<Button
							variant={showNotInstalled ? 'outline' : 'ghost'}
							size="sm"
							class="h-7 px-2.5 text-xs"
							onclick={() => (showNotInstalled = !showNotInstalled)}
						>
							{showNotInstalled ? 'Sembunyikan Belum Terpasang' : 'Tampilkan Semua Port wdev'}
						</Button>
					</div>
				</div>
			{/if}
		</Card.Content>
	</Card.Root>

	<!-- Tabel Detail Port: Lokal · VPN · IP VPS -->
	<div class="rounded-lg border bg-card">
		<div class="flex flex-col gap-1 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<h2 class="text-sm font-semibold">Detail Status Port (Lokal, VPN NetBird &amp; IP VPS)</h2>
				<p class="text-xs text-muted-foreground">
					Menampilkan {filteredPorts.length} dari {allPorts.length} port terkait wdev.
				</p>
			</div>
			{#if report?.checked_at}
				<span class="font-mono text-[11px] text-muted-foreground">
					Terakhir dicek: {new Date(report.checked_at).toLocaleTimeString('id-ID')}
				</span>
			{/if}
		</div>

		{#if loading && !report}
			<div class="flex items-center justify-center gap-2 p-8 text-sm text-muted-foreground">
				<Spinner class="size-4" />
				<span>Memuat daftar port...</span>
			</div>
		{:else if filteredPorts.length === 0}
			<Empty.Root class="border-0 py-10">
				<Empty.Header>
					<Empty.Media variant="icon">
						<EthernetPortIcon />
					</Empty.Media>
					<Empty.Title>Tidak Ada Port Sesuai Filter</Empty.Title>
					<Empty.Description>
						Ubah filter kategori, warna status, atau aktifkan tampilan port yang belum terpasang.
					</Empty.Description>
				</Empty.Header>
				<Empty.Content>
					<Button
						variant="outline"
						size="sm"
						onclick={() => {
							categoryFilter = 'all';
							colorFilter = 'all';
							showNotInstalled = true;
						}}
					>
						Reset Filter
					</Button>
				</Empty.Content>
			</Empty.Root>
		{:else}
			<ResponsiveTableWrapper label="Status Port Terkait wdev">
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head class="w-[105px]">Port</Table.Head>
							<Table.Head class="min-w-[200px]">Layanan &amp; Status</Table.Head>
							<Table.Head class="min-w-[165px]">
								Lokal ({report?.network.local_ip || '127.0.0.1'})
							</Table.Head>
							<Table.Head class="min-w-[175px]">
								VPN ({report?.network.vpn_ip || report?.network.vps_vpn_ip || 'NetBird'})
							</Table.Head>
							<Table.Head class="min-w-[180px]">
								IP VPS ({report?.network.mode === 'local'
									? 'Mode Lokal'
									: report?.network.vps_public_ip || 'Publik'})
							</Table.Head>
							<Table.Head class="min-w-[250px]">Keterangan &amp; Rekomendasi</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each filteredPorts as item (item.id)}
							<Table.Row class={!item.installed ? 'opacity-70' : ''}>
								<Table.Cell data-label="Port" class="align-top">
									<div class="flex items-center gap-1.5 font-mono text-sm font-bold">
										<span class={`size-2 shrink-0 rounded-full ${dotClassFromColor(item.overall_color)}`}></span>
										<span>{item.port}</span>
									</div>
									<div class="mt-0.5 font-mono text-[11px] text-muted-foreground">
										{item.protocol}
									</div>
									{#if item.bind_addresses && item.bind_addresses.length > 0}
										<div class="mt-0.5 font-mono text-[10px] text-muted-foreground" title={item.bind_addresses.join(', ')}>
											{item.bind_addresses.join(', ')}
										</div>
									{/if}
								</Table.Cell>

								<Table.Cell data-label="Layanan" class="align-top">
									<div class="font-semibold">{item.name}</div>
									<div class="mt-1 flex flex-wrap items-center gap-1">
										<StatusBadge
											tone={badgeToneFromColor(item.overall_color)}
											label={item.overall_label}
										/>
										<Badge variant="outline" class="text-[10px] font-normal">
											{item.category_label}
										</Badge>
									</div>
									{#if item.config_hint}
										<div class="mt-1 font-mono text-[11px] text-muted-foreground">
											{item.config_hint}
										</div>
									{/if}
								</Table.Cell>

								<Table.Cell data-label="Lokal" class="align-top">
									<div title={scopeTooltip(item, 'Lokal', item.local)}>
										<StatusBadge
											tone={badgeToneFromColor(item.local.color)}
											label={item.local.label}
										/>
									</div>
									{#if item.local.target && item.installed}
										<div class="mt-1 font-mono text-[11px] text-muted-foreground">
											{item.local.target}
										</div>
									{/if}
									{#if item.local.detail}
										<div class="mt-0.5 text-[11px] leading-snug text-muted-foreground">
											{item.local.detail}
										</div>
									{/if}
								</Table.Cell>

								<Table.Cell data-label="VPN NetBird" class="align-top">
									<div title={scopeTooltip(item, 'VPN', item.vpn)}>
										<StatusBadge
											tone={badgeToneFromColor(item.vpn.color)}
											label={item.vpn.label}
										/>
									</div>
									{#if item.vpn.target && item.installed}
										<div class="mt-1 font-mono text-[11px] text-muted-foreground">
											{item.vpn.target}
										</div>
									{/if}
									{#if item.vpn.detail}
										<div class="mt-0.5 text-[11px] leading-snug text-muted-foreground">
											{item.vpn.detail}
										</div>
									{/if}
								</Table.Cell>

								<Table.Cell data-label="IP VPS" class="align-top">
									<div title={scopeTooltip(item, 'IP VPS', item.vps)}>
										<StatusBadge
											tone={badgeToneFromColor(item.vps.color)}
											label={item.vps.label}
										/>
									</div>
									{#if item.vps.target && item.installed}
										<div class="mt-1 font-mono text-[11px] text-muted-foreground">
											{item.vps.target}
										</div>
									{/if}
									{#if item.vps.detail}
										<div class="mt-0.5 text-[11px] leading-snug text-muted-foreground">
											{item.vps.detail}
										</div>
									{/if}
								</Table.Cell>

								<Table.Cell data-label="Keterangan" class="align-top text-xs text-muted-foreground">
									<p class="leading-relaxed">{item.description}</p>
									{#if item.recommendation}
										<p class={`mt-1.5 rounded border px-2 py-1 text-[11px] leading-relaxed ${
											item.overall_color === 'red'
												? 'border-destructive/30 bg-destructive/5 text-destructive'
												: item.overall_color === 'yellow'
													? 'border-amber-500/30 bg-amber-500/5 text-amber-700 dark:text-amber-400'
													: 'border-border bg-muted/40 text-muted-foreground'
										}`}>
											{item.recommendation}
										</p>
									{/if}
								</Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</ResponsiveTableWrapper>
		{/if}

		<!-- Ringkasan Aksi / Rekomendasi Bawah Tabel -->
		{#if report}
			<div class="space-y-2 border-t p-4">
				{#if closedPorts.length > 0}
					<Alert.Root variant="destructive">
						<AlertCircleIcon />
						<Alert.Title>
							Layanan Terpasang Tidak Listen ({closedPorts.map((p) => `${p.port}/${p.service}`).join(', ')})
						</Alert.Title>
						<Alert.Description class="text-xs leading-relaxed">
							Port di atas dimiliki oleh layanan yang sudah terpasang tetapi saat ini tidak menerima koneksi di server lokal. Jalankan ulang layanannya dari menu <strong>Status &amp; Update</strong> atau periksa log service terkait.
						</Alert.Description>
					</Alert.Root>
				{/if}

				{#if blockedOrWarningPorts.length > 0}
					<Alert.Root class="border-amber-500/30 bg-amber-500/5">
						<InfoIcon class="text-amber-600 dark:text-amber-400" />
						<Alert.Title class="font-semibold text-amber-700 dark:text-amber-400">
							Port Terblokir / Memerlukan Perhatian ({blockedOrWarningPorts.map((p) => p.port).join(', ')})
						</Alert.Title>
						<Alert.Description class="text-xs leading-relaxed text-muted-foreground">
							Port berhasil aktif di server lokal, namun ditolak pada interface VPN (misal database hanya bind <span class="font-mono">127.0.0.1</span>) atau terblokir firewall host / Security Group cloud / ISP pada IP VPS publik. Periksa kolom <strong>VPN</strong>, <strong>IP VPS</strong>, dan rekomendasi di setiap baris.
						</Alert.Description>
					</Alert.Root>
				{/if}

				{#if closedPorts.length === 0 && blockedOrWarningPorts.length === 0}
					<div class="flex items-center gap-2 text-xs text-muted-foreground">
						<CheckIcon class="size-4 text-success" />
						<span>Seluruh port layanan yang terpasang berjalan optimal sesuai kebijakan Lokal, VPN, dan IP VPS.</span>
					</div>
				{/if}
			</div>
		{/if}
	</div>
</div>
