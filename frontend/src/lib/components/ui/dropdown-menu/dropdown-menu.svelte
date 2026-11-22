<script lang="ts">
	import { DropdownMenu as DropdownMenuPrimitive } from "bits-ui";
	import { setDropdownMenuController } from "./dropdown-menu-context.js";
	import { registerDismissableMenu } from "#lib/modal-overlay.svelte.js";

	let { open = $bindable(false), ...restProps }: DropdownMenuPrimitive.RootProps = $props();

	function close() {
		open = false;
	}

	setDropdownMenuController({ close });

	$effect(() => {
		return registerDismissableMenu(close);
	});
</script>

<DropdownMenuPrimitive.Root bind:open {...restProps} />
