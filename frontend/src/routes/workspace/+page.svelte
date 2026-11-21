<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { WorkspaceStatus } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import QrCodeIcon from '@lucide/svelte/icons/qr-code';
	import PlayIcon from '@lucide/svelte/icons/play';
	import SquareIcon from '@lucide/svelte/icons/square';
	import RotateCwIcon from '@lucide/svelte/icons/rotate-cw';
	import ArrowUpCircleIcon from '@lucide/svelte/icons/arrow-up-circle';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';

	let workspace = $state<WorkspaceStatus | null>(null);
	let loading = $state(true);
	let refreshing = $state(false);
	let orcaActionPending = $state<'start' | 'stop' | 'restart' | null>(null);
	let pairModalOpen = $state(false);

	async function loadWorkspace(forceRefresh = false) {
		if (forceRefresh) refreshing = true;
		else if (!workspace) loading = true;

		try {
			workspace = await api.getWorkspaceStatus(forceRefresh);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memuat status workspace');
		} finally {
			loading = false;
			refreshing = false;
		}
	}

	onMount(() => loadWorkspace(false));

	$effect(() => {
		if (cp.refreshTick > 0) loadWorkspace(false);
	});

	async function controlOrca(action: 'start' | 'stop' | 'restart') {
		orcaActionPending = action;
		try {
			await api.controlService('orca', action);
			toast.success(`Layanan Orca ADE berhasil di-${action}.`);
			await loadWorkspace(false);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal mengontrol layanan Orca');
		} finally {
			orcaActionPending = null;
		}
	}

	async function copyText(val: string, label: string) {
		if (!val || val === '-') return;
		try {
			await navigator.clipboard.writeText(val);
			toast.success(`${label} berhasil disalin ke clipboard.`);
		} catch {
			toast.error(`Gagal menyalin ${label}.`);
		}
	}

	function activeZshThemeKey(theme: string): string {
		if (theme.includes('powerlevel10k')) return 'zsh-p10k';
		if (theme.includes('agnoster')) return 'zsh-agnoster';
		if (theme.includes('robbyrussell')) return 'zsh-robbyrussell';
		return '';
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Workspace, ADE &amp; Shell</h1>
			<p class="text-sm text-muted-foreground">
				Autonomous Development Environment (Orca ADE), akses Web Client terenkripsi, dan Terminal Oh My Zsh.
			</p>
		</div>
		<Button
			variant="outline"
			size="sm"
			class="w-full sm:w-auto"
			disabled={refreshing}
			onclick={() => loadWorkspace(true)}
		>
			{#if refreshing}
				<Spinner data-icon="inline-start" />
				Memuat...
			{:else}
				<RefreshCwIcon data-icon="inline-start" />
				Segarkan
			{/if}
		</Button>
	</div>

	{#if loading || !workspace}
		<div class="grid gap-4 lg:grid-cols-2">
			<Skeleton class="h-80 w-full" />
			<Skeleton class="h-80 w-full" />
		</div>
	{:else}
		{@const orca = workspace.orca}
		{@const shell = workspace.shell}
		{@const isHeadless = (orca.mode || 'headless').toLowerCase() === 'headless'}
		{@const selectedThemeKey = activeZshThemeKey(shell.active_theme || '')}

		<div class="grid gap-4 lg:grid-cols-2">
			<!-- Orca ADE Card -->
			<Card.Root class="flex flex-col justify-between">
				<Card.Header>
					<div class="flex flex-wrap items-start justify-between gap-2">
						<div>
							<Card.Title>Orca ADE</Card.Title>
							<Card.Description>Autonomous Development Environment untuk Linux</Card.Description>
						</div>
						<div class="flex flex-wrap items-center gap-1.5">
							{#if orca.active}
								<StatusBadge
									tone="success"
									label={`Running (${isHeadless ? 'Headless' : 'Desktop'})`}
								/>
							{:else if orca.installed}
								<StatusBadge tone="destructive" label="Stopped" />
							{:else}
								<StatusBadge tone="neutral" label="Belum Terpasang" />
							{/if}
							{#if orca.has_update}
								<StatusBadge tone="warning" label="Update Tersedia" />
							{/if}
						</div>
					</div>
				</Card.Header>

				<Card.Content class="flex flex-col gap-4">
					<p class="text-xs leading-relaxed text-muted-foreground">
						IDE &amp; Agent Workspace otonom mendukung mode Desktop GUI maupun Headless Server 24/7 dengan remote web access terenkripsi.
					</p>

					<div class="flex flex-col gap-2 rounded-md border bg-muted/40 p-3 text-xs">
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Versi Terpasang</span>
							<span class="font-mono font-semibold">
								{orca.version ? `v${orca.version}` : orca.installed ? 'Terpasang' : 'Belum Terpasang'}
							</span>
						</div>
						<Separator />
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Rilis Resmi Terbaru</span>
							<span class="font-mono font-medium">
								{orca.latest_version ? `v${orca.latest_version}` : '-'}
							</span>
						</div>
						<Separator />
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Mode Operasi</span>
							<span class="text-right font-medium">
								{isHeadless ? 'Headless Server 24/7' : 'Desktop GUI'}
							</span>
						</div>
						{#if orca.active}
							<Separator />
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">Port WebSocket</span>
								<span class="font-mono font-semibold text-primary">{orca.port || 6768}</span>
							</div>
							{#if orca.pid}
								<Separator />
								<div class="flex items-center justify-between gap-2">
									<span class="text-muted-foreground">PID Proses</span>
									<span class="font-mono">{orca.pid}</span>
								</div>
							{/if}
						{/if}
					</div>
				</Card.Content>

				<Card.Footer class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
					{#if orca.installed}
						{#if orca.has_update}
							<Button size="sm" class="col-span-2 sm:col-span-1" onclick={() => cp.triggerInstall('orca-update')}>
								<ArrowUpCircleIcon data-icon="inline-start" />
								Update v{orca.latest_version}
							</Button>
						{/if}
						{#if orca.web_client_url}
							<Button
								size="sm"
								class="col-span-2 sm:col-span-1"
								href={orca.web_client_url}
								target="_blank"
								rel="noopener noreferrer"
							>
								<ExternalLinkIcon data-icon="inline-start" />
								Buka Web Client
							</Button>
						{/if}
						<Button variant="outline" size="sm" onclick={() => (pairModalOpen = true)}>
							<QrCodeIcon data-icon="inline-start" />
							Info &amp; Pairing
						</Button>
						{#if orca.active}
							<Button
								variant="outline"
								size="sm"
								disabled={Boolean(orcaActionPending)}
								onclick={() => controlOrca('restart')}
							>
								{#if orcaActionPending === 'restart'}
									<Spinner data-icon="inline-start" />
									Memulai ulang...
								{:else}
									<RotateCwIcon data-icon="inline-start" />
									Restart
								{/if}
							</Button>
							<Button
								variant="destructive"
								size="sm"
								disabled={Boolean(orcaActionPending)}
								onclick={() => controlOrca('stop')}
							>
								{#if orcaActionPending === 'stop'}
									<Spinner data-icon="inline-start" />
									Menghentikan...
								{:else}
									<SquareIcon data-icon="inline-start" />
									Stop
								{/if}
							</Button>
						{:else}
							<Button
								size="sm"
								disabled={Boolean(orcaActionPending)}
								onclick={() => controlOrca('start')}
							>
								{#if orcaActionPending === 'start'}
									<Spinner data-icon="inline-start" />
									Menjalankan...
								{:else}
									<PlayIcon data-icon="inline-start" />
									Mulai Layanan
								{/if}
							</Button>
						{/if}
						<Button
							variant="outline"
							size="sm"
							onclick={() =>
								cp.triggerInstall(isHeadless ? 'orca-switch-desktop' : 'orca-switch-headless')}
						>
							{isHeadless ? 'Mode Desktop' : 'Mode Headless'}
						</Button>
						<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('orca-ai')}>
							Commit AI
						</Button>
						<Button variant="destructive" size="sm" onclick={() => cp.triggerInstall('orca-uninstall')}>
							<Trash2Icon data-icon="inline-start" />
							Uninstall
						</Button>
					{:else}
						<Button size="sm" onclick={() => cp.triggerInstall('orca-headless')}>
							Setup Headless
						</Button>
						<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('orca-desktop')}>
							Setup Desktop
						</Button>
					{/if}
				</Card.Footer>
			</Card.Root>

			<!-- Oh My Zsh & Terminal Shell Card -->
			<Card.Root class="flex flex-col justify-between">
				<Card.Header>
					<div class="flex flex-wrap items-start justify-between gap-2">
						<div>
							<Card.Title>Oh My Zsh &amp; Terminal Shell</Card.Title>
							<Card.Description>CLI Developer Productivity &amp; Prompt Theme</Card.Description>
						</div>
						<StatusBadge
							tone={shell.oh_my_zsh ? 'success' : 'neutral'}
							label={shell.oh_my_zsh ? 'Terpasang' : 'Belum Terpasang'}
						/>
					</div>
				</Card.Header>

				<Card.Content class="flex flex-col gap-4">
					<p class="text-xs leading-relaxed text-muted-foreground">
						Shell interaktif Zsh lengkap dengan konfigurasi komunitas, autosuggestions, syntax highlighting, dan tema prompt modern.
					</p>

					<div class="flex flex-col gap-2 rounded-md border bg-muted/40 p-3 text-xs">
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Shell Default Pengguna</span>
							<span class="truncate font-mono font-semibold">{shell.current_shell || '/bin/bash'}</span>
						</div>
						<Separator />
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Status Oh My Zsh</span>
							<span class="text-right">{shell.oh_my_zsh ? 'Aktif di ~/.oh-my-zsh' : 'Belum Terpasang'}</span>
						</div>
						<Separator />
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Tema Prompt Aktif</span>
							<span class="truncate font-mono font-semibold text-primary">{shell.active_theme || '-'}</span>
						</div>
						<Separator />
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">Nerd Font</span>
							<span class="truncate font-mono font-semibold">
								{shell.nerd_font || 'Belum Terpasang'}
							</span>
						</div>
					</div>

					<Field.FieldGroup>
						<Field.Field>
							<Field.FieldTitle id="zsh-theme-label">Ganti Tema Prompt</Field.FieldTitle>
							<ToggleGroup.Root
								type="single"
								value={selectedThemeKey}
								variant="outline"
								spacing={2}
								class="flex w-full flex-wrap"
								aria-labelledby="zsh-theme-label"
								onValueChange={(val) => {
									if (val) cp.triggerInstall(val);
								}}
							>
								<ToggleGroup.Item value="zsh-p10k" class="min-w-[7rem] flex-1">Powerlevel10k</ToggleGroup.Item>
								<ToggleGroup.Item value="zsh-agnoster" class="min-w-[6rem] flex-1">Agnoster</ToggleGroup.Item>
								<ToggleGroup.Item value="zsh-robbyrussell" class="min-w-[7rem] flex-1">Robbyrussell</ToggleGroup.Item>
							</ToggleGroup.Root>
						</Field.Field>
					</Field.FieldGroup>
				</Card.Content>

				<Card.Footer>
					<Button
						variant={shell.oh_my_zsh ? 'outline' : 'default'}
						size="sm"
						class="w-full sm:w-auto"
						onclick={() => cp.triggerInstall('zsh')}
					>
						<TerminalIcon data-icon="inline-start" />
						{shell.oh_my_zsh ? 'Pasang Ulang / Sinkron Oh My Zsh' : 'Instal Oh My Zsh Lengkap'}
					</Button>
				</Card.Footer>
			</Card.Root>
		</div>
	{/if}
</div>

<!-- Orca Pair & Info Modal -->
{#if workspace}
	{@const orca = workspace.orca}
	{@const fallbackWebUrl = `http://${typeof window !== 'undefined' ? window.location.hostname : 'localhost'}:${orca.port || 6768}`}
	{@const webUrl = orca.web_client_url || fallbackWebUrl}
	<Dialog.Root bind:open={pairModalOpen}>
		<Dialog.Content class="sm:max-w-lg">
			<Dialog.Header>
				<Dialog.Title>Info &amp; Akses Orca ADE</Dialog.Title>
				<Dialog.Description>
					Gunakan URL Web Client atau Pairing URI untuk menghubungkan instance Orca ADE secara terenkripsi.
				</Dialog.Description>
			</Dialog.Header>

			<Field.FieldGroup>
				<Field.Field>
					<Field.FieldLabel for="orca-modal-weburl">Web Client URL (Browser IDE)</Field.FieldLabel>
					<InputGroup.Root>
						<InputGroup.Input id="orca-modal-weburl" value={webUrl} readonly class="font-mono text-xs" />
						<InputGroup.Addon align="inline-end">
							<Button type="button" variant="ghost" size="xs" onclick={() => copyText(webUrl, 'URL Web Client')}>
								<CopyIcon data-icon="inline-start" />
								Salin
							</Button>
						</InputGroup.Addon>
					</InputGroup.Root>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="orca-modal-paircode">Pairing URI / Code (Mobile &amp; Remote App)</Field.FieldLabel>
					<InputGroup.Root>
						<InputGroup.Input
							id="orca-modal-paircode"
							value={orca.pairing_url || '-'}
							readonly
							class="font-mono text-xs"
						/>
						<InputGroup.Addon align="inline-end">
							<Button
								type="button"
								variant="ghost"
								size="xs"
								disabled={!orca.pairing_url}
								onclick={() => copyText(orca.pairing_url || '', 'Pairing URI')}
							>
								<CopyIcon data-icon="inline-start" />
								Salin
							</Button>
						</InputGroup.Addon>
					</InputGroup.Root>
					<Field.FieldDescription>
						Tempelkan URI di atas pada aplikasi Orca Mobile atau Orca Desktop.
					</Field.FieldDescription>
				</Field.Field>
			</Field.FieldGroup>

			<div class="flex flex-col gap-2 rounded-md border bg-muted/40 p-3 text-xs">
				<div class="flex items-center justify-between gap-2">
					<span class="text-muted-foreground">Mode Layanan</span>
					<span class="font-semibold">{orca.mode === 'desktop' ? 'Desktop GUI' : 'Headless Server'}</span>
				</div>
				<Separator />
				<div class="flex items-center justify-between gap-2">
					<span class="text-muted-foreground">Port WebSocket</span>
					<span class="font-mono font-semibold">{orca.port || 6768}</span>
				</div>
				<Separator />
				<div class="flex items-center justify-between gap-2">
					<span class="text-muted-foreground">Status Proses</span>
					<span class="font-semibold">{orca.active ? 'Aktif (Running)' : 'Tidak Aktif (Stopped)'}</span>
				</div>
			</div>

			<Dialog.Footer class="sm:items-center sm:justify-between">
				<Button
					type="button"
					variant="outline"
					size="sm"
					onclick={() => {
						pairModalOpen = false;
						cp.triggerInstall('orca-pair');
					}}
				>
					Tampilkan Terminal QR
				</Button>
				<div class="flex flex-col-reverse gap-2 sm:flex-row sm:items-center">
					{#if orca.web_client_url}
						<Button size="sm" href={orca.web_client_url} target="_blank" rel="noopener noreferrer">
							<ExternalLinkIcon data-icon="inline-start" />
							Buka Web Client
						</Button>
					{/if}
					<Button type="button" variant="outline" size="sm" onclick={() => (pairModalOpen = false)}>
						Tutup
					</Button>
				</div>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>
{/if}
