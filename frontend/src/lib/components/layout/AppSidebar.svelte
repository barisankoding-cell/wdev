<script lang="ts">
	import { page } from '$app/state';
	import * as Sidebar from '#lib/components/ui/sidebar/index.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { useSidebar } from '#lib/components/ui/sidebar/index.js';
	import WdevLogo from '#lib/components/shared/WdevLogo.svelte';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import ContainerIcon from '@lucide/svelte/icons/container';
	import EthernetPortIcon from '@lucide/svelte/icons/ethernet-port';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import ArrowLeftRightIcon from '@lucide/svelte/icons/arrow-left-right';
	import WrenchIcon from '@lucide/svelte/icons/wrench';
	import MonitorIcon from '@lucide/svelte/icons/monitor';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import CloudIcon from '@lucide/svelte/icons/cloud';
	import MailIcon from '@lucide/svelte/icons/mail';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import SquareTerminalIcon from '@lucide/svelte/icons/square-terminal';
	import ZapIcon from '@lucide/svelte/icons/zap';
	import ServerIcon from '@lucide/svelte/icons/server';
	import LaptopIcon from '@lucide/svelte/icons/laptop';
	import PowerIcon from '@lucide/svelte/icons/power';
	import type { Component } from 'svelte';

	interface NavItem {
		label: string;
		href: string;
		icon: Component;
	}

	interface NavGroup {
		title: string;
		items: NavItem[];
	}

	const navGroups = $derived<NavGroup[]>([
		{
			title: t('layout.nav.groups.overview'),
			items: [{ label: t('layout.nav.items.dashboard'), href: '/', icon: ActivityIcon }]
		},
		{
			title: t('layout.nav.groups.webDev'),
			items: [
				{ label: t('layout.nav.items.webstack'), href: '/webstack', icon: GlobeIcon },
				{ label: t('layout.nav.items.compose'), href: '/compose', icon: ContainerIcon },
				{ label: t('layout.nav.items.switchers'), href: '/switchers', icon: ArrowLeftRightIcon },
				{ label: t('layout.nav.items.toolchains'), href: '/toolchains', icon: WrenchIcon },
				{ label: t('layout.nav.items.workspace'), href: '/workspace', icon: MonitorIcon }
			]
		},
		{
			title: t('layout.nav.groups.systemFiles'),
			items: [
				{ label: t('layout.nav.items.ports'), href: '/ports', icon: EthernetPortIcon },
				{ label: t('layout.nav.items.files'), href: '/files', icon: FolderOpenIcon },
				{ label: t('layout.nav.items.terminal'), href: '/terminal', icon: SquareTerminalIcon }
			]
		},
		modeStore.mode === 'edge'
			? {
					title: t('layout.nav.groups.networkEdge'),
					items: [
						{ label: t('layout.nav.items.powerdnsEdge'), href: '/powerdns', icon: NetworkIcon },
						{ label: t('layout.nav.items.cloudflare'), href: '/cloudflare', icon: CloudIcon },
						{ label: t('layout.nav.items.mailEdge'), href: '/mail', icon: MailIcon }
					]
			  }
			: modeStore.mode === 'vps'
				? {
						title: t('layout.nav.groups.domainDnsMail'),
						items: [
							{ label: t('layout.nav.items.powerdnsVps'), href: '/powerdns', icon: GlobeIcon },
							{ label: t('layout.nav.items.cloudflare'), href: '/cloudflare', icon: CloudIcon },
							{ label: t('layout.nav.items.mailVps'), href: '/mail', icon: MailIcon }
						]
				  }
				: {
						title: t('layout.nav.groups.optionalExpose'),
						items: [{ label: t('layout.nav.items.cloudflare'), href: '/cloudflare', icon: CloudIcon }]
				  },
		{
			title: t('layout.nav.groups.installSetup'),
			items: [{ label: t('layout.nav.items.setup'), href: '/setup', icon: ZapIcon }]
		}
	]);

	const sidebar = useSidebar();

	function isRouteActive(href: string): boolean {
		const path = page.url.pathname;
		if (href === '/') return path === '/';
		return path === href || path.startsWith(`${href}/`);
	}
</script>

<Sidebar.Root collapsible="icon">
	<Sidebar.Header class="border-b border-sidebar-border px-3 py-3 group-data-[collapsible=icon]:px-2">
		<div class="flex items-center gap-3 overflow-hidden group-data-[collapsible=icon]:justify-center">
			<WdevLogo class="size-8" />
			<div class="flex min-w-0 flex-col group-data-[collapsible=icon]:hidden">
				<span class="truncate text-sm font-bold tracking-tight">wdev</span>
				<span class="truncate text-xs text-muted-foreground">
					Control Plane {cp.systemStatus?.wdev_version ? `v${cp.systemStatus.wdev_version}` : ''}
				</span>
			</div>
		</div>
	</Sidebar.Header>

	<Sidebar.Content>
		{#each navGroups as group (group.title)}
			<Sidebar.Group>
				<Sidebar.GroupLabel>{group.title}</Sidebar.GroupLabel>
				<Sidebar.GroupContent>
					<Sidebar.Menu>
						{#each group.items as item (item.href)}
							{@const Icon = item.icon}
							<Sidebar.MenuItem>
								<Sidebar.MenuButton isActive={isRouteActive(item.href)} tooltipContent={item.label}>
									{#snippet child({ props })}
										<a
											href={item.href}
											{...props}
											onclick={() => {
												if (sidebar.isMobile) {
													sidebar.setOpenMobile(false);
												}
											}}
										>
											<Icon />
											<span>{item.label}</span>
										</a>
									{/snippet}
								</Sidebar.MenuButton>
							</Sidebar.MenuItem>
						{/each}
					</Sidebar.Menu>
				</Sidebar.GroupContent>
			</Sidebar.Group>
		{/each}
	</Sidebar.Content>

	<Sidebar.Footer class="border-t border-sidebar-border p-3 group-data-[collapsible=icon]:p-2 text-xs text-muted-foreground">
		<div class="flex items-center justify-between gap-2 group-data-[collapsible=icon]:hidden">
			<div class="flex flex-col gap-0.5 truncate">
				<span class="truncate font-medium text-sidebar-foreground">
					{cp.systemStatus?.hostname || t('layout.nav.footer.defaultHost')}
				</span>
				<span class="truncate">{cp.systemStatus?.os_name || t('layout.nav.footer.defaultSub')}</span>
			</div>
			<button
				type="button"
				onclick={() => {
					if (sidebar.isMobile) sidebar.setOpenMobile(false);
					modeStore.openModal();
				}}
				class="inline-flex sm:hidden shrink-0 items-center gap-1.5 rounded-md border border-sidebar-border px-2 py-1 text-[11px] font-medium text-sidebar-foreground hover:bg-sidebar-accent"
				aria-label={t('layout.nav.footer.changeModeAria')}
			>
				{#if modeStore.mode === 'vps'}
					<ServerIcon class="size-3 text-primary" />
					<span>{t('layout.nav.footer.modeVpsShort')}</span>
				{:else if modeStore.mode === 'local'}
					<LaptopIcon class="size-3 text-primary" />
					<span>{t('layout.nav.footer.modeLocalShort')}</span>
				{:else}
					<NetworkIcon class="size-3 text-primary" />
					<span>{t('layout.nav.footer.modeEdgeShort')}</span>
				{/if}
			</button>
		</div>
		<Sidebar.Menu>
			<Sidebar.MenuItem>
				<Sidebar.MenuButton
					variant="outline"
					tooltipContent={t('layout.nav.footer.restartSystem')}
					class="text-destructive hover:bg-destructive/10 hover:text-destructive dark:hover:bg-destructive/20"
					aria-label={t('layout.nav.footer.restartSystemAria')}
					onclick={() => {
						if (sidebar.isMobile) sidebar.setOpenMobile(false);
						void cp.restartSystem();
					}}
				>
					{#snippet child({ props })}
						<button type="button" disabled={cp.systemRestarting} {...props}>
							{#if cp.systemRestarting}
								<Spinner />
								<span>{t('layout.nav.footer.restartingSystem')}</span>
							{:else}
								<PowerIcon />
								<span>{t('layout.nav.footer.restartSystem')}</span>
							{/if}
						</button>
					{/snippet}
				</Sidebar.MenuButton>
			</Sidebar.MenuItem>
		</Sidebar.Menu>
	</Sidebar.Footer>
	<Sidebar.Rail />
</Sidebar.Root>
