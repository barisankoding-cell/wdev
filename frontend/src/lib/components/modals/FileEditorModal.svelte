<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import type { FileEntry } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import FileCodeIcon from '@lucide/svelte/icons/file-code';
	import ImageIcon from '@lucide/svelte/icons/image';
	import CheckIcon from '@lucide/svelte/icons/check';

	let {
		open = $bindable(false),
		entry = null,
		onSaved
	}: {
		open?: boolean;
		entry?: FileEntry | null;
		onSaved?: () => void;
	} = $props();

	let loading = $state(false);
	let saving = $state(false);
	let loadError = $state('');
	let content = $state('');
	let initialContent = $state('');
	let fileMode = $state('0644');
	let wrapLines = $state(false);

	let textareaEl = $state<HTMLTextAreaElement | null>(null);
	let gutterEl = $state<HTMLDivElement | null>(null);

	const isImageMode = $derived(Boolean(entry?.is_image && entry?.ext !== '.svg'));
	const isDirty = $derived(!isImageMode && content !== initialContent);
	const lineNumbers = $derived.by(() => {
		const count = Math.max(1, content.split('\n').length);
		return Array.from({ length: count }, (_, i) => i + 1);
	});

	$effect(() => {
		if (open && entry) {
			loadError = '';
			if (entry.is_image && entry.ext !== '.svg') {
				loading = false;
				content = '';
				initialContent = '';
			} else {
				void fetchTextContent(entry.path);
			}
		}
	});

	async function fetchTextContent(path: string) {
		loading = true;
		loadError = '';
		try {
			const res = await api.readFileContent(path);
			content = res.content;
			initialContent = res.content;
			fileMode = res.mode || '0644';
		} catch (err) {
			loadError = err instanceof Error ? err.message : t('files.editor.readFailed');
			content = '';
			initialContent = '';
		} finally {
			loading = false;
		}
	}

	async function saveFile() {
		if (!entry || isImageMode || saving) return;
		saving = true;
		try {
			await api.writeFileContent(entry.path, content);
			initialContent = content;
			toast.success(t('files.editor.savedToast', { name: entry.name }));
			onSaved?.();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('files.editor.saveFailed'));
		} finally {
			saving = false;
		}
	}

	function handleKeyDown(e: KeyboardEvent) {
		if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
			e.preventDefault();
			void saveFile();
			return;
		}
		if (e.key === 'Tab' && textareaEl) {
			e.preventDefault();
			const start = textareaEl.selectionStart;
			const end = textareaEl.selectionEnd;
			const indent = '    ';
			content = content.slice(0, start) + indent + content.slice(end);
			queueMicrotask(() => {
				if (textareaEl) {
					textareaEl.selectionStart = textareaEl.selectionEnd = start + indent.length;
				}
			});
		}
	}

	function syncScroll() {
		if (gutterEl && textareaEl) {
			gutterEl.scrollTop = textareaEl.scrollTop;
		}
	}

	function requestClose() {
		open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content
		class="flex h-[100dvh] max-h-[100dvh] w-full max-w-full flex-col gap-3 rounded-none pt-[max(0.75rem,env(safe-area-inset-top,0px))] sm:h-[88dvh] sm:max-h-[88dvh] sm:max-w-5xl sm:rounded-xl sm:pt-4"
	>
		<Dialog.Header>
			<div class="flex flex-wrap items-center justify-between gap-2 pr-8">
				<Dialog.Title class="flex min-w-0 items-center gap-2">
					{#if isImageMode}
						<ImageIcon class="size-4 shrink-0 text-primary" />
					{:else}
						<FileCodeIcon class="size-4 shrink-0 text-primary" />
					{/if}
					<span class="truncate font-mono text-sm sm:text-base">{entry?.name || t('files.editor.fileFallback')}</span>
					{#if isDirty}
						<Badge variant="outline" class="border-warning/40 bg-warning/15 text-[10px] text-warning">
							{t('files.editor.unsavedBadge')}
						</Badge>
					{/if}
				</Dialog.Title>
				<div class="flex flex-wrap items-center gap-1.5">
					{#if !isImageMode}
						<Badge variant="outline" class="font-mono text-[11px]">{fileMode}</Badge>
						<Button
							type="button"
							size="xs"
							variant={wrapLines ? 'secondary' : 'outline'}
							class="fm-touch-btn sm:min-h-0"
							onclick={() => (wrapLines = !wrapLines)}
						>
							{t('files.editor.wordWrap')}
						</Button>
					{/if}
					{#if entry}
						<Button
							type="button"
							size="xs"
							variant="outline"
							class="fm-touch-btn sm:min-h-0"
							href={api.fileDownloadUrl([entry.path])}
							target="_blank"
							rel="noopener noreferrer"
						>
							<DownloadIcon data-icon="inline-start" />
							{t('files.editor.downloadBtn')}
						</Button>
					{/if}
				</div>
			</div>
			<Dialog.Description class="break-all font-mono text-xs">{entry?.path || '-'}</Dialog.Description>
		</Dialog.Header>

		{#if loadError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>{t('files.editor.cannotOpenTitle')}</Alert.Title>
				<Alert.Description>{loadError}</Alert.Description>
			</Alert.Root>
		{:else if loading}
			<div class="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground">
				<Spinner />
				<span>{t('files.editor.loadingFile')}</span>
			</div>
		{:else if isImageMode && entry}
			<div class="flex min-h-0 flex-1 items-center justify-center overflow-auto overscroll-contain rounded-lg border bg-muted/30 p-3 sm:p-4">
				<img
					src={api.fileRawUrl(entry.path)}
					alt={entry.name}
					class="max-h-full max-w-full rounded object-contain shadow-sm"
				/>
			</div>
		{:else}
			<div class="flex min-h-0 flex-1 overflow-hidden rounded-lg border bg-background font-mono text-base leading-6 sm:text-[13px] sm:leading-5">
				<div
					bind:this={gutterEl}
					aria-hidden="true"
					class="w-9 shrink-0 select-none overflow-hidden border-r bg-muted/40 py-3 pr-1.5 text-right leading-6 text-muted-foreground sm:w-12 sm:pr-2.5 sm:leading-5"
				>
					{#each lineNumbers as ln (ln)}
						<div>{ln}</div>
					{/each}
				</div>
				<textarea
					bind:this={textareaEl}
					bind:value={content}
					onkeydown={handleKeyDown}
					onscroll={syncScroll}
					aria-label={t('files.editor.contentAria', { name: entry?.name || '' })}
					spellcheck={false}
					autocorrect="off"
					autocapitalize="none"
					autocomplete="off"
					class="min-h-0 flex-1 resize-none bg-transparent p-3 leading-6 outline-none sm:leading-5 {wrapLines
						? 'whitespace-pre-wrap break-words'
						: 'whitespace-pre overflow-x-auto'}"
				></textarea>
			</div>
		{/if}

		<Dialog.Footer
			class="flex-row flex-wrap items-center justify-between gap-2 pb-[max(0.75rem,env(safe-area-inset-bottom,0px))] sm:justify-between sm:rounded-b-xl sm:pb-4"
		>
			<span class="hidden text-[11px] text-muted-foreground sm:inline">
				{#if isImageMode}
					{t('files.editor.imagePreviewHint')}
				{:else}
					{t('files.editor.tipsPart1')}<kbd class="rounded border px-1 py-0.5 font-mono text-[10px]">Ctrl+S</kbd>{t('files.editor.tipsPart2')}<kbd class="rounded border px-1 py-0.5 font-mono text-[10px]">Tab</kbd>{t('files.editor.tipsPart3')}
				{/if}
			</span>
			<div class="flex w-full items-center gap-2 sm:w-auto sm:justify-end">
				<Button type="button" variant="outline" class="min-h-10 flex-1 sm:min-h-0 sm:flex-initial" onclick={requestClose}>
					{t('files.editor.closeBtn')}
				</Button>
				{#if !isImageMode && !loadError}
					<Button
						type="button"
						class="min-h-10 flex-1 sm:min-h-0 sm:flex-initial"
						disabled={loading || saving || !isDirty}
						onclick={saveFile}
					>
						{#if saving}
							<Spinner data-icon="inline-start" />
							{t('files.editor.savingBtn')}
						{:else}
							<CheckIcon data-icon="inline-start" />
							{t('files.editor.saveFileBtn')}
						{/if}
					</Button>
				{/if}
			</div>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
