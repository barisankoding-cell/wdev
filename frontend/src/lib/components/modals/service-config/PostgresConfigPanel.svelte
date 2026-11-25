<script lang="ts">
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PasswordInput from '#lib/components/shared/PasswordInput.svelte';
	import { t } from '#lib/i18n/index.svelte.js';
	import type {
		PostgresConfig,
		PostgresCreateDBPayload,
		PostgresDeleteRolePayload,
		PostgresInspectResult,
		PostgresRoleInfo,
		PostgresSaveRolePayload
	} from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import UserPlusIcon from '@lucide/svelte/icons/user-plus';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';

	let {
		mode,
		config = $bindable(),
		inspectData = null,
		inspectLoading = false,
		inspectError = '',
		inspectSuccess = '',
		saving = false,
		creatingDB = false,
		savingRole = false,
		deletingRole = '',
		formError = '',
		onVersionChange,
		onSave,
		onCancel,
		onRefreshInspect,
		onCreateDB,
		onSaveRole,
		onDeleteRole
	}: {
		mode: 'settings' | 'inspect';
		config: PostgresConfig;
		inspectData: PostgresInspectResult | null;
		inspectLoading: boolean;
		inspectError: string;
		inspectSuccess: string;
		saving: boolean;
		creatingDB: boolean;
		savingRole?: boolean;
		deletingRole?: string;
		formError: string;
		onVersionChange: (version: string) => void;
		onSave: (payload: Record<string, unknown>) => void;
		onCancel: () => void;
		onRefreshInspect: () => void;
		onCreateDB: (payload: PostgresCreateDBPayload) => Promise<boolean>;
		onSaveRole?: (payload: PostgresSaveRolePayload) => Promise<boolean>;
		onDeleteRole?: (payload: PostgresDeleteRolePayload) => Promise<boolean>;
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

	// Roles / Users management
	let roleFormOpen = $state(false);
	let roleEditing = $state(false);
	let roleName = $state('');
	let rolePass = $state('');
	let roleCanLogin = $state(true);
	let roleSuperuser = $state(false);
	let roleCreateDB = $state(false);
	let roleCreateRole = $state(false);
	let roleReplication = $state(false);
	let roleBypassRLS = $state(false);
	let roleConnLimitStr = $state('-1');
	let roleGrantDB = $state('');
	let roleFormError = $state('');

	const isSystemPostgresRole = $derived(roleEditing && roleName.trim() === 'postgres');

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

	const remoteOptions = $derived([
		{ value: 'local', label: t('modals.serviceConfig.postgres.remoteLocal') },
		{ value: 'private_vpn', label: t('modals.serviceConfig.postgres.remotePrivateVpn') },
		{ value: 'all', label: t('modals.serviceConfig.postgres.remoteAll') }
	]);

	const showListenWarning = $derived(remoteMode !== 'local' && listenAddresses.trim() === 'localhost');

	function handleRemoteModeChange(nextMode: string) {
		if (!nextMode) return;
		remoteMode = nextMode;
		const curListen = listenAddresses.trim().toLowerCase();
		if (nextMode !== 'local') {
			if (curListen === '' || curListen === 'localhost' || curListen === '127.0.0.1' || curListen === '::1') {
				listenAddresses = '*';
			}
		} else if (curListen === '*') {
			listenAddresses = 'localhost';
		}
	}

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
			localError = t('modals.serviceConfig.postgres.invalidPort');
			return;
		}
		const logMin = parseInt(slowLogStr.trim(), 10);
		if (Number.isNaN(logMin)) {
			localError = t('modals.serviceConfig.postgres.invalidSlowLog');
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

	function openCreateRole() {
		roleFormOpen = true;
		roleEditing = false;
		roleName = '';
		rolePass = '';
		roleCanLogin = true;
		roleSuperuser = false;
		roleCreateDB = false;
		roleCreateRole = false;
		roleReplication = false;
		roleBypassRLS = false;
		roleConnLimitStr = '-1';
		roleGrantDB = '';
		roleFormError = '';
	}

	function openEditRole(role: PostgresRoleInfo) {
		roleFormOpen = true;
		roleEditing = true;
		roleName = role.name;
		rolePass = '';
		roleCanLogin = role.can_login;
		roleSuperuser = role.superuser;
		roleCreateDB = Boolean(role.create_db);
		roleCreateRole = Boolean(role.create_role);
		roleReplication = Boolean(role.replication);
		roleBypassRLS = Boolean(role.bypass_rls);
		roleConnLimitStr = String(role.conn_limit ?? -1);
		roleGrantDB = '';
		roleFormError = '';
	}

	async function handleSaveRoleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!onSaveRole) return;
		roleFormError = '';
		const cleanName = roleName.trim();
		if (!cleanName) {
			roleFormError = t('modals.serviceConfig.postgres.roleNameRequired');
			return;
		}
		if (!roleEditing && roleCanLogin && !rolePass) {
			roleFormError = t('modals.serviceConfig.postgres.rolePassRequired');
			return;
		}
		const connLim = parseInt(roleConnLimitStr.trim(), 10);
		if (Number.isNaN(connLim) || connLim < -1) {
			roleFormError = t('modals.serviceConfig.postgres.connLimitInvalid');
			return;
		}
		const ok = await onSaveRole({
			version,
			role: cleanName,
			password: rolePass || undefined,
			can_login: isSystemPostgresRole ? true : roleCanLogin,
			superuser: isSystemPostgresRole ? true : roleSuperuser,
			create_db: roleCreateDB,
			create_role: roleCreateRole,
			replication: roleReplication,
			bypass_rls: roleBypassRLS,
			conn_limit: connLim,
			grant_db: roleGrantDB || undefined
		});
		if (ok) {
			roleFormOpen = false;
			rolePass = '';
		}
	}

	const activePreset = $derived.by((): 'dev' | 'balanced' | 'high' | 'custom' => {
		const maxConn = Number.parseInt(maxConnectionsStr.trim(), 10);
		const sb = sharedBuffers.trim().toLowerCase();
		const ecs = effectiveCacheSize.trim().toLowerCase();
		const wm = workMem.trim().toLowerCase();
		const mwm = maintenanceWorkMem.trim().toLowerCase();
		const tz = timezone.trim();
		const slowLog = Number.parseInt(slowLogStr.trim(), 10);

		if (maxConn === 50 && sb === '128mb' && ecs === '512mb' && wm === '4mb' && mwm === '64mb' && slowLog === 250) {
			return 'dev';
		}
		if (
			maxConn === 150 &&
			sb === '512mb' &&
			ecs === '2gb' &&
			wm === '16mb' &&
			mwm === '128mb' &&
			tz === 'Asia/Jakarta' &&
			slowLog === 500
		) {
			return 'balanced';
		}
		if (
			maxConn === 300 &&
			sb === '1gb' &&
			ecs === '4gb' &&
			wm === '32mb' &&
			mwm === '256mb' &&
			tz === 'Asia/Jakarta' &&
			slowLog === 1000
		) {
			return 'high';
		}
		return 'custom';
	});
</script>

{#if mode === 'settings'}
	<form onsubmit={handleSubmit} class="flex flex-col gap-5">
		{#if localError || formError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>{t('modals.serviceConfig.common.validationSaveFailed')}</Alert.Title>
				<Alert.Description>{localError || formError}</Alert.Description>
			</Alert.Root>
		{/if}

		{#if showListenWarning}
			<Alert.Root>
				<TriangleAlertIcon />
				<Alert.Title>{t('modals.serviceConfig.postgres.listenWarnTitle')}</Alert.Title>
				<Alert.Description>
					{t('modals.serviceConfig.postgres.listenWarnDesc')}
				</Alert.Description>
			</Alert.Root>
		{/if}

		<div class="flex flex-wrap items-center gap-2">
			<span class="text-xs font-medium text-muted-foreground">{t('modals.serviceConfig.common.quickPresets')}</span>
			<Button
				type="button"
				variant={activePreset === 'dev' ? 'default' : 'outline'}
				size="xs"
				onclick={() => applyPreset('dev')}
			>
				{t('modals.serviceConfig.postgres.presetDev')}
			</Button>
			<Button
				type="button"
				variant={activePreset === 'balanced' ? 'default' : 'outline'}
				size="xs"
				onclick={() => applyPreset('balanced')}
			>
				{t('modals.serviceConfig.postgres.presetBalanced')}
			</Button>
			<Button
				type="button"
				variant={activePreset === 'high' ? 'default' : 'outline'}
				size="xs"
				onclick={() => applyPreset('high')}
			>
				{t('modals.serviceConfig.postgres.presetHigh')}
			</Button>
			{#if activePreset === 'custom'}
				<Badge variant="outline">{t('modals.serviceConfig.common.customBadge')}</Badge>
			{/if}
		</div>

		<Field.FieldSet>
			<Field.FieldLegend variant="label">{t('modals.serviceConfig.postgres.clusterNetLegend')}</Field.FieldLegend>
			<Field.FieldGroup class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
				<Field.Field>
					<Field.FieldLabel for="pg-cfg-version">{t('modals.serviceConfig.postgres.clusterVersionLabel')}</Field.FieldLabel>
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
					<Field.FieldLabel for="pg-cfg-port">{t('modals.serviceConfig.postgres.serverPortLabel')}</Field.FieldLabel>
					<Input id="pg-cfg-port" name="port" inputmode="numeric" pattern="[0-9]+" bind:value={portStr} required />
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="pg-cfg-listen">{t('modals.serviceConfig.postgres.listenLabel')}</Field.FieldLabel>
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
					<Field.FieldDescription>{t('modals.serviceConfig.postgres.listenDesc')}</Field.FieldDescription>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="pg-cfg-remote-mode">{t('modals.serviceConfig.postgres.remoteModeLabel')}</Field.FieldLabel>
					<Select.Root type="single" value={remoteMode} onValueChange={handleRemoteModeChange}>
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
					<Field.FieldDescription>
						{t('modals.serviceConfig.postgres.remoteModeDesc')}
					</Field.FieldDescription>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="pg-cfg-auth">{t('modals.serviceConfig.postgres.authMethodLabel')}</Field.FieldLabel>
					<Select.Root type="single" bind:value={passwordEncryption}>
						<Select.Trigger id="pg-cfg-auth" class="w-full">
							{passwordEncryption === 'scram-sha-256'
								? t('modals.serviceConfig.postgres.authScram')
								: t('modals.serviceConfig.postgres.authMd5')}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="scram-sha-256">{t('modals.serviceConfig.postgres.authScram')}</Select.Item>
								<Select.Item value="md5">{t('modals.serviceConfig.postgres.authMd5')}</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="pg-cfg-newpass">{t('modals.serviceConfig.postgres.postgresPassLabel')}</Field.FieldLabel>
					<PasswordInput
						id="pg-cfg-newpass"
						name="new_postgres_password"
						autocomplete="new-password"
						placeholder={t('modals.serviceConfig.postgres.postgresPassPlaceholder')}
						bind:value={newPostgresPassword}
					/>
					<Field.FieldDescription>{t('modals.serviceConfig.postgres.postgresPassDesc')}</Field.FieldDescription>
				</Field.Field>
			</Field.FieldGroup>
		</Field.FieldSet>

		<Field.FieldSet>
			<Field.FieldLegend variant="label">{t('modals.serviceConfig.postgres.tuningLegend')}</Field.FieldLegend>
			<Field.FieldGroup class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
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
					<Field.FieldDescription>{t('modals.serviceConfig.postgres.sharedBuffersDesc')}</Field.FieldDescription>
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
				<Field.Field class="sm:col-span-2 lg:col-span-3">
					<Field.FieldLabel for="pg-cfg-slowlog">
						{t('modals.serviceConfig.postgres.slowLogLabel')}
					</Field.FieldLabel>
					<Input
						id="pg-cfg-slowlog"
						name="log_min_duration_statement"
						inputmode="numeric"
						bind:value={slowLogStr}
						required
					/>
					<Field.FieldDescription>
						{t('modals.serviceConfig.postgres.slowLogDesc')}
					</Field.FieldDescription>
				</Field.Field>
			</Field.FieldGroup>
		</Field.FieldSet>

		<Separator />

		<div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
			<Field.Field orientation="horizontal" class="w-auto">
				<Checkbox id="pg-cfg-restart" bind:checked={restart} />
				<Field.FieldLabel for="pg-cfg-restart" class="font-normal">
					{t('modals.serviceConfig.common.restartAfterSave')}
				</Field.FieldLabel>
			</Field.Field>
			<div class="flex w-full flex-col-reverse gap-2 sm:w-auto sm:flex-row sm:items-center">
				<Button type="button" variant="outline" onclick={onCancel}>{t('common.actions.cancel')}</Button>
				<Button type="submit" disabled={saving}>
					{#if saving}
						<Spinner data-icon="inline-start" />
						{t('common.actions.saving')}
					{:else}
						{t('modals.serviceConfig.common.saveConfigBtn')}
					{/if}
				</Button>
			</div>
		</div>
	</form>
{:else}
	<div class="flex flex-col gap-5">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h4 class="text-sm font-semibold">{t('modals.serviceConfig.postgres.createTitle')}</h4>
			<Button type="button" variant="outline" size="sm" disabled={inspectLoading} onclick={onRefreshInspect}>
				<RefreshCwIcon data-icon="inline-start" />
				{t('modals.serviceConfig.postgres.refreshDataBtn')}
			</Button>
		</div>

		{#if inspectError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>{t('modals.serviceConfig.postgres.operationFailed')}</Alert.Title>
				<Alert.Description>{inspectError}</Alert.Description>
			</Alert.Root>
		{/if}
		{#if inspectSuccess}
			<Alert.Root>
				<Alert.Title>{t('modals.serviceConfig.postgres.successTitle')}</Alert.Title>
				<Alert.Description>{inspectSuccess}</Alert.Description>
			</Alert.Root>
		{/if}

		<form onsubmit={handleCreateDB} class="rounded-lg border p-4">
			<Field.FieldSet>
				<Field.FieldLegend variant="label">{t('modals.serviceConfig.postgres.newDbLegend')}</Field.FieldLegend>
				<Field.FieldGroup class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
					<Field.Field>
						<Field.FieldLabel for="pg-new-db-name">{t('modals.serviceConfig.postgres.dbNameLabel')}</Field.FieldLabel>
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
						<Field.FieldLabel for="pg-new-db-owner">{t('modals.serviceConfig.postgres.dbOwnerLabel')}</Field.FieldLabel>
						<Input
							id="pg-new-db-owner"
							placeholder={t('modals.serviceConfig.postgres.dbOwnerPlaceholder')}
							autocorrect="off"
							autocapitalize="none"
							spellcheck={false}
							bind:value={newDBOwner}
						/>
					</Field.Field>
					<Field.Field>
						<Field.FieldLabel for="pg-new-db-pass">{t('modals.serviceConfig.postgres.dbPassLabel')}</Field.FieldLabel>
						<PasswordInput
							id="pg-new-db-pass"
							autocomplete="new-password"
							placeholder={t('modals.serviceConfig.postgres.dbPassPlaceholder')}
							bind:value={newDBPass}
						/>
					</Field.Field>
					<div class="flex items-end">
						<Button type="submit" disabled={creatingDB} class="w-full">
							{#if creatingDB}
								<Spinner data-icon="inline-start" />
								{t('modals.serviceConfig.postgres.creatingBtn')}
							{:else}
								<PlusIcon data-icon="inline-start" />
								{t('modals.serviceConfig.postgres.createDbBtn')}
							{/if}
						</Button>
					</div>
				</Field.FieldGroup>
			</Field.FieldSet>
		</form>

		<div class="flex flex-col gap-2">
			<h4 class="text-sm font-semibold">{t('modals.serviceConfig.postgres.dbListTitle')}</h4>
			<ResponsiveTableWrapper label={t('modals.serviceConfig.postgres.dbListAria')}>
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>Database</Table.Head>
							<Table.Head>Owner</Table.Head>
							<Table.Head>{t('modals.serviceConfig.postgres.sizeCol')}</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#if !inspectData?.databases || inspectData.databases.length === 0}
							<Table.Row>
								<Table.Cell colspan={3} class="py-4 text-center text-muted-foreground">
									{inspectLoading ? t('modals.serviceConfig.postgres.loadingDbs') : t('modals.serviceConfig.postgres.noDbs')}
								</Table.Cell>
							</Table.Row>
						{:else}
							{#each inspectData.databases as db (db.name)}
								<Table.Row>
									<Table.Cell data-label="Database" class="font-mono font-semibold">{db.name}</Table.Cell>
									<Table.Cell data-label="Owner">{db.owner}</Table.Cell>
									<Table.Cell data-label={t('modals.serviceConfig.postgres.sizeCol')}>{db.size}</Table.Cell>
								</Table.Row>
							{/each}
						{/if}
					</Table.Body>
				</Table.Root>
			</ResponsiveTableWrapper>
		</div>

		<div class="flex flex-col gap-3">
			<div class="flex flex-wrap items-center justify-between gap-2">
				<div>
					<h4 class="text-sm font-semibold">{t('modals.serviceConfig.postgres.rolesTitle')}</h4>
					<p class="text-xs text-muted-foreground">{t('modals.serviceConfig.postgres.rolesDesc')}</p>
				</div>
				<Button type="button" variant="outline" size="sm" onclick={() => (roleFormOpen ? (roleFormOpen = false) : openCreateRole())}>
					<UserPlusIcon data-icon="inline-start" />
					{roleFormOpen ? t('modals.serviceConfig.postgres.closeRoleForm') : t('modals.serviceConfig.postgres.addRoleBtn')}
				</Button>
			</div>

			{#if roleFormOpen}
				<form onsubmit={handleSaveRoleSubmit} class="rounded-lg border bg-muted/20 p-4">
					<Field.FieldSet>
						<Field.FieldLegend variant="label">
							{roleEditing
								? t('modals.serviceConfig.postgres.editRoleLegend', { role: roleName })
								: t('modals.serviceConfig.postgres.createRoleLegend')}
						</Field.FieldLegend>

						{#if roleFormError}
							<Alert.Root variant="destructive" class="mb-3">
								<AlertCircleIcon />
								<Alert.Description class="text-xs">{roleFormError}</Alert.Description>
							</Alert.Root>
						{/if}

						<Field.FieldGroup class="grid gap-3 sm:grid-cols-2">
							<Field.Field>
								<Field.FieldLabel for="pg-role-name">{t('modals.serviceConfig.postgres.roleNameLabel')}</Field.FieldLabel>
								<Input
									id="pg-role-name"
									placeholder="app_user"
									autocorrect="off"
									autocapitalize="none"
									spellcheck={false}
									disabled={roleEditing}
									bind:value={roleName}
									required
								/>
							</Field.Field>

							<Field.Field>
								<Field.FieldLabel for="pg-role-connlimit">{t('modals.serviceConfig.postgres.connLimitLabel')}</Field.FieldLabel>
								<Input
									id="pg-role-connlimit"
									inputmode="numeric"
									bind:value={roleConnLimitStr}
									required
								/>
							</Field.Field>

							<Field.Field class="sm:col-span-2">
								<Field.FieldLabel for="pg-role-password">
									{roleEditing ? t('modals.serviceConfig.postgres.rolePassEditLabel') : t('modals.serviceConfig.postgres.rolePassCreateLabel')}
								</Field.FieldLabel>
								<PasswordInput
									id="pg-role-password"
									autocomplete="new-password"
									placeholder={roleEditing
										? t('modals.serviceConfig.postgres.rolePassEditPlaceholder')
										: t('modals.serviceConfig.postgres.rolePassCreatePlaceholder')}
									bind:value={rolePass}
								/>
							</Field.Field>

							<Field.Field class="sm:col-span-2">
								<Field.FieldLabel for="pg-role-grant-db">{t('modals.serviceConfig.postgres.grantDbLabel')}</Field.FieldLabel>
								<Select.Root type="single" bind:value={roleGrantDB}>
									<Select.Trigger id="pg-role-grant-db" class="w-full">
										{roleGrantDB || t('modals.serviceConfig.postgres.noExtraGrant')}
									</Select.Trigger>
									<Select.Content>
										<Select.Group>
											<Select.Item value="">{t('modals.serviceConfig.postgres.noExtraGrant')}</Select.Item>
											{#each inspectData?.databases || [] as db (db.name)}
												<Select.Item value={db.name}>{db.name} (owner: {db.owner})</Select.Item>
											{/each}
										</Select.Group>
									</Select.Content>
								</Select.Root>
								<Field.FieldDescription>
									{t('modals.serviceConfig.postgres.grantDbDesc')}
								</Field.FieldDescription>
							</Field.Field>

							<div class="sm:col-span-2 flex flex-col gap-2">
								<span class="text-xs font-medium text-foreground">{t('modals.serviceConfig.postgres.roleAttrsLabel')}</span>
								<div class="grid grid-cols-2 gap-2.5 sm:grid-cols-3">
									<label class="flex items-center gap-2 text-xs">
										<Checkbox bind:checked={roleCanLogin} disabled={isSystemPostgresRole} />
										<span>{t('modals.serviceConfig.postgres.attrLogin')}</span>
									</label>
									<label class="flex items-center gap-2 text-xs">
										<Checkbox bind:checked={roleSuperuser} disabled={isSystemPostgresRole} />
										<span>SUPERUSER</span>
									</label>
									<label class="flex items-center gap-2 text-xs">
										<Checkbox bind:checked={roleCreateDB} />
										<span>CREATEDB</span>
									</label>
									<label class="flex items-center gap-2 text-xs">
										<Checkbox bind:checked={roleCreateRole} />
										<span>CREATEROLE</span>
									</label>
									<label class="flex items-center gap-2 text-xs">
										<Checkbox bind:checked={roleReplication} />
										<span>REPLICATION</span>
									</label>
									<label class="flex items-center gap-2 text-xs">
										<Checkbox bind:checked={roleBypassRLS} />
										<span>BYPASSRLS</span>
									</label>
								</div>
								{#if isSystemPostgresRole}
									<p class="text-[11px] text-muted-foreground">
										{t('modals.serviceConfig.postgres.systemRoleNote')}
									</p>
								{/if}
							</div>
						</Field.FieldGroup>

						<div class="mt-4 flex flex-wrap justify-end gap-2">
							<Button type="button" variant="outline" size="sm" onclick={() => (roleFormOpen = false)}>
								{t('common.actions.cancel')}
							</Button>
							<Button type="submit" size="sm" disabled={savingRole}>
								{#if savingRole}
									<Spinner data-icon="inline-start" />
									{t('modals.serviceConfig.postgres.savingRole')}
								{:else}
									{roleEditing ? t('modals.serviceConfig.postgres.saveRoleChangesBtn') : t('modals.serviceConfig.postgres.createRoleBtn')}
								{/if}
							</Button>
						</div>
					</Field.FieldSet>
				</form>
			{/if}

			<ResponsiveTableWrapper label={t('modals.serviceConfig.postgres.rolesListAria')}>
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>{t('modals.serviceConfig.postgres.roleCol')}</Table.Head>
							<Table.Head>{t('modals.serviceConfig.postgres.loginCol')}</Table.Head>
							<Table.Head>{t('modals.serviceConfig.postgres.privilegesCol')}</Table.Head>
							<Table.Head>{t('modals.serviceConfig.postgres.ownerDbCol')}</Table.Head>
							<Table.Head class="text-right">{t('modals.serviceConfig.common.actionsCol')}</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#if !inspectData?.roles || inspectData.roles.length === 0}
							<Table.Row>
								<Table.Cell colspan={5} class="py-4 text-center text-muted-foreground">
									{inspectLoading ? t('modals.serviceConfig.postgres.loadingRoles') : t('modals.serviceConfig.postgres.noRoles')}
								</Table.Cell>
							</Table.Row>
						{:else}
							{#each inspectData.roles as role (role.name)}
								<Table.Row>
									<Table.Cell data-label="Role" class="font-mono font-semibold">{role.name}</Table.Cell>
									<Table.Cell data-label="Login">
										<Badge variant={role.can_login ? 'secondary' : 'outline'}>
											{role.can_login ? 'LOGIN' : 'NOLOGIN'}
										</Badge>
									</Table.Cell>
									<Table.Cell data-label={t('modals.serviceConfig.postgres.privilegesCol')}>
										<div class="flex flex-wrap gap-1">
											{#if role.superuser}
												<Badge variant="destructive">SUPERUSER</Badge>
											{/if}
											{#if role.create_db}
												<Badge variant="outline">CREATEDB</Badge>
											{/if}
											{#if role.create_role}
												<Badge variant="outline">CREATEROLE</Badge>
											{/if}
											{#if role.replication}
												<Badge variant="outline">REPLICATION</Badge>
											{/if}
											{#if role.bypass_rls}
												<Badge variant="outline">BYPASSRLS</Badge>
											{/if}
											{#if !role.superuser && !role.create_db && !role.create_role && !role.replication && !role.bypass_rls}
												<span class="text-xs text-muted-foreground">{t('modals.serviceConfig.postgres.standardRole')}</span>
											{/if}
											<Badge variant="outline" class="font-mono text-[11px]">
												{role.conn_limit === undefined || role.conn_limit < 0
													? t('modals.serviceConfig.postgres.unlimitedLimit')
													: t('modals.serviceConfig.postgres.limitValue', { limit: role.conn_limit })}
											</Badge>
										</div>
									</Table.Cell>
									<Table.Cell data-label="Database" class="font-mono text-xs text-muted-foreground">
										{role.databases?.length ? role.databases.join(', ') : '-'}
									</Table.Cell>
									<Table.Cell data-label={t('modals.serviceConfig.common.actionsCol')} class="text-right">
										<div class="flex justify-end">
											<DropdownMenu.Root>
												<DropdownMenu.Trigger>
													{#snippet child({ props })}
														<Button
															type="button"
															variant="outline"
															size="xs"
															disabled={deletingRole === role.name}
															{...props}
														>
															{#if deletingRole === role.name}
																<Spinner data-icon="inline-start" />
																{t('common.actions.deleting')}
															{:else}
																{t('modals.serviceConfig.common.actionBtn')}
																<ChevronDownIcon data-icon="inline-end" />
															{/if}
														</Button>
													{/snippet}
												</DropdownMenu.Trigger>
												<DropdownMenu.Content align="end" class="w-44">
													<DropdownMenu.Group>
														<DropdownMenu.Label class="truncate text-xs text-muted-foreground">
															{role.name}
														</DropdownMenu.Label>
														<DropdownMenu.Separator />
														<DropdownMenu.Item onclick={() => openEditRole(role)}>
															<KeyRoundIcon />
															<span>{t('modals.serviceConfig.postgres.editPassItem')}</span>
														</DropdownMenu.Item>
														{#if role.name !== 'postgres' && onDeleteRole}
															<DropdownMenu.Separator />
															<DropdownMenu.Item
																variant="destructive"
																disabled={deletingRole === role.name}
																onclick={() => onDeleteRole({ version, role: role.name })}
															>
																<Trash2Icon />
																<span>{t('modals.serviceConfig.postgres.deleteRoleItem')}</span>
															</DropdownMenu.Item>
														{/if}
													</DropdownMenu.Group>
												</DropdownMenu.Content>
											</DropdownMenu.Root>
										</div>
									</Table.Cell>
								</Table.Row>
							{/each}
						{/if}
					</Table.Body>
				</Table.Root>
			</ResponsiveTableWrapper>
		</div>

		<div class="flex flex-col gap-2">
			<h4 class="text-sm font-semibold">{t('modals.serviceConfig.postgres.activeConnsTitle')}</h4>
			<ResponsiveTableWrapper label={t('modals.serviceConfig.postgres.activeConnsAria')}>
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
									{t('modals.serviceConfig.postgres.noConns')}
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
