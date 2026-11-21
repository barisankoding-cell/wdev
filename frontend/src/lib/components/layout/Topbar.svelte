<script lang="ts">
	import { page } from '$app/state';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import * as Sidebar from '#lib/components/ui/sidebar/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import { themeStore, type ThemePreference } from '#lib/stores/theme.svelte.js';
	import { cn } from '#lib/utils.js';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import SunIcon from '@lucide/svelte/icons/sun';
	import MoonIcon from '@lucide/svelte/icons/moon';
	import MonitorIcon from '@lucide/svelte/icons/monitor';
	import CheckIcon from '@lucide/svelte/icons/check';
	import UserIcon from '@lucide/svelte/icons/user';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import LogOutIcon from '@lucide/svelte/icons/log-out';
	import ServerIcon from '@lucide/svelte/icons/server';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import LaptopIcon from '@lucide/svelte/icons/laptop';

	const PAGE_TITLES = $derived<Record<string, string>>({
		'/': 'Status & Pembaruan Sistem',
		'/webstack': 'Web Stack & Virtual Host',
		'/switchers': 'Pengelola Multi-Versi Runtime',
		'/toolchains': 'Toolchains, Compilers & SDKs',
		'/workspace': 'Workspace, ADE & Shell',
		'/powerdns':
			modeStore.mode === 'edge' ? 'PowerDNS & VPS Edge Gateway' : 'Domain & DNS Manager',
		'/cloudflare': 'Cloudflare Tunnel & DNS',
		'/mail':
			modeStore.mode === 'edge'
				? 'Mail Server & Outbound Relay'
				: 'Mail Server Standalone',
		'/setup': 'Instalasi, Modular & Riwayat Tugas'
	});

	const pageTitle = $derived(PAGE_TITLES[page.url.pathname] ?? 'wdev Dashboard');

	const themeOptions: { value: ThemePreference; label: string }[] = [
		{ value: 'system', label: 'Ikuti Sistem' },
		{ value: 'light', label: 'Mode Terang' },
		{ value: 'dark', label: 'Mode Gelap' }
	];
</script>

<header
	class="sticky top-0 z-20 flex h-14 shrink-0 items-center justify-between gap-2 border-b bg-background/95 px-3 sm:gap-3 sm:px-4 backdrop-blur supports-[backdrop-filter]:bg-background/80"
>
	<div class="flex min-w-0 items-center gap-2">
		<Sidebar.Trigger class="size-8 sm:size-7" />
		<Separator orientation="vertical" class="h-5" />
		<span class="truncate text-sm font-semibold sm:text-base">{pageTitle}</span>
	</div>

	<div class="flex shrink-0 items-center gap-1.5 sm:gap-2">
		<Button
			variant="outline"
			size="sm"
			onclick={() => modeStore.openModal()}
			class="gap-1.5 px-2.5 h-8 text-xs font-medium border-primary/30 hover:border-primary"
			aria-label="Pilih Mode Operasional Server"
		>
			{#if modeStore.mode === 'vps'}
				<ServerIcon class="size-3.5 text-primary" />
				<span class="hidden sm:inline">Mode:</span>
				<span class="font-semibold text-primary">Full VPS</span>
			{:else if modeStore.mode === 'local'}
				<LaptopIcon class="size-3.5 text-primary" />
				<span class="hidden sm:inline">Mode:</span>
				<span class="font-semibold text-primary">Lokal</span>
			{:else}
				<NetworkIcon class="size-3.5 text-primary" />
				<span class="hidden sm:inline">Mode:</span>
				<span class="font-semibold text-primary">Edge Gateway</span>
			{/if}
		</Button>

		<div class="hidden items-center gap-1.5 lg:flex">
			{#if modeStore.mode === 'vps' || cp.systemStatus?.server_mode === 'vps' || !!(cp.systemStatus?.public_ip && cp.systemStatus.public_ip !== '-' && cp.systemStatus.public_ip !== '127.0.0.1')}
				{@const pubIP = cp.systemStatus?.public_ip || modeStore.publicIP || '-'}
				<Badge variant="outline" class="font-mono text-xs">
					IP VPS: {pubIP || '-'}
				</Badge>
			{:else}
				<Badge variant="outline" class="font-mono text-xs">
					NetBird: {cp.systemStatus?.netbird_ip || '-'}
				</Badge>
			{/if}
			<Badge variant="outline" class="font-mono text-xs">
				LAN: {cp.systemStatus?.lan_ip || '-'}
			</Badge>
		</div>

		<Button
			variant="outline"
			size="sm"
			onclick={() => cp.openTerminal()}
			class="relative"
			aria-label="Buka Live Terminal Console"
		>
			<TerminalIcon data-icon="inline-start" />
			<span class="hidden sm:inline">Terminal</span>
			{#if cp.isTaskRunning}
				<span
					class="ml-1 inline-block size-2 animate-pulse rounded-full bg-success"
					aria-label="Tugas sedang berjalan"
				></span>
			{/if}
		</Button>

		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<Button variant="outline" size="icon-sm" aria-label="Pilih tema tampilan" {...props}>
						{#if themeStore.resolved === 'dark'}
							<MoonIcon />
						{:else}
							<SunIcon />
						{/if}
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

		{#if authStore.authEnabled}
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<Button variant="outline" size="sm" class="gap-1.5 px-2.5" {...props}>
							<UserIcon class="size-3.5 text-muted-foreground" />
							<span class="max-w-[90px] truncate text-xs font-medium sm:max-w-none">{authStore.username}</span>
						</Button>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="end" class="w-48">
					<DropdownMenu.Group>
						<DropdownMenu.Label class="text-xs text-muted-foreground">
							Masuk sebagai <strong class="text-foreground">{authStore.username}</strong>
						</DropdownMenu.Label>
						<DropdownMenu.Separator />
						<DropdownMenu.Item onclick={() => authStore.openChangeCreds()}>
							<KeyRoundIcon class="size-4" />
							<span>Ubah Kredensial</span>
						</DropdownMenu.Item>
						<DropdownMenu.Separator />
						<DropdownMenu.Item onclick={() => authStore.logout()} class="text-destructive focus:text-destructive">
							<LogOutIcon class="size-4" />
							<span>Keluar Panel</span>
						</DropdownMenu.Item>
					</DropdownMenu.Group>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		{/if}

		<Button
			variant="outline"
			size="sm"
			disabled={cp.statusLoading}
			onclick={() => {
				cp.loadStatus(false);
				cp.refreshTick += 1;
			}}
		>
			<RefreshCwIcon data-icon="inline-start" class={cn(cp.statusLoading && 'animate-spin')} />
			<span class="hidden sm:inline">Segarkan</span>
		</Button>
	</div>
</header>
