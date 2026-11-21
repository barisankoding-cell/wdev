<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { TaskRecord } from '#lib/types/api.js';
	import CheckIcon from '@lucide/svelte/icons/check';
	import ZapIcon from '@lucide/svelte/icons/zap';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import FileTextIcon from '@lucide/svelte/icons/file-text';

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
			description: 'Stack server web, PHP, SSL lokal, dan penyimpanan data.',
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
					description: 'Sertifikat HTTPS lokal tepercaya untuk domain virtual.'
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
					description: 'Pasang Nginx, PHP-FPM, Composer, dan SSL lokal sekaligus.'
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
					description: 'Command-line tools, platform-tools, dan build-tools.'
				},
				{
					component: 'flutter',
					label: 'Flutter SDK',
					description: 'Framework UI untuk Android, Web, dan Linux desktop.'
				}
			]
		},
		{
			title: 'Jaringan, Mail & Workspace',
			description: 'Layanan tunnel, VPN mesh, mail server, dan shell workstation.',
			items: [
				{
					component: 'mail',
					label: 'Mail Server Stack',
					description: 'Stalwart Mail Server dengan webmail SnappyMail.'
				},
				{
					component: 'cloudflare',
					label: 'Cloudflare Tunnel',
					description: 'Publikasi layanan lokal lewat tunnel terenkripsi cloudflared.'
				},
				{
					component: 'netbird',
					label: 'NetBird Mesh VPN',
					description: 'Jaringan privat WireGuard peer-to-peer antar node.'
				},
				{
					component: 'pdns',
					label: 'PowerDNS Authoritative Server',
					description:
						'Server DNS authoritative mandiri (SQLite3 + API lokal 127.0.0.1:8081) tanpa NetBird.'
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
					description: 'Orca ADE dengan antarmuka desktop grafis lengkap.'
				},
				{
					component: 'orca-headless',
					label: 'Orca Headless Server',
					description: 'Mode daemon Orca ADE tanpa desktop untuk server remote.'
				}
			]
		}
	];

	const installPresets: InstallPreset[] = [
		{
			id: 'hosting-complete',
			title: 'Hosting Lengkap (Web + DB + DNS + Mail)',
			badge: 'Standalone Penuh',
			description:
				'Web stack Nginx/PHP, PostgreSQL, Redis, PowerDNS Authoritative, dan Mail Server standalone tanpa NetBird.',
			components: ['base', 'nginx', 'php', 'composer', 'ssl', 'postgres', 'redis', 'pdns', 'mail']
		},
		{
			id: 'dns-mail',
			title: 'DNS & Mail Server Standalone',
			badge: 'PowerDNS + Mail',
			description:
				'Pasang PowerDNS Authoritative lokal dan Mail Server (Stalwart + SnappyMail) secara mandiri tanpa NetBird.',
			components: ['base', 'pdns', 'mail']
		},
		{
			id: 'hosting',
			title: 'Hanya Hosting (Web + DB)',
			badge: 'Produksi / Hosting',
			description:
				'Siap melayani situs PHP, Laravel, WordPress, dan database tanpa menginstal compiler dev berat.',
			components: ['base', 'nginx', 'php', 'composer', 'ssl', 'postgres', 'redis']
		},
		{
			id: 'hosting-web',
			title: 'Hanya Web Server (Nginx + PHP)',
			badge: 'Ringan',
			description:
				'Komponen web inti (Nginx, PHP-FPM, Composer, SSL) bagi server yang memakai database eksternal.',
			components: ['base', 'nginx', 'php', 'composer', 'ssl']
		},
		{
			id: 'development',
			title: 'Development (Web, API & Tools)',
			badge: 'Workstation Dev',
			description:
				'Stack lengkap developer: runtime Node.js, Go, Rust, web server Nginx/PHP, DB, dan Oh My Zsh.',
			components: ['base', 'node', 'go', 'rust', 'postgres', 'redis', 'nginx', 'php', 'composer', 'ssl', 'zsh']
		},
		{
			id: 'mobile',
			title: 'Mobile & Cross-Platform',
			badge: 'Android & Flutter',
			description:
				'Lingkungan build aplikasi mobile lengkap dengan OpenJDK Java, Node.js, Android SDK, dan Flutter.',
			components: ['base', 'java', 'node', 'android', 'flutter']
		}
	];

	const modularItemMap = new Map<string, ModularItem>(
		modularCategories.flatMap((cat) => cat.items.map((item) => [item.component, item]))
	);

	let selectedComponents = $state<Record<string, boolean>>({});

	const selectedList = $derived.by(() => {
		const ordered: string[] = [];
		for (const cat of modularCategories) {
			for (const item of cat.items) {
				if (selectedComponents[item.component]) {
					ordered.push(item.component);
				}
			}
		}
		return ordered;
	});

	const matchedPreset = $derived.by(() => {
		if (selectedList.length === 0) return undefined;
		return installPresets.find(
			(preset) =>
				preset.components.length === selectedList.length &&
				preset.components.every((comp) => !!selectedComponents[comp])
		);
	});

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
		selectedComponents[component] = !selectedComponents[component];
	}

	function applyPresetToModular(preset: InstallPreset) {
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
		selectedComponents = {};
	}

	function installPresetDirect(preset: InstallPreset) {
		cp.triggerBatchInstall(`Preset ${preset.title}`, buildPayloadItems(preset.components));
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

	function installSelectedModular() {
		if (selectedList.length === 0) return;
		const label = matchedPreset
			? `Preset ${matchedPreset.title}`
			: `Instalasi Modular (${selectedList.length} Komponen)`;
		cp.triggerBatchInstall(label, buildPayloadItems(selectedList));
	}

	let selectedVersions = $state<Record<string, string>>({
		node: '22',
		java: '21',
		postgres: '18',
		php: '8.4',
		zsh: 'powerlevel10k'
	});

	let tasks = $state<TaskRecord[]>([]);
	let logDialogOpen = $state(false);
	let selectedTask = $state<TaskRecord | null>(null);

	async function loadTaskHistory() {
		try {
			const res = await api.getTasks(30);
			tasks = res.tasks ?? [];
		} catch {
			tasks = [];
		}
	}

	onMount(loadTaskHistory);

	$effect(() => {
		if (cp.refreshTick > 0) loadTaskHistory();
	});

	function openTaskLog(t: TaskRecord) {
		selectedTask = t;
		logDialogOpen = true;
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div>
		<h1 class="text-xl font-bold tracking-tight sm:text-2xl">Instalasi, Komponen Modular &amp; Riwayat Tugas</h1>
		<p class="text-sm text-muted-foreground">
			Jalankan setup otomatis 14 langkah, instal komponen terpisah dengan pemilih versi, atau periksa riwayat eksekusi runner.
		</p>
	</div>

	<!-- Full Auto Setup -->
	<Card.Root>
		<Card.Header>
			<Card.Title>Instalasi Lengkap (Full Auto Setup - 14 Langkah)</Card.Title>
			<Card.Description>
				Menyiapkan seluruh lingkungan pengembangan Linux secara idempoten dalam satu alur otomatis.
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
			<Button class="w-full sm:w-auto" onclick={() => cp.triggerInstall('full-install')}>
				<ZapIcon data-icon="inline-start" />
				Jalankan Instalasi Lengkap
			</Button>
		</Card.Footer>
	</Card.Root>

	<!-- Preset & Modular Installer -->
	<Card.Root>
		<Card.Header>
			<Card.Title>Preset & Instalasi Komponen Modular</Card.Title>
			<Card.Description>
				Pilih preset siap pakai (seperti Hanya Hosting atau Development) untuk langsung dipasang, atau sesuaikan komponen satu per satu di daftar modular.
			</Card.Description>
		</Card.Header>
		<Card.Content class="flex flex-col gap-8">
			<!-- Preset Instalasi Cepat -->
			<section aria-labelledby="preset-heading" class="flex flex-col gap-3">
				<div>
					<h3 id="preset-heading" class="text-sm font-semibold text-foreground">
						Preset Instalasi Cepat
					</h3>
					<p class="text-xs text-muted-foreground">
						Pasang seluruh isi preset secara berurutan, atau muat ke daftar modular di bawah untuk meninjau dan menambah/mengurangi komponen.
					</p>
				</div>

				<div class="grid grid-cols-1 gap-3 md:grid-cols-2">
					{#each installPresets as preset (preset.id)}
						{@const isActivePreset = matchedPreset?.id === preset.id}
						<div
							class="flex flex-col justify-between gap-4 rounded-xl border-2 p-4 transition-colors {isActivePreset
								? 'border-primary/70 bg-primary/[0.03]'
								: 'border-border/80 bg-card'}"
						>
							<div class="flex flex-col gap-2">
								<div class="flex items-start justify-between gap-2">
									<h4 class="text-sm font-semibold leading-snug text-foreground">
										{preset.title}
									</h4>
									<span class="shrink-0 rounded-md border border-border bg-muted px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
										{isActivePreset ? 'Aktif di Modular' : preset.badge}
									</span>
								</div>
								<p class="text-xs leading-relaxed text-muted-foreground">
									{preset.description}
								</p>
								<div class="mt-1 flex flex-wrap gap-1.5">
									{#each preset.components as comp (comp)}
										<span class="rounded border border-border/70 bg-background px-1.5 py-0.5 text-[11px] text-foreground/85">
											{formatComponentChip(comp)}
										</span>
									{/each}
								</div>
							</div>

							<div class="flex flex-wrap items-center gap-2 border-t border-border/60 pt-3">
								<Button
									type="button"
									size="sm"
									aria-label={`Pasang langsung preset ${preset.title}`}
									onclick={() => installPresetDirect(preset)}
								>
									Pasang Preset
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
								Centang komponen yang ingin dipasang, pilih versi bila ada, lalu tekan tombol Pasang.
							{:else if matchedPreset}
								{selectedList.length} komponen terpilih sesuai preset {matchedPreset.title}.
							{:else}
								{selectedList.length} komponen dipilih dan siap dipasang berurutan dalam satu tugas.
							{/if}
						</span>
					</div>
					<div class="flex flex-wrap items-center gap-2">
						<Button
							type="button"
							variant="ghost"
							size="sm"
							disabled={selectedList.length === 0}
							onclick={clearSelection}
						>
							Bersihkan
						</Button>
						<Button
							type="button"
							size="sm"
							disabled={selectedList.length === 0}
							onclick={installSelectedModular}
						>
							{selectedList.length > 0
								? `Pasang ${selectedList.length} Komponen Terpilih`
								: 'Pasang Komponen Terpilih'}
						</Button>
					</div>
				</div>

				{#each modularCategories as cat (cat.title)}
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
								{@const isChecked = !!selectedComponents[item.component]}
								{@const inputId = `modular-item-${item.component}`}
								<div
									class="flex flex-col justify-between gap-3 rounded-lg border-2 p-3 transition-colors {isChecked
										? 'border-primary/70 bg-primary/[0.04]'
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
										<span class="flex min-w-0 flex-1 flex-col gap-0.5">
											<span class="text-sm font-medium leading-tight text-foreground">
												{item.label}
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

										<Button
											type="button"
											variant="ghost"
											size="sm"
											class="h-7 px-2.5 text-xs"
											aria-label={`Pasang ${item.label} saja`}
											onclick={() => installSingleModular(item)}
										>
											Pasang Saja
										</Button>
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
								<span class="rounded border border-border bg-background px-2 py-0.5 text-xs text-foreground">
									{formatComponentChip(comp)}
								</span>
							{/each}
						</div>
						<Button
							type="button"
							size="sm"
							onclick={installSelectedModular}
						>
							Pasang {selectedList.length} Komponen Terpilih
						</Button>
					</div>
				{/if}
			</div>
		</Card.Content>
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
			<ResponsiveTableWrapper label="Tabel Riwayat Tugas Control Plane">
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
							{#each tasks as t (t.id)}
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
