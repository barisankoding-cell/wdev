<script lang="ts">
	import { tick, untrack } from 'svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import FolderPickerDialog from '#lib/components/modals/FolderPickerDialog.svelte';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import type { FileRootInfo } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import PlusIcon from '@lucide/svelte/icons/plus';

	let {
		open = $bindable(false),
		pdnsZones = [],
		parentDomains = [],
		onRefreshZones,
		onCreated
	}: {
		open: boolean;
		pdnsZones: string[];
		parentDomains: string[];
		onRefreshZones: () => void | Promise<void>;
		onCreated: (opts: { domain: string; wantVPSLetsEncrypt: boolean }) => void | Promise<void>;
	} = $props();

	let fileRoots = $state<FileRootInfo[]>([]);
	let folderPickerOpen = $state(false);
	let creating = $state(false);
	let formError = $state('');

	let domainMode = $state<'custom' | 'subdomain'>('custom');
	let domain = $state('');
	let subdomainPrefix = $state('');
	let parentDomain = $state('');
	let domainTouched = $state(false);

	let framework = $state('laravel');
	let phpVersion = $state('8.4');
	let rootPath = $state('');
	let selectedDriveRoot = $state<string | null>(null);
	let proxyPortStr = $state('3000');
	let prevOpen = false;

	let sslEnabled = $state(true);
	let edgeExposeEnabled = $state(true);
	let edgeSSLEnabled = $state(true);
	let addLocalHosts = $state(false);

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
	const isLocal = $derived(modeStore.mode === 'local');
	const supportsDNS = $derived(isVPS || isEdge);

	function isLocalOnlyDomain(dom: string): boolean {
		const clean = dom.trim().replace(/\.$/, '').toLowerCase();
		return /\.(test|local|localhost|invalid|example|arpa|internal)$/.test(clean);
	}

	const domainChoices = $derived(isEdge ? pdnsZones : parentDomains);

	const detectedDrives = $derived.by(() => {
		const byPath = new Map<string, { name: string; path: string }>();
		for (const r of fileRoots) {
			if (r.kind === 'drive') {
				byPath.set(r.path, { name: r.name, path: r.path });
			}
		}
		for (const d of cp.systemStatus?.mount_drives ?? []) {
			if (!byPath.has(d.mount_point)) {
				byPath.set(d.mount_point, { name: d.name, path: d.mount_point });
			}
		}
		return Array.from(byPath.values());
	});

	const cleanSubPrefix = $derived(
		subdomainPrefix.trim().replace(/^\.+|\.+$/g, '').toLowerCase()
	);

	const effectiveDomain = $derived(
		isEdge
			? !parentDomain
				? ''
				: cleanSubPrefix
					? `${cleanSubPrefix}.${parentDomain}`
					: parentDomain
			: isVPS && domainMode === 'subdomain'
				? cleanSubPrefix && parentDomain
					? `${cleanSubPrefix}.${parentDomain}`
					: ''
				: domain.trim().replace(/^\.+|\.+$/g, '').toLowerCase()
	);

	const domainInvalid = $derived(
		domainTouched &&
			(isEdge
				? !parentDomain
				: isVPS && domainMode === 'subdomain'
					? cleanSubPrefix.length === 0 || !parentDomain
					: effectiveDomain.length === 0)
	);

	const activePreset = $derived.by(() => {
		if (framework === 'laravel' && phpVersion === '8.4') return 'laravel';
		if (framework === 'wordpress' && phpVersion === '8.4') return 'wordpress';
		if (framework === 'proxy') return 'proxy';
		if (framework === 'static') return 'static';
		return 'custom';
	});

	const metaSubtitle = $derived.by(() => {
		const dom = effectiveDomain || (isLocal ? '<domain>.test' : '<domain>');
		const targetDesc = isProxy
			? `http://127.0.0.1:${proxyPortStr.trim() || '3000'}`
			: rootPath.trim()
				? rootPath.trim()
				: `/var/www/${effectiveDomain || '<domain>'}`;
		if (isEdge) {
			return `${dom} → ${targetDesc} · Tunnel VPS Edge${modeStore.publicIP ? ` (${modeStore.publicIP})` : ''}`;
		}
		if (isVPS) {
			return `/etc/nginx/sites-available/${dom} · Target: ${targetDesc} · VPS Publik`;
		}
		return `/etc/nginx/sites-available/${dom} · Target: ${targetDesc} · Lokal`;
	});

	async function refreshZonesQuiet() {
		if (!supportsDNS) return;
		await onRefreshZones();
		if (!parentDomain && domainChoices.length > 0) {
			parentDomain = domainChoices[0];
		}
	}

	function applyPreset(preset: 'laravel' | 'wordpress' | 'proxy' | 'static') {
		formError = '';
		framework = preset;
		if (preset === 'laravel' || preset === 'wordpress') {
			phpVersion = '8.4';
		} else if (preset === 'proxy' && !proxyPortStr.trim()) {
			proxyPortStr = '3000';
		}
	}

	function switchDomainMode(nextMode: string) {
		if (nextMode !== 'custom' && nextMode !== 'subdomain') return;
		domainMode = nextMode;
		domainTouched = false;
		formError = '';
		if (nextMode === 'subdomain') {
			if (!parentDomain && domainChoices.length > 0) {
				parentDomain = domainChoices[0];
			}
			void refreshZonesQuiet();
		}
	}

	$effect(() => {
		const isOpen = open;
		if (isOpen && !prevOpen) {
			prevOpen = true;
			untrack(() => {
				domainMode = isEdge ? 'subdomain' : 'custom';
				domain = '';
				subdomainPrefix = '';
				parentDomain = domainChoices[0] ?? '';
				domainTouched = false;
				formError = '';
				framework = 'laravel';
				phpVersion = '8.4';
				rootPath = '';
				selectedDriveRoot = null;
				proxyPortStr = '3000';
				sslEnabled = isLocal;
				edgeExposeEnabled = isEdge;
				edgeSSLEnabled = true;
				addLocalHosts = false;

				void api
					.getFileRoots()
					.then((rootsRes) => {
						fileRoots = rootsRes ?? [];
					})
					.catch(() => {});
				if (supportsDNS) {
					void refreshZonesQuiet();
				}
				void tick().then(() => {
					const targetId =
						isEdge || (isVPS && domainMode === 'subdomain') ? 'site-sub-prefix' : 'site-domain';
					document.getElementById(targetId)?.focus();
				});
			});
		} else if (!isOpen) {
			prevOpen = false;
		}
	});

	$effect(() => {
		if (domainChoices.length > 0 && (!parentDomain || !domainChoices.includes(parentDomain))) {
			parentDomain = domainChoices[0];
		}
	});

	$effect(() => {
		const drv = selectedDriveRoot;
		if (!drv) return;
		rootPath = effectiveDomain ? `${drv}/${effectiveDomain}` : drv;
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		domainTouched = true;
		formError = '';

		if (isEdge || (isVPS && domainMode === 'subdomain')) {
			if (!parentDomain) {
				formError = isEdge
					? 'Tambahkan domain/zone di menu PowerDNS & Edge terlebih dahulu.'
					: 'Pilih domain induk terlebih dahulu atau gunakan mode Domain Utama.';
				document.getElementById('site-parent-domain')?.focus();
				return;
			}
			if (!isEdge && !cleanSubPrefix) {
				formError = 'Awalan subdomain wajib diisi pada mode Subdomain.';
				document.getElementById('site-sub-prefix')?.focus();
				return;
			}
			if (
				cleanSubPrefix &&
				!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*$/.test(cleanSubPrefix)
			) {
				formError = 'Nama subdomain hanya boleh berisi huruf, angka, tanda hubung (-), atau titik.';
				document.getElementById('site-sub-prefix')?.focus();
				return;
			}
		}

		const cleanDomain = effectiveDomain;
		if (!cleanDomain) {
			formError = 'Nama domain wajib diisi.';
			document.getElementById('site-domain')?.focus();
			return;
		}

		const proxyPort = parseInt(proxyPortStr, 10) || 0;
		if (isProxy && (proxyPort < 1 || proxyPort > 65535)) {
			formError = 'Port aplikasi proxy harus antara 1 sampai 65535.';
			document.getElementById('site-proxy-port')?.focus();
			return;
		}

		const cleanIsLocal = isLocalOnlyDomain(cleanDomain);
		const wantVPSLetsEncrypt = isVPS && sslEnabled && !cleanIsLocal;
		const wantEdgeTunnel = isEdge && edgeExposeEnabled;

		creating = true;
		try {
			const res = await api.createSite({
				domain: cleanDomain,
				framework,
				php_version: isProxy || isStatic ? '' : phpVersion,
				root_path: rootPath.trim(),
				proxy_port: proxyPort,
				ssl_enabled: wantVPSLetsEncrypt || wantEdgeTunnel ? false : sslEnabled,
				...(isEdge ? { add_hosts: addLocalHosts } : {}),
				...(wantEdgeTunnel
					? {
							expose_edge: true,
							edge_domain: cleanDomain,
							edge_ssl: edgeSSLEnabled
						}
					: {})
			});

			if (res?.edge_error) {
				toast.warning(
					`Virtual host ${cleanDomain} berhasil dibuat, namun eksposur VPS Edge gagal: ${res.edge_error}`
				);
			} else if (res?.edge_exposed) {
				toast.success(
					`Virtual host ${cleanDomain} dibuat & terekspos ke ${res.edge_domain || cleanDomain}${res.dns_synced ? ' (DNS A & Proxy VPS aktif)' : ''}.`
				);
			} else if (res?.dns_synced && res?.dns_message) {
				toast.success(`Virtual host ${cleanDomain} berhasil dibuat (${res.dns_message}).`);
			} else {
				toast.success(`Virtual host ${cleanDomain} berhasil dibuat!`);
			}

			open = false;
			await onCreated({ domain: cleanDomain, wantVPSLetsEncrypt });
		} catch (err) {
			formError = err instanceof Error ? err.message : 'Gagal membuat virtual host';
		} finally {
			creating = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="flex max-h-[92dvh] max-w-[calc(100vw-1.5rem)] flex-col gap-4 sm:max-w-[min(94vw,64rem)]">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2 pr-6">
				<GlobeIcon class="size-4 shrink-0 text-primary" />
				<span class="truncate">Buat Virtual Host Baru</span>
			</Dialog.Title>
			<Dialog.Description class="break-words font-mono text-xs">{metaSubtitle}</Dialog.Description>
		</Dialog.Header>

		<form onsubmit={handleSubmit} class="flex min-h-0 flex-1 flex-col gap-4">
			<div class="min-h-0 flex-1 overflow-y-auto overscroll-contain pr-1">
				<div class="flex flex-col gap-5">
					{#if formError}
						<Alert.Root variant="destructive" role="alert">
							<AlertCircleIcon />
							<Alert.Title>Pembuatan Virtual Host Gagal</Alert.Title>
							<Alert.Description>{formError}</Alert.Description>
						</Alert.Root>
					{/if}

					<div class="flex flex-wrap items-center gap-2">
						<span class="text-xs font-medium text-muted-foreground">Preset Cepat:</span>
						<Button
							type="button"
							variant={activePreset === 'laravel' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('laravel')}
						>
							Laravel (PHP 8.4)
						</Button>
						<Button
							type="button"
							variant={activePreset === 'wordpress' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('wordpress')}
						>
							WordPress
						</Button>
						<Button
							type="button"
							variant={activePreset === 'proxy' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('proxy')}
						>
							Reverse Proxy
						</Button>
						<Button
							type="button"
							variant={activePreset === 'static' ? 'default' : 'outline'}
							size="xs"
							onclick={() => applyPreset('static')}
						>
							Static HTML
						</Button>
						{#if activePreset === 'custom'}
							<Badge variant="outline">Custom</Badge>
						{/if}
					</div>

					<Field.FieldSet>
						<Field.FieldLegend variant="label">Domain &amp; Identitas Host</Field.FieldLegend>
						<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
							<Field.Field class="sm:col-span-2" data-invalid={domainInvalid ? 'true' : undefined}>
								<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
									<Field.FieldLabel
										for={isEdge || (isVPS && domainMode === 'subdomain') ? 'site-sub-prefix' : 'site-domain'}
									>
										{isLocal ? 'Domain Lokal' : isEdge ? 'Domain PowerDNS' : 'Domain'}
									</Field.FieldLabel>
									{#if isEdge}
										<Badge variant="outline" class="w-fit text-[11px] font-medium">
											Zone PowerDNS ({domainChoices.length})
										</Badge>
									{:else if isVPS}
										<ToggleGroup.Root
											type="single"
											bind:value={() => domainMode, (v) => switchDomainMode(v)}
											variant="outline"
											spacing={2}
											class="grid w-full grid-cols-2 sm:inline-flex sm:w-auto"
											aria-label="Mode input domain"
										>
											<ToggleGroup.Item value="custom" class="min-h-10 text-xs sm:min-h-8 sm:px-3">
												Domain Utama
											</ToggleGroup.Item>
											<ToggleGroup.Item value="subdomain" class="min-h-10 text-xs sm:min-h-8 sm:px-3">
												Subdomain ({domainChoices.length})
											</ToggleGroup.Item>
										</ToggleGroup.Root>
									{/if}
								</div>

								{#if isEdge || (isVPS && domainMode === 'subdomain')}
									{#if domainChoices.length === 0}
										<div class="flex flex-col gap-2 rounded-md border border-dashed bg-muted/40 px-3 py-3 text-xs text-muted-foreground">
											{#if isEdge}
												<p>
													Belum ada zone di PowerDNS VPS Edge. Tambahkan domain zone di menu
													<strong>PowerDNS &amp; Edge</strong> terlebih dahulu sebelum membuat Virtual Host.
												</p>
												<div>
													<Button size="sm" variant="outline" href="/powerdns">
														<NetworkIcon data-icon="inline-start" />
														Kelola Zone PowerDNS
													</Button>
												</div>
											{:else}
												<p>
													Belum ada domain atau zone PowerDNS yang tersedia sebagai induk subdomain. Tambahkan zone di menu
													PowerDNS terlebih dahulu atau pilih <strong>Domain Utama</strong>.
												</p>
											{/if}
										</div>
									{:else}
										<div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)]">
											<InputGroup.Root class="h-11 min-w-0 sm:h-9">
												<InputGroup.Input
													id="site-sub-prefix"
													name="subdomain_prefix"
													placeholder={isEdge ? 'app / api (kosong = utama)' : 'app / api / admin'}
													class="h-full text-base md:text-sm"
													autocorrect="off"
													autocapitalize="none"
													spellcheck={false}
													enterkeyhint="next"
													aria-invalid={domainInvalid ? 'true' : undefined}
													bind:value={subdomainPrefix}
													onblur={() => (domainTouched = true)}
													oninput={() => {
														if (subdomainPrefix.trim()) domainTouched = false;
														formError = '';
													}}
													required={!isEdge}
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
													formError = '';
												}}
											>
												<Select.Trigger
													id="site-parent-domain"
													class="h-11 w-full text-base sm:h-9 sm:text-sm"
													aria-label="Pilih domain PowerDNS"
												>
													<span class="truncate">{parentDomain || 'Pilih domain PowerDNS'}</span>
												</Select.Trigger>
												<Select.Content>
													<Select.Group>
														{#each domainChoices as pd (pd)}
															<Select.Item value={pd}>{pd}</Select.Item>
														{/each}
													</Select.Group>
												</Select.Content>
											</Select.Root>
										</div>
										<div class="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
											<span class="shrink-0">Domain hasil:</span>
											<code class="min-w-0 break-all rounded bg-muted px-1.5 py-0.5 font-mono text-foreground">
												{effectiveDomain || `<subdomain>.${parentDomain}`}
											</code>
										</div>
									{/if}
								{:else}
									<Input
										id="site-domain"
										name="domain"
										class="h-11 text-base sm:h-9 sm:text-sm"
										placeholder={isLocal ? 'proyekanda.test' : 'domainanda.com'}
										autocorrect="off"
										autocapitalize="none"
										spellcheck={false}
										enterkeyhint="next"
										aria-invalid={domainInvalid ? 'true' : undefined}
										bind:value={domain}
										onblur={() => (domainTouched = true)}
										oninput={() => {
											if (domain.trim()) domainTouched = false;
											formError = '';
										}}
										required
									/>
								{/if}

								<Field.FieldDescription>
									{#if isEdge}
										Pilih zone PowerDNS lalu isi awalan subdomain (atau kosongkan awalan untuk memakai domain utama zone).
									{:else if isVPS && domainMode === 'subdomain'}
										Pilih domain induk dan isi awalan subdomain. Record DNS A otomatis dibuat bila zone dikelola PowerDNS lokal.
									{:else if isVPS}
										Masukkan domain FQDN yang mengarah ke IP publik VPS ini.
									{:else}
										Akhiran <code>.test</code> disarankan untuk pengembangan lokal.
									{/if}
								</Field.FieldDescription>
								{#if domainInvalid}
									<Field.FieldError
										errors={[
											{
												message: isEdge
													? 'Pilih zone PowerDNS terlebih dahulu.'
													: isVPS && domainMode === 'subdomain'
														? 'Awalan subdomain dan domain induk wajib diisi.'
														: 'Domain wajib diisi.'
											}
										]}
									/>
								{/if}
							</Field.Field>
						</Field.FieldGroup>
					</Field.FieldSet>

					<Separator />

					<Field.FieldSet>
						<Field.FieldLegend variant="label">Runtime, Framework &amp; Target Aplikasi</Field.FieldLegend>
						<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
							<Field.Field>
								<Field.FieldLabel for="site-framework">Framework / Pola Aplikasi</Field.FieldLabel>
								<Select.Root type="single" bind:value={framework}>
									<Select.Trigger id="site-framework" class="h-11 w-full text-base sm:h-9 sm:text-sm">
										<span class="truncate">{frameworks.find((f) => f.value === framework)?.label ?? framework}</span>
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
										type="text"
										name="proxy_port"
										class="h-11 font-mono text-base sm:h-9 sm:text-sm"
										inputmode="numeric"
										pattern="[0-9]+"
										enterkeyhint="done"
										placeholder="3000"
										bind:value={proxyPortStr}
										oninput={() => (formError = '')}
										required
									/>
									<Field.FieldDescription>Contoh: 3000 (Node/Vite), 8080 (Go/Docker), atau 3200 (Panel).</Field.FieldDescription>
								</Field.Field>
							{:else if !isStatic}
								<Field.Field>
									<Field.FieldLabel for="site-php-ver">Versi PHP Target</Field.FieldLabel>
									<Select.Root type="single" bind:value={phpVersion}>
										<Select.Trigger id="site-php-ver" class="h-11 w-full text-base sm:h-9 sm:text-sm">
											PHP {phpVersion}
										</Select.Trigger>
										<Select.Content>
											<Select.Group>
												{#each phpVersions as ver (ver)}
													<Select.Item value={ver}>PHP {ver}</Select.Item>
												{/each}
											</Select.Group>
										</Select.Content>
									</Select.Root>
									<Field.FieldDescription>Socket PHP-FPM yang melayani skrip PHP pada domain ini.</Field.FieldDescription>
								</Field.Field>
							{:else}
								<div class="flex flex-col justify-center rounded-lg border bg-muted/25 px-3.5 py-2.5 text-xs">
									<span class="font-medium text-foreground">Mode Static HTML</span>
									<span class="mt-0.5 text-muted-foreground">
										Nginx menyajikan berkas statis (<code>index.html</code>) secara langsung tanpa PHP-FPM.
									</span>
								</div>
							{/if}

							{#if !isProxy}
								<Field.Field class="sm:col-span-2">
									<Field.FieldLabel for="site-root-path">Document Root (Opsional)</Field.FieldLabel>
									<div class="flex flex-col gap-2 sm:flex-row sm:items-center">
										<Input
											id="site-root-path"
											name="root_path"
											class="h-11 min-w-0 flex-1 font-mono text-base sm:h-9 sm:text-xs"
											placeholder={effectiveDomain
												? `/var/www/${effectiveDomain}`
												: isLocal
													? '/var/www/proyekanda.test'
													: '/var/www/domainanda.com'}
											autocorrect="off"
											autocapitalize="none"
											spellcheck={false}
											enterkeyhint="done"
											bind:value={rootPath}
											oninput={() => {
												selectedDriveRoot = null;
											}}
										/>
										<Button
											type="button"
											variant="outline"
											class="min-h-11 w-full shrink-0 sm:min-h-0 sm:h-9 sm:w-auto"
											onclick={() => (folderPickerOpen = true)}
										>
											<FolderOpenIcon data-icon="inline-start" />
											Cari Folder
										</Button>
									</div>
									{#if detectedDrives.length > 0}
										<div class="flex flex-wrap items-center gap-1.5 pt-1">
											<span class="text-xs text-muted-foreground">Drive cepat:</span>
											<Button
												type="button"
												size="xs"
												variant={!rootPath.trim() ? 'secondary' : 'outline'}
												class="h-8 text-xs sm:h-7"
												onclick={() => {
													selectedDriveRoot = null;
													rootPath = '';
												}}
											>
												Default (/var/www)
											</Button>
											{#each detectedDrives as drv (drv.path)}
												{@const active = rootPath.trim() === drv.path || rootPath.trim().startsWith(drv.path + '/')}
												<Button
													type="button"
													size="xs"
													variant={active ? 'secondary' : 'outline'}
													class="h-8 max-w-full text-xs sm:h-7"
													onclick={() => {
														selectedDriveRoot = drv.path;
														rootPath = effectiveDomain ? `${drv.path}/${effectiveDomain}` : drv.path;
													}}
												>
													<HardDriveIcon data-icon="inline-start" />
													<span class="truncate">{drv.name}</span>
												</Button>
											{/each}
										</div>
									{/if}
									<Field.FieldDescription>
										Kosongkan untuk otomatis di <code>/var/www/&lt;domain&gt;</code>, atau klik <strong>Cari Folder</strong>.
										{#if !isStatic}
											Untuk Laravel/CI4/Symfony pilih folder root proyek (<code>/public</code> ditambahkan otomatis).
										{/if}
									</Field.FieldDescription>
								</Field.Field>
							{/if}
						</Field.FieldGroup>
					</Field.FieldSet>

					<Separator />

					<Field.FieldSet>
						<Field.FieldLegend variant="label">SSL, DNS &amp; Eksposur Jaringan</Field.FieldLegend>
						<Field.FieldGroup class="grid gap-3 sm:grid-cols-2">
							{#if isEdge}
								<Field.Field orientation="horizontal" class="sm:col-span-2">
									<Checkbox id="site-edge-expose" bind:checked={edgeExposeEnabled} />
									<Field.FieldLabel for="site-edge-expose" class="font-normal">
										Ekspos ke internet &amp; sinkronkan DNS via VPS Edge Tunnel (NetBird{modeStore.publicIP ? ` → ${modeStore.publicIP}` : ''})
									</Field.FieldLabel>
								</Field.Field>

								{#if edgeExposeEnabled}
									<Field.Field orientation="horizontal">
										<Checkbox id="site-edge-ssl" bind:checked={edgeSSLEnabled} />
										<Field.FieldLabel for="site-edge-ssl" class="font-normal">
											Terbitkan SSL Let's Encrypt (HTTPS) di VPS Edge Gateway
										</Field.FieldLabel>
									</Field.Field>
									<Field.Field orientation="horizontal">
										<Checkbox id="site-add-hosts" bind:checked={addLocalHosts} />
										<Field.FieldLabel for="site-add-hosts" class="font-normal">
											Tambahkan juga ke <code>/etc/hosts</code> lokal (opsional)
										</Field.FieldLabel>
									</Field.Field>
								{:else}
									<Field.Field orientation="horizontal" class="sm:col-span-2">
										<Checkbox id="site-ssl" bind:checked={sslEnabled} />
										<Field.FieldLabel for="site-ssl" class="font-normal">
											Aktifkan Sertifikat SSL Lokal (HTTPS mkcert)
										</Field.FieldLabel>
									</Field.Field>
								{/if}
							{:else if isVPS}
								<Field.Field orientation="horizontal" class="sm:col-span-2">
									<Checkbox id="site-ssl" bind:checked={sslEnabled} />
									<Field.FieldLabel for="site-ssl" class="font-normal">
										Terbitkan SSL Let's Encrypt (HTTPS) setelah virtual host dibuat
									</Field.FieldLabel>
								</Field.Field>
							{:else}
								<Field.Field orientation="horizontal" class="sm:col-span-2">
									<Checkbox id="site-ssl" bind:checked={sslEnabled} />
									<Field.FieldLabel for="site-ssl" class="font-normal">
										Aktifkan Sertifikat SSL Otomatis (HTTPS mkcert)
									</Field.FieldLabel>
								</Field.Field>
							{/if}
						</Field.FieldGroup>
					</Field.FieldSet>
				</div>
			</div>

			<Separator />

			<div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
				<p class="text-xs text-muted-foreground">
					{#if effectiveDomain}
						Konfigurasi Nginx akan diterapkan untuk <code class="rounded bg-muted px-1.5 py-0.5 font-mono text-foreground">{effectiveDomain}</code>.
					{:else}
						Lengkapi nama domain untuk membuat konfigurasi Virtual Host.
					{/if}
				</p>
				<div class="flex w-full flex-col-reverse gap-2 sm:w-auto sm:flex-row sm:items-center">
					<Button
						type="button"
						variant="outline"
						class="min-h-10 sm:min-h-0"
						onclick={() => (open = false)}
					>
						Batal
					</Button>
					<Button
						type="submit"
						class="min-h-10 sm:min-h-0"
						disabled={creating || (isEdge && domainChoices.length === 0)}
					>
						{#if creating}
							<Spinner data-icon="inline-start" />
							Membuat...
						{:else}
							<PlusIcon data-icon="inline-start" />
							Buat Virtual Host
						{/if}
					</Button>
				</div>
			</div>
		</form>
	</Dialog.Content>
</Dialog.Root>

<FolderPickerDialog
	bind:open={folderPickerOpen}
	initialPath={rootPath.trim()}
	onSelect={(picked) => {
		selectedDriveRoot = null;
		rootPath = picked;
	}}
/>
