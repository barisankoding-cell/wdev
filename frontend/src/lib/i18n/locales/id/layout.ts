import type { WidenStrings } from '../../types.js';
import type { layoutEn } from '../en/layout.js';

export const layoutId: WidenStrings<typeof layoutEn> = {
	appTitle: 'wdev Control Plane',
	defaultSeoTitle: 'wdev — Server Control Plane',
	defaultSeoDescription:
		'Panel kontrol server Linux untuk mengelola web stack, multi-versi runtime, toolchain, berkas, terminal SSH, DNS, dan layanan server.',
	loadingPanel: 'Memuat panel wdev...',
	skipToMain: 'Lompat ke konten utama',
	loginSeoTitle: 'Masuk — wdev Control Plane',
	loginSeoDescription: 'Masuk ke panel kontrol server wdev untuk mengelola infrastruktur dan layanan Anda.',
	nav: {
		groups: {
			overview: 'Ringkasan',
			webDev: 'Web & Pengembangan',
			systemFiles: 'Sistem & Berkas',
			networkEdge: 'Jaringan & Edge Gateway',
			domainDnsMail: 'Domain, DNS & Mail',
			optionalExpose: 'Ekspos Opsional',
			installSetup: 'Instalasi & Setup'
		},
		items: {
			dashboard: 'Status & Update',
			webstack: 'Web Stack & VHost',
			compose: 'Docker & Compose',
			switchers: 'Multi-Versi Runtime',
			toolchains: 'Toolchains & SDKs',
			workspace: 'Workspace & ADE',
			ports: 'Status Port & Jaringan',
			files: 'File Manager',
			terminal: 'Terminal SSH (PTY)',
			security: 'Keamanan & Serangan',
			powerdnsEdge: 'PowerDNS & Edge',
			powerdnsVps: 'Domain & DNS Manager',
			cloudflare: 'Cloudflare Tunnel',
			mailEdge: 'Mail Server & Relay',
			mailVps: 'Mail Server',
			setup: 'Setup & Komponen'
		},
		footer: {
			defaultHost: 'Linux Host',
			defaultSub: 'Control Plane',
			changeModeAria: 'Ubah Mode Server',
			modeLocalShort: 'Lokal',
			modeVpsShort: 'VPS',
			modeEdgeShort: 'Edge',
			restartSystem: 'Restart Sistem',
			restartingSystem: 'Me-restart...',
			restartSystemAria: 'Restart sistem server'
		}
	},
	topbar: {
		serverMode: 'Mode Server',
		panelMenuBtn: 'Panel',
		panelSettingsLabel: 'Pengaturan Panel',
		terminalBtn: 'Terminal',
		openTerminalAria: 'Buka Live Terminal Console',
		taskRunningAria: 'Tugas sedang berjalan',
		signedInAs: 'Masuk sebagai',
		changeCreds: 'Ubah Kredensial',
		panelDomainHttps: 'Domain & HTTPS Panel',
		signOut: 'Keluar Panel'
	},
	meta: {
		dashboard: {
			title: 'Status & Pembaruan Sistem',
			description: 'Pantau kesehatan server Linux, pembaruan sistem, runtime aktif, dan status layanan daemon.'
		},
		webstack: {
			titleLocal: 'Web Stack & Domain Lokal',
			titleDefault: 'Web Stack & Virtual Host',
			description: 'Kelola virtual host Nginx, konfigurasi PHP-FPM, domain, serta sertifikat SSL.'
		},
		compose: {
			title: 'Docker & Compose v5',
			description:
				'Kelola stack Docker Compose v5, status container, editor YAML/.env, live logs, exec shell, dan eksposur VHost.'
		},
		switchers: {
			title: 'Pengelola Multi-Versi Runtime',
			description: 'Beralih versi aktif Node.js, PHP CLI & FPM, PostgreSQL, dan Java OpenJDK dengan cepat.'
		},
		toolchains: {
			title: 'Toolchains, Compilers & SDKs',
			description: 'Status versi toolchain terpasang, deteksi rilis resmi upstream, dan audit PATH sistem.'
		},
		workspace: {
			title: 'Workspace, ADE & Shell',
			description: 'Kelola Orca Autonomous Development Environment (ADE), akses Web Client, dan Oh My Zsh.'
		},
		ports: {
			title: 'Status Port & Jaringan',
			description: 'Diagnostik port layanan wdev pada Lokal, VPN NetBird, Firewall, dan IP VPS.'
		},
		files: {
			title: 'File Manager',
			description: 'Jelajahi, unggah, unduh, edit, dan kelola berkas serta direktori server secara langsung.'
		},
		terminal: {
			title: 'Terminal SSH (PTY)',
			description: 'Akses sesi terminal shell interaktif (PTY) langsung dari browser untuk mengelola server.'
		},
		powerdns: {
			titleEdge: 'PowerDNS & VPS Edge Gateway',
			titleLocal: 'Domain & DNS Manager (Lokal)',
			titleDefault: 'Domain & DNS Manager',
			description: 'Kelola PowerDNS Authoritative Server, zona dan record DNS domain, serta edge gateway.'
		},
		cloudflare: {
			title: 'Cloudflare Tunnel & DNS Manager',
			description: 'Kelola profil multi-tunnel Cloudflare dan eksposur virtual host lokal ke domain publik.'
		},
		mail: {
			titleEdge: 'Mail Server & Outbound SMTP',
			titleLocal: 'Mail Server Standalone (Lokal)',
			titleDefault: 'Mail Server & Webmail',
			description: 'Kelola Stalwart Mail Server, webmail SnappyMail, kotak pos domain, dan outbound SMTP.'
		},
		setup: {
			title: 'Instalasi, Preset Mode & Komponen',
			description: 'Instalasi komponen modular server, pemilihan preset mode operasional, dan riwayat tugas.'
		},
		security: {
			title: 'Keamanan & Serangan Siber',
			description: 'Pantau aktivitas serangan siber, jebakan honeypot, brute force, dan kelola pemblokiran IP.'
		},
		fallback: {
			title: 'wdev Dashboard'
		}
	}
};
