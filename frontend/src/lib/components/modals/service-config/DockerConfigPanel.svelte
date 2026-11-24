<script module lang="ts">
	import type { DockerConfig } from '#lib/types/api.js';

	export function createDefaultDockerConfig(): DockerConfig {
		return {
			config_path: '/etc/docker/daemon.json',
			service_unit: 'docker',
			installed: false,
			active: false,
			data_root: '',
			log_driver: 'json-file',
			log_max_size: '10m',
			log_max_file: 3,
			live_restore: true,
			no_new_privileges: false,
			ipv6: false,
			userland_proxy: true,
			dns: [],
			registry_mirrors: [],
			default_address_pool_base: '',
			default_address_pool_size: 24,
			docker_group_user: '',
			add_user_to_docker_group: true,
			restart: true
		};
	}

	export function buildDockerConfigPayload(cfg: DockerConfig): Record<string, unknown> {
		return {
			data_root: cfg.data_root.trim(),
			log_driver: cfg.log_driver.trim() || 'json-file',
			log_max_size: cfg.log_max_size.trim() || '10m',
			log_max_file: Number(cfg.log_max_file) || 3,
			live_restore: Boolean(cfg.live_restore),
			no_new_privileges: Boolean(cfg.no_new_privileges),
			ipv6: Boolean(cfg.ipv6),
			userland_proxy: Boolean(cfg.userland_proxy),
			dns: (cfg.dns ?? []).map((s) => s.trim()).filter(Boolean),
			registry_mirrors: (cfg.registry_mirrors ?? []).map((s) => s.trim()).filter(Boolean),
			default_address_pool_base: (cfg.default_address_pool_base ?? '').trim(),
			default_address_pool_size: Number(cfg.default_address_pool_size) || 0,
			docker_group_user: (cfg.docker_group_user ?? '').trim(),
			add_user_to_docker_group: Boolean(cfg.add_user_to_docker_group),
			restart: Boolean(cfg.restart ?? true)
		};
	}
</script>

<script lang="ts">
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import FolderPickerDialog from '#lib/components/modals/FolderPickerDialog.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';

	let {
		config = $bindable(),
		saving = false,
		formError = '',
		showActions = true,
		onSave,
		onCancel
	}: {
		config: DockerConfig;
		saving?: boolean;
		formError?: string;
		showActions?: boolean;
		onSave?: (payload: Record<string, unknown>) => void;
		onCancel?: () => void;
	} = $props();

	let dnsText = $state('');
	let mirrorsText = $state('');
	let poolSizeStr = $state('24');
	let logMaxFileStr = $state('3');
	let folderPickerOpen = $state(false);
	let lastSyncedConfigRef: DockerConfig | null = null;

	const logDriverLabels: Record<string, string> = {
		'json-file': 'json-file (Standar)',
		local: 'local (Kompresi hemat disk)',
		journald: 'journald',
		none: 'none'
	};

	$effect(() => {
		if (config !== lastSyncedConfigRef) {
			lastSyncedConfigRef = config;
			dnsText = (config.dns ?? []).join(', ');
			mirrorsText = (config.registry_mirrors ?? []).join(', ');
			poolSizeStr = String(config.default_address_pool_size || 24);
			logMaxFileStr = String(config.log_max_file || 3);
		}
	});

	const activePreset = $derived.by((): { key: 'dev' | 'compact' | 'prod' | ''; label: string } => {
		const hasCustomNet =
			config.data_root.trim() !== '' ||
			(config.dns?.length ?? 0) > 0 ||
			(config.registry_mirrors?.length ?? 0) > 0 ||
			(config.default_address_pool_base ?? '').trim() !== '' ||
			config.ipv6;
		if (hasCustomNet) return { key: '', label: 'Custom' };
		const ld = (config.log_driver || '').trim().toLowerCase();
		const ms = (config.log_max_size || '').trim().toLowerCase();
		const mf = Number.parseInt(logMaxFileStr, 10) || config.log_max_file;
		if (ld === 'json-file' && ms === '10m' && mf === 3 && config.live_restore && !config.no_new_privileges) {
			return { key: 'dev', label: 'Dev Standar' };
		}
		if ((ld === 'local' || ld === 'json-file') && ms === '10m' && mf === 2 && config.live_restore && !config.no_new_privileges) {
			return { key: 'compact', label: 'Hemat Disk VPS' };
		}
		if (ld === 'json-file' && ms === '50m' && mf === 5 && config.live_restore && config.no_new_privileges) {
			return { key: 'prod', label: 'Production Hardened' };
		}
		return { key: '', label: 'Custom' };
	});

	function applyPreset(preset: 'dev' | 'compact' | 'prod') {
		config.data_root = '';
		config.dns = [];
		config.registry_mirrors = [];
		config.default_address_pool_base = '';
		config.ipv6 = false;
		dnsText = '';
		mirrorsText = '';

		if (preset === 'dev') {
			config.log_driver = 'json-file';
			config.log_max_size = '10m';
			config.log_max_file = 3;
			logMaxFileStr = '3';
			config.live_restore = true;
			config.no_new_privileges = false;
		} else if (preset === 'compact') {
			config.log_driver = 'local';
			config.log_max_size = '10m';
			config.log_max_file = 2;
			logMaxFileStr = '2';
			config.live_restore = true;
			config.no_new_privileges = false;
		} else {
			config.log_driver = 'json-file';
			config.log_max_size = '50m';
			config.log_max_file = 5;
			logMaxFileStr = '5';
			config.live_restore = true;
			config.no_new_privileges = true;
		}
	}

	function syncDerivedFields() {
		config.log_max_file = Math.max(1, Math.min(30, Number.parseInt(logMaxFileStr, 10) || 3));
		config.default_address_pool_size = Number.parseInt(poolSizeStr, 10) || 24;
		config.dns = dnsText
			.split(/[,\s]+/)
			.map((s) => s.trim())
			.filter(Boolean);
		config.registry_mirrors = mirrorsText
			.split(/[,\n]+/)
			.map((s) => s.trim())
			.filter(Boolean);
	}

	function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		syncDerivedFields();
		onSave?.(buildDockerConfigPayload(config));
	}
</script>

<form onsubmit={handleSubmit} class="flex flex-col gap-5">
	{#if formError}
		<Alert.Root variant="destructive">
			<AlertCircleIcon />
			<Alert.Title>Penyimpanan Gagal</Alert.Title>
			<Alert.Description>{formError}</Alert.Description>
		</Alert.Root>
	{/if}

	{#if !config.installed}
		<Alert.Root>
			<Alert.Title>Mode Pra-Konfigurasi (Docker Belum Terpasang)</Alert.Title>
			<Alert.Description class="text-xs">
				Pengaturan akan disimpan ke <code>{config.config_path || '/etc/docker/daemon.json'}</code> dan otomatis digunakan saat Docker Engine &amp; Compose v5 dipasang.
			</Alert.Description>
		</Alert.Root>
	{/if}

	<Field.FieldSet>
		<Field.FieldGroup>
			<Field.Field orientation="responsive">
				<Field.FieldTitle id="docker-preset-label">Preset Cepat</Field.FieldTitle>
				<div class="flex flex-wrap items-center gap-2">
					<ToggleGroup.Root
						type="single"
						variant="outline"
						size="sm"
						spacing={2}
						aria-labelledby="docker-preset-label"
						value={activePreset.key}
						onValueChange={(v) => {
							if (v === 'dev' || v === 'compact' || v === 'prod') {
								applyPreset(v);
							}
						}}
						class="flex w-full flex-wrap sm:w-auto"
					>
						<ToggleGroup.Item
							value="dev"
							class="h-auto min-h-9 py-1.5 text-left text-xs leading-tight whitespace-normal sm:min-h-7 sm:py-0 sm:whitespace-nowrap"
						>
							Dev Standar (10m × 3)
						</ToggleGroup.Item>
						<ToggleGroup.Item
							value="compact"
							class="h-auto min-h-9 py-1.5 text-left text-xs leading-tight whitespace-normal sm:min-h-7 sm:py-0 sm:whitespace-nowrap"
						>
							Hemat Disk VPS (local 10m × 2)
						</ToggleGroup.Item>
						<ToggleGroup.Item
							value="prod"
							class="h-auto min-h-9 py-1.5 text-left text-xs leading-tight whitespace-normal sm:min-h-7 sm:py-0 sm:whitespace-nowrap"
						>
							Production Hardened
						</ToggleGroup.Item>
					</ToggleGroup.Root>
					{#if activePreset.label === 'Custom'}
						<Badge variant="outline">Custom</Badge>
					{/if}
				</div>
			</Field.Field>
		</Field.FieldGroup>
	</Field.FieldSet>

	<Field.FieldSet>
		<Field.FieldLegend variant="label">Penyimpanan &amp; Rotasi Log Container</Field.FieldLegend>
		<Field.FieldGroup class="gap-4">
			<Field.Field class="min-w-0">
				<Field.FieldLabel for="docker-cfg-data-root">
					Data Root (opsional, default: /var/lib/docker)
				</Field.FieldLabel>
				<InputGroup.Root class="h-10 sm:h-8">
					<InputGroup.Input
						id="docker-cfg-data-root"
						bind:value={config.data_root}
						placeholder="/var/lib/docker atau /mnt/ssd/docker"
						class="font-mono"
					/>
					<InputGroup.Addon align="inline-end">
						<InputGroup.Button
							variant="outline"
							aria-label="Pilih folder Data Root Docker"
							onclick={() => (folderPickerOpen = true)}
						>
							<FolderOpenIcon data-icon="inline-start" />
							<span class="hidden sm:inline">Pilih Folder</span>
							<span class="sm:hidden">Pilih</span>
						</InputGroup.Button>
					</InputGroup.Addon>
				</InputGroup.Root>
				<Field.FieldDescription>
					Kosongkan untuk jalur default sistem, atau arahkan ke drive eksternal <code>/mnt/...</code> untuk image &amp; volume besar.
				</Field.FieldDescription>
			</Field.Field>

			<Field.FieldGroup class="grid gap-4 sm:grid-cols-3">
				<Field.Field class="min-w-0">
					<Field.FieldLabel for="docker-cfg-log-driver">Log Driver</Field.FieldLabel>
					<Select.Root
						type="single"
						value={config.log_driver || 'json-file'}
						onValueChange={(v) => {
							if (v) config.log_driver = v;
						}}
					>
						<Select.Trigger id="docker-cfg-log-driver" class="h-10 w-full min-w-0 sm:h-8">
							<span class="truncate">
								{logDriverLabels[config.log_driver || 'json-file'] ?? (config.log_driver || 'json-file')}
							</span>
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="json-file">json-file (Standar)</Select.Item>
								<Select.Item value="local">local (Kompresi hemat disk)</Select.Item>
								<Select.Item value="journald">journald</Select.Item>
								<Select.Item value="none">none</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				<Field.Field
					class="min-w-0"
					data-disabled={config.log_driver !== 'json-file' && config.log_driver !== 'local' ? 'true' : undefined}
				>
					<Field.FieldLabel for="docker-cfg-log-size">log max-size</Field.FieldLabel>
					<Input
						id="docker-cfg-log-size"
						bind:value={config.log_max_size}
						placeholder="10m"
						class="h-10 font-mono sm:h-8"
						disabled={config.log_driver !== 'json-file' && config.log_driver !== 'local'}
					/>
				</Field.Field>

				<Field.Field
					class="min-w-0"
					data-disabled={config.log_driver !== 'json-file' && config.log_driver !== 'local' ? 'true' : undefined}
				>
					<Field.FieldLabel for="docker-cfg-log-file">log max-file</Field.FieldLabel>
					<Input
						id="docker-cfg-log-file"
						type="number"
						min={1}
						max={30}
						bind:value={logMaxFileStr}
						oninput={syncDerivedFields}
						class="h-10 font-mono sm:h-8"
						disabled={config.log_driver !== 'json-file' && config.log_driver !== 'local'}
					/>
				</Field.Field>
			</Field.FieldGroup>
		</Field.FieldGroup>
	</Field.FieldSet>

	<Field.FieldSet>
		<Field.FieldLegend variant="label">Jaringan, DNS &amp; Registry</Field.FieldLegend>
		<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
			<Field.Field class="min-w-0">
				<Field.FieldLabel for="docker-cfg-dns">DNS Container (opsional)</Field.FieldLabel>
				<Input
					id="docker-cfg-dns"
					bind:value={dnsText}
					oninput={syncDerivedFields}
					placeholder="1.1.1.1, 8.8.8.8"
					class="h-10 font-mono sm:h-8"
				/>
				<Field.FieldDescription>Pisahkan dengan koma bila container butuh resolver kustom.</Field.FieldDescription>
			</Field.Field>

			<Field.Field class="min-w-0">
				<Field.FieldLabel for="docker-cfg-mirrors">Registry Mirrors (opsional)</Field.FieldLabel>
				<Input
					id="docker-cfg-mirrors"
					bind:value={mirrorsText}
					oninput={syncDerivedFields}
					placeholder="https://mirror.gcr.io"
					class="h-10 font-mono sm:h-8"
				/>
			</Field.Field>

			<Field.Field class="min-w-0">
				<Field.FieldLabel for="docker-cfg-pool-base">Default Address Pool CIDR (opsional)</Field.FieldLabel>
				<Input
					id="docker-cfg-pool-base"
					bind:value={config.default_address_pool_base}
					placeholder="172.26.0.0/16"
					class="h-10 font-mono sm:h-8"
				/>
				<Field.FieldDescription>Berguna agar subnet bridge Docker tidak bentrok dengan VPN/LAN.</Field.FieldDescription>
			</Field.Field>

			<Field.Field
				class="min-w-0"
				data-disabled={!(config.default_address_pool_base ?? '').trim() ? 'true' : undefined}
			>
				<Field.FieldLabel for="docker-cfg-pool-size">Ukuran Subnet Pool (/prefix)</Field.FieldLabel>
				<Input
					id="docker-cfg-pool-size"
					type="number"
					min={16}
					max={29}
					bind:value={poolSizeStr}
					oninput={syncDerivedFields}
					class="h-10 font-mono sm:h-8"
					disabled={!(config.default_address_pool_base ?? '').trim()}
				/>
			</Field.Field>
		</Field.FieldGroup>
	</Field.FieldSet>

	<Field.FieldSet>
		<Field.FieldLegend variant="label">Keamanan, Daemon &amp; Akses User</Field.FieldLegend>
		<Field.FieldGroup class="gap-3">
			<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
				<Checkbox id="docker-cfg-live-restore" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.live_restore} />
				<Field.FieldLabel for="docker-cfg-live-restore" class="cursor-pointer text-xs leading-snug font-normal">
					<span>
						Aktifkan <code>live-restore</code> (container tetap hidup saat daemon Docker restart/diperbarui)
					</span>
				</Field.FieldLabel>
			</Field.Field>

			<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
				<Checkbox id="docker-cfg-no-new-priv" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.no_new_privileges} />
				<Field.FieldLabel for="docker-cfg-no-new-priv" class="cursor-pointer text-xs leading-snug font-normal">
					<span>
						Aktifkan <code>no-new-privileges</code> default pada container (hardening produksi)
					</span>
				</Field.FieldLabel>
			</Field.Field>

			<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
				<Checkbox id="docker-cfg-userland-proxy" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.userland_proxy} />
				<Field.FieldLabel for="docker-cfg-userland-proxy" class="cursor-pointer text-xs leading-snug font-normal">
					<span>Aktifkan <code>userland-proxy</code> untuk port forwarding Docker</span>
				</Field.FieldLabel>
			</Field.Field>

			<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
				<Checkbox id="docker-cfg-ipv6" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.ipv6} />
				<Field.FieldLabel for="docker-cfg-ipv6" class="cursor-pointer text-xs leading-snug font-normal">
					<span>Aktifkan dukungan IPv6 pada daemon Docker</span>
				</Field.FieldLabel>
			</Field.Field>

			<Field.Separator />

			<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
				<Field.Field orientation="horizontal" class="w-auto items-start gap-2.5 sm:items-center">
					<Checkbox id="docker-cfg-group" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.add_user_to_docker_group} />
					<Field.FieldLabel for="docker-cfg-group" class="cursor-pointer text-xs leading-snug font-normal">
						<span>
							Tambahkan user <code class="font-semibold">{config.docker_group_user || 'non-root'}</code> ke grup <code>docker</code>
						</span>
					</Field.FieldLabel>
				</Field.Field>
				{#if (config.docker_group_members?.length ?? 0) > 0}
					<div class="flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
						<span>Anggota grup:</span>
						{#each config.docker_group_members ?? [] as member (member)}
							<Badge variant="secondary" class="font-mono">{member}</Badge>
						{/each}
					</div>
				{/if}
			</div>

			{#if config.installed && showActions}
				<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
					<Checkbox id="docker-cfg-restart" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.restart} />
					<Field.FieldLabel for="docker-cfg-restart" class="cursor-pointer text-xs leading-snug font-medium">
						<span>Restart <code>docker.service</code> setelah menyimpan konfigurasi</span>
					</Field.FieldLabel>
				</Field.Field>
			{/if}
		</Field.FieldGroup>
	</Field.FieldSet>

	{#if showActions}
		<Separator />
		<div class="flex flex-col-reverse gap-2 sm:flex-row sm:flex-wrap sm:justify-end">
			{#if onCancel}
				<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={onCancel}>Batal</Button>
			{/if}
			<Button type="submit" class="min-h-10 sm:min-h-0" disabled={saving}>
				{#if saving}
					<Spinner data-icon="inline-start" />
					Menyimpan...
				{:else}
					{config.installed ? 'Simpan & Terapkan' : 'Simpan Pra-Konfigurasi'}
				{/if}
			</Button>
		</div>
	{/if}
</form>

<FolderPickerDialog
	bind:open={folderPickerOpen}
	initialPath={config.data_root || '/mnt'}
	title="Pilih Direktori Data Root Docker"
	description="Pilih atau buat direktori penyimpanan image, volume, dan container Docker."
	onSelect={(picked) => {
		config.data_root = picked;
	}}
/>
