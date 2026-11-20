<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { cn } from '#lib/utils.js';
	import { toast } from 'svelte-sonner';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import HistoryIcon from '@lucide/svelte/icons/history';
	import CheckIcon from '@lucide/svelte/icons/check';
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
		}
	});

	async function copyLogs() {
		const text = cp.terminalLogs.map((l) => l.message).join('\n');
		if (!text) return;
		try {
			await navigator.clipboard.writeText(text);
			copied = true;
			toast.success('Log terminal disalin ke clipboard');
			setTimeout(() => (copied = false), 1500);
		} catch {
			toast.error('Gagal menyalin log ke clipboard');
		}
	}
</script>

<Dialog.Root bind:open={cp.terminalOpen}>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-3 sm:max-w-4xl">
		<Dialog.Header>
			<div class="flex flex-wrap items-center justify-between gap-2 pr-8">
				<div class="flex min-w-0 items-center gap-2.5">
					<TerminalIcon class="size-5 shrink-0 text-primary" />
					<div class="min-w-0">
						<Dialog.Title class="truncate">Live Terminal Console</Dialog.Title>
						<Dialog.Description class="truncate font-mono text-xs">
							{cp.currentTaskName}
						</Dialog.Description>
					</div>
				</div>
				<div class="flex items-center gap-2">
					{#if cp.lastTaskStatus === 'running'}
						<StatusBadge tone="info" label="Berjalan..." />
					{:else if cp.lastTaskStatus === 'success'}
						<StatusBadge tone="success" label="Selesai" />
					{:else if cp.lastTaskStatus === 'failed'}
						<StatusBadge tone="destructive" label="Gagal" />
					{:else}
						<StatusBadge tone="neutral" label={cp.sseConnected ? 'SSE Standby' : 'Menghubungkan...'} />
					{/if}
				</div>
			</div>
		</Dialog.Header>

		<Tabs.Root bind:value={activeTab} class="flex min-h-0 flex-1 flex-col gap-2">
			<div class="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
				<Tabs.List class="grid w-full grid-cols-2 sm:inline-flex sm:w-auto">
					<Tabs.Trigger value="live">Live Stream (SSE)</Tabs.Trigger>
					<Tabs.Trigger value="history">
						<HistoryIcon data-icon="inline-start" />
						Riwayat ({cp.recentTasks.length})
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
								Tersalin
							{:else}
								<CopyIcon data-icon="inline-start" />
								Salin Log
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
							Bersihkan
						</Button>
					</div>
				{/if}
			</div>

			<Tabs.Content value="live" class="mt-0 flex min-h-0 flex-1 flex-col">
				<!-- Scrollable live log panel needs tabindex="0" for keyboard scrolling -->
				<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
				<div
					bind:this={logContainer}
					role="log"
					aria-live="polite"
					aria-label="Output Konsol Terminal"
					tabindex="0"
					class="h-[46dvh] sm:h-[52dvh] w-full overflow-auto rounded-lg border bg-terminal p-3 sm:p-4 font-mono text-xs leading-relaxed text-terminal-foreground"
				>
					{#if cp.terminalLogs.length === 0}
						<p class="text-muted-foreground italic">
							Menunggu perintah... Pilih aksi instalasi, pengalihan runtime, atau pemeliharaan untuk melihat streaming log di sini.
						</p>
					{:else}
						{#each cp.terminalLogs as entry (entry.id)}
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
						{/each}
					{/if}
				</div>
			</Tabs.Content>

			<Tabs.Content
				value="history"
				class="mt-0 flex min-h-0 flex-1 flex-col gap-3 md:grid md:grid-cols-[260px_1fr]"
			>
				<div class="flex h-36 shrink-0 flex-col gap-1.5 overflow-y-auto rounded-lg border p-2 md:h-[52dvh]">
					{#if cp.recentTasks.length === 0}
						<p class="p-3 text-center text-xs text-muted-foreground">Belum ada riwayat tugas tersimpan.</p>
					{:else}
						{#each cp.recentTasks as t (t.id)}
							<button
								type="button"
								onclick={() => (selectedHistoryId = t.id)}
								class={cn(
									'flex flex-col gap-1 rounded-md border p-2.5 text-left transition-colors',
									selectedTask?.id === t.id
										? 'border-primary bg-accent text-accent-foreground'
										: 'border-transparent hover:bg-muted/60'
								)}
							>
								<div class="flex items-center justify-between gap-2">
									<span class="truncate text-xs font-semibold">{t.command}</span>
									<Badge
										variant={t.status === 'success'
											? 'secondary'
											: t.status === 'failed'
												? 'destructive'
												: 'outline'}
										class="shrink-0 text-[10px]"
									>
										{t.status}
									</Badge>
								</div>
								<span class="text-[11px] text-muted-foreground">
									{new Date(t.started_at).toLocaleString('id-ID')}
								</span>
							</button>
						{/each}
					{/if}
				</div>
				<pre
					class="h-48 min-h-0 flex-1 overflow-auto rounded-lg border bg-terminal p-3 sm:p-4 font-mono text-xs leading-relaxed text-terminal-foreground whitespace-pre-wrap break-words md:h-[52dvh]"
				>{selectedTask ? selectedTask.log_output || '(Log kosong)' : 'Pilih tugas untuk melihat output log.'}</pre>
			</Tabs.Content>
		</Tabs.Root>

		<Dialog.Footer class="sm:items-center sm:justify-between">
			<p class="text-xs text-muted-foreground">
				Streaming log langsung (SSE). Menutup jendela tidak menghentikan proses di background.
			</p>
			<Button type="button" onclick={() => (cp.terminalOpen = false)}>Tutup / Minimalkan</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
