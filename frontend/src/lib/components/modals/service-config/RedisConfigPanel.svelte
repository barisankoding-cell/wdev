<script lang="ts">
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import ResponsiveTableWrapper from '#lib/components/shared/ResponsiveTableWrapper.svelte';
	import PasswordInput from '#lib/components/shared/PasswordInput.svelte';
	import { t } from '#lib/i18n/index.svelte.js';
	import type { RedisConfig, RedisInspectResult } from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';

	function getActivePreset(): 'local' | 'cache' | 'queue' | 'custom' {
		const port = Number.parseInt(portStr.trim(), 10);
		const cleanBind = bind.trim();
		const isLocalBind = cleanBind === '127.0.0.1 -::1' || cleanBind === '127.0.0.1 ::1' || cleanBind === '127.0.0.1';
		const pm = protectedMode.trim().toLowerCase();
		const maxmem = maxmemory.trim().toLowerCase();
		const policy = maxmemoryPolicy.trim().toLowerCase();
		const pers = persistenceMode.trim().toLowerCase();

		if (
			port === 6379 &&
			isLocalBind &&
			pm === 'yes' &&
			maxmem === '256mb' &&
			policy === 'noeviction' &&
			pers === 'rdb'
		) {
			return 'local';
		}
		if (maxmem === '256mb' && policy === 'allkeys-lru' && pers === 'none') {
			return 'cache';
		}
		if (maxmem === '512mb' && policy === 'noeviction' && pers === 'aof') {
			return 'queue';
		}
		return 'custom';
	}

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

	const policyOptions = $derived([
		{ value: 'noeviction', label: t('modals.serviceConfig.redis.policyNoeviction') },
		{ value: 'allkeys-lru', label: t('modals.serviceConfig.redis.policyAllkeysLru') },
		{ value: 'volatile-lru', label: 'volatile-lru' },
		{ value: 'allkeys-lfu', label: 'allkeys-lfu' },
		{ value: 'volatile-lfu', label: 'volatile-lfu' },
		{ value: 'volatile-ttl', label: 'volatile-ttl' },
		{ value: 'allkeys-random', label: 'allkeys-random' },
		{ value: 'volatile-random', label: 'volatile-random' }
	]);

	const persistenceOptions = $derived([
		{ value: 'rdb', label: t('modals.serviceConfig.redis.persistRdb') },
		{ value: 'aof', label: t('modals.serviceConfig.redis.persistAof') },
		{ value: 'both', label: t('modals.serviceConfig.redis.persistBoth') },
		{ value: 'none', label: t('modals.serviceConfig.redis.persistNone') }
	]);

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
			localError = t('modals.serviceConfig.redis.invalidPort');
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
				<Alert.Title>{t('modals.serviceConfig.common.validationSaveFailed')}</Alert.Title>
				<Alert.Description>{localError || formError}</Alert.Description>
			</Alert.Root>
		{/if}

		<div class="flex flex-wrap items-center gap-2">
			<span class="text-xs font-medium text-muted-foreground">{t('modals.serviceConfig.common.quickPresets')}</span>
			<Button
				type="button"
				variant={getActivePreset() === 'local' ? 'default' : 'outline'}
				size="xs"
				onclick={() => applyPreset('local')}
			>
				{t('modals.serviceConfig.redis.presetLocal')}
			</Button>
			<Button
				type="button"
				variant={getActivePreset() === 'cache' ? 'default' : 'outline'}
				size="xs"
				onclick={() => applyPreset('cache')}
			>
				{t('modals.serviceConfig.redis.presetCache')}
			</Button>
			<Button
				type="button"
				variant={getActivePreset() === 'queue' ? 'default' : 'outline'}
				size="xs"
				onclick={() => applyPreset('queue')}
			>
				{t('modals.serviceConfig.redis.presetQueue')}
			</Button>
			{#if getActivePreset() === 'custom'}
				<Badge variant="outline">{t('modals.serviceConfig.common.customBadge')}</Badge>
			{/if}
		</div>

		<Field.FieldSet>
			<Field.FieldLegend variant="label">{t('modals.serviceConfig.redis.netSecurityLegend')}</Field.FieldLegend>
			<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
				<Field.Field>
					<Field.FieldLabel for="redis-cfg-port">{t('modals.serviceConfig.redis.portLabel')}</Field.FieldLabel>
					<Input id="redis-cfg-port" name="port" inputmode="numeric" pattern="[0-9]+" bind:value={portStr} required />
					<Field.FieldDescription>{t('modals.serviceConfig.redis.portDesc')}</Field.FieldDescription>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="redis-cfg-protected">{t('modals.serviceConfig.redis.protectedModeLabel')}</Field.FieldLabel>
					<Select.Root type="single" bind:value={protectedMode}>
						<Select.Trigger id="redis-cfg-protected" class="w-full">
							{protectedMode === 'yes' ? t('modals.serviceConfig.redis.protectedYes') : t('modals.serviceConfig.redis.protectedNo')}
						</Select.Trigger>
						<Select.Content>
							<Select.Group>
								<Select.Item value="yes">{t('modals.serviceConfig.redis.protectedYes')}</Select.Item>
								<Select.Item value="no">{t('modals.serviceConfig.redis.protectedNo')}</Select.Item>
							</Select.Group>
						</Select.Content>
					</Select.Root>
					<Field.FieldDescription>
						{t('modals.serviceConfig.redis.protectedDesc')}
					</Field.FieldDescription>
				</Field.Field>

				<Field.Field class="sm:col-span-2">
					<Field.FieldLabel for="redis-cfg-bind">{t('modals.serviceConfig.redis.bindAddressLabel')}</Field.FieldLabel>
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
						{t('modals.serviceConfig.redis.bindAddressDesc')}
					</Field.FieldDescription>
				</Field.Field>

				<Field.Field class="sm:col-span-2">
					<div class="flex items-center justify-between gap-2">
						<Field.FieldLabel for="redis-cfg-password">{t('modals.serviceConfig.redis.passwordLabel')}</Field.FieldLabel>
						<StatusBadge
							tone={config.has_password ? 'success' : 'warning'}
							label={config.has_password ? t('modals.serviceConfig.redis.passwordActive') : t('modals.serviceConfig.redis.noPassword')}
						/>
					</div>
					<PasswordInput
						id="redis-cfg-password"
						name="password"
						autocomplete="new-password"
						placeholder={t('modals.serviceConfig.redis.passwordPlaceholder')}
						bind:value={password}
					/>
					<Field.FieldDescription>
						{t('modals.serviceConfig.redis.passwordDesc')}
					</Field.FieldDescription>
				</Field.Field>

				<Field.Field orientation="horizontal" class="sm:col-span-2">
					<Checkbox id="redis-cfg-clear-pass" bind:checked={clearPassword} />
					<Field.FieldLabel for="redis-cfg-clear-pass" class="font-normal">
						{t('modals.serviceConfig.redis.clearPassLabel')}
					</Field.FieldLabel>
				</Field.Field>
			</Field.FieldGroup>
		</Field.FieldSet>

		<Field.FieldSet>
			<Field.FieldLegend variant="label">{t('modals.serviceConfig.redis.memoryLegend')}</Field.FieldLegend>
			<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
				<Field.Field>
					<Field.FieldLabel for="redis-cfg-maxmemory">{t('modals.serviceConfig.redis.maxmemoryLabel')}</Field.FieldLabel>
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
					<Field.FieldDescription>{t('modals.serviceConfig.redis.maxmemoryDesc')}</Field.FieldDescription>
				</Field.Field>

				<Field.Field>
					<Field.FieldLabel for="redis-cfg-policy">{t('modals.serviceConfig.redis.policyLabel')}</Field.FieldLabel>
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
					<Field.FieldLabel for="redis-cfg-persistence">{t('modals.serviceConfig.redis.persistenceLabel')}</Field.FieldLabel>
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
					<Field.FieldLabel for="redis-cfg-maxclients">{t('modals.serviceConfig.redis.maxclientsLabel')}</Field.FieldLabel>
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
					<Field.FieldLabel for="redis-cfg-databases">{t('modals.serviceConfig.redis.databasesLabel')}</Field.FieldLabel>
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
	<div class="flex flex-col gap-4">
		<div class="flex flex-wrap items-center justify-between gap-2">
			<h4 class="text-sm font-semibold">{t('modals.serviceConfig.redis.inspectSummaryTitle')}</h4>
			<Button type="button" variant="outline" size="sm" disabled={inspectLoading} onclick={onRefreshInspect}>
				{#if inspectLoading}
					<Spinner data-icon="inline-start" />
					{t('common.actions.refreshing')}
				{:else}
					<RefreshCwIcon data-icon="inline-start" />
					{t('common.actions.refresh')}
				{/if}
			</Button>
		</div>

		{#if inspectError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>{t('modals.serviceConfig.redis.inspectSummaryTitle')}</Alert.Title>
				<Alert.Description>{inspectError}</Alert.Description>
			</Alert.Root>
		{/if}

		{#if inspectLoading && !inspectData}
			<p class="text-sm text-muted-foreground">{t('modals.serviceConfig.redis.fetchingStats')}</p>
		{:else if inspectData}
			<div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">{t('modals.serviceConfig.redis.versionMode')}</div>
					<div class="mt-1 font-mono text-sm font-semibold">
						{inspectData.version || '-'} ({inspectData.mode || 'standalone'})
					</div>
				</div>
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">{t('modals.serviceConfig.redis.usedMemory')}</div>
					<div class="mt-1 font-mono text-sm font-semibold">{inspectData.used_memory_human || '-'}</div>
				</div>
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">{t('modals.serviceConfig.redis.peakMaxMemory')}</div>
					<div class="mt-1 font-mono text-sm font-semibold">
						{inspectData.peak_memory_human || '-'} / {inspectData.maxmemory_human || t('modals.serviceConfig.redis.unlimited')}
					</div>
				</div>
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">{t('modals.serviceConfig.redis.clientsUptime')}</div>
					<div class="mt-1 font-mono text-sm font-semibold">
						{t('modals.serviceConfig.redis.clientsCount', { count: inspectData.connected_clients || 0 })} · {Math.floor((inspectData.uptime_seconds || 0) / 60)}m
					</div>
				</div>
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">{t('modals.serviceConfig.redis.hitRate')}</div>
					<div class="mt-1 font-mono text-sm font-semibold">
						{(inspectData.hit_rate_percent || 0).toFixed(1)}% ({inspectData.keyspace_hits || 0}/{inspectData.keyspace_misses || 0})
					</div>
				</div>
				<div class="rounded-lg border p-3">
					<div class="text-xs text-muted-foreground">{t('modals.serviceConfig.redis.totalCommands')}</div>
					<div class="mt-1 font-mono text-sm font-semibold">{inspectData.total_commands_processed || 0}</div>
				</div>
			</div>

			<h4 class="text-sm font-semibold">{t('modals.serviceConfig.redis.keyspaceTitle')}</h4>
			<ResponsiveTableWrapper label={t('modals.serviceConfig.redis.keyspaceAria')}>
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>DB</Table.Head>
							<Table.Head>Keys</Table.Head>
							<Table.Head>Expires</Table.Head>
							<Table.Head>Avg TTL</Table.Head>
							<Table.Head class="text-right">{t('modals.serviceConfig.common.actionsCol')}</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#if !inspectData.keyspaces || inspectData.keyspaces.length === 0}
							<Table.Row>
								<Table.Cell colspan={5} class="py-6 text-center text-muted-foreground">
									{t('modals.serviceConfig.redis.noKeys')}
								</Table.Cell>
							</Table.Row>
						{:else}
							{#each inspectData.keyspaces as ks (ks.index)}
								<Table.Row>
									<Table.Cell data-label="DB" class="font-mono font-semibold">{ks.db}</Table.Cell>
									<Table.Cell data-label="Keys">{ks.keys}</Table.Cell>
									<Table.Cell data-label="Expires">{ks.expires}</Table.Cell>
									<Table.Cell data-label="Avg TTL">{ks.avg_ttl} ms</Table.Cell>
									<Table.Cell data-label={t('modals.serviceConfig.common.actionsCol')} class="text-right">
										<div class="flex justify-end">
											<DropdownMenu.Root>
												<DropdownMenu.Trigger>
													{#snippet child({ props })}
														<Button
															type="button"
															variant="outline"
															size="xs"
															disabled={flushingDB === ks.index}
															{...props}
														>
															{#if flushingDB === ks.index}
																<Spinner data-icon="inline-start" />
																{t('modals.serviceConfig.redis.flushing')}
															{:else}
																{t('modals.serviceConfig.common.actionBtn')}
																<ChevronDownIcon data-icon="inline-end" />
															{/if}
														</Button>
													{/snippet}
												</DropdownMenu.Trigger>
												<DropdownMenu.Content align="end" class="w-40">
													<DropdownMenu.Group>
														<DropdownMenu.Item
															variant="destructive"
															disabled={flushingDB === ks.index}
															onclick={() => onFlushDB(ks.index)}
														>
															<Trash2Icon />
															<span>{t('modals.serviceConfig.redis.flushItem', { db: ks.db })}</span>
														</DropdownMenu.Item>
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
		{/if}
	</div>
{/if}
