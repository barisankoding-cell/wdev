import type { ServerMode } from '#lib/stores/mode.svelte.js';

export const APP_TITLE = 'wdev Control Plane';
export const DEFAULT_SEO_TITLE = 'wdev — Server Control Plane';
export const DEFAULT_SEO_DESCRIPTION =
	'Panel kontrol server Linux untuk mengelola web stack, multi-versi runtime, toolchain, berkas, terminal SSH, DNS, dan layanan server.';

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
	return `${title} — ${APP_TITLE}`;
}

export function getPageMeta(pathname: string, mode: ServerMode): PageMeta {
	const path = normalizePathname(pathname);
	const resolvedPath = resolveRoutePath(path);

	switch (resolvedPath) {
		case '/':
			return {
				title: 'Status & Pembaruan Sistem',
				seoTitle: buildSeoTitle('Status & Pembaruan Sistem'),
				description:
					'Pantau kesehatan server Linux, pembaruan sistem, runtime aktif, dan status layanan daemon.'
			};
		case '/webstack': {
			const title = mode === 'local' ? 'Web Stack & Domain Lokal' : 'Web Stack & Virtual Host';
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description:
					'Kelola virtual host Nginx, konfigurasi PHP-FPM, domain, serta sertifikat SSL.'
			};
		}
		case '/compose':
			return {
				title: 'Docker & Compose v5',
				seoTitle: buildSeoTitle('Docker & Compose v5'),
				description:
					'Kelola stack Docker Compose v5, status container, editor YAML/.env, live logs, exec shell, dan eksposur VHost.'
			};
		case '/switchers':
			return {
				title: 'Pengelola Multi-Versi Runtime',
				seoTitle: buildSeoTitle('Pengelola Multi-Versi Runtime'),
				description:
					'Beralih versi aktif Node.js, PHP CLI & FPM, PostgreSQL, dan Java OpenJDK dengan cepat.'
			};
		case '/toolchains':
			return {
				title: 'Toolchains, Compilers & SDKs',
				seoTitle: buildSeoTitle('Toolchains, Compilers & SDKs'),
				description:
					'Status versi toolchain terpasang, deteksi rilis resmi upstream, dan audit PATH sistem.'
			};
		case '/workspace':
			return {
				title: 'Workspace, ADE & Shell',
				seoTitle: buildSeoTitle('Workspace, ADE & Shell'),
				description:
					'Kelola Orca Autonomous Development Environment (ADE), akses Web Client, dan Oh My Zsh.'
			};
		case '/ports':
			return {
				title: 'Status Port & Jaringan',
				seoTitle: buildSeoTitle('Status Port & Jaringan'),
				description: 'Diagnostik port layanan wdev pada Lokal, VPN NetBird, Firewall, dan IP VPS.'
			};
		case '/files':
			return {
				title: 'File Manager',
				seoTitle: buildSeoTitle('File Manager'),
				description:
					'Jelajahi, unggah, unduh, edit, dan kelola berkas serta direktori server secara langsung.'
			};
		case '/terminal':
			return {
				title: 'Terminal SSH (PTY)',
				seoTitle: buildSeoTitle('Terminal SSH (PTY)'),
				description:
					'Akses sesi terminal shell interaktif (PTY) langsung dari browser untuk mengelola server.'
			};
		case '/powerdns': {
			const title =
				mode === 'edge'
					? 'PowerDNS & VPS Edge Gateway'
					: mode === 'local'
						? 'Domain & DNS Manager (Lokal)'
						: 'Domain & DNS Manager';
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description:
					'Kelola PowerDNS Authoritative Server, zona dan record DNS domain, serta edge gateway.'
			};
		}
		case '/cloudflare':
			return {
				title: 'Cloudflare Tunnel & DNS Manager',
				seoTitle: buildSeoTitle('Cloudflare Tunnel & DNS Manager'),
				description:
					'Kelola profil multi-tunnel Cloudflare dan eksposur virtual host lokal ke domain publik.'
			};
		case '/mail': {
			const title =
				mode === 'edge'
					? 'Mail Server & Outbound SMTP'
					: mode === 'local'
						? 'Mail Server Standalone (Lokal)'
						: 'Mail Server & Webmail';
			return {
				title,
				seoTitle: buildSeoTitle(title),
				description:
					'Kelola Stalwart Mail Server, webmail SnappyMail, kotak pos domain, dan outbound SMTP.'
			};
		}
		case '/setup':
			return {
				title: 'Instalasi, Preset Mode & Komponen',
				seoTitle: buildSeoTitle('Instalasi, Preset Mode & Komponen'),
				description:
					'Instalasi komponen modular server, pemilihan preset mode operasional, dan riwayat tugas.'
			};
		default:
			return {
				title: 'wdev Dashboard',
				seoTitle: buildSeoTitle('wdev Dashboard'),
				description: DEFAULT_SEO_DESCRIPTION
			};
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
