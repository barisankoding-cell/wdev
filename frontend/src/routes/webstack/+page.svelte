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
	import type { SiteRecord } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import SearchIcon from '@lucide/svelte/icons/search';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import CloudUploadIcon from '@lucide/svelte/icons/cloud-upload';

	let sites = $state<SiteRecord[]>([]);
	let loading = $state(true);
	let searchQuery = $state('');
	let siteDeleting = $state<Record<string, boolean>>({});

	let createOpen = $state(false);
	let creating = $state(false);
	let domain = $state('');
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
		{ value: 'generic', label: 'Generic PHP / Static HTML' },
		{ value: 'proxy', label: 'Reverse Proxy (Node/Go/Python/Docker)' }
	];

	const phpVersions = ['8.4', '8.3', '8.2', '8.1'];
	const isProxy = $derived(framework === 'proxy');
	const domainInvalid = $derived(domainTouched && domain.trim().length === 0);

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

	async function loadSites() {
		loading = true;
		try {
			sites = (await api.listSites()) ?? [];
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memuat daftar virtual host');
		} finally {
			loading = false;
		}
	}

	onMount(loadSites);

	$effect(() => {
		if (cp.refreshTick > 0) loadSites();
	});

	function openCreate() {
		domain = '';
		domainTouched = false;
		framework = 'laravel';
		phpVersion = '8.4';
		rootPath = '';
		proxyPortStr = '3000';
		sslEnabled = true;
		createOpen = true;
	}

	async function submitCreate(e: SubmitEvent) {
		e.preventDefault();
		domainTouched = true;
		if (!domain.trim()) return;
		const proxyPort = parseInt(proxyPortStr, 10) || 0;
		if (isProxy && (proxyPort < 1 || proxyPort > 65535)) {
			toast.error('Port aplikasi proxy harus antara 1 sampai 65535.');
			return;
		}

		creating = true;
		try {
			await api.createSite({
				domain: domain.trim(),
				framework,
				php_version: phpVersion,
				root_path: rootPath.trim(),
				proxy_port: proxyPort,
				ssl_enabled: sslEnabled
			});
			toast.success(`Virtual host ${domain.trim()} berhasil dibuat!`);
			createOpen = false;
			await loadSites();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal membuat virtual host');
		} finally {
			creating = false;
		}
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
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Web Stack &amp; Domain Lokal</h1>
			<p class="text-sm text-muted-foreground">
				Kelola virtual host Nginx (<code>wdev site</code>), sertifikat SSL lokal (<code>mkcert</code>), dan eksposur VPS Edge.
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
						? 'Buat virtual host lokal pertama untuk proyek Laravel, WordPress, atau reverse proxy.'
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
		<ResponsiveTableWrapper label="Daftar Virtual Host Lokal">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Domain Lokal</Table.Head>
						<Table.Head>Framework</Table.Head>
						<Table.Head>Target Runtime</Table.Head>
						<Table.Head>Document Root</Table.Head>
						<Table.Head>SSL</Table.Head>
						<Table.Head>Ekspos VPS</Table.Head>
						<Table.Head class="text-right">Aksi</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each filteredSites as site (site.domain)}
						{@const proto = site.ssl_enabled ? 'https://' : 'http://'}
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
								{site.framework === 'proxy' ? `Forward :${site.proxy_port}` : `PHP ${site.php_version}`}
							</Table.Cell>
							<Table.Cell data-label="Root" class="sm:max-w-52">
								<code class="block break-all sm:truncate text-xs text-muted-foreground" title={site.root_path || '-'}>
									{site.root_path || '-'}
								</code>
							</Table.Cell>
							<Table.Cell data-label="SSL">
								{#if site.ssl_enabled}
									<StatusBadge tone="success" label="HTTPS" />
								{:else}
									<StatusBadge tone="neutral" label="HTTP" showIcon={false} />
								{/if}
							</Table.Cell>
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
							<Table.Cell data-label="Aksi" class="text-right">
								<Button
									variant="destructive"
									size="xs"
									disabled={Boolean(siteDeleting[site.domain])}
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
				Membuat konfigurasi Nginx (<code>wdev site create</code>), entri <code>/etc/hosts</code>, dan sertifikat SSL lokal.
			</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={submitCreate} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field data-invalid={domainInvalid ? 'true' : undefined}>
					<Field.FieldLabel for="site-domain">Domain Lokal</Field.FieldLabel>
					<Input
						id="site-domain"
						name="domain"
						placeholder="proyekanda.test"
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
					<Field.FieldDescription>Akhiran <code>.test</code> disarankan untuk pengembangan lokal.</Field.FieldDescription>
					{#if domainInvalid}
						<Field.FieldError errors={[{ message: 'Domain lokal wajib diisi.' }]} />
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

					<Field.Field>
						<Field.FieldLabel for="site-root-path">Document Root (Opsional)</Field.FieldLabel>
						<Input
							id="site-root-path"
							name="root_path"
							placeholder="/var/www/proyekanda.test"
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
						Aktifkan Sertifikat SSL Otomatis (HTTPS mkcert)
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
