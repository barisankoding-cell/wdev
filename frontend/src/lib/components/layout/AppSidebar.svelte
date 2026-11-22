<script lang="ts">
	import { page } from '$app/state';
	import * as Sidebar from '#lib/components/ui/sidebar/index.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import { useSidebar } from '#lib/components/ui/sidebar/index.js';
	import WdevLogo from '#lib/components/shared/WdevLogo.svelte';
	import ActivityIcon from '@lucide/svelte/icons/activity';
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
			title: 'Ringkasan',
			items: [{ label: 'Status & Update', href: '/', icon: ActivityIcon }]
		},
		{
			title: 'Web & Pengembangan',
			items: [
				{ label: 'Web Stack & VHost', href: '/webstack', icon: GlobeIcon },
				{ label: 'Multi-Versi Runtime', href: '/switchers', icon: ArrowLeftRightIcon },
				{ label: 'Toolchains & SDKs', href: '/toolchains', icon: WrenchIcon },
				{ label: 'Workspace & ADE', href: '/workspace', icon: MonitorIcon }
			]
		},
		{
			title: 'Sistem & Berkas',
			items: [
				{ label: 'File Manager', href: '/files', icon: FolderOpenIcon },
				{ label: 'Terminal SSH (PTY)', href: '/terminal', icon: SquareTerminalIcon }
			]
		},
		modeStore.mode === 'edge'
			? {
					title: 'Jaringan & Edge Gateway',
					items: [
						{ label: 'PowerDNS & Edge', href: '/powerdns', icon: NetworkIcon },
						{ label: 'Cloudflare Tunnel', href: '/cloudflare', icon: CloudIcon },
						{ label: 'Mail Server & Relay', href: '/mail', icon: MailIcon }
					]
			  }
			: modeStore.mode === 'vps'
				? {
						title: 'Domain, DNS & Mail',
						items: [
							{ label: 'Domain & DNS Manager', href: '/powerdns', icon: GlobeIcon },
							{ label: 'Cloudflare Tunnel', href: '/cloudflare', icon: CloudIcon },
							{ label: 'Mail Server', href: '/mail', icon: MailIcon }
						]
				  }
				: {
						title: 'Ekspos Opsional',
						items: [{ label: 'Cloudflare Tunnel', href: '/cloudflare', icon: CloudIcon }]
				  },
		{
			title: 'Instalasi & Setup',
			items: [{ label: 'Setup & Komponen', href: '/setup', icon: ZapIcon }]
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

	<Sidebar.Footer class="border-t border-sidebar-border p-3 text-xs text-muted-foreground">
		<div class="flex flex-col gap-0.5 truncate group-data-[collapsible=icon]:hidden">
			<span class="truncate font-medium text-sidebar-foreground">
				{cp.systemStatus?.hostname || 'Linux Host'}
			</span>
			<span class="truncate">{cp.systemStatus?.os_name || 'Control Plane'}</span>
		</div>
	</Sidebar.Footer>
	<Sidebar.Rail />
</Sidebar.Root>
