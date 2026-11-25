<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import PasswordInput from '#lib/components/shared/PasswordInput.svelte';
	import { t } from '#lib/i18n/index.svelte.js';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import { api, ApiError } from '#lib/api/client.js';
	import { toast } from 'svelte-sonner';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';

	let oldPassword = $state('');
	let newUsername = $state('');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let loading = $state(false);
	let errorMessage = $state('');

	$effect(() => {
		if (authStore.changeCredsOpen) {
			oldPassword = '';
			newUsername = authStore.username;
			newPassword = '';
			confirmPassword = '';
			errorMessage = '';
			loading = false;
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		errorMessage = '';

		if (!oldPassword) {
			errorMessage = t('modals.changeCreds.oldPasswordRequired');
			return;
		}

		if (newPassword && newPassword.length < 6) {
			errorMessage = t('modals.changeCreds.newPasswordMinLength');
			return;
		}

		if (newPassword && newPassword !== confirmPassword) {
			errorMessage = t('modals.changeCreds.confirmMismatch');
			return;
		}

		if (!newUsername.trim()) {
			errorMessage = t('modals.changeCreds.usernameEmpty');
			return;
		}

		loading = true;
		try {
			await api.changeCredentials({
				old_password: oldPassword,
				new_username: newUsername.trim() !== authStore.username ? newUsername.trim() : undefined,
				new_password: newPassword || undefined
			});

			if (newUsername.trim()) {
				authStore.username = newUsername.trim();
			}

			toast.success(t('modals.changeCreds.saveSuccessTitle'), {
				description: t('modals.changeCreds.saveSuccessDesc')
			});
			authStore.closeChangeCreds();
		} catch (err) {
			errorMessage = err instanceof ApiError ? err.message : t('modals.changeCreds.saveErrorFallback');
		} finally {
			loading = false;
		}
	}
</script>

<Dialog.Root
	open={authStore.changeCredsOpen}
	onOpenChange={(open) => {
		if (!open) authStore.closeChangeCreds();
	}}
>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2">
				<KeyRoundIcon class="size-4 text-primary" />
				<span>{t('modals.changeCreds.title')}</span>
			</Dialog.Title>
			<Dialog.Description>
				{t('modals.changeCreds.description')}
			</Dialog.Description>
		</Dialog.Header>

		{#if errorMessage}
			<Alert.Root variant="destructive" class="py-2.5">
				<AlertCircleIcon class="size-4" />
				<Alert.Description class="text-xs">{errorMessage}</Alert.Description>
			</Alert.Root>
		{/if}

		<form onsubmit={handleSubmit} class="space-y-3.5">
			<div class="space-y-1">
				<label for="old-password" class="text-xs font-medium text-foreground">
					{t('modals.changeCreds.currentPasswordLabel')} <span class="text-destructive">*</span>
				</label>
				<PasswordInput
					id="old-password"
					canGenerate={false}
					placeholder={t('modals.changeCreds.currentPasswordPlaceholder')}
					bind:value={oldPassword}
					disabled={loading}
					required
					autocomplete="current-password"
				/>
			</div>

			<div class="space-y-1">
				<label for="new-username" class="text-xs font-medium text-foreground">
					{t('modals.changeCreds.usernameLabel')}
				</label>
				<Input
					id="new-username"
					type="text"
					placeholder="admin"
					bind:value={newUsername}
					disabled={loading}
					autocomplete="username"
				/>
			</div>

			<div class="space-y-1">
				<label for="new-password" class="text-xs font-medium text-foreground">
					{t('modals.changeCreds.newPasswordLabel')}
				</label>
				<PasswordInput
					id="new-password"
					placeholder={t('modals.changeCreds.newPasswordPlaceholder')}
					bind:value={newPassword}
					disabled={loading}
					autocomplete="new-password"
					onGenerate={(pwd) => {
						confirmPassword = pwd;
					}}
				/>
			</div>

			{#if newPassword}
				<div class="space-y-1">
					<label for="confirm-password" class="text-xs font-medium text-foreground">
						{t('modals.changeCreds.confirmPasswordLabel')}
					</label>
					<PasswordInput
						id="confirm-password"
						canGenerate={false}
						placeholder={t('modals.changeCreds.confirmPasswordPlaceholder')}
						bind:value={confirmPassword}
						disabled={loading}
						autocomplete="new-password"
					/>
				</div>
			{/if}

			<Dialog.Footer class="pt-2">
				<Button type="button" variant="outline" onclick={() => authStore.closeChangeCreds()} disabled={loading}>
					{t('common.actions.cancel')}
				</Button>
				<Button type="submit" disabled={loading}>
					{#if loading}
						<Spinner class="mr-2 size-4" />
						<span>{t('common.actions.saving')}</span>
					{:else}
						{t('modals.changeCreds.saveBtn')}
					{/if}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
