<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import UserIcon from '@lucide/svelte/icons/user';
	import LockIcon from '@lucide/svelte/icons/lock';
	import EyeIcon from '@lucide/svelte/icons/eye';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';
	import LogInIcon from '@lucide/svelte/icons/log-in';
	import TerminalIcon from '@lucide/svelte/icons/terminal';

	let username = $state('');
	let password = $state('');
	let showPassword = $state(false);
	let errorMessage = $state('');

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		errorMessage = '';
		if (!username.trim() || !password) {
			errorMessage = 'Username dan kata sandi wajib diisi.';
			return;
		}

		const ok = await authStore.login(username.trim(), password);
		if (!ok) {
			errorMessage = 'Autentikasi gagal. Periksa kembali username dan kata sandi Anda.';
		}
	}
</script>

<div class="flex min-h-screen w-full items-center justify-center bg-background px-4 py-12">
	<div class="w-full max-w-md">
		<Card.Root class="border-border/60 shadow-xl backdrop-blur">
			<Card.Header class="space-y-2 text-center pb-6">
				<div class="mx-auto flex size-14 items-center justify-center rounded-2xl bg-primary/10 text-primary">
					<ShieldCheckIcon class="size-8" />
				</div>
				<Card.Title class="text-2xl font-bold tracking-tight">wdev Panel</Card.Title>
				<Card.Description class="text-xs sm:text-sm">
					Masuk untuk mengelola web stack, PowerDNS, mail server & infrastruktur
				</Card.Description>
			</Card.Header>

			<Card.Content>
				<form onsubmit={handleSubmit} class="space-y-4">
					{#if errorMessage}
						<Alert.Root variant="destructive" class="py-2.5">
							<Alert.Description class="text-xs">{errorMessage}</Alert.Description>
						</Alert.Root>
					{/if}

					<div class="space-y-1.5">
						<label for="username" class="text-xs font-medium text-foreground">
							Username Panel
						</label>
						<div class="relative">
							<span class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-muted-foreground">
								<UserIcon class="size-4" />
							</span>
							<Input
								id="username"
								type="text"
								placeholder="admin"
								bind:value={username}
								disabled={authStore.loading}
								autocomplete="username"
								required
								class="pl-9 text-sm"
							/>
						</div>
					</div>

					<div class="space-y-1.5">
						<label for="password" class="text-xs font-medium text-foreground">
							Kata Sandi
						</label>
						<div class="relative">
							<span class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-muted-foreground">
								<LockIcon class="size-4" />
							</span>
							<Input
								id="password"
								type={showPassword ? 'text' : 'password'}
								placeholder="••••••••••••"
								bind:value={password}
								disabled={authStore.loading}
								autocomplete="current-password"
								required
								class="pl-9 pr-9 text-sm"
							/>
							<button
								type="button"
								onclick={() => (showPassword = !showPassword)}
								class="absolute inset-y-0 right-0 flex items-center pr-3 text-muted-foreground hover:text-foreground focus:outline-none"
								aria-label={showPassword ? 'Sembunyikan sandi' : 'Tampilkan sandi'}
								tabindex="-1"
							>
								{#if showPassword}
									<EyeOffIcon class="size-4" />
								{:else}
									<EyeIcon class="size-4" />
								{/if}
							</button>
						</div>
					</div>

					<Button type="submit" class="w-full mt-2 font-medium" disabled={authStore.loading}>
						{#if authStore.loading}
							<Spinner class="mr-2 size-4" />
							<span>Memverifikasi...</span>
						{:else}
							<LogInIcon data-icon="inline-start" class="size-4" />
							<span>Masuk ke Panel</span>
						{/if}
					</Button>
				</form>

				<div class="mt-6 rounded-lg border border-border/50 bg-muted/40 p-3 text-center">
					<div class="flex items-center justify-center gap-1.5 text-xs text-muted-foreground">
						<TerminalIcon class="size-3.5" />
						<span>Lupa akun / sandi? Jalankan di terminal VPS:</span>
					</div>
					<code class="mt-1 block font-mono text-xs font-semibold text-primary select-all">
						wdev panel default
					</code>
				</div>
			</Card.Content>
		</Card.Root>

		<p class="mt-4 text-center text-xs text-muted-foreground">
			Dilindungi dengan Security Entrance & PBKDF2 Password Hashing
		</p>
	</div>
</div>
