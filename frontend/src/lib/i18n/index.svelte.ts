import {
	DEFAULT_LOCALE,
	LOCALE_STORAGE_KEY,
	flattenMessages,
	interpolate,
	type InterpolationParams,
	type LeafPaths,
	type Locale
} from './types.js';
import { enMessages, type TranslationSchema } from './locales/en/index.js';
import { idMessages } from './locales/id/index.js';

export type { Locale, InterpolationParams };
export { DEFAULT_LOCALE, LOCALE_STORAGE_KEY };
export type TranslationKey = LeafPaths<TranslationSchema>;

const flatEn = flattenMessages(enMessages as unknown as Record<string, unknown>);
const flatId = flattenMessages(idMessages as unknown as Record<string, unknown>);

function parseLocale(raw: string | null): Locale {
	if (raw === 'id' || raw === 'en') return raw;
	return DEFAULT_LOCALE;
}

function readStoredLocale(): Locale {
	if (typeof window === 'undefined') return DEFAULT_LOCALE;
	try {
		return parseLocale(window.localStorage.getItem(LOCALE_STORAGE_KEY));
	} catch {
		return DEFAULT_LOCALE;
	}
}

class I18nStore {
	locale = $state<Locale>(readStoredLocale());
	initialized = false;

	intlLocale = $derived<string>(this.locale === 'id' ? 'id-ID' : 'en-US');

	init() {
		if (this.initialized || typeof window === 'undefined') return;
		this.initialized = true;
		this.locale = readStoredLocale();
		this.applyToDocument();

		const onStorage = (e: StorageEvent) => {
			if (e.key === LOCALE_STORAGE_KEY) {
				this.locale = readStoredLocale();
				this.applyToDocument();
			}
		};
		window.addEventListener('storage', onStorage);
	}

	setLocale(next: Locale) {
		this.locale = next;
		if (typeof window !== 'undefined') {
			try {
				window.localStorage.setItem(LOCALE_STORAGE_KEY, next);
			} catch {
				// Ignore storage access errors in restricted contexts
			}
		}
		this.applyToDocument();
	}

	private applyToDocument() {
		if (typeof document === 'undefined') return;
		if (document.documentElement.lang !== this.locale) {
			document.documentElement.lang = this.locale;
		}
	}

	t = (key: TranslationKey, params?: InterpolationParams): string => {
		const table = this.locale === 'id' ? flatId : flatEn;
		const template = table[key] ?? flatEn[key] ?? key;
		return params ? interpolate(template, params) : template;
	};

	/** Lookup a dynamic key safely at runtime (for backend keys or mapped lookups). */
	tryTranslate = (key: string, params?: InterpolationParams): string | undefined => {
		const table = this.locale === 'id' ? flatId : flatEn;
		const template = table[key] ?? flatEn[key];
		if (!template) return undefined;
		return params ? interpolate(template, params) : template;
	};

	formatDate(
		value: string | number | Date | undefined | null,
		options?: Intl.DateTimeFormatOptions
	): string {
		if (value === undefined || value === null || value === '') return '';
		const d = value instanceof Date ? value : new Date(value);
		if (Number.isNaN(d.getTime())) return String(value);
		return d.toLocaleDateString(this.intlLocale, options);
	}

	formatTime(
		value?: string | number | Date | null,
		options?: Intl.DateTimeFormatOptions
	): string {
		const d = value === undefined || value === null || value === '' ? new Date() : value instanceof Date ? value : new Date(value);
		if (Number.isNaN(d.getTime())) return String(value ?? '');
		return d.toLocaleTimeString(this.intlLocale, options);
	}

	formatDateTime(
		value: string | number | Date | undefined | null,
		options?: Intl.DateTimeFormatOptions
	): string {
		if (value === undefined || value === null || value === '') return '';
		const d = value instanceof Date ? value : new Date(value);
		if (Number.isNaN(d.getTime())) return String(value);
		return d.toLocaleString(this.intlLocale, options);
	}

	formatNumber(value: number, options?: Intl.NumberFormatOptions): string {
		return new Intl.NumberFormat(this.intlLocale, options).format(value);
	}

	formatDaysHMS(seconds: number | undefined, fallback: string): string {
		if (seconds == null || seconds < 0) return fallback || 'N/A';
		const total = Math.floor(seconds);
		const days = Math.floor(total / 86400);
		const hours = Math.floor((total % 86400) / 3600);
		const mins = Math.floor((total % 3600) / 60);
		const secs = total % 60;
		const hms = [hours, mins, secs].map((n) => String(n).padStart(2, '0')).join(':');
		if (days <= 0) return hms;
		const unit = this.locale === 'id' ? 'hari' : days === 1 ? 'day' : 'days';
		return `${days} ${unit} ${hms}`;
	}
}

export const i18n = new I18nStore();

export function t(key: TranslationKey, params?: InterpolationParams): string {
	return i18n.t(key, params);
}
