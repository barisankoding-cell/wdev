<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api/client.js';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PaginationBar from '#lib/components/shared/PaginationBar.svelte';
	import PtyTerminalPane, {
		type TerminalPaneMeta
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
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { terminalStore } from '#lib/stores/terminal.svelte.js';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import type {
		TerminalAuditRecord,
		TerminalInfo,
		TerminalSessionInfo,
		TerminalTargetUser
	} from '#lib/types/api.js';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import ClipboardIcon from '@lucide/svelte/icons/clipboard';
	import ClockIcon from '@lucide/svelte/icons/clock';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import EraserIcon from '@lucide/svelte/icons/eraser';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import HistoryIcon from '@lucide/svelte/icons/history';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import LockIcon from '@lucide/svelte/icons/lock';
	import Maximize2Icon from '@lucide/svelte/icons/maximize-2';
	import Minimize2Icon from '@lucide/svelte/icons/minimize-2';
	import PinIcon from '@lucide/svelte/icons/pin';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw';
	import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import SquareTerminalIcon from '@lucide/svelte/icons/square-terminal';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import XIcon from '@lucide/svelte/icons/x';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	const TABS_STORAGE_KEY = 'wdev:terminal:tabs:v1';

	let { visible = true }: { visible?: boolean } = $props();

	interface TerminalTab {
		id: number;
		title: string;
		cwd: string;
		shell: string;
		user: string;
		composeProject?: string;
		composeService?: string;
		isRoot: boolean;
		locked: boolean;
		hasPaneMounted: boolean;
		sessionId?: string;
		token: string;
		meta: TerminalPaneMeta;
	}

	interface PersistedTabEntry {
		id: number;
		title: string;
		cwd: string;
		shell: string;
		user: string;
		composeProject?: string;
		composeService?: string;
		isRoot: boolean;
		sessionId: string;
		keepAlive?: boolean;
	}

	interface PersistedTabsState {
		activeTabId: number | null;
		nextTabSeq: number;
		tabs: PersistedTabEntry[];
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
	let liveBackendSessions = $state<TerminalSessionInfo[]>([]);

	// Step-up password unlock state
	let unlockPassword = $state('');
	let unlockTotpCode = $state('');
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

	// Active & detached sessions modal
	let sessionsOpen = $state(false);
	let sessionsLoading = $state(false);

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

	const canControlActiveTab = $derived(
		Boolean(
			activeTab &&
				!activeTab.locked &&
				(activeTab.meta.status === 'connected' || activeTab.meta.status === 'reconnecting')
		)
	);

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

	function persistTabsToStorage() {
		if (typeof window === 'undefined' || !infoLoaded) return;
		try {
			const entries: PersistedTabEntry[] = [];
			for (const tab of tabs) {
				const sid = tab.meta.sessionId || tab.sessionId;
				if (!sid) continue;
				if (tab.meta.status === 'exited' || tab.meta.status === 'idle_timeout') continue;
				entries.push({
					id: tab.id,
					title: tab.title,
					cwd: tab.meta.cwd || tab.cwd,
					shell: tab.meta.shell || tab.shell,
					user: tab.meta.user || tab.user,
					composeProject: tab.composeProject,
					composeService: tab.composeService,
					isRoot: tab.isRoot,
					sessionId: sid,
					keepAlive: tab.meta.keepAlive
				});
			}
			const payload: PersistedTabsState = {
				activeTabId,
				nextTabSeq,
				tabs: entries
			};
			window.sessionStorage.setItem(TABS_STORAGE_KEY, JSON.stringify(payload));
		} catch {
			// Abaikan error sessionStorage di mode privat
		}
	}

	function loadPersistedTabs(): PersistedTabsState | null {
		if (typeof window === 'undefined') return null;
		try {
			const raw = window.sessionStorage.getItem(TABS_STORAGE_KEY);
			if (!raw) return null;
			const parsed = JSON.parse(raw) as Partial<PersistedTabsState>;
			if (!Array.isArray(parsed.tabs)) return null;
			return {
				activeTabId: typeof parsed.activeTabId === 'number' ? parsed.activeTabId : null,
				nextTabSeq: typeof parsed.nextTabSeq === 'number' && parsed.nextTabSeq > 0 ? parsed.nextTabSeq : 1,
				tabs: parsed.tabs.filter(
					(item): item is PersistedTabEntry =>
						Boolean(item) && typeof item.sessionId === 'string' && item.sessionId.length > 0
				)
			};
		} catch {
			return null;
		}
	}

	function createTab(
		cwd = '',
		shell = '',
		requestedUser = '',
		composeProject = '',
		composeService = '',
		existingSession?: TerminalSessionInfo
	) {
		const id = nextTabSeq++;
		const userObj = resolveTargetUserObj(existingSession?.user || requestedUser || selectedUser);
		const targetUsername =
			existingSession?.user || userObj?.username || requestedUser || selectedUser || '';
		const isRoot = existingSession ? existingSession.is_root : Boolean(userObj?.is_root);
		const locked = doesUserNeedUnlock(userObj);

		const cleanCwd = (existingSession?.cwd || cwd).trim();
		const cleanCompProj = composeProject.trim();
		const cleanCompSvc = composeService.trim();
		const isComposeExec = Boolean(cleanCompProj && cleanCompSvc);
		const targetShell = existingSession?.shell
			? existingSession.shell
			: isComposeExec
				? shell.trim() || '/bin/sh'
				: shell.trim() || userObj?.shell || selectedShell;
		const prefix = isRoot ? 'root' : targetUsername || 'shell';
		const title = existingSession?.exec_label
			? existingSession.exec_label
			: isComposeExec
				? `exec:${cleanCompProj}/${cleanCompSvc}`
				: cleanCwd
					? `${prefix}:${shortDirLabel(cleanCwd)}`
					: `${prefix}#${id}`;

		const newTab: TerminalTab = {
			id,
			title,
			cwd: cleanCwd,
			shell: targetShell,
			user: targetUsername,
			composeProject: cleanCompProj || undefined,
			composeService: cleanCompSvc || undefined,
			isRoot,
			locked,
			hasPaneMounted: !locked,
			sessionId: existingSession?.session_id,
			token: '',
			meta: {
				status: locked ? 'error' : 'connecting',
				sessionId: existingSession?.session_id,
				cwd: cleanCwd,
				shell: targetShell,
				user: targetUsername,
				isRoot,
				keepAlive: existingSession?.keep_alive
			}
		};
		tabs = [...tabs, newTab];
		activeTabId = id;
		if (locked) {
			unlockPassword = '';
			unlockTotpCode = '';
			unlockError = null;
		}
		persistTabsToStorage();
	}

	async function closeTab(id: number) {
		const idx = tabs.findIndex((t) => t.id === id);
		if (idx < 0) return;
		const target = tabs[idx];

		if (
			!target.locked &&
			(target.meta.status === 'connected' || target.meta.status === 'reconnecting')
		) {
			const confirmed = await cp.confirm({
				title: t('terminal.closeActiveTabConfirmTitle'),
				description: t('terminal.closeActiveTabConfirmDesc', { title: target.title }),
				confirmText: t('terminal.closeActiveTabConfirmBtn'),
				destructive: true
			});
			if (!confirmed) return;
		}

		const sid = target.meta.sessionId || target.sessionId;
		paneRefs[id]?.closeSession();
		if (sid) {
			void api.closeTerminalSession(sid).catch(() => {});
		}

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
		persistTabsToStorage();
		void refreshLiveSessions();
	}

	function handleTabMeta(id: number, meta: TerminalPaneMeta) {
		const prev = tabs.find((t) => t.id === id);
		const prevSid = prev?.meta.sessionId;
		tabs = tabs.map((t) => {
			if (t.id !== id) return t;
			const who = meta.user || t.user;
			const nextTitle = meta.cwd ? `${who}:${shortDirLabel(meta.cwd)}` : t.title;
			return {
				...t,
				title: nextTitle,
				isRoot: meta.isRoot ?? t.isRoot,
				sessionId: meta.sessionId,
				meta
			};
		});
		persistTabsToStorage();
		if ((meta.sessionId && meta.sessionId !== prevSid) || (!meta.sessionId && prevSid)) {
			void refreshLiveSessions();
		}
	}

	function handlePaneUnlockRequired(tabId: number) {
		tabs = tabs.map((t) =>
			t.id === tabId ? { ...t, locked: true, token: '' } : t
		);
		if (info) {
			info = { ...info, root_unlocked: false };
		}
		unlockPassword = '';
		unlockTotpCode = '';
		unlockError = null;
	}

	async function submitUnlockTab(e: SubmitEvent, tab: TerminalTab) {
		e.preventDefault();
		if (!unlockPassword || unlockBusy) return;
		if (info?.totp_required && !unlockTotpCode.trim()) return;
		unlockBusy = true;
		unlockError = null;
		try {
			const res = await api.unlockTerminal(
				unlockPassword,
				tab.user,
				unlockRemember,
				unlockTotpCode.trim()
			);
			unlockPassword = '';
			unlockTotpCode = '';
			if (info) {
				info = {
					...info,
					root_unlocked: res.root_unlocked,
					root_unlock_expires_at: res.root_unlock_expires_at
				};
			}
			const alreadyMounted = tab.hasPaneMounted && Boolean(paneRefs[tab.id]);
			tabs = tabs.map((t) =>
				t.id === tab.id
					? {
							...t,
							locked: false,
							hasPaneMounted: true,
							token: res.token,
							meta: { ...t.meta, status: 'connecting', error: undefined }
						}
					: t
			);
			if (alreadyMounted) {
				paneRefs[tab.id]?.reconnect(res.token);
			}
			toast.success(t('terminal.toast.unlocked', { user: res.user }));
		} catch (err) {
			unlockError =
				err instanceof ApiError || err instanceof Error ? err.message : t('terminal.toast.unlockFailed');
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
			toast.success(t('terminal.toast.rootLocked'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('terminal.toast.lockFailed'));
		}
	}

	async function openAuditDialog() {
		auditOpen = true;
		auditLoading = true;
		auditPage = 1;
		try {
			auditRecords = await api.getTerminalAudit(50);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('terminal.toast.auditFailed'));
		} finally {
			auditLoading = false;
		}
	}

	async function refreshLiveSessions() {
		try {
			liveBackendSessions = await api.listTerminalSessions();
		} catch {
			// Abaikan kegagalan refresh pasif
		}
	}

	async function openSessionsDialog() {
		sessionsOpen = true;
		sessionsLoading = true;
		try {
			liveBackendSessions = await api.listTerminalSessions();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('terminal.sessions.loadFailed'));
		} finally {
			sessionsLoading = false;
		}
	}

	function attachBackendSession(sess: TerminalSessionInfo) {
		const existing = tabs.find(
			(tab) => tab.meta.sessionId === sess.session_id || tab.sessionId === sess.session_id
		);
		if (existing) {
			activeTabId = existing.id;
			sessionsOpen = false;
			return;
		}
		createTab(sess.cwd, sess.shell, sess.user, '', '', sess);
		sessionsOpen = false;
	}

	async function terminateBackendSession(sessId: string) {
		try {
			await api.closeTerminalSession(sessId);
			liveBackendSessions = liveBackendSessions.filter((s) => s.session_id !== sessId);
			const matchedTab = tabs.find(
				(tab) => tab.meta.sessionId === sessId || tab.sessionId === sessId
			);
			if (matchedTab) {
				paneRefs[matchedTab.id]?.closeSession();
				tabs = tabs.map((tab) =>
					tab.id === matchedTab.id
						? {
								...tab,
								sessionId: undefined,
								meta: { ...tab.meta, sessionId: undefined, status: 'exited', exitCode: 0 }
							}
						: tab
				);
			}
			toast.success(t('terminal.sessions.terminatedToast'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('terminal.sessions.terminateFailed'));
		}
	}

	function statusDotClass(tab: TerminalTab): string {
		if (tab.locked) return 'bg-amber-500';
		switch (tab.meta.status) {
			case 'connected':
				return 'bg-emerald-500';
			case 'connecting':
			case 'reconnecting':
				return 'bg-amber-500 animate-pulse';
			case 'idle_timeout':
				return 'bg-amber-500';
			case 'error':
				return 'bg-destructive';
			default:
				return 'bg-muted-foreground/50';
		}
	}

	function statusLabel(tab: TerminalTab): string {
		if (tab.locked) return t('terminal.status.locked');
		switch (tab.meta.status) {
			case 'connected':
				return t('terminal.status.connected', { user: tab.meta.user || tab.user || 'shell' });
			case 'connecting':
				return t('terminal.status.connecting');
			case 'reconnecting':
				return t('terminal.status.reconnecting');
			case 'idle_timeout':
				return t('terminal.status.idleTimeout');
			case 'exited':
				return typeof tab.meta.exitCode === 'number'
					? t('terminal.status.exitedCode', { code: tab.meta.exitCode })
					: t('terminal.status.closed');
			case 'error':
				return tab.meta.error || t('terminal.status.disconnected');
		}
	}

	function formatTime(iso?: string): string {
		if (!iso) return '—';
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return i18n.formatDateTime(d, {
			day: '2-digit',
			month: 'short',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit'
		});
	}

	async function refreshTerminalInfo() {
		try {
			const res = await api.getTerminalInfo();
			info = res;
			if (!selectedShell) selectedShell = res.shell;
			if (!selectedUser) selectedUser = res.default_user || res.user;
		} catch (err) {
			infoError =
				err instanceof ApiError || err instanceof Error ? err.message : t('terminal.toast.infoFailed');
		}
	}

	let lastTerminalMode: string | null = null;

	onMount(async () => {
		const [, sessions] = await Promise.all([
			refreshTerminalInfo(),
			api.listTerminalSessions().catch(() => [] as TerminalSessionInfo[])
		]);
		liveBackendSessions = sessions;

		const liveById = new Map<string, TerminalSessionInfo>();
		for (const s of sessions) {
			if (!s.exited) liveById.set(s.session_id, s);
		}

		const saved = loadPersistedTabs();
		if (saved && saved.tabs.length > 0 && liveById.size > 0) {
			const restored: TerminalTab[] = [];
			let maxId = saved.nextTabSeq;
			for (const entry of saved.tabs) {
				const live = liveById.get(entry.sessionId);
				if (!live) continue;
				const userObj = resolveTargetUserObj(live.user || entry.user);
				const locked = doesUserNeedUnlock(userObj);
				if (entry.id >= maxId) maxId = entry.id + 1;
				restored.push({
					id: entry.id,
					title: entry.title,
					cwd: live.cwd || entry.cwd,
					shell: live.shell || entry.shell,
					user: live.user || entry.user,
					composeProject: entry.composeProject,
					composeService: entry.composeService,
					isRoot: live.is_root,
					locked,
					hasPaneMounted: !locked,
					sessionId: live.session_id,
					token: '',
					meta: {
						status: locked ? 'error' : 'connecting',
						sessionId: live.session_id,
						cwd: live.cwd || entry.cwd,
						shell: live.shell || entry.shell,
						user: live.user || entry.user,
						isRoot: live.is_root,
						keepAlive: live.keep_alive
					}
				});
			}
			if (restored.length > 0) {
				nextTabSeq = maxId;
				tabs = restored;
				activeTabId =
					saved.activeTabId && restored.some((t) => t.id === saved.activeTabId)
						? saved.activeTabId
						: restored[0].id;
			}
		}

		infoLoaded = true;
	});

	$effect(() => {
		const currentMode = modeStore.mode;
		if (lastTerminalMode !== null && lastTerminalMode !== currentMode) {
			void refreshTerminalInfo();
		}
		lastTerminalMode = currentMode;
	});

	$effect(() => {
		const connected = tabs.filter(
			(tab) => tab.meta.status === 'connected' || tab.meta.status === 'reconnecting'
		).length;
		terminalStore.setConnectedCount(connected);
	});

	$effect(() => {
		if (!infoLoaded || !visible) return;
		const cwdParam = page.url.searchParams.get('cwd') ?? '';
		const shellParam = page.url.searchParams.get('shell') ?? '';
		const userParam = page.url.searchParams.get('user') ?? '';
		const compProjParam = page.url.searchParams.get('compose_project') ?? '';
		const compSvcParam = page.url.searchParams.get('compose_service') ?? '';
		const key = `${cwdParam}::${shellParam}::${userParam}::${compProjParam}::${compSvcParam}`;

		if (lastHandledQueryKey === key && tabs.length > 0) return;
		lastHandledQueryKey = key;

		if (tabs.length === 0) {
			createTab(cwdParam, shellParam, userParam, compProjParam, compSvcParam);
			return;
		}
		if (cwdParam || (compProjParam && compSvcParam)) {
			createTab(cwdParam, shellParam, userParam, compProjParam, compSvcParam);
		}
	});
</script>

<div class="flex min-h-0 flex-1 flex-col gap-3">
	<!-- Header -->
	{#if !expanded}
		<div class="shrink-0 space-y-2">
			<div class="flex flex-col gap-2.5 lg:flex-row lg:items-center lg:justify-between">
				<div class="flex min-w-0 flex-wrap items-center gap-2">
					<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('terminal.title')}</h1>
					{#if info}
						<div class="flex flex-wrap items-center gap-1.5">
							<Badge variant="outline" class="font-mono text-[11px]">
								{info.hostname}
							</Badge>
							<Badge variant="secondary" class="max-w-full truncate text-[11px]">
								{info.os_name}
							</Badge>
							<Badge variant="outline" class="whitespace-nowrap text-[11px]">
								{t('terminal.autoLockBadge', { minutes: idleMinutes })}
							</Badge>
						</div>
					{/if}
				</div>

				<div class="flex flex-wrap items-center gap-1.5 lg:shrink-0">
					{#if info?.root_unlocked}
						<Button variant="destructive" size="sm" onclick={handleLockRoot}>
							<LockIcon class="size-3.5" />
							{t('terminal.lockRoot')}
						</Button>
					{/if}

					<Button variant="outline" size="sm" onclick={openSessionsDialog}>
						<ActivityIcon class="size-3.5" />
						{t('terminal.activeSessionsBtn', { count: Math.max(liveBackendSessions.length, tabs.filter((tb) => Boolean(tb.meta.sessionId)).length) })}
					</Button>

					<Button variant="outline" size="sm" onclick={openAuditDialog}>
						<HistoryIcon class="size-3.5" />
						{t('terminal.auditSession')}
					</Button>

					<Button
						variant="outline"
						size="sm"
						href={currentFileMgrPath ? `/files?path=${encodeURIComponent(currentFileMgrPath)}` : '/files'}
					>
						<FolderOpenIcon class="size-3.5" />
						{t('terminal.fileManager')}
					</Button>
				</div>
			</div>

			<div class="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
				<p class="text-muted-foreground min-w-0 text-xs sm:text-sm">
					{t('terminal.subtitle')}
				</p>

				<div class="flex flex-wrap items-center gap-1.5 lg:shrink-0">
					{#if info && info.available_users.length > 0}
						<NativeSelect.Root
							size="sm"
							class="text-xs"
							bind:value={selectedUser}
							aria-label={t('terminal.selectUserAria')}
						>
							{#each info.available_users as u (u.username)}
								<NativeSelect.Option value={u.username}>
									{u.is_root
										? t('terminal.userRootOption', { username: u.username })
										: t('terminal.userNonRootOption', { username: u.username })}
								</NativeSelect.Option>
							{/each}
						</NativeSelect.Root>
					{/if}

					{#if info && info.available_shells.length > 0}
						<NativeSelect.Root
							size="sm"
							class="font-mono text-xs"
							bind:value={selectedShell}
							aria-label={t('terminal.selectShellAria')}
						>
							{#each info.available_shells as sh (sh)}
								<NativeSelect.Option value={sh}>{sh}</NativeSelect.Option>
							{/each}
						</NativeSelect.Root>
					{/if}

					<Button
						size="sm"
						disabled={!infoLoaded}
						onclick={() => createTab(activeTab?.meta.cwd || info?.home_dir || '', selectedShell, selectedUser)}
					>
						<PlusIcon class="size-3.5" />
						{t('terminal.newTabBtn')}
					</Button>
				</div>
			</div>
		</div>

		{#if showNonTLSWarning}
			<Alert variant="destructive" class="shrink-0">
				<TriangleAlertIcon class="size-4" />
				<AlertTitle>{t('terminal.tlsWarningTitle')}</AlertTitle>
				<AlertDescription class="flex flex-wrap items-center justify-between gap-2 text-xs">
					<span>
						{t('terminal.tlsWarningDescPart1')} <strong>root</strong> {t('terminal.tlsWarningDescPart2')}<code class="font-mono">http://</code>{t('terminal.tlsWarningDescPart3')}
					</span>
					<Button variant="outline" size="sm" class="h-7 shrink-0 text-xs" onclick={() => authStore.openPanelDomain()}>
						{t('terminal.bindDomainHttps')}
					</Button>
				</AlertDescription>
			</Alert>
		{/if}
	{/if}

	{#if infoError}
		<p class="text-destructive shrink-0 text-xs">{infoError}</p>
	{/if}

	<!-- Main Terminal Card -->
	<Card class="flex min-h-0 flex-1 flex-col gap-0 overflow-hidden py-0">
		<CardHeader class="bg-muted/30 shrink-0 space-y-2 border-b px-3 py-2">
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
								onclick={() => {
									activeTabId = tab.id;
									persistTabsToStorage();
								}}
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
								title={t('terminal.closeTabTitle')}
								onclick={() => void closeTab(tab.id)}
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
						title={t('terminal.addTabTitle')}
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
								{t('terminal.badgeRoot')}
							{:else}
								<ShieldCheckIcon class="size-3" />
								{t('terminal.badgeNonRoot')}
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
							title={t('terminal.fontDecreaseTitle')}
							onclick={() => (fontSize = Math.max(11, fontSize - 1))}
						>
							A-
						</button>
						<span class="text-muted-foreground border-x px-1.5 font-mono text-[11px]">{fontSize}px</span>
						<button
							type="button"
							class="hover:bg-muted px-2 py-1 text-xs font-medium"
							title={t('terminal.fontIncreaseTitle')}
							onclick={() => (fontSize = Math.min(22, fontSize + 1))}
						>
							A+
						</button>
					</div>

					<Button
						variant="outline"
						size="icon-sm"
						class="h-7 w-7"
						title={expanded ? t('terminal.normalSizeTitle') : t('terminal.expandTitle')}
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
							{t('terminal.cwdLabel')} <span class="text-foreground">{activeTab.meta.cwd || activeTab.cwd}</span>
						{/if}
						{#if activeTab.meta.shell || activeTab.shell}
							<span class="mx-1.5">•</span> {t('terminal.shellLabel')} <span class="text-foreground">{activeTab.meta.shell || activeTab.shell}</span>
						{/if}
					</div>

					{#if !activeTab.locked}
						<div class="flex flex-wrap items-center gap-1">
							<Button
								variant="ghost"
								size="sm"
								class="h-6 px-2 font-mono text-[11px]"
								disabled={!canControlActiveTab}
								title={t('terminal.sendCtrlCTitle')}
								onclick={() => activePane?.sendText('\x03')}
							>
								Ctrl+C
							</Button>
							<Button
								variant="ghost"
								size="sm"
								class="h-6 px-2 font-mono text-[11px]"
								disabled={!canControlActiveTab}
								title={t('terminal.sendTabTitle')}
								onclick={() => activePane?.sendText('\t')}
							>
								Tab
							</Button>
							<Button
								variant="ghost"
								size="sm"
								class="h-6 px-2 font-mono text-[11px]"
								disabled={!canControlActiveTab}
								title={t('terminal.sendEscTitle')}
								onclick={() => activePane?.sendText('\x1b')}
							>
								Esc
							</Button>
							<Button
								variant={activeTab.meta.keepAlive ? 'default' : 'outline'}
								size="sm"
								class="h-6 gap-1 px-2 text-[11px]"
								disabled={!canControlActiveTab}
								title={activeTab.meta.keepAlive ? t('terminal.keepAliveOnTitle') : t('terminal.keepAliveOffTitle')}
								onclick={() => activePane?.setKeepAlive(!activeTab.meta.keepAlive)}
							>
								<PinIcon class="size-3" />
								{activeTab.meta.keepAlive ? t('terminal.keepAliveOnLabel') : t('terminal.keepAliveOffLabel')}
							</Button>
							<Button
								variant="outline"
								size="sm"
								class="h-6 gap-1 px-2 text-[11px]"
								onclick={() => activePane?.copySelection()}
							>
								<CopyIcon class="size-3" />
								{t('terminal.copyBtn')}
							</Button>
							<Button
								variant="outline"
								size="sm"
								class="h-6 gap-1 px-2 text-[11px]"
								disabled={!canControlActiveTab}
								onclick={() => activePane?.pasteClipboard()}
							>
								<ClipboardIcon class="size-3" />
								{t('terminal.pasteBtn')}
							</Button>
							<Button
								variant="outline"
								size="sm"
								class="h-6 gap-1 px-2 text-[11px]"
								onclick={() => activePane?.clear()}
							>
								<EraserIcon class="size-3" />
								{t('terminal.clearBtn')}
							</Button>
							<Button
								variant="outline"
								size="sm"
								class="h-6 gap-1 px-2 text-[11px]"
								onclick={() => activePane?.reconnect(undefined, activeTab.meta.status === 'exited' || activeTab.meta.status === 'idle_timeout')}
							>
								<RotateCcwIcon class="size-3" />
								{t('terminal.reconnectBtn')}
							</Button>
						</div>
					{/if}
				</div>

				<!-- Idle Warning Countdown Bar -->
				{#if !activeTab.locked && activeTab.meta.status === 'connected' && typeof activeTab.meta.idleWarningSeconds === 'number' && activeTab.meta.idleWarningSeconds > 0}
					<div class="flex flex-wrap items-center justify-between gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-700 dark:text-amber-300">
						<div class="flex items-center gap-2 font-medium">
							<ClockIcon class="size-3.5 shrink-0" />
							<span>{t('terminal.idleWarningTitle', { seconds: activeTab.meta.idleWarningSeconds })}</span>
						</div>
						<div class="flex items-center gap-1.5">
							<Button
								variant="outline"
								size="sm"
								class="h-6 px-2 text-[11px]"
								onclick={() => activePane?.extendIdle()}
							>
								{t('terminal.extendIdleBtn')}
							</Button>
							<Button
								size="sm"
								class="h-6 px-2 text-[11px]"
								onclick={() => activePane?.setKeepAlive(true)}
							>
								{t('terminal.enableKeepAliveBtn')}
							</Button>
						</div>
					</div>
				{/if}

				<!-- Disconnected / Idle Timeout / Exited Inline Banner -->
				{#if !activeTab.locked && (activeTab.meta.status === 'idle_timeout' || activeTab.meta.status === 'error' || activeTab.meta.status === 'exited')}
					<div class="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border bg-muted/60 px-3 py-1.5 text-xs">
						<span class="text-muted-foreground">
							{#if activeTab.meta.status === 'idle_timeout'}
								{t('terminal.idleClosedBanner')}
							{:else if activeTab.meta.status === 'exited'}
								{t('terminal.exitedBanner', { code: activeTab.meta.exitCode ?? 0 })}
							{:else}
								{activeTab.meta.error || t('terminal.disconnectedBanner')}
							{/if}
						</span>
						<Button
							size="sm"
							variant="default"
							class="h-6 gap-1 px-2.5 text-[11px]"
							onclick={() =>
								activePane?.reconnect(
									undefined,
									activeTab.meta.status === 'exited' || activeTab.meta.status === 'idle_timeout'
								)}
						>
							<RotateCcwIcon class="size-3" />
							{t('terminal.reconnectNowBtn')}
						</Button>
					</div>
				{/if}
			{/if}
		</CardHeader>

		<CardContent class="relative min-h-0 flex-1 p-0">
			<div class="absolute inset-0">
				{#if !infoLoaded}
					<div class="flex h-full items-center justify-center bg-[#0b101b] text-slate-300 text-sm">
						<Spinner class="mr-2 size-4" /> {t('terminal.checkingPolicy')}
					</div>
				{:else}
					{#each tabs as tab (tab.id)}
						<div class="relative h-full w-full overflow-hidden" class:hidden={tab.id !== activeTabId}>
							{#if tab.hasPaneMounted || !tab.locked}
								<PtyTerminalPane
									bind:this={paneRefs[tab.id]}
									sessionId={tab.sessionId}
									cwd={tab.cwd}
									shell={tab.shell}
									user={tab.user}
									token={tab.token}
									composeProject={tab.composeProject}
									composeService={tab.composeService}
									{fontSize}
									active={visible && tab.id === activeTabId && !tab.locked}
									onMeta={(meta) => handleTabMeta(tab.id, meta)}
									onUnlockRequired={() => handlePaneUnlockRequired(tab.id)}
								/>
							{/if}

							{#if tab.locked}
								<div
									class={tab.hasPaneMounted
										? 'absolute inset-0 z-10 flex items-center justify-center overflow-y-auto bg-[#0b101b]/85 p-4 backdrop-blur-xs sm:p-6'
										: 'flex h-full items-center justify-center overflow-y-auto bg-[#0b101b] p-4 sm:p-6'}
								>
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
													{t('terminal.unlock.title', { user: tab.user || 'root' })}
												</h2>
												<p class="text-muted-foreground mt-0.5 text-xs">
													{t('terminal.unlock.descPrefix')} <code class="font-mono font-semibold">{tab.user || 'root'}</code> {t('terminal.unlock.descMiddle')} <code class="font-mono">wdev</code> {t('terminal.unlock.descSuffix')}
												</p>
											</div>
										</div>

										{#if unlockError}
											<Alert variant="destructive">
												<AlertDescription class="text-xs">{unlockError}</AlertDescription>
											</Alert>
										{/if}

										<Field>
											<FieldLabel for={`term-unlock-pass-${tab.id}`}>{t('terminal.unlock.passwordLabel')}</FieldLabel>
											<Input
												id={`term-unlock-pass-${tab.id}`}
												type="password"
												bind:value={unlockPassword}
												placeholder={t('terminal.unlock.passwordPlaceholder')}
												required
												autofocus
											/>
											<FieldDescription>
												{t('terminal.unlock.targetFolder')} <code class="font-mono">{tab.cwd || info?.home_dir || '/'}</code>
											</FieldDescription>
										</Field>

										{#if info?.totp_required}
											<Field>
												<FieldLabel for={`term-unlock-totp-${tab.id}`}>{t('terminal.unlock.totpLabel')}</FieldLabel>
												<Input
													id={`term-unlock-totp-${tab.id}`}
													type="text"
													autocomplete="one-time-code"
													bind:value={unlockTotpCode}
													placeholder={t('terminal.unlock.totpPlaceholder')}
													class="font-mono tracking-wider"
													required
												/>
											</Field>
										{/if}

										<label class="flex items-center gap-2 text-xs">
											<Checkbox bind:checked={unlockRemember} />
											<span>{t('terminal.unlock.rememberLabel')}</span>
										</label>

										<div class="flex items-center justify-end gap-2 pt-1">
											{#if !tab.hasPaneMounted && info && info.available_users.some((u) => !u.is_root) && tab.isRoot}
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
																		hasPaneMounted: true,
																		meta: {
																			...t.meta,
																			status: 'connecting',
																			user: safeUser.username,
																			isRoot: false
																		}
																	}
																: t
														);
													}}
												>
													{t('terminal.unlock.enterAsNonRoot', { user: safeUser.username })}
												</Button>
											{/if}
											<Button
												type="submit"
												size="sm"
												disabled={unlockBusy || !unlockPassword || (Boolean(info?.totp_required) && !unlockTotpCode.trim())}
											>
												{#if unlockBusy}
													<Spinner class="size-4" />
													{t('terminal.unlock.verifying')}
												{:else}
													{t('terminal.unlock.openTerminal', { user: tab.user || 'root' })}
												{/if}
											</Button>
										</div>
									</form>
								</div>
							{/if}
						</div>
					{/each}
				{/if}
			</div>
		</CardContent>
	</Card>
</div>

<!-- Active & Detached Sessions Modal -->
<Dialog bind:open={sessionsOpen}>
	<DialogContent class="flex max-h-[85dvh] flex-col sm:max-w-3xl">
		<DialogHeader>
			<DialogTitle>{t('terminal.sessions.title')}</DialogTitle>
			<DialogDescription>
				{t('terminal.sessions.description')}
			</DialogDescription>
		</DialogHeader>

		<div class="min-h-0 flex-1 overflow-y-auto space-y-2">
			{#if sessionsLoading}
				<div class="flex items-center justify-center py-8 text-sm">
					<Spinner class="mr-2 size-4" /> {t('terminal.sessions.loading')}
				</div>
			{:else if liveBackendSessions.length === 0}
				<p class="text-muted-foreground py-8 text-center text-sm">{t('terminal.sessions.empty')}</p>
			{:else}
				<div class="space-y-2">
					{#each liveBackendSessions as sess (sess.session_id)}
						<div class="flex flex-col justify-between gap-2 rounded-lg border p-3 sm:flex-row sm:items-center">
							<div class="min-w-0 space-y-1">
								<div class="flex flex-wrap items-center gap-1.5">
									<Badge variant={sess.is_root ? 'destructive' : 'secondary'} class="font-mono text-[11px]">
										{sess.user}
									</Badge>
									<Badge variant={sess.attached ? 'default' : 'outline'} class="text-[11px]">
										{sess.attached ? t('terminal.sessions.attachedBadge') : t('terminal.sessions.detachedBadge')}
									</Badge>
									{#if sess.keep_alive}
										<Badge variant="outline" class="text-[11px]">{t('terminal.sessions.keepAliveBadge')}</Badge>
									{/if}
									<span class="text-muted-foreground font-mono text-[11px]">#{sess.session_id}</span>
								</div>
								<div class="text-muted-foreground truncate font-mono text-xs">
									{sess.cwd} • {sess.shell}
								</div>
							</div>
							<div class="flex shrink-0 items-center gap-1.5">
								<Button size="sm" variant="outline" onclick={() => attachBackendSession(sess)}>
									{t('terminal.sessions.attachBtn')}
								</Button>
								<Button
									size="sm"
									variant="destructive"
									onclick={() => void terminateBackendSession(sess.session_id)}
								>
									<Trash2Icon class="size-3.5" />
									{t('terminal.sessions.killBtn')}
								</Button>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</DialogContent>
</Dialog>

<!-- Audit Log Modal -->
<Dialog bind:open={auditOpen}>
	<DialogContent class="flex max-h-[85dvh] flex-col sm:max-w-4xl">
		<DialogHeader>
			<DialogTitle>{t('terminal.audit.title')}</DialogTitle>
			<DialogDescription>
				{t('terminal.audit.description')}
			</DialogDescription>
		</DialogHeader>

		<div class="min-h-0 flex-1 overflow-y-auto">
			{#if auditLoading}
				<div class="flex items-center justify-center py-8 text-sm">
					<Spinner class="mr-2 size-4" /> {t('terminal.audit.loading')}
				</div>
			{:else if auditRecords.length === 0}
				<p class="text-muted-foreground py-8 text-center text-sm">{t('terminal.audit.empty')}</p>
			{:else}
				<div class="flex flex-col sm:overflow-hidden sm:rounded-lg sm:border sm:bg-card">
					<ResponsiveTableWrapper label={t('terminal.audit.tableAria')} class="rounded-none border-0 bg-transparent">
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>{t('terminal.audit.colOsUser')}</TableHead>
									<TableHead>{t('terminal.audit.colPanelUser')}</TableHead>
									<TableHead>{t('terminal.audit.colClientIp')}</TableHead>
									<TableHead>{t('terminal.audit.colCwdShell')}</TableHead>
									<TableHead>{t('terminal.audit.colStatus')}</TableHead>
									<TableHead>{t('terminal.audit.colStarted')}</TableHead>
									<TableHead>{t('terminal.audit.colEnded')}</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{#each paginatedAuditRecords as rec (rec.id)}
									<TableRow>
										<TableCell data-label={t('terminal.audit.colOsUser')}>
											<Badge variant={rec.target_user === 'root' ? 'destructive' : 'secondary'} class="font-mono text-xs">
												{rec.target_user || 'unknown'}
											</Badge>
										</TableCell>
										<TableCell data-label={t('terminal.audit.colPanelUser')} class="font-mono text-xs">
											{rec.panel_user || '-'}
										</TableCell>
										<TableCell data-label={t('terminal.audit.colClientIp')} class="font-mono text-xs">
											{rec.client_ip || '-'}
										</TableCell>
										<TableCell data-label={t('terminal.audit.colCwdShell')} class="font-mono text-xs">
											<div class="max-w-56 truncate" title={rec.cwd}>{rec.cwd}</div>
											<div class="text-muted-foreground text-[11px]">{rec.shell}</div>
										</TableCell>
										<TableCell data-label={t('terminal.audit.colStatus')}>
											<Badge
												variant={rec.status === 'active' ? 'default' : rec.status === 'idle_timeout' ? 'secondary' : 'outline'}
												class="text-xs"
											>
												{rec.status}
												{#if rec.status !== 'active'}(exit {rec.exit_code}){/if}
											</Badge>
										</TableCell>
										<TableCell data-label={t('terminal.audit.colStarted')} class="text-xs whitespace-nowrap">
											{formatTime(rec.started_at)}
										</TableCell>
										<TableCell data-label={t('terminal.audit.colEnded')} class="text-muted-foreground text-xs whitespace-nowrap">
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
						itemLabel={t('terminal.audit.itemLabel')}
						class="rounded-lg border bg-card sm:rounded-none sm:border-0 sm:border-t mt-3 sm:mt-0"
					/>
				</div>
			{/if}
		</div>
	</DialogContent>
</Dialog>
