export const setupEn = {
	title: 'Installation, Mode Presets & Components',
	modeBadgePrefix: 'Mode:',
	subtitleVps: 'Presets and components tailored for Full VPS servers (Hosting, PowerDNS, & Mail Server).',
	subtitleEdge: 'Presets and components tailored for Edge Gateway nodes via NetBird Tunnel.',
	subtitleLocal: 'Presets and components tailored for Local Standalone machines without PowerDNS or Mail Server.',
	switchModeBtn: 'Switch Mode',
	checkingBtn: 'Checking...',
	checkInstalledBtn: 'Check Installed',
	cardTitle: '{mode} Mode Presets & Modular Components',
	cardDesc:
		'Already-installed components are automatically marked and checked by default. Choose a mode preset or customize the modular checklist manually.',
	scanningBadge: 'Scanning components...',
	installedSummaryBadge: '{installed}/{total} installed',
	presetHeading: 'Ready-to-Use Presets ({mode})',
	presetSubheading:
		'Installs missing components in a preset, or loads them into the modular list below to customize components and versions.',
	presetCompleteBadge: 'Complete',
	presetInstalledRatio: '{installed}/{total} installed',
	presetActiveInModular: 'Active in Modular',
	installPresetAria: 'Install preset {title}',
	reinstallPresetBtn: 'Reinstall Preset',
	installMissingPresetBtn: 'Install ({count} Missing)',
	installPresetBtn: 'Install Preset',
	selectModularAria: 'Select {title} preset components in modular list',
	selectInModularBtn: 'Select in Modular',
	completingPresetLabel: 'Completing Preset {title}',
	presetTaskLabel: 'Preset {title}',
	modularTitle: 'Modular Components List',
	modularEmptyHint: 'Check components to install, or click "Check Installed" to select active components.',
	modularMatchedPreset: '{total} selected matching preset {title} ({installed} installed, {missing} new).',
	modularSelectedSummary: '{total} selected ({installed} installed, {missing} not installed).',
	modularTaskTitle: 'Modular Installation ({count} Components)',
	filterModeBtn: 'Filter {mode} Mode',
	allComponentsBtn: 'All Components (+{count})',
	checkInstalledSelectionBtn: 'Check Installed',
	installNewBtn: 'Install {count} New Components',
	reinstallAllBtn: 'Reinstall All ({count})',
	updateReinstallBtn: 'Update / Reinstall ({count})',
	updateReinstallTask: 'Update / Reinstall ({count} Components)',
	installSelectedBtn: 'Install Selected Components',
	selectedCountLabel: 'Selected ({count}):',
	installedBadgeVer: 'Installed · {ver}',
	versionAria: '{label} version',
	versionPrefix: 'Version: {ver}',
	autoLatest: 'Automatic / Latest',
	dockerConfigAria: 'Configure Docker Daemon',
	installOnlyAria: '{action} {label} only',
	reinstallVerb: 'Reinstall',
	installVerb: 'Install',
	reinstallOnlyBtn: 'Reinstall',
	installOnlyBtn: 'Install Only',
	fullSetupTitle: 'Complete Workstation Setup (Full Auto Setup - 14 Steps)',
	fullSetupBadge: 'Full Workstation Bundle',
	fullSetupDescVps:
		'Bundles the entire workstation dev environment (including Android SDK, Flutter, Local SSL mkcert, & Zsh). For production VPS servers, using VPS Mode Presets above is recommended.',
	fullSetupDescDefault: 'Sets up the complete Linux development environment idempotently in one automated flow.',
	runFullSetupBtn: 'Run Complete Installation',
	historyTitle: 'Runner Execution History',
	historyDesc: 'Recent installation, runtime switch, and system repair task records.',
	historyTableAria: 'Control Plane Task History Table',
	taskCol: 'Task',
	statusCol: 'Status',
	startedAtCol: 'Start Time',
	logCol: 'Log',
	noHistory: 'No task history recorded yet.',
	viewLogBtn: 'View Log',
	historyItemLabel: 'task history records',
	logDialogDefaultTitle: 'Task Log Details',
	logDialogStatus: 'Status: {status} · {time}',
	logDialogEmpty: '(Empty log)',
	fullSteps: {
		s1: '1. Base System Dependencies',
		s2: '2. Node.js LTS (Multi-version)',
		s3: '3. Rust + Mold + Sccache',
		s4: '4. Latest Stable Golang',
		s5: '5. OpenJDK Java (JDK 21)',
		s6: '6. Android SDK & CLI Tools',
		s7: '7. Flutter SDK Stable',
		s8: '8. Multi-version PostgreSQL',
		s9: '9. Redis / Valkey Server',
		s10: '10. Nginx Web Server',
		s11: '11. PHP & FPM (Multi-version)',
		s12: '12. Composer Package Manager',
		s13: '13. Local SSL (mkcert CA)',
		s14: '14. Oh My Zsh & Powerlevel10k'
	},
	categories: {
		core: {
			title: 'Core & Build Tools',
			description: 'Core programming language runtimes and compilers.'
		},
		webDb: {
			title: 'Web Hosting & Databases',
			description: 'Web server stack, PHP, SSL, and data stores.'
		},
		mobile: {
			title: 'Mobile & SDKs',
			description: 'Android and cross-platform app development toolkits.'
		},
		networkWorkspace: {
			title: 'Network, Mail & Workspace',
			description: 'Tunnel services, mesh VPN, DNS, mail server, and shell.'
		}
	},
	items: {
		base: { label: 'Base Dependencies', description: 'Essential system packages, build tools, curl, and git.' },
		node: { label: 'Node.js LTS', description: 'JavaScript/TypeScript runtime with npm and pnpm.' },
		go: { label: 'Golang', description: 'Latest stable Go compiler and toolchain.' },
		rust: { label: 'Rust Toolchain & Mold', description: 'Rustup, Cargo, and high-speed Mold linker.' },
		java: { label: 'OpenJDK Java', description: 'LTS JDK for backend services and Android tooling.' },
		nginx: { label: 'Nginx Web Server', description: 'High-performance web server and reverse proxy.' },
		php: { label: 'PHP & PHP-FPM', description: 'PHP runtime, web extensions, and PHP-FPM service manager.' },
		composer: { label: 'Composer', description: 'Official dependency manager for PHP & web frameworks.' },
		ssl: { label: 'Local SSL (mkcert)', description: 'Trusted local HTTPS certificates for dev domains (.test/.local).' },
		postgres: { label: 'PostgreSQL Server', description: 'Production- and development-ready relational database.' },
		redis: { label: 'Redis / Valkey Cache', description: 'Fast in-memory cache and job queue store.' },
		'php-stack': {
			label: 'PHP Web Stack Bundle',
			description: 'Install Nginx, PHP-FPM, Composer, and local SSL at once.'
		},
		docker: {
			label: 'Docker Engine & Compose v5',
			description: 'Docker container runtime & Compose v5 plugin from official Docker repository.'
		},
		android: { label: 'Android SDK & Tools', description: 'Command-line tools, platform-tools, and build-tools.' },
		flutter: { label: 'Flutter SDK', description: 'UI framework for Android, Web, and Linux desktop.' },
		netbird: {
			label: 'NetBird Mesh VPN',
			description: 'Peer-to-peer WireGuard tunnel between local node and VPS Edge Gateway.'
		},
		pdns: {
			label: 'PowerDNS Authoritative Server',
			description: 'Standalone authoritative DNS server (SQLite3 + API 127.0.0.1:8081).'
		},
		mail: { label: 'Mail Server Stack', description: 'Stalwart Mail Server with SnappyMail webmail.' },
		cloudflare: { label: 'Cloudflare Tunnel', description: 'Expose services via encrypted cloudflared tunnels.' },
		zsh: { label: 'Oh My Zsh', description: 'Interactive Zsh terminal with productivity themes.' },
		'orca-desktop': { label: 'Orca Desktop GUI', description: 'Orca ADE with full graphical desktop interface.' },
		'orca-headless': { label: 'Orca Headless Server', description: 'Desktop-less Orca ADE daemon mode for remote servers.' }
	},
	presets: {
		'local-dev': {
			title: 'Complete Development Workstation',
			badge: 'Recommended Local',
			description:
				'Full local dev stack: Node.js, Go, Rust, Nginx, PHP, local SSL (mkcert), DBs, and Oh My Zsh without PowerDNS/Mail.'
		},
		'local-web': {
			title: 'Local Web Stack & Databases',
			badge: 'Local Dev Hosting',
			description: 'Nginx, PHP-FPM, Composer, mkcert HTTPS, PostgreSQL, and Redis for Laravel, WordPress, or PHP projects.'
		},
		'local-cf': {
			title: 'Local Web + Cloudflare Tunnel',
			badge: 'Lightweight + Tunnel',
			description: 'Core web components (Nginx, PHP, Composer, mkcert) with cloudflared for temporary preview/exposure without a VPS.'
		},
		'local-mobile': {
			title: 'Mobile & Cross-Platform',
			badge: 'Android & Flutter',
			description: 'Complete mobile app build environment with OpenJDK Java, Node.js, Android SDK, and Flutter.'
		},
		'edge-client': {
			title: 'Edge Node Web & Database',
			badge: 'Recommended Edge',
			description: 'Core Edge Client components: NetBird VPN to tunnel to VPS Edge Gateway, Nginx, PHP-FPM, Composer, PostgreSQL, and Redis.'
		},
		'edge-dev': {
			title: 'Edge Developer Workstation + Tunnel',
			badge: 'Dev + Tunnel',
			description: 'Complete dev environment (Node.js, Go, Rust, Web Stack, DBs, Zsh) exposed directly via NetBird to VPS Edge.'
		},
		'edge-lite': {
			title: 'Lightweight Edge Web (No DB)',
			badge: 'Minimal Edge',
			description: 'Only base dependencies, NetBird VPN, Nginx, PHP-FPM, and Composer when databases live on a separate server.'
		},
		'edge-mobile': {
			title: 'Mobile & Cross-Platform',
			badge: 'Android & Flutter',
			description: 'Mobile app build toolkit with OpenJDK Java, Node.js, Android SDK, and Flutter.'
		},
		'vps-complete': {
			title: 'Full VPS Hosting (Web + DB + DNS + Mail)',
			badge: 'Recommended VPS',
			description: 'aaPanel-style standalone VPS hosting: Nginx, PHP-FPM, Composer, PostgreSQL, Redis, PowerDNS Authoritative, and Stalwart Mail.'
		},
		'vps-hosting': {
			title: 'VPS Production (Web + Database)',
			badge: 'Web Hosting',
			description: 'Ready to serve PHP, Laravel, WordPress, Node/Go production sites and databases without local DNS & Mail Server.'
		},
		'vps-dns-mail': {
			title: 'VPS Authoritative DNS & Mail Server',
			badge: 'PowerDNS + Mail',
			description: 'Install local PowerDNS Authoritative (ports 53/8081) and Stalwart Mail Server + SnappyMail webmail on VPS.'
		},
		'vps-web-lite': {
			title: 'Lightweight VPS Web Server (Nginx + PHP)',
			badge: 'Lightweight',
			description: 'Only Nginx web server, PHP-FPM, and Composer for VPS setups using external managed databases.'
		}
	}
};
