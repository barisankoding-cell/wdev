<script lang="ts">
	import { Button } from '#lib/components/ui/button/index.js';
	import ChevronsLeftIcon from '@lucide/svelte/icons/chevrons-left';
	import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import ChevronsRightIcon from '@lucide/svelte/icons/chevrons-right';
	import { cn } from '#lib/utils.js';

	let {
		page = $bindable(1),
		totalItems = 0,
		pageSize = 10,
		itemLabel = 'data',
		class: className = '',
		onPageChange
	}: {
		page?: number;
		totalItems: number;
		pageSize?: number;
		itemLabel?: string;
		class?: string;
		onPageChange?: (p: number) => void;
	} = $props();

	const totalPages = $derived(Math.max(1, Math.ceil(totalItems / pageSize)));
	const startItem = $derived(totalItems === 0 ? 0 : (page - 1) * pageSize + 1);
	const endItem = $derived(Math.min(page * pageSize, totalItems));

	function setPage(p: number) {
		const target = Math.max(1, Math.min(totalPages, p));
		page = target;
		onPageChange?.(target);
	}
</script>

{#if totalItems > 0}
	<div
		class={cn(
			'flex flex-col items-center justify-between gap-3 border-t bg-muted/10 px-4 py-2.5 sm:flex-row',
			className
		)}
	>
		<div class="text-xs text-muted-foreground">
			Menampilkan <span class="font-medium text-foreground">{startItem}</span>–<span
				class="font-medium text-foreground">{endItem}</span
			> dari <span class="font-medium text-foreground">{totalItems}</span> {itemLabel}
		</div>
		{#if totalPages > 1}
			<div class="flex items-center gap-1.5">
				<Button
					variant="outline"
					size="xs"
					disabled={page <= 1}
					onclick={() => setPage(1)}
					aria-label="Halaman pertama"
				>
					<ChevronsLeftIcon class="size-3.5" />
				</Button>
				<Button
					variant="outline"
					size="xs"
					disabled={page <= 1}
					onclick={() => setPage(page - 1)}
					aria-label="Halaman sebelumnya"
				>
					<ChevronLeftIcon class="size-3.5" />
					<span class="hidden sm:inline">Sebelumnya</span>
				</Button>

				<div class="flex items-center gap-1 px-1.5 text-xs text-muted-foreground">
					<span>Hal.</span>
					<span class="font-semibold text-foreground">{page}</span>
					<span>/</span>
					<span class="font-semibold text-foreground">{totalPages}</span>
				</div>

				<Button
					variant="outline"
					size="xs"
					disabled={page >= totalPages}
					onclick={() => setPage(page + 1)}
					aria-label="Halaman berikutnya"
				>
					<span class="hidden sm:inline">Berikutnya</span>
					<ChevronRightIcon class="size-3.5" />
				</Button>
				<Button
					variant="outline"
					size="xs"
					disabled={page >= totalPages}
					onclick={() => setPage(totalPages)}
					aria-label="Halaman terakhir"
				>
					<ChevronsRightIcon class="size-3.5" />
				</Button>
			</div>
		{/if}
	</div>
{/if}
