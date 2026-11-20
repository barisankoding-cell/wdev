<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { cn } from '#lib/utils.js';
	import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
	import CircleDashedIcon from '@lucide/svelte/icons/circle-dashed';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
	import CircleXIcon from '@lucide/svelte/icons/circle-x';
	import InfoIcon from '@lucide/svelte/icons/info';

	type StatusTone = 'success' | 'warning' | 'destructive' | 'info' | 'neutral';

	let {
		tone = 'neutral',
		label,
		showIcon = true,
		class: className = ''
	}: {
		tone?: StatusTone;
		label: string;
		showIcon?: boolean;
		class?: string;
	} = $props();

	const toneClasses: Record<StatusTone, string> = {
		success: 'bg-success/15 text-success border-success/30',
		warning: 'bg-warning/15 text-warning border-warning/30',
		destructive: 'bg-destructive/15 text-destructive border-destructive/30',
		info: 'bg-info/15 text-info border-info/30',
		neutral: 'bg-secondary text-secondary-foreground border-border'
	};
</script>

<Badge variant="outline" class={cn(toneClasses[tone], className)}>
	{#if showIcon}
		{#if tone === 'success'}
			<CircleCheckIcon data-icon="inline-start" />
		{:else if tone === 'warning'}
			<CircleAlertIcon data-icon="inline-start" />
		{:else if tone === 'destructive'}
			<CircleXIcon data-icon="inline-start" />
		{:else if tone === 'info'}
			<InfoIcon data-icon="inline-start" />
		{:else}
			<CircleDashedIcon data-icon="inline-start" />
		{/if}
	{/if}
	<span>{label}</span>
</Badge>
