<script lang="ts">
	import { Dialog as DialogPrimitive } from "bits-ui";
	import { trackOverlayOpen } from "#lib/modal-overlay.svelte.js";

	let { open = $bindable(false), ...restProps }: DialogPrimitive.RootProps = $props();

	$effect.pre(() => {
		if (!open) return;
		return trackOverlayOpen("modal");
	});
</script>

<DialogPrimitive.Root bind:open {...restProps} />
