<script lang="ts">
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import { cp, type TerminalLogEntry } from '#lib/stores/control-plane.svelte.js';
	import { cn } from '#lib/utils.js';
	import { toast } from 'svelte-sonner';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import HistoryIcon from '@lucide/svelte/icons/history';
	import CheckIcon from '@lucide/svelte/icons/check';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import { tick } from 'svelte';

	let activeTab = $state<'live' | 'history'>('live');
	let logContainer = $state<HTMLDivElement | null>(null);
	let copied = $state(false);
	let selectedHistoryId = $state<number | null>(null);

	const selectedTask = $derived(
		cp.recentTasks.find((t) => t.id === selectedHistoryId) ?? cp.recentTasks[0] ?? null
	);

	$effect(() => {
		const count = cp.terminalLogs.length;
		if (cp.terminalOpen && activeTab === 'live' && count > 0) {
			tick().then(() => {
				if (logContainer) {
					logContainer.scrollTop = logContainer.scrollHeight;
				}
			});
		}
	});

	$effect(() => {
		if (cp.terminalOpen && activeTab === 'history') {
			cp.loadTasks();
			if (cp.isTaskRunning) {
				const timer = setInterval(() => {
					cp.loadTasks();
				}, 2000);
				return () => clearInterval(timer);
			}
		}
	});

	async function copyText(text: string, msg: string) {
		if (!text) return;
		try {
			await navigator.clipboard.writeText(text);
			toast.success(msg);
			return true;
		} catch {
			toast.error(t('common.clipboard.genericCopyFailed'));
			return false;
		}
	}

	async function copyLogs() {
		const text = cp.terminalLogs.map((l) => l.message).join('\n');
		if (!text) return;
		if (await copyText(text, t('modals.terminalDialog.logsCopiedToast'))) {
			copied = true;
			setTimeout(() => (copied = false), 1500);
		}
	}
	const QR_LINE_RE = /^[█▀▄ ]{17,}$/;

	function isQRBlockLine(line: string): boolean {
		return QR_LINE_RE.test(line) && /[█▀▄]/.test(line);
	}

	type SegmentedBlock<T> =
		| { kind: 'qr'; text: string; key: string }
		| { kind: 'item'; item: T; key: string };

	// Gabungkan baris QR UTF-8 yang berurutan menjadi satu blok putih-hitam tanpa jeda antarbaris agar mudah dipindai kamera HP.
	function segmentQrBlocks<T>(
		items: readonly T[],
		getText: (item: T) => string,
		getKey: (item: T, idx: number) => string
	): SegmentedBlock<T>[] {
		const out: SegmentedBlock<T>[] = [];
		let i = 0;
		while (i < items.length) {
			if (isQRBlockLine(getText(items[i]))) {
				let j = i;
				while (j < items.length && isQRBlockLine(getText(items[j]))) {
					j++;
				}
				if (j - i >= 5) {
					const lines: string[] = [];
					for (let k = i; k < j; k++) {
						lines.push(getText(items[k]));
					}
					out.push({ kind: 'qr', text: lines.join('\n'), key: `qr-${getKey(items[i], i)}` });
					i = j;
					continue;
				}
			}
			out.push({ kind: 'item', item: items[i], key: getKey(items[i], i) });
			i++;
		}
		return out;
	}

	const liveBlocks = $derived(
		segmentQrBlocks(
			cp.terminalLogs,
			(e) => e.message,
			(e) => String(e.id)
		)
	);

	const historyBlocks = $derived(
		selectedTask?.log_output
			? segmentQrBlocks(
					selectedTask.log_output.split(/\r?\n/),
					(line) => line,
					(_line, idx) => String(idx)
				)
			: []
	);
</script>

{#snippet qrCodeBlock(text: string)}
	<div class="my-2 inline-block w-fit max-w-full overflow-x-auto rounded-lg border border-border/60 bg-white p-2.5 text-zinc-950 shadow-xs">
		<pre class="m-0 font-mono text-[8px] leading-none tracking-normal whitespace-pre select-all sm:text-[10px]">{text}</pre>
	</div>
{/snippet}

<Dialog.Root bind:open={cp.terminalOpen}>
	<Dialog.Content class="flex max-h-[90dvh] max-w-[calc(100vw-1.5rem)] flex-col gap-3 overflow-hidden sm:max-w-[min(calc(100vw-2rem),56rem)]">
		<Dialog.Header class="shrink-0">
			<div class="flex flex-wrap items-center justify-between gap-2 pr-8">
				<div class="flex min-w-0 items-center gap-2.5">
					<TerminalIcon class="size-5 shrink-0 text-primary" />
					<div class="min-w-0">
						<Dialog.Title class="truncate">{t('modals.terminalDialog.title')}</Dialog.Title>
						<Dialog.Description class="truncate font-mono text-xs">
							{cp.currentTaskName}
						</Dialog.Description>
					</div>
				</div>
				<div class="flex items-center gap-2">
					{#if cp.lastTaskStatus === 'running'}
						<StatusBadge tone="info" label={t('modals.terminalDialog.statusRunning')} />
					{:else if cp.lastTaskStatus === 'success'}
						<StatusBadge tone="success" label={t('modals.terminalDialog.statusSuccess')} />
					{:else if cp.lastTaskStatus === 'failed'}
						<StatusBadge tone="destructive" label={t('modals.terminalDialog.statusFailed')} />
					{:else}
						<StatusBadge
							tone="neutral"
							label={cp.sseConnected ? t('modals.terminalDialog.statusStandby') : t('modals.terminalDialog.statusConnecting')}
						/>
					{/if}
				</div>
			</div>
		</Dialog.Header>

		<Tabs.Root bind:value={activeTab} class="flex min-h-0 flex-1 flex-col gap-2">
			<div class="flex shrink-0 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
				<Tabs.List class="grid w-full grid-cols-2 sm:inline-flex sm:w-auto">
					<Tabs.Trigger value="live">{t('modals.terminalDialog.tabLive')}</Tabs.Trigger>
					<Tabs.Trigger value="history">
						<HistoryIcon data-icon="inline-start" />
						{t('modals.terminalDialog.tabHistory', { count: cp.recentTasks.length })}
					</Tabs.Trigger>
				</Tabs.List>

				{#if activeTab === 'live'}
					<div class="flex w-full items-center gap-2 sm:w-auto">
						<Button
							variant="outline"
							size="sm"
							class="flex-1 sm:flex-initial"
							disabled={cp.terminalLogs.length === 0}
							onclick={copyLogs}
						>
							{#if copied}
								<CheckIcon data-icon="inline-start" />
								{t('common.actions.copied')}
							{:else}
								<CopyIcon data-icon="inline-start" />
								{t('modals.terminalDialog.copyLogBtn')}
							{/if}
						</Button>
						<Button
							variant="outline"
							size="sm"
							class="flex-1 sm:flex-initial"
							disabled={cp.terminalLogs.length === 0}
							onclick={() => cp.clearTerminal()}
						>
							<Trash2Icon data-icon="inline-start" />
							{t('common.actions.clear')}
						</Button>
					</div>
				{/if}
			</div>

			<Tabs.Content value="live" class="mt-0 flex min-h-0 flex-1 flex-col gap-2">
				{#if cp.pendingPanelDomainURL && cp.lastTaskStatus !== 'failed'}
					<Alert.Root
						class={cn(
							'py-2.5',
							cp.lastTaskStatus === 'success'
								? 'border-emerald-500/40 bg-emerald-500/10'
								: 'border-primary/30 bg-primary/5'
						)}
					>
						<ShieldCheckIcon
							class={cn(
								'size-4',
								cp.lastTaskStatus === 'success'
									? 'text-emerald-600 dark:text-emerald-400'
									: 'text-primary'
							)}
						/>
						<Alert.Title class="text-xs sm:text-sm font-semibold">
							{cp.lastTaskStatus === 'success' ? t('modals.terminalDialog.panelReadyTitle') : t('modals.terminalDialog.panelTargetTitle')}
						</Alert.Title>
						<Alert.Description class="mt-1 space-y-2 text-xs">
							<p>
								{cp.lastTaskStatus === 'success'
									? t('modals.terminalDialog.panelReadyDesc')
									: t('modals.terminalDialog.panelTargetDesc')}
							</p>
							<div class="flex flex-wrap items-center gap-2">
								<code class="bg-background rounded border px-2 py-1 font-mono text-xs">{cp.pendingPanelDomainURL}</code>
								<Button
									size="sm"
									variant="outline"
									class="h-7 px-2 text-xs"
									onclick={() => copyText(cp.pendingPanelDomainURL, t('modals.panelDomain.urlCopied'))}
								>
									<CopyIcon class="size-3.5" />
									{t('common.actions.copy')}
								</Button>
								{#if cp.lastTaskStatus === 'success'}
									<Button
										size="sm"
										class="h-7 px-2 text-xs"
										href={cp.pendingPanelDomainURL}
										target="_blank"
										rel="noopener"
									>
										<ExternalLinkIcon class="size-3.5" />
										{t('modals.panelDomain.openNewPanelBtn')}
									</Button>
								{/if}
							</div>
						</Alert.Description>
					</Alert.Root>
				{/if}

				<!-- Scrollable live log panel needs tabindex="0" for keyboard scrolling -->
				<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
				<div
					bind:this={logContainer}
					role="log"
					aria-live="polite"
					aria-label={t('modals.terminalDialog.outputAriaLabel')}
					tabindex="0"
					class="min-h-36 flex-1 w-full overflow-auto overscroll-contain rounded-lg border bg-terminal p-3 sm:min-h-[45dvh] sm:p-4 font-mono text-xs leading-relaxed text-terminal-foreground"
				>
					{#if cp.terminalLogs.length === 0}
						<p class="text-muted-foreground italic">
							{t('modals.terminalDialog.emptyLogs')}
						</p>
					{:else}
						{#each liveBlocks as block (block.key)}
							{#if block.kind === 'qr'}
								{@render qrCodeBlock(block.text)}
							{:else}
								{@const entry = block.item}
								<div
									class={cn(
										'whitespace-pre-wrap break-words py-0.5',
										entry.type === 'start' && 'font-semibold text-info',
										entry.type === 'finish' && entry.status === 'success' && 'font-semibold text-success',
										entry.type === 'finish' && entry.status !== 'success' && 'font-semibold text-destructive'
									)}
								>
									{entry.message}
								</div>
							{/if}
						{/each}
					{/if}
				</div>
			</Tabs.Content>

			<Tabs.Content
				value="history"
				class="mt-0 flex min-h-0 flex-1 flex-col gap-3 md:grid md:grid-cols-[260px_1fr]"
			>
				<div class="flex h-32 shrink-0 flex-col gap-1.5 overflow-y-auto overscroll-contain rounded-lg border p-2 md:h-[50dvh]">
					{#if cp.recentTasks.length === 0}
						<p class="p-3 text-center text-xs text-muted-foreground">{t('modals.terminalDialog.noHistory')}</p>
					{:else}
						{#each cp.recentTasks as task (task.id)}
							<button
								type="button"
								onclick={() => (selectedHistoryId = task.id)}
								class={cn(
									'flex flex-col gap-1 rounded-md border p-2.5 text-left transition-colors',
									selectedTask?.id === task.id
										? 'border-primary bg-accent text-accent-foreground'
										: 'border-transparent hover:bg-muted/60'
								)}
							>
								<div class="flex items-center justify-between gap-2">
									<span class="truncate text-xs font-semibold">{task.command}</span>
									<Badge
										variant={task.status === 'success'
											? 'secondary'
											: task.status === 'failed'
												? 'destructive'
												: 'outline'}
										class="shrink-0 text-[10px]"
									>
										{task.status}
									</Badge>
								</div>
								<span class="text-[11px] text-muted-foreground">
									{i18n.formatDateTime(task.started_at)}
								</span>
							</button>
						{/each}
					{/if}
				</div>
				<div
					class="min-h-36 flex-1 overflow-auto overscroll-contain rounded-lg border bg-terminal p-3 sm:p-4 font-mono text-xs leading-relaxed text-terminal-foreground md:h-[50dvh]"
				>
					{#if !selectedTask}
						<p class="text-muted-foreground italic">{t('modals.terminalDialog.selectTaskPrompt')}</p>
					{:else if !selectedTask.log_output}
						<p class="text-muted-foreground italic">{t('modals.terminalDialog.emptyTaskLog')}</p>
					{:else}
						{#each historyBlocks as block (block.key)}
							{#if block.kind === 'qr'}
								{@render qrCodeBlock(block.text)}
							{:else}
								<div class="whitespace-pre-wrap break-words">{block.item || '\u00a0'}</div>
							{/if}
						{/each}
					{/if}
				</div>
			</Tabs.Content>
		</Tabs.Root>

		<Dialog.Footer class="shrink-0 pb-[max(1rem,env(safe-area-inset-bottom,0px))] sm:items-center sm:justify-between">
			<p class="text-xs text-muted-foreground">
				{t('modals.terminalDialog.footerHint')}
			</p>
			<Button type="button" class="min-h-10 sm:min-h-0" onclick={() => (cp.terminalOpen = false)}>
				{t('modals.terminalDialog.closeMinimizeBtn')}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
