<script lang="ts">
	import { AlertDialog as AlertDialogPrimitive } from "bits-ui";
	import { trackOverlayOpen } from "#lib/modal-overlay.svelte.js";

	let { open = $bindable(false), ...restProps }: AlertDialogPrimitive.RootProps = $props();

	$effect.pre(() => {
		if (!open) return;
		return trackOverlayOpen("modal");
	});
</script>

<AlertDialogPrimitive.Root bind:open {...restProps} />
