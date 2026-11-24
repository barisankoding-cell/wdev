<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PaginationBar from '#lib/components/shared/PaginationBar.svelte';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore, MODE_LABELS, type ServerMode } from '#lib/stores/mode.svelte.js';
	import type { SetupComponentStatus, TaskRecord } from '#lib/types/api.js';
	import CheckIcon from '@lucide/svelte/icons/check';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import ZapIcon from '@lucide/svelte/icons/zap';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import FileTextIcon from '@lucide/svelte/icons/file-text';
	import LayersIcon from '@lucide/svelte/icons/layers';

	const fullSetupSteps = [
		'1. Dependensi Dasar Sistem',
		'2. Node.js LTS (Multi-versi)',
		'3. Rust + Mold + Sccache',
		'4. Golang Stabil Terbaru',
		'5. OpenJDK Java (JDK 21)',
		'6. Android SDK & CLI Tools',
		'7. Flutter SDK Stable',
		'8. PostgreSQL Multi-versi',
		'9. Redis / Valkey Server',
		'10. Nginx Web Server',
		'11. PHP & FPM (Multi-versi)',
		'12. Composer Package Manager',
		'13. Local SSL (mkcert CA)',
		'14. Oh My Zsh & Powerlevel10k'
	];

	interface ModularItem {
		component: string;
		label: string;
		description: string;
		versions?: string[];
		defaultVersion?: string;
		modes?: ServerMode[];
	}

	interface ModularCategory {
		title: string;
		description: string;
		items: ModularItem[];
	}

	interface InstallPreset {
		id: string;
		title: string;
		badge: string;
		recommended?: boolean;
		description: string;
		components: string[];
	}

	const modularCategories: ModularCategory[] = [
		{
			title: 'Core & Build Tools',
			description: 'Runtime bahasa pemrograman dan compiler inti.',
			items: [
				{
					component: 'base',
					label: 'Dependensi Dasar',
					description: 'Paket sistem esensial, build tools, curl, dan git.'
				},
				{
					component: 'node',
					label: 'Node.js LTS',
					description: 'Runtime JavaScript/TypeScript dengan npm dan pnpm.',
					versions: ['24', '22', '20'],
					defaultVersion: '22'
				},
				{
					component: 'go',
					label: 'Golang',
					description: 'Compiler dan toolchain Go rilis stabil terbaru.'
				},
				{
					component: 'rust',
					label: 'Rust Toolchain & Mold',
					description: 'Rustup, Cargo, dan linker Mold berkecepatan tinggi.'
				},
				{
					component: 'java',
					label: 'OpenJDK Java',
					description: 'JDK LTS untuk service backend dan tooling Android.',
					versions: ['25', '21', '17'],
					defaultVersion: '21'
				}
			]
		},
		{
			title: 'Hosting Web & Database',
			description: 'Stack server web, PHP, SSL, dan penyimpanan data.',
			items: [
				{
					component: 'nginx',
					label: 'Nginx Web Server',
					description: 'Web server dan reverse proxy berkinerja tinggi.'
				},
				{
					component: 'php',
					label: 'PHP & PHP-FPM',
					description: 'Runtime PHP, ekstensi web, dan service manager PHP-FPM.',
					versions: ['8.4', '8.3', '8.2', '8.1'],
					defaultVersion: '8.4'
				},
				{
					component: 'composer',
					label: 'Composer',
					description: 'Manajer dependensi resmi untuk PHP & framework web.'
				},
				{
					component: 'ssl',
					label: 'Local SSL (mkcert)',
					description: 'Sertifikat HTTPS lokal tepercaya untuk domain dev (.test/.local).',
					modes: ['local', 'edge']
				},
				{
					component: 'postgres',
					label: 'PostgreSQL Server',
					description: 'Database relasional siap produksi dan pengembangan.',
					versions: ['18', '17', '16'],
					defaultVersion: '18'
				},
				{
					component: 'redis',
					label: 'Redis / Valkey Cache',
					description: 'Penyimpanan cache memori dan antrean kerja cepat.'
				},
				{
					component: 'php-stack',
					label: 'Bundel PHP Web Stack',
					description: 'Pasang Nginx, PHP-FPM, Composer, dan SSL lokal sekaligus.',
					modes: ['local']
				},
				{
					component: 'docker',
					label: 'Docker Engine & Compose v5',
					description: 'Container runtime Docker & plugin Compose v5 dari repositori resmi Docker.'
				}
			]
		},
		{
			title: 'Mobile & SDKs',
			description: 'Toolkit pengembangan aplikasi Android dan cross-platform.',
			items: [
				{
					component: 'android',
					label: 'Android SDK & Tools',
					description: 'Command-line tools, platform-tools, dan build-tools.',
					modes: ['local', 'edge']
				},
				{
					component: 'flutter',
					label: 'Flutter SDK',
					description: 'Framework UI untuk Android, Web, dan Linux desktop.',
					modes: ['local', 'edge']
				}
			]
		},
		{
			title: 'Jaringan, Mail & Workspace',
			description: 'Layanan tunnel, VPN mesh, DNS, mail server, dan shell.',
			items: [
				{
					component: 'netbird',
					label: 'NetBird Mesh VPN',
					description: 'Tunnel WireGuard peer-to-peer antara node lokal dan VPS Edge Gateway.',
					modes: ['edge']
				},
				{
					component: 'pdns',
					label: 'PowerDNS Authoritative Server',
					description: 'Server DNS authoritative mandiri (SQLite3 + API 127.0.0.1:8081).',
					modes: ['vps']
				},
				{
					component: 'mail',
					label: 'Mail Server Stack',
					description: 'Stalwart Mail Server dengan webmail SnappyMail.',
					modes: ['vps']
				},
				{
					component: 'cloudflare',
					label: 'Cloudflare Tunnel',
					description: 'Publikasi layanan lewat tunnel terenkripsi cloudflared.'
				},
				{
					component: 'zsh',
					label: 'Oh My Zsh',
					description: 'Terminal Zsh interaktif dengan tema produktivitas.',
					versions: ['powerlevel10k', 'agnoster', 'robbyrussell'],
					defaultVersion: 'powerlevel10k'
				},
				{
					component: 'orca-desktop',
					label: 'Orca Desktop GUI',
					description: 'Orca ADE dengan antarmuka desktop grafis lengkap.',
					modes: ['local', 'edge']
				},
				{
					component: 'orca-headless',
					label: 'Orca Headless Server',
					description: 'Mode daemon Orca ADE tanpa desktop untuk server remote.',
					modes: ['edge', 'vps']
				}
			]
		}
	];

	const presetsByMode: Record<ServerMode, InstallPreset[]> = {
		local: [
			{
				id: 'local-dev',
				title: 'Workstation Development Lengkap',
				badge: 'Rekomendasi Lokal',
				recommended: true,
				description:
					'Stack pengembangan lokal lengkap: Node.js, Go, Rust, Nginx, PHP, SSL lokal (mkcert), DB, dan Oh My Zsh tanpa PowerDNS/Mail.',
				components: ['base', 'node', 'go', 'rust', 'nginx', 'php', 'composer', 'ssl', 'postgres', 'redis', 'zsh']
			},
			{
				id: 'local-web',
				title: 'Web Stack & Database Lokal',
				badge: 'Hosting Dev Lokal',
				description:
					'Nginx, PHP-FPM, Composer, HTTPS mkcert, PostgreSQL, dan Redis untuk proyek Laravel, WordPress, maupun PHP.',
				components: ['base', 'nginx', 'php', 'composer', 'ssl', 'postgres', 'redis']
			},
			{
				id: 'local-cf',
				title: 'Web Lokal + Cloudflare Tunnel',
				badge: 'Ringan + Tunnel',
				description:
					'Komponen web inti (Nginx, PHP, Composer, mkcert) dengan cloudflared untuk preview/ekspos sementara tanpa VPS.',
				components: ['base', 'nginx', 'php', 'composer', 'ssl', 'cloudflare']
			},
			{
				id: 'local-mobile',
				title: 'Mobile & Cross-Platform',
				badge: 'Android & Flutter',
				description:
					'Lingkungan build aplikasi mobile lengkap dengan OpenJDK Java, Node.js, Android SDK, dan Flutter.',
				components: ['base', 'java', 'node', 'android', 'flutter']
			}
		],
		edge: [
			{
				id: 'edge-client',
				title: 'Edge Node Web & Database',
				badge: 'Rekomendasi Edge',
				recommended: true,
				description:
					'Komponen inti Edge Client: NetBird VPN untuk tunnel ke VPS Edge Gateway, Nginx, PHP-FPM, Composer, PostgreSQL, dan Redis.',
				components: ['base', 'netbird', 'nginx', 'php', 'composer', 'postgres', 'redis']
			},
			{
				id: 'edge-dev',
				title: 'Edge Workstation Developer + Tunnel',
				badge: 'Dev + Tunnel',
				description:
					'Lingkungan dev lengkap (Node.js, Go, Rust, Web Stack, DB, Zsh) yang langsung terekspos via NetBird ke VPS Edge.',
				components: ['base', 'netbird', 'node', 'go', 'rust', 'nginx', 'php', 'composer', 'postgres', 'redis', 'zsh']
			},
			{
				id: 'edge-lite',
				title: 'Edge Web Ringan (Tanpa DB)',
				badge: 'Minimal Edge',
				description:
					'Hanya dependensi dasar, NetBird VPN, Nginx, PHP-FPM, dan Composer bila database berada di server terpisah.',
				components: ['base', 'netbird', 'nginx', 'php', 'composer']
			},
			{
				id: 'edge-mobile',
				title: 'Mobile & Cross-Platform',
				badge: 'Android & Flutter',
				description:
					'Toolkit build aplikasi mobile dengan OpenJDK Java, Node.js, Android SDK, dan Flutter.',
				components: ['base', 'java', 'node', 'android', 'flutter']
			}
		],
		vps: [
			{
				id: 'vps-complete',
				title: 'Full VPS Hosting (Web + DB + DNS + Mail)',
				badge: 'Rekomendasi VPS',
				recommended: true,
				description:
					'Hosting VPS mandiri ala aaPanel: Nginx, PHP-FPM, Composer, PostgreSQL, Redis, PowerDNS Authoritative, dan Mail Server Stalwart.',
				components: ['base', 'nginx', 'php', 'composer', 'postgres', 'redis', 'pdns', 'mail']
			},
			{
				id: 'vps-hosting',
				title: 'Produksi VPS (Web + Database)',
				badge: 'Hosting Web',
				description:
					'Siap melayani situs produksi PHP, Laravel, WordPress, Node/Go, dan database tanpa DNS & Mail Server lokal.',
				components: ['base', 'nginx', 'php', 'composer', 'postgres', 'redis']
			},
			{
				id: 'vps-dns-mail',
				title: 'DNS Authoritative & Mail Server VPS',
				badge: 'PowerDNS + Mail',
				description:
					'Pasang PowerDNS Authoritative lokal (port 53/8081) dan Mail Server Stalwart + Webmail SnappyMail di VPS.',
				components: ['base', 'pdns', 'mail']
			},
			{
				id: 'vps-web-lite',
				title: 'Web Server VPS Ringan (Nginx + PHP)',
				badge: 'Ringan',
				description:
					'Hanya web server Nginx, PHP-FPM, dan Composer bagi VPS yang memakai managed database eksternal.',
				components: ['base', 'nginx', 'php', 'composer']
			}
		]
	};

	const modularItemMap = new Map<string, ModularItem>(
		modularCategories.flatMap((cat) => cat.items.map((item) => [item.component, item]))
	);

	let componentStatuses = $state<Record<string, SetupComponentStatus>>({});
	let statusLoading = $state(true);
	let showAllComponents = $state(false);
	let hasUserToggled = $state(false);
	let selectedComponents = $state<Record<string, boolean>>({});
	let selectedVersions = $state<Record<string, string>>({
		node: '22',
		java: '21',
		postgres: '18',
		php: '8.4',
		zsh: 'powerlevel10k'
	});

	let tasks = $state<TaskRecord[]>([]);
	const TASK_PAGE_SIZE = 10;
	let taskPage = $state(1);
	const paginatedTasks = $derived(
		tasks.slice((taskPage - 1) * TASK_PAGE_SIZE, taskPage * TASK_PAGE_SIZE)
	);
	let logDialogOpen = $state(false);
	let selectedTask = $state<TaskRecord | null>(null);

	const activeMode = $derived<ServerMode>(modeStore.mode);
	const installPresets = $derived<InstallPreset[]>(presetsByMode[activeMode] ?? presetsByMode.local);

	const hiddenCount = $derived.by(() => {
		let count = 0;
		for (const cat of modularCategories) {
			for (const item of cat.items) {
				if (item.modes && !item.modes.includes(activeMode)) {
					count++;
				}
			}
		}
		return count;
	});

	const visibleCategories = $derived.by(() =>
		modularCategories
			.map((cat) => ({
				...cat,
				items: cat.items.filter(
					(item) => showAllComponents || !item.modes || item.modes.includes(activeMode)
				)
			}))
			.filter((cat) => cat.items.length > 0)
	);

	const visibleComponentSet = $derived(
		new Set(visibleCategories.flatMap((c) => c.items.map((i) => i.component)))
	);

	const selectedList = $derived.by(() => {
		const ordered: string[] = [];
		for (const cat of modularCategories) {
			for (const item of cat.items) {
				if (selectedComponents[item.component] && visibleComponentSet.has(item.component)) {
					ordered.push(item.component);
				}
			}
		}
		return ordered;
	});

	const installedInSelection = $derived(
		selectedList.filter((comp) => Boolean(componentStatuses[comp]?.installed))
	);
	const missingInSelection = $derived(
		selectedList.filter((comp) => !componentStatuses[comp]?.installed)
	);

	const installedCountTotal = $derived(
		Array.from(visibleComponentSet).filter((comp) => Boolean(componentStatuses[comp]?.installed)).length
	);

	const matchedPreset = $derived.by(() => {
		if (selectedList.length === 0) return undefined;
		return installPresets.find(
			(preset) =>
				preset.components.length === selectedList.length &&
				preset.components.every((comp) => Boolean(selectedComponents[comp]))
		);
	});

	function isInstalled(component: string): boolean {
		return Boolean(componentStatuses[component]?.installed);
	}

	function syncVersionsFromStatuses(statuses: Record<string, SetupComponentStatus>) {
		const next = { ...selectedVersions };
		const phpVer = statuses.php?.version;
		if (phpVer && ['8.4', '8.3', '8.2', '8.1'].includes(phpVer)) {
			next.php = phpVer;
		}
		const pgVer = statuses.postgres?.version;
		if (pgVer && ['18', '17', '16'].includes(pgVer)) {
			next.postgres = pgVer;
		}
		const nodeVer = statuses.node?.version?.replace(/^v/, '').split('.')[0];
		if (nodeVer && ['24', '22', '20'].includes(nodeVer)) {
			next.node = nodeVer;
		}
		const zshVer = statuses.zsh?.version;
		if (zshVer && ['powerlevel10k', 'agnoster', 'robbyrussell'].includes(zshVer)) {
			next.zsh = zshVer;
		}
		selectedVersions = next;
	}

	function applyInstalledChecklist(resetUserFlag = false) {
		const next: Record<string, boolean> = {};
		for (const [comp, st] of Object.entries(componentStatuses)) {
			// Skip alias bundel php-stack di checklist default agar tidak duplikat saat memasang ulang.
			if (comp === 'php-stack') continue;
			if (st.installed) {
				next[comp] = true;
			}
		}
		selectedComponents = next;
		if (resetUserFlag) {
			hasUserToggled = false;
		}
	}

	function formatComponentChip(component: string): string {
		const item = modularItemMap.get(component);
		if (!item) return component;
		if (item.versions && item.versions.length > 0) {
			const ver = selectedVersions[component] ?? item.defaultVersion;
			return ver ? `${item.label} (${ver})` : item.label;
		}
		return item.label;
	}

	function buildPayloadItems(components: string[]): { component: string; version?: string }[] {
		return components.map((comp) => {
			const item = modularItemMap.get(comp);
			const ver = item?.versions?.length
				? (selectedVersions[comp] ?? item.defaultVersion)
				: undefined;
			return ver ? { component: comp, version: ver } : { component: comp };
		});
	}

	function toggleComponent(component: string) {
		hasUserToggled = true;
		selectedComponents[component] = !selectedComponents[component];
	}

	function applyPresetToModular(preset: InstallPreset) {
		hasUserToggled = true;
		const next: Record<string, boolean> = {};
		for (const comp of preset.components) {
			next[comp] = true;
		}
		selectedComponents = next;
		document.getElementById('modular-selection-anchor')?.scrollIntoView({
			behavior: 'smooth',
			block: 'start'
		});
	}

	function clearSelection() {
		hasUserToggled = true;
		selectedComponents = {};
	}

	function installPresetDirect(preset: InstallPreset) {
		const missing = preset.components.filter((comp) => !isInstalled(comp));
		const targets = missing.length > 0 ? missing : preset.components;
		const title =
			missing.length > 0 && missing.length < preset.components.length
				? `Melengkapi Preset ${preset.title}`
				: `Preset ${preset.title}`;
		cp.triggerBatchInstall(title, buildPayloadItems(targets));
	}

	function installSingleModular(item: ModularItem) {
		const ver = item.versions?.length
			? (selectedVersions[item.component] ?? item.defaultVersion)
			: undefined;
		if (item.component === 'mail') {
			cp.triggerBatchInstall(item.label, [{ component: item.component }]);
			return;
		}
		cp.triggerInstall(item.component, ver);
	}

	function installModularList(components: string[], labelPrefix?: string) {
		if (components.length === 0) return;
		const label = labelPrefix
			? labelPrefix
			: matchedPreset
				? `Preset ${matchedPreset.title}`
				: `Instalasi Modular (${components.length} Komponen)`;
		cp.triggerBatchInstall(label, buildPayloadItems(components));
	}

	async function loadComponentStatuses() {
		statusLoading = true;
		try {
			const res = await api.getSetupComponents();
			componentStatuses = res ?? {};
			syncVersionsFromStatuses(componentStatuses);
			if (!hasUserToggled) {
				applyInstalledChecklist(false);
			}
		} catch {
			// Biarkan modular tetap dapat dipakai jika deteksi gagal
		} finally {
			statusLoading = false;
		}
	}

	async function loadTaskHistory() {
		try {
			const res = await api.getTasks(30);
			tasks = res.tasks ?? [];
		} catch {
			tasks = [];
		}
	}

	async function refreshAll() {
		await Promise.all([loadComponentStatuses(), loadTaskHistory()]);
	}

	onMount(() => {
		void refreshAll();
	});

	$effect(() => {
		if (cp.refreshTick > 0) {
			void refreshAll();
		}
	});

	function openTaskLog(t: TaskRecord) {
		selectedTask = t;
		logDialogOpen = true;
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<div class="flex flex-wrap items-center gap-2">
				<h1 class="text-xl font-bold tracking-tight sm:text-2xl">
					Instalasi, Preset Mode &amp; Komponen
				</h1>
				<Badge variant="outline" class="font-medium">
					Mode: {MODE_LABELS[activeMode]}
				</Badge>
			</div>
			<p class="text-sm text-muted-foreground">
				{#if activeMode === 'vps'}
					Preset dan daftar komponen disesuaikan untuk server <strong>Full VPS</strong> (Hosting, PowerDNS, &amp; Mail Server).
				{:else if activeMode === 'edge'}
					Preset dan daftar komponen disesuaikan untuk node <strong>Edge Gateway</strong> lewat NetBird Tunnel.
				{:else}
					Preset dan daftar komponen disesuaikan untuk mesin <strong>Lokal Standalone</strong> tanpa PowerDNS maupun Mail Server.
				{/if}
			</p>
		</div>
		<div class="flex flex-wrap items-center gap-2">
			<Button variant="outline" size="sm" onclick={() => modeStore.openModal()}>
				Ganti Mode
			</Button>
			<Button variant="outline" size="sm" disabled={statusLoading} onclick={refreshAll}>
				{#if statusLoading}
					<Spinner data-icon="inline-start" />
					Memeriksa...
				{:else}
					<RefreshCwIcon data-icon="inline-start" />
					Periksa Terpasang
				{/if}
			</Button>
		</div>
	</div>

	<!-- Preset & Modular Installer (Prioritas Sesuai Mode) -->
	<Card.Root>
		<Card.Header>
			<div class="flex flex-wrap items-start justify-between gap-2">
				<div>
					<Card.Title>Preset Mode {MODE_LABELS[activeMode]} &amp; Komponen Modular</Card.Title>
					<Card.Description>
						Komponen yang sudah terpasang otomatis diberi tanda &amp; centang default. Pilih preset sesuai kebutuhan mode atau atur checklist modular secara manual.
					</Card.Description>
				</div>
				<Badge variant="secondary" class="shrink-0">
					{#if statusLoading}
						Memindai komponen...
					{:else}
						{installedCountTotal}/{visibleComponentSet.size} terpasang
					{/if}
				</Badge>
			</div>
		</Card.Header>
		<Card.Content class="flex flex-col gap-8">
			<!-- Preset Instalasi Cepat -->
			<section aria-labelledby="preset-heading" class="flex flex-col gap-3">
				<div>
					<h3 id="preset-heading" class="text-sm font-semibold text-foreground">
						Preset Siap Pakai ({MODE_LABELS[activeMode]})
					</h3>
					<p class="text-xs text-muted-foreground">
						Memasang komponen yang belum terpasang dalam preset, atau muat ke daftar modular di bawah untuk menyesuaikan komponen dan versi.
					</p>
				</div>

				<div class="grid grid-cols-1 gap-3 md:grid-cols-2">
					{#each installPresets as preset (preset.id)}
						{@const isActivePreset = matchedPreset?.id === preset.id}
						{@const installedInPreset = preset.components.filter((c) => isInstalled(c)).length}
						{@const missingInPreset = preset.components.filter((c) => !isInstalled(c))}
						{@const allPresetInstalled = missingInPreset.length === 0}
						<div
							class="flex flex-col justify-between gap-4 rounded-xl border-2 p-4 transition-colors {isActivePreset
								? 'border-primary/70 bg-primary/[0.03]'
								: preset.recommended
									? 'border-primary/35 bg-card'
									: 'border-border/80 bg-card'}"
						>
							<div class="flex flex-col gap-2">
								<div class="flex items-start justify-between gap-2">
									<h4 class="text-sm font-semibold leading-snug text-foreground">
										{preset.title}
									</h4>
									<div class="flex shrink-0 flex-wrap items-center gap-1.5">
										<span
											class="rounded-md border px-2 py-0.5 text-[11px] font-medium {allPresetInstalled
												? 'border-success/40 bg-success/10 text-success'
												: 'border-border bg-muted text-muted-foreground'}"
										>
											{allPresetInstalled
												? 'Lengkap'
												: `${installedInPreset}/${preset.components.length} terpasang`}
										</span>
										<span
											class="rounded-md border px-2 py-0.5 text-[11px] font-medium {preset.recommended
												? 'border-primary/40 bg-primary/10 text-primary'
												: 'border-border bg-muted text-muted-foreground'}"
										>
											{isActivePreset ? 'Aktif di Modular' : preset.badge}
										</span>
									</div>
								</div>
								<p class="text-xs leading-relaxed text-muted-foreground">
									{preset.description}
								</p>
								<div class="mt-1 flex flex-wrap gap-1.5">
									{#each preset.components as comp (comp)}
										{@const done = isInstalled(comp)}
										<span
											class="inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[11px] {done
												? 'border-success/40 bg-success/10 text-success'
												: 'border-border/70 bg-background text-foreground/85'}"
										>
											{#if done}
												<CheckIcon class="size-3 shrink-0" />
											{/if}
											{formatComponentChip(comp)}
										</span>
									{/each}
								</div>
							</div>

							<div class="flex flex-wrap items-center gap-2 border-t border-border/60 pt-3">
								<Button
									type="button"
									size="sm"
									variant={allPresetInstalled ? 'outline' : 'default'}
									aria-label={`Pasang preset ${preset.title}`}
									onclick={() => installPresetDirect(preset)}
								>
									{allPresetInstalled
										? 'Pasang Ulang Preset'
										: missingInPreset.length < preset.components.length
											? `Pasang (${missingInPreset.length} Belum Terpasang)`
											: 'Pasang Preset'}
								</Button>
								<Button
									type="button"
									variant="outline"
									size="sm"
									aria-label={`Pilih komponen preset ${preset.title} di daftar modular`}
									onclick={() => applyPresetToModular(preset)}
								>
									Pilih di Modular
								</Button>
							</div>
						</div>
					{/each}
				</div>
			</section>

			<!-- Pemilihan Komponen Modular -->
			<div id="modular-selection-anchor" class="flex flex-col gap-6 border-t border-border pt-6">
				<div class="flex flex-col gap-3 rounded-xl border bg-muted/35 p-3.5 sm:flex-row sm:items-center sm:justify-between">
					<div class="flex flex-col gap-0.5">
						<span class="text-sm font-semibold text-foreground">
							Daftar Komponen Modular
						</span>
						<span class="text-xs text-muted-foreground">
							{#if selectedList.length === 0}
								Centang komponen yang ingin dipasang, atau klik "Centang Terpasang" untuk menampilkan status aktif secara default.
							{:else if matchedPreset}
								{selectedList.length} terpilih sesuai preset {matchedPreset.title} ({installedInSelection.length} sudah terpasang, {missingInSelection.length} baru).
							{:else}
								{selectedList.length} terpilih ({installedInSelection.length} sudah terpasang, {missingInSelection.length} belum terpasang).
							{/if}
						</span>
					</div>
					<div class="flex flex-wrap items-center gap-2">
						{#if hiddenCount > 0}
							<Button
								type="button"
								variant="outline"
								size="sm"
								onclick={() => (showAllComponents = !showAllComponents)}
							>
								<LayersIcon data-icon="inline-start" />
								{showAllComponents ? `Filter Mode ${MODE_LABELS[activeMode]}` : `Semua Komponen (+${hiddenCount})`}
							</Button>
						{/if}
						<Button
							type="button"
							variant="outline"
							size="sm"
							onclick={() => applyInstalledChecklist(true)}
						>
							Centang Terpasang
						</Button>
						<Button
							type="button"
							variant="ghost"
							size="sm"
							disabled={selectedList.length === 0}
							onclick={clearSelection}
						>
							Bersihkan
						</Button>
						{#if missingInSelection.length > 0}
							<Button
								type="button"
								size="sm"
								onclick={() =>
									installModularList(
										missingInSelection,
										`Pasang ${missingInSelection.length} Komponen Baru`
									)}
							>
								Pasang {missingInSelection.length} Komponen Baru
							</Button>
							{#if installedInSelection.length > 0}
								<Button
									type="button"
									variant="outline"
									size="sm"
									onclick={() => installModularList(selectedList)}
								>
									Pasang Ulang Semua ({selectedList.length})
								</Button>
							{/if}
						{:else}
							<Button
								type="button"
								variant="outline"
								size="sm"
								disabled={selectedList.length === 0}
								onclick={() =>
									installModularList(
										selectedList,
										`Perbarui / Pasang Ulang (${selectedList.length} Komponen)`
									)}
							>
								{selectedList.length > 0
									? `Perbarui / Pasang Ulang (${selectedList.length})`
									: 'Pasang Komponen Terpilih'}
							</Button>
						{/if}
					</div>
				</div>

				{#each visibleCategories as cat (cat.title)}
					<fieldset class="flex flex-col gap-3 border-0 p-0">
						<legend class="mb-0.5 flex flex-wrap items-baseline gap-2">
							<span class="text-xs font-bold uppercase tracking-wider text-muted-foreground">
								{cat.title}
							</span>
							<span class="text-xs text-muted-foreground/80">
								— {cat.description}
							</span>
						</legend>
						<div class="grid grid-cols-1 gap-2.5 sm:grid-cols-2 xl:grid-cols-3">
							{#each cat.items as item (item.component)}
								{@const isChecked = Boolean(selectedComponents[item.component])}
								{@const st = componentStatuses[item.component]}
								{@const installed = Boolean(st?.installed)}
								{@const inputId = `modular-item-${item.component}`}
								<div
									class="flex flex-col justify-between gap-3 rounded-lg border-2 p-3 transition-colors {isChecked
										? 'border-primary/70 bg-primary/[0.04]'
										: installed
											? 'border-success/40 bg-success/[0.02] hover:border-success/70'
											: 'border-border/75 bg-card hover:border-border'}"
								>
									<label for={inputId} class="flex cursor-pointer items-start gap-2.5 select-none">
										<input
											id={inputId}
											type="checkbox"
											checked={isChecked}
											onchange={() => toggleComponent(item.component)}
											class="mt-0.5 size-4 shrink-0 cursor-pointer rounded border-input accent-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
										/>
										<span class="flex min-w-0 flex-1 flex-col gap-1">
											<span class="flex flex-wrap items-center justify-between gap-1.5">
												<span class="text-sm font-medium leading-tight text-foreground">
													{item.label}
												</span>
												{#if installed}
													<StatusBadge
														tone="success"
														label={st?.version ? `Terpasang · ${st.version}` : 'Terpasang'}
													/>
												{/if}
											</span>
											<span class="text-xs leading-snug text-muted-foreground">
												{item.description}
											</span>
										</span>
									</label>

									<div class="flex items-center justify-between gap-2 border-t border-border/50 pt-2">
										{#if item.versions && item.versions.length > 0}
											<Select.Root
												type="single"
												value={selectedVersions[item.component] ?? item.defaultVersion}
												onValueChange={(v) => {
													if (v) selectedVersions = { ...selectedVersions, [item.component]: v };
												}}
											>
												<Select.Trigger class="h-7 min-w-24 text-xs" aria-label={`Versi ${item.label}`}>
													Versi: {selectedVersions[item.component] ?? item.defaultVersion}
												</Select.Trigger>
												<Select.Content>
													<Select.Group>
														{#each item.versions as v (v)}
															<Select.Item value={v}>{v}</Select.Item>
														{/each}
													</Select.Group>
												</Select.Content>
											</Select.Root>
										{:else}
											<span class="text-[11px] text-muted-foreground/80">Otomatis / Terbaru</span>
										{/if}

										<div class="flex items-center gap-1">
											{#if item.component === 'docker'}
												<Button
													type="button"
													variant="ghost"
													size="sm"
													class="h-7 px-2 text-xs"
													aria-label="Konfigurasi Docker Daemon"
													onclick={() => cp.openServiceConfig('docker')}
												>
													<SettingsIcon class="size-3.5" data-icon="inline-start" />
													Konfigurasi
												</Button>
											{/if}
											<Button
												type="button"
												variant="ghost"
												size="sm"
												class="h-7 px-2.5 text-xs"
												aria-label={`${installed ? 'Pasang ulang' : 'Pasang'} ${item.label} saja`}
												onclick={() => installSingleModular(item)}
											>
												{installed ? 'Pasang Ulang' : 'Pasang Saja'}
											</Button>
										</div>
									</div>
								</div>
							{/each}
						</div>
					</fieldset>
				{/each}

				{#if selectedList.length > 0}
					<div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-primary/30 bg-primary/[0.04] p-3.5">
						<div class="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
							<span class="mr-1 text-xs font-medium text-foreground">Terpilih ({selectedList.length}):</span>
							{#each selectedList as comp (comp)}
								{@const done = isInstalled(comp)}
								<span
									class="inline-flex items-center gap-1 rounded border px-2 py-0.5 text-xs {done
										? 'border-success/40 bg-success/10 text-success'
										: 'border-border bg-background text-foreground'}"
								>
									{#if done}
										<CheckIcon class="size-3 shrink-0" />
									{/if}
									{formatComponentChip(comp)}
								</span>
							{/each}
						</div>
						{#if missingInSelection.length > 0}
							<Button
								type="button"
								size="sm"
								onclick={() =>
									installModularList(
										missingInSelection,
										`Pasang ${missingInSelection.length} Komponen Baru`
									)}
							>
								Pasang {missingInSelection.length} Komponen Baru
							</Button>
						{:else}
							<Button
								type="button"
								variant="outline"
								size="sm"
								onclick={() =>
									installModularList(
										selectedList,
										`Perbarui / Pasang Ulang (${selectedList.length} Komponen)`
									)}
							>
								Perbarui / Pasang Ulang ({selectedList.length})
							</Button>
						{/if}
					</div>
				{/if}
			</div>
		</Card.Content>
	</Card.Root>

	<!-- Full Auto Setup (Bundel Workstation 14 Langkah) -->
	<Card.Root>
		<Card.Header>
			<div class="flex flex-wrap items-center justify-between gap-2">
				<Card.Title>Instalasi Workstation Lengkap (Full Auto Setup - 14 Langkah)</Card.Title>
				{#if activeMode !== 'local'}
					<Badge variant="outline">Bundel Workstation Penuh</Badge>
				{/if}
			</div>
			<Card.Description>
				{#if activeMode === 'vps'}
					Bundel seluruh lingkungan pengembangan workstation (termasuk Android SDK, Flutter, Local SSL mkcert, &amp; Zsh). Untuk server produksi VPS disarankan memakai <strong>Preset Mode VPS</strong> di atas.
				{:else}
					Menyiapkan seluruh lingkungan pengembangan Linux secara idempoten dalam satu alur otomatis.
				{/if}
			</Card.Description>
		</Card.Header>
		<Card.Content>
			<div class="grid gap-2 text-xs text-muted-foreground sm:grid-cols-2 lg:grid-cols-3">
				{#each fullSetupSteps as step (step)}
					<div class="flex items-center gap-1.5">
						<CheckIcon class="size-3.5 shrink-0 text-success" />
						<span>{step}</span>
					</div>
				{/each}
			</div>
		</Card.Content>
		<Card.Footer>
			<Button
				variant={activeMode === 'vps' ? 'outline' : 'default'}
				class="w-full sm:w-auto"
				onclick={() => cp.triggerInstall('full-install')}
			>
				<ZapIcon data-icon="inline-start" />
				Jalankan Instalasi Lengkap
			</Button>
		</Card.Footer>
	</Card.Root>

	<!-- Task History -->
	<Card.Root>
		<Card.Header>
			<div class="flex flex-wrap items-start justify-between gap-3">
				<div>
					<Card.Title>Riwayat Eksekusi Runner</Card.Title>
					<Card.Description>Catatan tugas instalasi, switch runtime, dan perbaikan sistem terakhir.</Card.Description>
				</div>
				<Button variant="outline" size="sm" class="w-full sm:w-auto" onclick={loadTaskHistory}>
					<RefreshCwIcon data-icon="inline-start" />
					Segarkan
				</Button>
			</div>
		</Card.Header>
		<Card.Content>
			<div class="flex flex-col sm:overflow-hidden sm:rounded-lg sm:border sm:bg-card">
				<ResponsiveTableWrapper label="Tabel Riwayat Tugas Control Plane" class="rounded-none border-0 bg-transparent">
					<Table.Root>
						<Table.Header>
							<Table.Row>
								<Table.Head>Tugas</Table.Head>
								<Table.Head>Status</Table.Head>
								<Table.Head>Waktu Mulai</Table.Head>
								<Table.Head class="text-right">Log</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#if tasks.length === 0}
								<Table.Row>
									<Table.Cell colspan={4} class="py-6 text-center text-muted-foreground">
										Belum ada riwayat tugas yang terekam.
									</Table.Cell>
								</Table.Row>
							{:else}
								{#each paginatedTasks as t (t.id)}
									<Table.Row>
										<Table.Cell data-label="Tugas" class="font-medium">{t.command}</Table.Cell>
										<Table.Cell data-label="Status">
											<StatusBadge
												tone={t.status === 'success'
													? 'success'
													: t.status === 'failed'
														? 'destructive'
														: 'info'}
												label={t.status}
											/>
										</Table.Cell>
										<Table.Cell data-label="Waktu" class="font-mono text-xs text-muted-foreground">
											{new Date(t.started_at).toLocaleString('id-ID')}
										</Table.Cell>
										<Table.Cell data-label="Log" class="text-right">
											<Button variant="outline" size="xs" onclick={() => openTaskLog(t)}>
												<FileTextIcon data-icon="inline-start" />
												Lihat Log
											</Button>
										</Table.Cell>
									</Table.Row>
								{/each}
							{/if}
						</Table.Body>
					</Table.Root>
				</ResponsiveTableWrapper>
				<PaginationBar
					bind:page={taskPage}
					totalItems={tasks.length}
					pageSize={TASK_PAGE_SIZE}
					itemLabel="riwayat tugas"
					class="rounded-lg border bg-card sm:rounded-none sm:border-0 sm:border-t mt-3 sm:mt-0"
				/>
			</div>
		</Card.Content>
	</Card.Root>
</div>

<Dialog.Root bind:open={logDialogOpen}>
	<Dialog.Content class="flex max-h-[85dvh] flex-col gap-3 sm:max-w-3xl">
		<Dialog.Header>
			<Dialog.Title>{selectedTask?.command || 'Detail Log Tugas'}</Dialog.Title>
			{#if selectedTask}
				<Dialog.Description class="font-mono text-xs">
					Status: {selectedTask.status} · {new Date(selectedTask.started_at).toLocaleString('id-ID')}
				</Dialog.Description>
			{/if}
		</Dialog.Header>
		<pre
			class="max-h-[55dvh] overflow-auto rounded-lg border bg-terminal p-4 font-mono text-xs leading-relaxed text-terminal-foreground whitespace-pre-wrap break-words"
		>{selectedTask?.log_output || '(Log kosong)'}</pre>
		<Dialog.Footer>
			<Button type="button" onclick={() => (logDialogOpen = false)}>Tutup</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
