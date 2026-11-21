<script lang="ts">
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import type { PHPConfig, PHPInspectResult } from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import SearchIcon from '@lucide/svelte/icons/search';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import CpuIcon from '@lucide/svelte/icons/cpu';
	import FileCodeIcon from '@lucide/svelte/icons/file-code';
	import CheckIcon from '@lucide/svelte/icons/check';

	let {
		mode = 'settings',
		config = $bindable(),
		inspectData = null,
		inspectLoading = false,
		inspectError = '',
		saving = false,
		formError = '',
		onVersionChange,
		onSave,
		onCancel,
		onRefreshInspect
	}: {
		mode?: 'settings' | 'inspect';
		config: PHPConfig;
		inspectData?: PHPInspectResult | null;
		inspectLoading?: boolean;
		inspectError?: string;
		saving: boolean;
		formError: string;
		onVersionChange: (version: string) => void;
		onSave: (payload: Record<string, unknown>) => void;
		onCancel: () => void;
		onRefreshInspect?: () => void;
	} = $props();

	let version = $state('8.4');
	let timezone = $state('UTC');
	let memoryLimit = $state('256M');
	let uploadMaxFilesize = $state('64M');
	let postMaxSize = $state('64M');
	let maxExecutionTimeStr = $state('60');
	let maxInputVarsStr = $state('3000');
	let opcacheMemStr = $state('128');
	let displayErrors = $state(false);
	let opcacheEnable = $state(true);
	let syncCLI = $state(true);

	let pm = $state('dynamic');
	let pmMaxChildrenStr = $state('10');
	let pmStartServersStr = $state('2');
	let pmMinSpareStr = $state('1');
	let pmMaxSpareStr = $state('4');
	let pmMaxRequestsStr = $state('500');
	let restart = $state(true);
	let localError = $state('');
	let extQuery = $state('');

	$effect(() => {
		version = config.version || '8.4';
		timezone = config.timezone || 'UTC';
		memoryLimit = config.memory_limit || '256M';
		uploadMaxFilesize = config.upload_max_filesize || '64M';
		postMaxSize = config.post_max_size || '64M';
		maxExecutionTimeStr = String(config.max_execution_time ?? 60);
		maxInputVarsStr = String(config.max_input_vars || 3000);
		opcacheMemStr = String(config.opcache_memory_consumption || 128);
		displayErrors = Boolean(config.display_errors);
		opcacheEnable = Boolean(config.opcache_enable);
		syncCLI = config.sync_cli ?? true;
		pm = config.pm || 'dynamic';
		pmMaxChildrenStr = String(config.pm_max_children || 10);
		pmStartServersStr = String(config.pm_start_servers || 2);
		pmMinSpareStr = String(config.pm_min_spare_servers || 1);
		pmMaxSpareStr = String(config.pm_max_spare_servers || 4);
		pmMaxRequestsStr = String(config.pm_max_requests ?? 500);
		restart = true;
		localError = '';
	});

	const availableVersions = $derived(
		inspectData?.available_versions && inspectData.available_versions.length > 0
			? inspectData.available_versions
			: config.available_versions && config.available_versions.length > 0
				? config.available_versions
				: [config.version || '8.4']
	);

	const filteredExtensions = $derived.by(() => {
		const list = inspectData?.extensions ?? [];
		const q = extQuery.trim().toLowerCase();
		if (!q) return list;
		return list.filter((ext) => ext.toLowerCase().includes(q));
	});

	const cliVersionMismatch = $derived.by(() => {
		if (!inspectData?.default_cli_version || !inspectData?.target_version) return false;
		return !inspectData.default_cli_version.startsWith(inspectData.target_version);
	});

	const pmOptions = [
		{ value: 'dynamic', label: 'dynamic (Seimbang & Responsif)' },
		{ value: 'ondemand', label: 'ondemand (Hemat RAM)' },
		{ value: 'static', label: 'static (Trafik Tinggi Tetap)' }
	];

	function applyPreset(preset: 'dev' | 'prod') {
		localError = '';
		if (preset === 'dev') {
			timezone = 'Asia/Jakarta';
			memoryLimit = '512M';
			uploadMaxFilesize = '128M';
			postMaxSize = '128M';
			maxExecutionTimeStr = '300';
			maxInputVarsStr = '5000';
			displayErrors = true;
			opcacheEnable = true;
			opcacheMemStr = '128';
			pm = 'ondemand';
			pmMaxChildrenStr = '10';
			pmMaxRequestsStr = '500';
		} else {
			timezone = 'Asia/Jakarta';
			memoryLimit = '512M';
			uploadMaxFilesize = '64M';
			postMaxSize = '64M';
			maxExecutionTimeStr = '120';
			maxInputVarsStr = '3000';
			displayErrors = false;
			opcacheEnable = true;
			opcacheMemStr = '256';
			pm = 'dynamic';
			pmMaxChildrenStr = '20';
			pmStartServersStr = '4';
			pmMinSpareStr = '2';
			pmMaxSpareStr = '6';
			pmMaxRequestsStr = '500';
		}
	}

	function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		localError = '';
		const maxChildren = parseInt(pmMaxChildrenStr.trim(), 10);
		const startServers = parseInt(pmStartServersStr.trim(), 10) || 2;
		const minSpare = parseInt(pmMinSpareStr.trim(), 10) || 1;
		const maxSpare = parseInt(pmMaxSpareStr.trim(), 10) || 3;

		if (pm === 'dynamic') {
			if (minSpare > maxSpare || maxSpare > maxChildren || startServers < minSpare || startServers > maxSpare) {
				localError = `Untuk mode dynamic: pastikan min_spare (${minSpare}) <= start_servers (${startServers}) <= max_spare (${maxSpare}) <= max_children (${maxChildren}).`;
				return;
			}
		}

		onSave({
			version,
			timezone: timezone.trim(),
			memory_limit: memoryLimit.trim(),
			upload_max_filesize: uploadMaxFilesize.trim(),
			post_max_size: postMaxSize.trim(),
			max_execution_time: parseInt(maxExecutionTimeStr.trim(), 10) || 60,
			max_input_vars: parseInt(maxInputVarsStr.trim(), 10) || 3000,
			opcache_memory_consumption: parseInt(opcacheMemStr.trim(), 10) || 128,
			display_errors: displayErrors,
			opcache_enable: opcacheEnable,
			sync_cli: syncCLI,
			pm,
			pm_max_children: maxChildren,
			pm_start_servers: startServers,
			pm_min_spare_servers: minSpare,
			pm_max_spare_servers: maxSpare,
			pm_max_requests: parseInt(pmMaxRequestsStr.trim(), 10) || 0,
			restart
		});
	}
</script>

{#if mode === 'inspect'}
	<div class="flex flex-col gap-5">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<div class="flex flex-wrap items-center gap-2">
				<span class="text-xs font-medium text-muted-foreground">Versi PHP diperiksa:</span>
				<Select.Root
					type="single"
					value={version}
					onValueChange={(v) => {
						if (v && v !== version) onVersionChange(v);
					}}
				>
					<Select.Trigger class="h-8 min-w-28 text-xs" aria-label="Pilih versi PHP info">
						PHP {version}
					</Select.Trigger>
					<Select.Content>
						<Select.Group>
							{#each availableVersions as v (v)}
								<Select.Item value={v}>PHP {v}</Select.Item>
							{/each}
						</Select.Group>
					</Select.Content>
				</Select.Root>
			</div>

			<Button
				type="button"
				variant="outline"
				size="sm"
				disabled={inspectLoading}
				onclick={() => onRefreshInspect?.()}
			>
				{#if inspectLoading}
					<Spinner data-icon="inline-start" />
					Memeriksa...
				{:else}
					<RefreshCwIcon data-icon="inline-start" />
					Muat Ulang Info PHP
				{/if}
			</Button>
		</div>

		{#if inspectError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>Gagal Mengambil Info PHP</Alert.Title>
				<Alert.Description>{inspectError}</Alert.Description>
			</Alert.Root>
		{/if}

		{#if inspectLoading && !inspectData}
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
				<Skeleton class="h-20 w-full" />
				<Skeleton class="h-20 w-full" />
				<Skeleton class="h-20 w-full" />
				<Skeleton class="h-20 w-full" />
			</div>
			<Skeleton class="h-44 w-full" />
		{:else if inspectData}
			<!-- Ringkasan Versi & Engine -->
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
				<div class="flex flex-col justify-between gap-1 rounded-lg border bg-muted/25 p-3">
					<span class="flex items-center gap-1.5 text-xs text-muted-foreground">
						<TerminalIcon class="size-3.5 text-primary" />
						PHP CLI Target ({inspectData.target_version || version})
					</span>
					<div class="font-mono text-base font-bold text-foreground">
						{inspectData.cli_version || '-'}
					</div>
					<span class="truncate font-mono text-[11px] text-muted-foreground" title={inspectData.cli_binary}>
						{inspectData.cli_binary || '-'}
					</span>
				</div>

				<div class="flex flex-col justify-between gap-1 rounded-lg border bg-muted/25 p-3">
					<div class="flex items-center justify-between gap-1">
						<span class="flex items-center gap-1.5 text-xs text-muted-foreground">
							<TerminalIcon class="size-3.5" />
							PHP CLI Default (`php`)
						</span>
						{#if cliVersionMismatch}
							<StatusBadge tone="warning" label="Berbeda" showIcon={false} />
						{/if}
					</div>
					<div class="font-mono text-base font-bold text-foreground">
						{inspectData.default_cli_version || '-'}
					</div>
					<span class="truncate font-mono text-[11px] text-muted-foreground" title={inspectData.default_cli_binary}>
						{inspectData.default_cli_binary || 'Belum terpasang'}
					</span>
				</div>

				<div class="flex flex-col justify-between gap-1 rounded-lg border bg-muted/25 p-3">
					<span class="flex items-center gap-1.5 text-xs text-muted-foreground">
						<CpuIcon class="size-3.5 text-primary" />
						PHP-FPM &amp; Zend Engine
					</span>
					<div class="font-mono text-base font-bold text-foreground">
						{inspectData.fpm_version || inspectData.cli_version || '-'}
					</div>
					<span class="truncate font-mono text-[11px] text-muted-foreground" title={inspectData.fpm_binary || inspectData.zend_version}>
						Zend {inspectData.zend_version || '-'} · SAPI: {inspectData.sapi || 'cli'}
					</span>
				</div>

				<div class="flex flex-col justify-between gap-1 rounded-lg border bg-muted/25 p-3">
					<span class="flex items-center gap-1.5 text-xs text-muted-foreground">
						<FileCodeIcon class="size-3.5 text-primary" />
						Ekstensi &amp; Composer
					</span>
					<div class="text-base font-bold text-foreground">
						{inspectData.extensions.length} Ekstensi Aktif
					</div>
					<span class="truncate font-mono text-[11px] text-muted-foreground">
						Composer: {inspectData.composer_version || 'Belum terpasang'}
					</span>
				</div>
			</div>

			<!-- Berkas INI & Parameter Efektif CLI -->
			<div class="grid gap-4 lg:grid-cols-2">
				<div class="flex flex-col gap-3 rounded-lg border p-4">
					<h4 class="text-sm font-semibold">Berkas Konfigurasi INI &amp; Direktori</h4>
					<dl class="grid grid-cols-1 gap-2.5 text-xs">
						<div class="flex flex-col gap-0.5">
							<dt class="text-muted-foreground">Loaded Configuration File (CLI)</dt>
							<dd class="break-all rounded bg-muted/60 px-2 py-1 font-mono text-foreground">
								{inspectData.loaded_ini || '(none)'}
							</dd>
						</div>
						<div class="flex flex-col gap-0.5">
							<dt class="text-muted-foreground">Extension Directory (extension_dir)</dt>
							<dd class="break-all rounded bg-muted/60 px-2 py-1 font-mono text-foreground">
								{inspectData.extension_dir || '-'}
							</dd>
						</div>
						<div class="flex flex-col gap-0.5">
							<dt class="text-muted-foreground">Include Path</dt>
							<dd class="break-all rounded bg-muted/60 px-2 py-1 font-mono text-foreground">
								{inspectData.include_path || '-'}
							</dd>
						</div>
						<div class="flex flex-col gap-1">
							<dt class="text-muted-foreground">
								Additional .ini files parsed ({inspectData.additional_ini_files.length})
							</dt>
							{#if inspectData.additional_ini_files.length === 0}
								<dd class="text-muted-foreground">-</dd>
							{:else}
								<dd class="max-h-36 overflow-y-auto rounded border bg-muted/30 p-2 font-mono text-[11px] leading-relaxed">
									{#each inspectData.additional_ini_files as filePath (filePath)}
										<div class="break-all">{filePath}</div>
									{/each}
								</dd>
							{/if}
						</div>
					</dl>
				</div>

				<div class="flex flex-col gap-3 rounded-lg border p-4">
					<h4 class="text-sm font-semibold">Nilai Efektif PHP CLI &amp; Status Modul</h4>
					<div class="grid grid-cols-2 gap-2.5 text-xs">
						<div class="rounded border bg-muted/20 p-2.5">
							<div class="text-muted-foreground">memory_limit</div>
							<div class="mt-0.5 font-mono text-sm font-semibold">{inspectData.memory_limit || '-'}</div>
						</div>
						<div class="rounded border bg-muted/20 p-2.5">
							<div class="text-muted-foreground">max_execution_time</div>
							<div class="mt-0.5 font-mono text-sm font-semibold">{inspectData.max_execution_time || '-'}s</div>
						</div>
						<div class="rounded border bg-muted/20 p-2.5">
							<div class="text-muted-foreground">upload_max_filesize</div>
							<div class="mt-0.5 font-mono text-sm font-semibold">{inspectData.upload_max_filesize || '-'}</div>
						</div>
						<div class="rounded border bg-muted/20 p-2.5">
							<div class="text-muted-foreground">post_max_size</div>
							<div class="mt-0.5 font-mono text-sm font-semibold">{inspectData.post_max_size || '-'}</div>
						</div>
						<div class="rounded border bg-muted/20 p-2.5">
							<div class="text-muted-foreground">date.timezone</div>
							<div class="mt-0.5 font-mono text-sm font-semibold">{inspectData.timezone || '-'}</div>
						</div>
						<div class="rounded border bg-muted/20 p-2.5">
							<div class="text-muted-foreground">Zend OPcache</div>
							<div class="mt-1 flex flex-wrap gap-1.5">
								<StatusBadge
									tone={inspectData.opcache_enabled ? 'success' : 'neutral'}
									label={inspectData.opcache_enabled ? 'Aktif' : 'Nonaktif'}
								/>
								<Badge variant="outline" class="text-[11px]">
									CLI: {inspectData.opcache_cli_enabled ? 'On' : 'Off'}
								</Badge>
							</div>
						</div>
					</div>

					<div class="flex flex-col gap-1 text-xs">
						<span class="text-muted-foreground">disable_functions (CLI)</span>
						{#if inspectData.disabled_functions.length === 0}
							<span class="text-muted-foreground">Tidak ada fungsi yang diblokir (kosong).</span>
						{:else}
							<div class="flex flex-wrap gap-1.5">
								{#each inspectData.disabled_functions as fn (fn)}
									<Badge variant="secondary" class="font-mono text-[11px]">{fn}</Badge>
								{/each}
							</div>
						{/if}
					</div>
				</div>
			</div>

			<!-- Daftar Ekstensi Aktif -->
			<div class="flex flex-col gap-3 rounded-lg border p-4">
				<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
					<div>
						<h4 class="text-sm font-semibold">Ekstensi PHP &amp; Modul Zend Aktif ({inspectData.extensions.length})</h4>
						<p class="text-xs text-muted-foreground">
							Daftar seluruh modul yang termuat saat menjalankan PHP {inspectData.target_version || version}.
						</p>
					</div>
					<InputGroup.Root class="w-full sm:w-64">
						<InputGroup.Addon align="inline-start">
							<SearchIcon />
						</InputGroup.Addon>
						<InputGroup.Input
							type="search"
							placeholder="Cari ekstensi (mis. pdo, gd, intl)..."
							aria-label="Cari ekstensi PHP"
							bind:value={extQuery}
						/>
					</InputGroup.Root>
				</div>

				{#if filteredExtensions.length === 0}
					<div class="py-4 text-center text-xs text-muted-foreground">
						Tidak ada ekstensi aktif yang cocok dengan pencarian "{extQuery}".
					</div>
				{:else}
					<div class="flex flex-wrap gap-1.5">
						{#each filteredExtensions as ext (ext)}
							<span class="inline-flex items-center gap-1 rounded-md border bg-muted/40 px-2.5 py-1 font-mono text-xs text-foreground">
								<CheckIcon class="size-3 shrink-0 text-success" />
								<span>{ext}</span>
							</span>
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	</div>
{:else}
{@const phpActivePreset = (() => {
	const tz = timezone.trim();
	const mem = memoryLimit.trim().toLowerCase();
	const up = uploadMaxFilesize.trim().toLowerCase();
	const post = postMaxSize.trim().toLowerCase();
	const execTime = Number.parseInt(maxExecutionTimeStr.trim(), 10);
	const inputVars = Number.parseInt(maxInputVarsStr.trim(), 10);
	const opcacheMem = Number.parseInt(opcacheMemStr.trim(), 10);
	const pmMode = pm.trim().toLowerCase();
	const maxChildren = Number.parseInt(pmMaxChildrenStr.trim(), 10);
	const startServers = Number.parseInt(pmStartServersStr.trim(), 10);
	const minSpare = Number.parseInt(pmMinSpareStr.trim(), 10);
	const maxSpare = Number.parseInt(pmMaxSpareStr.trim(), 10);
	const maxReqs = Number.parseInt(pmMaxRequestsStr.trim(), 10);

	if (
		tz === 'Asia/Jakarta' &&
		mem === '512m' &&
		up === '128m' &&
		post === '128m' &&
		execTime === 300 &&
		inputVars === 5000 &&
		displayErrors &&
		opcacheEnable &&
		opcacheMem === 128 &&
		pmMode === 'ondemand' &&
		maxChildren === 10 &&
		maxReqs === 500
	) {
		return 'Development (Debug On)';
	}
	if (
		tz === 'Asia/Jakarta' &&
		mem === '512m' &&
		up === '64m' &&
		post === '64m' &&
		execTime === 120 &&
		inputVars === 3000 &&
		!displayErrors &&
		opcacheEnable &&
		opcacheMem === 256 &&
		pmMode === 'dynamic' &&
		maxChildren === 20 &&
		startServers === 4 &&
		minSpare === 2 &&
		maxSpare === 6 &&
		maxReqs === 500
	) {
		return 'Laravel / WordPress Optimal';
	}
	return 'Custom';
})()}
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
		<Button
			type="button"
			variant={phpActivePreset === 'Development (Debug On)' ? 'default' : 'outline'}
			size="xs"
			onclick={() => applyPreset('dev')}
		>
			Development (Debug On)
		</Button>
		<Button
			type="button"
			variant={phpActivePreset === 'Laravel / WordPress Optimal' ? 'default' : 'outline'}
			size="xs"
			onclick={() => applyPreset('prod')}
		>
			Laravel / WordPress Optimal
		</Button>
		{#if phpActivePreset === 'Custom'}
			<Badge variant="outline">Custom</Badge>
		{/if}
	</div>

	<Field.FieldSet>
		<Field.FieldLegend variant="label">Versi &amp; Pengaturan PHP.ini (99-wdev.ini)</Field.FieldLegend>
		<Field.FieldGroup class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
			<Field.Field>
				<Field.FieldLabel for="php-cfg-version">Versi PHP Target</Field.FieldLabel>
				<Select.Root
					type="single"
					value={version}
					onValueChange={(v) => {
						if (v && v !== version) onVersionChange(v);
					}}
				>
					<Select.Trigger id="php-cfg-version" class="w-full">
						PHP {version}
					</Select.Trigger>
					<Select.Content>
						<Select.Group>
							{#each availableVersions as v (v)}
								<Select.Item value={v}>PHP {v}</Select.Item>
							{/each}
						</Select.Group>
					</Select.Content>
				</Select.Root>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="php-cfg-timezone">date.timezone</Field.FieldLabel>
				<Input
					id="php-cfg-timezone"
					name="timezone"
					placeholder="Asia/Jakarta"
					autocorrect="off"
					autocapitalize="none"
					spellcheck={false}
					bind:value={timezone}
					required
				/>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="php-cfg-memlimit">memory_limit</Field.FieldLabel>
				<Input
					id="php-cfg-memlimit"
					name="memory_limit"
					placeholder="512M"
					autocorrect="off"
					autocapitalize="none"
					spellcheck={false}
					bind:value={memoryLimit}
					required
				/>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="php-cfg-upload">upload_max_filesize</Field.FieldLabel>
				<Input
					id="php-cfg-upload"
					name="upload_max_filesize"
					placeholder="64M"
					autocorrect="off"
					autocapitalize="none"
					spellcheck={false}
					bind:value={uploadMaxFilesize}
					required
				/>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="php-cfg-postmax">post_max_size</Field.FieldLabel>
				<Input
					id="php-cfg-postmax"
					name="post_max_size"
					placeholder="64M"
					autocorrect="off"
					autocapitalize="none"
					spellcheck={false}
					bind:value={postMaxSize}
					required
				/>
				<Field.FieldDescription>Disarankan &ge; upload_max_filesize.</Field.FieldDescription>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="php-cfg-maxexec">max_execution_time (detik)</Field.FieldLabel>
				<Input
					id="php-cfg-maxexec"
					name="max_execution_time"
					inputmode="numeric"
					pattern="[0-9]+"
					bind:value={maxExecutionTimeStr}
					required
				/>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="php-cfg-maxvars">max_input_vars</Field.FieldLabel>
				<Input
					id="php-cfg-maxvars"
					name="max_input_vars"
					inputmode="numeric"
					pattern="[0-9]+"
					bind:value={maxInputVarsStr}
					required
				/>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="php-cfg-opcache-mem">OPcache Memory (MB)</Field.FieldLabel>
				<Input
					id="php-cfg-opcache-mem"
					name="opcache_memory_consumption"
					inputmode="numeric"
					pattern="[0-9]+"
					bind:value={opcacheMemStr}
					required
				/>
			</Field.Field>

			<div class="flex flex-col justify-center gap-2.5 sm:col-span-2 lg:col-span-3">
				<Field.Field orientation="horizontal">
					<Checkbox id="php-cfg-display-errors" bind:checked={displayErrors} />
					<Field.FieldLabel for="php-cfg-display-errors" class="font-normal">
						Tampilkan Error (display_errors = On)
					</Field.FieldLabel>
				</Field.Field>

				<Field.Field orientation="horizontal">
					<Checkbox id="php-cfg-opcache" bind:checked={opcacheEnable} />
					<Field.FieldLabel for="php-cfg-opcache" class="font-normal">
						Aktifkan Zend OPcache
					</Field.FieldLabel>
				</Field.Field>

				<Field.Field orientation="horizontal">
					<Checkbox id="php-cfg-sync-cli" bind:checked={syncCLI} />
					<Field.FieldLabel for="php-cfg-sync-cli" class="font-normal">
						Terapkan juga pengaturan INI ke PHP CLI (Composer / Artisan)
					</Field.FieldLabel>
				</Field.Field>
			</div>
		</Field.FieldGroup>
	</Field.FieldSet>

	<Field.FieldSet>
		<Field.FieldLegend variant="label">Worker Pool PHP-FPM ([www] www.conf)</Field.FieldLegend>
		<Field.FieldGroup class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
			<Field.Field>
				<Field.FieldLabel for="php-cfg-pm">Process Manager (pm)</Field.FieldLabel>
				<Select.Root type="single" bind:value={pm}>
					<Select.Trigger id="php-cfg-pm" class="w-full">
						{pmOptions.find((o) => o.value === pm)?.label ?? pm}
					</Select.Trigger>
					<Select.Content>
						<Select.Group>
							{#each pmOptions as opt (opt.value)}
								<Select.Item value={opt.value}>{opt.label}</Select.Item>
							{/each}
						</Select.Group>
					</Select.Content>
				</Select.Root>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="php-cfg-maxchildren">pm.max_children</Field.FieldLabel>
				<Input
					id="php-cfg-maxchildren"
					name="pm_max_children"
					inputmode="numeric"
					pattern="[0-9]+"
					bind:value={pmMaxChildrenStr}
					required
				/>
			</Field.Field>

			{#if pm === 'dynamic'}
				<Field.Field>
					<Field.FieldLabel for="php-cfg-startservers">pm.start_servers</Field.FieldLabel>
					<Input
						id="php-cfg-startservers"
						name="pm_start_servers"
						inputmode="numeric"
						pattern="[0-9]+"
						bind:value={pmStartServersStr}
						required
					/>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="php-cfg-minspare">pm.min_spare_servers</Field.FieldLabel>
					<Input
						id="php-cfg-minspare"
						name="pm_min_spare_servers"
						inputmode="numeric"
						pattern="[0-9]+"
						bind:value={pmMinSpareStr}
						required
					/>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="php-cfg-maxspare">pm.max_spare_servers</Field.FieldLabel>
					<Input
						id="php-cfg-maxspare"
						name="pm_max_spare_servers"
						inputmode="numeric"
						pattern="[0-9]+"
						bind:value={pmMaxSpareStr}
						required
					/>
				</Field.Field>
			{/if}

			<Field.Field>
				<Field.FieldLabel for="php-cfg-maxreq">pm.max_requests</Field.FieldLabel>
				<Input
					id="php-cfg-maxreq"
					name="pm_max_requests"
					inputmode="numeric"
					pattern="[0-9]+"
					bind:value={pmMaxRequestsStr}
					required
				/>
				<Field.FieldDescription>Contoh: 500 untuk mencegah kebocoran memori worker.</Field.FieldDescription>
			</Field.Field>
		</Field.FieldGroup>
	</Field.FieldSet>

	<Separator />

	<div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
		<Field.Field orientation="horizontal" class="w-auto">
			<Checkbox id="php-cfg-restart" bind:checked={restart} />
			<Field.FieldLabel for="php-cfg-restart" class="font-normal">
				Uji sintaks (-t) &amp; restart PHP-FPM
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
{/if}
