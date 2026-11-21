<script lang="ts">
	import { onMount } from 'svelte';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
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

	let sites = $state<SiteRecord[]>([]);
	let zones = $state<ZoneInfo[]>([]);
	let loading = $state(true);
	let searchQuery = $state('');
	let siteDeleting = $state<Record<string, boolean>>({});
	let sslPending = $state<Record<string, boolean>>({});

	let createOpen = $state(false);
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

	const parentDomains = $derived.by(() => {
		const fromZones = Array.from(
			new Set(
				zones
					.map((z) => (z.name || '').replace(/\.$/, '').trim().toLowerCase())
					.filter((name) => Boolean(name) && !name.endsWith('.arpa'))
			)
		).sort();
		const seen = new Set(fromZones);
		const fromSites = Array.from(
			new Set(
				sites
					.map((s) => (s.domain || '').replace(/\.$/, '').trim().toLowerCase())
					.filter((name) => Boolean(name) && !name.startsWith('www.') && !seen.has(name))
			)
		).sort();
		return [...fromZones, ...fromSites];
	});

	const cleanSubPrefix = $derived(
		subdomainPrefix.trim().replace(/^\.+|\.+$/g, '').toLowerCase()
	);
	const effectiveDomain = $derived(
		domainMode === 'subdomain'
			? cleanSubPrefix && parentDomain
				? `${cleanSubPrefix}.${parentDomain}`
				: ''
			: domain.trim().replace(/^\.+|\.+$/g, '').toLowerCase()
	);
	const domainInvalid = $derived(
		domainTouched &&
			(domainMode === 'subdomain'
				? cleanSubPrefix.length === 0 || !parentDomain
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
		try {
			zones = (await api.listPDNSZones()) ?? [];
			if (!parentDomain && parentDomains.length > 0) {
				parentDomain = parentDomains[0];
			}
		} catch {
			// PowerDNS opsional di mode lokal / belum terhubung
		}
	}

	async function loadSites() {
		loading = true;
		try {
			const [siteRes, zoneRes] = await Promise.allSettled([api.listSites(), api.listPDNSZones()]);
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
		if (parentDomains.length > 0 && (!parentDomain || !parentDomains.includes(parentDomain))) {
			parentDomain = parentDomains[0];
		}
	});

	function openCreate() {
		domainMode = 'custom';
		domain = '';
		subdomainPrefix = '';
		parentDomain = parentDomains[0] ?? '';
		domainTouched = false;
		framework = 'laravel';
		phpVersion = '8.4';
		rootPath = '';
		proxyPortStr = '3000';
		sslEnabled = !isVPS;
		createOpen = true;
		void refreshZonesQuiet();
	}

	async function triggerLetsEncrypt(siteDomain: string, skipConfirm = false) {
		const clean = siteDomain.trim().replace(/\.$/, '').toLowerCase();
		if (/\.(test|local|localhost|internal|invalid)$/i.test(clean)) {
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

	async function submitCreate(e: SubmitEvent) {
		e.preventDefault();
		domainTouched = true;
		if (domainMode === 'subdomain') {
			if (!parentDomain) {
				toast.warning('Pilih domain induk terlebih dahulu atau gunakan mode Domain Utama / Manual.');
				return;
			}
			if (!cleanSubPrefix) return;
			if (!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*$/.test(cleanSubPrefix)) {
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

		const wantVPSLetsEncrypt = isVPS && sslEnabled;
		creating = true;
		try {
			const res = await api.createSite({
				domain: cleanDomain,
				framework,
				php_version: isProxy || isStatic ? '' : phpVersion,
				root_path: rootPath.trim(),
				proxy_port: proxyPort,
				ssl_enabled: wantVPSLetsEncrypt ? false : sslEnabled
			});
			toast.success(
				res?.dns_synced && res?.dns_message
					? `Virtual host ${cleanDomain} berhasil dibuat (${res.dns_message}).`
					: `Virtual host ${cleanDomain} berhasil dibuat!`
			);
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

	async function deleteSite(siteDomain: string) {
		const confirmed = await cp.confirm({
			title: 'Hapus Virtual Host',
			description: `Hapus virtual host ${siteDomain}? Tindakan ini menghapus konfigurasi Nginx dan entri /etc/hosts lokal.`,
			confirmText: 'Hapus',
			destructive: true
		});
		if (!confirmed) return;

		siteDeleting = { ...siteDeleting, [siteDomain]: true };
		try {
			await api.deleteSite(siteDomain);
			toast.success(`Virtual host ${siteDomain} berhasil dihapus.`);
			await loadSites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal menghapus virtual host');
		} finally {
			const next = { ...siteDeleting };
			delete next[siteDomain];
			siteDeleting = next;
		}
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">
				{isVPS ? 'Web Stack & Virtual Host' : 'Web Stack & Domain Lokal'}
			</h1>
			<p class="text-sm text-muted-foreground">
				{#if isVPS}
					Kelola virtual host Nginx (<code>wdev site</code>), document root, dan sertifikat SSL Let's Encrypt langsung di VPS.
				{:else if isEdge}
					Kelola virtual host Nginx (<code>wdev site</code>), sertifikat SSL lokal (<code>mkcert</code>), dan eksposur VPS Edge.
				{:else}
					Kelola virtual host Nginx (<code>wdev site</code>) dan sertifikat SSL lokal (<code>mkcert</code>).
				{/if}
			</p>
		</div>
		<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
			<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('sync-hosts')}>
				<RefreshCwIcon data-icon="inline-start" />
				Sync Hosts &amp; SSL
			</Button>
			<Button size="sm" onclick={openCreate}>
				<PlusIcon data-icon="inline-start" />
				Tambah VHost
			</Button>
		</div>
	</div>

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
						<Table.Head>{isVPS ? 'Domain' : 'Domain Lokal'}</Table.Head>
						<Table.Head>Framework</Table.Head>
						<Table.Head>Target Runtime</Table.Head>
						<Table.Head>Document Root</Table.Head>
						<Table.Head>SSL</Table.Head>
						{#if isEdge}
							<Table.Head>Ekspos VPS</Table.Head>
						{/if}
						<Table.Head class="text-right">Aksi</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each filteredSites as site (site.domain)}
						{@const proto = site.ssl_enabled ? 'https://' : 'http://'}
						{@const hasLetsEncrypt = site.ssl_enabled && site.ssl_provider === 'letsencrypt'}
						{@const sslExpiry = formatSSLExpiry(site.ssl_expires_at, site.ssl_days_left)}
						<Table.Row>
							<Table.Cell data-label="Domain">
								<a
									href={`${proto}${site.domain}`}
									target="_blank"
									rel="noopener noreferrer"
									class="inline-flex items-center gap-1 break-all font-semibold text-primary hover:underline"
								>
									<span>{site.domain}</span>
									<ExternalLinkIcon class="size-3.5 shrink-0" />
								</a>
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
								{#if site.ssl_enabled}
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
									<StatusBadge tone="neutral" label="HTTP" showIcon={false} />
								{/if}
							</Table.Cell>
							{#if isEdge}
								<Table.Cell data-label="Ekspos VPS">
									{#if site.exposed_to}
										<a
											href={`https://${site.exposed_to}`}
											target="_blank"
											rel="noopener noreferrer"
											class="inline-flex items-center gap-1 text-xs font-medium text-primary hover:underline"
										>
											<StatusBadge tone="success" label={site.exposed_to} />
										</a>
									{:else}
										<Button
											variant="outline"
											size="xs"
											href={`/powerdns?expose=${encodeURIComponent(site.domain)}`}
										>
											<CloudUploadIcon data-icon="inline-start" />
											Ekspos VPS
										</Button>
									{/if}
								</Table.Cell>
							{/if}
							<Table.Cell data-label="Aksi" class="text-right">
								<div class="flex flex-wrap justify-end gap-1.5">
									{#if isVPS}
										<Button
											variant={hasLetsEncrypt ? 'outline' : 'default'}
											size="xs"
											disabled={Boolean(sslPending[site.domain]) || Boolean(siteDeleting[site.domain])}
											onclick={() => triggerLetsEncrypt(site.domain)}
										>
											{#if sslPending[site.domain]}
												<Spinner data-icon="inline-start" />
												Memproses...
											{:else}
												<ShieldCheckIcon data-icon="inline-start" />
												{hasLetsEncrypt ? 'Perbarui SSL' : "Generate Let's Encrypt"}
											{/if}
										</Button>
									{/if}
									<Button
										variant="destructive"
										size="xs"
										disabled={Boolean(siteDeleting[site.domain]) || Boolean(sslPending[site.domain])}
										onclick={() => deleteSite(site.domain)}
									>
										{#if siteDeleting[site.domain]}
											<Spinner data-icon="inline-start" />
											Menghapus...
										{:else}
											<Trash2Icon data-icon="inline-start" />
											Hapus
										{/if}
									</Button>
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
				{:else}
					Membuat konfigurasi Nginx (<code>wdev site create</code>), entri <code>/etc/hosts</code>, dan sertifikat SSL lokal.
				{/if}
			</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={submitCreate} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field data-invalid={domainInvalid ? 'true' : undefined}>
					<div class="flex flex-wrap items-center justify-between gap-2">
						<Field.FieldLabel for={domainMode === 'subdomain' ? 'site-sub-prefix' : 'site-domain'}>
							{isVPS ? 'Domain' : 'Domain Lokal'}
						</Field.FieldLabel>
						<div class="inline-flex rounded-lg border bg-muted p-0.5 text-xs" role="group" aria-label="Mode input domain">
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
									if (!parentDomain && parentDomains.length > 0) {
										parentDomain = parentDomains[0];
									}
									void refreshZonesQuiet();
								}}
							>
								Subdomain ({parentDomains.length})
							</button>
						</div>
					</div>

					{#if domainMode === 'subdomain'}
						{#if parentDomains.length === 0}
							<div class="rounded-md border border-dashed bg-muted/40 px-3 py-2.5 text-xs text-muted-foreground">
								Belum ada domain atau zone PowerDNS yang tersedia sebagai induk subdomain. Tambahkan domain utama terlebih dahulu atau pilih <strong>Domain Utama</strong>.
							</div>
						{:else}
							<div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)]">
								<InputGroup.Root>
									<InputGroup.Input
										id="site-sub-prefix"
										name="subdomain_prefix"
										placeholder="app / api / admin"
										autocorrect="off"
										autocapitalize="none"
										spellcheck={false}
										aria-invalid={domainInvalid ? 'true' : undefined}
										bind:value={subdomainPrefix}
										onblur={() => (domainTouched = true)}
										oninput={() => {
											if (subdomainPrefix.trim()) domainTouched = false;
										}}
										required
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
									<Select.Trigger id="site-parent-domain" class="w-full" aria-label="Pilih domain induk">
										{parentDomain || 'Pilih domain induk'}
									</Select.Trigger>
									<Select.Content>
										<Select.Group>
											{#each parentDomains as pd (pd)}
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
							placeholder={isVPS ? 'domainanda.com' : 'proyekanda.test'}
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
						{#if domainMode === 'subdomain'}
							{#if isVPS}
								Pilih domain induk dan isi awalan subdomain. Record DNS A otomatis dibuat bila zone dikelola PowerDNS lokal.
							{:else}
								Pilih domain induk dan isi awalan subdomain (misal <code>app</code> atau <code>api</code>).
							{/if}
						{:else if isVPS}
							Masukkan domain FQDN yang mengarah ke IP publik VPS ini.
						{:else}
							Akhiran <code>.test</code> disarankan untuk pengembangan lokal.
						{/if}
					</Field.FieldDescription>
					{#if domainInvalid}
						<Field.FieldError
							errors={[{ message: domainMode === 'subdomain' ? 'Awalan subdomain dan domain induk wajib diisi.' : 'Domain wajib diisi.' }]}
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
						<Input
							id="site-root-path"
							name="root_path"
							placeholder={effectiveDomain ? `/var/www/${effectiveDomain}` : (isVPS ? '/var/www/domainanda.com' : '/var/www/proyekanda.test')}
							autocorrect="off"
							autocapitalize="none"
							spellcheck={false}
							bind:value={rootPath}
						/>
						<Field.FieldDescription>Kosongkan untuk otomatis di /var/www/&lt;domain&gt;.</Field.FieldDescription>
					</Field.Field>
				{/if}

				<Field.Field orientation="horizontal">
					<Checkbox id="site-ssl" bind:checked={sslEnabled} />
					<Field.FieldLabel for="site-ssl" class="font-normal">
						{isVPS
							? "Terbitkan SSL Let's Encrypt (HTTPS) setelah virtual host dibuat"
							: 'Aktifkan Sertifikat SSL Otomatis (HTTPS mkcert)'}
					</Field.FieldLabel>
				</Field.Field>
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
