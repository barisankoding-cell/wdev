<script lang="ts">
	import { cn } from '#lib/utils.js';
	import type { Snippet } from 'svelte';

	let {
		label,
		stackOnMobile = true,
		class: className = '',
		children
	}: {
		label: string;
		stackOnMobile?: boolean;
		class?: string;
		children: Snippet;
	} = $props();
</script>

<!-- Required for keyboard horizontal scroll accessibility on overflow regions -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
	tabindex="0"
	role="region"
	aria-label={label}
	data-stack-mobile={stackOnMobile ? 'true' : 'false'}
	class={cn('responsive-table-region', className)}
>
	{@render children()}
</div>
