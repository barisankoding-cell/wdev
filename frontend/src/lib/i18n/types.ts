export type Locale = 'en' | 'id';

export const DEFAULT_LOCALE: Locale = 'en';
export const LOCALE_STORAGE_KEY = 'wdev:locale';

export type InterpolationParams = Record<string, string | number>;

export type WidenStrings<T> = T extends string
	? string
	: T extends object
		? { readonly [K in keyof T]: WidenStrings<T[K]> }
		: T;

export type LeafPaths<T, Prefix extends string = ''> = T extends string
	? Prefix
	: T extends object
		? {
				[K in keyof T & string]: LeafPaths<T[K], Prefix extends '' ? K : `${Prefix}.${K}`>;
			}[keyof T & string]
		: never;

export function interpolate(template: string, params?: InterpolationParams): string {
	if (!params) return template;
	return template.replace(/\{(\w+)\}/g, (match, key: string) => {
		const val = params[key];
		return val !== undefined ? String(val) : match;
	});
}

export function flattenMessages(obj: Record<string, unknown>, prefix = ''): Record<string, string> {
	const out: Record<string, string> = {};
	for (const [key, value] of Object.entries(obj)) {
		const path = prefix ? `${prefix}.${key}` : key;
		if (typeof value === 'string') {
			out[path] = value;
		} else if (value && typeof value === 'object' && !Array.isArray(value)) {
			Object.assign(out, flattenMessages(value as Record<string, unknown>, path));
		}
	}
	return out;
}
