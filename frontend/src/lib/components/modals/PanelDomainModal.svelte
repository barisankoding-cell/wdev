<script lang="ts">
	import { api, ApiError } from '#lib/api/client.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { Field, FieldDescription, FieldLabel } from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as NativeSelect from '#lib/components/ui/native-select/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { PanelDomainStatus } from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import LockIcon from '@lucide/svelte/icons/lock';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import { toast } from 'svelte-sonner';

	let loading = $state(false);
	let busy = $state(false);
	let errorMsg = $state<string | null>(null);
	let status = $state<PanelDomainStatus | null>(null);

	// Form state
	let domainInputMode = $state<'subdomain' | 'custom'>('subdomain');
	let rootZone = $state('');
	let subPrefix = $state('panel');
	let customDomain = $state('');
	let sslMode = $state<'letsencrypt' | 'edge' | 'local' | 'external' | 'none'>('letsencrypt');
	let sslEmail = $state('');
	let blockDirectIP = $state(true);
	let syncDNS = $state(true);
	let pendingTargetURL = $state('');

	const activeExistingDomain = $derived(status?.domain || status?.detected_proxy_domain || '');

	const resolvedDomain = $derived.by(() => {
		if (domainInputMode === 'subdomain' && rootZone) {
			const cleanPrefix = subPrefix.trim().toLowerCase().replace(/\.+$/, '');
			return cleanPrefix ? `${cleanPrefix}.${rootZone}` : rootZone;
		}
		return customDomain.trim().toLowerCase().replace(/\.+$/, '');
	});

	async function loadStatus() {
		loading = true;
		errorMsg = null;
		try {
			const res = await api.getPanelDomain();
			status = res;
			if (res.available_domains.length > 0) {
				if (!rootZone || !res.available_domains.includes(rootZone)) {
					rootZone = res.available_domains[0];
				}
			} else {
				domainInputMode = 'custom';
			}
			customDomain = res.domain || res.detected_proxy_domain || '';
			if (res.server_mode === 'edge') {
				sslMode = res.edge_configured === false ? 'external' : 'edge';
			} else if (res.server_mode === 'vps') {
				sslMode = 'letsencrypt';
			} else {
				sslMode = 'local';
			}
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : 'Gagal memuat status domain panel';
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		if (authStore.panelDomainOpen) {
			pendingTargetURL = '';
			void loadStatus();
		}
	});

	async function handleBindSubmit(e: SubmitEvent) {
		e.preventDefault();
		const target = resolvedDomain;
		if (!target) {
			errorMsg = 'Nama domain atau subdomain wajib diisi.';
			return;
		}
		busy = true;
		errorMsg = null;
		try {
			const res = await cp.runWithSudoRetry(
				'Mengonfigurasi proxy Nginx & SSL domain panel memerlukan hak akses administrator (sudo).',
				(sudoPass) =>
					api.bindPanelDomain({
						domain: target,
						ssl_mode: sslMode,
						email: sslMode === 'letsencrypt' ? sslEmail.trim() : undefined,
						block_direct_ip: blockDirectIP,
						sync_dns: syncDNS,
						sudo_pass: sudoPass
					})
			);
			if (!res) return;

			if (res.status === 'started') {
				pendingTargetURL = res.target_url || '';
				cp.openTerminal(`Bind Domain Panel HTTPS: ${target}`);
				toast.success('Konfigurasi domain panel dimulai', {
					description: 'Pantau proses SSL & Nginx di Live Terminal. Setelah selesai, akses lewat URL domain baru.'
				});
			} else {
				toast.success(`Domain panel ${target} berhasil disimpan.`);
				await loadStatus();
			}
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : 'Gagal mengikat domain panel';
		} finally {
			busy = false;
		}
	}

	async function toggleBlockIP(nextBlock: boolean) {
		busy = true;
		errorMsg = null;
		try {
			await api.setPanelBlockIP(nextBlock);
			toast.success(nextBlock ? 'Akses langsung via IP sekarang diblokir.' : 'Akses via IP kembali diizinkan.');
			await loadStatus();
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : 'Gagal mengubah kebijakan IP';
		} finally {
			busy = false;
		}
	}

	async function togglePanelSSL(nextSSL: boolean) {
		busy = true;
		errorMsg = null;
		try {
			const hosts = window.location.hostname ? [window.location.hostname] : [];
			const res = await api.setPanelSSL(nextSSL, hosts);
			toast.success(
				nextSSL
					? 'Panel SSL (Force HTTPS) diaktifkan. Koneksi HTTP ke port panel sekarang dialihkan ke HTTPS.'
					: 'Panel SSL (Force HTTPS) dinonaktifkan.'
			);
			if (nextSSL && res.redirect_url && window.location.protocol === 'http:') {
				pendingTargetURL = res.redirect_url;
				window.location.href = res.redirect_url;
				return;
			}
			await loadStatus();
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : 'Gagal mengubah status Panel SSL';
		} finally {
			busy = false;
		}
	}

	async function handleUnbind() {
		const targetName = activeExistingDomain || 'domain panel';
		const ok = await cp.confirm({
			title: 'Hapus Domain Panel & DNS',
			description: `Hapus domain "${targetName}" beserta konfigurasi proxy Nginx port ${status?.panel_port}, tunnel Edge (jika ada), dan seluruh record DNS terkait? Setelah dihapus, Anda dapat mengaktifkan atau mengganti domain panel kembali.`,
			confirmText: 'Hapus Domain & DNS',
			destructive: true
		});
		if (!ok) return;

		busy = true;
		errorMsg = null;
		try {
			const res = await api.unbindPanelDomain(true);
			pendingTargetURL = '';
			const dnsNote =
				res.dns_records_deleted && res.dns_records_deleted > 0
					? ` (${res.dns_records_deleted} record DNS dihapus)`
					: '';
			toast.success(`Domain panel dan DNS terkait berhasil dihapus${dnsNote}. Sekarang Anda dapat mengaktifkan domain baru.`);
			await loadStatus();
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : 'Gagal menghapus domain panel';
		} finally {
			busy = false;
		}
	}

	async function copyText(val: string) {
		try {
			await navigator.clipboard.writeText(val);
			toast.success('URL disalin ke clipboard');
		} catch {
			toast.error('Gagal menyalin URL');
		}
	}

	function formatExpiry(iso?: string): string {
		if (!iso) return '';
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return d.toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' });
	}
</script>

<Dialog.Root
	open={authStore.panelDomainOpen}
	onOpenChange={(open) => {
		if (!open) authStore.closePanelDomain();
	}}
>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 overflow-y-auto sm:max-w-xl">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2">
				<GlobeIcon class="size-4 text-primary" />
				<span>Domain Panel, HTTPS Forward &amp; Blokir IP</span>
			</Dialog.Title>
			<Dialog.Description>
				Ikat panel <code class="font-mono">wdev</code> ke domain/subdomain HTTPS (gaya aaPanel) agar trafik login &amp; terminal terenkripsi dan akses lewat nomor IP langsung diblokir otomatis.
			</Dialog.Description>
		</Dialog.Header>

		{#if errorMsg}
			<Alert.Root variant="destructive" class="py-2.5">
				<AlertCircleIcon class="size-4" />
				<Alert.Description class="text-xs">{errorMsg}</Alert.Description>
			</Alert.Root>
		{/if}

		{#if pendingTargetURL}
			<Alert.Root class="border-emerald-500/40 bg-emerald-500/10 py-3">
				<ShieldCheckIcon class="size-4 text-emerald-600 dark:text-emerald-400" />
				<Alert.Title class="text-sm font-semibold">URL Domain Panel Baru</Alert.Title>
				<Alert.Description class="mt-1 space-y-2 text-xs">
					<p>Setelah proses di Live Terminal selesai, panel ini hanya dapat diakses melalui:</p>
					<div class="flex flex-wrap items-center gap-2">
						<code class="bg-background rounded border px-2 py-1 font-mono text-xs">{pendingTargetURL}</code>
						<Button size="sm" variant="outline" class="h-7 px-2 text-xs" onclick={() => copyText(pendingTargetURL)}>
							<CopyIcon class="size-3.5" />
							Salin
						</Button>
						<Button size="sm" class="h-7 px-2 text-xs" href={pendingTargetURL} target="_blank" rel="noopener">
							<ExternalLinkIcon class="size-3.5" />
							Buka Panel Baru
						</Button>
					</div>
				</Alert.Description>
			</Alert.Root>
		{/if}

		{#if loading && !status}
			<div class="flex items-center justify-center py-6 text-sm">
				<Spinner class="mr-2 size-4" /> Memuat status domain panel...
			</div>
		{:else if status}
			<div class="bg-muted/30 space-y-2.5 rounded-lg border p-3.5 text-xs">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<div class="flex items-center gap-2">
						<LockIcon class="size-4 text-primary" />
						<span class="font-semibold">Force HTTPS Langsung (Panel SSL - Port {status.panel_port})</span>
					</div>
					<Badge variant={status.panel_ssl_enabled ? 'default' : 'secondary'} class="text-[11px]">
						{status.panel_ssl_enabled ? 'HTTPS Wajib (Self-Signed)' : 'HTTP (Non-SSL)'}
					</Badge>
				</div>
				<p class="text-muted-foreground leading-relaxed">
					Enkripsi akses langsung port <code class="font-mono">:{status.panel_port}</code> (termasuk akses via nomor IP) dengan sertifikat ECDSA self-signed dan alihkan otomatis <code class="font-mono">http://</code> &rarr; <code class="font-mono">https://</code> (cocok jika belum memakai domain).
				</p>
				{#if status.panel_ssl_expires_at}
					<p class="text-muted-foreground">
						Masa berlaku sertifikat panel hingga <strong class="text-foreground">{formatExpiry(status.panel_ssl_expires_at)}</strong>
						{#if typeof status.panel_ssl_days_left === 'number'}
							({status.panel_ssl_days_left} hari lagi)
						{/if}
					</p>
				{/if}
				<div class="flex flex-wrap items-center gap-2 pt-0.5">
					<Button
						size="sm"
						variant={status.panel_ssl_enabled ? 'outline' : 'default'}
						class="h-7 text-xs"
						disabled={busy}
						onclick={() => togglePanelSSL(!status?.panel_ssl_enabled)}
					>
						{#if busy}
							<Spinner class="mr-1.5 size-3.5" />
						{/if}
						{status.panel_ssl_enabled ? 'Matikan Force HTTPS Langsung' : 'Aktifkan Force HTTPS (Self-Signed)'}
					</Button>
				</div>
			</div>

			{#if activeExistingDomain}
				{@const isFormalBound = Boolean(status.domain)}
				{@const cardURL = status.domain_url || status.detected_proxy_url || ''}
				{@const cardProvider = status.ssl_provider || status.detected_proxy_ssl_provider || ''}
				{@const cardHasSSL = isFormalBound ? status.ssl_enabled : Boolean(cardProvider || cardURL.startsWith('https://'))}
				<div class="bg-muted/40 space-y-3 rounded-lg border p-3.5 text-xs">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<div class="flex items-center gap-2">
							<ShieldCheckIcon class="size-4 text-emerald-500" />
							<span class="font-semibold">
								{isFormalBound ? `Domain Terikat: ${status.domain}` : `Domain Port Panel (:${status.panel_port}) Terdeteksi: ${status.detected_proxy_domain}`}
							</span>
						</div>
						<div class="flex flex-wrap items-center gap-1.5">
							<Badge variant={cardHasSSL ? 'default' : 'secondary'} class="text-[11px]">
								{cardHasSSL ? `HTTPS (${cardProvider || 'SSL'})` : 'HTTP'}
							</Badge>
							{#if isFormalBound}
								<Badge variant={status.block_direct_ip ? 'destructive' : 'outline'} class="text-[11px]">
									{status.block_direct_ip ? 'IP Langsung Diblokir' : 'IP Diizinkan'}
								</Badge>
							{:else}
								<Badge variant="outline" class="text-[11px]">
									Proxy Port {status.panel_port}
								</Badge>
							{/if}
						</div>
					</div>

					{#if cardURL}
						<div class="flex flex-wrap items-center gap-2">
							<code class="bg-background min-w-0 flex-1 truncate rounded border px-2 py-1 font-mono">{cardURL}</code>
							<Button size="sm" variant="outline" class="h-7 px-2 text-xs" onclick={() => copyText(cardURL)}>
								<CopyIcon class="size-3.5" />
								Salin
							</Button>
							<Button size="sm" class="h-7 px-2 text-xs" href={cardURL} target="_blank" rel="noopener">
								<ExternalLinkIcon class="size-3.5" />
								Buka
							</Button>
						</div>
					{/if}

					{#if status.ssl_expires_at}
						<p class="text-muted-foreground">
							Masa berlaku SSL hingga <strong class="text-foreground">{formatExpiry(status.ssl_expires_at)}</strong>
							{#if typeof status.ssl_days_left === 'number'}
								({status.ssl_days_left} hari tersisa)
							{/if}
						</p>
					{/if}

					<Alert.Root class="border-amber-500/40 bg-amber-500/10 py-2.5">
						<AlertCircleIcon class="size-4 text-amber-600 dark:text-amber-400" />
						<Alert.Description class="text-xs leading-relaxed">
							Port panel <code class="font-mono">:{status.panel_port}</code> sudah terpakai oleh domain <code class="font-mono font-semibold">{activeExistingDomain}</code>. Untuk mencabut, mengganti, atau mengaktifkan ulang domain panel, klik <strong>Hapus Domain &amp; DNS</strong> terlebih dahulu.
						</Alert.Description>
					</Alert.Root>

					<div class="flex flex-wrap items-center justify-between gap-2 pt-1">
						{#if isFormalBound}
							<Button
								type="button"
								variant="outline"
								size="sm"
								class="h-7 text-xs"
								disabled={busy}
								onclick={() => toggleBlockIP(!status!.block_direct_ip)}
							>
								<LockIcon class="size-3.5" />
								{status.block_direct_ip ? 'Izinkan Akses IP Sementara' : 'Blokir Akses via IP'}
							</Button>
						{:else}
							<span></span>
						{/if}
						<Button
							type="button"
							variant="destructive"
							size="sm"
							class="h-7 text-xs"
							disabled={busy}
							onclick={handleUnbind}
						>
							<Trash2Icon class="size-3.5" />
							Hapus Domain &amp; DNS
						</Button>
					</div>
				</div>

				<Dialog.Footer>
					<Button type="button" variant="outline" onclick={() => authStore.closePanelDomain()} disabled={busy}>
						Tutup
					</Button>
				</Dialog.Footer>
			{:else}
			<form onsubmit={handleBindSubmit} class="space-y-4">
				{#if status.available_domains.length > 0}
					<div class="grid grid-cols-2 gap-1.5 rounded-lg border bg-muted/30 p-1">
						<button
							type="button"
							class={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
								domainInputMode === 'subdomain' ? 'bg-background shadow-xs text-foreground' : 'text-muted-foreground hover:text-foreground'
							}`}
							onclick={() => (domainInputMode = 'subdomain')}
						>
							Subdomain dari Domain Saya
						</button>
						<button
							type="button"
							class={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
								domainInputMode === 'custom' ? 'bg-background shadow-xs text-foreground' : 'text-muted-foreground hover:text-foreground'
							}`}
							onclick={() => (domainInputMode = 'custom')}
						>
							Domain / Subdomain Kustom
						</button>
					</div>
				{/if}

				{#if domainInputMode === 'subdomain' && status.available_domains.length > 0}
					<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
						<Field>
							<FieldLabel for="pd-sub-prefix">Prefix Subdomain</FieldLabel>
							<Input
								id="pd-sub-prefix"
								bind:value={subPrefix}
								placeholder="panel"
								class="font-mono text-xs"
								required
							/>
						</Field>
						<Field>
							<FieldLabel>Domain Induk</FieldLabel>
							<NativeSelect.Root bind:value={rootZone}>
								{#each status.available_domains as dom (dom)}
									<NativeSelect.Option value={dom}>{dom}</NativeSelect.Option>
								{/each}
							</NativeSelect.Root>
						</Field>
					</div>
				{:else}
					<Field>
						<FieldLabel for="pd-custom-domain">Domain / Subdomain Panel</FieldLabel>
						<Input
							id="pd-custom-domain"
							bind:value={customDomain}
							placeholder="panel.domainanda.id"
							class="font-mono text-xs"
							required
						/>
						<FieldDescription>
							Gunakan subdomain khusus (mis. <code class="font-mono">panel.domain.id</code>) agar tidak menimpa situs utama.
						</FieldDescription>
					</Field>
				{/if}

				{#if resolvedDomain}
					<div class="bg-muted/30 rounded-md border px-3 py-2 text-xs">
						Target forwarding: <code class="font-mono font-semibold">{sslMode === 'none' ? 'http' : 'https'}://{resolvedDomain}{status.entrance_path}</code>
						→ <code class="font-mono">127.0.0.1:{status.panel_port}</code>
					</div>
				{/if}

				<Field>
					<FieldLabel>Mode SSL &amp; Forwarding</FieldLabel>
					<NativeSelect.Root bind:value={sslMode}>
						{#if status.server_mode === 'edge'}
							<NativeSelect.Option value="edge">
								HTTPS Edge Gateway (Let's Encrypt via VPS Edge + Tunnel NetBird)
							</NativeSelect.Option>
						{:else}
							<NativeSelect.Option value="letsencrypt">
								HTTPS Let's Encrypt (Auto Proxy Nginx + Certbot — VPS Publik)
							</NativeSelect.Option>
						{/if}
						<NativeSelect.Option value="local">
							HTTPS Lokal / Self-Signed (Nginx Proxy HTTPS)
						</NativeSelect.Option>
						<NativeSelect.Option value="external">
							Proxy Eksternal / Cloudflare Tunnel (Tanpa Ubah Nginx Lokal)
						</NativeSelect.Option>
						<NativeSelect.Option value="none">
							HTTP Nginx Reverse Proxy (Tanpa SSL)
						</NativeSelect.Option>
					</NativeSelect.Root>
					{#if status.server_mode === 'edge' && sslMode === 'edge'}
						<FieldDescription>
							{#if status.edge_configured}
								Nginx lokal melayani HTTP di belakang terowongan NetBird, sementara VPS Edge ({status.public_ip || 'Gateway'}) menerbitkan SSL Let's Encrypt dan DNS A record (jika zona dikelola PowerDNS).
							{:else}
								VPS Edge Gateway belum terhubung. Konfigurasikan di menu Server Mode terlebih dahulu atau pilih Proxy Eksternal.
							{/if}
						</FieldDescription>
					{/if}
				</Field>

				{#if sslMode === 'letsencrypt'}
					<Field>
						<FieldLabel for="pd-ssl-email">Email Let's Encrypt (Opsional)</FieldLabel>
						<Input
							id="pd-ssl-email"
							type="email"
							bind:value={sslEmail}
							placeholder="admin@domainanda.id"
							class="text-xs"
						/>
					</Field>
				{/if}

				<div class="space-y-2 text-xs">
					<label class="flex items-center gap-2">
						<Checkbox bind:checked={blockDirectIP} />
						<span>
							<strong>Blokir akses langsung memakai nomor IP</strong> setelah domain aktif (hanya bisa diakses lewat domain)
						</span>
					</label>
					{#if status.server_mode === 'vps' && sslMode !== 'external'}
						<label class="flex items-center gap-2">
							<Checkbox bind:checked={syncDNS} />
							<span>Sinkronkan record DNS <code class="font-mono">A</code> otomatis di PowerDNS ({status.public_ip || 'IP VPS'})</span>
						</label>
					{/if}
				</div>

				<p class="text-muted-foreground text-[11px]">
					Pemulihan darurat bila DNS bermasalah: jalankan <code class="font-mono">sudo wdev panel domain off</code> melalui SSH.
				</p>

				<Dialog.Footer>
					<Button type="button" variant="outline" onclick={() => authStore.closePanelDomain()} disabled={busy}>
						Tutup
					</Button>
					<Button type="submit" disabled={busy || !resolvedDomain}>
						{#if busy}
							<Spinner class="size-4" />
							Menyiapkan...
						{:else}
							Ikat Domain &amp; Terapkan
						{/if}
					</Button>
				</Dialog.Footer>
			</form>
			{/if}
		{/if}
	</Dialog.Content>
</Dialog.Root>
