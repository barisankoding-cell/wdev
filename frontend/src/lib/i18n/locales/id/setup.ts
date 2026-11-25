import type { WidenStrings } from '../../types.js';
import type { setupEn } from '../en/setup.js';

export const setupId: WidenStrings<typeof setupEn> = {
	title: 'Instalasi, Preset Mode & Komponen',
	modeBadgePrefix: 'Mode:',
	subtitleVps: 'Preset dan daftar komponen disesuaikan untuk server Full VPS (Hosting, PowerDNS, & Mail Server).',
	subtitleEdge: 'Preset dan daftar komponen disesuaikan untuk node Edge Gateway lewat NetBird Tunnel.',
	subtitleLocal: 'Preset dan daftar komponen disesuaikan untuk mesin Lokal Standalone tanpa PowerDNS maupun Mail Server.',
	switchModeBtn: 'Ganti Mode',
	checkingBtn: 'Memeriksa...',
	checkInstalledBtn: 'Periksa Terpasang',
	cardTitle: 'Preset Mode {mode} & Komponen Modular',
	cardDesc:
		'Komponen yang sudah terpasang otomatis diberi tanda & centang default. Pilih preset sesuai kebutuhan mode atau atur checklist modular secara manual.',
	scanningBadge: 'Memindai komponen...',
	installedSummaryBadge: '{installed}/{total} terpasang',
	presetHeading: 'Preset Siap Pakai ({mode})',
	presetSubheading:
		'Memasang komponen yang belum terpasang dalam preset, atau muat ke daftar modular di bawah untuk menyesuaikan komponen dan versi.',
	presetCompleteBadge: 'Lengkap',
	presetInstalledRatio: '{installed}/{total} terpasang',
	presetActiveInModular: 'Aktif di Modular',
	installPresetAria: 'Pasang preset {title}',
	reinstallPresetBtn: 'Pasang Ulang Preset',
	installMissingPresetBtn: 'Pasang ({count} Belum Terpasang)',
	installPresetBtn: 'Pasang Preset',
	selectModularAria: 'Pilih komponen preset {title} di daftar modular',
	selectInModularBtn: 'Pilih di Modular',
	completingPresetLabel: 'Melengkapi Preset {title}',
	presetTaskLabel: 'Preset {title}',
	modularTitle: 'Daftar Komponen Modular',
	modularEmptyHint: 'Centang komponen yang ingin dipasang, atau klik "Centang Terpasang" untuk menampilkan status aktif secara default.',
	modularMatchedPreset: '{total} terpilih sesuai preset {title} ({installed} sudah terpasang, {missing} baru).',
	modularSelectedSummary: '{total} terpilih ({installed} sudah terpasang, {missing} belum terpasang).',
	modularTaskTitle: 'Instalasi Modular ({count} Komponen)',
	filterModeBtn: 'Filter Mode {mode}',
	allComponentsBtn: 'Semua Komponen (+{count})',
	checkInstalledSelectionBtn: 'Centang Terpasang',
	installNewBtn: 'Pasang {count} Komponen Baru',
	reinstallAllBtn: 'Pasang Ulang Semua ({count})',
	updateReinstallBtn: 'Perbarui / Pasang Ulang ({count})',
	updateReinstallTask: 'Perbarui / Pasang Ulang ({count} Komponen)',
	installSelectedBtn: 'Pasang Komponen Terpilih',
	selectedCountLabel: 'Terpilih ({count}):',
	installedBadgeVer: 'Terpasang · {ver}',
	versionAria: 'Versi {label}',
	versionPrefix: 'Versi: {ver}',
	autoLatest: 'Otomatis / Terbaru',
	dockerConfigAria: 'Konfigurasi Docker Daemon',
	installOnlyAria: '{action} {label} saja',
	reinstallVerb: 'Pasang ulang',
	installVerb: 'Pasang',
	reinstallOnlyBtn: 'Pasang Ulang',
	installOnlyBtn: 'Pasang Saja',
	fullSetupTitle: 'Instalasi Workstation Lengkap (Full Auto Setup - 14 Langkah)',
	fullSetupBadge: 'Bundel Workstation Penuh',
	fullSetupDescVps:
		'Bundel seluruh lingkungan pengembangan workstation (termasuk Android SDK, Flutter, Local SSL mkcert, & Zsh). Untuk server produksi VPS disarankan memakai Preset Mode VPS di atas.',
	fullSetupDescDefault: 'Menyiapkan seluruh lingkungan pengembangan Linux secara idempoten dalam satu alur otomatis.',
	runFullSetupBtn: 'Jalankan Instalasi Lengkap',
	historyTitle: 'Riwayat Eksekusi Runner',
	historyDesc: 'Catatan tugas instalasi, switch runtime, dan perbaikan sistem terakhir.',
	historyTableAria: 'Tabel Riwayat Tugas Control Plane',
	taskCol: 'Tugas',
	statusCol: 'Status',
	startedAtCol: 'Waktu Mulai',
	logCol: 'Log',
	noHistory: 'Belum ada riwayat tugas yang terekam.',
	viewLogBtn: 'Lihat Log',
	historyItemLabel: 'riwayat tugas',
	logDialogDefaultTitle: 'Detail Log Tugas',
	logDialogStatus: 'Status: {status} · {time}',
	logDialogEmpty: '(Log kosong)',
	fullSteps: {
		s1: '1. Dependensi Dasar Sistem',
		s2: '2. Node.js LTS (Multi-versi)',
		s3: '3. Rust + Mold + Sccache',
		s4: '4. Golang Stabil Terbaru',
		s5: '5. OpenJDK Java (JDK 21)',
		s6: '6. Android SDK & CLI Tools',
		s7: '7. Flutter SDK Stable',
		s8: '8. PostgreSQL Multi-versi',
		s9: '9. Redis / Valkey Server',
		s10: '10. Nginx Web Server',
		s11: '11. PHP & FPM (Multi-versi)',
		s12: '12. Composer Package Manager',
		s13: '13. Local SSL (mkcert CA)',
		s14: '14. Oh My Zsh & Powerlevel10k'
	},
	categories: {
		core: {
			title: 'Core & Build Tools',
			description: 'Runtime bahasa pemrograman dan compiler inti.'
		},
		webDb: {
			title: 'Hosting Web & Database',
			description: 'Stack server web, PHP, SSL, dan penyimpanan data.'
		},
		mobile: {
			title: 'Mobile & SDKs',
			description: 'Toolkit pengembangan aplikasi Android dan cross-platform.'
		},
		networkWorkspace: {
			title: 'Jaringan, Mail & Workspace',
			description: 'Layanan tunnel, VPN mesh, DNS, mail server, dan shell.'
		}
	},
	items: {
		base: { label: 'Dependensi Dasar', description: 'Paket sistem esensial, build tools, curl, dan git.' },
		node: { label: 'Node.js LTS', description: 'Runtime JavaScript/TypeScript dengan npm dan pnpm.' },
		go: { label: 'Golang', description: 'Compiler dan toolchain Go rilis stabil terbaru.' },
		rust: { label: 'Rust Toolchain & Mold', description: 'Rustup, Cargo, dan linker Mold berkecepatan tinggi.' },
		java: { label: 'OpenJDK Java', description: 'JDK LTS untuk service backend dan tooling Android.' },
		nginx: { label: 'Nginx Web Server', description: 'Web server dan reverse proxy berkinerja tinggi.' },
		php: { label: 'PHP & PHP-FPM', description: 'Runtime PHP, ekstensi web, dan service manager PHP-FPM.' },
		composer: { label: 'Composer', description: 'Manajer dependensi resmi untuk PHP & framework web.' },
		ssl: { label: 'Local SSL (mkcert)', description: 'Sertifikat HTTPS lokal tepercaya untuk domain dev (.test/.local).' },
		postgres: { label: 'PostgreSQL Server', description: 'Database relasional siap produksi dan pengembangan.' },
		redis: { label: 'Redis / Valkey Cache', description: 'Penyimpanan cache memori dan antrean kerja cepat.' },
		'php-stack': {
			label: 'Bundel PHP Web Stack',
			description: 'Pasang Nginx, PHP-FPM, Composer, dan SSL lokal sekaligus.'
		},
		docker: {
			label: 'Docker Engine & Compose v5',
			description: 'Container runtime Docker & plugin Compose v5 dari repositori resmi Docker.'
		},
		android: { label: 'Android SDK & Tools', description: 'Command-line tools, platform-tools, dan build-tools.' },
		flutter: { label: 'Flutter SDK', description: 'Framework UI untuk Android, Web, dan Linux desktop.' },
		netbird: {
			label: 'NetBird Mesh VPN',
			description: 'Tunnel WireGuard peer-to-peer antara node lokal dan VPS Edge Gateway.'
		},
		pdns: {
			label: 'PowerDNS Authoritative Server',
			description: 'Server DNS authoritative mandiri (SQLite3 + API 127.0.0.1:8081).'
		},
		mail: { label: 'Mail Server Stack', description: 'Stalwart Mail Server dengan webmail SnappyMail.' },
		cloudflare: { label: 'Cloudflare Tunnel', description: 'Publikasi layanan lewat tunnel terenkripsi cloudflared.' },
		zsh: { label: 'Oh My Zsh', description: 'Terminal Zsh interaktif dengan tema produktivitas.' },
		'orca-desktop': { label: 'Orca Desktop GUI', description: 'Orca ADE dengan antarmuka desktop grafis lengkap.' },
		'orca-headless': { label: 'Orca Headless Server', description: 'Mode daemon Orca ADE tanpa desktop untuk server remote.' }
	},
	presets: {
		'local-dev': {
			title: 'Workstation Development Lengkap',
			badge: 'Rekomendasi Lokal',
			description:
				'Stack pengembangan lokal lengkap: Node.js, Go, Rust, Nginx, PHP, SSL lokal (mkcert), DB, dan Oh My Zsh tanpa PowerDNS/Mail.'
		},
		'local-web': {
			title: 'Web Stack & Database Lokal',
			badge: 'Hosting Dev Lokal',
			description: 'Nginx, PHP-FPM, Composer, HTTPS mkcert, PostgreSQL, dan Redis untuk proyek Laravel, WordPress, maupun PHP.'
		},
		'local-cf': {
			title: 'Web Lokal + Cloudflare Tunnel',
			badge: 'Ringan + Tunnel',
			description: 'Komponen web inti (Nginx, PHP, Composer, mkcert) dengan cloudflared untuk preview/ekspos sementara tanpa VPS.'
		},
		'local-mobile': {
			title: 'Mobile & Cross-Platform',
			badge: 'Android & Flutter',
			description: 'Lingkungan build aplikasi mobile lengkap dengan OpenJDK Java, Node.js, Android SDK, dan Flutter.'
		},
		'edge-client': {
			title: 'Edge Node Web & Database',
			badge: 'Rekomendasi Edge',
			description: 'Komponen inti Edge Client: NetBird VPN untuk tunnel ke VPS Edge Gateway, Nginx, PHP-FPM, Composer, PostgreSQL, dan Redis.'
		},
		'edge-dev': {
			title: 'Edge Workstation Developer + Tunnel',
			badge: 'Dev + Tunnel',
			description: 'Lingkungan dev lengkap (Node.js, Go, Rust, Web Stack, DB, Zsh) yang langsung terekspos via NetBird ke VPS Edge.'
		},
		'edge-lite': {
			title: 'Edge Web Ringan (Tanpa DB)',
			badge: 'Minimal Edge',
			description: 'Hanya dependensi dasar, NetBird VPN, Nginx, PHP-FPM, dan Composer bila database berada di server terpisah.'
		},
		'edge-mobile': {
			title: 'Mobile & Cross-Platform',
			badge: 'Android & Flutter',
			description: 'Toolkit build aplikasi mobile dengan OpenJDK Java, Node.js, Android SDK, dan Flutter.'
		},
		'vps-complete': {
			title: 'Full VPS Hosting (Web + DB + DNS + Mail)',
			badge: 'Rekomendasi VPS',
			description: 'Hosting VPS mandiri ala aaPanel: Nginx, PHP-FPM, Composer, PostgreSQL, Redis, PowerDNS Authoritative, dan Mail Server Stalwart.'
		},
		'vps-hosting': {
			title: 'Produksi VPS (Web + Database)',
			badge: 'Hosting Web',
			description: 'Siap melayani situs produksi PHP, Laravel, WordPress, Node/Go, dan database tanpa DNS & Mail Server lokal.'
		},
		'vps-dns-mail': {
			title: 'DNS Authoritative & Mail Server VPS',
			badge: 'PowerDNS + Mail',
			description: 'Pasang PowerDNS Authoritative lokal (port 53/8081) dan Mail Server Stalwart + Webmail SnappyMail di VPS.'
		},
		'vps-web-lite': {
			title: 'Web Server VPS Ringan (Nginx + PHP)',
			badge: 'Ringan',
			description: 'Hanya web server Nginx, PHP-FPM, dan Composer bagi VPS yang memakai managed database eksternal.'
		}
	}
};
