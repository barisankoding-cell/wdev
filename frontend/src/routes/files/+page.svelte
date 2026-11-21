<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api/client.js';
	import FileEditorModal from '#lib/components/modals/FileEditorModal.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import { Alert, AlertDescription, AlertTitle } from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle
	} from '#lib/components/ui/card/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle
	} from '#lib/components/ui/dialog/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import { Field, FieldDescription, FieldLabel } from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import {
		Table,
		TableBody,
		TableCell,
		TableHead,
		TableHeader,
		TableRow
	} from '#lib/components/ui/table/index.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { FileEntry, FileListResult, FileRootInfo } from '#lib/types/api.js';
	import ArrowDownIcon from '@lucide/svelte/icons/arrow-down';
	import ArrowUpDownIcon from '@lucide/svelte/icons/arrow-up-down';
	import ArrowUpIcon from '@lucide/svelte/icons/arrow-up';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import ClipboardPasteIcon from '@lucide/svelte/icons/clipboard-paste';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import EyeIcon from '@lucide/svelte/icons/eye';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';
	import FileArchiveIcon from '@lucide/svelte/icons/file-archive';
	import FileCodeIcon from '@lucide/svelte/icons/file-code';
	import FileIcon from '@lucide/svelte/icons/file';
	import FilePlusIcon from '@lucide/svelte/icons/file-plus';
	import FileTextIcon from '@lucide/svelte/icons/file-text';
	import FolderIcon from '@lucide/svelte/icons/folder';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import FolderPlusIcon from '@lucide/svelte/icons/folder-plus';
	import FolderSymlinkIcon from '@lucide/svelte/icons/folder-symlink';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import HomeIcon from '@lucide/svelte/icons/home';
	import ImageIcon from '@lucide/svelte/icons/image';
	import LockIcon from '@lucide/svelte/icons/lock';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import ScissorsIcon from '@lucide/svelte/icons/scissors';
	import SearchIcon from '@lucide/svelte/icons/search';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import UploadIcon from '@lucide/svelte/icons/upload';
	import XIcon from '@lucide/svelte/icons/x';
	import { toast } from 'svelte-sonner';

	type SortKey = 'name' | 'size' | 'mod_time';

	interface BreadcrumbSegment {
		label: string;
		path: string;
		clickable: boolean;
	}

	interface ClipboardState {
		mode: 'copy' | 'move';
		paths: string[];
		sourceDir: string;
	}

	const CODE_EXTS = new Set([
		'.php',
		'.js',
		'.mjs',
		'.cjs',
		'.ts',
		'.mts',
		'.tsx',
		'.jsx',
		'.svelte',
		'.vue',
		'.go',
		'.py',
		'.rb',
		'.rs',
		'.java',
		'.html',
		'.htm',
		'.css',
		'.scss',
		'.json',
		'.yaml',
		'.yml',
		'.toml',
		'.sh',
		'.bash',
		'.zsh',
		'.sql',
		'.env',
		'.xml',
		'.svg'
	]);

	const TEXT_EXTS = new Set([
		'.md',
		'.txt',
		'.log',
		'.conf',
		'.ini',
		'.cfg',
		'.csv',
		'.htaccess',
		'.gitignore',
		'.dockerignore',
		'.lock'
	]);

	let listing = $state<FileListResult | null>(null);
	let loading = $state(true);
	let errorMsg = $state<string | null>(null);
	let showHidden = $state(false);

	// Sorting
	let sortBy = $state<SortKey>('name');
	let sortDir = $state<'asc' | 'desc'>('asc');

	// Search state
	let searchInput = $state('');
	let activeSearchQuery = $state('');
	let searchResults = $state<FileEntry[] | null>(null);
	let searching = $state(false);

	// Jump path editor state
	let pathEditMode = $state(false);
	let pathInput = $state('');

	// Selection & Clipboard state
	let selectedPaths = $state<string[]>([]);
	let clipboard = $state<ClipboardState | null>(null);
	let bulkBusy = $state(false);

	// Drag & drop upload overlay
	let dragOver = $state(false);
	let dragCounter = 0;
	let uploading = $state(false);

	// Editor / Preview modal
	let editorOpen = $state(false);
	let editorEntry = $state<FileEntry | null>(null);

	// Dialog: New File
	let newFileOpen = $state(false);
	let newFileName = $state('');
	let newFileOpenAfter = $state(true);
	let newFileBusy = $state(false);

	// Dialog: New Folder
	let newFolderOpen = $state(false);
	let newFolderName = $state('');
	let newFolderBusy = $state(false);

	// Dialog: Upload
	let uploadOpen = $state(false);
	let uploadPickedFiles = $state<File[]>([]);
	let uploadInputRef = $state<HTMLInputElement | null>(null);

	// Dialog: Rename
	let renameOpen = $state(false);
	let renameEntry = $state<FileEntry | null>(null);
	let renameNewName = $state('');
	let renameBusy = $state(false);

	// Dialog: Chmod / Permissions
	let chmodOpen = $state(false);
	let chmodTargets = $state<string[]>([]);
	let chmodMode = $state('0755');
	let chmodRecursive = $state(false);
	let chmodWebOwner = $state(false);
	let chmodBusy = $state(false);

	// Dialog: Compress ZIP
	let compressOpen = $state(false);
	let compressTargets = $state<string[]>([]);
	let compressOutput = $state('');
	let compressBusy = $state(false);

	// Dialog: Extract Archive
	let extractOpen = $state(false);
	let extractEntry = $state<FileEntry | null>(null);
	let extractDest = $state('');
	let extractBusy = $state(false);

	let lastSyncedUrlParam: string | null = null;
	let loadSerial = 0;

	const currentPath = $derived(listing?.path ?? '');
	const roots = $derived<FileRootInfo[]>(listing?.roots ?? []);

	const isSearchMode = $derived(searchResults !== null);
	const rawEntries = $derived<FileEntry[]>(
		searchResults !== null ? searchResults : (listing?.entries ?? [])
	);

	const visibleEntries = $derived.by(() => {
		const list = [...rawEntries];
		list.sort((a, b) => {
			if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
			let cmp = 0;
			if (sortBy === 'size') {
				cmp = a.size - b.size;
			} else if (sortBy === 'mod_time') {
				cmp = new Date(a.mod_time).getTime() - new Date(b.mod_time).getTime();
			} else {
				cmp = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' });
			}
			return sortDir === 'asc' ? cmp : -cmp;
		});
		return list;
	});

	const selectedSet = $derived(new Set(selectedPaths));
	const allVisibleSelected = $derived(
		visibleEntries.length > 0 && visibleEntries.every((e) => selectedSet.has(e.path))
	);
	const someVisibleSelected = $derived(
		visibleEntries.some((e) => selectedSet.has(e.path)) && !allVisibleSelected
	);

	const breadcrumbs = $derived.by<BreadcrumbSegment[]>(() => {
		if (!currentPath) return [];
		const normalized = currentPath.replace(/\/+$/, '') || '/';
		const parts = normalized.split('/').filter(Boolean);
		const out: BreadcrumbSegment[] = [
			{
				label: '/',
				path: '/',
				clickable: isPathAllowed('/', roots)
			}
		];
		let acc = '';
		for (const part of parts) {
			acc += '/' + part;
			out.push({
				label: part,
				path: acc,
				clickable: isPathAllowed(acc, roots)
			});
		}
		return out;
	});

	const chmodBits = $derived.by(() => parseOctalMode(chmodMode));

	$effect(() => {
		const target = page.url.searchParams.get('path') ?? '';
		if (target === lastSyncedUrlParam) return;
		lastSyncedUrlParam = target;
		void loadDirectory(target);
	});

	function isPathAllowed(candidate: string, allowed: FileRootInfo[]): boolean {
		if (!candidate || allowed.length === 0) return false;
		const clean = candidate.replace(/\/+$/, '') || '/';
		return allowed.some((r) => {
			const rp = r.path.replace(/\/+$/, '') || '/';
			if (rp === '/') return true;
			return clean === rp || clean.startsWith(rp + '/');
		});
	}

	function parseOctalMode(modeStr: string): number {
		const clean = modeStr.trim().replace(/^0+/, '') || '0';
		const parsed = Number.parseInt(clean, 8);
		if (Number.isNaN(parsed)) return 0o755;
		return parsed & 0o777;
	}

	function formatOctal(bits: number): string {
		return '0' + (bits & 0o777).toString(8).padStart(3, '0');
	}

	function hasPermBit(whoShift: number, bitVal: number): boolean {
		return ((chmodBits >> whoShift) & bitVal) !== 0;
	}

	function togglePermBit(whoShift: number, bitVal: number) {
		const mask = bitVal << whoShift;
		const next = chmodBits ^ mask;
		chmodMode = formatOctal(next);
	}

	function formatBytes(bytes: number): string {
		if (bytes <= 0) return '0 B';
		const units = ['B', 'KB', 'MB', 'GB', 'TB'];
		const exp = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
		const val = bytes / Math.pow(1024, exp);
		return `${exp === 0 ? val : val.toFixed(1)} ${units[exp]}`;
	}

	function formatModTime(iso: string): string {
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return d.toLocaleString('id-ID', {
			day: '2-digit',
			month: 'short',
			year: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	function joinChildPath(dir: string, name: string): string {
		const cleanDir = dir.replace(/\/+$/, '');
		if (!cleanDir) return '/' + name;
		return `${cleanDir}/${name}`;
	}

	function getParentPath(fullPath: string): string {
		const idx = fullPath.lastIndexOf('/');
		if (idx <= 0) return '/';
		return fullPath.slice(0, idx);
	}

	function formatErr(err: unknown, fallback: string): string {
		if (err instanceof ApiError || err instanceof Error) {
			return err.message || fallback;
		}
		return fallback;
	}

	async function navigateTo(targetPath: string) {
		const clean = targetPath.trim();
		clearSearch();
		lastSyncedUrlParam = clean;
		const targetUrl = clean ? `/files?path=${encodeURIComponent(clean)}` : '/files';
		if (page.url.pathname + page.url.search !== targetUrl) {
			await goto(targetUrl, { reset: false });
		}
		await loadDirectory(clean);
	}

	async function loadDirectory(targetPath: string) {
		const serial = ++loadSerial;
		loading = true;
		errorMsg = null;
		selectedPaths = [];
		try {
			const res = await api.listFiles(targetPath, showHidden);
			if (serial !== loadSerial) return;
			listing = res;
			pathInput = res.path;
			pathEditMode = false;
		} catch (err) {
			if (serial !== loadSerial) return;
			errorMsg = formatErr(err, 'Gagal membaca direktori');
		} finally {
			if (serial === loadSerial) {
				loading = false;
			}
		}
	}

	async function refreshCurrent() {
		if (isSearchMode && activeSearchQuery) {
			await runSearch();
			return;
		}
		await loadDirectory(currentPath);
	}

	async function toggleShowHidden() {
		showHidden = !showHidden;
		await refreshCurrent();
	}

	function toggleSort(key: SortKey) {
		if (sortBy === key) {
			sortDir = sortDir === 'asc' ? 'desc' : 'asc';
		} else {
			sortBy = key;
			sortDir = key === 'mod_time' ? 'desc' : 'asc';
		}
	}

	async function runSearch() {
		const q = searchInput.trim();
		if (!q) {
			clearSearch();
			return;
		}
		searching = true;
		errorMsg = null;
		selectedPaths = [];
		try {
			const res = await api.searchFiles(currentPath, q, showHidden);
			searchResults = res.entries ?? [];
			activeSearchQuery = q;
		} catch (err) {
			toast.error(formatErr(err, 'Gagal mencari berkas'));
		} finally {
			searching = false;
		}
	}

	function clearSearch() {
		searchInput = '';
		activeSearchQuery = '';
		searchResults = null;
	}

	function handleJumpSubmit(e: SubmitEvent) {
		e.preventDefault();
		const target = pathInput.trim();
		if (!target) {
			pathEditMode = false;
			return;
		}
		void navigateTo(target);
	}

	function toggleSelect(path: string, checked: boolean) {
		if (checked) {
			if (!selectedSet.has(path)) {
				selectedPaths = [...selectedPaths, path];
			}
		} else {
			selectedPaths = selectedPaths.filter((p) => p !== path);
		}
	}

	function toggleSelectAll(checked: boolean) {
		if (checked) {
			selectedPaths = visibleEntries.map((e) => e.path);
		} else {
			selectedPaths = [];
		}
	}

	function handleRowPrimaryClick(entry: FileEntry) {
		if (entry.is_dir) {
			void navigateTo(entry.path);
			return;
		}
		if (entry.editable || entry.is_image) {
			openEditor(entry);
			return;
		}
		if (entry.is_archive) {
			openExtractDialog(entry);
			return;
		}
		triggerDownload([entry.path]);
	}

	function openEditor(entry: FileEntry) {
		editorEntry = entry;
		editorOpen = true;
	}

	function triggerDownload(paths: string[]) {
		if (paths.length === 0) return;
		const url = api.fileDownloadUrl(paths);
		const a = document.createElement('a');
		a.href = url;
		a.rel = 'noopener';
		document.body.appendChild(a);
		a.click();
		a.remove();
	}

	function setClipboard(mode: 'copy' | 'move', paths: string[]) {
		if (paths.length === 0) return;
		clipboard = {
			mode,
			paths: [...paths],
			sourceDir: currentPath
		};
		selectedPaths = [];
		toast.success(
			`${paths.length} item ditandai untuk ${mode === 'copy' ? 'disalin' : 'dipindahkan'}. Buka folder tujuan lalu klik Tempel.`
		);
	}

	async function pasteClipboard() {
		if (!clipboard || !currentPath || bulkBusy) return;
		const { mode, paths } = clipboard;
		bulkBusy = true;
		try {
			if (mode === 'copy') {
				const res = await api.copyFiles(paths, currentPath);
				toast.success(`${res.count} item berhasil disalin.`);
			} else {
				const res = await api.moveFiles(paths, currentPath);
				toast.success(`${res.count} item berhasil dipindahkan.`);
				clipboard = null;
			}
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, 'Gagal menempel item'));
		} finally {
			bulkBusy = false;
		}
	}

	async function confirmDelete(paths: string[]) {
		if (paths.length === 0 || bulkBusy) return;
		const label =
			paths.length === 1 ? `"${paths[0].split('/').pop() || paths[0]}"` : `${paths.length} item`;
		const ok = await cp.confirm({
			title: 'Hapus Berkas / Folder',
			description: `Anda yakin ingin menghapus permanen ${label}? Tindakan ini tidak dapat dibatalkan.`,
			confirmText: 'Hapus Permanen',
			destructive: true
		});
		if (!ok) return;

		bulkBusy = true;
		try {
			const res = await api.deleteFiles(paths);
			toast.success(`${res.count} item berhasil dihapus.`);
			selectedPaths = selectedPaths.filter((p) => !paths.includes(p));
			if (clipboard) {
				const remaining = clipboard.paths.filter((p) => !paths.includes(p));
				clipboard = remaining.length > 0 ? { ...clipboard, paths: remaining } : null;
			}
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, 'Gagal menghapus berkas'));
		} finally {
			bulkBusy = false;
		}
	}

	// New File
	function openNewFileDialog() {
		newFileName = '';
		newFileOpenAfter = true;
		newFileOpen = true;
	}

	async function submitNewFile(e: SubmitEvent) {
		e.preventDefault();
		const name = newFileName.trim();
		if (!name || !currentPath || newFileBusy) return;
		if (name.includes('/')) {
			toast.error('Masukkan nama file saja tanpa garis miring.');
			return;
		}
		const targetPath = joinChildPath(currentPath, name);
		newFileBusy = true;
		try {
			await api.writeFileContent(targetPath, '');
			newFileOpen = false;
			toast.success(`File "${name}" dibuat.`);
			await loadDirectory(currentPath);
			if (newFileOpenAfter) {
				const created = listing?.entries.find((item) => item.path === targetPath);
				if (created) {
					openEditor(created);
				}
			}
		} catch (err) {
			toast.error(formatErr(err, 'Gagal membuat file baru'));
		} finally {
			newFileBusy = false;
		}
	}

	// New Folder
	function openNewFolderDialog() {
		newFolderName = '';
		newFolderOpen = true;
	}

	async function submitNewFolder(e: SubmitEvent) {
		e.preventDefault();
		const name = newFolderName.trim();
		if (!name || !currentPath || newFolderBusy) return;
		if (name.includes('/')) {
			toast.error('Masukkan nama folder saja tanpa garis miring.');
			return;
		}
		newFolderBusy = true;
		try {
			await api.mkdirFile(joinChildPath(currentPath, name));
			newFolderOpen = false;
			toast.success(`Folder "${name}" dibuat.`);
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, 'Gagal membuat folder'));
		} finally {
			newFolderBusy = false;
		}
	}

	// Upload
	function openUploadDialog() {
		uploadPickedFiles = [];
		uploadOpen = true;
	}

	function handleUploadFileChange(e: Event) {
		const target = e.currentTarget as HTMLInputElement;
		uploadPickedFiles = target.files ? Array.from(target.files) : [];
	}

	async function performUpload(files: File[]) {
		if (!currentPath || files.length === 0 || uploading) return;
		uploading = true;
		try {
			const res = await api.uploadFiles(currentPath, files);
			uploadOpen = false;
			uploadPickedFiles = [];
			if (uploadInputRef) uploadInputRef.value = '';
			toast.success(`${res.count} berkas berhasil diunggah ke ${currentPath}.`);
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, 'Gagal mengunggah berkas'));
		} finally {
			uploading = false;
		}
	}

	function handleWorkspaceDragEnter(e: DragEvent) {
		if (!e.dataTransfer?.types?.includes('Files')) return;
		e.preventDefault();
		dragCounter += 1;
		dragOver = true;
	}

	function handleWorkspaceDragOver(e: DragEvent) {
		if (!e.dataTransfer?.types?.includes('Files')) return;
		e.preventDefault();
	}

	function handleWorkspaceDragLeave(e: DragEvent) {
		if (!e.dataTransfer?.types?.includes('Files')) return;
		e.preventDefault();
		dragCounter = Math.max(0, dragCounter - 1);
		if (dragCounter === 0) {
			dragOver = false;
		}
	}

	function handleWorkspaceDrop(e: DragEvent) {
		e.preventDefault();
		dragCounter = 0;
		dragOver = false;
		const files = e.dataTransfer?.files ? Array.from(e.dataTransfer.files) : [];
		if (files.length > 0 && currentPath && !isSearchMode) {
			void performUpload(files);
		}
	}

	// Rename
	function openRenameDialog(entry: FileEntry) {
		renameEntry = entry;
		renameNewName = entry.name;
		renameOpen = true;
	}

	async function submitRename(e: SubmitEvent) {
		e.preventDefault();
		const target = renameEntry;
		const nextName = renameNewName.trim();
		if (!target || !nextName || renameBusy) return;
		if (nextName === target.name) {
			renameOpen = false;
			return;
		}
		renameBusy = true;
		try {
			await api.renameFile(target.path, nextName);
			renameOpen = false;
			toast.success(`Diubah menjadi "${nextName}".`);
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, 'Gagal mengubah nama'));
		} finally {
			renameBusy = false;
		}
	}

	// Chmod
	function openChmodDialog(entries: FileEntry[], fallbackPaths?: string[]) {
		if (entries.length > 0) {
			chmodTargets = entries.map((item) => item.path);
			chmodMode = entries[0].mode || '0755';
			chmodRecursive = entries.some((item) => item.is_dir);
		} else if (fallbackPaths && fallbackPaths.length > 0) {
			chmodTargets = [...fallbackPaths];
			chmodMode = '0755';
			chmodRecursive = false;
		} else {
			return;
		}
		chmodWebOwner = false;
		chmodOpen = true;
	}

	async function submitChmod(e: SubmitEvent) {
		e.preventDefault();
		if (chmodTargets.length === 0 || chmodBusy) return;
		const modeClean = chmodMode.trim();
		if (!/^[0-7]{3,4}$/.test(modeClean)) {
			toast.error('Mode octal harus berupa 3 atau 4 digit angka 0-7 (contoh: 0755 atau 644).');
			return;
		}
		chmodBusy = true;
		try {
			await api.chmodFiles(chmodTargets, modeClean, chmodRecursive, chmodWebOwner);
			chmodOpen = false;
			toast.success('Permission berhasil diperbarui.');
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, 'Gagal mengubah permission'));
		} finally {
			chmodBusy = false;
		}
	}

	// Compress
	function openCompressDialog(paths: string[]) {
		if (paths.length === 0) return;
		compressTargets = [...paths];
		const baseName =
			paths.length === 1
				? `${paths[0].split('/').pop() || 'archive'}.zip`
				: `arsip-${new Date().toISOString().slice(0, 10)}.zip`;
		compressOutput = joinChildPath(currentPath || getParentPath(paths[0]), baseName);
		compressOpen = true;
	}

	async function submitCompress(e: SubmitEvent) {
		e.preventDefault();
		if (compressTargets.length === 0 || !compressOutput.trim() || compressBusy) return;
		compressBusy = true;
		try {
			const res = await api.compressFiles(compressTargets, compressOutput.trim());
			compressOpen = false;
			selectedPaths = [];
			toast.success(`Arsip "${res.entry.name}" berhasil dibuat.`);
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, 'Gagal membuat arsip ZIP'));
		} finally {
			compressBusy = false;
		}
	}

	// Extract
	function openExtractDialog(entry: FileEntry) {
		extractEntry = entry;
		extractDest = getParentPath(entry.path);
		extractOpen = true;
	}

	async function submitExtract(e: SubmitEvent) {
		e.preventDefault();
		if (!extractEntry || extractBusy) return;
		extractBusy = true;
		try {
			const res = await api.extractFile(extractEntry.path, extractDest.trim());
			extractOpen = false;
			toast.success(`${res.count} entri berhasil diekstrak.`);
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, 'Gagal mengekstrak arsip'));
		} finally {
			extractBusy = false;
		}
	}

	function isCodeFile(entry: FileEntry): boolean {
		return CODE_EXTS.has((entry.ext || '').toLowerCase());
	}

	function isTextFile(entry: FileEntry): boolean {
		const ext = (entry.ext || '').toLowerCase();
		const lower = entry.name.toLowerCase();
		return TEXT_EXTS.has(ext) || lower.startsWith('.env') || lower === 'dockerfile' || lower === 'makefile';
	}
</script>

<svelte:head>
	<title>File Manager - wdev Control Plane</title>
</svelte:head>

<div class="space-y-5">
	<!-- Page Header -->
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-2xl font-bold tracking-tight">File Manager</h1>
			<p class="text-muted-foreground text-sm">
				Kelola berkas website, VHost document root, permission, upload, editor, dan arsip ZIP/TAR secara langsung.
			</p>
		</div>

		<div class="flex flex-wrap items-center gap-2">
			<Button variant="outline" size="sm" onclick={openNewFileDialog} disabled={!currentPath || isSearchMode}>
				<FilePlusIcon class="size-4" />
				File Baru
			</Button>
			<Button variant="outline" size="sm" onclick={openNewFolderDialog} disabled={!currentPath || isSearchMode}>
				<FolderPlusIcon class="size-4" />
				Folder Baru
			</Button>
			<Button size="sm" onclick={openUploadDialog} disabled={!currentPath || isSearchMode || uploading}>
				{#if uploading}
					<Spinner class="size-4" />
				{:else}
					<UploadIcon class="size-4" />
				{/if}
				Upload
			</Button>
		</div>
	</div>

	<!-- Quick Access Roots / VHosts -->
	{#if roots.length > 0}
		<div class="flex flex-wrap items-center gap-2">
			<span class="text-muted-foreground text-xs font-medium uppercase tracking-wide">Akses Cepat:</span>
			{#each roots as root (root.path)}
				{@const active = currentPath === root.path || currentPath.startsWith(root.path.replace(/\/+$/, '') + '/')}
				<Button
					variant={active ? 'secondary' : 'outline'}
					size="sm"
					class="h-7 gap-1.5 text-xs"
					onclick={() => navigateTo(root.path)}
				>
					{#if root.kind === 'home'}
						<HomeIcon class="size-3.5" />
					{:else}
						<HardDriveIcon class="size-3.5" />
					{/if}
					<span>{root.name}</span>
				</Button>
			{/each}
		</div>
	{/if}

	<!-- Main Explorer Card (with drag-and-drop dropzone) -->
	<Card
		class={`relative transition-colors ${dragOver ? 'border-primary ring-primary/30 ring-2' : ''}`}
		ondragenter={handleWorkspaceDragEnter}
		ondragover={handleWorkspaceDragOver}
		ondragleave={handleWorkspaceDragLeave}
		ondrop={handleWorkspaceDrop}
	>
		{#if dragOver}
			<div class="bg-background/85 pointer-events-none absolute inset-0 z-20 flex flex-col items-center justify-center rounded-xl border-2 border-dashed border-primary backdrop-blur-[1px]">
				<UploadIcon class="text-primary mb-2 size-10" />
				<p class="text-sm font-semibold">Lepaskan file untuk mengunggah ke:</p>
				<p class="text-muted-foreground mt-1 font-mono text-xs">{currentPath}</p>
			</div>
		{/if}

		<CardHeader class="space-y-3 pb-3">
			<!-- Toolbar Row: Up / Home / Refresh / Breadcrumb or Jump Path / Hidden Toggle / Search -->
			<div class="flex flex-col gap-2.5 lg:flex-row lg:items-center lg:justify-between">
				<div class="flex min-w-0 flex-1 items-center gap-1.5">
					<Button
						variant="outline"
						size="icon-sm"
						title="Naik ke folder induk"
						disabled={!listing?.can_go_up || loading || isSearchMode}
						onclick={() => listing?.parent && navigateTo(listing.parent)}
					>
						<ArrowUpIcon class="size-4" />
					</Button>
					<Button
						variant="outline"
						size="icon-sm"
						title="Ke direktori awal"
						disabled={loading}
						onclick={() => navigateTo(roots[0]?.path ?? '')}
					>
						<HomeIcon class="size-4" />
					</Button>
					<Button
						variant="outline"
						size="icon-sm"
						title="Segarkan"
						disabled={loading || searching}
						onclick={refreshCurrent}
					>
						<RefreshCwIcon class={`size-4 ${loading || searching ? 'animate-spin' : ''}`} />
					</Button>

					<!-- Breadcrumb / Inline Path Input -->
					{#if pathEditMode}
						<form onsubmit={handleJumpSubmit} class="flex min-w-0 flex-1 items-center gap-1.5">
							<Input
								bind:value={pathInput}
								class="h-8 font-mono text-xs"
								placeholder="/var/www/..."
								aria-label="Path direktori"
								autofocus
							/>
							<Button type="submit" size="sm" class="h-8">Buka</Button>
							<Button
								type="button"
								variant="ghost"
								size="icon-sm"
								onclick={() => {
									pathEditMode = false;
									pathInput = currentPath;
								}}
								aria-label="Batal ubah path"
							>
								<XIcon class="size-4" />
							</Button>
						</form>
					{:else}
						<div class="bg-muted/40 flex min-h-8 min-w-0 flex-1 flex-wrap items-center gap-0.5 rounded-md border px-2 py-1 text-xs">
							{#if breadcrumbs.length === 0}
								<span class="text-muted-foreground font-mono">Memuat path...</span>
							{:else}
								{#each breadcrumbs as seg, idx (seg.path)}
									{#if idx > 0}
										<ChevronRightIcon class="text-muted-foreground/60 size-3.5 shrink-0" />
									{/if}
									{#if seg.clickable && seg.path !== currentPath}
										<button
											type="button"
											class="hover:bg-accent hover:text-accent-foreground rounded px-1.5 py-0.5 font-mono transition-colors"
											onclick={() => navigateTo(seg.path)}
										>
											{seg.label}
										</button>
									{:else if seg.path === currentPath}
										<span class="text-foreground rounded px-1.5 py-0.5 font-mono font-semibold">
											{seg.label}
										</span>
									{:else}
										<span class="text-muted-foreground px-1 py-0.5 font-mono">
											{seg.label}
										</span>
									{/if}
								{/each}
							{/if}
							<button
								type="button"
								class="text-muted-foreground hover:text-foreground ml-auto rounded p-0.5"
								title="Ketik path manual"
								onclick={() => {
									pathInput = currentPath;
									pathEditMode = true;
								}}
							>
								<PencilIcon class="size-3" />
							</button>
						</div>
					{/if}
				</div>

				<!-- Search & Hidden Toggle -->
				<div class="flex flex-wrap items-center gap-2">
					<Button
						variant={showHidden ? 'secondary' : 'outline'}
						size="sm"
						class="h-8 gap-1.5 text-xs"
						onclick={toggleShowHidden}
						title="Tampilkan / sembunyikan file awalan titik (.env, .htaccess, dll)"
					>
						{#if showHidden}
							<EyeIcon class="size-3.5" />
						{:else}
							<EyeOffIcon class="size-3.5" />
						{/if}
						Hidden (.dot)
					</Button>

					<form
						onsubmit={(e) => {
							e.preventDefault();
							void runSearch();
						}}
						class="flex items-center gap-1.5"
					>
						<div class="relative">
							<SearchIcon class="text-muted-foreground pointer-events-none absolute top-2 left-2.5 size-3.5" />
							<Input
								bind:value={searchInput}
								placeholder="Cari file di folder ini..."
								class="h-8 w-48 pl-8 text-xs sm:w-56"
							/>
						</div>
						<Button type="submit" variant="outline" size="sm" class="h-8" disabled={searching || !currentPath}>
							{#if searching}
								<Spinner class="size-3.5" />
							{:else}
								Cari
							{/if}
						</Button>
						{#if isSearchMode}
							<Button type="button" variant="ghost" size="sm" class="h-8 text-xs" onclick={clearSearch}>
								Reset
							</Button>
						{/if}
					</form>
				</div>
			</div>

			<!-- Clipboard Banner -->
			{#if clipboard}
				<div class="bg-primary/5 border-primary/25 flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-xs">
					<div class="flex items-center gap-2">
						{#if clipboard.mode === 'copy'}
							<CopyIcon class="text-primary size-4 shrink-0" />
						{:else}
							<ScissorsIcon class="text-primary size-4 shrink-0" />
						{/if}
						<span>
							<strong>{clipboard.paths.length} item</strong> siap {clipboard.mode === 'copy' ? 'disalin' : 'dipindahkan'} dari
							<code class="bg-muted rounded px-1 py-0.5 font-mono">{clipboard.sourceDir}</code>
						</span>
					</div>
					<div class="flex items-center gap-1.5">
						<Button size="sm" class="h-7 gap-1 text-xs" disabled={bulkBusy || isSearchMode || !currentPath} onclick={pasteClipboard}>
							{#if bulkBusy}
								<Spinner class="size-3.5" />
							{:else}
								<ClipboardPasteIcon class="size-3.5" />
							{/if}
							Tempel di Sini
						</Button>
						<Button variant="ghost" size="sm" class="h-7 text-xs" onclick={() => (clipboard = null)}>
							Batal
						</Button>
					</div>
				</div>
			{/if}

			<!-- Multi-select Bulk Actions Bar -->
			{#if selectedPaths.length > 0}
				<div class="bg-muted/60 flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-xs">
					<div class="flex items-center gap-2 font-medium">
						<Badge variant="secondary">{selectedPaths.length} dipilih</Badge>
						<button
							type="button"
							class="text-muted-foreground hover:text-foreground underline"
							onclick={() => (selectedPaths = [])}
						>
							Bersihkan
						</button>
					</div>
					<div class="flex flex-wrap items-center gap-1.5">
						<Button variant="outline" size="sm" class="h-7 gap-1 text-xs" onclick={() => setClipboard('copy', selectedPaths)}>
							<CopyIcon class="size-3.5" />
							Salin
						</Button>
						<Button variant="outline" size="sm" class="h-7 gap-1 text-xs" onclick={() => setClipboard('move', selectedPaths)}>
							<ScissorsIcon class="size-3.5" />
							Pindah
						</Button>
						<Button variant="outline" size="sm" class="h-7 gap-1 text-xs" onclick={() => openCompressDialog(selectedPaths)}>
							<FileArchiveIcon class="size-3.5" />
							Kompres ZIP
						</Button>
						<Button variant="outline" size="sm" class="h-7 gap-1 text-xs" onclick={() => triggerDownload(selectedPaths)}>
							<DownloadIcon class="size-3.5" />
							Unduh ZIP
						</Button>
						<Button
							variant="outline"
							size="sm"
							class="h-7 gap-1 text-xs"
							onclick={() =>
								openChmodDialog(
									visibleEntries.filter((e) => selectedSet.has(e.path)),
									selectedPaths
								)}
						>
							<LockIcon class="size-3.5" />
							Permission
						</Button>
						<Button
							variant="destructive"
							size="sm"
							class="h-7 gap-1 text-xs"
							disabled={bulkBusy}
							onclick={() => confirmDelete(selectedPaths)}
						>
							<Trash2Icon class="size-3.5" />
							Hapus
						</Button>
					</div>
				</div>
			{/if}

			<!-- Status Summary / Search Banner -->
			<div class="flex flex-wrap items-center justify-between gap-2">
				{#if isSearchMode}
					<CardTitle class="text-sm font-medium">
						Hasil pencarian "{activeSearchQuery}" ({visibleEntries.length} ditemukan)
					</CardTitle>
				{:else if listing}
					<CardDescription class="text-xs">
						{listing.total_dirs} folder, {listing.total_files} file • Total ukuran file: {formatBytes(listing.total_size)}
						• Tips: seret & lepas (drag & drop) file ke area tabel ini untuk mengunggah.
					</CardDescription>
				{/if}
			</div>
		</CardHeader>

		<CardContent class="pt-0">
			{#if errorMsg}
				<Alert variant="destructive" class="mb-4">
					<CircleAlertIcon class="size-4" />
					<AlertTitle>Gagal Memuat Direktori</AlertTitle>
					<AlertDescription class="flex flex-wrap items-center justify-between gap-2">
						<span>{errorMsg}</span>
						<Button variant="outline" size="sm" onclick={() => navigateTo(roots[0]?.path ?? '')}>
							Kembali ke Root Awal
						</Button>
					</AlertDescription>
				</Alert>
			{/if}

			{#if loading && !listing}
				<div class="space-y-2">
					{#each [1, 2, 3, 4, 5, 6] as row (row)}
						<Skeleton class="h-10 w-full" />
					{/each}
				</div>
			{:else if visibleEntries.length === 0}
				<Empty.Root class="border">
					<Empty.Header>
						<Empty.Media variant="icon">
							<FolderOpenIcon class="size-5" />
						</Empty.Media>
						<Empty.Title>{isSearchMode ? 'Tidak Ada Hasil' : 'Direktori Kosong'}</Empty.Title>
						<Empty.Description>
							{#if isSearchMode}
								Tidak ditemukan berkas atau folder yang cocok dengan kata kunci "{activeSearchQuery}".
							{:else}
								Belum ada berkas di direktori ini. Gunakan tombol di atas atau seret berkas ke sini untuk mengunggah.
							{/if}
						</Empty.Description>
					</Empty.Header>
					{#if !isSearchMode}
						<Empty.Content class="flex-row justify-center gap-2">
							<Button variant="outline" size="sm" onclick={openNewFileDialog}>
								<FilePlusIcon class="size-4" />
								Buat File Baru
							</Button>
							<Button size="sm" onclick={openUploadDialog}>
								<UploadIcon class="size-4" />
								Upload Berkas
							</Button>
						</Empty.Content>
					{/if}
				</Empty.Root>
			{:else}
				<ResponsiveTableWrapper label="Daftar Berkas File Manager">
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead class="w-10">
									<Checkbox
										checked={allVisibleSelected}
										indeterminate={someVisibleSelected}
										onCheckedChange={(v) => toggleSelectAll(Boolean(v))}
										aria-label="Pilih semua berkas"
									/>
								</TableHead>
								<TableHead>
									<button
										type="button"
										class="hover:text-foreground inline-flex items-center gap-1 font-medium"
										onclick={() => toggleSort('name')}
									>
										Nama
										{#if sortBy === 'name'}
											{#if sortDir === 'asc'}
												<ArrowUpIcon class="size-3.5" />
											{:else}
												<ArrowDownIcon class="size-3.5" />
											{/if}
										{:else}
											<ArrowUpDownIcon class="text-muted-foreground/50 size-3.5" />
										{/if}
									</button>
								</TableHead>
								<TableHead class="w-28">
									<button
										type="button"
										class="hover:text-foreground inline-flex items-center gap-1 font-medium"
										onclick={() => toggleSort('size')}
									>
										Ukuran
										{#if sortBy === 'size'}
											{#if sortDir === 'asc'}
												<ArrowUpIcon class="size-3.5" />
											{:else}
												<ArrowDownIcon class="size-3.5" />
											{/if}
										{:else}
											<ArrowUpDownIcon class="text-muted-foreground/50 size-3.5" />
										{/if}
									</button>
								</TableHead>
								<TableHead class="w-36">Permission</TableHead>
								<TableHead class="w-36">Owner</TableHead>
								<TableHead class="w-40">
									<button
										type="button"
										class="hover:text-foreground inline-flex items-center gap-1 font-medium"
										onclick={() => toggleSort('mod_time')}
									>
										Diubah
										{#if sortBy === 'mod_time'}
											{#if sortDir === 'asc'}
												<ArrowUpIcon class="size-3.5" />
											{:else}
												<ArrowDownIcon class="size-3.5" />
											{/if}
										{:else}
											<ArrowUpDownIcon class="text-muted-foreground/50 size-3.5" />
										{/if}
									</button>
								</TableHead>
								<TableHead class="text-right">Aksi</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{#each visibleEntries as entry (entry.path)}
								{@const checked = selectedSet.has(entry.path)}
								<TableRow class={checked ? 'bg-muted/40' : undefined}>
									<TableCell data-label="Pilih">
										<Checkbox
											{checked}
											onCheckedChange={(v) => toggleSelect(entry.path, Boolean(v))}
											aria-label={`Pilih ${entry.name}`}
										/>
									</TableCell>
									<TableCell data-label="Nama">
										<div class="flex min-w-0 items-center gap-2.5">
											{#if entry.is_dir}
												{#if entry.is_symlink}
													<FolderSymlinkIcon class="size-4 shrink-0 text-amber-500" />
												{:else}
													<FolderIcon class="size-4 shrink-0 text-amber-500" />
												{/if}
											{:else if entry.is_image}
												<ImageIcon class="size-4 shrink-0 text-purple-500" />
											{:else if entry.is_archive}
												<FileArchiveIcon class="size-4 shrink-0 text-orange-500" />
											{:else if isCodeFile(entry)}
												<FileCodeIcon class="size-4 shrink-0 text-emerald-500" />
											{:else if isTextFile(entry)}
												<FileTextIcon class="size-4 shrink-0 text-sky-500" />
											{:else}
												<FileIcon class="text-muted-foreground size-4 shrink-0" />
											{/if}

											<div class="min-w-0 flex-1">
												<button
													type="button"
													class="hover:text-primary block max-w-full truncate text-left font-medium hover:underline"
													onclick={() => handleRowPrimaryClick(entry)}
												>
													{entry.name}
												</button>
												{#if entry.is_symlink && entry.symlink_target}
													<p class="text-muted-foreground truncate font-mono text-[11px]">
														→ {entry.symlink_target}
													</p>
												{/if}
												{#if isSearchMode}
													<button
														type="button"
														class="text-muted-foreground hover:text-foreground block truncate font-mono text-[11px] hover:underline"
														onclick={() => navigateTo(getParentPath(entry.path))}
													>
														{getParentPath(entry.path)}
													</button>
												{/if}
											</div>
										</div>
									</TableCell>
									<TableCell data-label="Ukuran" class="font-mono text-xs whitespace-nowrap">
										{entry.is_dir ? '—' : formatBytes(entry.size)}
									</TableCell>
									<TableCell data-label="Permission">
										<button
											type="button"
											class="hover:bg-muted inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 font-mono text-xs transition-colors"
											title="Ubah permission"
											onclick={() => openChmodDialog([entry])}
										>
											<span class="font-semibold">{entry.mode}</span>
											<span class="text-muted-foreground hidden sm:inline">({entry.perm_text})</span>
										</button>
									</TableCell>
									<TableCell data-label="Owner" class="text-muted-foreground font-mono text-xs whitespace-nowrap">
										{entry.owner}:{entry.group}
									</TableCell>
									<TableCell data-label="Diubah" class="text-muted-foreground text-xs whitespace-nowrap">
										{formatModTime(entry.mod_time)}
									</TableCell>
									<TableCell data-label="Aksi" class="text-right">
										<div class="flex flex-wrap items-center justify-end gap-1">
											{#if !entry.is_dir && (entry.editable || entry.is_image)}
												<Button
													variant="outline"
													size="sm"
													class="h-7 px-2 text-xs"
													onclick={() => openEditor(entry)}
												>
													{entry.editable ? 'Edit' : 'Lihat'}
												</Button>
											{/if}
											{#if entry.is_archive}
												<Button
													variant="outline"
													size="sm"
													class="h-7 px-2 text-xs"
													onclick={() => openExtractDialog(entry)}
												>
													Ekstrak
												</Button>
											{/if}
											<Button
												variant="ghost"
												size="icon-sm"
												title="Unduh"
												onclick={() => triggerDownload([entry.path])}
											>
												<DownloadIcon class="size-3.5" />
											</Button>
											<Button
												variant="ghost"
												size="icon-sm"
												title="Ubah nama"
												onclick={() => openRenameDialog(entry)}
											>
												<PencilIcon class="size-3.5" />
											</Button>
											<Button
												variant="ghost"
												size="icon-sm"
												title="Permission / Chmod"
												onclick={() => openChmodDialog([entry])}
											>
												<LockIcon class="size-3.5" />
											</Button>
											<Button
												variant="ghost"
												size="icon-sm"
												class="text-destructive hover:text-destructive"
												title="Hapus"
												onclick={() => confirmDelete([entry.path])}
											>
												<Trash2Icon class="size-3.5" />
											</Button>
										</div>
									</TableCell>
								</TableRow>
							{/each}
						</TableBody>
					</Table>
				</ResponsiveTableWrapper>
			{/if}
		</CardContent>
	</Card>
</div>

<!-- Editor & Image Preview Modal -->
<FileEditorModal bind:open={editorOpen} entry={editorEntry} onSaved={refreshCurrent} />

<!-- New File Dialog -->
<Dialog bind:open={newFileOpen}>
	<DialogContent class="sm:max-w-md">
		<DialogHeader>
			<DialogTitle>Buat File Baru</DialogTitle>
			<DialogDescription class="font-mono text-xs break-all">Lokasi: {currentPath}</DialogDescription>
		</DialogHeader>
		<form onsubmit={submitNewFile} class="space-y-4">
			<Field>
				<FieldLabel for="fm-new-file-name">Nama File</FieldLabel>
				<Input
					id="fm-new-file-name"
					bind:value={newFileName}
					placeholder="index.html, .env, config.php"
					required
					autofocus
				/>
			</Field>
			<label class="flex items-center gap-2 text-sm">
				<Checkbox bind:checked={newFileOpenAfter} />
				<span>Langsung buka di editor setelah dibuat</span>
			</label>
			<DialogFooter>
				<Button type="button" variant="outline" onclick={() => (newFileOpen = false)} disabled={newFileBusy}>
					Batal
				</Button>
				<Button type="submit" disabled={newFileBusy || !newFileName.trim()}>
					{#if newFileBusy}<Spinner class="size-4" />{/if}
					Buat File
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- New Folder Dialog -->
<Dialog bind:open={newFolderOpen}>
	<DialogContent class="sm:max-w-md">
		<DialogHeader>
			<DialogTitle>Buat Folder Baru</DialogTitle>
			<DialogDescription class="font-mono text-xs break-all">Lokasi: {currentPath}</DialogDescription>
		</DialogHeader>
		<form onsubmit={submitNewFolder} class="space-y-4">
			<Field>
				<FieldLabel for="fm-new-folder-name">Nama Folder</FieldLabel>
				<Input
					id="fm-new-folder-name"
					bind:value={newFolderName}
					placeholder="assets, uploads, public"
					required
					autofocus
				/>
			</Field>
			<DialogFooter>
				<Button type="button" variant="outline" onclick={() => (newFolderOpen = false)} disabled={newFolderBusy}>
					Batal
				</Button>
				<Button type="submit" disabled={newFolderBusy || !newFolderName.trim()}>
					{#if newFolderBusy}<Spinner class="size-4" />{/if}
					Buat Folder
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- Upload Dialog -->
<Dialog bind:open={uploadOpen}>
	<DialogContent class="sm:max-w-lg">
		<DialogHeader>
			<DialogTitle>Upload Berkas</DialogTitle>
			<DialogDescription class="font-mono text-xs break-all">
				Folder tujuan: {currentPath}
			</DialogDescription>
		</DialogHeader>
		<div class="space-y-4">
			<Field>
				<FieldLabel for="fm-upload-input">Pilih Satu atau Beberapa Berkas</FieldLabel>
				<input
					bind:this={uploadInputRef}
					id="fm-upload-input"
					type="file"
					multiple
					onchange={handleUploadFileChange}
					class="border-input bg-background file:bg-muted file:text-foreground block w-full cursor-pointer rounded-md border px-3 py-2 text-sm file:mr-3 file:rounded file:border-0 file:px-2.5 file:py-1 file:text-xs file:font-medium"
				/>
				<FieldDescription>Maksimum 256 MB per unggahan. File bernama sama akan menimpa file yang ada.</FieldDescription>
			</Field>

			{#if uploadPickedFiles.length > 0}
				<div class="bg-muted/40 max-h-44 space-y-1 overflow-y-auto rounded-md border p-2.5 text-xs">
					{#each uploadPickedFiles as file, idx (`${file.name}-${idx}`)}
						<div class="flex items-center justify-between gap-2">
							<span class="truncate font-mono">{file.name}</span>
							<span class="text-muted-foreground shrink-0 font-mono">{formatBytes(file.size)}</span>
						</div>
					{/each}
				</div>
			{/if}

			<DialogFooter>
				<Button type="button" variant="outline" onclick={() => (uploadOpen = false)} disabled={uploading}>
					Batal
				</Button>
				<Button
					type="button"
					disabled={uploading || uploadPickedFiles.length === 0}
					onclick={() => performUpload(uploadPickedFiles)}
				>
					{#if uploading}
						<Spinner class="size-4" />
						Mengunggah...
					{:else}
						<UploadIcon class="size-4" />
						Unggah ({uploadPickedFiles.length})
					{/if}
				</Button>
			</DialogFooter>
		</div>
	</DialogContent>
</Dialog>

<!-- Rename Dialog -->
<Dialog bind:open={renameOpen}>
	<DialogContent class="sm:max-w-md">
		<DialogHeader>
			<DialogTitle>Ubah Nama</DialogTitle>
			<DialogDescription class="font-mono text-xs break-all">{renameEntry?.path}</DialogDescription>
		</DialogHeader>
		<form onsubmit={submitRename} class="space-y-4">
			<Field>
				<FieldLabel for="fm-rename-input">Nama Baru</FieldLabel>
				<Input id="fm-rename-input" bind:value={renameNewName} required autofocus />
			</Field>
			<DialogFooter>
				<Button type="button" variant="outline" onclick={() => (renameOpen = false)} disabled={renameBusy}>
					Batal
				</Button>
				<Button type="submit" disabled={renameBusy || !renameNewName.trim()}>
					{#if renameBusy}<Spinner class="size-4" />{/if}
					Simpan
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- Interactive Chmod / Permission Dialog -->
<Dialog bind:open={chmodOpen}>
	<DialogContent class="sm:max-w-lg">
		<DialogHeader>
			<DialogTitle>Permission & Kepemilikan (Chmod)</DialogTitle>
			<DialogDescription class="text-xs">
				{#if chmodTargets.length === 1}
					Target: <code class="font-mono break-all">{chmodTargets[0]}</code>
				{:else}
					Target: <strong>{chmodTargets.length} item terpilih</strong>
				{/if}
			</DialogDescription>
		</DialogHeader>

		<form onsubmit={submitChmod} class="space-y-4">
			<div class="grid grid-cols-4 gap-2 rounded-lg border p-3 text-xs">
				<div class="text-muted-foreground font-semibold">Scope</div>
				<div class="text-center font-semibold">Read (4)</div>
				<div class="text-center font-semibold">Write (2)</div>
				<div class="text-center font-semibold">Exec (1)</div>

				{#each [
					{ label: 'Owner', shift: 6 },
					{ label: 'Group', shift: 3 },
					{ label: 'Public', shift: 0 }
				] as group (group.label)}
					<div class="flex items-center font-medium">{group.label}</div>
					{#each [4, 2, 1] as bitVal (bitVal)}
						<div class="flex justify-center">
							<Checkbox
								checked={hasPermBit(group.shift, bitVal)}
								onCheckedChange={() => togglePermBit(group.shift, bitVal)}
								aria-label={`${group.label} ${bitVal}`}
							/>
						</div>
					{/each}
				{/each}
			</div>

			<div class="flex flex-col gap-3 sm:flex-row sm:items-end">
				<Field class="flex-1">
					<FieldLabel for="fm-chmod-mode">Nilai Octal</FieldLabel>
					<Input
						id="fm-chmod-mode"
						bind:value={chmodMode}
						class="font-mono"
						placeholder="0755"
						maxlength={4}
					/>
				</Field>
				<div class="flex flex-wrap gap-1.5">
					{#each ['0755', '0644', '0775', '0600'] as preset (preset)}
						<Button
							type="button"
							variant={chmodMode === preset || chmodMode === preset.slice(1) ? 'secondary' : 'outline'}
							size="sm"
							class="h-8 font-mono text-xs"
							onclick={() => (chmodMode = preset)}
						>
							{preset}
						</Button>
					{/each}
				</div>
			</div>

			<div class="space-y-2 text-sm">
				<label class="flex items-center gap-2">
					<Checkbox bind:checked={chmodRecursive} />
					<span>Terapkan secara rekursif ke seluruh isi folder</span>
				</label>
				<label class="flex items-center gap-2">
					<Checkbox bind:checked={chmodWebOwner} />
					<span>Samakan kepemilikan (owner/group) ke user web server (<code class="font-mono text-xs">www-data / nginx</code>)</span>
				</label>
			</div>

			<DialogFooter>
				<Button type="button" variant="outline" onclick={() => (chmodOpen = false)} disabled={chmodBusy}>
					Batal
				</Button>
				<Button type="submit" disabled={chmodBusy}>
					{#if chmodBusy}<Spinner class="size-4" />{/if}
					Terapkan Permission
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- Compress ZIP Dialog -->
<Dialog bind:open={compressOpen}>
	<DialogContent class="sm:max-w-md">
		<DialogHeader>
			<DialogTitle>Kompres ke Arsip ZIP</DialogTitle>
			<DialogDescription class="text-xs">
				Mengarsipkan <strong>{compressTargets.length} item</strong> menjadi satu file `.zip`.
			</DialogDescription>
		</DialogHeader>
		<form onsubmit={submitCompress} class="space-y-4">
			<Field>
				<FieldLabel for="fm-compress-out">Path File Arsip ZIP Output</FieldLabel>
				<Input id="fm-compress-out" bind:value={compressOutput} class="font-mono text-xs" required autofocus />
			</Field>
			<DialogFooter>
				<Button type="button" variant="outline" onclick={() => (compressOpen = false)} disabled={compressBusy}>
					Batal
				</Button>
				<Button type="submit" disabled={compressBusy || !compressOutput.trim()}>
					{#if compressBusy}<Spinner class="size-4" />{/if}
					Buat Arsip ZIP
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- Extract Archive Dialog -->
<Dialog bind:open={extractOpen}>
	<DialogContent class="sm:max-w-md">
		<DialogHeader>
			<DialogTitle>Ekstrak Arsip</DialogTitle>
			<DialogDescription class="font-mono text-xs break-all">
				Arsip: {extractEntry?.name}
			</DialogDescription>
		</DialogHeader>
		<form onsubmit={submitExtract} class="space-y-4">
			<Field>
				<FieldLabel for="fm-extract-dest">Folder Tujuan Ekstraksi</FieldLabel>
				<Input id="fm-extract-dest" bind:value={extractDest} class="font-mono text-xs" required autofocus />
				<FieldDescription>Mendukung format .zip, .tar.gz, .tgz, dan .tar dengan proteksi Zip-Slip.</FieldDescription>
			</Field>
			<DialogFooter>
				<Button type="button" variant="outline" onclick={() => (extractOpen = false)} disabled={extractBusy}>
					Batal
				</Button>
				<Button type="submit" disabled={extractBusy || !extractDest.trim()}>
					{#if extractBusy}<Spinner class="size-4" />{/if}
					Ekstrak Sekarang
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>
