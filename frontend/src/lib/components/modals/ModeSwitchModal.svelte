<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { modeStore, MODE_LABELS, type ServerMode } from '#lib/stores/mode.svelte.js';
	import LaptopIcon from '@lucide/svelte/icons/laptop';
	import ServerIcon from '@lucide/svelte/icons/server';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import CheckCircle2Icon from '@lucide/svelte/icons/check-circle-2';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';

	let selected = $state<ServerMode>(modeStore.mode);

	$effect(() => {
		if (modeStore.modalOpen) {
			selected = modeStore.mode;
		}
	});

	async function applyMode() {
		await modeStore.switchMode(selected);
	}
</script>

<Dialog.Root bind:open={modeStore.modalOpen}>
	<Dialog.Content class="flex max-h-[85dvh] sm:max-h-[90dvh] w-full max-w-[calc(100%-1.5rem)] sm:max-w-4xl flex-col p-4 sm:p-6 overflow-hidden gap-0">
		<!-- Header (Fixed at top) -->
		<Dialog.Header class="shrink-0 pb-3 border-b border-border/40">
			<div class="flex items-center gap-2.5">
				<div class="flex size-8 sm:size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
					<SparklesIcon class="size-4 sm:size-5" />
				</div>
				<div class="min-w-0 flex-1">
					<Dialog.Title class="text-base sm:text-lg font-bold leading-tight truncate">
						{t('modals.modeSwitch.title')}
					</Dialog.Title>
					<Dialog.Description class="text-xs text-muted-foreground mt-0.5">
						{t('modals.modeSwitch.description')}
					</Dialog.Description>
				</div>
			</div>
		</Dialog.Header>

		<!-- Scrollable Body -->
		<div class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto py-3 pr-1 -mr-1">
			<fieldset class="grid gap-3 md:grid-cols-3">
				<legend class="sr-only">{t('modals.modeSwitch.legend')}</legend>

				<!-- Option 1: Local Standalone -->
				<label
					class={`flex flex-col text-left rounded-xl border-2 p-3 sm:p-3.5 transition-colors duration-150 relative cursor-pointer has-focus-visible:ring-2 has-focus-visible:ring-ring has-focus-visible:ring-offset-2 ${
						selected === 'local'
							? 'border-primary bg-primary/5 shadow-sm'
							: 'border-border hover:border-muted-foreground/40 hover:bg-muted/30'
					}`}
				>
					<input
						type="radio"
						name="wdev_server_mode"
						value="local"
						bind:group={selected}
						class="sr-only"
					/>
					<div class="flex items-start justify-between gap-2 mb-2">
						<div class="flex size-8 sm:size-9 items-center justify-center rounded-lg bg-primary/10 text-primary shrink-0">
							<LaptopIcon class="size-4 sm:size-4.5" />
						</div>
						<div class="flex items-center gap-1.5">
							{#if modeStore.mode === 'local' && modeStore.isSet}
								<Badge variant="default" class="text-[9px] sm:text-[10px] px-1.5 py-0 h-4">{t('modals.modeSwitch.activeBadge')}</Badge>
							{/if}
							<div
								class={`size-4 sm:size-5 rounded-full border flex items-center justify-center shrink-0 ${
									selected === 'local'
										? 'border-primary bg-primary text-primary-foreground'
										: 'border-muted-foreground/30'
								}`}
							>
								{#if selected === 'local'}
									<CheckCircle2Icon class="size-3 sm:size-3.5" />
								{/if}
							</div>
						</div>
					</div>

					<div class="font-bold text-xs sm:text-sm text-foreground mb-0.5">
						{t('modals.modeSwitch.local.title')}
					</div>
					<p class="text-[11px] sm:text-xs text-muted-foreground mb-2 leading-relaxed">
						{t('modals.modeSwitch.local.subtitle')}
					</p>

					<ul class="space-y-1 text-[11px] sm:text-xs text-muted-foreground border-t border-border/60 pt-2 mt-auto">
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-success shrink-0 mt-0.5" />
							<span>{t('modals.modeSwitch.local.b1')}</span>
						</li>
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-success shrink-0 mt-0.5" />
							<span>{t('modals.modeSwitch.local.b2')}</span>
						</li>
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-success shrink-0 mt-0.5" />
							<span>{t('modals.modeSwitch.local.b3')}</span>
						</li>
					</ul>
				</label>

				<!-- Option 2: Edge Gateway Mode (Hybrid) -->
				<label
					class={`flex flex-col text-left rounded-xl border-2 p-3 sm:p-3.5 transition-colors duration-150 relative cursor-pointer has-focus-visible:ring-2 has-focus-visible:ring-ring has-focus-visible:ring-offset-2 ${
						selected === 'edge'
							? 'border-primary bg-primary/5 shadow-sm'
							: 'border-border hover:border-muted-foreground/40 hover:bg-muted/30'
					}`}
				>
					<input
						type="radio"
						name="wdev_server_mode"
						value="edge"
						bind:group={selected}
						class="sr-only"
					/>
					<div class="flex items-start justify-between gap-2 mb-2">
						<div class="flex size-8 sm:size-9 items-center justify-center rounded-lg bg-secondary text-secondary-foreground shrink-0">
							<NetworkIcon class="size-4 sm:size-4.5" />
						</div>
						<div class="flex items-center gap-1.5">
							{#if modeStore.mode === 'edge' && modeStore.isSet}
								<Badge variant="default" class="text-[9px] sm:text-[10px] px-1.5 py-0 h-4">{t('modals.modeSwitch.activeBadge')}</Badge>
							{/if}
							<div
								class={`size-4 sm:size-5 rounded-full border flex items-center justify-center shrink-0 ${
									selected === 'edge'
										? 'border-primary bg-primary text-primary-foreground'
										: 'border-muted-foreground/30'
								}`}
							>
								{#if selected === 'edge'}
									<CheckCircle2Icon class="size-3 sm:size-3.5" />
								{/if}
							</div>
						</div>
					</div>

					<div class="font-bold text-xs sm:text-sm text-foreground mb-0.5">
						{t('modals.modeSwitch.edge.title')}
					</div>
					<p class="text-[11px] sm:text-xs text-muted-foreground mb-2 leading-relaxed">
						{t('modals.modeSwitch.edge.subtitle')}
					</p>

					<ul class="space-y-1 text-[11px] sm:text-xs text-muted-foreground border-t border-border/60 pt-2 mt-auto">
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-primary shrink-0 mt-0.5" />
							<span>{t('modals.modeSwitch.edge.b1')}</span>
						</li>
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-primary shrink-0 mt-0.5" />
							<span>{t('modals.modeSwitch.edge.b2')}</span>
						</li>
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-primary shrink-0 mt-0.5" />
							<span>{t('modals.modeSwitch.edge.b3')}</span>
						</li>
					</ul>
				</label>

				<!-- Option 3: Full VPS Mode (aaPanel Style) -->
				<label
					class={`flex flex-col text-left rounded-xl border-2 p-3 sm:p-3.5 transition-colors duration-150 relative cursor-pointer has-focus-visible:ring-2 has-focus-visible:ring-ring has-focus-visible:ring-offset-2 ${
						selected === 'vps'
							? 'border-primary bg-primary/5 shadow-sm'
							: 'border-border hover:border-muted-foreground/40 hover:bg-muted/30'
					}`}
				>
					<input
						type="radio"
						name="wdev_server_mode"
						value="vps"
						bind:group={selected}
						class="sr-only"
					/>
					<div class="flex items-start justify-between gap-2 mb-2">
						<div class="flex size-8 sm:size-9 items-center justify-center rounded-lg bg-primary/10 text-primary shrink-0">
							<ServerIcon class="size-4 sm:size-4.5" />
						</div>
						<div class="flex items-center gap-1.5">
							{#if modeStore.mode === 'vps' && modeStore.isSet}
								<Badge variant="default" class="text-[9px] sm:text-[10px] px-1.5 py-0 h-4">{t('modals.modeSwitch.activeBadge')}</Badge>
							{/if}
							<div
								class={`size-4 sm:size-5 rounded-full border flex items-center justify-center shrink-0 ${
									selected === 'vps'
										? 'border-primary bg-primary text-primary-foreground'
										: 'border-muted-foreground/30'
								}`}
							>
								{#if selected === 'vps'}
									<CheckCircle2Icon class="size-3 sm:size-3.5" />
								{/if}
							</div>
						</div>
					</div>

					<div class="font-bold text-xs sm:text-sm text-foreground mb-0.5">
						{t('modals.modeSwitch.vps.title')}
					</div>
					<p class="text-[11px] sm:text-xs text-muted-foreground mb-2 leading-relaxed">
						{t('modals.modeSwitch.vps.subtitle')}
					</p>

					<ul class="space-y-1 text-[11px] sm:text-xs text-muted-foreground border-t border-border/60 pt-2 mt-auto">
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-success shrink-0 mt-0.5" />
							<span>{t('modals.modeSwitch.vps.b1')}</span>
						</li>
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-success shrink-0 mt-0.5" />
							<span>{t('modals.modeSwitch.vps.b2')}</span>
						</li>
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-success shrink-0 mt-0.5" />
							<span>{t('modals.modeSwitch.vps.b3')}</span>
						</li>
					</ul>
				</label>
			</fieldset>

			<div class="rounded-lg bg-muted/40 p-2.5 text-[11px] sm:text-xs text-muted-foreground border">
				💡 <strong>{t('modals.modeSwitch.tipPrefix')}</strong> {t('modals.modeSwitch.tipBody')}
			</div>
		</div>

		<!-- Footer (Fixed at bottom) -->
		<Dialog.Footer class="shrink-0 pt-3 border-t border-border/40 flex flex-col-reverse sm:flex-row sm:items-center sm:justify-end gap-2">
			{#if modeStore.isSet}
				<Button type="button" variant="outline" size="sm" class="w-full sm:w-auto" onclick={() => modeStore.closeModal()}>
					{t('common.actions.cancel')}
				</Button>
			{/if}
			<Button
				type="button"
				variant="default"
				size="sm"
				class="w-full sm:w-auto"
				disabled={modeStore.loading}
				onclick={applyMode}
			>
				{#if modeStore.loading}
					<Spinner data-icon="inline-start" />
					{t('common.actions.applying')}
				{:else}
					{t('modals.modeSwitch.selectModeBtn', { mode: MODE_LABELS[selected] })}
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
