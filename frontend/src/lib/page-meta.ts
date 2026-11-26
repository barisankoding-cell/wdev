import { t } from '#lib/i18n/index.svelte.js';
import type { ServerMode } from '#lib/stores/mode.svelte.js';

export function getAppTitle(): string {
	return t('layout.appTitle');
}

export function getDefaultSeoTitle(): string {
	return t('layout.defaultSeoTitle');
}

export function getDefaultSeoDescription(): string {
	return t('layout.defaultSeoDescription');
}

export interface PageMeta {
	title: string;
	seoTitle: string;
	description: string;
}

export function normalizePathname(pathname: string): string {
	if (!pathname || pathname === '/') return '/';
	return pathname.replace(/\/+$/, '') || '/';
}

function buildSeoTitle(title: string): string {
	return `${title} — ${getAppTitle()}`;
}

export function getPageMeta(pathname: string, mode: ServerMode): PageMeta {
	const path = normalizePathname(pathname);
	const resolvedPath = resolveRoutePath(path);

	switch (resolvedPath) {
		case '/': {
			const title = t('layout.meta.dashboard.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.dashboard.description')
			};
		}
		case '/webstack': {
			const title =
				mode === 'local'
					? t('layout.meta.webstack.titleLocal')
					: t('layout.meta.webstack.titleDefault');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.webstack.description')
			};
		}
		case '/compose': {
			const title = t('layout.meta.compose.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.compose.description')
			};
		}
		case '/switchers': {
			const title = t('layout.meta.switchers.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.switchers.description')
			};
		}
		case '/toolchains': {
			const title = t('layout.meta.toolchains.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.toolchains.description')
			};
		}
		case '/workspace': {
			const title = t('layout.meta.workspace.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.workspace.description')
			};
		}
		case '/ports': {
			const title = t('layout.meta.ports.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.ports.description')
			};
		}
		case '/files': {
			const title = t('layout.meta.files.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.files.description')
			};
		}
		case '/terminal': {
			const title = t('layout.meta.terminal.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.terminal.description')
		};
	}
	case '/security': {
		const title = t('layout.meta.security.title');
		return {
			title,
			seoTitle: buildSeoTitle(title),
			description: t('layout.meta.security.description')
		};
	}
		case '/powerdns': {
			const title =
				mode === 'edge'
					? t('layout.meta.powerdns.titleEdge')
					: mode === 'local'
						? t('layout.meta.powerdns.titleLocal')
						: t('layout.meta.powerdns.titleDefault');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.powerdns.description')
			};
		}
		case '/cloudflare': {
			const title = t('layout.meta.cloudflare.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.cloudflare.description')
			};
		}
		case '/mail': {
			const title =
				mode === 'edge'
					? t('layout.meta.mail.titleEdge')
					: mode === 'local'
						? t('layout.meta.mail.titleLocal')
						: t('layout.meta.mail.titleDefault');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.mail.description')
			};
		}
		case '/setup': {
			const title = t('layout.meta.setup.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: t('layout.meta.setup.description')
			};
		}
		default: {
			const title = t('layout.meta.fallback.title');
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description: getDefaultSeoDescription()
			};
		}
	}
}

const KNOWN_ROUTES = [
	'/',
	'/webstack',
	'/compose',
	'/switchers',
	'/toolchains',
	'/workspace',
	'/ports',
	'/files',
	'/terminal',
	'/security',
	'/powerdns',
	'/cloudflare',
	'/mail',
	'/setup'
] as const;

function resolveRoutePath(path: string): string {
	if (KNOWN_ROUTES.includes(path as (typeof KNOWN_ROUTES)[number])) {
		return path;
	}
	for (const route of KNOWN_ROUTES) {
		if (route !== '/' && path.startsWith(`${route}/`)) {
			return route;
		}
	}
	return '';
}

export function syncMetaDescription(content: string): void {
	if (typeof document === 'undefined') return;
	let meta = document.querySelector<HTMLMetaElement>('meta[name="description"]');
	if (!meta) {
		meta = document.createElement('meta');
		meta.name = 'description';
		document.head.appendChild(meta);
	}
	if (meta.getAttribute('content') !== content) {
		meta.setAttribute('content', content);
	}
}
