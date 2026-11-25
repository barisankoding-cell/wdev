export const layoutEn = {
	appTitle: 'wdev Control Plane',
	defaultSeoTitle: 'wdev — Server Control Plane',
	defaultSeoDescription:
		'Linux server control panel for managing web stacks, multi-version runtimes, toolchains, files, SSH terminal, DNS, and server services.',
	loadingPanel: 'Loading wdev panel...',
	skipToMain: 'Skip to main content',
	loginSeoTitle: 'Sign In — wdev Control Plane',
	loginSeoDescription: 'Sign in to the wdev server control panel to manage your infrastructure and services.',
	nav: {
		groups: {
			overview: 'Overview',
			webDev: 'Web & Development',
			systemFiles: 'System & Files',
			networkEdge: 'Network & Edge Gateway',
			domainDnsMail: 'Domain, DNS & Mail',
			optionalExpose: 'Optional Exposure',
			installSetup: 'Installation & Setup'
		},
		items: {
			dashboard: 'Status & Updates',
			webstack: 'Web Stack & VHosts',
			compose: 'Docker & Compose',
			switchers: 'Multi-Version Runtimes',
			toolchains: 'Toolchains & SDKs',
			workspace: 'Workspace & ADE',
			ports: 'Port & Network Status',
			files: 'File Manager',
			terminal: 'SSH Terminal (PTY)',
			powerdnsEdge: 'PowerDNS & Edge',
			powerdnsVps: 'Domain & DNS Manager',
			cloudflare: 'Cloudflare Tunnel',
			mailEdge: 'Mail Server & Relay',
			mailVps: 'Mail Server',
			setup: 'Setup & Components'
		},
		footer: {
			defaultHost: 'Linux Host',
			defaultSub: 'Control Plane',
			changeModeAria: 'Change Server Mode',
			modeLocalShort: 'Local',
			modeVpsShort: 'VPS',
			modeEdgeShort: 'Edge'
		}
	},
	topbar: {
		selectModeAria: 'Select Server Operational Mode',
		modePrefix: 'Mode:',
		panelDomainBtn: 'Panel Domain',
		panelDomainTitle: 'Bind Panel Domain, HTTPS Forward & IP Block',
		terminalBtn: 'Terminal',
		openTerminalAria: 'Open Live Terminal Console',
		taskRunningAria: 'Task is running',
		signedInAs: 'Signed in as',
		changeCreds: 'Change Credentials',
		panelDomainHttps: 'Panel Domain & HTTPS',
		signOut: 'Sign Out'
	},
	meta: {
		dashboard: {
			title: 'System Status & Updates',
			description: 'Monitor Linux server health, system updates, active runtimes, and daemon service status.'
		},
		webstack: {
			titleLocal: 'Web Stack & Local Domains',
			titleDefault: 'Web Stack & Virtual Hosts',
			description: 'Manage Nginx virtual hosts, PHP-FPM configurations, domains, and SSL certificates.'
		},
		compose: {
			title: 'Docker & Compose v5',
			description:
				'Manage Docker Compose v5 stacks, container status, YAML/.env editor, live logs, exec shell, and VHost exposure.'
		},
		switchers: {
			title: 'Multi-Version Runtime Manager',
			description: 'Quickly switch active Node.js, PHP CLI & FPM, PostgreSQL, and OpenJDK Java versions.'
		},
		toolchains: {
			title: 'Toolchains, Compilers & SDKs',
			description: 'Installed toolchain versions, official upstream release checks, and system PATH auditing.'
		},
		workspace: {
			title: 'Workspace, ADE & Shell',
			description: 'Manage Orca Autonomous Development Environment (ADE), Web Client access, and Oh My Zsh.'
		},
		ports: {
			title: 'Port & Network Status',
			description: 'Diagnostics for wdev service ports across Local, NetBird VPN, Firewall, and VPS IP.'
		},
		files: {
			title: 'File Manager',
			description: 'Browse, upload, download, edit, and manage server files and directories directly.'
		},
		terminal: {
			title: 'SSH Terminal (PTY)',
			description: 'Access an interactive shell terminal session (PTY) directly from your browser to manage the server.'
		},
		powerdns: {
			titleEdge: 'PowerDNS & VPS Edge Gateway',
			titleLocal: 'Domain & DNS Manager (Local)',
			titleDefault: 'Domain & DNS Manager',
			description: 'Manage PowerDNS Authoritative Server, domain zones and DNS records, and edge gateway.'
		},
		cloudflare: {
			title: 'Cloudflare Tunnel & DNS Manager',
			description: 'Manage Cloudflare multi-tunnel profiles and expose local virtual hosts to public domains.'
		},
		mail: {
			titleEdge: 'Mail Server & Outbound SMTP',
			titleLocal: 'Mail Server Standalone (Local)',
			titleDefault: 'Mail Server & Webmail',
			description: 'Manage Stalwart Mail Server, SnappyMail webmail, domain mailboxes, and outbound SMTP.'
		},
		setup: {
			title: 'Installation, Mode Presets & Components',
			description: 'Modular server component installation, operational mode presets, and task history.'
		},
		fallback: {
			title: 'wdev Dashboard'
		}
	}
};
