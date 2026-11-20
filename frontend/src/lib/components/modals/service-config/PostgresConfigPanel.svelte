<script lang="ts">
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import type { PostgresConfig, PostgresCreateDBPayload, PostgresInspectResult } from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import PlusIcon from '@lucide/svelte/icons/plus';

	let {
		mode,
		config = $bindable(),
		inspectData = null,
		inspectLoading = false,
		inspectError = '',
		inspectSuccess = '',
		saving = false,
		creatingDB = false,
		formError = '',
		onVersionChange,
		onSave,
		onCancel,
		onRefreshInspect,
		onCreateDB
	}: {
		mode: 'settings' | 'inspect';
		config: PostgresConfig;
		inspectData: PostgresInspectResult | null;
		inspectLoading: boolean;
		inspectError: string;
		inspectSuccess: string;
		saving: boolean;
		creatingDB: boolean;
		formError: string;
		onVersionChange: (version: string) => void;
		onSave: (payload: Record<string, unknown>) => void;
		onCancel: () => void;
		onRefreshInspect: () => void;
		onCreateDB: (payload: PostgresCreateDBPayload) => Promise<boolean>;
	} = $props();

	let version = $state('16');
	let portStr = $state('5432');
	let listenAddresses = $state('localhost');
	let remoteMode = $state('local');
	let passwordEncryption = $state('scram-sha-256');
	let newPostgresPassword = $state('');
	let maxConnectionsStr = $state('100');
	let sharedBuffers = $state('128MB');
	let effectiveCacheSize = $state('4GB');
	let workMem = $state('4MB');
	let maintenanceWorkMem = $state('64MB');
	let timezone = $state('UTC');
	let slowLogStr = $state('-1');
	let restart = $state(true);
	let localError = $state('');

	let newDBName = $state('');
	let newDBOwner = $state('');
	let newDBPass = $state('');

	$effect(() => {
		version = config.version || '16';
		portStr = String(config.port || 5432);
		listenAddresses = config.listen_addresses || 'localhost';
		remoteMode = config.remote_access_mode || 'local';
		passwordEncryption = config.password_encryption || 'scram-sha-256';
		newPostgresPassword = '';
		maxConnectionsStr = String(config.max_connections || 100);
		sharedBuffers = config.shared_buffers || '128MB';
		effectiveCacheSize = config.effective_cache_size || '4GB';
		workMem = config.work_mem || '4MB';
		maintenanceWorkMem = config.maintenance_work_mem || '64MB';
		timezone = config.timezone || 'UTC';
		slowLogStr = String(config.log_min_duration_statement ?? -1);
		restart = true;
		localError = '';
	});

	const availableVersions = $derived(
		config.available_versions && config.available_versions.length > 0
			? config.available_versions
			: [config.version || '16']
	);

	const remoteOptions = [
		{ value: 'local', label: 'Lokal Saja (127.0.0.1 / Unix Socket)' },
		{ value: 'private_vpn', label: 'LAN & NetBird VPN (RFC1918 + 100.64.0.0/10)' },
		{ value: 'all', label: 'Semua IP (0.0.0.0/0 — Wajib Password)' }
	];

	const showListenWarning = $derived(remoteMode !== 'local' && listenAddresses.trim() === 'localhost');

	function applyPreset(preset: 'dev' | 'balanced' | 'high') {
		localError = '';
		if (preset === 'dev') {
			maxConnectionsStr = '50';
			sharedBuffers = '128MB';
			effectiveCacheSize = '512MB';
			workMem = '4MB';
			maintenanceWorkMem = '64MB';
			slowLogStr = '250';
		} else if (preset === 'balanced') {
			maxConnectionsStr = '150';
			sharedBuffers = '512MB';
			effectiveCacheSize = '2GB';
			workMem = '16MB';
			maintenanceWorkMem = '128MB';
			timezone = 'Asia/Jakarta';
			slowLogStr = '500';
		} else if (preset === 'high') {
			maxConnectionsStr = '300';
			sharedBuffers = '1GB';
			effectiveCacheSize = '4GB';
			workMem = '32MB';
			maintenanceWorkMem = '256MB';
			timezone = 'Asia/Jakarta';
			slowLogStr = '1000';
		}
	}

	function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		localError = '';
		const port = parseInt(portStr.trim(), 10);
		if (!port || port < 1 || port > 65535) {
			localError = 'Port harus berada di antara 1 sampai 65535.';
			return;
		}
		const logMin = parseInt(slowLogStr.trim(), 10);
		if (Number.isNaN(logMin)) {
			localError = 'log_min_duration_statement harus berupa angka (mis. -1 atau 500).';
			return;
		}
		onSave({
			version,
			port,
			listen_addresses: listenAddresses.trim(),
			remote_access_mode: remoteMode,
			password_encryption: passwordEncryption,
			new_postgres_password: newPostgresPassword,
			max_connections: parseInt(maxConnectionsStr.trim(), 10) || 100,
			shared_buffers: sharedBuffers.trim(),
			effective_cache_size: effectiveCacheSize.trim(),
			work_mem: workMem.trim(),
			maintenance_work_mem: maintenanceWorkMem.trim(),
			timezone: timezone.trim(),
			log_min_duration_statement: logMin,
			restart
		});
	}

	async function handleCreateDB(e: SubmitEvent) {
		e.preventDefault();
		if (!newDBName.trim()) return;
		const ok = await onCreateDB({
			version,
			database: newDBName.trim(),
			owner: newDBOwner.trim(),
			password: newDBPass
		});
		if (ok) {
			newDBName = '';
			newDBPass = '';
		}
	}
</script>

{#if mode === 'settings'}
	<form onsubmit={handleSubmit} class="flex flex-col gap-5">
		{#if localError || formError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>Validasi / Penyimpanan Gagal</Alert.Title>
				<Alert.Description>{localError || formError}</Alert.Description>
			</Alert.Root>
		{/if}

		{#if showListenWarning}
			<Alert.Root>
				<TriangleAlertIcon />
				<Alert.Title>Perhatian Listen Addresses</Alert.Title>
				<Alert.Description>
					Aturan remote diaktifkan: ubah Listen Addresses menjadi <code>*</code> agar koneksi dari luar localhost dapat diterima.
				</Alert.Description>
			</Alert.Root>
		{/if}

		<div class="flex flex-wrap items-center gap-2">
			<span class="text-xs font-medium text-muted-foreground">Preset Cepat:</span>
			<Button type="button" variant="outline" size="xs" onclick={() => applyPreset('dev')}>
				Dev Hemat RAM
			</Button>
			<Button type="button" variant="outline" size="xs" onclick={() => applyPreset('balanced')}>
				Balanced (2-4GB RAM)
			</Button>
			<Button type="button" variant="outline" size="xs" onclick={() => applyPreset('high')}>
				High Throughput
			</Button>
		</div>

		<Field.FieldSet>
			<Field.FieldLegend variant="label">Cluster, Jaringan &amp; Akses Remote</Field.FieldLegend>
			<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
				<Field.Field>
					<Field.FieldLabel for="pg-cfg-version">Versi Cluster PostgreSQL</Field.FieldLabel>
					<Select.Root
						type="single"
						value={version}
						onValueChange={(v) => {
							if (v && v !== version) onVersionChange(v);
						}}
					>
						<Select.Trigger id="pg-cfg-version" class="w-full">
							PostgreSQL {version}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								{#each availableVersions as v (v)}
									<Select.Item value={v}>PostgreSQL {v}</Select.Item>
								{/each}
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="pg-cfg-port">Port Server</Field.FieldLabel>
					<Input id="pg-cfg-port" name="port" inputmode="numeric" pattern="[0-9]+" bind:value={portStr} required />
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="pg-cfg-listen">Listen Addresses</Field.FieldLabel>
					<Input
						id="pg-cfg-listen"
						name="listen_addresses"
						placeholder="localhost"
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						bind:value={listenAddresses}
						required
					/>
					<Field.FieldDescription>localhost (aman lokal) atau * (semua interface).</Field.FieldDescription>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="pg-cfg-remote-mode">Aturan Akses (pg_hba.conf)</Field.FieldLabel>
					<Select.Root type="single" bind:value={remoteMode}>
						<Select.Trigger id="pg-cfg-remote-mode" class="w-full">
							{remoteOptions.find((o) => o.value === remoteMode)?.label ?? remoteMode}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								{#each remoteOptions as opt (opt.value)}
									<Select.Item value={opt.value}>{opt.label}</Select.Item>
								{/each}
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="pg-cfg-auth">Metode Enkripsi Password</Field.FieldLabel>
					<Select.Root type="single" bind:value={passwordEncryption}>
						<Select.Trigger id="pg-cfg-auth" class="w-full">
							{passwordEncryption === 'scram-sha-256'
								? 'scram-sha-256 (Direkomendasikan)'
								: 'md5 (Legacy Kompatibilitas)'}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="scram-sha-256">scram-sha-256 (Direkomendasikan)</Select.Item>
								<Select.Item value="md5">md5 (Legacy Kompatibilitas)</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="pg-cfg-newpass">Setel Password User postgres (Opsional)</Field.FieldLabel>
					<Input
						id="pg-cfg-newpass"
						name="new_postgres_password"
						type="password"
						autocomplete="new-password"
						placeholder="Kosongkan bila tidak diubah..."
						bind:value={newPostgresPassword}
					/>
				</Field.Field>
			</Field.FieldGroup>
		</Field.FieldSet>

		<Field.FieldSet>
			<Field.FieldLegend variant="label">Tuning Memori, Koneksi &amp; Diagnostik</Field.FieldLegend>
			<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
				<Field.Field>
					<Field.FieldLabel for="pg-cfg-maxconn">max_connections</Field.FieldLabel>
					<Input
						id="pg-cfg-maxconn"
						name="max_connections"
						inputmode="numeric"
						pattern="[0-9]+"
						bind:value={maxConnectionsStr}
						required
					/>
				</Field.Field>
				<Field.Field>
					<Field.FieldLabel for="pg-cfg-sharedbuf">shared_buffers</Field.FieldLabel>
					<Input
						id="pg-cfg-sharedbuf"
						name="shared_buffers"
						placeholder="256MB"
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						bind:value={sharedBuffers}
						required
					/>
					<Field.FieldDescription>Rekomendasi: ~25% dari total RAM sistem.</Field.FieldDescription>
				</Field.Field>
				<Field.Field>
					<Field.FieldLabel for="pg-cfg-effcache">effective_cache_size</Field.FieldLabel>
					<Input
						id="pg-cfg-effcache"
						name="effective_cache_size"
						placeholder="1GB"
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						bind:value={effectiveCacheSize}
						required
					/>
				</Field.Field>
				<Field.Field>
					<Field.FieldLabel for="pg-cfg-workmem">work_mem</Field.FieldLabel>
					<Input
						id="pg-cfg-workmem"
						name="work_mem"
						placeholder="8MB"
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						bind:value={workMem}
						required
					/>
				</Field.Field>
				<Field.Field>
					<Field.FieldLabel for="pg-cfg-maintmem">maintenance_work_mem</Field.FieldLabel>
					<Input
						id="pg-cfg-maintmem"
						name="maintenance_work_mem"
						placeholder="128MB"
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						bind:value={maintenanceWorkMem}
						required
					/>
				</Field.Field>
				<Field.Field>
					<Field.FieldLabel for="pg-cfg-timezone">Timezone</Field.FieldLabel>
					<Input
						id="pg-cfg-timezone"
						name="timezone"
						placeholder="Asia/Jakarta"
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						bind:value={timezone}
						required
					/>
				</Field.Field>
				<Field.Field class="sm:col-span-2">
					<Field.FieldLabel for="pg-cfg-slowlog">
						Log Slow Query (log_min_duration_statement ms)
					</Field.FieldLabel>
					<Input
						id="pg-cfg-slowlog"
						name="log_min_duration_statement"
						inputmode="numeric"
						bind:value={slowLogStr}
						required
					/>
					<Field.FieldDescription>
						-1 untuk nonaktif, 0 untuk semua query, atau mis. 500 (catat query &gt; 500ms).
					</Field.FieldDescription>
				</Field.Field>
			</Field.FieldGroup>
		</Field.FieldSet>

		<Separator />

		<div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
			<Field.Field orientation="horizontal" class="w-auto">
				<Checkbox id="pg-cfg-restart" bind:checked={restart} />
				<Field.FieldLabel for="pg-cfg-restart" class="font-normal">
					Restart layanan setelah simpan
				</Field.FieldLabel>
			</Field.Field>
			<div class="flex w-full flex-col-reverse gap-2 sm:w-auto sm:flex-row sm:items-center">
				<Button type="button" variant="outline" onclick={onCancel}>Batal</Button>
				<Button type="submit" disabled={saving}>
					{#if saving}
						<Spinner data-icon="inline-start" />
						Menyimpan...
					{:else}
						Simpan Konfigurasi
					{/if}
				</Button>
			</div>
		</div>
	</form>
{:else}
	<div class="flex flex-col gap-5">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h4 class="text-sm font-semibold">Buat Database &amp; User Baru</h4>
			<Button type="button" variant="outline" size="sm" disabled={inspectLoading} onclick={onRefreshInspect}>
				<RefreshCwIcon data-icon="inline-start" />
				Segarkan Data
			</Button>
		</div>

		{#if inspectError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>Operasi Gagal</Alert.Title>
				<Alert.Description>{inspectError}</Alert.Description>
			</Alert.Root>
		{/if}
		{#if inspectSuccess}
			<Alert.Root>
				<Alert.Title>Berhasil</Alert.Title>
				<Alert.Description>{inspectSuccess}</Alert.Description>
			</Alert.Root>
		{/if}

		<form onsubmit={handleCreateDB} class="rounded-lg border p-4">
			<Field.FieldSet>
				<Field.FieldLegend variant="label">Database Baru</Field.FieldLegend>
				<Field.FieldGroup class="grid gap-3 sm:grid-cols-2">
					<Field.Field>
						<Field.FieldLabel for="pg-new-db-name">Nama Database</Field.FieldLabel>
						<Input
							id="pg-new-db-name"
							placeholder="myapp_db"
							autocorrect="off"
							autocapitalize="none"
							spellcheck={false}
							bind:value={newDBName}
							required
						/>
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="pg-new-db-owner">Owner / Username (Opsional)</Field.FieldLabel>
						<Input
							id="pg-new-db-owner"
							placeholder="postgres atau myapp_user"
							autocorrect="off"
							autocapitalize="none"
							spellcheck={false}
							bind:value={newDBOwner}
						/>
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="pg-new-db-pass">Password User Baru (jika user baru)</Field.FieldLabel>
						<Input
							id="pg-new-db-pass"
							type="password"
							autocomplete="new-password"
							placeholder="Wajib bila membuat user baru..."
							bind:value={newDBPass}
						/>
					</Field.Field>
					<div class="flex items-end">
						<Button type="submit" disabled={creatingDB} class="w-full">
							{#if creatingDB}
								<Spinner data-icon="inline-start" />
								Membuat...
							{:else}
								<PlusIcon data-icon="inline-start" />
								Buat Database
							{/if}
						</Button>
					</div>
				</Field.FieldGroup>
			</Field.FieldSet>
		</form>

		<div class="flex flex-col gap-2">
			<h4 class="text-sm font-semibold">Daftar Database</h4>
			<ResponsiveTableWrapper label="Daftar Database PostgreSQL">
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>Database</Table.Head>
							<Table.Head>Owner</Table.Head>
							<Table.Head>Ukuran</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#if !inspectData?.databases || inspectData.databases.length === 0}
							<Table.Row>
								<Table.Cell colspan={3} class="py-4 text-center text-muted-foreground">
									{inspectLoading ? 'Memuat database...' : 'Tidak ada database ditemukan.'}
								</Table.Cell>
							</Table.Row>
						{:else}
							{#each inspectData.databases as db (db.name)}
								<Table.Row>
									<Table.Cell data-label="Database" class="font-mono font-semibold">{db.name}</Table.Cell>
									<Table.Cell data-label="Owner">{db.owner}</Table.Cell>
									<Table.Cell data-label="Ukuran">{db.size}</Table.Cell>
								</Table.Row>
							{/each}
						{/if}
					</Table.Body>
				</Table.Root>
			</ResponsiveTableWrapper>
		</div>

		<div class="flex flex-col gap-2">
			<h4 class="text-sm font-semibold">Roles / Users</h4>
			<div class="flex flex-wrap gap-2">
				{#if !inspectData?.roles || inspectData.roles.length === 0}
					<span class="text-xs text-muted-foreground">Tidak ada role terdeteksi.</span>
				{:else}
					{#each inspectData.roles as role (role.name)}
						<Badge variant="outline">
							{role.name}{role.superuser ? ' (superuser)' : ''}{!role.can_login ? ' [nologin]' : ''}
						</Badge>
					{/each}
				{/if}
			</div>
		</div>

		<div class="flex flex-col gap-2">
			<h4 class="text-sm font-semibold">Koneksi Aktif (pg_stat_activity)</h4>
			<ResponsiveTableWrapper label="Koneksi Aktif PostgreSQL">
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>PID</Table.Head>
							<Table.Head>DB</Table.Head>
							<Table.Head>User</Table.Head>
							<Table.Head>Client</Table.Head>
							<Table.Head>State</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#if !inspectData?.connections || inspectData.connections.length === 0}
							<Table.Row>
								<Table.Cell colspan={5} class="py-4 text-center text-muted-foreground">
									Tidak ada koneksi aktif selain sesi inspeksi.
								</Table.Cell>
							</Table.Row>
						{:else}
							{#each inspectData.connections as conn (conn.pid)}
								<Table.Row>
									<Table.Cell data-label="PID" class="font-mono">{conn.pid}</Table.Cell>
									<Table.Cell data-label="DB">{conn.database || '-'}</Table.Cell>
									<Table.Cell data-label="User">{conn.user || '-'}</Table.Cell>
									<Table.Cell data-label="Client">{conn.client_addr || 'local'}</Table.Cell>
									<Table.Cell data-label="State">{conn.state || '-'}</Table.Cell>
								</Table.Row>
							{/each}
						{/if}
					</Table.Body>
				</Table.Root>
			</ResponsiveTableWrapper>
		</div>
	</div>
{/if}
