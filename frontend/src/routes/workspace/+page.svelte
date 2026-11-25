<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import OrcaCommitAiModal from '#lib/components/modals/OrcaCommitAiModal.svelte';
	import OrcaPairingModal from '#lib/components/modals/OrcaPairingModal.svelte';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { WorkspaceStatus } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import QrCodeIcon from '@lucide/svelte/icons/qr-code';
	import PlayIcon from '@lucide/svelte/icons/play';
	import SquareIcon from '@lucide/svelte/icons/square';
	import RotateCwIcon from '@lucide/svelte/icons/rotate-cw';
	import ArrowUpCircleIcon from '@lucide/svelte/icons/arrow-up-circle';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';

	let workspace = $state<WorkspaceStatus | null>(null);
	let loading = $state(true);
	let refreshing = $state(false);
	let orcaActionPending = $state<'start' | 'stop' | 'restart' | null>(null);
	let pairModalOpen = $state(false);
	let commitAiModalOpen = $state(false);

	async function loadWorkspace(forceRefresh = false) {
		if (forceRefresh) refreshing = true;
		else if (!workspace) loading = true;

		try {
			workspace = await api.getWorkspaceStatus(forceRefresh);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('workspace.loadFailed'));
		} finally {
			loading = false;
			refreshing = false;
		}
	}

	onMount(() => loadWorkspace(false));

	$effect(() => {
		if (cp.refreshTick > 0) loadWorkspace(false);
	});

	async function controlOrca(action: 'start' | 'stop' | 'restart') {
		orcaActionPending = action;
		try {
			await api.controlService('orca', action);
			toast.success(
				t('workspace.orcaActionSuccess', {
					action: t(`workspace.actionVerbs.${action}`)
				})
			);
			await loadWorkspace(false);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('workspace.orcaActionFailed'));
		} finally {
			orcaActionPending = null;
		}
	}

	async function copyText(val: string, label: string) {
		if (!val || val === '-') return;
		try {
			await navigator.clipboard.writeText(val);
			toast.success(t('common.clipboard.copySuccess', { label }));
		} catch {
			toast.error(t('common.clipboard.copyFailed', { label }));
		}
	}

	function activeZshThemeKey(theme: string): string {
		if (theme.includes('powerlevel10k')) return 'zsh-p10k';
		if (theme.includes('agnoster')) return 'zsh-agnoster';
		if (theme.includes('robbyrussell')) return 'zsh-robbyrussell';
		return '';
	}

	function pairingModeLabel(mode?: string): string {
		switch (mode) {
			case 'local':
				return t('workspace.pairModal.addressModeLocal');
			case 'vpn':
				return t('workspace.pairModal.addressModeVpn');
			case 'custom':
				return t('workspace.pairModal.addressModeCustom');
			default:
				return t('workspace.pairModal.addressModeAuto');
		}
	}

	function openPairModal() {
		pairModalOpen = true;
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<div>
			<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('workspace.title')}</h1>
			<p class="text-sm text-muted-foreground">
				{t('workspace.subtitle')}
			</p>
		</div>
		<Button
			variant="outline"
			size="sm"
			class="w-full sm:w-auto"
			disabled={refreshing}
			onclick={() => loadWorkspace(true)}
		>
			{#if refreshing}
				<Spinner data-icon="inline-start" />
				{t('common.actions.loading')}
			{:else}
				<RefreshCwIcon data-icon="inline-start" />
				{t('common.actions.refresh')}
			{/if}
		</Button>
	</div>

	{#if loading || !workspace}
		<div class="grid gap-4 lg:grid-cols-2">
			<Skeleton class="h-80 w-full" />
			<Skeleton class="h-80 w-full" />
		</div>
	{:else}
		{@const orca = workspace.orca}
		{@const shell = workspace.shell}
		{@const isHeadless = (orca.mode || 'headless').toLowerCase() === 'headless'}
		{@const selectedThemeKey = activeZshThemeKey(shell.active_theme || '')}

		<div class="grid gap-4 lg:grid-cols-2">
			<!-- Orca ADE Card -->
			<Card.Root class="flex flex-col justify-between">
				<Card.Header>
					<div class="flex flex-wrap items-start justify-between gap-2">
						<div>
							<Card.Title>{t('workspace.orca.title')}</Card.Title>
							<Card.Description>{t('workspace.orca.subtitle')}</Card.Description>
						</div>
						<div class="flex flex-wrap items-center gap-1.5">
							{#if orca.active}
								<StatusBadge
									tone="success"
									label={t('workspace.orca.runningStatus', {
										mode: isHeadless ? t('workspace.orca.modeShortHeadless') : t('workspace.orca.modeShortDesktop')
									})}
								/>
							{:else if orca.installed}
								<StatusBadge tone="destructive" label={t('common.status.stopped')} />
							{:else}
								<StatusBadge tone="neutral" label={t('common.status.notInstalled')} />
							{/if}
							{#if orca.has_update}
								<StatusBadge tone="warning" label={t('common.status.updateAvailable')} />
							{/if}
						</div>
					</div>
				</Card.Header>

				<Card.Content class="flex flex-col gap-4">
					<p class="text-xs leading-relaxed text-muted-foreground">
						{t('workspace.orca.description')}
					</p>

					<div class="flex flex-col gap-2 rounded-md border bg-muted/40 p-3 text-xs">
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">{t('workspace.orca.installedVersion')}</span>
							<span class="font-mono font-semibold">
								{orca.version ? `v${orca.version}` : orca.installed ? t('common.status.installed') : t('common.status.notInstalled')}
							</span>
						</div>
						<Separator />
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">{t('workspace.orca.latestRelease')}</span>
							<span class="font-mono font-medium">
								{orca.latest_version ? `v${orca.latest_version}` : '-'}
							</span>
						</div>
						<Separator />
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">{t('workspace.orca.operationMode')}</span>
							<span class="text-right font-medium">
								{isHeadless ? t('workspace.orca.modeHeadlessLabel') : t('workspace.orca.modeDesktopLabel')}
							</span>
						</div>
						{#if orca.active}
							<Separator />
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">{t('workspace.orca.wsPort')}</span>
								<span class="font-mono font-semibold text-primary">{orca.port || 6768}</span>
							</div>
							{#if orca.endpoint}
								<Separator />
								<div class="flex items-center justify-between gap-2">
									<span class="text-muted-foreground">{t('workspace.orca.pairingEndpoint')}</span>
									<div class="flex min-w-0 items-center gap-1.5">
										<Badge variant="outline" class="shrink-0">
											{pairingModeLabel(orca.pairing_address_mode)}
										</Badge>
										<span class="truncate font-mono font-medium">{orca.endpoint}</span>
									</div>
								</div>
							{/if}
							{#if orca.pid}
								<Separator />
								<div class="flex items-center justify-between gap-2">
									<span class="text-muted-foreground">{t('workspace.orca.pid')}</span>
									<span class="font-mono">{orca.pid}</span>
								</div>
							{/if}
						{/if}
						{#if orca.commit_ai}
							<Separator />
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">{t('workspace.orca.commitAiAgent')}</span>
								<span class="text-right font-mono font-semibold">
									{#if orca.commit_ai.enabled}
										{orca.commit_ai.agent_label || orca.commit_ai.agent_id}
									{:else}
										{t('common.status.disabled')}
									{/if}
								</span>
							</div>
							{#if orca.commit_ai.enabled && orca.commit_ai.model_id}
								<Separator />
								<div class="flex items-center justify-between gap-2">
									<span class="text-muted-foreground">{t('workspace.orca.commitAiModel')}</span>
									<span class="max-w-[60%] truncate text-right font-mono font-medium">
										{orca.commit_ai.model_id}{orca.commit_ai.thinking_level ? ` (${orca.commit_ai.thinking_level})` : ''}
									</span>
								</div>
							{/if}
							<Separator />
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground">{t('workspace.orca.commitTemplate')}</span>
								<span class="text-right font-medium">
									{orca.commit_ai.has_custom_prompt ? t('workspace.orca.templateCustom') : t('workspace.orca.templateDefault')}
									{#if orca.commit_ai.repo_override_count > 0}
										<span class="ml-1 text-amber-600 dark:text-amber-400">
											{t('workspace.orca.repoOverrideCount', { count: orca.commit_ai.repo_override_count })}
										</span>
									{/if}
								</span>
							</div>
							{#if orca.sync_status?.managed}
								<Separator />
								<div class="flex items-center justify-between gap-2">
									<span class="text-muted-foreground">{t('workspace.orca.commitAiSync')}</span>
									<span class="text-right font-medium">
										{#if orca.sync_status.in_sync}
											<span class="text-emerald-600 dark:text-emerald-400">{t('common.status.synced')}</span>
										{:else if orca.sync_status.state === 'blocked'}
											<span class="text-amber-600 dark:text-amber-400">{t('workspace.orca.syncBlocked')}</span>
										{:else}
											<span class="text-destructive">{t('common.status.needsSync')}</span>
										{/if}
									</span>
								</div>
							{/if}
						{/if}
					</div>
				</Card.Content>

				<Card.Footer class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
					{#if orca.installed}
						{#if orca.has_update}
							<Button size="sm" class="col-span-2 sm:col-span-1" onclick={() => cp.triggerInstall('orca-update')}>
								<ArrowUpCircleIcon data-icon="inline-start" />
								{t('workspace.orca.updateBtn', { version: orca.latest_version ?? '' })}
							</Button>
						{/if}
						{#if orca.web_client_url}
							<Button
								size="sm"
								class="col-span-2 sm:col-span-1"
								href={orca.web_client_url}
								target="_blank"
								rel="noopener noreferrer"
							>
								<ExternalLinkIcon data-icon="inline-start" />
								{t('workspace.orca.openWebClient')}
							</Button>
						{/if}
						<Button variant="outline" size="sm" onclick={openPairModal}>
							<QrCodeIcon data-icon="inline-start" />
							{t('workspace.orca.infoPairingBtn')}
						</Button>
						{#if orca.active}
							<Button
								variant="outline"
								size="sm"
								disabled={Boolean(orcaActionPending)}
								onclick={() => controlOrca('restart')}
							>
								{#if orcaActionPending === 'restart'}
									<Spinner data-icon="inline-start" />
									{t('workspace.orca.restarting')}
								{:else}
									<RotateCwIcon data-icon="inline-start" />
									{t('common.actions.restart')}
								{/if}
							</Button>
							<Button
								variant="destructive"
								size="sm"
								disabled={Boolean(orcaActionPending)}
								onclick={() => controlOrca('stop')}
							>
								{#if orcaActionPending === 'stop'}
									<Spinner data-icon="inline-start" />
									{t('workspace.orca.stopping')}
								{:else}
									<SquareIcon data-icon="inline-start" />
									{t('common.actions.stop')}
								{/if}
							</Button>
						{:else}
							<Button
								size="sm"
								disabled={Boolean(orcaActionPending)}
								onclick={() => controlOrca('start')}
							>
								{#if orcaActionPending === 'start'}
									<Spinner data-icon="inline-start" />
									{t('workspace.orca.starting')}
								{:else}
									<PlayIcon data-icon="inline-start" />
									{t('workspace.orca.startServiceBtn')}
								{/if}
							</Button>
						{/if}
						<Button
							variant="outline"
							size="sm"
							onclick={() =>
								cp.triggerInstall(isHeadless ? 'orca-switch-desktop' : 'orca-switch-headless')}
						>
							{isHeadless ? t('workspace.orca.switchToDesktop') : t('workspace.orca.switchToHeadless')}
						</Button>
						<Button variant="outline" size="sm" onclick={() => (commitAiModalOpen = true)}>
							<SparklesIcon data-icon="inline-start" />
							{t('workspace.orca.commitAiBtn')}
						</Button>
						<Button variant="destructive" size="sm" onclick={() => cp.triggerInstall('orca-uninstall')}>
							<Trash2Icon data-icon="inline-start" />
							{t('common.actions.uninstall')}
						</Button>
					{:else}
						<Button size="sm" onclick={() => cp.triggerInstall('orca-headless')}>
							{t('workspace.orca.setupHeadless')}
						</Button>
						<Button variant="outline" size="sm" onclick={() => cp.triggerInstall('orca-desktop')}>
							{t('workspace.orca.setupDesktop')}
						</Button>
					{/if}
				</Card.Footer>
			</Card.Root>

			<!-- Oh My Zsh & Terminal Shell Card -->
			<Card.Root class="flex flex-col justify-between">
				<Card.Header>
					<div class="flex flex-wrap items-start justify-between gap-2">
						<div>
							<Card.Title>{t('workspace.shell.title')}</Card.Title>
							<Card.Description>{t('workspace.shell.subtitle')}</Card.Description>
						</div>
						<StatusBadge
							tone={shell.oh_my_zsh ? 'success' : 'neutral'}
							label={shell.oh_my_zsh ? t('common.status.installed') : t('common.status.notInstalled')}
						/>
					</div>
				</Card.Header>

				<Card.Content class="flex flex-col gap-4">
					<p class="text-xs leading-relaxed text-muted-foreground">
						{t('workspace.shell.description')}
					</p>

					<div class="flex flex-col gap-2 rounded-md border bg-muted/40 p-3 text-xs">
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">{t('workspace.shell.defaultUserShell')}</span>
							<span class="truncate font-mono font-semibold">{shell.current_shell || '/bin/bash'}</span>
						</div>
						<Separator />
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">{t('workspace.shell.omzStatus')}</span>
							<span class="text-right">{shell.oh_my_zsh ? t('workspace.shell.omzActivePath') : t('common.status.notInstalled')}</span>
						</div>
						<Separator />
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">{t('workspace.shell.activePromptTheme')}</span>
							<span class="truncate font-mono font-semibold text-primary">{shell.active_theme || '-'}</span>
						</div>
						<Separator />
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">{t('workspace.shell.nerdFont')}</span>
							<span class="truncate font-mono font-semibold">
								{shell.nerd_font || t('common.status.notInstalled')}
							</span>
						</div>
					</div>

					<Field.FieldGroup>
						<Field.Field>
							<Field.FieldTitle id="zsh-theme-label">{t('workspace.shell.changeThemeLabel')}</Field.FieldTitle>
							<ToggleGroup.Root
								type="single"
								value={selectedThemeKey}
								variant="outline"
								spacing={2}
								class="flex w-full flex-wrap"
								aria-labelledby="zsh-theme-label"
								onValueChange={(val) => {
									if (val) cp.triggerInstall(val);
								}}
							>
								<ToggleGroup.Item value="zsh-p10k" class="min-w-[7rem] flex-1">Powerlevel10k</ToggleGroup.Item>
								<ToggleGroup.Item value="zsh-agnoster" class="min-w-[6rem] flex-1">Agnoster</ToggleGroup.Item>
								<ToggleGroup.Item value="zsh-robbyrussell" class="min-w-[7rem] flex-1">Robbyrussell</ToggleGroup.Item>
							</ToggleGroup.Root>
						</Field.Field>
					</Field.FieldGroup>
				</Card.Content>

				<Card.Footer>
					<Button
						variant={shell.oh_my_zsh ? 'outline' : 'default'}
						size="sm"
						class="w-full sm:w-auto"
						onclick={() => cp.triggerInstall('zsh')}
					>
						<TerminalIcon data-icon="inline-start" />
						{shell.oh_my_zsh ? t('workspace.shell.reinstallOmz') : t('workspace.shell.installOmz')}
					</Button>
				</Card.Footer>
			</Card.Root>
		</div>
	{/if}
</div>

<OrcaPairingModal bind:open={pairModalOpen} bind:workspace onCopy={copyText} />

<OrcaCommitAiModal bind:open={commitAiModalOpen} onSaved={() => loadWorkspace(false)} />
