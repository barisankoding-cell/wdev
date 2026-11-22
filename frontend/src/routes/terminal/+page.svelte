<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api/client.js';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PaginationBar from '#lib/components/shared/PaginationBar.svelte';
	import PtyTerminalPane, {
		type TerminalPaneMeta,
		type TerminalPaneStatus
	} from '#lib/components/terminal/PtyTerminalPane.svelte';
	import { Alert, AlertDescription, AlertTitle } from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Card, CardContent, CardHeader } from '#lib/components/ui/card/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogHeader,
		DialogTitle
	} from '#lib/components/ui/dialog/index.js';
	import { Field, FieldDescription, FieldLabel } from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as NativeSelect from '#lib/components/ui/native-select/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import {
		Table,
		TableBody,
		TableCell,
		TableHead,
		TableHeader,
		TableRow
	} from '#lib/components/ui/table/index.js';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import type { TerminalAuditRecord, TerminalInfo, TerminalTargetUser } from '#lib/types/api.js';
	import ClipboardIcon from '@lucide/svelte/icons/clipboard';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import EraserIcon from '@lucide/svelte/icons/eraser';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import HistoryIcon from '@lucide/svelte/icons/history';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import LockIcon from '@lucide/svelte/icons/lock';
	import Maximize2Icon from '@lucide/svelte/icons/maximize-2';
	import Minimize2Icon from '@lucide/svelte/icons/minimize-2';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw';
	import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import SquareTerminalIcon from '@lucide/svelte/icons/square-terminal';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import XIcon from '@lucide/svelte/icons/x';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	interface TerminalTab {
		id: number;
		title: string;
		cwd: string;
		shell: string;
		user: string;
		isRoot: boolean;
		locked: boolean;
		token: string;
		meta: TerminalPaneMeta;
	}

	let info = $state<TerminalInfo | null>(null);
	let infoLoaded = $state(false);
	let infoError = $state<string | null>(null);
	let selectedShell = $state('');
	let selectedUser = $state('');
	let fontSize = $state(14);
	let expanded = $state(false);

	let nextTabSeq = 1;
	let tabs = $state<TerminalTab[]>([]);
	let activeTabId = $state<number | null>(null);
	let paneRefs = $state<Record<number, PtyTerminalPane | undefined>>({});
	let lastHandledQueryKey: string | null = null;

	// Step-up password unlock state
	let unlockPassword = $state('');
	let unlockRemember = $state(true);
	let unlockBusy = $state(false);
	let unlockError = $state<string | null>(null);

	// Audit history modal
	let auditOpen = $state(false);
	let auditLoading = $state(false);
	let auditRecords = $state<TerminalAuditRecord[]>([]);
	const AUDIT_PAGE_SIZE = 10;
	let auditPage = $state(1);
	const paginatedAuditRecords = $derived(
		auditRecords.slice((auditPage - 1) * AUDIT_PAGE_SIZE, auditPage * AUDIT_PAGE_SIZE)
	);

	const activeTab = $derived(tabs.find((t) => t.id === activeTabId) ?? null);
	const activePane = $derived(activeTab && !activeTab.locked ? paneRefs[activeTab.id] : undefined);
	const currentFileMgrPath = $derived(activeTab?.meta.cwd || activeTab?.cwd || info?.home_dir || '');
	const idleMinutes = $derived(
		info?.idle_timeout_seconds ? Math.max(1, Math.round(info.idle_timeout_seconds / 60)) : 15
	);

	const showNonTLSWarning = $derived.by(() => {
		if (!info || !info.is_daemon_root || info.is_https || typeof window === 'undefined') return false;
		const h = window.location.hostname.toLowerCase();
		return h !== 'localhost' && h !== '127.0.0.1' && h !== '::1';
	});

	function resolveTargetUserObj(username: string): TerminalTargetUser | null {
		if (!info || info.available_users.length === 0) return null;
		return (
			info.available_users.find((u) => u.username === username) ??
			info.available_users.find((u) => u.username === info?.default_user) ??
			info.available_users[0]
		);
	}

	function doesUserNeedUnlock(u: TerminalTargetUser | null): boolean {
		if (!info) return true;
		if (info.step_up_always_required) return !info.root_unlocked;
		if (u?.is_root) return !info.root_unlocked;
		return false;
	}

	function shortDirLabel(dir: string): string {
		const clean = dir.trim();
		if (!clean) return 'Shell';
		if (clean === '/') return '/';
		const parts = clean.split('/').filter(Boolean);
		return parts[parts.length - 1] || clean;
	}

	function createTab(cwd = '', shell = '', requestedUser = '') {
		const id = nextTabSeq++;
		const userObj = resolveTargetUserObj(requestedUser || selectedUser);
		const targetUsername = userObj?.username || requestedUser || selectedUser || '';
		const isRoot = Boolean(userObj?.is_root);
		const locked = doesUserNeedUnlock(userObj);

		const cleanCwd = cwd.trim();
		const targetShell = shell.trim() || userObj?.shell || selectedShell;
		const prefix = isRoot ? 'root' : targetUsername || 'shell';
		const title = cleanCwd ? `${prefix}:${shortDirLabel(cleanCwd)}` : `${prefix}#${id}`;

		const newTab: TerminalTab = {
			id,
			title,
			cwd: cleanCwd,
			shell: targetShell,
			user: targetUsername,
			isRoot,
			locked,
			token: '',
			meta: {
				status: locked ? 'error' : 'connecting',
				cwd: cleanCwd,
				shell: targetShell,
				user: targetUsername,
				isRoot
			}
		};
		tabs = [...tabs, newTab];
		activeTabId = id;
		if (locked) {
			unlockPassword = '';
			unlockError = null;
		}
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
				createTab(info?.home_dir ?? '', selectedShell, selectedUser);
			}
		}
	}

	function handleTabMeta(id: number, meta: TerminalPaneMeta) {
		tabs = tabs.map((t) => {
			if (t.id !== id) return t;
			const who = meta.user || t.user;
			const nextTitle = meta.cwd ? `${who}:${shortDirLabel(meta.cwd)}` : t.title;
			return {
				...t,
				title: nextTitle,
				isRoot: meta.isRoot ?? t.isRoot,
				meta
			};
		});
	}

	function handlePaneUnlockRequired(tabId: number) {
		tabs = tabs.map((t) => (t.id === tabId ? { ...t, locked: true, token: '' } : t));
		if (info) {
			info = { ...info, root_unlocked: false };
		}
	}

	async function submitUnlockTab(e: SubmitEvent, tab: TerminalTab) {
		e.preventDefault();
		if (!unlockPassword || unlockBusy) return;
		unlockBusy = true;
		unlockError = null;
		try {
			const res = await api.unlockTerminal(unlockPassword, tab.user, unlockRemember);
			unlockPassword = '';
			if (info) {
				info = {
					...info,
					root_unlocked: res.root_unlocked,
					root_unlock_expires_at: res.root_unlock_expires_at
				};
			}
			tabs = tabs.map((t) =>
				t.id === tab.id
					? {
							...t,
							locked: false,
							token: res.token,
							meta: { ...t.meta, status: 'connecting', error: undefined }
						}
					: t
			);
			toast.success(`Sesi terminal ${res.user} dibuka.`);
		} catch (err) {
			unlockError =
				err instanceof ApiError || err instanceof Error ? err.message : 'Verifikasi password gagal';
		} finally {
			unlockBusy = false;
		}
	}

	async function handleLockRoot() {
		try {
			await api.lockTerminal();
			if (info) {
				info = { ...info, root_unlocked: false, root_unlock_expires_at: undefined };
			}
			toast.success('Izin sesi root baru dikunci kembali.');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal mengunci akses root');
		}
	}

	async function openAuditDialog() {
		auditOpen = true;
		auditLoading = true;
		auditPage = 1;
		try {
			auditRecords = await api.getTerminalAudit(50);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memuat riwayat audit terminal');
		} finally {
			auditLoading = false;
		}
	}

	function statusDotClass(tab: TerminalTab): string {
		if (tab.locked) return 'bg-amber-500';
		switch (tab.meta.status) {
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

	function statusLabel(tab: TerminalTab): string {
		if (tab.locked) return 'Terkunci (Verifikasi Password)';
		switch (tab.meta.status) {
			case 'connected':
				return `Terhubung (${tab.meta.user || tab.user || 'shell'})`;
			case 'connecting':
				return 'Menghubungkan...';
			case 'exited':
				return typeof tab.meta.exitCode === 'number'
					? `Selesai (exit ${tab.meta.exitCode})`
					: 'Sesi ditutup';
			case 'error':
				return tab.meta.error || 'Terputus';
		}
	}

	function formatTime(iso?: string): string {
		if (!iso) return '—';
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return d.toLocaleString('id-ID', {
			day: '2-digit',
			month: 'short',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit'
		});
	}

	onMount(async () => {
		try {
			const res = await api.getTerminalInfo();
			info = res;
			selectedShell = res.shell;
			selectedUser = res.default_user || res.user;
		} catch (err) {
			infoError =
				err instanceof ApiError || err instanceof Error ? err.message : 'Gagal memuat info terminal';
		} finally {
			infoLoaded = true;
		}
	});

	$effect(() => {
		if (!infoLoaded) return;
		const cwdParam = page.url.searchParams.get('cwd') ?? '';
		const shellParam = page.url.searchParams.get('shell') ?? '';
		const userParam = page.url.searchParams.get('user') ?? '';
		const key = `${cwdParam}::${shellParam}::${userParam}`;

		if (lastHandledQueryKey === key && tabs.length > 0) return;
		lastHandledQueryKey = key;

		if (tabs.length === 0) {
			createTab(cwdParam, shellParam, userParam);
			return;
		}
		if (cwdParam) {
			createTab(cwdParam, shellParam, userParam);
		}
	});
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
						{info.hostname}
					</Badge>
					<Badge variant="secondary" class="text-xs">
						{info.os_name}
					</Badge>
					<Badge variant="outline" class="text-xs">
						Auto-Lock Idle: {idleMinutes}m
					</Badge>
				{/if}
			</div>
			<p class="text-muted-foreground text-sm">
				Sesi shell interaktif penuh dengan pemisahan hak akses non-root default, verifikasi sandi untuk root, idle auto-close, dan audit log.
			</p>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			{#if info && info.available_users.length > 0}
				<div class="w-48">
					<NativeSelect.Root bind:value={selectedUser} aria-label="Pilih akun OS untuk tab baru">
						{#each info.available_users as u (u.username)}
							<NativeSelect.Option value={u.username}>
								{u.is_root ? `${u.username} (Root Penuh)` : `${u.username} (Non-Root)`}
							</NativeSelect.Option>
						{/each}
					</NativeSelect.Root>
				</div>
			{/if}

			{#if info && info.available_shells.length > 0}
				<div class="w-36">
					<NativeSelect.Root bind:value={selectedShell} aria-label="Pilih shell untuk tab baru">
						{#each info.available_shells as sh (sh)}
							<NativeSelect.Option value={sh}>{sh}</NativeSelect.Option>
						{/each}
					</NativeSelect.Root>
				</div>
			{/if}

			{#if info?.root_unlocked}
				<Button variant="destructive" size="sm" onclick={handleLockRoot}>
					<LockIcon class="size-4" />
					Kunci Root
				</Button>
			{/if}

			<Button variant="outline" size="sm" onclick={openAuditDialog}>
				<HistoryIcon class="size-4" />
				Audit Sesi
			</Button>

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
				disabled={!infoLoaded}
				onclick={() => createTab(activeTab?.meta.cwd || info?.home_dir || '', selectedShell, selectedUser)}
			>
				<PlusIcon class="size-4" />
				Tab Shell Baru
			</Button>
		</div>
	</div>

	{#if showNonTLSWarning}
		<Alert variant="destructive">
			<TriangleAlertIcon class="size-4" />
			<AlertTitle>Peringatan Keamanan: Akses HTTP Tanpa Enkripsi TLS</AlertTitle>
			<AlertDescription class="flex flex-wrap items-center justify-between gap-2 text-xs">
				<span>
					Daemon berjalan sebagai <strong>root</strong> dan saat ini Anda mengakses panel lewat HTTP biasa (<code class="font-mono">http://</code>).
					Ikat panel ke domain HTTPS atau akses via NetBird VPN untuk mencegah penyadapan sesi &amp; ketikan terminal.
				</span>
				<Button variant="outline" size="sm" class="h-7 shrink-0 text-xs" onclick={() => authStore.openPanelDomain()}>
					Ikat Domain &amp; HTTPS
				</Button>
			</AlertDescription>
		</Alert>
	{/if}

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
							class={`inline-flex max-w-60 items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs transition-colors ${
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
								<span class={`size-2 shrink-0 rounded-full ${statusDotClass(tab)}`}></span>
								{#if tab.isRoot}
									<ShieldAlertIcon class="size-3.5 shrink-0 text-destructive" />
								{:else}
									<SquareTerminalIcon class="size-3.5 shrink-0" />
								{/if}
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
						disabled={!infoLoaded}
						title="Tambah tab terminal"
						onclick={() => createTab(activeTab?.meta.cwd || info?.home_dir || '', selectedShell, selectedUser)}
					>
						<PlusIcon class="size-3.5" />
					</Button>
				</div>

				<!-- Session Status & Font Controls -->
				<div class="flex flex-wrap items-center gap-1.5">
					{#if activeTab}
						<Badge variant={activeTab.isRoot ? 'destructive' : 'secondary'} class="gap-1 text-[11px]">
							{#if activeTab.isRoot}
								<ShieldAlertIcon class="size-3" />
								ROOT
							{:else}
								<ShieldCheckIcon class="size-3" />
								Non-Root
							{/if}
						</Badge>

						<Badge variant="outline" class="gap-1.5 font-mono text-[11px]">
							<span class={`size-1.5 rounded-full ${statusDotClass(activeTab)}`}></span>
							{statusLabel(activeTab)}
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
						{#if activeTab.meta.cwd || activeTab.cwd}
							CWD: <span class="text-foreground">{activeTab.meta.cwd || activeTab.cwd}</span>
						{/if}
						{#if activeTab.meta.shell || activeTab.shell}
							<span class="mx-1.5">•</span> Shell: <span class="text-foreground">{activeTab.meta.shell || activeTab.shell}</span>
						{/if}
					</div>

					{#if !activeTab.locked}
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
					{/if}
				</div>
			{/if}
		</CardHeader>

		<CardContent class="p-0">
			<div class={expanded ? 'h-[calc(100dvh-10rem)] min-h-[520px]' : 'h-[calc(100dvh-17rem)] min-h-[440px]'}>
				{#if !infoLoaded}
					<div class="flex h-full items-center justify-center bg-[#0b101b] text-slate-300 text-sm">
						<Spinner class="mr-2 size-4" /> Memeriksa kebijakan keamanan terminal...
					</div>
				{:else}
					{#each tabs as tab (tab.id)}
						<div class="h-full w-full" class:hidden={tab.id !== activeTabId}>
							{#if tab.locked}
								<div class="flex h-full items-center justify-center bg-[#0b101b] p-6">
									<form
										onsubmit={(e) => submitUnlockTab(e, tab)}
										class="bg-card text-card-foreground w-full max-w-md space-y-4 rounded-xl border p-5 shadow-xl"
									>
										<div class="flex items-start gap-3">
											<div class="bg-destructive/10 text-destructive flex size-10 shrink-0 items-center justify-center rounded-lg">
												<KeyRoundIcon class="size-5" />
											</div>
											<div>
												<h2 class="text-base font-semibold">
													Verifikasi Password Terminal ({tab.user || 'root'})
												</h2>
												<p class="text-muted-foreground mt-0.5 text-xs">
													Demi keamanan server, akses shell <code class="font-mono font-semibold">{tab.user || 'root'}</code> memerlukan konfirmasi ulang kata sandi admin panel <code class="font-mono">wdev</code>.
												</p>
											</div>
										</div>

										{#if unlockError}
											<Alert variant="destructive">
												<AlertDescription class="text-xs">{unlockError}</AlertDescription>
											</Alert>
										{/if}

										<Field>
											<FieldLabel for={`term-unlock-pass-${tab.id}`}>Kata Sandi Panel Admin</FieldLabel>
											<Input
												id={`term-unlock-pass-${tab.id}`}
												type="password"
												bind:value={unlockPassword}
												placeholder="Masukkan kata sandi admin panel..."
												required
												autofocus
											/>
											<FieldDescription>
												Target folder: <code class="font-mono">{tab.cwd || info?.home_dir || '/'}</code>
											</FieldDescription>
										</Field>

										<label class="flex items-center gap-2 text-xs">
											<Checkbox bind:checked={unlockRemember} />
											<span>Ingat verifikasi di sesi ini selama 10 menit</span>
										</label>

										<div class="flex items-center justify-end gap-2 pt-1">
											{#if info && info.available_users.some((u) => !u.is_root) && tab.isRoot}
												{@const safeUser = info.available_users.find((u) => !u.is_root)!}
												<Button
													type="button"
													variant="outline"
													size="sm"
													onclick={() => {
														tabs = tabs.map((t) =>
															t.id === tab.id
																? {
																		...t,
																		user: safeUser.username,
																		isRoot: false,
																		locked: false,
																		meta: { ...t.meta, status: 'connecting', user: safeUser.username, isRoot: false }
																	}
																: t
														);
													}}
												>
													Masuk sebagai {safeUser.username} (Non-Root)
												</Button>
											{/if}
											<Button type="submit" size="sm" disabled={unlockBusy || !unlockPassword}>
												{#if unlockBusy}
													<Spinner class="size-4" />
													Memverifikasi...
												{:else}
													Buka Terminal {tab.user || 'root'}
												{/if}
											</Button>
										</div>
									</form>
								</div>
							{:else}
								<PtyTerminalPane
									bind:this={paneRefs[tab.id]}
									cwd={tab.cwd}
									shell={tab.shell}
									user={tab.user}
									token={tab.token}
									{fontSize}
									active={tab.id === activeTabId}
									onMeta={(meta) => handleTabMeta(tab.id, meta)}
									onUnlockRequired={() => handlePaneUnlockRequired(tab.id)}
								/>
							{/if}
						</div>
					{/each}
				{/if}
			</div>
		</CardContent>
	</Card>
</div>

<!-- Audit Log Modal -->
<Dialog bind:open={auditOpen}>
	<DialogContent class="flex max-h-[85dvh] flex-col sm:max-w-4xl">
		<DialogHeader>
			<DialogTitle>Riwayat Audit Sesi Terminal PTY</DialogTitle>
			<DialogDescription>
				Catatan 50 sesi terminal interaktif terakhir (akun OS target, IP klien, folder awal, dan status penutupan).
			</DialogDescription>
		</DialogHeader>

		<div class="min-h-0 flex-1 overflow-y-auto">
			{#if auditLoading}
				<div class="flex items-center justify-center py-8 text-sm">
					<Spinner class="mr-2 size-4" /> Memuat riwayat audit...
				</div>
			{:else if auditRecords.length === 0}
				<p class="text-muted-foreground py-8 text-center text-sm">Belum ada sesi terminal yang tercatat.</p>
			{:else}
				<div class="flex flex-col sm:overflow-hidden sm:rounded-lg sm:border sm:bg-card">
					<ResponsiveTableWrapper label="Riwayat Audit Terminal PTY" class="rounded-none border-0 bg-transparent">
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>User OS</TableHead>
									<TableHead>User Panel</TableHead>
									<TableHead>IP Klien</TableHead>
									<TableHead>CWD / Shell</TableHead>
									<TableHead>Status</TableHead>
									<TableHead>Mulai</TableHead>
									<TableHead>Selesai</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{#each paginatedAuditRecords as rec (rec.id)}
									<TableRow>
										<TableCell data-label="User OS">
											<Badge variant={rec.target_user === 'root' ? 'destructive' : 'secondary'} class="font-mono text-xs">
												{rec.target_user || 'unknown'}
											</Badge>
										</TableCell>
										<TableCell data-label="User Panel" class="font-mono text-xs">
											{rec.panel_user || '-'}
										</TableCell>
										<TableCell data-label="IP Klien" class="font-mono text-xs">
											{rec.client_ip || '-'}
										</TableCell>
										<TableCell data-label="CWD / Shell" class="font-mono text-xs">
											<div class="max-w-56 truncate" title={rec.cwd}>{rec.cwd}</div>
											<div class="text-muted-foreground text-[11px]">{rec.shell}</div>
										</TableCell>
										<TableCell data-label="Status">
											<Badge
												variant={rec.status === 'active' ? 'default' : rec.status === 'idle_timeout' ? 'secondary' : 'outline'}
												class="text-xs"
											>
												{rec.status}
												{#if rec.status !== 'active'}(exit {rec.exit_code}){/if}
											</Badge>
										</TableCell>
										<TableCell data-label="Mulai" class="text-xs whitespace-nowrap">
											{formatTime(rec.started_at)}
										</TableCell>
										<TableCell data-label="Selesai" class="text-muted-foreground text-xs whitespace-nowrap">
											{formatTime(rec.ended_at)}
										</TableCell>
									</TableRow>
								{/each}
							</TableBody>
						</Table>
					</ResponsiveTableWrapper>
					<PaginationBar
						bind:page={auditPage}
						totalItems={auditRecords.length}
						pageSize={AUDIT_PAGE_SIZE}
						itemLabel="sesi audit"
						class="rounded-lg border bg-card sm:rounded-none sm:border-0 sm:border-t mt-3 sm:mt-0"
					/>
				</div>
			{/if}
		</div>
	</DialogContent>
</Dialog>
