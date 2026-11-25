<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { RuntimeInfo, SwitchersMap } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import CheckIcon from '@lucide/svelte/icons/check';
	import SettingsIcon from '@lucide/svelte/icons/settings';

	let switchers = $state<SwitchersMap>({});
	let loading = $state(true);

	async function loadSwitchers() {
		loading = true;
		try {
			switchers = (await api.getSwitchers()) ?? {};
		} catch (err) {
			toast.error(err instanceof Error ? err.message : t('switchers.loadFailed'));
		} finally {
			loading = false;
		}
	}

	onMount(loadSwitchers);

	$effect(() => {
		if (cp.refreshTick > 0) loadSwitchers();
	});

	function isCurrentVersion(item: RuntimeInfo, ver: string): boolean {
		if (item.active_major) {
			return item.active_major === ver || item.active_major === ver.replace(/^v/, '');
		}
		const cleanActive = (item.active_version || '').replace(/^v/, '').trim();
		const cleanV = ver.replace(/^v/, '').trim();
		return cleanActive === cleanV || cleanActive.startsWith(`${cleanV}.`);
	}
</script>

<div class="flex flex-col gap-5 sm:gap-6">
	<div>
		<h1 class="text-xl font-bold tracking-tight sm:text-2xl">{t('switchers.title')}</h1>
		<p class="text-sm text-muted-foreground">
			{t('switchers.subtitle', {
				nodeCmd: 'wdev switch node',
				phpCmd: 'wdev switch php',
				pgCmd: 'wdev switch pg'
			})}
		</p>
	</div>

	{#if loading}
		<div class="grid gap-3 sm:gap-4 md:grid-cols-2">
			{#each [1, 2, 3, 4] as idx (idx)}
				<Card.Root>
					<Card.Header>
						<Skeleton class="h-5 w-32" />
						<Skeleton class="h-4 w-48" />
					</Card.Header>
					<Card.Content>
						<Skeleton class="h-9 w-full" />
					</Card.Content>
				</Card.Root>
			{/each}
		</div>
	{:else}
		<div class="grid gap-3 sm:gap-4 md:grid-cols-2">
			{#each Object.entries(switchers) as [key, item] (key)}
				<Card.Root>
					<Card.Header>
						<div class="flex flex-wrap items-start justify-between gap-2">
							<div class="min-w-0">
								<Card.Title>{item.name}</Card.Title>
								<Card.Description>
									{t('switchers.activeVersion')}
									<span class="font-mono font-semibold text-foreground">
										{item.active_version}
									</span>
								</Card.Description>
							</div>
							<div class="flex flex-wrap items-center gap-2">
								{#if item.active_version && item.active_version !== 'None'}
									<StatusBadge tone="success" label={t('common.status.installed')} />
								{:else}
									<StatusBadge tone="neutral" label={t('common.status.notActive')} />
								{/if}
								{#if (key === 'php' || key === 'pg') && item.active_version && item.active_version !== 'None'}
									<Button
										variant="outline"
										size="xs"
										onclick={() =>
											cp.openServiceConfig(
												key === 'php' ? 'php-fpm' : 'postgresql',
												item.active_major || ''
											)}
									>
										<SettingsIcon data-icon="inline-start" />
										{t('switchers.configBtn')}
									</Button>
								{/if}
							</div>
						</div>
					</Card.Header>

					<Card.Content>
						<div class="flex flex-col gap-2">
							<span class="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
								{t('switchers.switchToLabel')}
							</span>
							<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
								{#each item.available_versions || [] as ver (ver)}
									{@const active = isCurrentVersion(item, ver)}
									<Button
										variant={active ? 'default' : 'outline'}
										size="sm"
										onclick={() => cp.switchRuntime(key, ver)}
									>
										{#if active}
											<CheckIcon data-icon="inline-start" />
										{/if}
										{ver}
									</Button>
								{/each}
							</div>
						</div>
					</Card.Content>
				</Card.Root>
			{/each}
		</div>
	{/if}
</div>
