<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import EyeIcon from '@lucide/svelte/icons/eye';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';

	let password = $state('');
	let showPassword = $state(false);
	let touched = $state(false);

	$effect(() => {
		if (cp.sudoOpen) {
			password = '';
			showPassword = false;
			touched = false;
		}
	});

	const hasValidationError = $derived(touched && password.length === 0);

	function handleCancel() {
		cp.resolveSudo(null);
	}

	function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		touched = true;
		if (!password) return;
		const val = password;
		password = '';
		cp.resolveSudo(val);
	}
</script>

<Dialog.Root
	open={cp.sudoOpen}
	onOpenChange={(open) => {
		if (!open && cp.sudoOpen) handleCancel();
	}}
>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2">
				<KeyRoundIcon class="size-4 text-primary" />
				<span>Akses Sudo Diperlukan</span>
			</Dialog.Title>
			<Dialog.Description>
				{cp.sudoMessage || 'Operasi ini memerlukan hak akses administrator (sudo).'}
			</Dialog.Description>
		</Dialog.Header>

		{#if cp.sudoError}
			<Alert.Root variant="destructive">
				<AlertCircleIcon />
				<Alert.Title>Autentikasi Gagal</Alert.Title>
				<Alert.Description>{cp.sudoError}</Alert.Description>
			</Alert.Root>
		{/if}

		<form onsubmit={handleSubmit} class="flex flex-col gap-4">
			<Field.FieldGroup>
				<Field.Field data-invalid={hasValidationError ? 'true' : undefined}>
					<Field.FieldLabel for="wdev-sudo-password">Password Sudo</Field.FieldLabel>
					<InputGroup.Root>
						<InputGroup.Input
							id="wdev-sudo-password"
							name="sudo_password"
							type={showPassword ? 'text' : 'password'}
							autocomplete="current-password"
							placeholder="Masukkan password sudo..."
							aria-invalid={hasValidationError ? 'true' : undefined}
							bind:value={password}
							onblur={() => (touched = true)}
							oninput={() => {
								if (password.length > 0) touched = false;
							}}
							required
						/>
						<InputGroup.Addon align="inline-end">
							<Button
								type="button"
								variant="ghost"
								size="icon-xs"
								aria-pressed={showPassword}
								aria-label={showPassword ? 'Sembunyikan password' : 'Tampilkan password'}
								onclick={() => (showPassword = !showPassword)}
							>
								{#if showPassword}
									<EyeOffIcon />
								{:else}
									<EyeIcon />
								{/if}
							</Button>
						</InputGroup.Addon>
					</InputGroup.Root>
					{#if hasValidationError}
						<Field.FieldError errors={[{ message: 'Password sudo wajib diisi.' }]} />
					{/if}
				</Field.Field>
			</Field.FieldGroup>

			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={handleCancel}>Batal</Button>
				<Button type="submit">Lanjutkan</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
