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
	import { t } from '#lib/i18n/index.svelte.js';
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

	const logDriverLabels = $derived<Record<string, string>>({
		'json-file': t('modals.serviceConfig.docker.driverStandard'),
		local: t('modals.serviceConfig.docker.driverLocal'),
		journald: 'journald',
		none: 'none'
	});

	$effect(() => {
		if (config !== lastSyncedConfigRef) {
			lastSyncedConfigRef = config;
			dnsText = (config.dns ?? []).join(', ');
			mirrorsText = (config.registry_mirrors ?? []).join(', ');
			poolSizeStr = String(config.default_address_pool_size || 24);
			logMaxFileStr = String(config.log_max_file || 3);
		}
	});

	const activePreset = $derived.by((): 'dev' | 'compact' | 'prod' | 'custom' => {
		const hasCustomNet =
			config.data_root.trim() !== '' ||
			(config.dns?.length ?? 0) > 0 ||
			(config.registry_mirrors?.length ?? 0) > 0 ||
			(config.default_address_pool_base ?? '').trim() !== '' ||
			config.ipv6;
		if (hasCustomNet) return 'custom';
		const ld = (config.log_driver || '').trim().toLowerCase();
		const ms = (config.log_max_size || '').trim().toLowerCase();
		const mf = Number.parseInt(logMaxFileStr, 10) || config.log_max_file;
		if (ld === 'json-file' && ms === '10m' && mf === 3 && config.live_restore && !config.no_new_privileges) {
			return 'dev';
		}
		if ((ld === 'local' || ld === 'json-file') && ms === '10m' && mf === 2 && config.live_restore && !config.no_new_privileges) {
			return 'compact';
		}
		if (ld === 'json-file' && ms === '50m' && mf === 5 && config.live_restore && config.no_new_privileges) {
			return 'prod';
		}
		return 'custom';
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
			<Alert.Title>{t('modals.serviceConfig.common.validationSaveFailed')}</Alert.Title>
			<Alert.Description>{formError}</Alert.Description>
		</Alert.Root>
	{/if}

	{#if !config.installed}
		<Alert.Root>
			<Alert.Title>{t('modals.serviceConfig.docker.preConfigTitle')}</Alert.Title>
			<Alert.Description class="text-xs">
				{t('modals.serviceConfig.docker.preConfigDesc', { path: config.config_path || '/etc/docker/daemon.json' })}
			</Alert.Description>
		</Alert.Root>
	{/if}

	<Field.FieldSet>
		<Field.FieldGroup>
			<Field.Field orientation="responsive">
				<Field.FieldTitle id="docker-preset-label">{t('modals.serviceConfig.common.quickPresets')}</Field.FieldTitle>
				<div class="flex flex-wrap items-center gap-2">
					<ToggleGroup.Root
						type="single"
						variant="outline"
						size="sm"
						spacing={2}
						aria-labelledby="docker-preset-label"
						value={activePreset === 'custom' ? '' : activePreset}
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
							{t('modals.serviceConfig.docker.presetDevBtn')}
						</ToggleGroup.Item>
						<ToggleGroup.Item
							value="compact"
							class="h-auto min-h-9 py-1.5 text-left text-xs leading-tight whitespace-normal sm:min-h-7 sm:py-0 sm:whitespace-nowrap"
						>
							{t('modals.serviceConfig.docker.presetCompactBtn')}
						</ToggleGroup.Item>
						<ToggleGroup.Item
							value="prod"
							class="h-auto min-h-9 py-1.5 text-left text-xs leading-tight whitespace-normal sm:min-h-7 sm:py-0 sm:whitespace-nowrap"
						>
							{t('modals.serviceConfig.docker.presetProd')}
						</ToggleGroup.Item>
					</ToggleGroup.Root>
					{#if activePreset === 'custom'}
						<Badge variant="outline">{t('modals.serviceConfig.common.customBadge')}</Badge>
					{/if}
				</div>
			</Field.Field>
		</Field.FieldGroup>
	</Field.FieldSet>

	<Field.FieldSet>
		<Field.FieldLegend variant="label">{t('modals.serviceConfig.docker.storageLegend')}</Field.FieldLegend>
		<Field.FieldGroup class="gap-4">
			<Field.Field class="min-w-0">
				<Field.FieldLabel for="docker-cfg-data-root">
					{t('modals.serviceConfig.docker.dataRootLabel')}
				</Field.FieldLabel>
				<InputGroup.Root class="h-10 sm:h-8">
					<InputGroup.Input
						id="docker-cfg-data-root"
						bind:value={config.data_root}
						placeholder={t('modals.serviceConfig.docker.dataRootPlaceholder')}
						class="font-mono"
					/>
					<InputGroup.Addon align="inline-end">
						<InputGroup.Button
							variant="outline"
							aria-label={t('modals.serviceConfig.docker.dataRootAria')}
							onclick={() => (folderPickerOpen = true)}
						>
							<FolderOpenIcon data-icon="inline-start" />
							<span class="hidden sm:inline">{t('modals.serviceConfig.docker.chooseFolder')}</span>
							<span class="sm:hidden">{t('modals.serviceConfig.docker.chooseShort')}</span>
						</InputGroup.Button>
					</InputGroup.Addon>
				</InputGroup.Root>
				<Field.FieldDescription>
					{t('modals.serviceConfig.docker.dataRootDesc')}
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
								<Select.Item value="json-file">{t('modals.serviceConfig.docker.driverStandard')}</Select.Item>
								<Select.Item value="local">{t('modals.serviceConfig.docker.driverLocal')}</Select.Item>
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
		<Field.FieldLegend variant="label">{t('modals.serviceConfig.docker.networkLegend')}</Field.FieldLegend>
		<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
			<Field.Field class="min-w-0">
				<Field.FieldLabel for="docker-cfg-dns">{t('modals.serviceConfig.docker.dnsLabel')}</Field.FieldLabel>
				<Input
					id="docker-cfg-dns"
					bind:value={dnsText}
					oninput={syncDerivedFields}
					placeholder="1.1.1.1, 8.8.8.8"
					class="h-10 font-mono sm:h-8"
				/>
				<Field.FieldDescription>{t('modals.serviceConfig.docker.dnsDesc')}</Field.FieldDescription>
			</Field.Field>

			<Field.Field class="min-w-0">
				<Field.FieldLabel for="docker-cfg-mirrors">{t('modals.serviceConfig.docker.mirrorsLabel')}</Field.FieldLabel>
				<Input
					id="docker-cfg-mirrors"
					bind:value={mirrorsText}
					oninput={syncDerivedFields}
					placeholder="https://mirror.gcr.io"
					class="h-10 font-mono sm:h-8"
				/>
			</Field.Field>

			<Field.Field class="min-w-0">
				<Field.FieldLabel for="docker-cfg-pool-base">{t('modals.serviceConfig.docker.poolBaseLabel')}</Field.FieldLabel>
				<Input
					id="docker-cfg-pool-base"
					bind:value={config.default_address_pool_base}
					placeholder="172.26.0.0/16"
					class="h-10 font-mono sm:h-8"
				/>
				<Field.FieldDescription>{t('modals.serviceConfig.docker.poolBaseDesc')}</Field.FieldDescription>
			</Field.Field>

			<Field.Field
				class="min-w-0"
				data-disabled={!(config.default_address_pool_base ?? '').trim() ? 'true' : undefined}
			>
				<Field.FieldLabel for="docker-cfg-pool-size">{t('modals.serviceConfig.docker.poolSizeLabel')}</Field.FieldLabel>
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
		<Field.FieldLegend variant="label">{t('modals.serviceConfig.docker.securityLegend')}</Field.FieldLegend>
		<Field.FieldGroup class="gap-3">
			<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
				<Checkbox id="docker-cfg-live-restore" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.live_restore} />
				<Field.FieldLabel for="docker-cfg-live-restore" class="cursor-pointer text-xs leading-snug font-normal">
					<span>{t('modals.serviceConfig.docker.liveRestoreLabel')}</span>
				</Field.FieldLabel>
			</Field.Field>

			<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
				<Checkbox id="docker-cfg-no-new-priv" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.no_new_privileges} />
				<Field.FieldLabel for="docker-cfg-no-new-priv" class="cursor-pointer text-xs leading-snug font-normal">
					<span>{t('modals.serviceConfig.docker.noNewPrivLabel')}</span>
				</Field.FieldLabel>
			</Field.Field>

			<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
				<Checkbox id="docker-cfg-userland-proxy" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.userland_proxy} />
				<Field.FieldLabel for="docker-cfg-userland-proxy" class="cursor-pointer text-xs leading-snug font-normal">
					<span>{t('modals.serviceConfig.docker.userlandProxyLabel')}</span>
				</Field.FieldLabel>
			</Field.Field>

			<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
				<Checkbox id="docker-cfg-ipv6" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.ipv6} />
				<Field.FieldLabel for="docker-cfg-ipv6" class="cursor-pointer text-xs leading-snug font-normal">
					<span>{t('modals.serviceConfig.docker.ipv6Label')}</span>
				</Field.FieldLabel>
			</Field.Field>

			<Field.Separator />

			<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
				<Field.Field orientation="horizontal" class="w-auto items-start gap-2.5 sm:items-center">
					<Checkbox id="docker-cfg-group" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={config.add_user_to_docker_group} />
					<Field.FieldLabel for="docker-cfg-group" class="cursor-pointer text-xs leading-snug font-normal">
						<span>
							{t('modals.serviceConfig.docker.addUserGroupLabel', { user: config.docker_group_user || 'non-root' })}
						</span>
					</Field.FieldLabel>
				</Field.Field>
				{#if (config.docker_group_members?.length ?? 0) > 0}
					<div class="flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
						<span>{t('modals.serviceConfig.docker.groupMembers')}</span>
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
						<span>{t('modals.serviceConfig.docker.restartAfterSave')}</span>
					</Field.FieldLabel>
				</Field.Field>
			{/if}
		</Field.FieldGroup>
	</Field.FieldSet>

	{#if showActions}
		<Separator />
		<div class="flex flex-col-reverse gap-2 sm:flex-row sm:flex-wrap sm:justify-end">
			{#if onCancel}
				<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={onCancel}>{t('common.actions.cancel')}</Button>
			{/if}
			<Button type="submit" class="min-h-10 sm:min-h-0" disabled={saving}>
				{#if saving}
					<Spinner data-icon="inline-start" />
					{t('common.actions.saving')}
				{:else}
					{config.installed ? t('modals.serviceConfig.docker.saveApplyBtn') : t('modals.serviceConfig.docker.savePreConfigBtn')}
				{/if}
			</Button>
		</div>
	{/if}
</form>

<FolderPickerDialog
	bind:open={folderPickerOpen}
	initialPath={config.data_root || '/mnt'}
	title={t('modals.serviceConfig.docker.pickerTitle')}
	description={t('modals.serviceConfig.docker.pickerDesc')}
	onSelect={(picked) => {
		config.data_root = picked;
	}}
/>
