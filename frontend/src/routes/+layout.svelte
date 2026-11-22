<script lang="ts">
	import './layout.css';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ModeWatcher } from 'mode-watcher';
	import { Toaster } from '#lib/components/ui/sonner/index.js';
	import * as Sidebar from '#lib/components/ui/sidebar/index.js';
	import AppSidebar from '#lib/components/layout/AppSidebar.svelte';
	import Topbar from '#lib/components/layout/Topbar.svelte';
	import TerminalDialog from '#lib/components/modals/TerminalDialog.svelte';
	import SudoDialog from '#lib/components/modals/SudoDialog.svelte';
	import ConfirmDialog from '#lib/components/modals/ConfirmDialog.svelte';
	import ServiceConfigModal from '#lib/components/modals/ServiceConfigModal.svelte';
	import ChangeCredsModal from '#lib/components/modals/ChangeCredsModal.svelte';
	import PanelDomainModal from '#lib/components/modals/PanelDomainModal.svelte';
	import ModeSwitchModal from '#lib/components/modals/ModeSwitchModal.svelte';
	import Login from '#lib/components/auth/Login.svelte';
	import WdevLogo from '#lib/components/shared/WdevLogo.svelte';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { themeStore } from '#lib/stores/theme.svelte.js';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import {
		DEFAULT_SEO_DESCRIPTION,
		DEFAULT_SEO_TITLE,
		getPageMeta,
		syncMetaDescription
	} from '#lib/page-meta.js';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

	const seoHead = $derived.by(() => {
		if (!authStore.authChecked) {
			return {
				title: DEFAULT_SEO_TITLE,
				description: DEFAULT_SEO_DESCRIPTION
			};
		}
		if (authStore.authEnabled && !authStore.isAuthenticated) {
			return {
				title: 'Masuk — wdev Control Plane',
				description: 'Masuk ke panel kontrol server wdev untuk mengelola infrastruktur dan layanan Anda.'
			};
		}
		const meta = getPageMeta(page.url.pathname, modeStore.mode);
		return {
			title: meta.seoTitle,
			description: meta.description
		};
	});

	$effect(() => {
		syncMetaDescription(seoHead.description);
	});

	onMount(() => {
		themeStore.init();
		authStore.checkAuth();

		return () => {
			cp.stopPollingAndSSE();
		};
	});

	$effect(() => {
		if (authStore.authChecked) {
			if (authStore.isAuthenticated) {
				cp.startPollingAndSSE();
				modeStore.fetchMode().then((m) => {
					if (m && !m.is_set) {
						modeStore.openModal();
					}
				});
			} else {
				cp.stopPollingAndSSE();
			}
		}
	});
</script>

<svelte:head>
	<title>{seoHead.title}</title>
</svelte:head>

<ModeWatcher />
<Toaster richColors position="bottom-right" />

{#if !authStore.authChecked}
	<div class="flex min-h-screen w-full items-center justify-center bg-background">
		<div class="flex flex-col items-center gap-3.5">
			<WdevLogo class="size-12 shadow-sm" decorative={false} />
			<div class="flex items-center gap-2 text-xs text-muted-foreground">
				<Spinner class="size-3.5 text-primary" />
				<span>Memuat panel wdev...</span>
			</div>
		</div>
	</div>
{:else if authStore.authEnabled && !authStore.isAuthenticated}
	<Login />
{:else}
	<a
		href="#main-content"
		class="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-md focus:bg-primary focus:px-3 focus:py-2 focus:text-sm focus:font-medium focus:text-primary-foreground"
	>
		Lompat ke konten utama
	</a>

	<Sidebar.Provider>
		<AppSidebar />
		<Sidebar.Inset class="min-w-0">
			<Topbar />
			<div id="main-content" tabindex="-1" class="flex-1 px-3 py-4 sm:px-4 md:p-6">
				<div class="mx-auto flex w-full max-w-7xl flex-col gap-5 sm:gap-6">
					{@render children()}
				</div>
			</div>
		</Sidebar.Inset>
	</Sidebar.Provider>

	<ServiceConfigModal />
	<ChangeCredsModal />
	<PanelDomainModal />
	<ModeSwitchModal />
	<TerminalDialog />
	<SudoDialog />
	<ConfirmDialog />
{/if}
