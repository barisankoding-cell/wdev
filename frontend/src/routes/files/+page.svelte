<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api/client.js';
	import FileEditorModal from '#lib/components/modals/FileEditorModal.svelte';
	import PaginationBar from '#lib/components/shared/PaginationBar.svelte';
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
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import { Field, FieldDescription, FieldLabel } from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
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
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import * as NativeSelect from '#lib/components/ui/native-select/index.js';
	import * as Sheet from '#lib/components/ui/sheet/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { cn } from '#lib/utils.js';
	import {
		hasOpenModal,
		registerActionSheet,
		registerDismissableMenu
	} from '#lib/modal-overlay.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import type { FileEntry, FileListResult, FileRootInfo } from '#lib/types/api.js';
	import ArrowDownIcon from '@lucide/svelte/icons/arrow-down';
	import ArrowUpDownIcon from '@lucide/svelte/icons/arrow-up-down';
	import ArrowUpIcon from '@lucide/svelte/icons/arrow-up';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import ContainerIcon from '@lucide/svelte/icons/container';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import ClipboardPasteIcon from '@lucide/svelte/icons/clipboard-paste';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import EllipsisVerticalIcon from '@lucide/svelte/icons/ellipsis-vertical';
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
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import HomeIcon from '@lucide/svelte/icons/home';
	import ImageIcon from '@lucide/svelte/icons/image';
	import LockIcon from '@lucide/svelte/icons/lock';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import ScissorsIcon from '@lucide/svelte/icons/scissors';
	import SearchIcon from '@lucide/svelte/icons/search';
	import SquareTerminalIcon from '@lucide/svelte/icons/square-terminal';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import UploadIcon from '@lucide/svelte/icons/upload';
	import XIcon from '@lucide/svelte/icons/x';
	import { tick, untrack } from 'svelte';
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

	interface ContextMenuState {
		x: number;
		y: number;
		entry: FileEntry | null;
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

	// Dialog: Upload & Remote Fetch URL
	interface UploadQueueItem {
		file: File;
		relativePath: string;
	}

	let uploadOpen = $state(false);
	let uploadTab = $state<'device' | 'url'>('device');
	let uploadQueue = $state<UploadQueueItem[]>([]);
	let uploadOverwrite = $state(true);
	let uploadProgress = $state<{ loaded: number; total: number; percent: number }>({
		loaded: 0,
		total: 0,
		percent: 0
	});
	let uploadAbortCtrl = $state<AbortController | null>(null);
	let uploadDropDragOver = $state(false);
	let uploadFileInputRef = $state<HTMLInputElement | null>(null);
	let uploadFolderInputRef = $state<HTMLInputElement | null>(null);

	let fetchUrl = $state('');
	let fetchFilename = $state('');
	let fetchOverwrite = $state(true);
	let fetchingUrl = $state(false);

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

	// Mobile sheets & responsive bar state
	let mobileEntrySheetOpen = $state(false);
	let mobileEntrySheetEntry = $state<FileEntry | null>(null);
	let mobileFolderSheetOpen = $state(false);
	let mobileBulkSheetOpen = $state(false);
	let breadcrumbScrollEl = $state<HTMLElement | null>(null);

	// Pagination (dinamis menyesuaikan tinggi area daftar agar tidak perlu scroll)
	const DEFAULT_PAGE_SIZE = 10;
	const MOBILE_PAGE_SIZE = 10;
	const BASE_ROW_PAD_Y = 11;
	const MAX_ROW_EXTRA_PAD_Y = 5;
	let currentPage = $state(1);
	let pageSize = $state(DEFAULT_PAGE_SIZE);
	let rowPadY = $state(BASE_ROW_PAD_Y);
	let listContainerEl = $state<HTMLDivElement | null>(null);

	let lastSyncedUrlParam: string | null = null;
	let loadSerial = 0;

	const currentPath = $derived(listing?.path ?? '');
	const roots = $derived<FileRootInfo[]>(listing?.roots ?? []);
	const hasMobileBottomBar = $derived(selectedPaths.length > 0 || clipboard !== null);

	$effect(() => {
		void breadcrumbs;
		const el = breadcrumbScrollEl;
		if (!el) return;
		queueMicrotask(() => {
			el.scrollLeft = el.scrollWidth;
		});
	});

	const isSearchMode = $derived(searchResults !== null);
	const rawEntries = $derived<FileEntry[]>(
		searchResults !== null ? searchResults : (listing?.entries ?? [])
	);
	const hasComposeManifest = $derived.by(() => {
		if (!listing || isSearchMode) return false;
		return listing.entries.some((e) => {
			if (e.is_dir) return false;
			const low = e.name.toLowerCase();
			return (
				low === 'compose.yaml' ||
				low === 'compose.yml' ||
				low === 'docker-compose.yaml' ||
				low === 'docker-compose.yml'
			);
		});
	});

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

	const totalPages = $derived(Math.max(1, Math.ceil(visibleEntries.length / pageSize)));
	const pagedEntries = $derived(
		visibleEntries.slice((currentPage - 1) * pageSize, currentPage * pageSize)
	);

	const selectedSet = $derived(new Set(selectedPaths));
	const allVisibleSelected = $derived(
		pagedEntries.length > 0 && pagedEntries.every((e) => selectedSet.has(e.path))
	);
	const someVisibleSelected = $derived(
		pagedEntries.some((e) => selectedSet.has(e.path)) && !allVisibleSelected
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
		return i18n.formatDateTime(d, {
			day: '2-digit',
			month: 'short',
			year: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	function formatCompactModTime(iso: string): string {
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return i18n.formatDateTime(d, {
			day: '2-digit',
			month: 'short',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	function splitDisplayName(name: string, isDir: boolean): { base: string; ext: string } {
		if (isDir) return { base: name, ext: '' };
		const lower = name.toLowerCase();
		for (const compound of ['.tar.gz', '.tar.bz2', '.tar.xz', '.tar.zst']) {
			if (lower.endsWith(compound) && name.length > compound.length) {
				const cut = name.length - compound.length;
				return { base: name.slice(0, cut), ext: name.slice(cut) };
			}
		}
		const dotIdx = name.lastIndexOf('.');
		if (dotIdx <= 0 || name.length - dotIdx > 10) {
			return { base: name, ext: '' };
		}
		return { base: name.slice(0, dotIdx), ext: name.slice(dotIdx) };
	}

	function handleSortSelect(value: string) {
		const next = (value === 'size' || value === 'mod_time' ? value : 'name') as SortKey;
		if (sortBy === next) return;
		sortBy = next;
		sortDir = next === 'mod_time' ? 'desc' : 'asc';
		currentPage = 1;
	}

	let lastFittedSignature = '';
	let clampedLayoutKey = '';

	function recalcPageSize() {
		const el = listContainerEl;
		if (!el || typeof window === 'undefined') return;
		if (window.innerWidth < 768) {
			if (pageSize !== MOBILE_PAGE_SIZE) {
				pageSize = MOBILE_PAGE_SIZE;
			}
			if (rowPadY !== BASE_ROW_PAD_Y) {
				rowPadY = BASE_ROW_PAD_Y;
			}
			return;
		}
		const w = el.clientWidth;
		const h = el.clientHeight;
		if (h < 80) return;

		const desktopTable = w >= 704;
		const layoutKey = `${h}:${isSearchMode}:${desktopTable}`;
		if (clampedLayoutKey === layoutKey) return;

		if (desktopTable) {
			const thead = el.querySelector('thead');
			const theadH = thead ? Math.ceil(thead.getBoundingClientRect().height) : 41;
			const firstCell = el.querySelector('tbody td') as HTMLElement | null;
			const renderedRows = el.querySelectorAll('tbody tr');
			let baseRowH = isSearchMode ? 63 : 47;
			if (renderedRows.length > 0) {
				let minMeasured = Infinity;
				for (const r of renderedRows) {
					const rh = r.getBoundingClientRect().height;
					if (rh > 0 && rh < minMeasured) minMeasured = rh;
				}
				if (Number.isFinite(minMeasured)) {
					const appliedPadY = firstCell
						? Number.parseFloat(getComputedStyle(firstCell).paddingTop) || BASE_ROW_PAD_Y
						: BASE_ROW_PAD_Y;
					const normalized = Math.ceil(minMeasured - 2 * Math.max(0, appliedPadY - BASE_ROW_PAD_Y));
					baseRowH = Math.max(isSearchMode ? 58 : 44, normalized);
				}
			}

			const availRowsH = Math.max(baseRowH, h - theadH - 6);
			const next = Math.max(4, Math.floor(availRowsH / baseRowH));
			const leftover = Math.max(0, availRowsH - next * baseRowH);
			const extraPad = Math.min(MAX_ROW_EXTRA_PAD_Y, Math.floor(leftover / (next * 2)));
			const nextRowPadY = BASE_ROW_PAD_Y + extraPad;

			if (next !== pageSize) {
				pageSize = next;
			}
			if (nextRowPadY !== rowPadY) {
				rowPadY = nextRowPadY;
			}
		} else {
			const headerBarH = 58;
			const rowH = isSearchMode ? 80 : 62;
			const availRowsH = Math.max(rowH, h - headerBarH - 6);
			const next = Math.max(3, Math.floor(availRowsH / rowH));
			if (next !== pageSize) {
				pageSize = next;
			}
			if (rowPadY !== BASE_ROW_PAD_Y) {
				rowPadY = BASE_ROW_PAD_Y;
			}
		}
	}

	$effect(() => {
		if (currentPage > totalPages) {
			currentPage = totalPages;
		}
	});

	$effect(() => {
		const el = listContainerEl;
		void isSearchMode;
		void currentPath;
		if (!el || typeof ResizeObserver === 'undefined') return;
		clampedLayoutKey = '';
		untrack(() => recalcPageSize());
		const ro = new ResizeObserver(() => {
			recalcPageSize();
		});
		ro.observe(el);
		return () => {
			ro.disconnect();
		};
	});

	$effect(() => {
		const el = listContainerEl;
		if (!el || typeof window === 'undefined' || window.innerWidth < 768) return;
		const sig = `${currentPage}:${pageSize}:${rowPadY}:${pagedEntries.length}:${isSearchMode}:${el.clientHeight}`;
		if (sig === lastFittedSignature) return;
		lastFittedSignature = sig;
		void tick().then(() => {
			const box = listContainerEl;
			if (!box) return;
			const isDesktopTable = box.clientWidth >= 704;
			const scrollBox = isDesktopTable
				? (box.querySelector('.responsive-table-region') as HTMLElement | null)
				: box;
			if (scrollBox && scrollBox.scrollHeight > scrollBox.clientHeight + 2) {
				clampedLayoutKey = `${box.clientHeight}:${isSearchMode}:${isDesktopTable}`;
				if (isDesktopTable && rowPadY > BASE_ROW_PAD_Y) {
					rowPadY = BASE_ROW_PAD_Y;
					return;
				}
				if (pageSize > 3) {
					const excess = scrollBox.scrollHeight - scrollBox.clientHeight;
					const approxRowH = isDesktopTable ? (isSearchMode ? 63 : 47) : isSearchMode ? 80 : 62;
					const step = Math.max(1, Math.ceil(excess / approxRowH));
					pageSize = Math.max(3, pageSize - step);
				}
			}
		});
	});

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
		if (clean !== currentPath) {
			currentPage = 1;
		}
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
		const prevPath = currentPath;
		loading = true;
		errorMsg = null;
		selectedPaths = [];
		try {
			const res = await api.listFiles(targetPath, showHidden);
			if (serial !== loadSerial) return;
			listing = res;
			pathInput = res.path;
			pathEditMode = false;
			if (res.path !== prevPath) {
				currentPage = 1;
			}
		} catch (err) {
			if (serial !== loadSerial) return;
			errorMsg = formatErr(err, t('files.page.toast.loadDirFailed'));
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
		currentPage = 1;
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
		currentPage = 1;
		try {
			const res = await api.searchFiles(currentPath, q, showHidden);
			searchResults = res.entries ?? [];
			activeSearchQuery = q;
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.searchFailed')));
		} finally {
			searching = false;
		}
	}

	function clearSearch() {
		searchInput = '';
		activeSearchQuery = '';
		searchResults = null;
		currentPage = 1;
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
		const pagePathSet = new Set(pagedEntries.map((e) => e.path));
		if (checked) {
			const next = [...selectedPaths];
			for (const entry of pagedEntries) {
				if (!selectedSet.has(entry.path)) {
					next.push(entry.path);
				}
			}
			selectedPaths = next;
		} else {
			selectedPaths = selectedPaths.filter((p) => !pagePathSet.has(p));
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
			mode === 'copy'
				? t('files.page.toast.clipboardSetCopy', { count: paths.length })
				: t('files.page.toast.clipboardSetMove', { count: paths.length })
		);
	}

	async function pasteClipboard() {
		if (!clipboard || !currentPath || bulkBusy) return;
		const { mode, paths } = clipboard;
		bulkBusy = true;
		try {
			if (mode === 'copy') {
				const res = await api.copyFiles(paths, currentPath);
				toast.success(t('files.page.toast.copiedCount', { count: res.count }));
			} else {
				const res = await api.moveFiles(paths, currentPath);
				toast.success(t('files.page.toast.movedCount', { count: res.count }));
				clipboard = null;
			}
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.pasteFailed')));
		} finally {
			bulkBusy = false;
		}
	}

	async function confirmDelete(paths: string[]) {
		if (paths.length === 0 || bulkBusy) return;
		const label =
			paths.length === 1
				? `"${paths[0].split('/').pop() || paths[0]}"`
				: t('files.page.toast.deleteConfirmItemMulti', { count: paths.length });
		const ok = await cp.confirm({
			title: t('files.page.toast.deleteConfirmTitle'),
			description: t('files.page.toast.deleteConfirmDesc', { label }),
			confirmText: t('files.page.toast.deleteConfirmBtn'),
			destructive: true
		});
		if (!ok) return;

		bulkBusy = true;
		try {
			const res = await api.deleteFiles(paths);
			toast.success(t('files.page.toast.deletedCount', { count: res.count }));
			selectedPaths = selectedPaths.filter((p) => !paths.includes(p));
			if (clipboard) {
				const remaining = clipboard.paths.filter((p) => !paths.includes(p));
				clipboard = remaining.length > 0 ? { ...clipboard, paths: remaining } : null;
			}
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.deleteFailed')));
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
			toast.error(t('files.page.toast.invalidFileNameNoSlash'));
			return;
		}
		const targetPath = joinChildPath(currentPath, name);
		newFileBusy = true;
		try {
			await api.writeFileContent(targetPath, '');
			newFileOpen = false;
			toast.success(t('files.page.toast.fileCreated', { name }));
			await loadDirectory(currentPath);
			if (newFileOpenAfter) {
				const created = listing?.entries.find((item) => item.path === targetPath);
				if (created) {
					openEditor(created);
				}
			}
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.createFileFailed')));
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
			toast.error(t('files.page.toast.invalidFolderNameNoSlash'));
			return;
		}
		newFolderBusy = true;
		try {
			await api.mkdirFile(joinChildPath(currentPath, name));
			newFolderOpen = false;
			toast.success(t('files.page.toast.folderCreated', { name }));
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.createFolderFailed')));
		} finally {
			newFolderBusy = false;
		}
	}

	// Upload & Remote URL Download
	interface WebkitEntryLike {
		isFile: boolean;
		isDirectory: boolean;
		name: string;
		fullPath?: string;
		file?: (cb: (f: File) => void, err?: (e: unknown) => void) => void;
		createReader?: () => {
			readEntries: (cb: (entries: WebkitEntryLike[]) => void, err?: (e: unknown) => void) => void;
		};
	}

	function setDirectoryAttr(node: HTMLInputElement) {
		node.webkitdirectory = true;
	}

	const uploadTotalBytes = $derived(uploadQueue.reduce((sum, item) => sum + item.file.size, 0));

	async function readWebkitEntryTree(entry: WebkitEntryLike, prefix = ''): Promise<UploadQueueItem[]> {
		if (entry.isFile && entry.file) {
			const file = await new Promise<File>((resolve, reject) => {
				entry.file!(resolve, reject);
			});
			return [{ file, relativePath: prefix ? `${prefix}/${entry.name}` : entry.name }];
		}
		if (entry.isDirectory && entry.createReader) {
			const reader = entry.createReader();
			const allChildren: WebkitEntryLike[] = [];
			while (true) {
				const batch = await new Promise<WebkitEntryLike[]>((resolve, reject) => {
					reader.readEntries(resolve, reject);
				});
				if (batch.length === 0) break;
				allChildren.push(...batch);
			}
			const nextPrefix = prefix ? `${prefix}/${entry.name}` : entry.name;
			const nested: UploadQueueItem[] = [];
			for (const child of allChildren) {
				nested.push(...(await readWebkitEntryTree(child, nextPrefix)));
			}
			return nested;
		}
		return [];
	}

	async function collectDroppedItems(dt: DataTransfer | null): Promise<UploadQueueItem[]> {
		if (!dt) return [];
		const rawItems = dt.items ? Array.from(dt.items) : [];
		const entryGetters = rawItems
			.map((it) => {
				const withEntry = it as unknown as { webkitGetAsEntry?: () => WebkitEntryLike | null };
				return withEntry.webkitGetAsEntry ? withEntry.webkitGetAsEntry() : null;
			})
			.filter((e): e is WebkitEntryLike => e !== null);

		if (entryGetters.length > 0) {
			const collected: UploadQueueItem[] = [];
			for (const entry of entryGetters) {
				collected.push(...(await readWebkitEntryTree(entry, '')));
			}
			return collected;
		}
		return Array.from(dt.files ?? []).map((file) => ({ file, relativePath: file.name }));
	}

	function addFilesToQueue(list: FileList | null) {
		if (!list || list.length === 0) return;
		const nextItems: UploadQueueItem[] = Array.from(list).map((file) => {
			const rel = (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name;
			return { file, relativePath: rel };
		});
		mergeQueueItems(nextItems);
	}

	function mergeQueueItems(nextItems: UploadQueueItem[]) {
		const map = new Map<string, UploadQueueItem>();
		for (const item of uploadQueue) {
			map.set(item.relativePath, item);
		}
		for (const item of nextItems) {
			map.set(item.relativePath, item);
		}
		uploadQueue = Array.from(map.values());
	}

	function removeUploadQueueIndex(idx: number) {
		uploadQueue = uploadQueue.filter((_, i) => i !== idx);
	}

	function openUploadDialog(initialTab: 'device' | 'url' = 'device') {
		uploadTab = initialTab;
		if (!uploading) {
			uploadQueue = [];
			uploadProgress = { loaded: 0, total: 0, percent: 0 };
		}
		fetchUrl = '';
		fetchFilename = '';
		uploadOpen = true;
	}

	function handleUploadFileChange(e: Event) {
		const target = e.currentTarget as HTMLInputElement;
		addFilesToQueue(target.files);
		target.value = '';
	}

	function handleUploadDropzoneDragOver(e: DragEvent) {
		if (!e.dataTransfer?.types?.includes('Files')) return;
		e.preventDefault();
		e.stopPropagation();
		uploadDropDragOver = true;
	}

	function handleUploadDropzoneDragLeave(e: DragEvent) {
		e.preventDefault();
		e.stopPropagation();
		uploadDropDragOver = false;
	}

	async function handleUploadDropzoneDrop(e: DragEvent) {
		e.preventDefault();
		e.stopPropagation();
		uploadDropDragOver = false;
		const items = await collectDroppedItems(e.dataTransfer);
		if (items.length > 0) {
			mergeQueueItems(items);
		}
	}

	function cancelUpload() {
		uploadAbortCtrl?.abort();
	}

	async function performUpload(items: UploadQueueItem[], overwrite = uploadOverwrite) {
		if (!currentPath || items.length === 0 || uploading) return;
		uploading = true;
		const totalEst = items.reduce((sum, i) => sum + i.file.size, 0);
		uploadProgress = { loaded: 0, total: totalEst, percent: 0 };
		const ctrl = new AbortController();
		uploadAbortCtrl = ctrl;

		try {
			const res = await api.uploadFiles(currentPath, items, {
				overwrite,
				signal: ctrl.signal,
				onProgress: (info) => {
					uploadProgress = info;
				}
			});
			uploadOpen = false;
			uploadQueue = [];
			if (uploadFileInputRef) uploadFileInputRef.value = '';
			if (uploadFolderInputRef) uploadFolderInputRef.value = '';
			const skippedCount = res.skipped_count ?? 0;
			if (skippedCount > 0) {
				toast.success(t('files.page.toast.uploadedSkipped', { count: res.count, skipped: skippedCount }));
			} else {
				toast.success(t('files.page.toast.uploadedSuccess', { count: res.count, path: currentPath }));
			}
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.uploadFailed')));
		} finally {
			uploading = false;
			uploadAbortCtrl = null;
		}
	}

	async function submitFetchURL(e: SubmitEvent) {
		e.preventDefault();
		const targetURL = fetchUrl.trim();
		if (!currentPath || !targetURL || fetchingUrl) return;
		fetchingUrl = true;
		try {
			const res = await api.fetchFileFromURL(currentPath, targetURL, fetchFilename.trim(), fetchOverwrite);
			uploadOpen = false;
			toast.success(t('files.page.toast.fetchedFromUrl', { name: res.entry.name, size: formatBytes(res.entry.size), path: currentPath }));
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.fetchUrlFailed')));
		} finally {
			fetchingUrl = false;
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

	async function handleWorkspaceDrop(e: DragEvent) {
		e.preventDefault();
		dragCounter = 0;
		dragOver = false;
		const items = await collectDroppedItems(e.dataTransfer);
		if (items.length > 0 && currentPath && !isSearchMode) {
			await performUpload(items, true);
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
			toast.success(t('files.page.toast.renamed', { name: nextName }));
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.renameFailed')));
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
			toast.error(t('files.page.toast.invalidOctal'));
			return;
		}
		chmodBusy = true;
		try {
			await api.chmodFiles(chmodTargets, modeClean, chmodRecursive, chmodWebOwner);
			chmodOpen = false;
			toast.success(t('files.page.toast.chmodUpdated'));
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.chmodFailed')));
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
				: `${t('files.page.dialogs.archivePrefixName')}-${new Date().toISOString().slice(0, 10)}.zip`;
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
			toast.success(t('files.page.toast.compressed', { name: res.entry.name }));
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.compressFailed')));
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
			toast.success(t('files.page.toast.extracted', { count: res.count }));
			await refreshCurrent();
		} catch (err) {
			toast.error(formatErr(err, t('files.page.toast.extractFailed')));
		} finally {
			extractBusy = false;
		}
	}

	let contextMenu = $state<ContextMenuState | null>(null);

	function openMobileEntrySheet(entry: FileEntry) {
		closeContextMenu();
		mobileEntrySheetEntry = entry;
		mobileEntrySheetOpen = true;
	}

	function openContextMenu(e: MouseEvent, entry: FileEntry | null) {
		if (!currentPath) return;
		e.preventDefault();
		e.stopPropagation();
		if (
			typeof window !== 'undefined' &&
			(window.matchMedia('(pointer: coarse)').matches || window.innerWidth < 640)
		) {
			if (entry) {
				openMobileEntrySheet(entry);
			} else {
				mobileFolderSheetOpen = true;
			}
			return;
		}
		const vw = typeof window !== 'undefined' ? window.innerWidth : 1024;
		const vh = typeof window !== 'undefined' ? window.innerHeight : 768;
		const x = Math.max(8, Math.min(e.clientX, vw - 240));
		const y = Math.max(8, Math.min(e.clientY, vh - 360));
		contextMenu = { x, y, entry };
	}

	function closeContextMenu() {
		contextMenu = null;
	}

	function runContextMenuAction(action: (entry: FileEntry) => void) {
		const entry = contextMenu?.entry;
		closeContextMenu();
		if (!entry) return;
		action(entry);
	}

	function closeMobileActionSheets() {
		mobileFolderSheetOpen = false;
		mobileEntrySheetOpen = false;
		mobileBulkSheetOpen = false;
	}

	$effect(() => {
		const unregMenu = registerDismissableMenu(closeContextMenu);
		const unregSheet = registerActionSheet(closeMobileActionSheets);
		return () => {
			unregMenu();
			unregSheet();
		};
	});

	function openTerminalAt(targetPath: string) {
		closeContextMenu();
		const clean = targetPath.trim();
		const url = clean ? `/terminal?cwd=${encodeURIComponent(clean)}` : '/terminal';
		void goto(url);
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

{#snippet entryIcon(entry: FileEntry, extraClass = '')}
	{#if entry.is_dir}
		{#if entry.is_symlink}
			<FolderSymlinkIcon class={cn('size-4 shrink-0 text-amber-500', extraClass)} />
		{:else}
			<FolderIcon class={cn('size-4 shrink-0 text-amber-500', extraClass)} />
		{/if}
	{:else if entry.is_image}
		<ImageIcon class={cn('size-4 shrink-0 text-purple-500', extraClass)} />
	{:else if entry.is_archive}
		<FileArchiveIcon class={cn('size-4 shrink-0 text-orange-500', extraClass)} />
	{:else if isCodeFile(entry)}
		<FileCodeIcon class={cn('size-4 shrink-0 text-emerald-500', extraClass)} />
	{:else if isTextFile(entry)}
		<FileTextIcon class={cn('size-4 shrink-0 text-sky-500', extraClass)} />
	{:else}
		<FileIcon class={cn('text-muted-foreground size-4 shrink-0', extraClass)} />
	{/if}
{/snippet}

{#snippet filenameDisplay(entry: FileEntry)}
	{@const parts = splitDisplayName(entry.name, entry.is_dir)}
	{#if parts.ext}
		<span class="fm-filename"><span>{parts.base}</span>{parts.ext}</span>
	{:else}
		<span class="block truncate">{parts.base}</span>
	{/if}
{/snippet}

<div class={cn('flex flex-col gap-4 sm:gap-5 md:min-h-0 md:flex-1', hasMobileBottomBar && 'pb-24 md:pb-0')}>
	<!-- Page Header -->
	<div class="flex shrink-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div class="min-w-0">
			<h1 class="text-xl font-bold tracking-tight text-balance sm:text-2xl">{t('files.page.title')}</h1>
			<p class="text-muted-foreground text-xs sm:text-sm">
				{t('files.page.subtitle')}
			</p>
		</div>

		<!-- Desktop Header Actions -->
		<div class="hidden flex-wrap items-center gap-2 sm:flex">
			{#if hasComposeManifest && currentPath}
				<Button variant="secondary" size="sm" href={`/compose?dir=${encodeURIComponent(currentPath)}`}>
					<ContainerIcon data-icon="inline-start" />
					{t('files.page.manageInCompose')}
				</Button>
			{/if}
			<Button variant="outline" size="sm" onclick={() => openTerminalAt(currentPath)} disabled={!currentPath}>
				<SquareTerminalIcon data-icon="inline-start" />
				{t('files.page.openTerminal')}
			</Button>
			<Button variant="outline" size="sm" onclick={openNewFileDialog} disabled={!currentPath || isSearchMode}>
				<FilePlusIcon data-icon="inline-start" />
				{t('files.page.newFile')}
			</Button>
			<Button variant="outline" size="sm" onclick={openNewFolderDialog} disabled={!currentPath || isSearchMode}>
				<FolderPlusIcon data-icon="inline-start" />
				{t('files.page.newFolder')}
			</Button>
			<Button
				variant="outline"
				size="sm"
				disabled={!currentPath}
				onclick={() => triggerDownload(selectedPaths.length > 0 ? selectedPaths : [currentPath])}
			>
				<DownloadIcon data-icon="inline-start" />
				{selectedPaths.length > 0 ? t('files.page.downloadSelected', { count: selectedPaths.length }) : t('files.page.downloadFolder')}
			</Button>
			<Button size="sm" onclick={() => openUploadDialog('device')} disabled={!currentPath || isSearchMode}>
				{#if uploading}
					<Spinner data-icon="inline-start" />
					{t('files.page.uploadProgress', { percent: uploadProgress.percent })}
				{:else}
					<UploadIcon data-icon="inline-start" />
					{t('files.page.uploadBtn')}
				{/if}
			</Button>
		</div>

		<!-- Mobile Header Actions -->
		<div class="flex items-center gap-2 sm:hidden">
			<Button
				class="min-h-11 flex-1"
				onclick={() => openUploadDialog('device')}
				disabled={!currentPath || isSearchMode}
			>
				{#if uploading}
					<Spinner data-icon="inline-start" />
					{t('files.page.uploadProgress', { percent: uploadProgress.percent })}
				{:else}
					<UploadIcon data-icon="inline-start" />
					{t('files.page.uploadBtn')}
				{/if}
			</Button>
			<Button
				variant="outline"
				class="min-h-11 flex-1"
				onclick={() => (mobileFolderSheetOpen = true)}
				disabled={!currentPath}
			>
				<PlusIcon data-icon="inline-start" />
				{t('files.page.newAndActionsBtn')}
			</Button>
			<Button
				variant="outline"
				size="icon-lg"
				class="min-h-11 min-w-11 shrink-0"
				aria-label={t('files.page.openTerminalHereAria')}
				onclick={() => openTerminalAt(currentPath)}
				disabled={!currentPath}
			>
				<SquareTerminalIcon />
			</Button>
		</div>
	</div>

	<!-- Quick Access Roots / VHosts -->
	{#if roots.length > 0}
		<nav
			aria-label={t('files.page.quickAccessAria')}
			class="flex shrink-0 items-center gap-2 overflow-x-auto overscroll-x-contain pb-1 sm:flex-wrap sm:overflow-visible sm:pb-0"
		>
			<span class="text-muted-foreground shrink-0 text-xs font-medium uppercase tracking-wide">{t('files.page.quickAccessLabel')}</span>
			{#each roots as root (root.path)}
				{@const active = currentPath === root.path || currentPath.startsWith(root.path.replace(/\/+$/, '') + '/')}
				<Button
					variant={active ? 'secondary' : 'outline'}
					size="sm"
					class="fm-touch-btn h-8 shrink-0 gap-1.5 text-xs sm:h-7"
					onclick={() => navigateTo(root.path)}
				>
					{#if root.kind === 'home'}
						<HomeIcon data-icon="inline-start" />
					{:else}
						<HardDriveIcon data-icon="inline-start" />
					{/if}
					<span>{root.name}</span>
				</Button>
			{/each}
		</nav>
	{/if}

	<!-- Main Explorer Card (with drag-and-drop dropzone) -->
	<Card
		class={cn(
			'relative flex min-h-[420px] flex-col gap-0 overflow-hidden pb-0 transition-colors md:min-h-0 md:flex-1',
			dragOver && 'border-primary ring-primary/30 ring-2'
		)}
		ondragenter={handleWorkspaceDragEnter}
		ondragover={handleWorkspaceDragOver}
		ondragleave={handleWorkspaceDragLeave}
		ondrop={handleWorkspaceDrop}
		oncontextmenu={(e) => openContextMenu(e, null)}
	>
		{#if dragOver}
			<div class="bg-background/85 pointer-events-none absolute inset-0 z-20 flex flex-col items-center justify-center rounded-xl border-2 border-dashed border-primary p-4 text-center backdrop-blur-[1px]">
				<UploadIcon class="text-primary mb-2 size-10" />
				<p class="text-sm font-semibold">{t('files.page.dropzoneReleaseTitle')}</p>
				<p class="text-muted-foreground mt-1 break-all font-mono text-xs">{currentPath}</p>
			</div>
		{/if}

		<CardHeader class="flex shrink-0 flex-col gap-3 pb-4">
			<!-- Toolbar Row: Up / Home / Refresh / Breadcrumb or Jump Path / Hidden Toggle / Search -->
			<div class="flex flex-col gap-2.5 lg:flex-row lg:items-center lg:justify-between">
				<div class="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
					<Button
						variant="outline"
						size="icon-sm"
						class="fm-touch-btn shrink-0"
						title={t('files.page.upTitle')}
						aria-label={t('files.page.upTitle')}
						disabled={!listing?.can_go_up || loading || isSearchMode}
						onclick={() => listing?.parent && navigateTo(listing.parent)}
					>
						<ArrowUpIcon />
					</Button>
					<Button
						variant="outline"
						size="icon-sm"
						class="fm-touch-btn shrink-0"
						title={t('files.page.homeTitle')}
						aria-label={t('files.page.homeTitle')}
						disabled={loading}
						onclick={() => navigateTo(roots[0]?.path ?? '')}
					>
						<HomeIcon />
					</Button>
					<Button
						variant="outline"
						size="icon-sm"
						class="fm-touch-btn shrink-0"
						title={t('files.page.refreshTitle')}
						aria-label={t('files.page.refreshAria')}
						disabled={loading || searching}
						onclick={refreshCurrent}
					>
						<RefreshCwIcon class={cn((loading || searching) && 'animate-spin')} />
					</Button>
					<Button
						variant={showHidden ? 'secondary' : 'outline'}
						size="sm"
						class="fm-touch-btn ml-auto h-8 gap-1.5 text-xs sm:hidden"
						aria-pressed={showHidden}
						onclick={toggleShowHidden}
					>
						{#if showHidden}
							<EyeIcon data-icon="inline-start" />
						{:else}
							<EyeOffIcon data-icon="inline-start" />
						{/if}
						.dot
					</Button>

					<!-- Breadcrumb / Inline Path Input -->
					{#if pathEditMode}
						<form onsubmit={handleJumpSubmit} class="flex min-w-0 flex-1 basis-full items-center gap-1.5 sm:basis-0">
							<Input
								bind:value={pathInput}
								class="h-10 min-w-0 flex-1 font-mono text-base sm:h-8 sm:text-xs"
								placeholder={t('files.page.pathInputPlaceholder')}
								aria-label={t('files.page.pathInputAria')}
								autocapitalize="none"
								autocorrect="off"
								spellcheck={false}
								autofocus
							/>
							<Button type="submit" size="sm" class="fm-touch-btn h-10 shrink-0 sm:h-8">{t('files.page.openBtn')}</Button>
							<Button
								type="button"
								variant="ghost"
								size="icon-sm"
								class="fm-touch-btn shrink-0"
								onclick={() => {
									pathEditMode = false;
									pathInput = currentPath;
								}}
								aria-label={t('files.page.cancelPathEditAria')}
							>
								<XIcon />
							</Button>
						</form>
					{:else}
						<div class="bg-muted/40 flex min-h-9 min-w-0 flex-1 basis-full items-center gap-1 rounded-md border px-2 py-1 text-xs sm:min-h-8 sm:basis-0">
							<nav
								bind:this={breadcrumbScrollEl}
								aria-label={t('files.page.dirLocationAria')}
								class="flex min-w-0 flex-1 flex-nowrap items-center gap-0.5 overflow-x-auto overscroll-x-contain sm:flex-wrap sm:overflow-visible"
							>
								{#if breadcrumbs.length === 0}
									<span class="text-muted-foreground font-mono">{t('files.page.loadingPath')}</span>
								{:else}
									{#each breadcrumbs as seg, idx (seg.path)}
										{#if idx > 0}
											<ChevronRightIcon class="text-muted-foreground/60 size-3.5 shrink-0" aria-hidden="true" />
										{/if}
										{#if seg.clickable && seg.path !== currentPath}
											<button
												type="button"
												class="fm-touch-hit hover:bg-accent hover:text-accent-foreground shrink-0 rounded px-1.5 py-1 font-mono transition-colors sm:py-0.5"
												onclick={() => navigateTo(seg.path)}
											>
												{seg.label}
											</button>
										{:else if seg.path === currentPath}
											<span class="text-foreground bg-background/80 shrink-0 rounded px-1.5 py-1 font-mono font-semibold shadow-2xs sm:bg-transparent sm:py-0.5 sm:shadow-none">
												{seg.label}
											</span>
										{:else}
											<span class="text-muted-foreground shrink-0 px-1 py-1 font-mono sm:py-0.5">
												{seg.label}
											</span>
										{/if}
									{/each}
								{/if}
							</nav>
							<button
								type="button"
								class="fm-touch-hit text-muted-foreground hover:text-foreground hover:bg-accent inline-flex size-7 shrink-0 items-center justify-center rounded sm:size-6"
								title={t('files.page.editPathManuallyTitle')}
								aria-label={t('files.page.editPathManuallyTitle')}
								onclick={() => {
									pathInput = currentPath;
									pathEditMode = true;
								}}
							>
								<PencilIcon class="size-3.5 sm:size-3" />
							</button>
						</div>
					{/if}
				</div>

				<!-- Search & Hidden Toggle -->
				<div class="flex w-full flex-wrap items-center gap-2 lg:w-auto lg:flex-nowrap">
					<Button
						variant={showHidden ? 'secondary' : 'outline'}
						size="sm"
						class="hidden h-8 shrink-0 gap-1.5 text-xs sm:inline-flex"
						aria-pressed={showHidden}
						onclick={toggleShowHidden}
						title={t('files.page.toggleHiddenTitle')}
					>
						{#if showHidden}
							<EyeIcon data-icon="inline-start" />
						{:else}
							<EyeOffIcon data-icon="inline-start" />
						{/if}
						{t('files.page.hiddenDotLabel')}
					</Button>

					<form
						onsubmit={(e) => {
							e.preventDefault();
							void runSearch();
						}}
						class="flex min-w-0 flex-1 items-center gap-1.5 lg:flex-initial"
					>
						<InputGroup.Root class="h-10 min-w-0 flex-1 sm:h-8 lg:w-56">
							<InputGroup.Addon align="inline-start">
								<SearchIcon />
							</InputGroup.Addon>
							<InputGroup.Input
								type="search"
								bind:value={searchInput}
								placeholder={t('files.page.searchPlaceholder')}
								aria-label={t('files.page.searchAria')}
								class="h-full text-base sm:text-xs"
							/>
							{#if searchInput}
								<InputGroup.Addon align="inline-end">
									<InputGroup.Button
										type="button"
										size="icon-xs"
										class="fm-touch-hit"
										aria-label={t('files.page.clearSearchAria')}
										onclick={() => (searchInput = '')}
									>
										<XIcon />
									</InputGroup.Button>
								</InputGroup.Addon>
							{/if}
						</InputGroup.Root>
						<Button type="submit" variant="outline" size="sm" class="fm-touch-btn h-10 shrink-0 sm:h-8" disabled={searching || !currentPath}>
							{#if searching}
								<Spinner />
							{:else}
								{t('files.page.searchBtn')}
							{/if}
						</Button>
						{#if isSearchMode}
							<Button type="button" variant="ghost" size="sm" class="fm-touch-btn h-10 shrink-0 text-xs sm:h-8" onclick={clearSearch}>
								{t('files.page.resetBtn')}
							</Button>
						{/if}
					</form>
				</div>
			</div>

			<!-- Clipboard Banner (Desktop / >= md; mobile uses sticky bottom bar) -->
			{#if clipboard}
				<div class="bg-primary/5 border-primary/25 hidden flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-xs md:flex">
					<div class="flex min-w-0 items-center gap-2">
						{#if clipboard.mode === 'copy'}
							<CopyIcon class="text-primary size-4 shrink-0" />
						{:else}
							<ScissorsIcon class="text-primary size-4 shrink-0" />
						{/if}
						<span class="min-w-0">
							<strong>{t('files.page.clipboardCountItems', { count: clipboard.paths.length })}</strong> {clipboard.mode === 'copy' ? t('files.page.clipboardBannerCopy') : t('files.page.clipboardBannerMove')} {t('files.page.clipboardBannerFrom')}
							<code class="bg-muted rounded px-1 py-0.5 font-mono break-all">{clipboard.sourceDir}</code>
						</span>
					</div>
					<div class="flex items-center gap-1.5">
						<Button size="sm" class="h-7 gap-1 text-xs" disabled={bulkBusy || isSearchMode || !currentPath} onclick={pasteClipboard}>
							{#if bulkBusy}
								<Spinner data-icon="inline-start" />
							{:else}
								<ClipboardPasteIcon data-icon="inline-start" />
							{/if}
							{t('files.page.pasteHereBtn')}
						</Button>
						<Button variant="ghost" size="sm" class="h-7 text-xs" onclick={() => (clipboard = null)}>
							{t('files.page.cancelBtn')}
						</Button>
					</div>
				</div>
			{/if}

			<!-- Multi-select Bulk Actions Bar (Desktop / >= md; mobile uses sticky bottom bar) -->
			{#if selectedPaths.length > 0}
				<div class="bg-muted/60 hidden flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-xs md:flex">
					<div class="flex items-center gap-2 font-medium">
						<Badge variant="secondary">{t('files.page.selectedCount', { count: selectedPaths.length })}</Badge>
						<button
							type="button"
							class="text-muted-foreground hover:text-foreground underline"
							onclick={() => (selectedPaths = [])}
						>
							{t('files.page.clearSelection')}
						</button>
					</div>
					<div class="flex flex-wrap items-center gap-1.5">
						<Button variant="outline" size="sm" class="h-7 gap-1 text-xs" onclick={() => setClipboard('copy', selectedPaths)}>
							<CopyIcon data-icon="inline-start" />
							{t('files.page.copyBtn')}
						</Button>
						<Button variant="outline" size="sm" class="h-7 gap-1 text-xs" onclick={() => setClipboard('move', selectedPaths)}>
							<ScissorsIcon data-icon="inline-start" />
							{t('files.page.moveBtn')}
						</Button>
						<Button variant="outline" size="sm" class="h-7 gap-1 text-xs" onclick={() => openCompressDialog(selectedPaths)}>
							<FileArchiveIcon data-icon="inline-start" />
							{t('files.page.compressZipBtn')}
						</Button>
						<Button variant="outline" size="sm" class="h-7 gap-1 text-xs" onclick={() => triggerDownload(selectedPaths)}>
							<DownloadIcon data-icon="inline-start" />
							{t('files.page.downloadZipBtn')}
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
							<LockIcon data-icon="inline-start" />
							{t('files.page.permissionBtn')}
						</Button>
						<Button
							variant="destructive"
							size="sm"
							class="h-7 gap-1 text-xs"
							disabled={bulkBusy}
							onclick={() => confirmDelete(selectedPaths)}
						>
							<Trash2Icon data-icon="inline-start" />
							{t('files.page.deleteBtn')}
						</Button>
					</div>
				</div>
			{/if}

			<!-- Active Upload Progress Banner (for drag-and-drop uploads too) -->
			{#if uploading}
				<div class="bg-primary/5 border-primary/25 flex flex-col gap-2 rounded-lg border px-3 py-2.5 text-xs">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<div class="flex min-w-0 items-center gap-2 font-medium">
							<Spinner class="size-3.5 shrink-0" />
							<span class="truncate">{t('files.page.uploadingTo1')}<code class="font-mono">{currentPath}</code>{t('files.page.uploadingTo2')}</span>
						</div>
						<div class="flex items-center gap-2">
							<span class="font-mono text-[11px]">
								{uploadProgress.percent}% ({formatBytes(uploadProgress.loaded)} / {formatBytes(uploadProgress.total)})
							</span>
							<Button variant="ghost" size="sm" class="fm-touch-btn h-7 px-2 text-xs" onclick={cancelUpload}>
								{t('files.page.cancelBtn')}
							</Button>
						</div>
					</div>
					<Progress value={uploadProgress.percent} class="h-1.5" />
				</div>
			{/if}

			<!-- Status Summary / Search Banner -->
			<div class="flex flex-wrap items-center justify-between gap-2">
				{#if isSearchMode}
					<CardTitle class="text-sm font-medium">
						{t('files.page.searchResultsHeading', { query: activeSearchQuery, count: visibleEntries.length })}
					</CardTitle>
				{:else if listing}
					<CardDescription class="text-xs">
						{t('files.page.statsSummary', { dirs: listing.total_dirs, files: listing.total_files, total: formatBytes(listing.total_size) })}<span class="fm-pointer-fine-only ml-1">{t('files.page.statsTipsDesktop')}</span><span class="fm-pointer-coarse-only ml-1">{t('files.page.statsTipsMobile')}</span>
					</CardDescription>
				{/if}
			</div>
		</CardHeader>

		<CardContent class="flex min-h-0 flex-1 flex-col pt-0 pb-4">
			{#if errorMsg}
				<Alert variant="destructive" class="mb-4 shrink-0">
					<CircleAlertIcon />
					<AlertTitle>{t('files.page.loadDirFailedTitle')}</AlertTitle>
					<AlertDescription class="flex flex-wrap items-center justify-between gap-2">
						<span>{errorMsg}</span>
						<Button variant="outline" size="sm" class="fm-touch-btn" onclick={() => navigateTo(roots[0]?.path ?? '')}>
							{t('files.page.returnInitialRoot')}
						</Button>
					</AlertDescription>
				</Alert>
			{/if}

			{#if loading && !listing}
				<div class="flex flex-col gap-2">
					{#each [1, 2, 3, 4, 5, 6] as row (row)}
						<Skeleton class="h-12 w-full sm:h-10" />
					{/each}
				</div>
			{:else if visibleEntries.length === 0}
				<Empty.Root class="flex min-h-[240px] flex-1 flex-col justify-center border md:min-h-0">
					<Empty.Header>
						<Empty.Media variant="icon">
							<FolderOpenIcon class="size-5" />
						</Empty.Media>
						<Empty.Title>{isSearchMode ? t('files.page.emptySearchTitle') : t('files.page.emptyDirTitle')}</Empty.Title>
						<Empty.Description>
							{#if isSearchMode}
								{t('files.page.emptySearchDesc', { query: activeSearchQuery })}
							{:else}
								{t('files.page.emptyDirDesc')}
							{/if}
						</Empty.Description>
					</Empty.Header>
					{#if !isSearchMode}
						<Empty.Content class="flex-row flex-wrap justify-center gap-2">
							<Button variant="outline" size="sm" class="fm-touch-btn" onclick={openNewFileDialog}>
								<FilePlusIcon data-icon="inline-start" />
								{t('files.page.createNewFileBtn')}
							</Button>
							<Button size="sm" class="fm-touch-btn" onclick={() => openUploadDialog('device')}>
								<UploadIcon data-icon="inline-start" />
								{t('files.page.uploadFilesBtn')}
							</Button>
						</Empty.Content>
					{/if}
				</Empty.Root>
			{:else}
				<div
					bind:this={listContainerEl}
					class="fm-list-container min-h-[240px] flex-1 md:min-h-0 md:overflow-hidden"
					style:--fm-row-py={`${rowPadY}px`}
				>
					<!-- Desktop Table View (@container >= 44rem) -->
					<div class="fm-desktop-view md:h-full">
						<ResponsiveTableWrapper label={t('files.page.tableAria')} stackOnMobile={false} class="md:max-h-full">
							<Table>
								<TableHeader>
									<TableRow>
										<TableHead scope="col" class="w-10">
											<Checkbox
												checked={allVisibleSelected}
												indeterminate={someVisibleSelected}
												onCheckedChange={(v) => toggleSelectAll(Boolean(v))}
												aria-label={t('files.page.selectAllAria')}
											/>
										</TableHead>
										<TableHead scope="col">
											<button
												type="button"
												class="hover:text-foreground inline-flex items-center gap-1 font-medium"
												onclick={() => toggleSort('name')}
											>
												{t('files.page.colName')}
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
										<TableHead scope="col" class="w-28">
											<button
												type="button"
												class="hover:text-foreground inline-flex items-center gap-1 font-medium"
												onclick={() => toggleSort('size')}
											>
												{t('files.page.colSize')}
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
										<TableHead scope="col" class="w-36">{t('files.page.colPermission')}</TableHead>
										<TableHead scope="col" class="w-36">{t('files.page.colOwner')}</TableHead>
										<TableHead scope="col" class="w-40">
											<button
												type="button"
												class="hover:text-foreground inline-flex items-center gap-1 font-medium"
												onclick={() => toggleSort('mod_time')}
											>
												{t('files.page.colModified')}
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
										<TableHead scope="col" class="text-right">{t('files.page.colActions')}</TableHead>
									</TableRow>
								</TableHeader>
								<TableBody>
									{#each pagedEntries as entry (entry.path)}
										{@const checked = selectedSet.has(entry.path)}
										<TableRow
											class={cn(
												checked && 'bg-muted/40',
												contextMenu?.entry?.path === entry.path && 'ring-primary/50 ring-1'
											)}
											oncontextmenu={(e) => openContextMenu(e, entry)}
										>
											<TableCell data-label={t('files.page.dataSelect')}>
												<Checkbox
													{checked}
													onCheckedChange={(v) => toggleSelect(entry.path, Boolean(v))}
													aria-label={t('files.page.selectEntryAria', { name: entry.name })}
												/>
											</TableCell>
											<TableCell data-label={t('files.page.dataName')}>
												<div class="flex min-w-0 items-center gap-2.5">
													{@render entryIcon(entry)}

													<div class="min-w-0 flex-1">
														<button
															type="button"
															class="hover:text-primary block max-w-[20rem] text-left font-medium hover:underline lg:max-w-[28rem] xl:max-w-[36rem]"
															onclick={() => handleRowPrimaryClick(entry)}
														>
															{@render filenameDisplay(entry)}
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
											<TableCell data-label={t('files.page.dataSize')} class="font-mono text-xs whitespace-nowrap">
												{entry.is_dir ? '—' : formatBytes(entry.size)}
											</TableCell>
											<TableCell data-label={t('files.page.dataPermission')}>
												<button
													type="button"
													class="hover:bg-muted inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 font-mono text-xs transition-colors"
													title={t('files.page.changePermTitle')}
													onclick={() => openChmodDialog([entry])}
												>
													<span class="font-semibold">{entry.mode}</span>
													<span class="text-muted-foreground hidden sm:inline">({entry.perm_text})</span>
												</button>
											</TableCell>
											<TableCell data-label={t('files.page.dataOwner')} class="text-muted-foreground font-mono text-xs whitespace-nowrap">
												{entry.owner}:{entry.group}
											</TableCell>
											<TableCell data-label={t('files.page.dataModified')} class="text-muted-foreground text-xs whitespace-nowrap">
												{formatModTime(entry.mod_time)}
											</TableCell>
											<TableCell data-label={t('files.page.dataActions')} class="text-right">
												<div class="flex justify-end">
													<DropdownMenu.Root>
														<DropdownMenu.Trigger>
															{#snippet child({ props })}
																{@const triggerOnClick = props.onclick as ((e: MouseEvent) => void) | undefined}
																<Button
																	variant="outline"
																	size="xs"
																	{...props}
																	onclick={(e) => {
																		e.stopPropagation();
																		triggerOnClick?.(e);
																	}}
																>
																	{t('files.page.actionsBtn')}
																	<ChevronDownIcon data-icon="inline-end" />
																</Button>
															{/snippet}
														</DropdownMenu.Trigger>
														<DropdownMenu.Content align="end" class="w-48">
															<DropdownMenu.Group>
																<DropdownMenu.Label class="truncate text-xs text-muted-foreground">
																	{entry.name}
																</DropdownMenu.Label>
																<DropdownMenu.Separator />
																{#if entry.is_dir}
																	<DropdownMenu.Item onclick={() => void navigateTo(entry.path)}>
																		<FolderOpenIcon />
																		<span>{t('files.page.menuOpenFolder')}</span>
																	</DropdownMenu.Item>
																{:else if entry.editable || entry.is_image}
																	<DropdownMenu.Item onclick={() => openEditor(entry)}>
																		<EyeIcon />
																		<span>{entry.editable ? t('files.page.menuEditFile') : t('files.page.menuViewImage')}</span>
																	</DropdownMenu.Item>
																{/if}
																{#if entry.is_archive}
																	<DropdownMenu.Item onclick={() => openExtractDialog(entry)}>
																		<FileArchiveIcon />
																		<span>{t('files.page.menuExtractArchive')}</span>
																	</DropdownMenu.Item>
																{/if}
																<DropdownMenu.Item onclick={() => triggerDownload([entry.path])}>
																	<DownloadIcon />
																	<span>{entry.is_dir ? t('files.page.menuDownloadZip') : t('files.page.menuDownloadFile')}</span>
																</DropdownMenu.Item>
																<DropdownMenu.Item
																	onclick={() => openTerminalAt(entry.is_dir ? entry.path : currentPath)}
																>
																	<SquareTerminalIcon />
																	<span>{t('files.page.openTerminal')}</span>
																</DropdownMenu.Item>
																<DropdownMenu.Separator />
																<DropdownMenu.Item onclick={() => openRenameDialog(entry)}>
																	<PencilIcon />
																	<span>{t('files.page.menuRename')}</span>
																</DropdownMenu.Item>
																<DropdownMenu.Item onclick={() => openChmodDialog([entry])}>
																	<LockIcon />
																	<span>{t('files.page.menuPermChmod')}</span>
																</DropdownMenu.Item>
																<DropdownMenu.Separator />
																<DropdownMenu.Item
																	variant="destructive"
																	onclick={() => confirmDelete([entry.path])}
																>
																	<Trash2Icon />
																	<span>{t('files.page.menuDelete')}</span>
																</DropdownMenu.Item>
															</DropdownMenu.Group>
														</DropdownMenu.Content>
													</DropdownMenu.Root>
												</div>
											</TableCell>
										</TableRow>
									{/each}
								</TableBody>
							</Table>
						</ResponsiveTableWrapper>
					</div>

					<!-- Mobile Ergonomic Card List View (@container < 44rem) -->
					<div class="fm-mobile-view">
						<div class="flex flex-col gap-3">
							<!-- Mobile Control Bar: Select All + Sort -->
							<div class="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-muted/30 px-3 py-2">
							<label
								for="fm-mobile-select-all"
								class="fm-touch-check cursor-pointer gap-2 px-1 text-xs font-medium text-foreground select-none"
							>
								<Checkbox
									id="fm-mobile-select-all"
									checked={allVisibleSelected}
									indeterminate={someVisibleSelected}
									onCheckedChange={(v) => toggleSelectAll(Boolean(v))}
									aria-label={t('files.page.selectAllAria')}
								/>
								<span>
									{allVisibleSelected ? t('files.page.mobileDeselectAll') : t('files.page.mobileSelectAll')} ({pagedEntries.length})
								</span>
							</label>

							<div class="flex items-center gap-1.5">
								<NativeSelect.Root
									bind:value={sortBy}
									size="sm"
									aria-label={t('files.page.sortByAria')}
									class="text-xs"
								>
									<NativeSelect.Option value="name">{t('files.page.sortName')}</NativeSelect.Option>
									<NativeSelect.Option value="size">{t('files.page.sortSize')}</NativeSelect.Option>
									<NativeSelect.Option value="mod_time">{t('files.page.sortModified')}</NativeSelect.Option>
								</NativeSelect.Root>
								<Button
									type="button"
									variant="outline"
									size="icon-sm"
									class="fm-touch-btn shrink-0"
									aria-label={sortDir === 'asc'
										? t('files.page.sortAscAria')
										: t('files.page.sortDescAria')}
									onclick={() => (sortDir = sortDir === 'asc' ? 'desc' : 'asc')}
								>
									{#if sortDir === 'asc'}
										<ArrowUpIcon />
									{:else}
										<ArrowDownIcon />
									{/if}
								</Button>
							</div>
						</div>

						<!-- Compact Mobile File List -->
						<ul class="divide-y divide-border overflow-hidden rounded-lg border bg-card">
							{#each pagedEntries as entry, idx (entry.path)}
								{@const checked = selectedSet.has(entry.path)}
								{@const checkId = `fm-mobile-row-${idx}`}
								<li
									class={cn(
										'flex items-center gap-1.5 px-2 py-1.5 transition-colors',
										checked && 'bg-primary/5'
									)}
									oncontextmenu={(e) => openContextMenu(e, entry)}
								>
									<label
										for={checkId}
										class="fm-touch-check flex shrink-0 cursor-pointer items-center justify-center rounded-md px-2"
									>
										<Checkbox
											id={checkId}
											{checked}
											onCheckedChange={(v) => toggleSelect(entry.path, Boolean(v))}
											aria-label={t('files.page.selectEntryAria', { name: entry.name })}
										/>
									</label>

									<button
										type="button"
										class="flex min-h-12 min-w-0 flex-1 items-center gap-2.5 rounded-md px-1.5 py-2 text-left transition-colors active:bg-muted/70"
										onclick={() => handleRowPrimaryClick(entry)}
									>
										{@render entryIcon(entry, 'size-5')}

										<div class="flex min-w-0 flex-1 flex-col gap-0.5">
											<div class="text-sm font-medium text-foreground">
												{@render filenameDisplay(entry)}
											</div>
											<div class="text-muted-foreground flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs">
												<span>{entry.is_dir ? t('files.page.folderType') : formatBytes(entry.size)}</span>
												<span aria-hidden="true">•</span>
												<span class="font-mono">{entry.mode}</span>
												<span aria-hidden="true">•</span>
												<span>{formatCompactModTime(entry.mod_time)}</span>
											</div>
											{#if entry.is_symlink && entry.symlink_target}
												<span class="text-muted-foreground truncate font-mono text-[11px]">
													→ {entry.symlink_target}
												</span>
											{/if}
											{#if isSearchMode}
												<span class="text-muted-foreground truncate font-mono text-[11px]">
													{getParentPath(entry.path)}
												</span>
											{/if}
										</div>
									</button>

									<Button
										type="button"
										variant="ghost"
										size="icon"
										class="fm-touch-btn text-muted-foreground shrink-0"
										aria-label={t('files.page.entryActionsAria', { name: entry.name })}
										onclick={() => openMobileEntrySheet(entry)}
									>
										<EllipsisVerticalIcon />
									</Button>
								</li>
							{/each}
						</ul>
						</div>
					</div>
				</div>
			{/if}
		</CardContent>
		{#if !loading && visibleEntries.length > 0}
			<PaginationBar
				bind:page={currentPage}
				totalItems={visibleEntries.length}
				{pageSize}
				itemLabel={t('files.page.paginationItemLabel')}
				class="shrink-0 rounded-b-xl"
			/>
		{/if}
	</Card>
</div>

<!-- Editor & Image Preview Modal -->
<FileEditorModal bind:open={editorOpen} entry={editorEntry} onSaved={refreshCurrent} />

<!-- New File Dialog -->
<Dialog bind:open={newFileOpen}>
	<DialogContent class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-md">
		<DialogHeader>
			<DialogTitle>{t('files.page.dialogs.newFileTitle')}</DialogTitle>
			<DialogDescription class="font-mono text-xs break-all">{t('files.page.dialogs.locationPrefix', { path: currentPath })}</DialogDescription>
		</DialogHeader>
		<form onsubmit={submitNewFile} class="flex flex-col gap-4">
			<Field>
				<FieldLabel for="fm-new-file-name">{t('files.page.dialogs.newFileLabel')}</FieldLabel>
				<Input
					id="fm-new-file-name"
					bind:value={newFileName}
					class="h-10 text-base sm:h-8 sm:text-sm"
					placeholder={t('files.page.dialogs.newFilePlaceholder')}
					autocapitalize="none"
					autocorrect="off"
					spellcheck={false}
					required
					autofocus
				/>
			</Field>
			<label for="fm-new-file-open-after" class="fm-touch-check cursor-pointer gap-2 text-sm">
				<Checkbox id="fm-new-file-open-after" bind:checked={newFileOpenAfter} />
				<span>{t('files.page.dialogs.openAfterLabel')}</span>
			</label>
			<DialogFooter>
				<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (newFileOpen = false)} disabled={newFileBusy}>
					{t('files.page.cancelBtn')}
				</Button>
				<Button type="submit" class="min-h-10 sm:min-h-0" disabled={newFileBusy || !newFileName.trim()}>
					{#if newFileBusy}<Spinner data-icon="inline-start" />{/if}
					{t('files.page.dialogs.createFileBtn')}
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- New Folder Dialog -->
<Dialog bind:open={newFolderOpen}>
	<DialogContent class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-md">
		<DialogHeader>
			<DialogTitle>{t('files.page.dialogs.newFolderTitle')}</DialogTitle>
			<DialogDescription class="font-mono text-xs break-all">{t('files.page.dialogs.locationPrefix', { path: currentPath })}</DialogDescription>
		</DialogHeader>
		<form onsubmit={submitNewFolder} class="flex flex-col gap-4">
			<Field>
				<FieldLabel for="fm-new-folder-name">{t('files.page.dialogs.newFolderLabel')}</FieldLabel>
				<Input
					id="fm-new-folder-name"
					bind:value={newFolderName}
					class="h-10 text-base sm:h-8 sm:text-sm"
					placeholder={t('files.page.dialogs.newFolderPlaceholder')}
					autocapitalize="none"
					autocorrect="off"
					spellcheck={false}
					required
					autofocus
				/>
			</Field>
			<DialogFooter>
				<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (newFolderOpen = false)} disabled={newFolderBusy}>
					{t('files.page.cancelBtn')}
				</Button>
				<Button type="submit" class="min-h-10 sm:min-h-0" disabled={newFolderBusy || !newFolderName.trim()}>
					{#if newFolderBusy}<Spinner data-icon="inline-start" />{/if}
					{t('files.page.dialogs.createFolderBtn')}
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- Upload & Remote URL Download Dialog -->
<Dialog bind:open={uploadOpen}>
	<DialogContent class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-xl">
		<DialogHeader>
			<DialogTitle>{t('files.page.dialogs.uploadTitle')}</DialogTitle>
			<DialogDescription class="font-mono text-xs break-all">
				{t('files.page.dialogs.destPrefix', { path: currentPath })}
			</DialogDescription>
		</DialogHeader>

		<Tabs.Root bind:value={uploadTab} class="flex flex-col gap-4">
			<Tabs.List class="grid h-auto w-full grid-cols-2">
				<Tabs.Trigger value="device" class="min-h-9 py-1.5 text-xs sm:text-sm" disabled={fetchingUrl}>{t('files.page.dialogs.tabDevice')}</Tabs.Trigger>
				<Tabs.Trigger value="url" class="min-h-9 py-1.5 text-xs sm:text-sm" disabled={uploading}>{t('files.page.dialogs.tabUrl')}</Tabs.Trigger>
			</Tabs.List>

			<Tabs.Content value="device" class="flex flex-col gap-4">
				<!-- Hidden file & folder pickers -->
				<input
					bind:this={uploadFileInputRef}
					type="file"
					multiple
					onchange={handleUploadFileChange}
					class="hidden"
				/>
				<input
					bind:this={uploadFolderInputRef}
					use:setDirectoryAttr
					type="file"
					multiple
					onchange={handleUploadFileChange}
					class="hidden"
				/>

				<!-- Interactive Dropzone inside modal -->
				<div
					role="region"
					aria-label={t('files.page.dialogs.dropzoneAria')}
					ondragover={handleUploadDropzoneDragOver}
					ondragleave={handleUploadDropzoneDragLeave}
					ondrop={handleUploadDropzoneDrop}
					class={cn(
						'flex flex-col items-center justify-center gap-3 rounded-lg border-2 border-dashed p-4 text-center transition-colors sm:p-5',
						uploadDropDragOver ? 'border-primary bg-primary/5' : 'border-muted-foreground/30 bg-muted/20'
					)}
				>
					<UploadIcon class="text-muted-foreground size-7" />
					<div class="flex flex-col gap-1">
						<p class="text-sm font-medium">{t('files.page.dialogs.dropzoneTitle')}</p>
						<p class="text-muted-foreground text-xs">
							{t('files.page.dialogs.dropzoneSubtitle')}
						</p>
					</div>
					<div class="flex w-full flex-wrap items-center justify-center gap-2 sm:w-auto">
						<Button
							type="button"
							variant="outline"
							size="sm"
							class="min-h-10 flex-1 sm:min-h-0 sm:flex-initial"
							disabled={uploading}
							onclick={() => uploadFileInputRef?.click()}
						>
							<FilePlusIcon data-icon="inline-start" />
							{t('files.page.dialogs.pickFile')}
						</Button>
						<Button
							type="button"
							variant="outline"
							size="sm"
							class="min-h-10 flex-1 sm:min-h-0 sm:flex-initial"
							disabled={uploading}
							onclick={() => uploadFolderInputRef?.click()}
						>
							<FolderPlusIcon data-icon="inline-start" />
							{t('files.page.dialogs.pickFolder')}
						</Button>
					</div>
				</div>

				{#if uploadQueue.length > 0}
					<div class="flex flex-col gap-2">
						<div class="flex items-center justify-between text-xs">
							<span class="font-medium">
								{t('files.page.dialogs.queueCount', { count: uploadQueue.length, size: formatBytes(uploadTotalBytes) })}
							</span>
							{#if !uploading}
								<button
									type="button"
									class="fm-touch-hit text-muted-foreground hover:text-foreground underline"
									onclick={() => (uploadQueue = [])}
								>
									{t('files.page.dialogs.clearQueue')}
								</button>
							{/if}
						</div>
						<div class="bg-muted/40 flex max-h-40 flex-col gap-1 overflow-y-auto overscroll-contain rounded-md border p-2 text-xs">
							{#each uploadQueue as item, idx (`${item.relativePath}-${idx}`)}
								<div class="flex min-h-8 items-center justify-between gap-2 rounded px-1 py-0.5 hover:bg-muted/60">
									<span class="min-w-0 flex-1 truncate font-mono">{item.relativePath}</span>
									<span class="text-muted-foreground shrink-0 font-mono">{formatBytes(item.file.size)}</span>
									{#if !uploading}
										<button
											type="button"
											class="fm-touch-hit text-muted-foreground hover:text-destructive inline-flex size-6 items-center justify-center rounded"
											title={t('files.page.dialogs.removeQueueTitle')}
											aria-label={t('files.page.dialogs.removeQueueAria', { name: item.relativePath })}
											onclick={() => removeUploadQueueIndex(idx)}
										>
											<XIcon class="size-3.5" />
										</button>
									{/if}
								</div>
							{/each}
						</div>
					</div>
				{/if}

				<label for="fm-upload-overwrite" class="fm-touch-check cursor-pointer gap-2 text-xs">
					<Checkbox id="fm-upload-overwrite" bind:checked={uploadOverwrite} disabled={uploading} />
					<span>{t('files.page.dialogs.overwriteCheck')}</span>
				</label>

				{#if uploading}
					<div class="flex flex-col gap-1.5 rounded-md border p-2.5 text-xs">
						<div class="flex items-center justify-between gap-2">
							<span class="font-medium">{t('files.page.dialogs.uploadingLabel')}</span>
							<span class="font-mono">
								{uploadProgress.percent}% ({formatBytes(uploadProgress.loaded)} / {formatBytes(uploadProgress.total)})
							</span>
						</div>
						<Progress value={uploadProgress.percent} class="h-1.5" />
					</div>
				{/if}

				<DialogFooter>
					{#if uploading}
						<Button type="button" variant="destructive" class="min-h-10 sm:min-h-0" onclick={cancelUpload}>
							{t('files.page.dialogs.cancelUpload')}
						</Button>
					{:else}
						<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (uploadOpen = false)}>
							{t('files.page.dialogs.closeBtn')}
						</Button>
						<Button
							type="button"
							class="min-h-10 sm:min-h-0"
							disabled={uploadQueue.length === 0}
							onclick={() => performUpload(uploadQueue)}
						>
							<UploadIcon data-icon="inline-start" />
							{t('files.page.dialogs.startUpload', { count: uploadQueue.length })}
						</Button>
					{/if}
				</DialogFooter>
			</Tabs.Content>

			<Tabs.Content value="url">
				<form onsubmit={submitFetchURL} class="flex flex-col gap-4">
					<Field>
						<FieldLabel for="fm-fetch-url">{t('files.page.dialogs.urlLabel')}</FieldLabel>
						<Input
							id="fm-fetch-url"
							type="url"
							bind:value={fetchUrl}
							placeholder="https://wordpress.org/latest.zip"
							class="h-10 font-mono text-base sm:h-8 sm:text-xs"
							required
						/>
						<FieldDescription>
							{t('files.page.dialogs.urlDesc')}
						</FieldDescription>
					</Field>

					<Field>
						<FieldLabel for="fm-fetch-filename">{t('files.page.dialogs.saveAsLabel')}</FieldLabel>
						<Input
							id="fm-fetch-filename"
							bind:value={fetchFilename}
							placeholder={t('files.page.dialogs.saveAsPlaceholder')}
							autocapitalize="none"
							autocorrect="off"
							spellcheck={false}
							class="h-10 font-mono text-base sm:h-8 sm:text-xs"
						/>
					</Field>

					<label for="fm-fetch-overwrite" class="fm-touch-check cursor-pointer gap-2 text-xs">
						<Checkbox id="fm-fetch-overwrite" bind:checked={fetchOverwrite} disabled={fetchingUrl} />
						<span>{t('files.page.dialogs.fetchOverwriteLabel')}</span>
					</label>

					<DialogFooter>
						<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (uploadOpen = false)} disabled={fetchingUrl}>
							{t('files.page.cancelBtn')}
						</Button>
						<Button type="submit" class="min-h-10 sm:min-h-0" disabled={fetchingUrl || !fetchUrl.trim()}>
							{#if fetchingUrl}
								<Spinner data-icon="inline-start" />
								{t('files.page.dialogs.downloadingBtn')}
							{:else}
								<GlobeIcon data-icon="inline-start" />
								{t('files.page.dialogs.downloadToFolderBtn')}
							{/if}
						</Button>
					</DialogFooter>
				</form>
			</Tabs.Content>
		</Tabs.Root>
	</DialogContent>
</Dialog>

<!-- Rename Dialog -->
<Dialog bind:open={renameOpen}>
	<DialogContent class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-md">
		<DialogHeader>
			<DialogTitle>{t('files.page.dialogs.renameTitle')}</DialogTitle>
			<DialogDescription class="font-mono text-xs break-all">{renameEntry?.path}</DialogDescription>
		</DialogHeader>
		<form onsubmit={submitRename} class="flex flex-col gap-4">
			<Field>
				<FieldLabel for="fm-rename-input">{t('files.page.dialogs.newNameLabel')}</FieldLabel>
				<Input
					id="fm-rename-input"
					bind:value={renameNewName}
					class="h-10 text-base sm:h-8 sm:text-sm"
					autocapitalize="none"
					autocorrect="off"
					spellcheck={false}
					required
					autofocus
				/>
			</Field>
			<DialogFooter>
				<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (renameOpen = false)} disabled={renameBusy}>
					{t('files.page.cancelBtn')}
				</Button>
				<Button type="submit" class="min-h-10 sm:min-h-0" disabled={renameBusy || !renameNewName.trim()}>
					{#if renameBusy}<Spinner data-icon="inline-start" />{/if}
					{t('files.page.dialogs.saveBtn')}
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- Interactive Chmod / Permission Dialog -->
<Dialog bind:open={chmodOpen}>
	<DialogContent class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-lg">
		<DialogHeader>
			<DialogTitle>{t('files.page.dialogs.chmodTitle')}</DialogTitle>
			<DialogDescription class="text-xs">
				{#if chmodTargets.length === 1}
					{t('files.page.dialogs.targetPrefix')} <code class="font-mono break-all">{chmodTargets[0]}</code>
				{:else}
					{t('files.page.dialogs.targetPrefix')} <strong>{t('files.page.dialogs.targetMulti', { count: chmodTargets.length })}</strong>
				{/if}
			</DialogDescription>
		</DialogHeader>

		<form onsubmit={submitChmod} class="flex flex-col gap-4">
			<div class="grid grid-cols-4 gap-2 rounded-lg border p-3 text-xs">
				<div class="text-muted-foreground font-semibold">{t('files.page.dialogs.colScope')}</div>
				<div class="text-center font-semibold">{t('files.page.dialogs.colRead')}</div>
				<div class="text-center font-semibold">{t('files.page.dialogs.colWrite')}</div>
				<div class="text-center font-semibold">{t('files.page.dialogs.colExec')}</div>

				{#each [
					{ key: 'owner', label: t('files.page.dialogs.scopeOwner'), shift: 6 },
					{ key: 'group', label: t('files.page.dialogs.scopeGroup'), shift: 3 },
					{ key: 'public', label: t('files.page.dialogs.scopePublic'), shift: 0 }
				] as group (group.key)}
					<div class="flex min-h-10 items-center font-medium sm:min-h-0">{group.label}</div>
					{#each [4, 2, 1] as bitVal (bitVal)}
						<div class="flex min-h-10 items-center justify-center sm:min-h-0">
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
					<FieldLabel for="fm-chmod-mode">{t('files.page.dialogs.octalLabel')}</FieldLabel>
					<Input
						id="fm-chmod-mode"
						bind:value={chmodMode}
						class="h-10 font-mono text-base sm:h-8 sm:text-sm"
						inputmode="numeric"
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
							class="fm-touch-btn h-9 font-mono text-xs sm:h-8"
							onclick={() => (chmodMode = preset)}
						>
							{preset}
						</Button>
					{/each}
				</div>
			</div>

			<div class="flex flex-col gap-1 text-sm">
				<label for="fm-chmod-recursive" class="fm-touch-check cursor-pointer gap-2">
					<Checkbox id="fm-chmod-recursive" bind:checked={chmodRecursive} />
					<span>{t('files.page.dialogs.recursiveLabel')}</span>
				</label>
				<label for="fm-chmod-webowner" class="fm-touch-check cursor-pointer gap-2">
					<Checkbox id="fm-chmod-webowner" bind:checked={chmodWebOwner} />
					<span>{t('files.page.dialogs.webOwner1')}<code class="font-mono text-xs">www-data / nginx</code>{t('files.page.dialogs.webOwner2')}</span>
				</label>
			</div>

			<DialogFooter>
				<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (chmodOpen = false)} disabled={chmodBusy}>
					{t('files.page.cancelBtn')}
				</Button>
				<Button type="submit" class="min-h-10 sm:min-h-0" disabled={chmodBusy}>
					{#if chmodBusy}<Spinner data-icon="inline-start" />{/if}
					{t('files.page.dialogs.applyPermBtn')}
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- Compress ZIP Dialog -->
<Dialog bind:open={compressOpen}>
	<DialogContent class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-md">
		<DialogHeader>
			<DialogTitle>{t('files.page.dialogs.compressTitle')}</DialogTitle>
			<DialogDescription class="text-xs">
				{t('files.page.dialogs.compressDesc1')}<strong>{t('files.page.dialogs.compressDesc2', { count: compressTargets.length })}</strong>{t('files.page.dialogs.compressDesc3')}<code>.zip</code>{t('files.page.dialogs.compressDesc4')}
			</DialogDescription>
		</DialogHeader>
		<form onsubmit={submitCompress} class="flex flex-col gap-4">
			<Field>
				<FieldLabel for="fm-compress-out">{t('files.page.dialogs.compressOutLabel')}</FieldLabel>
				<Input
					id="fm-compress-out"
					bind:value={compressOutput}
					class="h-10 font-mono text-base sm:h-8 sm:text-xs"
					autocapitalize="none"
					autocorrect="off"
					spellcheck={false}
					required
					autofocus
				/>
			</Field>
			<DialogFooter>
				<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (compressOpen = false)} disabled={compressBusy}>
					{t('files.page.cancelBtn')}
				</Button>
				<Button type="submit" class="min-h-10 sm:min-h-0" disabled={compressBusy || !compressOutput.trim()}>
					{#if compressBusy}<Spinner data-icon="inline-start" />{/if}
					{t('files.page.dialogs.compressBtn')}
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- Extract Archive Dialog -->
<Dialog bind:open={extractOpen}>
	<DialogContent class="max-h-[90dvh] overflow-y-auto overscroll-contain sm:max-w-md">
		<DialogHeader>
			<DialogTitle>{t('files.page.dialogs.extractTitle')}</DialogTitle>
			<DialogDescription class="font-mono text-xs break-all">
				{t('files.page.dialogs.extractArchiveLabel', { name: extractEntry?.name ?? '' })}
			</DialogDescription>
		</DialogHeader>
		<form onsubmit={submitExtract} class="flex flex-col gap-4">
			<Field>
				<FieldLabel for="fm-extract-dest">{t('files.page.dialogs.extractDestLabel')}</FieldLabel>
				<Input
					id="fm-extract-dest"
					bind:value={extractDest}
					class="h-10 font-mono text-base sm:h-8 sm:text-xs"
					autocapitalize="none"
					autocorrect="off"
					spellcheck={false}
					required
					autofocus
				/>
				<FieldDescription>{t('files.page.dialogs.extractDestDesc')}</FieldDescription>
			</Field>
			<DialogFooter>
				<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (extractOpen = false)} disabled={extractBusy}>
					{t('files.page.cancelBtn')}
				</Button>
				<Button type="submit" class="min-h-10 sm:min-h-0" disabled={extractBusy || !extractDest.trim()}>
					{#if extractBusy}<Spinner data-icon="inline-start" />{/if}
					{t('files.page.dialogs.extractBtn')}
				</Button>
			</DialogFooter>
		</form>
	</DialogContent>
</Dialog>

<!-- Mobile Sticky Bottom Action Bar (Selection & Clipboard) -->
{#if hasMobileBottomBar && !hasOpenModal()}
	<div
		data-fm-bottom-bar="true"
		class="fixed inset-x-0 bottom-0 z-30 border-t bg-background/95 pt-2.5 pb-[max(0.625rem,env(safe-area-inset-bottom,0px))] pl-[max(0.75rem,env(safe-area-inset-left,0px))] pr-[max(0.75rem,env(safe-area-inset-right,0px))] shadow-lg backdrop-blur supports-[backdrop-filter]:bg-background/85 md:hidden"
	>
		{#if selectedPaths.length > 0}
			<div class="mx-auto flex max-w-xl items-center justify-between gap-2">
				<div class="flex min-w-0 items-center gap-1.5">
					<Badge variant="secondary" class="shrink-0 font-mono">{selectedPaths.length}</Badge>
					<span class="truncate text-xs font-medium text-muted-foreground">{t('files.page.selectedShortLabel')}</span>
				</div>
				<div class="flex shrink-0 items-center gap-1.5">
					<Button
						type="button"
						variant="outline"
						size="sm"
						class="min-h-10 gap-1 text-xs"
						onclick={() => setClipboard('copy', selectedPaths)}
					>
						<CopyIcon data-icon="inline-start" />
						{t('files.page.copyBtn')}
					</Button>
					<Button
						type="button"
						variant="outline"
						size="sm"
						class="min-h-10 gap-1 text-xs"
						onclick={() => setClipboard('move', selectedPaths)}
					>
						<ScissorsIcon data-icon="inline-start" />
						{t('files.page.moveBtn')}
					</Button>
					<Button
						type="button"
						variant="destructive"
						size="icon"
						class="min-h-10 min-w-10"
						aria-label={t('files.page.deleteSelectedAria', { count: selectedPaths.length })}
						disabled={bulkBusy}
						onclick={() => confirmDelete(selectedPaths)}
					>
						<Trash2Icon />
					</Button>
					<Button
						type="button"
						variant="outline"
						size="icon"
						class="min-h-10 min-w-10"
						aria-label={t('files.page.moreBatchActionsAria')}
						onclick={() => (mobileBulkSheetOpen = true)}
					>
						<EllipsisIcon />
					</Button>
				</div>
			</div>
		{:else if clipboard}
			<div class="mx-auto flex max-w-xl items-center justify-between gap-2">
				<div class="flex min-w-0 items-center gap-2 text-xs">
					{#if clipboard.mode === 'copy'}
						<CopyIcon class="text-primary size-4 shrink-0" />
					{:else}
						<ScissorsIcon class="text-primary size-4 shrink-0" />
					{/if}
					<div class="flex min-w-0 flex-col">
						<span class="truncate font-medium">
							{t('files.page.clipboardCountItems', { count: clipboard.paths.length })} {clipboard.mode === 'copy' ? t('files.page.clipboardBannerCopy') : t('files.page.clipboardBannerMove')}
						</span>
						<span class="truncate font-mono text-[11px] text-muted-foreground">{clipboard.sourceDir}</span>
					</div>
				</div>
				<div class="flex shrink-0 items-center gap-1.5">
					<Button type="button" variant="ghost" size="sm" class="min-h-10 text-xs" onclick={() => (clipboard = null)}>
						{t('files.page.cancelBtn')}
					</Button>
					<Button
						type="button"
						size="sm"
						class="min-h-10 gap-1 text-xs"
						disabled={bulkBusy || isSearchMode || !currentPath}
						onclick={pasteClipboard}
					>
						{#if bulkBusy}
							<Spinner data-icon="inline-start" />
						{:else}
							<ClipboardPasteIcon data-icon="inline-start" />
						{/if}
						{t('files.page.pasteShortBtn')}
					</Button>
				</div>
			</div>
		{/if}
	</div>
{/if}

<!-- Mobile Bottom Sheet: Folder Actions -->
<Sheet.Root bind:open={mobileFolderSheetOpen}>
	<Sheet.Content side="bottom" class="rounded-t-2xl pb-[max(1rem,env(safe-area-inset-bottom,0px))]">
		<Sheet.Header class="pr-8">
			<Sheet.Title>{t('files.page.sheets.folderActionsTitle')}</Sheet.Title>
			<Sheet.Description class="truncate font-mono text-xs">{currentPath || '-'}</Sheet.Description>
		</Sheet.Header>
		<div class="flex max-h-[65dvh] flex-col gap-1.5 overflow-y-auto overscroll-contain px-4 pb-2">
			<Button
				type="button"
				variant="outline"
				class="min-h-11 w-full justify-start gap-3"
				disabled={isSearchMode}
				onclick={() => {
					mobileFolderSheetOpen = false;
					openNewFileDialog();
				}}
			>
				<FilePlusIcon data-icon="inline-start" />
				{t('files.page.newFile')}
			</Button>
			<Button
				type="button"
				variant="outline"
				class="min-h-11 w-full justify-start gap-3"
				disabled={isSearchMode}
				onclick={() => {
					mobileFolderSheetOpen = false;
					openNewFolderDialog();
				}}
			>
				<FolderPlusIcon data-icon="inline-start" />
				{t('files.page.newFolder')}
			</Button>
			<Button
				type="button"
				variant="outline"
				class="min-h-11 w-full justify-start gap-3"
				disabled={isSearchMode}
				onclick={() => {
					mobileFolderSheetOpen = false;
					openUploadDialog('device');
				}}
			>
				<UploadIcon data-icon="inline-start" />
				{t('files.page.sheets.uploadFileFolder')}
			</Button>
			<Button
				type="button"
				variant="outline"
				class="min-h-11 w-full justify-start gap-3"
				disabled={isSearchMode}
				onclick={() => {
					mobileFolderSheetOpen = false;
					openUploadDialog('url');
				}}
			>
				<GlobeIcon data-icon="inline-start" />
				{t('files.page.sheets.downloadFromUrl')}
			</Button>

			{#if clipboard}
				<Button
					type="button"
					class="min-h-11 w-full justify-start gap-3"
					disabled={bulkBusy || isSearchMode}
					onclick={() => {
						mobileFolderSheetOpen = false;
						void pasteClipboard();
					}}
				>
					<ClipboardPasteIcon data-icon="inline-start" />
					{t('files.page.sheets.pasteItems', { count: clipboard.paths.length })}
				</Button>
			{/if}

			<Separator class="my-1" />

			<Button
				type="button"
				variant="ghost"
				class="min-h-11 w-full justify-start gap-3"
				onclick={() => {
					mobileFolderSheetOpen = false;
					triggerDownload([currentPath]);
				}}
			>
				<DownloadIcon data-icon="inline-start" />
				{t('files.page.sheets.downloadFolderZip')}
			</Button>
			<Button
				type="button"
				variant="ghost"
				class="min-h-11 w-full justify-start gap-3"
				onclick={() => {
					mobileFolderSheetOpen = false;
					openTerminalAt(currentPath);
				}}
			>
				<SquareTerminalIcon data-icon="inline-start" />
				{t('files.page.sheets.openTerminalHere')}
			</Button>
			<Button
				type="button"
				variant="ghost"
				class="min-h-11 w-full justify-start gap-3"
				onclick={() => {
					mobileFolderSheetOpen = false;
					void toggleShowHidden();
				}}
			>
				{#if showHidden}
					<EyeOffIcon data-icon="inline-start" />
					{t('files.page.sheets.hideDotFiles')}
				{:else}
					<EyeIcon data-icon="inline-start" />
					{t('files.page.sheets.showDotFiles')}
				{/if}
			</Button>
			<Button
				type="button"
				variant="ghost"
				class="min-h-11 w-full justify-start gap-3"
				onclick={() => {
					mobileFolderSheetOpen = false;
					void refreshCurrent();
				}}
			>
				<RefreshCwIcon data-icon="inline-start" />
				{t('files.page.sheets.refreshDir')}
			</Button>
		</div>
	</Sheet.Content>
</Sheet.Root>

<!-- Mobile Bottom Sheet: Single File/Folder Entry Actions -->
<Sheet.Root bind:open={mobileEntrySheetOpen}>
	<Sheet.Content side="bottom" class="rounded-t-2xl pb-[max(1rem,env(safe-area-inset-bottom,0px))]">
		{#if mobileEntrySheetEntry}
			{@const entry = mobileEntrySheetEntry}
			{@const terminalDir = entry.is_dir ? entry.path : getParentPath(entry.path)}
			<Sheet.Header class="pr-8">
				<Sheet.Title class="flex min-w-0 items-center gap-2">
					{@render entryIcon(entry, 'size-5')}
					<span class="truncate font-mono text-sm">{entry.name}</span>
				</Sheet.Title>
				<Sheet.Description class="font-mono text-xs break-all">
					{entry.is_dir ? t('files.page.folderType') : formatBytes(entry.size)} • {entry.mode} ({entry.perm_text}) • {entry.owner}:{entry.group}
				</Sheet.Description>
			</Sheet.Header>

			<div class="flex max-h-[65dvh] flex-col gap-1.5 overflow-y-auto overscroll-contain px-4 pb-2">
				{#if entry.is_dir}
					<Button
						type="button"
						variant="outline"
						class="min-h-11 w-full justify-start gap-3"
						onclick={() => {
							mobileEntrySheetOpen = false;
							void navigateTo(entry.path);
						}}
					>
						<FolderOpenIcon data-icon="inline-start" />
						{t('files.page.menuOpenFolder')}
					</Button>
				{:else if entry.editable || entry.is_image}
					<Button
						type="button"
						variant="outline"
						class="min-h-11 w-full justify-start gap-3"
						onclick={() => {
							mobileEntrySheetOpen = false;
							openEditor(entry);
						}}
					>
						<EyeIcon data-icon="inline-start" />
						{entry.editable ? t('files.page.menuEditFile') : t('files.page.menuViewImage')}
					</Button>
				{/if}

				{#if isSearchMode}
					<Button
						type="button"
						variant="ghost"
						class="min-h-11 w-full justify-start gap-3"
						onclick={() => {
							mobileEntrySheetOpen = false;
							void navigateTo(getParentPath(entry.path));
						}}
					>
						<FolderOpenIcon data-icon="inline-start" />
						{t('files.page.sheets.openParentFolder')}
					</Button>
				{/if}

				{#if entry.is_archive}
					<Button
						type="button"
						variant="outline"
						class="min-h-11 w-full justify-start gap-3"
						onclick={() => {
							mobileEntrySheetOpen = false;
							openExtractDialog(entry);
						}}
					>
						<FileArchiveIcon data-icon="inline-start" />
						{t('files.page.menuExtractArchive')}
					</Button>
				{/if}

				<Button
					type="button"
					variant="ghost"
					class="min-h-11 w-full justify-start gap-3"
					onclick={() => {
						mobileEntrySheetOpen = false;
						openTerminalAt(terminalDir);
					}}
				>
					<SquareTerminalIcon data-icon="inline-start" />
					{entry.is_dir ? t('files.page.sheets.openTerminalHere') : t('files.page.sheets.openTerminalParent')}
				</Button>
				<Button
					type="button"
					variant="ghost"
					class="min-h-11 w-full justify-start gap-3"
					onclick={() => {
						mobileEntrySheetOpen = false;
						triggerDownload([entry.path]);
					}}
				>
					<DownloadIcon data-icon="inline-start" />
					{entry.is_dir ? t('files.page.menuDownloadZip') : t('files.page.menuDownloadFile')}
				</Button>

				<Separator class="my-1" />

				<Button
					type="button"
					variant="ghost"
					class="min-h-11 w-full justify-start gap-3"
					onclick={() => {
						mobileEntrySheetOpen = false;
						setClipboard('copy', [entry.path]);
					}}
				>
					<CopyIcon data-icon="inline-start" />
					{t('files.page.sheets.copyFull')}
				</Button>
				<Button
					type="button"
					variant="ghost"
					class="min-h-11 w-full justify-start gap-3"
					onclick={() => {
						mobileEntrySheetOpen = false;
						setClipboard('move', [entry.path]);
					}}
				>
					<ScissorsIcon data-icon="inline-start" />
					{t('files.page.sheets.moveFull')}
				</Button>
				<Button
					type="button"
					variant="ghost"
					class="min-h-11 w-full justify-start gap-3"
					onclick={() => {
						mobileEntrySheetOpen = false;
						openRenameDialog(entry);
					}}
				>
					<PencilIcon data-icon="inline-start" />
					{t('files.page.menuRename')}
				</Button>
				<Button
					type="button"
					variant="ghost"
					class="min-h-11 w-full justify-start gap-3"
					onclick={() => {
						mobileEntrySheetOpen = false;
						openChmodDialog([entry]);
					}}
				>
					<LockIcon data-icon="inline-start" />
					{t('files.page.menuPermChmod')}
				</Button>
				<Button
					type="button"
					variant="ghost"
					class="min-h-11 w-full justify-start gap-3"
					onclick={() => {
						mobileEntrySheetOpen = false;
						openCompressDialog([entry.path]);
					}}
				>
					<FileArchiveIcon data-icon="inline-start" />
					{t('files.page.sheets.compressToZip')}
				</Button>

				<Separator class="my-1" />

				<Button
					type="button"
					variant="destructive"
					class="min-h-11 w-full justify-start gap-3"
					onclick={() => {
						mobileEntrySheetOpen = false;
						void confirmDelete([entry.path]);
					}}
				>
					<Trash2Icon data-icon="inline-start" />
					{t('files.page.sheets.deletePermanent')}
				</Button>
			</div>
		{:else}
			<Sheet.Header>
				<Sheet.Title>{t('files.page.sheets.itemActionsTitle')}</Sheet.Title>
			</Sheet.Header>
		{/if}
	</Sheet.Content>
</Sheet.Root>

<!-- Mobile Bottom Sheet: Bulk Selection Overflow Actions -->
<Sheet.Root bind:open={mobileBulkSheetOpen}>
	<Sheet.Content side="bottom" class="rounded-t-2xl pb-[max(1rem,env(safe-area-inset-bottom,0px))]">
		<Sheet.Header class="pr-8">
			<Sheet.Title>{t('files.page.sheets.bulkTitle', { count: selectedPaths.length })}</Sheet.Title>
			<Sheet.Description class="text-xs">{t('files.page.sheets.bulkDesc')}</Sheet.Description>
		</Sheet.Header>
		<div class="flex flex-col gap-1.5 px-4 pb-2">
			<Button
				type="button"
				variant="outline"
				class="min-h-11 w-full justify-start gap-3"
				onclick={() => {
					mobileBulkSheetOpen = false;
					openCompressDialog(selectedPaths);
				}}
			>
				<FileArchiveIcon data-icon="inline-start" />
				{t('files.page.sheets.compressToZip')}
			</Button>
			<Button
				type="button"
				variant="outline"
				class="min-h-11 w-full justify-start gap-3"
				onclick={() => {
					mobileBulkSheetOpen = false;
					triggerDownload(selectedPaths);
				}}
			>
				<DownloadIcon data-icon="inline-start" />
				{t('files.page.menuDownloadZip')}
			</Button>
			<Button
				type="button"
				variant="outline"
				class="min-h-11 w-full justify-start gap-3"
				onclick={() => {
					mobileBulkSheetOpen = false;
					openChmodDialog(
						visibleEntries.filter((e) => selectedSet.has(e.path)),
						selectedPaths
					);
				}}
			>
				<LockIcon data-icon="inline-start" />
				{t('files.page.sheets.changePermChmod')}
			</Button>
			<Separator class="my-1" />
			<Button
				type="button"
				variant="ghost"
				class="min-h-11 w-full justify-start gap-3"
				onclick={() => {
					mobileBulkSheetOpen = false;
					selectedPaths = [];
				}}
			>
				<XIcon data-icon="inline-start" />
				{t('files.page.sheets.clearSelectionBtn')}
			</Button>
		</div>
	</Sheet.Content>
</Sheet.Root>

<svelte:window
	onclick={closeContextMenu}
	onkeydown={(e) => {
		if (e.key === 'Escape') closeContextMenu();
	}}
/>

<!-- Floating Right-Click Context Menu (Desktop) -->
{#if contextMenu}
	{@const targetEntry = contextMenu.entry}
	<div
		role="menu"
		tabindex="-1"
		aria-label={t('files.page.contextMenu.ariaLabel')}
		style={`left: ${contextMenu.x}px; top: ${contextMenu.y}px;`}
		class="bg-popover text-popover-foreground ring-foreground/10 fixed z-50 w-56 rounded-lg border p-1 shadow-lg ring-1"
		onclick={(e) => e.stopPropagation()}
		onkeydown={(e) => {
			if (e.key === 'Escape') closeContextMenu();
		}}
		oncontextmenu={(e) => e.preventDefault()}
	>
		<div class="text-muted-foreground border-b px-2 py-1.5 text-[11px] font-medium">
			<span class="block truncate font-mono">
				{targetEntry ? targetEntry.name : currentPath}
			</span>
		</div>

		{#if targetEntry}
			{#if targetEntry.is_dir}
				<button
					type="button"
					role="menuitem"
					class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
					onclick={() => runContextMenuAction((entry) => void navigateTo(entry.path))}
				>
					<FolderOpenIcon class="size-3.5 text-amber-500" />
					{t('files.page.menuOpenFolder')}
				</button>
			{:else if targetEntry.editable || targetEntry.is_image}
				<button
					type="button"
					role="menuitem"
					class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
					onclick={() => runContextMenuAction((entry) => openEditor(entry))}
				>
					<EyeIcon class="size-3.5 text-sky-500" />
					{targetEntry.editable ? t('files.page.menuEditFile') : t('files.page.menuViewImage')}
				</button>
			{/if}

			{#if targetEntry.is_archive}
				<button
					type="button"
					role="menuitem"
					class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
					onclick={() => runContextMenuAction((entry) => openExtractDialog(entry))}
				>
					<FileArchiveIcon class="size-3.5 text-orange-500" />
					{t('files.page.menuExtractArchive')}
				</button>
			{/if}

			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground text-primary flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs font-medium"
				onclick={() =>
					runContextMenuAction((entry) =>
						openTerminalAt(entry.is_dir ? entry.path : getParentPath(entry.path))
					)}
			>
				<SquareTerminalIcon class="size-3.5" />
				{targetEntry.is_dir ? t('files.page.sheets.openTerminalHere') : t('files.page.sheets.openTerminalParent')}
			</button>

			<div class="bg-border my-1 h-px"></div>

			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				onclick={() => runContextMenuAction((entry) => triggerDownload([entry.path]))}
			>
				<DownloadIcon class="size-3.5" />
				{targetEntry.is_dir ? t('files.page.contextMenu.downloadFolderShort') : t('files.page.contextMenu.downloadFileShort')}
			</button>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				onclick={() => runContextMenuAction((entry) => setClipboard('copy', [entry.path]))}
			>
				<CopyIcon class="size-3.5" />
				{t('files.page.sheets.copyFull')}
			</button>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				onclick={() => runContextMenuAction((entry) => setClipboard('move', [entry.path]))}
			>
				<ScissorsIcon class="size-3.5" />
				{t('files.page.sheets.moveFull')}
			</button>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				onclick={() => runContextMenuAction((entry) => openRenameDialog(entry))}
			>
				<PencilIcon class="size-3.5" />
				{t('files.page.menuRename')}
			</button>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				onclick={() => runContextMenuAction((entry) => openChmodDialog([entry]))}
			>
				<LockIcon class="size-3.5" />
				{t('files.page.contextMenu.permShort')}
			</button>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				onclick={() => runContextMenuAction((entry) => openCompressDialog([entry.path]))}
			>
				<FileArchiveIcon class="size-3.5" />
				{t('files.page.sheets.compressToZip')}
			</button>

			<div class="bg-border my-1 h-px"></div>

			<button
				type="button"
				role="menuitem"
				class="hover:bg-destructive/10 text-destructive flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				onclick={() => runContextMenuAction((entry) => void confirmDelete([entry.path]))}
			>
				<Trash2Icon class="size-3.5" />
				{t('files.page.menuDelete')}
			</button>
		{:else}
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground text-primary flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs font-medium"
				onclick={() => openTerminalAt(currentPath)}
			>
				<SquareTerminalIcon class="size-3.5" />
				{t('files.page.sheets.openTerminalHere')}
			</button>
			{#if clipboard}
				<button
					type="button"
					role="menuitem"
					class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
					disabled={bulkBusy || isSearchMode}
					onclick={() => {
						closeContextMenu();
						void pasteClipboard();
					}}
				>
					<ClipboardPasteIcon class="size-3.5" />
					{t('files.page.sheets.pasteItems', { count: clipboard.paths.length })}
				</button>
			{/if}
			<div class="bg-border my-1 h-px"></div>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				disabled={isSearchMode}
				onclick={() => {
					closeContextMenu();
					openNewFileDialog();
				}}
			>
				<FilePlusIcon class="size-3.5" />
				{t('files.page.newFile')}
			</button>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				disabled={isSearchMode}
				onclick={() => {
					closeContextMenu();
					openNewFolderDialog();
				}}
			>
				<FolderPlusIcon class="size-3.5" />
				{t('files.page.newFolder')}
			</button>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				disabled={isSearchMode}
				onclick={() => {
					closeContextMenu();
					openUploadDialog('device');
				}}
			>
				<UploadIcon class="size-3.5" />
				{t('files.page.sheets.uploadFileFolder')}
			</button>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				disabled={isSearchMode}
				onclick={() => {
					closeContextMenu();
					openUploadDialog('url');
				}}
			>
				<GlobeIcon class="size-3.5" />
				{t('files.page.sheets.downloadFromUrl')}
			</button>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				onclick={() => {
					closeContextMenu();
					triggerDownload([currentPath]);
				}}
			>
				<DownloadIcon class="size-3.5" />
				{t('files.page.sheets.downloadFolderZip')}
			</button>
			<button
				type="button"
				role="menuitem"
				class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs"
				onclick={() => {
					closeContextMenu();
					void refreshCurrent();
				}}
			>
				<RefreshCwIcon class="size-3.5" />
				{t('files.page.contextMenu.refreshShort')}
			</button>
		{/if}
	</div>
{/if}

<style>
	.fm-list-container {
		container: fm-list / inline-size;
	}

	.fm-mobile-view {
		display: block;
	}

	.fm-desktop-view {
		display: none;
	}

	.fm-desktop-view :global([data-slot='table-head']) {
		padding-inline: 0.75rem;
	}

	.fm-desktop-view :global([data-slot='table-head']:has([role='checkbox'])) {
		padding-left: 0.75rem;
		padding-right: 0;
	}

	.fm-desktop-view :global([data-slot='table-cell']) {
		padding: var(--fm-row-py, 11px) 0.75rem;
	}

	.fm-desktop-view :global([data-slot='table-cell']:has([role='checkbox'])) {
		padding-right: 0;
	}

	@container fm-list (inline-size >= 44rem) {
		.fm-mobile-view {
			display: none;
		}

		.fm-desktop-view {
			display: block;
		}
	}

	.fm-pointer-coarse-only {
		display: none;
	}

	@media (pointer: coarse) {
		.fm-pointer-fine-only {
			display: none;
		}

		.fm-pointer-coarse-only {
			display: inline;
		}
	}
</style>
