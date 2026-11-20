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
		versions?: string[];
		defaultVersion?: string;
	}

	interface ModularCategory {
		title: string;
		items: ModularItem[];
	}

	const modularCategories: ModularCategory[] = [
		{
			title: 'Core & Build Tools',
			items: [
				{ component: 'base', label: 'Dependensi Dasar' },
				{ component: 'node', label: 'Node.js LTS', versions: ['24', '22', '20'], defaultVersion: '22' },
				{ component: 'rust', label: 'Rust Toolchain & Mold' },
				{ component: 'go', label: 'Golang' },
				{ component: 'java', label: 'OpenJDK Java', versions: ['25', '21', '17'], defaultVersion: '21' }
			]
		},
		{
			title: 'Mobile & SDKs',
			items: [
				{ component: 'android', label: 'Android SDK & Tools' },
				{ component: 'flutter', label: 'Flutter SDK' }
			]
		},
		{
			title: 'Database & Cache',
			items: [
				{ component: 'postgres', label: 'PostgreSQL Server', versions: ['18', '17', '16'], defaultVersion: '18' },
				{ component: 'redis', label: 'Redis / Valkey Cache' }
			]
		},
		{
			title: 'Web Stack & Networking',
			items: [
				{ component: 'nginx', label: 'Nginx Web Server' },
				{ component: 'php', label: 'PHP & PHP-FPM', versions: ['8.4', '8.3', '8.2', '8.1'], defaultVersion: '8.4' },
				{ component: 'composer', label: 'Composer' },
				{ component: 'ssl', label: 'Local SSL (mkcert)' },
				{ component: 'php-stack', label: 'Full PHP Stack' },
				{ component: 'cloudflare', label: 'Cloudflare Tunnel' },
				{ component: 'pdns', label: 'PowerDNS & NetBird' }
			]
		},
		{
			title: 'Workspace, ADE & Shell',
			items: [
				{
					component: 'zsh',
					label: 'Oh My Zsh',
					versions: ['powerlevel10k', 'agnoster', 'robbyrussell'],
					defaultVersion: 'powerlevel10k'
				},
				{ component: 'orca-desktop', label: 'Orca Desktop GUI' },
				{ component: 'orca-headless', label: 'Orca Headless Server' }
			]
		}
	];

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

	<!-- Modular Installer -->
	<Card.Root>
		<Card.Header>
			<Card.Title>Instalasi Komponen Modular</Card.Title>
			<Card.Description>
				Pasang atau perbarui paket sistem, runtime, database, dan toolchain secara terpisah.
			</Card.Description>
		</Card.Header>
		<Card.Content class="flex flex-col gap-6">
			{#each modularCategories as cat (cat.title)}
				<div class="flex flex-col gap-2.5">
					<h3 class="text-xs font-bold uppercase tracking-wider text-muted-foreground">
						{cat.title}
					</h3>
					<div class="grid grid-cols-1 gap-2 sm:flex sm:flex-wrap">
						{#each cat.items as item (item.component)}
							{#if item.versions && item.versions.length > 0}
								<div class="flex items-center justify-between gap-1 rounded-lg border bg-card p-1 sm:inline-flex">
									<Button
										variant="ghost"
										size="sm"
										class="flex-1 justify-start sm:flex-initial sm:justify-center"
										onclick={() => cp.triggerInstall(item.component, selectedVersions[item.component])}
									>
										{item.label}
									</Button>
									<Select.Root
										type="single"
										value={selectedVersions[item.component] ?? item.defaultVersion}
										onValueChange={(v) => {
											if (v) selectedVersions = { ...selectedVersions, [item.component]: v };
										}}
									>
										<Select.Trigger class="h-7 min-w-24 sm:min-w-20 text-xs" aria-label={`Versi ${item.label}`}>
											{selectedVersions[item.component] ?? item.defaultVersion}
										</Select.Trigger>
										<Select.Content>
											<Select.Group>
												{#each item.versions as v (v)}
													<Select.Item value={v}>{v}</Select.Item>
												{/each}
											</Select.Group>
										</Select.Content>
									</Select.Root>
								</div>
							{:else}
								<Button
									variant="outline"
									size="sm"
									class="justify-start sm:justify-center"
									onclick={() => cp.triggerInstall(item.component)}
								>
									{item.label}
								</Button>
							{/if}
						{/each}
					</div>
				</div>
			{/each}
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
