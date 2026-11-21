<script lang="ts">
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PasswordInput from '#lib/components/shared/PasswordInput.svelte';
	import type { RedisConfig, RedisInspectResult } from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';

	let {
		mode,
		config = $bindable(),
		inspectData = null,
		inspectLoading = false,
		inspectError = '',
		flushingDB = null,
		saving = false,
		formError = '',
		onSave,
		onCancel,
		onRefreshInspect,
		onFlushDB
	}: {
		mode: 'settings' | 'inspect';
		config: RedisConfig;
		inspectData: RedisInspectResult | null;
		inspectLoading: boolean;
		inspectError: string;
		flushingDB?: number | null;
		saving: boolean;
		formError: string;
		onSave: (payload: Record<string, unknown>) => void;
		onCancel: () => void;
		onRefreshInspect: () => void;
		onFlushDB: (dbIndex: number) => void;
	} = $props();

	let portStr = $state('6379');
	let bind = $state('127.0.0.1 -::1');
	let protectedMode = $state('yes');
	let password = $state('');
	let clearPassword = $state(false);
	let maxmemory = $state('0');
	let maxmemoryPolicy = $state('noeviction');
	let persistenceMode = $state('rdb');
	let maxclientsStr = $state('10000');
	let databasesStr = $state('16');
	let restart = $state(true);
	let localError = $state('');

	$effect(() => {
		portStr = String(config.port || 6379);
		bind = config.bind || '127.0.0.1 -::1';
		protectedMode = config.protected_mode || 'yes';
		password = '';
		clearPassword = false;
		maxmemory = config.maxmemory || '0';
		maxmemoryPolicy = config.maxmemory_policy || 'noeviction';
		persistenceMode = config.persistence_mode || 'rdb';
		maxclientsStr = String(config.maxclients || 10000);
		databasesStr = String(config.databases || 16);
		restart = true;
		localError = '';
	});

	const policyOptions = [
		{ value: 'noeviction', label: 'noeviction (Cocok untuk Queue/Job)' },
		{ value: 'allkeys-lru', label: 'allkeys-lru (Cocok untuk Cache)' },
		{ value: 'volatile-lru', label: 'volatile-lru' },
		{ value: 'allkeys-lfu', label: 'allkeys-lfu' },
		{ value: 'volatile-lfu', label: 'volatile-lfu' },
		{ value: 'volatile-ttl', label: 'volatile-ttl' },
		{ value: 'allkeys-random', label: 'allkeys-random' },
		{ value: 'volatile-random', label: 'volatile-random' }
	];

	const persistenceOptions = [
		{ value: 'rdb', label: 'RDB Snapshot (Default, hemat I/O)' },
		{ value: 'aof', label: 'AOF (Append-Only File, durabilitas tinggi)' },
		{ value: 'both', label: 'RDB + AOF (Durabilitas maksimal)' },
		{ value: 'none', label: 'Nonaktif (Pure In-Memory Cache)' }
	];

	function applyPreset(preset: 'local' | 'cache' | 'queue') {
		localError = '';
		if (preset === 'local') {
			portStr = '6379';
			bind = '127.0.0.1 -::1';
			protectedMode = 'yes';
			maxmemory = '256mb';
			maxmemoryPolicy = 'noeviction';
			persistenceMode = 'rdb';
		} else if (preset === 'cache') {
			maxmemory = '256mb';
			maxmemoryPolicy = 'allkeys-lru';
			persistenceMode = 'none';
		} else if (preset === 'queue') {
			maxmemory = '512mb';
			maxmemoryPolicy = 'noeviction';
			persistenceMode = 'aof';
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
		onSave({
			port,
			bind: bind.trim(),
			protected_mode: protectedMode,
			password,
			clear_password: clearPassword,
			maxmemory: maxmemory.trim(),
			maxmemory_policy: maxmemoryPolicy,
			persistence_mode: persistenceMode,
			maxclients: parseInt(maxclientsStr.trim(), 10) || 10000,
			databases: parseInt(databasesStr.trim(), 10) || 16,
			restart
		});
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

		<div class="flex flex-wrap items-center gap-2">
			<span class="text-xs font-medium text-muted-foreground">Preset Cepat:</span>
			<Button type="button" variant="outline" size="xs" onclick={() => applyPreset('local')}>
				Dev Lokal
			</Button>
			<Button type="button" variant="outline" size="xs" onclick={() => applyPreset('cache')}>
				Cache LRU (256MB)
			</Button>
			<Button type="button" variant="outline" size="xs" onclick={() => applyPreset('queue')}>
				Queue / Laravel Horizon
			</Button>
		</div>

		<Field.FieldSet>
			<Field.FieldLegend variant="label">Jaringan &amp; Keamanan</Field.FieldLegend>
			<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
				<Field.Field>
					<Field.FieldLabel for="redis-cfg-port">Port Layanan</Field.FieldLabel>
					<Input id="redis-cfg-port" name="port" inputmode="numeric" pattern="[0-9]+" bind:value={portStr} required />
					<Field.FieldDescription>Standar Redis: 6379.</Field.FieldDescription>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="redis-cfg-protected">Protected Mode</Field.FieldLabel>
					<Select.Root type="single" bind:value={protectedMode}>
						<Select.Trigger id="redis-cfg-protected" class="w-full">
							{protectedMode === 'yes' ? 'yes (Direkomendasikan)' : 'no (Nonaktifkan proteksi remote)'}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="yes">yes (Direkomendasikan)</Select.Item>
								<Select.Item value="no">no (Nonaktifkan proteksi remote)</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
					<Field.FieldDescription>
						Jika bind 0.0.0.0, wajib set password atau atur ke no.
					</Field.FieldDescription>
				</Field.Field>

				<Field.Field class="sm:col-span-2">
					<Field.FieldLabel for="redis-cfg-bind">Bind Address</Field.FieldLabel>
					<Input
						id="redis-cfg-bind"
						name="bind"
						placeholder="127.0.0.1 -::1"
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						bind:value={bind}
						required
					/>
					<Field.FieldDescription>
						Gunakan 127.0.0.1 -::1 untuk akses lokal saja, atau 0.0.0.0 untuk akses LAN / NetBird VPN.
					</Field.FieldDescription>
				</Field.Field>

				<Field.Field class="sm:col-span-2">
					<div class="flex items-center justify-between gap-2">
						<Field.FieldLabel for="redis-cfg-password">Password Autentikasi (requirepass)</Field.FieldLabel>
						<StatusBadge
							tone={config.has_password ? 'success' : 'warning'}
							label={config.has_password ? 'Password Aktif' : 'Tanpa Password'}
						/>
					</div>
					<PasswordInput
						id="redis-cfg-password"
						name="password"
						autocomplete="new-password"
						placeholder="Isi untuk menyetel / mengganti password..."
						bind:value={password}
					/>
					<Field.FieldDescription>
						Biarkan kosong untuk mempertahankan password saat ini.
					</Field.FieldDescription>
				</Field.Field>

				<Field.Field orientation="horizontal" class="sm:col-span-2">
					<Checkbox id="redis-cfg-clear-pass" bind:checked={clearPassword} />
					<Field.FieldLabel for="redis-cfg-clear-pass" class="font-normal">
						Hapus password autentikasi (izinkan koneksi tanpa AUTH)
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>
		</Field.FieldSet>

		<Field.FieldSet>
			<Field.FieldLegend variant="label">Memori, Persistensi &amp; Batas Koneksi</Field.FieldLegend>
			<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
				<Field.Field>
					<Field.FieldLabel for="redis-cfg-maxmemory">Batas Memori (maxmemory)</Field.FieldLabel>
					<Input
						id="redis-cfg-maxmemory"
						name="maxmemory"
						placeholder="256mb"
						autocorrect="off"
						autocapitalize="none"
						spellcheck={false}
						bind:value={maxmemory}
						required
					/>
					<Field.FieldDescription>Gunakan 0 untuk tanpa batas, atau mis. 256mb, 1gb.</Field.FieldDescription>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="redis-cfg-policy">Eviction Policy (maxmemory-policy)</Field.FieldLabel>
					<Select.Root type="single" bind:value={maxmemoryPolicy}>
						<Select.Trigger id="redis-cfg-policy" class="w-full">
							{policyOptions.find((o) => o.value === maxmemoryPolicy)?.label ?? maxmemoryPolicy}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								{#each policyOptions as opt (opt.value)}
									<Select.Item value={opt.value}>{opt.label}</Select.Item>
								{/each}
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				<Field.Field class="sm:col-span-2">
					<Field.FieldLabel for="redis-cfg-persistence">Mode Persistensi Data</Field.FieldLabel>
					<Select.Root type="single" bind:value={persistenceMode}>
						<Select.Trigger id="redis-cfg-persistence" class="w-full">
							{persistenceOptions.find((o) => o.value === persistenceMode)?.label ?? persistenceMode}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								{#each persistenceOptions as opt (opt.value)}
									<Select.Item value={opt.value}>{opt.label}</Select.Item>
								{/each}
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="redis-cfg-maxclients">Maksimal Klien (maxclients)</Field.FieldLabel>
					<Input
						id="redis-cfg-maxclients"
						name="maxclients"
						inputmode="numeric"
						pattern="[0-9]+"
						bind:value={maxclientsStr}
						required
					/>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="redis-cfg-databases">Jumlah Logical DB (databases)</Field.FieldLabel>
					<Input
						id="redis-cfg-databases"
						name="databases"
						inputmode="numeric"
						pattern="[0-9]+"
						bind:value={databasesStr}
						required
					/>
				</Field.Field>
			</Field.FieldGroup>
		</Field.FieldSet>

		<Separator />

		<div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
			<Field.Field orientation="horizontal" class="w-auto">
				<Checkbox id="redis-cfg-restart" bind:checked={restart} />
				<Field.FieldLabel for="redis-cfg-restart" class="font-normal">
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
	<div class="flex flex-col gap-4">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h4 class="text-sm font-semibold">Ringkasan Runtime &amp; Memori</h4>
			<Button type="button" variant="outline" size="sm" disabled={inspectLoading} onclick={onRefreshInspect}>
				{#if inspectLoading}
					<Spinner data-icon="inline-start" />
					Menyegarkan...
				{:else}
					<RefreshCwIcon data-icon="inline-start" />
					Segarkan
				{/if}
			</Button>
		</div>

		{#if inspectError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>Inspeksi Gagal</Alert.Title>
				<Alert.Description>{inspectError}</Alert.Description>
			</Alert.Root>
		{/if}

		{#if inspectLoading && !inspectData}
			<p class="text-sm text-muted-foreground">Mengambil statistik Redis...</p>
		{:else if inspectData}
			<div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">Versi / Mode</div>
					<div class="mt-1 font-mono text-sm font-semibold">
						{inspectData.version || '-'} ({inspectData.mode || 'standalone'})
					</div>
				</div>
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">Memori Terpakai</div>
					<div class="mt-1 font-mono text-sm font-semibold">{inspectData.used_memory_human || '-'}</div>
				</div>
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">Peak / Max Memory</div>
					<div class="mt-1 font-mono text-sm font-semibold">
						{inspectData.peak_memory_human || '-'} / {inspectData.maxmemory_human || 'Unlimited'}
					</div>
				</div>
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">Klien / Uptime</div>
					<div class="mt-1 font-mono text-sm font-semibold">
						{inspectData.connected_clients || 0} klien · {Math.floor((inspectData.uptime_seconds || 0) / 60)}m
					</div>
				</div>
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">Cache Hit Rate</div>
					<div class="mt-1 font-mono text-sm font-semibold">
						{(inspectData.hit_rate_percent || 0).toFixed(1)}% ({inspectData.keyspace_hits || 0}/{inspectData.keyspace_misses || 0})
					</div>
				</div>
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">Total Perintah</div>
					<div class="mt-1 font-mono text-sm font-semibold">{inspectData.total_commands_processed || 0}</div>
				</div>
			</div>

			<h4 class="text-sm font-semibold">Keyspace per Database</h4>
			<ResponsiveTableWrapper label="Keyspace Redis per Database">
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>DB</Table.Head>
							<Table.Head>Keys</Table.Head>
							<Table.Head>Expires</Table.Head>
							<Table.Head>Avg TTL</Table.Head>
							<Table.Head>Aksi</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#if !inspectData.keyspaces || inspectData.keyspaces.length === 0}
							<Table.Row>
								<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
									Belum ada key tersimpan di database mana pun.
								</Table.Cell>
							</Table.Row>
						{:else}
							{#each inspectData.keyspaces as ks (ks.index)}
								<Table.Row>
									<Table.Cell data-label="DB" class="font-mono font-semibold">{ks.db}</Table.Cell>
									<Table.Cell data-label="Keys">{ks.keys}</Table.Cell>
									<Table.Cell data-label="Expires">{ks.expires}</Table.Cell>
									<Table.Cell data-label="Avg TTL">{ks.avg_ttl} ms</Table.Cell>
									<Table.Cell data-label="Aksi">
										<Button
											type="button"
											variant="destructive"
											size="xs"
											disabled={flushingDB === ks.index}
											onclick={() => onFlushDB(ks.index)}
										>
											{#if flushingDB === ks.index}
												<Spinner data-icon="inline-start" />
												Mengosongkan...
											{:else}
												<Trash2Icon data-icon="inline-start" />
												Flush {ks.db}
											{/if}
										</Button>
									</Table.Cell>
								</Table.Row>
							{/each}
						{/if}
					</Table.Body>
				</Table.Root>
			</ResponsiveTableWrapper>
		{/if}
	</div>
{/if}
