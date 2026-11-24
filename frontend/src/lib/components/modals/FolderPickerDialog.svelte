<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { api, ApiError } from '#lib/api/client.js';
	import type { FileEntry, FileRootInfo } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import ArrowUpIcon from '@lucide/svelte/icons/arrow-up';
	import CheckIcon from '@lucide/svelte/icons/check';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import FolderIcon from '@lucide/svelte/icons/folder';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import FolderPlusIcon from '@lucide/svelte/icons/folder-plus';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import HomeIcon from '@lucide/svelte/icons/home';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import SearchIcon from '@lucide/svelte/icons/search';
	import XIcon from '@lucide/svelte/icons/x';

	interface Props {
		open: boolean;
		initialPath?: string;
		title?: string;
		description?: string;
		onSelect: (path: string) => void;
	}

	let {
		open = $bindable(false),
		initialPath = '',
		title = 'Pilih Folder Document Root',
		description = 'Telusuri direktori server yang tersedia atau buat folder baru untuk dijadikan Document Root.',
		onSelect
	}: Props = $props();

	let currentPath = $state('');
	let parentPath = $state('');
	let canGoUp = $state(false);
	let roots = $state<FileRootInfo[]>([]);
	let folders = $state<FileEntry[]>([]);
	let loading = $state(false);
	let loadError = $state('');
	let filterQuery = $state('');

	let creatingOpen = $state(false);
	let creatingFolder = $state(false);
	let newFolderName = $state('');
	let prevOpen = $state(false);

	const quickRoots = $derived.by(() => {
		const nonVhost = roots.filter((r) => r.kind !== 'vhost');
		return nonVhost.length > 0 ? nonVhost : roots;
	});

	const filteredFolders = $derived.by(() => {
		const q = filterQuery.trim().toLowerCase();
		if (!q) return folders;
		return folders.filter((f) => f.name.toLowerCase().includes(q));
	});

	const breadcrumbs = $derived.by(() => {
		if (!currentPath) return [];
		const matchedRoot = roots
			.filter((r) => currentPath === r.path || currentPath.startsWith(r.path + '/'))
			.sort((a, b) => a.path.length - b.path.length)[0];

		const segments: { label: string; path: string; clickable: boolean }[] = [];
		const parts = currentPath.split('/').filter(Boolean);
		let acc = '';
		for (const part of parts) {
			acc += `/${part}`;
			const clickable = !matchedRoot || acc.length >= matchedRoot.path.length;
			segments.push({ label: part, path: acc, clickable });
		}
		return segments;
	});

	async function browse(targetPath = '') {
		loading = true;
		loadError = '';
		filterQuery = '';

		const candidates: string[] = [];
		const trimmed = targetPath.trim().replace(/\/+$/, '');
		if (trimmed.startsWith('/')) {
			let cur = trimmed;
			while (cur.length > 1) {
				candidates.push(cur);
				const idx = cur.lastIndexOf('/');
				if (idx <= 0) break;
				cur = cur.slice(0, idx);
			}
		}
		candidates.push('');

		try {
			let lastErr: unknown = null;
			for (const candidate of candidates) {
				try {
					const res = await api.listFiles(candidate, false);
					currentPath = res.path;
					parentPath = res.parent || '';
					canGoUp = res.can_go_up;
					roots = res.roots ?? [];
					folders = (res.entries ?? []).filter((e) => e.is_dir);
					return;
				} catch (err) {
					lastErr = err;
				}
			}
			loadError =
				lastErr instanceof ApiError || lastErr instanceof Error
					? lastErr.message
					: 'Gagal membaca direktori server.';
		} finally {
			loading = false;
		}
	}

	async function handleCreateFolder() {
		const name = newFolderName.trim().replace(/^\/+|\/+$/g, '');
		if (!name || !currentPath) return;
		if (name.includes('/')) {
			toast.warning('Masukkan nama satu folder tanpa garis miring (/).');
			return;
		}
		const target = `${currentPath === '/' ? '' : currentPath}/${name}`;
		creatingFolder = true;
		try {
			await api.mkdirFile(target);
			toast.success(`Folder ${name} berhasil dibuat.`);
			newFolderName = '';
			creatingOpen = false;
			await browse(target);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal membuat folder baru');
		} finally {
			creatingFolder = false;
		}
	}

	function choosePath(path: string) {
		const clean = path.trim();
		if (!clean) return;
		onSelect(clean);
		open = false;
	}

	$effect(() => {
		if (open && !prevOpen) {
			creatingOpen = false;
			newFolderName = '';
			void browse(initialPath);
		}
		prevOpen = open;
	});
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-3 overflow-y-auto overscroll-contain sm:max-w-xl">
		<Dialog.Header class="pr-8">
			<Dialog.Title class="flex items-center gap-2">
				<FolderOpenIcon class="size-4 shrink-0 text-primary" />
				<span class="leading-snug">{title}</span>
			</Dialog.Title>
			<Dialog.Description>{description}</Dialog.Description>
		</Dialog.Header>

		{#if quickRoots.length > 0}
			<div class="flex items-center gap-1.5 overflow-x-auto overscroll-x-contain pb-1 sm:flex-wrap sm:overflow-visible sm:pb-0">
				<span class="shrink-0 text-xs text-muted-foreground">Lokasi cepat:</span>
				{#each quickRoots as root (root.path)}
					{@const active = currentPath === root.path}
					<Button
						type="button"
						variant={active ? 'secondary' : 'outline'}
						size="xs"
						class="fm-touch-btn h-8 shrink-0 text-xs sm:h-7"
						disabled={loading}
						onclick={() => browse(root.path)}
					>
						{#if root.kind === 'home'}
							<HomeIcon data-icon="inline-start" />
						{:else if root.kind === 'webroot'}
							<GlobeIcon data-icon="inline-start" />
						{:else}
							<HardDriveIcon data-icon="inline-start" />
						{/if}
						<span class="max-w-44 truncate">{root.name}</span>
					</Button>
				{/each}
			</div>
		{/if}

		<!-- Breadcrumb + Actions -->
		<div class="flex flex-col gap-2 rounded-lg border bg-muted/30 px-2.5 py-2 sm:flex-row sm:items-center sm:justify-between">
			<div class="flex min-w-0 flex-1 flex-wrap items-center gap-1 text-xs">
				<span class="font-mono text-muted-foreground">/</span>
				{#each breadcrumbs as seg, idx (seg.path)}
					{#if idx > 0}
						<ChevronRightIcon class="size-3.5 shrink-0 text-muted-foreground" />
					{/if}
					{#if seg.clickable && idx < breadcrumbs.length - 1}
						<button
							type="button"
							class="fm-touch-hit rounded px-1.5 py-1 font-mono font-medium text-primary hover:bg-muted"
							onclick={() => browse(seg.path)}
						>
							{seg.label}
						</button>
					{:else}
						<span
							class="rounded px-1.5 py-1 font-mono {idx === breadcrumbs.length - 1
								? 'bg-background font-semibold text-foreground shadow-xs'
								: 'text-muted-foreground'}"
						>
							{seg.label}
						</span>
					{/if}
				{/each}
			</div>

			<div class="flex shrink-0 items-center justify-end gap-1.5">
				<Button
					type="button"
					variant="outline"
					size="xs"
					class="fm-touch-btn"
					title="Naik ke folder induk"
					aria-label="Naik ke folder induk"
					disabled={loading || !canGoUp || !parentPath}
					onclick={() => parentPath && browse(parentPath)}
				>
					<ArrowUpIcon data-icon="inline-start" />
					<span class="sr-only sm:not-sr-only">Naik</span>
				</Button>
				<Button
					type="button"
					variant={creatingOpen ? 'secondary' : 'outline'}
					size="xs"
					class="fm-touch-btn"
					title="Buat folder baru di lokasi ini"
					aria-label="Buat folder baru di lokasi ini"
					disabled={loading || !currentPath}
					onclick={() => {
						creatingOpen = !creatingOpen;
						newFolderName = '';
					}}
				>
					<FolderPlusIcon data-icon="inline-start" />
					<span class="sr-only sm:not-sr-only">Folder Baru</span>
				</Button>
				<Button
					type="button"
					variant="outline"
					size="xs"
					class="fm-touch-btn"
					title="Muat ulang folder"
					aria-label="Muat ulang folder"
					disabled={loading}
					onclick={() => browse(currentPath)}
				>
					<RefreshCwIcon />
				</Button>
			</div>
		</div>

		{#if creatingOpen}
			<div class="flex items-center gap-2 rounded-lg border border-primary/30 bg-primary/5 p-2">
				<Input
					placeholder={`Nama folder baru di ${currentPath}`}
					autocorrect="off"
					autocapitalize="none"
					spellcheck={false}
					class="h-10 flex-1 text-base sm:h-8 sm:text-xs"
					bind:value={newFolderName}
					onkeydown={(e) => {
						if (e.key === 'Enter') {
							e.preventDefault();
							void handleCreateFolder();
						}
					}}
				/>
				<Button
					type="button"
					size="xs"
					class="fm-touch-btn"
					disabled={creatingFolder || !newFolderName.trim()}
					onclick={handleCreateFolder}
				>
					{#if creatingFolder}
						<Spinner data-icon="inline-start" />
					{/if}
					Buat
				</Button>
				<Button
					type="button"
					variant="ghost"
					size="xs"
					class="fm-touch-btn"
					aria-label="Tutup form folder baru"
					onclick={() => {
						creatingOpen = false;
						newFolderName = '';
					}}
				>
					<XIcon />
				</Button>
			</div>
		{/if}

		<!-- Filter Subfolder -->
		<InputGroup.Root>
			<InputGroup.Addon align="inline-start">
				<SearchIcon />
			</InputGroup.Addon>
			<InputGroup.Input
				type="search"
				placeholder="Saring subfolder di lokasi ini..."
				aria-label="Cari folder"
				bind:value={filterQuery}
			/>
		</InputGroup.Root>

		<!-- Folder List -->
		<div class="min-h-48 max-h-72 overflow-y-auto overscroll-contain rounded-lg border bg-background">
			{#if loading}
				<div class="flex flex-col gap-2 p-3">
					<Skeleton class="h-8 w-full" />
					<Skeleton class="h-8 w-full" />
					<Skeleton class="h-8 w-full" />
					<Skeleton class="h-8 w-4/5" />
				</div>
			{:else if loadError}
				<div class="flex h-full min-h-48 flex-col items-center justify-center gap-2 p-4 text-center text-xs text-destructive">
					<p>{loadError}</p>
					<Button type="button" variant="outline" size="xs" class="fm-touch-btn" onclick={() => browse('')}>
						Kembali ke Root Default
					</Button>
				</div>
			{:else if filteredFolders.length === 0}
				<div class="flex h-full min-h-48 flex-col items-center justify-center gap-1.5 p-4 text-center text-xs text-muted-foreground">
					<FolderIcon class="size-7 opacity-40" />
					<p>
						{folders.length === 0
							? 'Tidak ada subfolder di dalam direktori ini.'
							: 'Tidak ada subfolder yang cocok dengan pencarian.'}
					</p>
					<p class="text-[11px]">Anda dapat langsung memilih folder saat ini atau membuat folder baru.</p>
				</div>
			{:else}
				<ul class="divide-y divide-border/60">
					{#each filteredFolders as dir (dir.path)}
						<li class="flex min-h-11 items-center justify-between gap-2 px-3 py-1.5 transition-colors hover:bg-muted/50">
							<button
								type="button"
								class="flex min-h-10 min-w-0 flex-1 items-center gap-2.5 text-left"
								onclick={() => browse(dir.path)}
							>
								<FolderIcon class="size-4 shrink-0 text-amber-500" />
								<span class="truncate text-sm font-medium">{dir.name}</span>
							</button>
							<Button
								type="button"
								variant="ghost"
								size="xs"
								class="fm-touch-btn shrink-0 text-xs text-primary hover:text-primary"
								onclick={() => choosePath(dir.path)}
							>
								<CheckIcon data-icon="inline-start" />
								Pilih
							</Button>
						</li>
					{/each}
				</ul>
			{/if}
		</div>

		<Dialog.Footer class="flex-col gap-2 pb-[env(safe-area-inset-bottom,0px)] sm:flex-row sm:items-center sm:justify-between sm:pb-0">
			<div class="min-w-0 flex-1 text-left text-xs text-muted-foreground">
				<span>Dipilih:</span>
				<code class="ml-1 inline-block max-w-full truncate rounded bg-muted px-1.5 py-0.5 font-mono text-foreground align-bottom">
					{currentPath || '-'}
				</code>
			</div>
			<div class="flex shrink-0 items-center justify-end gap-2">
				<Button type="button" variant="outline" class="min-h-10 flex-1 sm:min-h-0 sm:flex-initial" onclick={() => (open = false)}>Batal</Button>
				<Button type="button" class="min-h-10 flex-1 sm:min-h-0 sm:flex-initial" disabled={loading || !currentPath} onclick={() => choosePath(currentPath)}>
					<CheckIcon data-icon="inline-start" />
					Pilih Folder Ini
				</Button>
			</div>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
