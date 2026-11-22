<script lang="ts">
	import { Select as SelectPrimitive } from "bits-ui";
	import { registerDismissableMenu } from "#lib/modal-overlay.svelte.js";

	let {
		open = $bindable(false),
		value = $bindable(),
		...restProps
	}: SelectPrimitive.RootProps = $props();

	$effect(() => {
		return registerDismissableMenu(() => {
			open = false;
		});
	});
</script>

<SelectPrimitive.Root bind:open bind:value={value as never} {...restProps} />
