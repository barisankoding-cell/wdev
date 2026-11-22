<script lang="ts">
	import { onDestroy, tick } from 'svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Collapsible from '#lib/components/ui/collapsible/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import { themeStore, type ThemePreference } from '#lib/stores/theme.svelte.js';
	import WdevLogo from '#lib/components/shared/WdevLogo.svelte';
	import { cn } from '#lib/utils.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import CheckIcon from '@lucide/svelte/icons/check';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import EyeIcon from '@lucide/svelte/icons/eye';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';
	import LockIcon from '@lucide/svelte/icons/lock';
	import LogInIcon from '@lucide/svelte/icons/log-in';
	import MonitorIcon from '@lucide/svelte/icons/monitor';
	import MoonIcon from '@lucide/svelte/icons/moon';
	import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
	import SunIcon from '@lucide/svelte/icons/sun';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import UserIcon from '@lucide/svelte/icons/user';

	const themeOptions: { value: ThemePreference; label: string }[] = [
		{ value: 'system', label: 'Ikuti Sistem' },
		{ value: 'light', label: 'Mode Terang' },
		{ value: 'dark', label: 'Mode Gelap' }
	];

	let username = $state('');
	let password = $state('');
	let showPassword = $state(false);
	let capsLockOn = $state(false);
	let recoveryOpen = $state(false);

	// Client-side validation state synced with interaction timing (blur / submit)
	let usernameDirty = $state(false);
	let usernameBlurred = $state(false);
	let passwordDirty = $state(false);
	let passwordBlurred = $state(false);
	let submitAttempted = $state(false);

	// Server-side authentication error state
	let serverError = $state('');
	let serverErrorStatus = $state(0);

	// Polite screen-reader live region announcement (debounced after blur)
	let liveAnnouncement = $state('');
	let announceTimer: ReturnType<typeof setTimeout> | null = null;
	let lastAnnouncedField = '';

	let usernameInputRef = $state<HTMLInputElement | null>(null);
	let passwordInputRef = $state<HTMLInputElement | null>(null);
	let alertRef = $state<HTMLDivElement | null>(null);

	const usernameErrorText = $derived.by(() => {
		if (!username.length) return 'Masukkan username panel Anda.';
		if (!username.trim()) return 'Username tidak boleh hanya berisi spasi.';
		return '';
	});

	const passwordErrorText = $derived.by(() => {
		if (!password) return 'Masukkan kata sandi panel Anda.';
		return '';
	});

	const usernameInvalid = $derived(
		Boolean(usernameErrorText) && ((usernameDirty && usernameBlurred) || submitAttempted)
	);

	const passwordInvalid = $derived(
		Boolean(passwordErrorText) && ((passwordDirty && passwordBlurred) || submitAttempted)
	);

	const passwordDescribedBy = $derived.by(() => {
		const ids: string[] = [];
		if (capsLockOn) ids.push('login-capslock-hint');
		if (passwordInvalid) ids.push('login-password-error');
		return ids.length ? ids.join(' ') : undefined;
	});

	const isLockedOut = $derived(serverErrorStatus === 429);

	onDestroy(() => {
		if (announceTimer) clearTimeout(announceTimer);
	});

	function scheduleFieldAnnouncement(fieldKey: 'username' | 'password', label: string, msg: string) {
		if (!msg) return;
		if (announceTimer) clearTimeout(announceTimer);
		liveAnnouncement = '';
		lastAnnouncedField = fieldKey;
		// Delay slightly so screen reader finishes announcing the next focused element first.
		announceTimer = setTimeout(() => {
			liveAnnouncement = `${label}: ${msg}`;
		}, 500);
	}

	function clearFieldAnnouncement(fieldKey: 'username' | 'password') {
		if (lastAnnouncedField === fieldKey) {
			if (announceTimer) clearTimeout(announceTimer);
			liveAnnouncement = '';
			lastAnnouncedField = '';
		}
	}

	function syncUsernameCustomValidity(val: string) {
		if (!usernameInputRef) return;
		if (val.length > 0 && !val.trim()) {
			usernameInputRef.setCustomValidity('Username tidak boleh hanya berisi spasi.');
		} else {
			usernameInputRef.setCustomValidity('');
		}
	}

	function handleUsernameInput() {
		usernameDirty = true;
		syncUsernameCustomValidity(username);
		if (!usernameErrorText) {
			clearFieldAnnouncement('username');
		}
	}

	function handleUsernameBlur() {
		usernameBlurred = true;
		syncUsernameCustomValidity(username);
		if (usernameInvalid && usernameErrorText) {
			scheduleFieldAnnouncement('username', 'Username', usernameErrorText);
		}
	}

	function handlePasswordInput() {
		passwordDirty = true;
		if (!passwordErrorText) {
			clearFieldAnnouncement('password');
		}
	}

	function handlePasswordBlur() {
		passwordBlurred = true;
		capsLockOn = false;
		if (passwordInvalid && passwordErrorText) {
			scheduleFieldAnnouncement('password', 'Kata Sandi', passwordErrorText);
		}
	}

	function detectCapsLock(e: KeyboardEvent) {
		if (typeof e.getModifierState === 'function') {
			capsLockOn = e.getModifierState('CapsLock');
		}
	}

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (authStore.loading) return;

		submitAttempted = true;
		serverError = '';
		serverErrorStatus = 0;
		syncUsernameCustomValidity(username);

		if (usernameErrorText) {
			await tick();
			usernameInputRef?.focus();
			return;
		}

		if (passwordErrorText) {
			await tick();
			passwordInputRef?.focus();
			return;
		}

		const result = await authStore.login(username.trim(), password);
		if (!result.ok) {
			serverError =
				result.error || 'Autentikasi gagal. Periksa kembali username dan kata sandi Anda.';
			serverErrorStatus = result.status ?? 0;
			if (result.status === 429) {
				recoveryOpen = true;
			}
			await tick();
			alertRef?.focus();
		}
	}
</script>

<svelte:head>
	<title>Masuk — wdev Control Plane</title>
</svelte:head>

<div class="login-shell grid min-h-[100dvh] w-full bg-background text-foreground lg:grid-cols-[minmax(360px,430px)_1fr]">
	<!-- Desktop Architectural Infrastructure Rail -->
	<aside
		aria-label="Informasi Control Plane"
		class="login-rail relative hidden border-r border-border bg-terminal p-8 text-terminal-foreground lg:flex lg:flex-col lg:justify-between xl:p-10"
	>
		<div class="relative z-10 flex flex-col gap-10">
			<div class="flex items-center gap-3">
				<WdevLogo class="size-9" />
				<div class="flex min-w-0 flex-col">
					<span class="text-base font-semibold tracking-tight text-terminal-foreground">wdev</span>
					<span class="text-xs text-terminal-foreground/70">Server Control Plane</span>
				</div>
			</div>

			<div class="flex flex-col gap-3">
				<h2 class="text-xl leading-snug font-semibold tracking-tight text-balance text-terminal-foreground">
					Satu portal kendali untuk layanan server Anda.
				</h2>
				<p class="max-w-[36ch] text-sm leading-relaxed text-pretty text-terminal-foreground/75">
					Masuk menggunakan akun administrator berwenang untuk mengelola layanan dan konfigurasi sistem secara terpusat.
				</p>
			</div>
		</div>

		<footer class="relative z-10 flex flex-col gap-1 text-xs text-terminal-foreground/65">
			<p>Akses terbatas hanya untuk administrator yang berwenang.</p>
		</footer>
	</aside>

	<!-- Primary Auth Workspace -->
	<div class="flex min-h-[100dvh] min-w-0 flex-col bg-background">
		<header class="flex items-center justify-between gap-3 px-4 pt-[max(1rem,env(safe-area-inset-top,0px))] pb-2 sm:px-8 sm:pt-6 lg:justify-end">
			<div class="flex items-center gap-2.5 lg:hidden">
				<WdevLogo class="size-8" />
				<div class="flex min-w-0 flex-col">
					<span class="text-sm leading-tight font-semibold tracking-tight">wdev</span>
					<span class="text-xs leading-tight text-muted-foreground">Control Plane</span>
				</div>
			</div>

			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<Button
							variant="outline"
							size="sm"
							class="min-h-9 px-3"
							aria-label="Pilih tema tampilan"
							{...props}
						>
							{#if themeStore.resolved === 'dark'}
								<MoonIcon data-icon="inline-start" />
							{:else}
								<SunIcon data-icon="inline-start" />
							{/if}
							<span>Tema</span>
						</Button>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="end">
					<DropdownMenu.Group>
						<DropdownMenu.Label>Tema Tampilan</DropdownMenu.Label>
						<DropdownMenu.Separator />
						{#each themeOptions as opt (opt.value)}
							<DropdownMenu.Item onclick={() => themeStore.setPreference(opt.value)}>
								{#if opt.value === 'system'}
									<MonitorIcon />
								{:else if opt.value === 'light'}
									<SunIcon />
								{:else}
									<MoonIcon />
								{/if}
								<span>{opt.label}</span>
								{#if themeStore.preference === opt.value}
									<CheckIcon class="ml-auto" />
								{/if}
							</DropdownMenu.Item>
						{/each}
					</DropdownMenu.Group>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</header>

		<main class="flex min-w-0 flex-1 flex-col items-center justify-start px-4 pt-6 pb-[max(2rem,env(safe-area-inset-bottom,0px))] sm:justify-center sm:px-8 sm:py-10">
			<div class="flex min-w-0 w-full max-w-[400px] flex-col gap-6">
				<div class="flex flex-col gap-1.5 text-left">
					<h1 class="text-2xl font-semibold tracking-tight text-balance">Masuk ke Panel</h1>
					<p class="text-sm leading-relaxed text-pretty text-muted-foreground">
						Masukkan kredensial administrator untuk mengakses control plane server.
					</p>
				</div>

				{#if serverError}
					<Alert.Root
						bind:ref={alertRef}
						tabindex={-1}
						variant="destructive"
						class="border-destructive/50 bg-destructive/5 focus:outline-none focus-visible:ring-2 focus-visible:ring-destructive/40"
					>
						{#if isLockedOut}
							<ShieldAlertIcon />
						{:else}
							<AlertCircleIcon />
						{/if}
						<Alert.Title>{isLockedOut ? 'Akses Dikunci Sementara' : 'Autentikasi Gagal'}</Alert.Title>
						<Alert.Description class="text-xs leading-relaxed">
							{serverError}
						</Alert.Description>
					</Alert.Root>
				{/if}

				<form method="post" novalidate onsubmit={handleSubmit} class="flex flex-col gap-5">
					<Field.FieldGroup class="gap-4">
						<Field.Field
							data-invalid={usernameInvalid ? 'true' : undefined}
							data-disabled={authStore.loading ? 'true' : undefined}
						>
							<Field.FieldLabel for="login-username" class="text-sm font-medium">
								Username <span aria-hidden="true" class="text-destructive">*</span>
							</Field.FieldLabel>
							<InputGroup.Root class="login-input-group min-h-11 h-auto overflow-hidden">
								<InputGroup.Addon align="inline-start">
									<UserIcon />
								</InputGroup.Addon>
								<InputGroup.Input
									id="login-username"
									name="username"
									type="text"
									placeholder="Contoh: admin"
									bind:ref={usernameInputRef}
									bind:value={username}
									disabled={authStore.loading}
									required
									autocomplete="username"
									autocorrect="off"
									autocapitalize="none"
									spellcheck={false}
									enterkeyhint="next"
									aria-invalid={usernameInvalid ? 'true' : 'false'}
									aria-describedby={usernameInvalid ? 'login-username-error' : undefined}
									class="h-auto py-2.5 text-base"
									oninput={handleUsernameInput}
									onblur={handleUsernameBlur}
								/>
							</InputGroup.Root>
							{#if usernameInvalid}
								<Field.FieldError id="login-username-error" role={undefined} class="flex items-start gap-1.5 text-xs">
									<AlertCircleIcon aria-hidden="true" class="mt-0.5 size-3.5 shrink-0" />
									<span>{usernameErrorText}</span>
								</Field.FieldError>
							{/if}
						</Field.Field>

						<Field.Field
							data-invalid={passwordInvalid ? 'true' : undefined}
							data-disabled={authStore.loading ? 'true' : undefined}
						>
							<div class="flex min-h-5 items-center justify-between gap-2">
								<Field.FieldLabel for="login-password" class="text-sm font-medium">
									Kata Sandi <span aria-hidden="true" class="text-destructive">*</span>
								</Field.FieldLabel>
								{#if capsLockOn}
									<Badge
										id="login-capslock-hint"
										variant="outline"
										class="border-warning/50 bg-warning/15 text-foreground"
									>
										<TriangleAlertIcon aria-hidden="true" class="text-warning" />
										<span>Caps Lock aktif</span>
									</Badge>
								{/if}
							</div>
							<InputGroup.Root class="login-input-group min-h-11 h-auto overflow-hidden">
								<InputGroup.Addon align="inline-start">
									<LockIcon />
								</InputGroup.Addon>
								<InputGroup.Input
									id="login-password"
									name="password"
									type={showPassword ? 'text' : 'password'}
									bind:ref={passwordInputRef}
									bind:value={password}
									disabled={authStore.loading}
									required
									autocomplete="current-password"
									enterkeyhint="done"
									aria-invalid={passwordInvalid ? 'true' : 'false'}
									aria-describedby={passwordDescribedBy}
									class="h-auto py-2.5 text-base"
									oninput={handlePasswordInput}
									onblur={handlePasswordBlur}
									onkeydown={detectCapsLock}
									onkeyup={detectCapsLock}
								/>
								<InputGroup.Addon align="inline-end">
									<InputGroup.Button
										size="icon-sm"
										class="min-h-8 min-w-8 fm-touch-btn"
										disabled={authStore.loading}
										aria-pressed={showPassword}
										aria-controls="login-password"
										aria-label={showPassword ? 'Sembunyikan kata sandi' : 'Tampilkan kata sandi'}
										aria-describedby="login-password-visibility-note"
										onclick={() => (showPassword = !showPassword)}
									>
										{#if showPassword}
											<EyeOffIcon />
										{:else}
											<EyeIcon />
										{/if}
									</InputGroup.Button>
								</InputGroup.Addon>
							</InputGroup.Root>
							<span id="login-password-visibility-note" class="login-visually-hidden">
								Perhatian: mengaktifkan ini akan menampilkan kata sandi di layar.
							</span>
							{#if passwordInvalid}
								<Field.FieldError id="login-password-error" role={undefined} class="flex items-start gap-1.5 text-xs">
									<AlertCircleIcon aria-hidden="true" class="mt-0.5 size-3.5 shrink-0" />
									<span>{passwordErrorText}</span>
								</Field.FieldError>
							{/if}
						</Field.Field>
					</Field.FieldGroup>

					<Button
						type="submit"
						size="lg"
						class="min-h-11 h-auto w-full py-2.5 text-sm font-medium"
						disabled={authStore.loading}
					>
						{#if authStore.loading}
							<Spinner data-icon="inline-start" />
							<span>Memverifikasi...</span>
						{:else}
							<LogInIcon data-icon="inline-start" />
							<span>Masuk ke Panel</span>
						{/if}
					</Button>
				</form>

				<Separator />

				<!-- Administrator Access Recovery Note (placed below submit button so mobile keyboards never cover primary actions) -->
				<Collapsible.Root bind:open={recoveryOpen} class="flex min-w-0 w-full flex-col gap-3">
					<Collapsible.Trigger>
						{#snippet child({ props })}
							<Button
								variant="ghost"
								size="sm"
								class="min-h-10 h-auto w-full justify-between px-2 py-2 text-left text-xs whitespace-normal text-muted-foreground hover:text-foreground"
								{...props}
							>
								<span class="flex min-w-0 items-center gap-2">
									<TerminalIcon class="size-3.5 shrink-0" aria-hidden="true" />
									<span>Lupa kredensial atau butuh pemulihan akses?</span>
								</span>
								<ChevronDownIcon
									data-icon="inline-end"
									class={cn('shrink-0 transition-transform duration-200', recoveryOpen && 'rotate-180')}
								/>
							</Button>
						{/snippet}
					</Collapsible.Trigger>

					<Collapsible.Content class="min-w-0 rounded-lg border border-border bg-muted/40 p-3.5">
						<p class="text-xs leading-relaxed text-muted-foreground">
							Untuk alasan keamanan, pemulihan atau pengaturan ulang kredensial hanya dapat dilakukan langsung oleh administrator melalui terminal SSH server dengan hak akses root atau sudo.
						</p>
					</Collapsible.Content>
				</Collapsible.Root>
			</div>
		</main>
	</div>
</div>

<!-- Single page-level polite live region for deferred blur validation announcements -->
<div class="login-visually-hidden" aria-live="polite" aria-atomic="true">
	{liveAnnouncement}
</div>

<style>
	/* Accessible >= 3:1 input boundary contrast (WCAG 1.4.11) + autofill theme harmonization */
	.login-shell {
		--input: oklch(0.54 0.02 255);
		--login-autofill-bg: #eef4ff;
		--login-autofill-fg: oklch(0.145 0.015 260);
	}

	:global(.dark) .login-shell {
		--input: oklch(0.58 0.02 255);
		--login-autofill-bg: #182438;
		--login-autofill-fg: oklch(0.96 0.006 245);
	}

	/* Keep visually-hidden helpers strictly 1x1 within viewport so Field [&>.sr-only]:w-auto cannot cause mobile overflow */
	:global(.login-visually-hidden) {
		position: fixed !important;
		inset-block-start: 0 !important;
		inset-inline-start: 0 !important;
		clip-path: inset(50%) !important;
		overflow: hidden !important;
		width: 1px !important;
		height: 1px !important;
		margin: 0 !important;
		padding: 0 !important;
		border: 0 !important;
		white-space: nowrap !important;
		pointer-events: none !important;
	}

	.login-rail {
		background-image:
			linear-gradient(to right, oklch(1 0 0 / 4%) 1px, transparent 1px),
			linear-gradient(to bottom, oklch(1 0 0 / 4%) 1px, transparent 1px);
		background-size: 2rem 2rem;
	}

	/* Keep autofilled fields legible across light/dark themes with a non-color 2px inset ring */
	.login-shell :global([data-slot='input-group']:has(input:is(:autofill, :-webkit-autofill))) {
		background-color: var(--login-autofill-bg);
		box-shadow: inset 0 0 0 2px var(--primary);
	}

	.login-shell :global([data-slot='input-group']:has(input:is(:autofill, :-webkit-autofill):focus-visible)) {
		outline: 2px solid var(--ring);
		outline-offset: 2px;
	}

	.login-shell :global(input:is(:autofill, :-webkit-autofill)) {
		box-shadow: inset 0 0 0 100vmax var(--login-autofill-bg);
		-webkit-text-fill-color: var(--login-autofill-fg);
		caret-color: var(--login-autofill-fg);
	}

	@media (pointer: coarse) {
		.login-shell :global(.login-input-group) {
			min-block-size: 3rem;
		}
	}

	@media (forced-colors: active) {
		.login-shell :global([data-slot='input-group']) {
			border: 1px solid ButtonBorder;
		}

		.login-shell :global([data-slot='input-group']:has([data-slot='input-group-control']:focus-visible)) {
			outline: 2px solid Highlight;
			outline-offset: 2px;
		}

		.login-shell :global([data-slot='input-group']:has([aria-invalid='true'])) {
			border-color: Mark;
		}
	}
</style>
