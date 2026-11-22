<script lang="ts">
	import { Dialog as SheetPrimitive } from "bits-ui";
	import { trackOverlayOpen } from "#lib/modal-overlay.svelte.js";

	let { open = $bindable(false), ...restProps }: SheetPrimitive.RootProps = $props();

	$effect.pre(() => {
		if (!open) return;
		return trackOverlayOpen("sheet");
	});
</script>

<SheetPrimitive.Root bind:open {...restProps} />
