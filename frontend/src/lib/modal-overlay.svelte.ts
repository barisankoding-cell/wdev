import { untrack } from 'svelte';

export type ModalOverlayKind = 'modal' | 'sheet';

const dismissableMenus = new Set<() => void>();
const actionSheets = new Set<() => void>();

let openModalCount = $state(0);

/**
 * Mengembalikan true jika ada modal (Dialog atau AlertDialog) yang sedang terbuka.
 */
export function hasOpenModal(): boolean {
	return openModalCount > 0;
}

/**
 * Mendaftarkan callback penutup untuk menu aksi mengambang (DropdownMenu, ContextMenu, Select).
 */
export function registerDismissableMenu(closeFn: () => void): () => void {
	dismissableMenus.add(closeFn);
	return () => {
		dismissableMenus.delete(closeFn);
	};
}

/**
 * Mendaftarkan callback penutup untuk Sheet aksi mobile agar ikut tertutup saat modal muncul.
 */
export function registerActionSheet(closeFn: () => void): () => void {
	actionSheets.add(closeFn);
	return () => {
		actionSheets.delete(closeFn);
	};
}

/**
 * Menutup menu/overlay aksi yang terbuka saat modal atau sheet baru tampil.
 */
export function notifyOverlayOpen(kind: ModalOverlayKind): void {
	untrack(() => {
		for (const closeMenu of dismissableMenus) {
			closeMenu();
		}
		if (kind === 'modal') {
			for (const closeSheet of actionSheets) {
				closeSheet();
			}
		}
	});
}

/**
 * Melacak siklus hidup modal/sheet terbuka; dipanggil dari $effect.pre ketika `open` bernilai true.
 */
export function trackOverlayOpen(kind: ModalOverlayKind): () => void {
	notifyOverlayOpen(kind);
	if (kind === 'modal') {
		untrack(() => {
			openModalCount += 1;
		});
		return () => {
			untrack(() => {
				openModalCount = Math.max(0, openModalCount - 1);
			});
		};
	}
	return () => {};
}
