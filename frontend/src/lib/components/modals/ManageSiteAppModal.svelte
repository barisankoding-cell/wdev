<script lang="ts">
	import { tick, untrack } from 'svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { SiteAppEnvEntry, SiteRecord } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import CheckIcon from '@lucide/svelte/icons/check';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import HammerIcon from '@lucide/svelte/icons/hammer';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import PauseIcon from '@lucide/svelte/icons/pause';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import PlayIcon from '@lucide/svelte/icons/play';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import RotateCwIcon from '@lucide/svelte/icons/rotate-cw';
	import SaveIcon from '@lucide/svelte/icons/save';
	import SquareIcon from '@lucide/svelte/icons/square';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';

	let {
		open = $bindable(false),
		site = null,
		onUpdated,
		onEditVhost
	}: {
		open: boolean;
		site: SiteRecord | null;
		onUpdated?: () => void | Promise<void>;
		onEditVhost?: (site: SiteRecord) => void;
	} = $props();

	let activeTab = $state<'overview' | 'env' | 'logs'>('overview');
	let actionPending = $state<string | null>(null);

	// Env state
	let envLoading = $state(false);
	let envSaving = $state(false);
	let envFile = $state('');
	let envMode = $state<'pairs' | 'raw'>('pairs');
	let envEntries = $state<SiteAppEnvEntry[]>([]);
	let envRaw = $state('');
	let envError = $state('');
	let envLoadedForDomain = '';

	// Logs state
	let logsLoading = $state(false);
	let logLines = $state<string[]>([]);
	let logStreamLive = $state(false);
	let logContainer = $state<HTMLPreElement | null>(null);
	let logEventSource: EventSource | null = null;
	let copiedLogs = $state(false);

	const app = $derived(site?.app);
	const status = $derived(site?.app_status);
	const runtimeLabel = $derived(
		site?.framework === 'nodejs' ? 'Node.js' : site?.framework === 'go' ? 'Go' : site?.framework === 'rust' ? 'Rust' : site?.framework || ''
	);

	const statusTone = $derived.by((): 'success' | 'info' | 'destructive' | 'neutral' => {
		const st = status?.active_state;
		if (st === 'active') return 'success';
		if (st === 'activating' || st === 'reloading') return 'info';
		if (st === 'failed') return 'destructive';
		return 'neutral';
	});

	const statusText = $derived.by(() => {
		const st = status?.active_state || 'inactive';
		if (st === 'active') return 'Active (Running)';
		if (st === 'activating') return 'Starting...';
		if (st === 'failed') return 'Failed';
		return 'Stopped';
	});

	function formatBytes(bytes?: number): string {
		if (!bytes || bytes <= 0) return '-';
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
		return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
	}

	function stopLogStream() {
		if (logEventSource) {
			logEventSource.close();
			logEventSource = null;
		}
		logStreamLive = false;
	}

	$effect(() => {
		if (!open || !site) {
			untrack(() => {
				stopLogStream();
				envLoadedForDomain = '';
				activeTab = 'overview';
				logLines = [];
			});
		}
	});

	$effect(() => {
		if (!open || !site) return;
		const dom = site.domain;
		if (activeTab === 'env' && envLoadedForDomain !== dom) {
			untrack(() => {
				void loadEnv(dom);
			});
		}
		if (activeTab === 'logs' && !logStreamLive && logLines.length === 0) {
			untrack(() => {
				void startLiveLogs(dom);
			});
		}
		if (activeTab !== 'logs' && logStreamLive) {
			untrack(() => {
				stopLogStream();
			});
		}
	});

	async function runAction(action: 'start' | 'stop' | 'restart' | 'enable' | 'disable') {
		if (!site) return;
		actionPending = action;
		try {
			const res = await cp.runWithSudoRetry(
				`Mengontrol service systemd untuk ${site.domain} memerlukan hak akses administrator (sudo).`,
				(sudoPass) => api.siteAppAction(site.domain, action, sudoPass)
			);
			if (!res) return;
			const labels: Record<string, string> = {
				start: 'Service aplikasi dijalankan',
				stop: 'Service aplikasi dihentikan',
				restart: 'Service aplikasi di-restart',
				enable: 'Auto-start boot diaktifkan',
				disable: 'Auto-start boot dinonaktifkan'
			};
			toast.success(labels[action] || 'Aksi berhasil');
			if (onUpdated) await onUpdated();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : `Gagal menjalankan ${action}`);
		} finally {
			actionPending = null;
		}
	}

	async function triggerBuild() {
		if (!site) return;
		if (!app?.build_command) {
			toast.warning('Aplikasi ini belum memiliki Build Command. Edit VHost untuk menambahkannya.');
			return;
		}
		actionPending = 'build';
		try {
			const res = await cp.runWithSudoRetry(
				`Menjalankan build & restart service ${site.domain} memerlukan hak akses administrator (sudo).`,
				(sudoPass) => api.buildSiteApp(site.domain, true, sudoPass)
			);
			if (!res) return;
			cp.openTerminal(`Build ${site.domain} (${runtimeLabel})`);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memulai build');
		} finally {
			actionPending = null;
		}
	}

	async function loadEnv(dom = site?.domain || '') {
		if (!dom) return;
		envLoading = true;
		envError = '';
		try {
			const res = await api.getSiteAppEnv(dom);
			if (res.status === 'sudo_required') {
				const sudoPass = await cp.requestSudo(
					res.message || 'Membaca file .env (0600) memerlukan hak akses administrator (sudo).'
				);
				if (!sudoPass) return;
				await api.verifySudo(sudoPass).catch(() => {});
				const retryRes = await api.getSiteAppEnv(dom);
				envFile = retryRes.env_file || app?.env_file || '';
				envEntries = retryRes.entries ? [...retryRes.entries] : [];
				envRaw = retryRes.raw || '';
			} else {
				envFile = res.env_file || app?.env_file || '';
				envEntries = res.entries ? [...res.entries] : [];
				envRaw = res.raw || '';
			}
			envLoadedForDomain = dom;
		} catch (err) {
			envError = err instanceof Error ? err.message : 'Gagal memuat variabel environment';
		} finally {
			envLoading = false;
		}
	}

	function addEnvRow() {
		envEntries = [...envEntries, { key: '', value: '' }];
	}

	function removeEnvRow(idx: number) {
		envEntries = envEntries.filter((_, i) => i !== idx);
	}

	async function saveEnv(restart: boolean) {
		if (!site) return;
		envSaving = true;
		envError = '';
		try {
			const payload =
				envMode === 'raw'
					? { domain: site.domain, raw: envRaw, restart }
					: {
							domain: site.domain,
							entries: envEntries.filter((e) => e.key.trim() !== ''),
							restart
						};
			const res = await cp.runWithSudoRetry(
				`Menyimpan file .env (0600) untuk ${site.domain} memerlukan hak akses administrator (sudo).`,
				(sudoPass) => api.saveSiteAppEnv({ ...payload, ...(sudoPass ? { sudo_pass: sudoPass } : {}) })
			);
			if (!res) return;
			envEntries = res.entries ? [...res.entries] : envEntries;
			envRaw = res.raw ?? envRaw;
			toast.success(
				restart ? 'Environment .env disimpan dan service di-restart.' : 'Environment .env disimpan (0600).'
			);
			if (restart && onUpdated) await onUpdated();
		} catch (err) {
			envError = err instanceof Error ? err.message : 'Gagal menyimpan environment';
		} finally {
			envSaving = false;
		}
	}

	async function loadSnapshotLogs(dom = site?.domain || '') {
		if (!dom) return;
		logsLoading = true;
		try {
			const res = await api.getSiteAppLogs(dom, 200);
			if (res.status === 'sudo_required') {
				const sudoPass = await cp.requestSudo(res.message || 'Membaca log membutuhkan akses sudo.');
				if (!sudoPass) return;
				const retry = await api.getSiteAppLogs(dom, 200);
				logLines = (retry.logs || '').split('\n').filter(Boolean);
			} else {
				logLines = (res.logs || '').split('\n').filter(Boolean);
			}
			await scrollLogsBottom();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Gagal memuat snapshot log');
		} finally {
			logsLoading = false;
		}
	}

	function startLiveLogs(dom = site?.domain || '') {
		if (!dom) return;
		stopLogStream();
		logLines = [];
		const es = new EventSource(api.siteAppLogsStreamUrl(dom, 120));
		logEventSource = es;
		logStreamLive = true;

		es.onmessage = (ev) => {
			try {
				const parsed = JSON.parse(ev.data) as { type: string; message: string };
				if (parsed.type === 'log') {
					logLines = [...logLines.slice(-800), parsed.message];
					void scrollLogsBottom();
				} else if (parsed.type === 'error') {
					logLines = [...logLines, `[ERROR] ${parsed.message}`];
					void scrollLogsBottom();
				} else if (parsed.type === 'finish') {
					stopLogStream();
				}
			} catch {
				// ignore malformed frame
			}
		};

		es.onerror = () => {
			stopLogStream();
		};
	}

	async function scrollLogsBottom() {
		await tick();
		if (logContainer) {
			logContainer.scrollTop = logContainer.scrollHeight;
		}
	}

	async function copyAllLogs() {
		const text = logLines.join('\n');
		if (!text) return;
		try {
			await navigator.clipboard.writeText(text);
			copiedLogs = true;
			toast.success('Log aplikasi disalin ke clipboard');
			setTimeout(() => (copiedLogs = false), 1500);
		} catch {
			toast.error('Gagal menyalin log');
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="flex max-h-[92dvh] max-w-[calc(100vw-1.25rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
		<Dialog.Header class="shrink-0 border-b px-5 pt-5 pb-3 pr-10">
			<div class="flex flex-wrap items-center gap-2">
				<CpuIcon class="size-4 shrink-0 text-primary" />
				<Dialog.Title class="truncate text-base sm:text-lg">{site?.domain || 'Managed App'}</Dialog.Title>
				{#if runtimeLabel}
					<Badge variant="secondary">{runtimeLabel}</Badge>
				{/if}
				<StatusBadge tone={statusTone} label={statusText} />
			</div>
			<Dialog.Description class="truncate font-mono text-xs">
				{app?.service_name ? `${app.service_name}.service` : ''}
				{app?.port ? ` · 127.0.0.1:${app.port}` : ''}
			</Dialog.Description>
		</Dialog.Header>

		<div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 py-4">
			<Tabs.Root bind:value={activeTab} class="flex flex-col gap-4">
				<Tabs.List class="grid w-full grid-cols-3">
					<Tabs.Trigger value="overview" class="text-xs sm:text-sm">Kontrol &amp; Info</Tabs.Trigger>
					<Tabs.Trigger value="env" class="text-xs sm:text-sm">Environment (.env)</Tabs.Trigger>
					<Tabs.Trigger value="logs" class="text-xs sm:text-sm">Logs (journalctl)</Tabs.Trigger>
				</Tabs.List>

				<Tabs.Content value="overview" class="flex flex-col gap-4">
					<div class="grid grid-cols-2 gap-2 sm:grid-cols-4">
						<Button
							variant="outline"
							size="sm"
							class="min-h-10 sm:min-h-0"
							disabled={actionPending !== null || status?.active_state === 'active'}
							onclick={() => runAction('start')}
						>
							{#if actionPending === 'start'}
								<Spinner data-icon="inline-start" />
							{:else}
								<PlayIcon data-icon="inline-start" />
							{/if}
							Start
						</Button>
						<Button
							variant="outline"
							size="sm"
							class="min-h-10 sm:min-h-0"
							disabled={actionPending !== null}
							onclick={() => runAction('restart')}
						>
							{#if actionPending === 'restart'}
								<Spinner data-icon="inline-start" />
							{:else}
								<RotateCwIcon data-icon="inline-start" />
							{/if}
							Restart
						</Button>
						<Button
							variant="outline"
							size="sm"
							class="min-h-10 sm:min-h-0"
							disabled={actionPending !== null || status?.active_state !== 'active'}
							onclick={() => runAction('stop')}
						>
							{#if actionPending === 'stop'}
								<Spinner data-icon="inline-start" />
							{:else}
								<SquareIcon data-icon="inline-start" />
							{/if}
							Stop
						</Button>
						<Button
							variant="default"
							size="sm"
							class="min-h-10 sm:min-h-0"
							disabled={actionPending !== null || !app?.build_command}
							onclick={triggerBuild}
						>
							{#if actionPending === 'build'}
								<Spinner data-icon="inline-start" />
							{:else}
								<HammerIcon data-icon="inline-start" />
							{/if}
							Build &amp; Restart
						</Button>
					</div>

					<div class="grid gap-3 rounded-lg border bg-card p-3.5 sm:grid-cols-2">
						<div class="flex flex-col gap-1">
							<span class="text-[11px] font-medium text-muted-foreground">Unit Systemd</span>
							<code class="break-all font-mono text-xs text-foreground">{app?.service_name}.service</code>
						</div>
						<div class="flex flex-col gap-1">
							<span class="text-[11px] font-medium text-muted-foreground">Target Reverse Proxy</span>
							<code class="font-mono text-xs text-foreground">http://127.0.0.1:{app?.port || site?.proxy_port}</code>
						</div>
						<div class="flex flex-col gap-1 sm:col-span-2">
							<span class="text-[11px] font-medium text-muted-foreground">Working Directory</span>
							<div class="flex items-center gap-1.5">
								<FolderOpenIcon class="size-3.5 shrink-0 text-muted-foreground" />
								<code class="break-all font-mono text-xs text-foreground">{app?.working_dir || site?.root_path}</code>
							</div>
						</div>
						{#if app?.build_command}
							<div class="flex flex-col gap-1 sm:col-span-2">
								<span class="text-[11px] font-medium text-muted-foreground">Build Command</span>
								<code class="break-all rounded bg-muted px-2 py-1 font-mono text-xs text-foreground">{app.build_command}</code>
							</div>
						{/if}
						<div class="flex flex-col gap-1 sm:col-span-2">
							<span class="text-[11px] font-medium text-muted-foreground">Start Command (ExecStart)</span>
							<code class="break-all rounded bg-muted px-2 py-1 font-mono text-xs text-foreground">{app?.start_command || '-'}</code>
						</div>
						<div class="flex flex-col gap-1">
							<span class="text-[11px] font-medium text-muted-foreground">Run As User</span>
							<span class="font-mono text-xs text-foreground">{app?.run_user || '-'}</span>
						</div>
						<div class="flex flex-col gap-1">
							<span class="text-[11px] font-medium text-muted-foreground">PID &amp; Memori</span>
							<span class="font-mono text-xs text-foreground">
								{status?.main_pid ? `PID ${status.main_pid}` : 'Belum berjalan'} · {formatBytes(status?.memory_bytes)}
							</span>
						</div>
					</div>

					<div class="flex flex-col gap-3 rounded-lg border bg-muted/30 p-3.5 sm:flex-row sm:items-center sm:justify-between">
						<div class="flex flex-col gap-0.5">
							<span class="text-sm font-medium">Auto-Start Saat Boot (`systemd enable`)</span>
							<span class="text-xs text-muted-foreground">
								Otomatis menjalankan service aplikasi saat server dinyalakan ulang.
							</span>
						</div>
						<Switch
							checked={status?.enabled ?? app?.auto_start ?? false}
							disabled={actionPending !== null}
							onCheckedChange={(checked) => runAction(checked ? 'enable' : 'disable')}
							aria-label="Toggle auto-start saat boot"
						/>
					</div>
				</Tabs.Content>

				<Tabs.Content value="env" class="flex flex-col gap-4">
					<Alert.Root>
						<KeyRoundIcon />
						<Alert.Title>File Environment Terkelola (0600)</Alert.Title>
						<Alert.Description class="text-xs">
							Disimpan di <code class="font-mono">{envFile || app?.env_file || '/etc/wdev/apps/...'}</code>. Variabel <code>PORT</code> selalu diselaraskan dengan port Nginx VHost (<code>{app?.port}</code>).
						</Alert.Description>
					</Alert.Root>

					{#if envError}
						<Alert.Root variant="destructive">
							<Alert.Description class="text-xs">{envError}</Alert.Description>
						</Alert.Root>
					{/if}

					<div class="flex flex-wrap items-center justify-between gap-2">
						<div class="flex items-center gap-2">
							<Button
								type="button"
								size="xs"
								variant={envMode === 'pairs' ? 'default' : 'outline'}
								onclick={() => (envMode = 'pairs')}
							>
								Key-Value
							</Button>
							<Button
								type="button"
								size="xs"
								variant={envMode === 'raw' ? 'default' : 'outline'}
								onclick={() => {
									if (envEntries.length > 0 && !envRaw.trim()) {
										envRaw = envEntries.map((e) => `${e.key}=${e.value}`).join('\n');
									}
									envMode = 'raw';
								}}
							>
								Raw Text
							</Button>
						</div>
						<Button type="button" size="xs" variant="outline" disabled={envLoading} onclick={() => loadEnv()}>
							<RefreshCwIcon data-icon="inline-start" />
							Muat Ulang
						</Button>
					</div>

					{#if envLoading}
						<div class="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
							<Spinner />
							<span>Memuat variabel environment...</span>
						</div>
					{:else if envMode === 'pairs'}
						<Field.FieldGroup class="gap-2.5">
							{#each envEntries as entry, idx (idx)}
								<div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,0.9fr)_minmax(0,1.4fr)_auto] sm:items-center">
									<Input
										class="h-10 font-mono text-base sm:h-9 sm:text-xs"
										placeholder="KEY"
										autocorrect="off"
										autocapitalize="none"
										spellcheck={false}
										bind:value={entry.key}
									/>
									<Input
										class="h-10 font-mono text-base sm:h-9 sm:text-xs"
										placeholder="value"
										autocorrect="off"
										autocapitalize="none"
										spellcheck={false}
										bind:value={entry.value}
									/>
									<Button
										type="button"
										variant="ghost"
										size="icon"
										class="justify-self-end text-muted-foreground hover:text-destructive"
										onclick={() => removeEnvRow(idx)}
										aria-label="Hapus variabel"
									>
										<Trash2Icon />
									</Button>
								</div>
							{/each}
							<div>
								<Button type="button" size="xs" variant="outline" onclick={addEnvRow}>
									<PlusIcon data-icon="inline-start" />
									Tambah Variabel
								</Button>
							</div>
						</Field.FieldGroup>
					{:else}
						<Field.Field>
							<Field.FieldLabel for="site-app-env-raw">Isi File .env</Field.FieldLabel>
							<Textarea
								id="site-app-env-raw"
								rows={10}
								class="font-mono text-xs"
								placeholder="NODE_ENV=production&#10;DATABASE_URL=postgres://..."
								bind:value={envRaw}
							/>
						</Field.Field>
					{/if}

					<div class="flex flex-col gap-2 sm:flex-row sm:justify-end">
						<Button type="button" variant="outline" disabled={envSaving} onclick={() => saveEnv(false)}>
							<SaveIcon data-icon="inline-start" />
							Simpan Saja
						</Button>
						<Button type="button" disabled={envSaving} onclick={() => saveEnv(true)}>
							{#if envSaving}
								<Spinner data-icon="inline-start" />
								Menyimpan...
							{:else}
								<RotateCwIcon data-icon="inline-start" />
								Simpan &amp; Restart Service
							{/if}
						</Button>
					</div>
				</Tabs.Content>

				<Tabs.Content value="logs" class="flex flex-col gap-3">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<div class="flex items-center gap-2">
							<Button
								type="button"
								size="xs"
								variant={logStreamLive ? 'default' : 'outline'}
								onclick={() => (logStreamLive ? stopLogStream() : startLiveLogs())}
							>
								{#if logStreamLive}
									<PauseIcon data-icon="inline-start" />
									Pause Stream
								{:else}
									<TerminalIcon data-icon="inline-start" />
									Live Stream
								{/if}
							</Button>
							<Button type="button" size="xs" variant="outline" disabled={logsLoading} onclick={() => loadSnapshotLogs()}>
								<RefreshCwIcon data-icon="inline-start" />
								Snapshot
							</Button>
						</div>
						<Button type="button" size="xs" variant="outline" disabled={logLines.length === 0} onclick={copyAllLogs}>
							{#if copiedLogs}
								<CheckIcon data-icon="inline-start" />
								Disalin
							{:else}
								<CopyIcon data-icon="inline-start" />
								Salin Log
							{/if}
						</Button>
					</div>

					<pre
						bind:this={logContainer}
						class="h-[44dvh] sm:h-80 overflow-y-auto whitespace-pre-wrap break-all rounded-lg border bg-muted/60 p-3 font-mono text-xs text-foreground"
					>{#if logLines.length > 0}{logLines.join('\n')}{:else if logsLoading}Memuat log...{:else}Belum ada log journalctl untuk service ini.{/if}</pre>
				</Tabs.Content>
			</Tabs.Root>
		</div>

		<Separator />

		<Dialog.Footer class="flex flex-col-reverse gap-2 px-5 py-3 sm:flex-row sm:items-center sm:justify-between">
			{#if site && onEditVhost}
				<Button
					type="button"
					variant="outline"
					class="min-h-10 sm:min-h-0"
					onclick={() => {
						const target = site;
						open = false;
						if (target) onEditVhost(target);
					}}
				>
					<PencilIcon data-icon="inline-start" />
					Ubah Perintah / Port VHost
				</Button>
			{/if}
			<Button type="button" variant="secondary" class="min-h-10 sm:min-h-0" onclick={() => (open = false)}>
				Tutup
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
