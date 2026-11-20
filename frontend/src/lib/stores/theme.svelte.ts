import { resetMode, setMode } from 'mode-watcher';

export type ThemePreference = 'system' | 'light' | 'dark';

const STORAGE_KEY = 'color-scheme';

function readStoredPreference(): ThemePreference {
	if (typeof window === 'undefined') return 'system';
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		if (raw === 'light' || raw === 'dark') return raw;
	} catch {
		// Ignore storage access errors in restricted contexts
	}
	return 'system';
}

function getSystemIsDark(): boolean {
	if (typeof window === 'undefined') return true;
	return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

class ThemeStore {
	preference = $state<ThemePreference>('system');
	systemDark = $state<boolean>(true);
	initialized = false;

	resolved = $derived<'light' | 'dark'>(
		this.preference === 'system' ? (this.systemDark ? 'dark' : 'light') : this.preference
	);

	init() {
		if (this.initialized || typeof window === 'undefined') return;
		this.initialized = true;
		this.preference = readStoredPreference();
		this.systemDark = getSystemIsDark();
		this.applyToDocument();

		const mql = window.matchMedia('(prefers-color-scheme: dark)');
		const onMediaChange = (e: MediaQueryListEvent) => {
			this.systemDark = e.matches;
			this.applyToDocument();
		};
		mql.addEventListener('change', onMediaChange);

		const onStorage = (e: StorageEvent) => {
			if (e.key === STORAGE_KEY) {
				this.preference = readStoredPreference();
				this.applyToDocument();
			}
		};
		window.addEventListener('storage', onStorage);
	}

	setPreference(next: ThemePreference) {
		this.preference = next;
		if (typeof window !== 'undefined') {
			try {
				if (next === 'system') {
					localStorage.removeItem(STORAGE_KEY);
				} else {
					localStorage.setItem(STORAGE_KEY, next);
				}
			} catch {
				// Ignore storage access errors
			}
		}
		this.applyToDocument();
	}

	private applyToDocument() {
		if (typeof document === 'undefined') return;
		const meta = document.querySelector<HTMLMetaElement>('meta[name="color-scheme"]');
		if (meta) {
			meta.content = this.preference === 'system' ? 'light dark' : this.preference;
		}
		if (this.preference === 'system') {
			resetMode();
		} else {
			setMode(this.preference);
		}
		document.documentElement.classList.toggle('dark', this.resolved === 'dark');
	}
}

export const themeStore = new ThemeStore();
