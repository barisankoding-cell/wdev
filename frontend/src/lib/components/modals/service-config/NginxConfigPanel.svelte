<script lang="ts">
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import type { NginxConfig } from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';

	function getActivePreset(): 'dev' | 'upload' | 'prod' | 'custom' {
		const wp = workerProcesses.trim().toLowerCase();
		const conn = Number.parseInt(workerConnectionsStr.trim(), 10);
		const maxBody = clientMaxBodySize.trim().toLowerCase();
		const keepalive = Number.parseInt(keepaliveTimeoutStr.trim(), 10);

		if (wp === 'auto' && conn === 1024 && maxBody === '64m' && keepalive === 65 && gzip && serverTokens) {
			return 'dev';
		}
		if (wp === 'auto' && conn === 2048 && maxBody === '256m' && keepalive === 120 && gzip && !serverTokens) {
			return 'upload';
		}
		if (wp === 'auto' && conn === 4096 && maxBody === '64m' && keepalive === 65 && gzip && !serverTokens) {
			return 'prod';
		}
		return 'custom';
	}

	let {
		config = $bindable(),
		saving = false,
		formError = '',
		onSave,
		onCancel
	}: {
		config: NginxConfig;
		saving: boolean;
		formError: string;
		onSave: (payload: Record<string, unknown>) => void;
		onCancel: () => void;
	} = $props();

	let workerProcesses = $state('auto');
	let workerConnectionsStr = $state('1024');
	let clientMaxBodySize = $state('64M');
	let keepaliveTimeoutStr = $state('65');
	let gzip = $state(true);
	let serverTokens = $state(false);
	let restart = $state(true);

	$effect(() => {
		workerProcesses = config.worker_processes || 'auto';
		workerConnectionsStr = String(config.worker_connections || 1024);
		clientMaxBodySize = config.client_max_body_size || '64M';
		keepaliveTimeoutStr = String(config.keepalive_timeout ?? 65);
		gzip = Boolean(config.gzip);
		serverTokens = Boolean(config.server_tokens);
		restart = true;
	});

	function applyPreset(preset: 'dev' | 'upload' | 'prod') {
		if (preset === 'dev') {
			workerProcesses = 'auto';
			workerConnectionsStr = '1024';
			clientMaxBodySize = '64M';
			keepaliveTimeoutStr = '65';
			gzip = true;
			serverTokens = true;
		} else if (preset === 'upload') {
			workerProcesses = 'auto';
			workerConnectionsStr = '2048';
			clientMaxBodySize = '256M';
			keepaliveTimeoutStr = '120';
			gzip = true;
			serverTokens = false;
		} else {
			workerProcesses = 'auto';
			workerConnectionsStr = '4096';
			clientMaxBodySize = '64M';
			keepaliveTimeoutStr = '65';
			gzip = true;
			serverTokens = false;
		}
	}

	function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		onSave({
			worker_processes: workerProcesses.trim(),
			worker_connections: parseInt(workerConnectionsStr.trim(), 10) || 1024,
			client_max_body_size: clientMaxBodySize.trim(),
			keepalive_timeout: parseInt(keepaliveTimeoutStr.trim(), 10) || 0,
			gzip,
			server_tokens: serverTokens,
			restart
		});
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

	<div class="flex flex-wrap items-center gap-2">
		<span class="text-xs font-medium text-muted-foreground">{t('modals.serviceConfig.common.quickPresets')}</span>
		<Button
			type="button"
			variant={getActivePreset() === 'dev' ? 'default' : 'outline'}
			size="xs"
			onclick={() => applyPreset('dev')}
		>
			{t('modals.serviceConfig.nginx.presetDev')}
		</Button>
		<Button
			type="button"
			variant={getActivePreset() === 'upload' ? 'default' : 'outline'}
			size="xs"
			onclick={() => applyPreset('upload')}
		>
			{t('modals.serviceConfig.nginx.presetUpload')}
		</Button>
		<Button
			type="button"
			variant={getActivePreset() === 'prod' ? 'default' : 'outline'}
			size="xs"
			onclick={() => applyPreset('prod')}
		>
			{t('modals.serviceConfig.nginx.presetProd')}
		</Button>
		{#if getActivePreset() === 'custom'}
			<Badge variant="outline">{t('modals.serviceConfig.common.customBadge')}</Badge>
		{/if}
	</div>

	<Field.FieldSet>
		<Field.FieldLegend variant="label">{t('modals.serviceConfig.nginx.legend')}</Field.FieldLegend>
		<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
			<Field.Field>
				<Field.FieldLabel for="nginx-cfg-workers">worker_processes</Field.FieldLabel>
				<Input
					id="nginx-cfg-workers"
					name="worker_processes"
					placeholder="auto"
					autocorrect="off"
					autocapitalize="none"
					spellcheck={false}
					bind:value={workerProcesses}
					required
				/>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="nginx-cfg-conn">worker_connections</Field.FieldLabel>
				<Input
					id="nginx-cfg-conn"
					name="worker_connections"
					inputmode="numeric"
					pattern="[0-9]+"
					bind:value={workerConnectionsStr}
					required
				/>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="nginx-cfg-bodysize">client_max_body_size</Field.FieldLabel>
				<Input
					id="nginx-cfg-bodysize"
					name="client_max_body_size"
					placeholder="64M"
					autocorrect="off"
					autocapitalize="none"
					spellcheck={false}
					bind:value={clientMaxBodySize}
					required
				/>
				<Field.FieldDescription>
					{t('modals.serviceConfig.nginx.bodySizeDesc')}
				</Field.FieldDescription>
			</Field.Field>

			<Field.Field>
				<Field.FieldLabel for="nginx-cfg-keepalive">{t('modals.serviceConfig.nginx.keepaliveLabel')}</Field.FieldLabel>
				<Input
					id="nginx-cfg-keepalive"
					name="keepalive_timeout"
					inputmode="numeric"
					pattern="[0-9]+"
					bind:value={keepaliveTimeoutStr}
					required
				/>
			</Field.Field>

			<Field.Field orientation="horizontal">
				<Checkbox id="nginx-cfg-gzip" bind:checked={gzip} />
				<Field.FieldLabel for="nginx-cfg-gzip" class="font-normal">
					{t('modals.serviceConfig.nginx.gzipLabel')}
				</Field.FieldLabel>
			</Field.Field>

			<Field.Field orientation="horizontal">
				<Checkbox id="nginx-cfg-tokens" bind:checked={serverTokens} />
				<Field.FieldLabel for="nginx-cfg-tokens" class="font-normal">
					{t('modals.serviceConfig.nginx.tokensLabel')}
				</Field.FieldLabel>
			</Field.Field>
		</Field.FieldGroup>
	</Field.FieldSet>

	<Separator />

	<div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
		<Field.Field orientation="horizontal" class="w-auto">
			<Checkbox id="nginx-cfg-restart" bind:checked={restart} />
			<Field.FieldLabel for="nginx-cfg-restart" class="font-normal">
				{t('modals.serviceConfig.nginx.testRestartLabel')}
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
