<script lang="ts">
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import type { PHPConfig } from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';

	let {
		config = $bindable(),
		saving = false,
		formError = '',
		onVersionChange,
		onSave,
		onCancel
	}: {
		config: PHPConfig;
		saving: boolean;
		formError: string;
		onVersionChange: (version: string) => void;
		onSave: (payload: Record<string, unknown>) => void;
		onCancel: () => void;
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
		config.available_versions && config.available_versions.length > 0
			? config.available_versions
			: [config.version || '8.4']
	);

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
		<Button type="button" variant="outline" size="xs" onclick={() => applyPreset('dev')}>
			Development (Debug On)
		</Button>
		<Button type="button" variant="outline" size="xs" onclick={() => applyPreset('prod')}>
			Laravel / WordPress Optimal
		</Button>
	</div>

	<Field.FieldSet>
		<Field.FieldLegend variant="label">Versi &amp; Pengaturan PHP.ini (99-wdev.ini)</Field.FieldLegend>
		<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
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

			<Field.Field orientation="horizontal" class="sm:col-span-2">
				<Checkbox id="php-cfg-sync-cli" bind:checked={syncCLI} />
				<Field.FieldLabel for="php-cfg-sync-cli" class="font-normal">
					Terapkan juga pengaturan INI ke PHP CLI (Composer / Artisan)
				</Field.FieldLabel>
			</Field.Field>
		</Field.FieldGroup>
	</Field.FieldSet>

	<Field.FieldSet>
		<Field.FieldLegend variant="label">Worker Pool PHP-FPM ([www] www.conf)</Field.FieldLegend>
		<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
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
