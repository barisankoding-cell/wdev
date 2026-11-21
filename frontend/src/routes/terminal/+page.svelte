<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api/client.js';
	import PtyTerminalPane, {
		type TerminalPaneMeta,
		type TerminalPaneStatus
	} from '#lib/components/terminal/PtyTerminalPane.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Card, CardContent, CardHeader } from '#lib/components/ui/card/index.js';
	import * as NativeSelect from '#lib/components/ui/native-select/index.js';
	import type { TerminalInfo } from '#lib/types/api.js';
	import ClipboardIcon from '@lucide/svelte/icons/clipboard';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import EraserIcon from '@lucide/svelte/icons/eraser';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import Maximize2Icon from '@lucide/svelte/icons/maximize-2';
	import Minimize2Icon from '@lucide/svelte/icons/minimize-2';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw';
	import SquareTerminalIcon from '@lucide/svelte/icons/square-terminal';
	import XIcon from '@lucide/svelte/icons/x';
	import { onMount } from 'svelte';

	interface TerminalTab {
		id: number;
		title: string;
		cwd: string;
		shell: string;
		meta: TerminalPaneMeta;
	}

	let info = $state<TerminalInfo | null>(null);
	let infoError = $state<string | null>(null);
	let selectedShell = $state('');
	let fontSize = $state(14);
	let expanded = $state(false);

	let nextTabSeq = 1;
	let tabs = $state<TerminalTab[]>([]);
	let activeTabId = $state<number | null>(null);
	let paneRefs = $state<Record<number, PtyTerminalPane | undefined>>({});
	let lastHandledQueryKey: string | null = null;

	const activeTab = $derived(tabs.find((t) => t.id === activeTabId) ?? null);

	function shortDirLabel(dir: string): string {
		const clean = dir.trim();
		if (!clean) return 'Shell';
		if (clean === '/') return '/';
		const parts = clean.split('/').filter(Boolean);
		return parts[parts.length - 1] || clean;
	}

	function createTab(cwd = '', shell = '') {
		const id = nextTabSeq++;
		const cleanCwd = cwd.trim();
		const targetShell = shell.trim() || selectedShell;
		const title = cleanCwd ? `${shortDirLabel(cleanCwd)}` : `Shell ${id}`;
		const newTab: TerminalTab = {
			id,
			title,
			cwd: cleanCwd,
			shell: targetShell,
			meta: {
				status: 'connecting',
				cwd: cleanCwd,
				shell: targetShell
			}
		};
		tabs = [...tabs, newTab];
		activeTabId = id;
	}

	function closeTab(id: number) {
		const idx = tabs.findIndex((t) => t.id === id);
		if (idx < 0) return;
		const remaining = tabs.filter((t) => t.id !== id);
		tabs = remaining;
		if (activeTabId === id) {
			if (remaining.length > 0) {
				const fallbackIdx = Math.min(idx, remaining.length - 1);
				activeTabId = remaining[fallbackIdx].id;
			} else {
				createTab(info?.home_dir ?? '', selectedShell);
			}
		}
	}

	function handleTabMeta(id: number, meta: TerminalPaneMeta) {
		tabs = tabs.map((t) => {
			if (t.id !== id) return t;
			const nextTitle = meta.cwd ? shortDirLabel(meta.cwd) : t.title;
			return {
				...t,
				title: nextTitle,
				meta
			};
		});
	}

	function statusDotClass(status: TerminalPaneStatus): string {
		switch (status) {
			case 'connected':
				return 'bg-emerald-500';
			case 'connecting':
				return 'bg-amber-500 animate-pulse';
			case 'error':
				return 'bg-destructive';
			default:
				return 'bg-muted-foreground/50';
		}
	}

	function statusLabel(meta: TerminalPaneMeta): string {
		switch (meta.status) {
			case 'connected':
				return 'Terhubung (PTY)';
			case 'connecting':
				return 'Menghubungkan...';
			case 'exited':
				return typeof meta.exitCode === 'number'
					? `Selesai (exit ${meta.exitCode})`
					: 'Sesi ditutup';
			case 'error':
				return meta.error || 'Terputus';
		}
	}

	onMount(async () => {
		try {
			const res = await api.getTerminalInfo();
			info = res;
			if (!selectedShell && res.shell) {
				selectedShell = res.shell;
			}
		} catch (err) {
			infoError = err instanceof ApiError || err instanceof Error ? err.message : 'Gagal memuat info terminal';
		}
	});

	$effect(() => {
		const cwdParam = page.url.searchParams.get('cwd') ?? '';
		const shellParam = page.url.searchParams.get('shell') ?? '';
		const key = `${cwdParam}::${shellParam}`;

		if (lastHandledQueryKey === key && tabs.length > 0) return;
		lastHandledQueryKey = key;

		if (tabs.length === 0) {
			createTab(cwdParam, shellParam);
			return;
		}
		if (cwdParam) {
			createTab(cwdParam, shellParam);
		}
	});

	const activePane = $derived(activeTab ? paneRefs[activeTab.id] : undefined);
	const currentFileMgrPath = $derived(activeTab?.meta.cwd || activeTab?.cwd || info?.home_dir || '');
</script>

<svelte:head>
	<title>Terminal SSH (PTY) - wdev Control Plane</title>
</svelte:head>

<div class="space-y-4">
	<!-- Header -->
	<div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
		<div>
			<div class="flex flex-wrap items-center gap-2">
				<h1 class="text-2xl font-bold tracking-tight">Terminal SSH (PTY)</h1>
				{#if info}
					<Badge variant="outline" class="font-mono text-xs">
						{info.user}@{info.hostname}
					</Badge>
					<Badge variant="secondary" class="text-xs">
						{info.os_name}
					</Badge>
				{/if}
			</div>
			<p class="text-muted-foreground text-sm">
				Sesi terminal shell Linux interaktif penuh (vim, nano, htop, tab completion, warna ANSI &amp; resize otomatis).
			</p>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			{#if info && info.available_shells.length > 0}
				<div class="w-40">
					<NativeSelect.Root bind:value={selectedShell} aria-label="Pilih shell default untuk tab baru">
						{#each info.available_shells as sh (sh)}
							<NativeSelect.Option value={sh}>{sh}</NativeSelect.Option>
						{/each}
					</NativeSelect.Root>
				</div>
			{/if}

			<Button
				variant="outline"
				size="sm"
				href={currentFileMgrPath ? `/files?path=${encodeURIComponent(currentFileMgrPath)}` : '/files'}
			>
				<FolderOpenIcon class="size-4" />
				File Manager
			</Button>

			<Button
				size="sm"
				onclick={() => createTab(activeTab?.meta.cwd || info?.home_dir || '', selectedShell)}
			>
				<PlusIcon class="size-4" />
				Tab Shell Baru
			</Button>
		</div>
	</div>

	{#if infoError}
		<p class="text-destructive text-xs">{infoError}</p>
	{/if}

	<!-- Main Terminal Card -->
	<Card class="gap-0 overflow-hidden py-0">
		<CardHeader class="bg-muted/30 space-y-2 border-b px-3 py-2.5">
			<!-- Tabs Row -->
			<div class="flex flex-wrap items-center justify-between gap-2">
				<div class="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
					{#each tabs as tab (tab.id)}
						{@const isActive = tab.id === activeTabId}
						<div
							class={`inline-flex max-w-56 items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs transition-colors ${
								isActive
									? 'bg-background text-foreground border-primary/40 font-medium shadow-xs'
									: 'bg-muted/40 text-muted-foreground hover:bg-muted border-transparent'
							}`}
						>
							<button
								type="button"
								class="flex min-w-0 items-center gap-1.5"
								onclick={() => (activeTabId = tab.id)}
							>
								<span class={`size-2 shrink-0 rounded-full ${statusDotClass(tab.meta.status)}`}></span>
								<SquareTerminalIcon class="size-3.5 shrink-0" />
								<span class="truncate font-mono">{tab.title}</span>
							</button>
							<button
								type="button"
								class="text-muted-foreground hover:text-foreground ml-1 rounded p-0.5"
								title="Tutup tab"
								onclick={() => closeTab(tab.id)}
							>
								<XIcon class="size-3" />
							</button>
						</div>
					{/each}

					<Button
						variant="ghost"
						size="icon-sm"
						class="h-7 w-7"
						title="Tambah tab terminal"
						onclick={() => createTab(activeTab?.meta.cwd || info?.home_dir || '', selectedShell)}
					>
						<PlusIcon class="size-3.5" />
					</Button>
				</div>

				<!-- Session Status & Font Controls -->
				<div class="flex flex-wrap items-center gap-1.5">
					{#if activeTab}
						<Badge variant="outline" class="gap-1.5 font-mono text-[11px]">
							<span class={`size-1.5 rounded-full ${statusDotClass(activeTab.meta.status)}`}></span>
							{statusLabel(activeTab.meta)}
						</Badge>
					{/if}

					<div class="flex items-center rounded-md border">
						<button
							type="button"
							class="hover:bg-muted px-2 py-1 text-xs font-medium"
							title="Perkecil font"
							onclick={() => (fontSize = Math.max(11, fontSize - 1))}
						>
							A-
						</button>
						<span class="text-muted-foreground border-x px-1.5 font-mono text-[11px]">{fontSize}px</span>
						<button
							type="button"
							class="hover:bg-muted px-2 py-1 text-xs font-medium"
							title="Perbesar font"
							onclick={() => (fontSize = Math.min(22, fontSize + 1))}
						>
							A+
						</button>
					</div>

					<Button
						variant="outline"
						size="icon-sm"
						class="h-7 w-7"
						title={expanded ? 'Ukuran normal' : 'Perbesar area terminal'}
						onclick={() => (expanded = !expanded)}
					>
						{#if expanded}
							<Minimize2Icon class="size-3.5" />
						{:else}
							<Maximize2Icon class="size-3.5" />
						{/if}
					</Button>
				</div>
			</div>

			<!-- Quick Control Keys & Actions -->
			{#if activeTab}
				<div class="flex flex-wrap items-center justify-between gap-2 text-xs">
					<div class="text-muted-foreground min-w-0 truncate font-mono text-[11px]">
						{#if activeTab.meta.cwd}
							CWD: <span class="text-foreground">{activeTab.meta.cwd}</span>
						{/if}
						{#if activeTab.meta.shell}
							<span class="mx-1.5">•</span> Shell: <span class="text-foreground">{activeTab.meta.shell}</span>
						{/if}
					</div>

					<div class="flex flex-wrap items-center gap-1">
						<Button
							variant="ghost"
							size="sm"
							class="h-6 px-2 font-mono text-[11px]"
							title="Kirim Ctrl+C (SIGINT)"
							onclick={() => activePane?.sendText('\x03')}
						>
							Ctrl+C
						</Button>
						<Button
							variant="ghost"
							size="sm"
							class="h-6 px-2 font-mono text-[11px]"
							title="Kirim Tab"
							onclick={() => activePane?.sendText('\t')}
						>
							Tab
						</Button>
						<Button
							variant="ghost"
							size="sm"
							class="h-6 px-2 font-mono text-[11px]"
							title="Kirim Esc"
							onclick={() => activePane?.sendText('\x1b')}
						>
							Esc
						</Button>
						<Button
							variant="outline"
							size="sm"
							class="h-6 gap-1 px-2 text-[11px]"
							onclick={() => activePane?.copySelection()}
						>
							<CopyIcon class="size-3" />
							Copy
						</Button>
						<Button
							variant="outline"
							size="sm"
							class="h-6 gap-1 px-2 text-[11px]"
							onclick={() => activePane?.pasteClipboard()}
						>
							<ClipboardIcon class="size-3" />
							Paste
						</Button>
						<Button
							variant="outline"
							size="sm"
							class="h-6 gap-1 px-2 text-[11px]"
							onclick={() => activePane?.clear()}
						>
							<EraserIcon class="size-3" />
							Clear
						</Button>
						<Button
							variant="outline"
							size="sm"
							class="h-6 gap-1 px-2 text-[11px]"
							onclick={() => activePane?.reconnect()}
						>
							<RotateCcwIcon class="size-3" />
							Reconnect
						</Button>
					</div>
				</div>
			{/if}
		</CardHeader>

		<CardContent class="p-0">
			<div class={expanded ? 'h-[calc(100dvh-10rem)] min-h-[520px]' : 'h-[calc(100dvh-17rem)] min-h-[440px]'}>
				{#each tabs as tab (tab.id)}
					<div class="h-full w-full" class:hidden={tab.id !== activeTabId}>
						<PtyTerminalPane
							bind:this={paneRefs[tab.id]}
							cwd={tab.cwd}
							shell={tab.shell}
							{fontSize}
							active={tab.id === activeTabId}
							onMeta={(meta) => handleTabMeta(tab.id, meta)}
						/>
					</div>
				{/each}
			</div>
		</CardContent>
	</Card>
</div>
