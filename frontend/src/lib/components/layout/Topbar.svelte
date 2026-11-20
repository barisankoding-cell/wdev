<script lang="ts">
	import { page } from '$app/state';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import * as Sidebar from '#lib/components/ui/sidebar/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { themeStore, type ThemePreference } from '#lib/stores/theme.svelte.js';
	import { cn } from '#lib/utils.js';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import SunIcon from '@lucide/svelte/icons/sun';
	import MoonIcon from '@lucide/svelte/icons/moon';
	import MonitorIcon from '@lucide/svelte/icons/monitor';
	import CheckIcon from '@lucide/svelte/icons/check';

	const PAGE_TITLES: Record<string, string> = {
		'/': 'Status & Pembaruan Sistem',
		'/webstack': 'Web Stack & Virtual Host',
		'/switchers': 'Pengelola Multi-Versi Runtime',
		'/toolchains': 'Toolchains, Compilers & SDKs',
		'/workspace': 'Workspace, ADE & Shell',
		'/powerdns': 'PowerDNS & VPS Edge Gateway',
		'/cloudflare': 'Cloudflare Tunnel & DNS',
		'/mail': 'Mail Server & Outbound Relay',
		'/setup': 'Instalasi, Modular & Riwayat Tugas'
	};

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
		<div class="hidden items-center gap-1.5 lg:flex">
			<Badge variant="outline" class="font-mono text-xs">
				NetBird: {cp.systemStatus?.netbird_ip || '-'}
			</Badge>
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
