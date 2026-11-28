<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import PasswordInput from '#lib/components/shared/PasswordInput.svelte';
	import QrCode from '#lib/components/shared/QrCode.svelte';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import { api, ApiError } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import type { TOTPEnrollment, TOTPStatus } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';

	let loadingStatus = $state(false);
	let busy = $state(false);
	let status = $state<TOTPStatus | null>(null);
	let enrollment = $state<TOTPEnrollment | null>(null);
	let recoveryCodes = $state<string[] | null>(null);
	let errorMessage = $state('');

	let password = $state('');
	let otpCode = $state('');
	let activeTab = $state<'manage' | 'regenerate' | 'disable'>('manage');

	function resetFormFields() {
		password = '';
		otpCode = '';
		errorMessage = '';
	}

	async function loadStatus() {
		loadingStatus = true;
		errorMessage = '';
		try {
			const res = await api.get2FAStatus();
			status = res;
			authStore.totpEnabled = res.enabled;
			authStore.totpTerminalRequired = res.terminal_required;
		} catch (err) {
			errorMessage = err instanceof ApiError ? err.message : t('modals.twoFactor.errorFallback');
		} finally {
			loadingStatus = false;
		}
	}

	$effect(() => {
		if (authStore.twoFactorOpen) {
			enrollment = null;
			recoveryCodes = null;
			activeTab = 'manage';
			resetFormFields();
			void loadStatus();
		}
	});

	async function handleStartSetup(e: SubmitEvent) {
		e.preventDefault();
		errorMessage = '';
		if (!password) {
			errorMessage = t('modals.twoFactor.passwordRequired');
			return;
		}
		busy = true;
		try {
			enrollment = await api.setup2FA(password);
			password = '';
			otpCode = '';
		} catch (err) {
			errorMessage = err instanceof ApiError ? err.message : t('modals.twoFactor.errorFallback');
		} finally {
			busy = false;
		}
	}

	async function handleVerifySetup(e: SubmitEvent) {
		e.preventDefault();
		errorMessage = '';
		if (!otpCode.trim()) {
			errorMessage = t('modals.twoFactor.codeRequired');
			return;
		}
		busy = true;
		try {
			const res = await api.verify2FA(otpCode.trim());
			status = res.totp;
			authStore.totpEnabled = res.totp.enabled;
			authStore.totpTerminalRequired = res.totp.terminal_required;
			enrollment = null;
			recoveryCodes = res.recovery_codes;
			otpCode = '';
			toast.success(res.message || t('modals.twoFactor.enabledToast'));
		} catch (err) {
			errorMessage = err instanceof ApiError ? err.message : t('modals.twoFactor.errorFallback');
		} finally {
			busy = false;
		}
	}

	async function handleToggleTerminalRequired(nextVal: boolean) {
		if (!status || busy) return;
		busy = true;
		errorMessage = '';
		try {
			const res = await api.update2FASettings(nextVal);
			status = res.totp;
			authStore.totpTerminalRequired = res.totp.terminal_required;
			toast.success(res.message || t('modals.twoFactor.policyUpdated'));
		} catch (err) {
			errorMessage = err instanceof ApiError ? err.message : t('modals.twoFactor.errorFallback');
		} finally {
			busy = false;
		}
	}

	async function handleRegenerate(e: SubmitEvent) {
		e.preventDefault();
		errorMessage = '';
		if (!password) {
			errorMessage = t('modals.twoFactor.passwordRequired');
			return;
		}
		if (!otpCode.trim()) {
			errorMessage = t('modals.twoFactor.codeRequired');
			return;
		}
		busy = true;
		try {
			const res = await api.regenerate2FARecovery(password, otpCode.trim());
			status = res.totp;
			recoveryCodes = res.recovery_codes;
			resetFormFields();
			toast.success(res.message || t('modals.twoFactor.regeneratedToast'));
		} catch (err) {
			errorMessage = err instanceof ApiError ? err.message : t('modals.twoFactor.errorFallback');
		} finally {
			busy = false;
		}
	}

	async function handleDisable(e: SubmitEvent) {
		e.preventDefault();
		errorMessage = '';
		if (!password) {
			errorMessage = t('modals.twoFactor.passwordRequired');
			return;
		}
		if (!otpCode.trim()) {
			errorMessage = t('modals.twoFactor.codeRequired');
			return;
		}
		busy = true;
		try {
			const res = await api.disable2FA(password, otpCode.trim());
			status = res.totp;
			authStore.totpEnabled = false;
			authStore.totpTerminalRequired = false;
			recoveryCodes = null;
			resetFormFields();
			toast.success(res.message || t('modals.twoFactor.disabledToast'));
		} catch (err) {
			errorMessage = err instanceof ApiError ? err.message : t('modals.twoFactor.errorFallback');
		} finally {
			busy = false;
		}
	}

	async function copyText(val: string, msg: string) {
		try {
			await navigator.clipboard.writeText(val);
			toast.success(msg);
		} catch {
			// ignore clipboard errors
		}
	}

	function downloadRecoveryCodes(codes: string[]) {
		const content = `wdev Panel 2FA Recovery Codes (${status?.account || 'admin'})\n` + codes.join('\n') + '\n';
		const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `wdev-2fa-recovery-${status?.account || 'admin'}.txt`;
		a.click();
		URL.revokeObjectURL(url);
	}
</script>

<Dialog.Root
	open={authStore.twoFactorOpen}
	onOpenChange={(open) => {
		if (!open) authStore.closeTwoFactor();
	}}
>
	<Dialog.Content class="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2">
				<ShieldCheckIcon class="size-4 text-primary" />
				<span>{t('modals.twoFactor.title')}</span>
				{#if status}
					<Badge variant={status.enabled ? 'default' : 'secondary'} class="ml-1 text-[11px]">
						{status.enabled ? t('modals.twoFactor.statusActive') : t('modals.twoFactor.statusInactive')}
					</Badge>
				{/if}
			</Dialog.Title>
			<Dialog.Description>
				{t('modals.twoFactor.description')}
			</Dialog.Description>
		</Dialog.Header>

		{#if errorMessage}
			<Alert.Root variant="destructive" class="py-2.5">
				<AlertCircleIcon class="size-4" />
				<Alert.Description class="text-xs">{errorMessage}</Alert.Description>
			</Alert.Root>
		{/if}

		{#if loadingStatus}
			<div class="flex items-center justify-center gap-2 py-8 text-xs text-muted-foreground">
				<Spinner class="size-4" />
				<span>{t('modals.twoFactor.loading')}</span>
			</div>
		{:else if recoveryCodes}
			<div class="space-y-4">
				<Alert.Root class="border-amber-500/40 bg-amber-500/10 py-2.5">
					<ShieldAlertIcon class="size-4 text-amber-600 dark:text-amber-400" />
					<Alert.Title class="text-xs font-semibold">{t('modals.twoFactor.recoveryTitle')}</Alert.Title>
					<Alert.Description class="text-xs leading-relaxed">
						{t('modals.twoFactor.recoveryWarn')}
					</Alert.Description>
				</Alert.Root>

				<div class="grid grid-cols-2 gap-2 rounded-lg border bg-muted/40 p-3 font-mono text-xs">
					{#each recoveryCodes as code (code)}
						<div class="rounded border bg-background px-2.5 py-1.5 text-center font-semibold tracking-wider">
							{code}
						</div>
					{/each}
				</div>

				<div class="flex flex-wrap items-center justify-between gap-2">
					<div class="flex items-center gap-2">
						<Button
							type="button"
							variant="outline"
							size="sm"
							onclick={() => copyText(recoveryCodes?.join('\n') || '', t('modals.twoFactor.recoveryCopied'))}
						>
							<CopyIcon class="mr-1.5 size-3.5" />
							{t('modals.twoFactor.copyRecoveryBtn')}
						</Button>
						<Button
							type="button"
							variant="outline"
							size="sm"
							onclick={() => downloadRecoveryCodes(recoveryCodes || [])}
						>
							<DownloadIcon class="mr-1.5 size-3.5" />
							{t('modals.twoFactor.downloadRecoveryBtn')}
						</Button>
					</div>
					<Button
						type="button"
						size="sm"
						onclick={() => {
							recoveryCodes = null;
							activeTab = 'manage';
						}}
					>
						{t('modals.twoFactor.ackRecoveryBtn')}
					</Button>
				</div>
			</div>
		{:else if enrollment}
			<form onsubmit={handleVerifySetup} class="space-y-4">
				<div class="space-y-1">
					<p class="text-xs font-semibold">{t('modals.twoFactor.scanTitle')}</p>
					<p class="text-xs text-muted-foreground">{t('modals.twoFactor.scanDesc')}</p>
				</div>

				<div class="flex flex-col items-center gap-3 rounded-lg border bg-muted/30 p-4">
					<div class="overflow-hidden rounded-lg border bg-white p-2 shadow-xs">
						<QrCode value={enrollment.otpauth_url} size={184} label={t('modals.twoFactor.qrAriaLabel')} />
					</div>
					<div class="w-full space-y-1">
						<span class="text-[11px] font-medium text-muted-foreground">{t('modals.twoFactor.manualKeyLabel')}</span>
						<div class="flex items-center gap-2">
							<code class="flex-1 truncate rounded border bg-background px-2.5 py-1.5 font-mono text-xs select-all">
								{enrollment.secret}
							</code>
							<Button
								type="button"
								variant="outline"
								size="sm"
								onclick={() => copyText(enrollment?.secret || '', t('modals.twoFactor.secretCopied'))}
							>
								<CopyIcon class="mr-1 size-3.5" />
								{t('modals.twoFactor.copySecretBtn')}
							</Button>
						</div>
					</div>
				</div>

				<div class="space-y-1.5">
					<label for="totp-verify-input" class="text-xs font-medium">
						{t('modals.twoFactor.verifyCodeLabel')}
					</label>
					<Input
						id="totp-verify-input"
						type="text"
						inputmode="numeric"
						autocomplete="one-time-code"
						placeholder={t('modals.twoFactor.verifyCodePlaceholder')}
						bind:value={otpCode}
						disabled={busy}
						class="font-mono text-center text-base tracking-[0.25em]"
					/>
				</div>

				<Dialog.Footer class="gap-2 pt-1">
					<Button
						type="button"
						variant="outline"
						disabled={busy}
						onclick={() => {
							enrollment = null;
							resetFormFields();
						}}
					>
						{t('modals.twoFactor.cancelSetupBtn')}
					</Button>
					<Button type="submit" disabled={busy || !otpCode.trim()}>
						{#if busy}
							<Spinner class="mr-2 size-4" />
							<span>{t('modals.twoFactor.verifyingBtn')}</span>
						{:else}
							<span>{t('modals.twoFactor.verifyActivateBtn')}</span>
						{/if}
					</Button>
				</Dialog.Footer>
			</form>
		{:else if status?.enabled}
			<Tabs.Root
				bind:value={activeTab}
				onValueChange={() => {
					resetFormFields();
				}}
				class="space-y-4"
			>
				<Tabs.List class="grid w-full grid-cols-3">
					<Tabs.Trigger value="manage" class="text-xs">{t('modals.twoFactor.actionManageTab')}</Tabs.Trigger>
					<Tabs.Trigger value="regenerate" class="text-xs">{t('modals.twoFactor.actionRegenerateTab')}</Tabs.Trigger>
					<Tabs.Trigger value="disable" class="text-xs">{t('modals.twoFactor.actionDisableTab')}</Tabs.Trigger>
				</Tabs.List>

				<Tabs.Content value="manage" class="space-y-4">
					<div class="rounded-lg border bg-muted/30 p-3.5 text-xs space-y-1.5">
						<p class="font-medium text-foreground">
							{t('modals.twoFactor.enabledSummary', { account: status.account })}
						</p>
						<p class="text-muted-foreground">
							{t('modals.twoFactor.recoveryRemaining', { count: status.recovery_remaining })}
						</p>
					</div>

					<div class="flex items-start justify-between gap-4 rounded-lg border p-3.5">
						<div class="space-y-0.5">
							<p class="text-xs font-medium">{t('modals.twoFactor.terminalRequiredLabel')}</p>
							<p class="text-xs text-muted-foreground leading-relaxed">
								{t('modals.twoFactor.terminalRequiredDesc')}
							</p>
						</div>
						<Switch
							checked={status.terminal_required}
							disabled={busy}
							onCheckedChange={(v) => handleToggleTerminalRequired(v)}
						/>
					</div>
				</Tabs.Content>

				<Tabs.Content value="regenerate">
					<form onsubmit={handleRegenerate} class="space-y-3.5">
						<p class="text-xs leading-relaxed text-muted-foreground">
							{t('modals.twoFactor.regenerateDesc')}
						</p>
						<div class="space-y-1">
							<label for="totp-regen-pass" class="text-xs font-medium">
								{t('modals.twoFactor.passwordPromptLabel')}
							</label>
							<PasswordInput
								id="totp-regen-pass"
								canGenerate={false}
								placeholder={t('modals.twoFactor.passwordPromptPlaceholder')}
								bind:value={password}
								disabled={busy}
							/>
						</div>
						<div class="space-y-1">
							<label for="totp-regen-code" class="text-xs font-medium">
								{t('modals.twoFactor.currentCodeLabel')}
							</label>
							<Input
								id="totp-regen-code"
								type="text"
								autocomplete="one-time-code"
								placeholder={t('modals.twoFactor.currentCodePlaceholder')}
								bind:value={otpCode}
								disabled={busy}
								class="font-mono"
							/>
						</div>
						<Dialog.Footer class="pt-1">
							<Button type="submit" disabled={busy || !password || !otpCode.trim()}>
								{#if busy}
									<Spinner class="mr-2 size-4" />
								{:else}
									<RefreshCwIcon class="mr-2 size-4" />
								{/if}
								<span>{t('modals.twoFactor.regenerateBtn')}</span>
							</Button>
						</Dialog.Footer>
					</form>
				</Tabs.Content>

				<Tabs.Content value="disable">
					<form onsubmit={handleDisable} class="space-y-3.5">
						<p class="text-xs leading-relaxed text-muted-foreground">
							{t('modals.twoFactor.disableDesc')}
						</p>
						<div class="space-y-1">
							<label for="totp-dis-pass" class="text-xs font-medium">
								{t('modals.twoFactor.passwordPromptLabel')}
							</label>
							<PasswordInput
								id="totp-dis-pass"
								canGenerate={false}
								placeholder={t('modals.twoFactor.passwordPromptPlaceholder')}
								bind:value={password}
								disabled={busy}
							/>
						</div>
						<div class="space-y-1">
							<label for="totp-dis-code" class="text-xs font-medium">
								{t('modals.twoFactor.currentCodeLabel')}
							</label>
							<Input
								id="totp-dis-code"
								type="text"
								autocomplete="one-time-code"
								placeholder={t('modals.twoFactor.currentCodePlaceholder')}
								bind:value={otpCode}
								disabled={busy}
								class="font-mono"
							/>
						</div>
						<Dialog.Footer class="pt-1">
							<Button type="submit" variant="destructive" disabled={busy || !password || !otpCode.trim()}>
								{#if busy}
									<Spinner class="mr-2 size-4" />
									<span>{t('modals.twoFactor.disablingBtn')}</span>
								{:else}
									<span>{t('modals.twoFactor.disableBtn')}</span>
								{/if}
							</Button>
						</Dialog.Footer>
					</form>
				</Tabs.Content>
			</Tabs.Root>
		{:else}
			<form onsubmit={handleStartSetup} class="space-y-4">
				<div class="rounded-lg border bg-muted/40 p-3 text-xs leading-relaxed text-muted-foreground">
					{t('modals.twoFactor.disabledBanner')}
				</div>
				<div class="space-y-1">
					<label for="totp-setup-pass" class="text-xs font-medium">
						{t('modals.twoFactor.passwordPromptLabel')}
					</label>
					<PasswordInput
						id="totp-setup-pass"
						canGenerate={false}
						placeholder={t('modals.twoFactor.passwordPromptPlaceholder')}
						bind:value={password}
						disabled={busy}
						required
						autocomplete="current-password"
					/>
				</div>
				<Dialog.Footer class="pt-1">
					<Button type="button" variant="outline" onclick={() => authStore.closeTwoFactor()} disabled={busy}>
						{t('common.actions.cancel')}
					</Button>
					<Button type="submit" disabled={busy || !password}>
						{#if busy}
							<Spinner class="mr-2 size-4" />
							<span>{t('modals.twoFactor.startingSetup')}</span>
						{:else}
							<KeyRoundIcon class="mr-2 size-4" />
							<span>{t('modals.twoFactor.startSetupBtn')}</span>
						{/if}
					</Button>
				</Dialog.Footer>
			</form>
		{/if}
	</Dialog.Content>
</Dialog.Root>
