<script lang="ts">
	import { cn } from '#lib/utils.js';

	interface Props {
		class?: string;
		decorative?: boolean;
	}

	let { class: className, decorative = true }: Props = $props();
</script>

<img
	src="/logo.png"
	alt={decorative ? '' : 'wdev'}
	aria-hidden={decorative ? 'true' : undefined}
	decoding="async"
	class={cn(
		'shrink-0 rounded-[22%] object-contain select-none ring-1 ring-border/75 dark:ring-white/15',
		className
	)}
/>
