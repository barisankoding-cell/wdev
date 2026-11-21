<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { TunnelStatus } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import CloudUploadIcon from '@lucide/svelte/icons/cloud-upload';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';

	let cfStatus = $state<TunnelStatus | null>(null);
	let localDomain = $state('');
	let publicHost = $state('');
	let profile = $state('');
	let submitting = $state(false);
	let exposureDeleting = $state<Record<string, boolean>>({});

	async function loadCloudflare() {
		try {
			cfStatus = await api.getCFStatus();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memuat status Cloudflare Tunnel');
		}
	}

	onMount(loadCloudflare);

	$effect(() => {
		if (cp.refreshTick > 0) loadCloudflare();
	});

	async function submitExpose(e: SubmitEvent) {
		e.preventDefault();
		if (!localDomain.trim() || !publicHost.trim()) {
			toast.warning('Domain lokal dan domain publik wajib diisi.');
			return;
		}
		submitting = true;
		try {
			await api.exposeCF(localDomain.trim(), publicHost.trim(), profile.trim());
			toast.success(`Eksposur berhasil dibuat untuk ${localDomain.trim()} -> ${publicHost.trim()}`);
			localDomain = '';
			publicHost = '';
			await loadCloudflare();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal mengekspos domain Cloudflare');
		} finally {
			submitting = false;
		}
	}

	async function unexpose(domain: string) {
		const ok = await cp.confirm({
			title: 'Hapus Eksposur Cloudflare',
			description: `Hapus exposure Cloudflare untuk ${domain}?`,
			confirmText: 'Hapus',
			destructive: true
		});
		if (!ok) return;
		exposureDeleting = { ...exposureDeleting, [domain]: true };
		try {
			await api.unexposeCF(domain);
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
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Cloudflare Tunnel &amp; DNS Manager</h1>
			<p class="text-sm text-muted-foreground">
				Kelola profil akun multi-tunnel (<code>wdev cf</code>) dan ekspos vhost lokal ke domain publik Cloudflare.
			</p>
		</div>
		<div class="flex flex-wrap items-center gap-2">
			{#if cfStatus}
				<StatusBadge
					tone={cfStatus.active ? 'success' : 'neutral'}
					label={cfStatus.active ? 'Tunnel Berjalan' : 'Tunnel Nonaktif'}
				/>
			{/if}
			<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('cf-doctor')}>
				<ActivityIcon data-icon="inline-start" />
				Diagnostik Doctor
			</Button>
		</div>
	</div>

	<Card.Root>
		<Card.Header>
			<Card.Title class="text-base">Profil Akun Terdaftar</Card.Title>
			<Card.Description class="break-all">
				{cfStatus?.binary ? `Biner cloudflared: ${cfStatus.binary}` : 'Status profil akun tunnel lokal'}
			</Card.Description>
		</Card.Header>
		<Card.Content>
			{#if !cfStatus?.profiles || cfStatus.profiles.length === 0}
				<p class="text-sm text-muted-foreground">Belum ada profil akun Cloudflare terdaftar.</p>
			{:else}
				<div class="flex flex-wrap gap-2">
					{#each cfStatus.profiles as prof (prof)}
						<Badge variant="secondary">{prof}</Badge>
					{/each}
				</div>
			{/if}
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title class="text-base">Ekspos Site Lokal ke Cloudflare (wdev cf expose)</Card.Title>
			<Card.Description>Petakan domain lokal (mis. myapp.test) ke hostname publik Cloudflare Anda.</Card.Description>
		</Card.Header>
		<Card.Content>
			<form onsubmit={submitExpose}>
				<Field.FieldGroup class="grid gap-4 sm:grid-cols-2 lg:grid-cols-[1fr_1fr_160px_auto] lg:items-end">
					<Field.Field>
						<Field.FieldLabel for="cf-local-domain">Domain Lokal</Field.FieldLabel>
						<Input id="cf-local-domain" placeholder="myapp.test" bind:value={localDomain} required />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="cf-public-host">Domain Publik Cloudflare</Field.FieldLabel>
						<Input id="cf-public-host" placeholder="myapp.example.com" bind:value={publicHost} required />
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="cf-profile">Profil (Opsional)</Field.FieldLabel>
						<Input id="cf-profile" placeholder="default" bind:value={profile} />
					</Field.Field>
					<Button type="submit" disabled={submitting} class="w-full sm:col-span-2 lg:col-span-1 lg:w-auto">
						{#if submitting}
							<Spinner data-icon="inline-start" />
							Memproses...
						{:else}
							<CloudUploadIcon data-icon="inline-start" />
							Ekspos Sekarang
						{/if}
					</Button>
				</Field.FieldGroup>
			</form>
		</Card.Content>
	</Card.Root>

	<section class="flex flex-col gap-3">
		<h2 class="text-lg font-semibold tracking-tight">Daftar Domain Publik Aktif</h2>
		<ResponsiveTableWrapper label="Tabel Domain Publik Cloudflare">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Domain Lokal</Table.Head>
						<Table.Head>Domain Publik</Table.Head>
						<Table.Head>Profil</Table.Head>
						<Table.Head class="text-right">Aksi</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#if !cfStatus?.exposures || cfStatus.exposures.length === 0}
						<Table.Row>
							<Table.Cell colspan={4} class="py-6 text-center text-muted-foreground">
								Belum ada exposure Cloudflare Tunnel.
							</Table.Cell>
						</Table.Row>
					{:else}
						{#each cfStatus.exposures as exp (exp.local_domain)}
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
														onclick={() => unexpose(exp.local_domain)}
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
	</section>
</div>
