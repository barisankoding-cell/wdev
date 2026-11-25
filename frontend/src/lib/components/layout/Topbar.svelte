<script lang="ts">
	import { page } from '$app/state';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import * as Sidebar from '#lib/components/ui/sidebar/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import LanguageSwitcher from '#lib/components/shared/LanguageSwitcher.svelte';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import { modeStore, MODE_LABELS } from '#lib/stores/mode.svelte.js';
	import { themeStore, type ThemePreference } from '#lib/stores/theme.svelte.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { getPageMeta } from '#lib/page-meta.js';
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
	import GlobeIcon from '@lucide/svelte/icons/globe';

	const pageTitle = $derived(getPageMeta(page.url.pathname, modeStore.mode).title);

	const activeModeLabel = $derived(
		modeStore.mode === 'vps'
			? MODE_LABELS.vps
			: modeStore.mode === 'local'
				? t('layout.nav.footer.modeLocalShort')
				: MODE_LABELS.edge
	);

	const themeOptions = $derived<{ value: ThemePreference; label: string }[]>([
		{ value: 'system', label: t('common.theme.system') },
		{ value: 'light', label: t('common.theme.light') },
		{ value: 'dark', label: t('common.theme.dark') }
	]);
</script>

<header
	class="sticky top-0 z-20 flex min-h-14 shrink-0 items-center justify-between gap-2 border-b bg-background/95 pt-[env(safe-area-inset-top,0px)] pl-[max(0.75rem,env(safe-area-inset-left,0px))] pr-[max(0.75rem,env(safe-area-inset-right,0px))] backdrop-blur supports-[backdrop-filter]:bg-background/80 sm:gap-3 sm:pl-[max(1rem,env(safe-area-inset-left,0px))] sm:pr-[max(1rem,env(safe-area-inset-right,0px))]"
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
			onclick={() => cp.openTerminal()}
			class="relative"
			aria-label={t('layout.topbar.openTerminalAria')}
		>
			<TerminalIcon data-icon="inline-start" />
			<span class="hidden sm:inline">{t('layout.topbar.terminalBtn')}</span>
			{#if cp.isTaskRunning}
				<span
					class="ml-1 inline-block size-2 animate-pulse rounded-full bg-success"
					aria-label={t('layout.topbar.taskRunningAria')}
				></span>
			{/if}
		</Button>

		<LanguageSwitcher size="sm" class="h-8 px-2" />

		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<Button variant="outline" size="icon-sm" aria-label={t('common.theme.ariaLabel')} {...props}>
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
					<DropdownMenu.Label>{t('common.theme.label')}</DropdownMenu.Label>
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

		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<Button variant="outline" size="sm" class="gap-1.5 px-2.5" {...props}>
						<UserIcon class="size-3.5 text-muted-foreground" />
						<span class="max-w-[90px] truncate text-xs font-medium sm:max-w-none">
							{authStore.authEnabled ? authStore.username : t('layout.topbar.panelMenuBtn')}
						</span>
					</Button>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="end" class="w-60">
				<DropdownMenu.Group>
					<DropdownMenu.Label class="text-xs text-muted-foreground">
						{#if authStore.authEnabled}
							{t('layout.topbar.signedInAs')} <strong class="text-foreground">{authStore.username}</strong>
						{:else}
							{t('layout.topbar.panelSettingsLabel')}
						{/if}
					</DropdownMenu.Label>
					<DropdownMenu.Separator />
					<DropdownMenu.Item onclick={() => modeStore.openModal()}>
						{#if modeStore.mode === 'vps'}
							<ServerIcon class="size-4 text-primary" />
						{:else if modeStore.mode === 'local'}
							<LaptopIcon class="size-4 text-primary" />
						{:else}
							<NetworkIcon class="size-4 text-primary" />
						{/if}
						<span>{t('layout.topbar.serverMode')}</span>
						<span class="ml-auto rounded bg-primary/10 px-1.5 py-0.5 text-[11px] font-semibold text-primary">
							{activeModeLabel}
						</span>
					</DropdownMenu.Item>
					<DropdownMenu.Item onclick={() => authStore.openPanelDomain()}>
						<GlobeIcon class="size-4" />
						<span>{t('layout.topbar.panelDomainHttps')}</span>
					</DropdownMenu.Item>
					{#if authStore.authEnabled}
						<DropdownMenu.Item onclick={() => authStore.openChangeCreds()}>
							<KeyRoundIcon class="size-4" />
							<span>{t('layout.topbar.changeCreds')}</span>
						</DropdownMenu.Item>
						<DropdownMenu.Separator />
						<DropdownMenu.Item onclick={() => authStore.logout()} class="text-destructive focus:text-destructive">
							<LogOutIcon class="size-4" />
							<span>{t('layout.topbar.signOut')}</span>
						</DropdownMenu.Item>
					{/if}
				</DropdownMenu.Group>
			</DropdownMenu.Content>
		</DropdownMenu.Root>

		<Button
			variant="outline"
			size="icon-sm"
			aria-label={t('common.actions.refresh')}
			title={t('common.actions.refresh')}
			disabled={cp.statusLoading}
			onclick={() => {
				cp.loadStatus(false);
				cp.refreshTick += 1;
			}}
		>
			<RefreshCwIcon class={cn(cp.statusLoading && 'animate-spin')} />
		</Button>
	</div>
</header>
