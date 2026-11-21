<script lang="ts">
	import './layout.css';
	import { onMount } from 'svelte';
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
	import ModeSwitchModal from '#lib/components/modals/ModeSwitchModal.svelte';
	import Login from '#lib/components/auth/Login.svelte';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { themeStore } from '#lib/stores/theme.svelte.js';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

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

<ModeWatcher />
<Toaster richColors position="bottom-right" />

{#if !authStore.authChecked}
	<div class="flex min-h-screen w-full items-center justify-center bg-background">
		<div class="flex flex-col items-center gap-3">
			<div class="size-8 animate-spin rounded-full border-2 border-primary border-t-transparent"></div>
			<span class="text-xs text-muted-foreground">Memuat panel wdev...</span>
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

	<TerminalDialog />
	<SudoDialog />
	<ConfirmDialog />
	<ServiceConfigModal />
	<ChangeCredsModal />
	<ModeSwitchModal />
{/if}
