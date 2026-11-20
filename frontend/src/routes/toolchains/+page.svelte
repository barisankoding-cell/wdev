<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { EnvStatusResponse, ToolchainStatus } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import ArrowUpCircleIcon from '@lucide/svelte/icons/arrow-up-circle';
	import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
	import WrenchIcon from '@lucide/svelte/icons/wrench';
	import FolderCheckIcon from '@lucide/svelte/icons/folder-check';

	let toolchains = $state<ToolchainStatus[]>([]);
	let loading = $state(true);
	let refreshing = $state(false);
	let envStatus = $state<EnvStatusResponse | null>(null);

	async function loadAll(forceRefresh = false) {
		if (forceRefresh) refreshing = true;
		else if (toolchains.length === 0) loading = true;

		try {
			const [tc, env] = await Promise.all([
				api.getToolchains(forceRefresh),
				api.getEnvStatus().catch(() => null)
			]);
			toolchains = tc ?? [];
			if (env) envStatus = env;
			if (forceRefresh) {
				toast.success('Informasi versi resmi toolchain diperbarui.');
			}
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memuat toolchain');
		} finally {
			loading = false;
			refreshing = false;
		}
	}

	onMount(() => loadAll(false));

	$effect(() => {
		if (cp.refreshTick > 0) loadAll(false);
	});
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Toolchains, Compilers &amp; SDKs</h1>
			<p class="text-sm text-muted-foreground">
				Status versi terpasang, deteksi rilis resmi upstream, serta audit konsistensi PATH sistem.
			</p>
		</div>
		<div class="grid grid-cols-1 gap-2 sm:flex sm:flex-wrap sm:items-center">
			<Button variant="outline" size="sm" disabled={refreshing} onclick={() => loadAll(true)}>
				{#if refreshing}
					<Spinner data-icon="inline-start" />
					Memeriksa...
				{:else}
					<RefreshCwIcon data-icon="inline-start" />
					Periksa Pembaruan
				{/if}
			</Button>
			<Button size="sm" onclick={() => cp.triggerInstall('update')}>
				<DownloadIcon data-icon="inline-start" />
				Perbarui Toolchain
			</Button>
		</div>
	</div>

	{#if loading}
		<div class="grid gap-3 sm:gap-4 md:grid-cols-2 xl:grid-cols-3">
			{#each [1, 2, 3, 4, 5, 6] as idx (idx)}
				<Card.Root>
					<Card.Header>
						<Skeleton class="h-5 w-40" />
						<Skeleton class="h-4 w-56" />
					</Card.Header>
					<Card.Content>
						<Skeleton class="h-20 w-full" />
					</Card.Content>
				</Card.Root>
			{/each}
		</div>
	{:else}
		<div class="grid gap-3 sm:gap-4 md:grid-cols-2 xl:grid-cols-3">
			{#each toolchains as item (item.id)}
				<Card.Root class="flex flex-col justify-between">
					<Card.Header>
						<div class="flex flex-wrap items-start justify-between gap-2">
							<div class="min-w-0">
								<Card.Title class="text-base">{item.name}</Card.Title>
								<Card.Description class="mt-1">{item.description}</Card.Description>
							</div>
							{#if !item.installed}
								<StatusBadge tone="neutral" label="Belum Terpasang" />
							{:else if item.has_update}
								<StatusBadge tone="warning" label="Update Tersedia" />
							{:else}
								<StatusBadge tone="success" label="Terpasang" />
							{/if}
						</div>
					</Card.Header>

					<Card.Content>
						<div class="flex flex-col gap-2 rounded-md border bg-muted/40 p-2.5 text-xs">
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">Versi Terpasang</span>
								<span class="truncate font-mono font-semibold">
									{item.installed ? item.installed_version : 'Belum terpasang'}
								</span>
							</div>
							{#if item.extra_info}
								<Separator />
								<div class="flex items-center justify-between gap-2">
									<span class="text-muted-foreground">Detail</span>
									<span class="truncate text-right font-mono text-muted-foreground">{item.extra_info}</span>
								</div>
							{/if}
							<Separator />
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">Rilis Resmi Terbaru</span>
								<span class="truncate font-mono font-medium">{item.latest_version || '-'}</span>
							</div>
						</div>
					</Card.Content>

					<Card.Footer>
						{#if !item.installed}
							<Button size="sm" class="w-full" onclick={() => cp.triggerInstall(item.install_component)}>
								<DownloadIcon data-icon="inline-start" />
								Pasang Sekarang
							</Button>
						{:else if item.has_update}
							<Button size="sm" class="w-full" onclick={() => cp.triggerInstall(item.install_component)}>
								<ArrowUpCircleIcon data-icon="inline-start" />
								Perbarui ke {item.latest_version}
							</Button>
						{:else}
							<Button
								variant="outline"
								size="sm"
								class="w-full"
								onclick={() => cp.triggerInstall(item.install_component)}
							>
								<RefreshCwIcon data-icon="inline-start" />
								Pasang Ulang / Sinkron
							</Button>
						{/if}
					</Card.Footer>
				</Card.Root>
			{/each}
		</div>
	{/if}

	<!-- Environment & PATH Diagnostics -->
	<Card.Root>
		<Card.Header>
			<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<div class="min-w-0">
					<Card.Title class="flex items-center gap-2 text-base">
						<FolderCheckIcon class="size-4 shrink-0 text-primary" />
						<span>Diagnostik Environment &amp; PATH Toolchain</span>
					</Card.Title>
					<Card.Description>
						Sumber tunggal environment di <code>/etc/profile.d/wdev.sh</code> dan validasi symlink kanonik.
					</Card.Description>
				</div>
				<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
					<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('audit')}>
						<ShieldAlertIcon data-icon="inline-start" />
						Audit Duplikat
					</Button>
					<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('audit-fix')}>
						<WrenchIcon data-icon="inline-start" />
						Perbaiki
					</Button>
					<Button size="sm" class="col-span-2 sm:col-span-1" onclick={() => cp.syncToolchainEnv()}>
						<RefreshCwIcon data-icon="inline-start" />
						Sinkronkan PATH
					</Button>
				</div>
			</div>
		</Card.Header>
		{#if envStatus}
			<Card.Content class="flex flex-col gap-4">
				<pre class="max-w-full overflow-x-auto rounded-md border bg-muted/50 p-3 font-mono text-xs leading-relaxed">
{envStatus.report.join('\n')}</pre>
				{#if envStatus.path_entries && envStatus.path_entries.length > 0}
					<div class="flex flex-col gap-1.5">
						<span class="text-xs font-medium text-muted-foreground">Urutan PATH Login Shell Efektif:</span>
						<div class="flex flex-wrap gap-1.5">
							{#each envStatus.path_entries as p (p)}
								<Badge variant="outline" class="max-w-full truncate font-mono text-[11px]">{p}</Badge>
							{/each}
						</div>
					</div>
				{/if}
			</Card.Content>
		{/if}
	</Card.Root>
</div>
