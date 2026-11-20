<script lang="ts">
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import ServerIcon from '@lucide/svelte/icons/server';
	import NetworkIcon from '@lucide/svelte/icons/network';
	import CheckCircle2Icon from '@lucide/svelte/icons/check-circle-2';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';

	let selected = $state<'vps' | 'edge'>(modeStore.mode);

	$effect(() => {
		if (modeStore.modalOpen) {
			selected = modeStore.mode;
		}
	});

	function handleSelect(mode: 'vps' | 'edge') {
		selected = mode;
	}

	async function applyMode() {
		await modeStore.switchMode(selected);
	}
</script>

<Dialog.Root bind:open={modeStore.modalOpen}>
	<Dialog.Content class="flex max-h-[85dvh] sm:max-h-[90dvh] w-full max-w-[calc(100%-1.5rem)] sm:max-w-2xl flex-col p-4 sm:p-6 overflow-hidden gap-0">
		<!-- Header (Fixed at top) -->
		<Dialog.Header class="shrink-0 pb-3 border-b border-border/40">
			<div class="flex items-center gap-2.5">
				<div class="flex size-8 sm:size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
					<SparklesIcon class="size-4 sm:size-5" />
				</div>
				<div class="min-w-0 flex-1">
					<Dialog.Title class="text-base sm:text-lg font-bold leading-tight truncate">
						Pilih Mode Operasional Server
					</Dialog.Title>
					<Dialog.Description class="text-xs text-muted-foreground mt-0.5">
						Tentukan arsitektur wdev sesuai lingkungan mesin ini. Dapat diubah kapan saja.
					</Dialog.Description>
				</div>
			</div>
		</Dialog.Header>

		<!-- Scrollable Body (Guarantees smooth scrolling on mobile devices) -->
		<div class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto py-3 pr-1 -mr-1">
			<div class="grid gap-2.5 sm:grid-cols-2">
				<!-- Option 1: Edge Gateway Mode (Hybrid) -->
				<button
					type="button"
					onclick={() => handleSelect('edge')}
					class={`flex flex-col text-left rounded-xl border p-3 sm:p-3.5 transition-all duration-150 relative cursor-pointer focus:outline-none ${
						selected === 'edge'
							? 'border-primary bg-primary/5 ring-2 ring-primary/20 shadow-sm'
							: 'border-border hover:border-muted-foreground/30 hover:bg-muted/30'
					}`}
				>
					<div class="flex items-start justify-between gap-2 mb-2">
						<div class="flex size-8 sm:size-9 items-center justify-center rounded-lg bg-secondary text-secondary-foreground shrink-0">
							<NetworkIcon class="size-4 sm:size-4.5" />
						</div>
						<div class="flex items-center gap-1.5">
							{#if modeStore.mode === 'edge' && modeStore.isSet}
								<Badge variant="default" class="text-[9px] sm:text-[10px] px-1.5 py-0 h-4">Aktif</Badge>
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
						Edge Gateway (Hybrid Mode)
					</div>
					<p class="text-[11px] sm:text-xs text-muted-foreground mb-2 leading-relaxed">
						Untuk PC / Laptop developer di balik NAT kantor atau rumah.
					</p>

					<ul class="space-y-1 text-[11px] sm:text-xs text-muted-foreground border-t border-border/60 pt-2 mt-auto">
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-primary shrink-0 mt-0.5" />
							<span>Terhubung ke VPS Edge via NetBird VPN (<code class="font-mono text-[10px]">wt0</code>).</span>
						</li>
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-primary shrink-0 mt-0.5" />
							<span>Webstack & file mail tetap berada di storage lokal PC.</span>
						</li>
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-primary shrink-0 mt-0.5" />
							<span>VPS Edge mengelola IP publik & SSL reverse proxy.</span>
						</li>
					</ul>
				</button>

				<!-- Option 2: Full VPS Mode (aaPanel Style) -->
				<button
					type="button"
					onclick={() => handleSelect('vps')}
					class={`flex flex-col text-left rounded-xl border p-3 sm:p-3.5 transition-all duration-150 relative cursor-pointer focus:outline-none ${
						selected === 'vps'
							? 'border-primary bg-primary/5 ring-2 ring-primary/20 shadow-sm'
							: 'border-border hover:border-muted-foreground/30 hover:bg-muted/30'
					}`}
				>
					<div class="flex items-start justify-between gap-2 mb-2">
						<div class="flex size-8 sm:size-9 items-center justify-center rounded-lg bg-primary/10 text-primary shrink-0">
							<ServerIcon class="size-4 sm:size-4.5" />
						</div>
						<div class="flex items-center gap-1.5">
							{#if modeStore.mode === 'vps' && modeStore.isSet}
								<Badge variant="default" class="text-[9px] sm:text-[10px] px-1.5 py-0 h-4">Aktif</Badge>
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
						Full VPS (aaPanel Style)
					</div>
					<p class="text-[11px] sm:text-xs text-muted-foreground mb-2 leading-relaxed">
						Standalone server untuk Cloud VPS / Dedicated publik.
					</p>

					<ul class="space-y-1 text-[11px] sm:text-xs text-muted-foreground border-t border-border/60 pt-2 mt-auto">
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-success shrink-0 mt-0.5" />
							<span>PowerDNS lokal authoritative (Port 53 UDP/TCP & 8081).</span>
						</li>
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-success shrink-0 mt-0.5" />
							<span>Domain & DNS record langsung aktif tanpa tunnel.</span>
						</li>
						<li class="flex items-start gap-1.5">
							<ShieldCheckIcon class="size-3 text-success shrink-0 mt-0.5" />
							<span>Mail server Stalwart langsung melayani publik.</span>
						</li>
					</ul>
				</button>
			</div>

			<div class="rounded-lg bg-muted/40 p-2.5 text-[11px] sm:text-xs text-muted-foreground border">
				💡 <strong>Tips:</strong> Anda dapat mengganti mode ini kapan saja melalui tombol badge mode di bagian atas panel.
			</div>
		</div>

		<!-- Footer (Fixed at bottom) -->
		<Dialog.Footer class="shrink-0 pt-3 border-t border-border/40 flex flex-col-reverse sm:flex-row sm:items-center sm:justify-end gap-2">
			{#if modeStore.isSet}
				<Button type="button" variant="outline" size="sm" class="w-full sm:w-auto" onclick={() => modeStore.closeModal()}>
					Batal
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
					Menerapkan...
				{:else}
					Pilih Mode {selected === 'edge' ? 'Edge Gateway' : 'Full VPS'}
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
