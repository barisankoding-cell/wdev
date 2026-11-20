<script lang="ts">
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';

	function handleOpenChange(open: boolean) {
		if (!open && cp.confirmOpen) {
			cp.resolveConfirm(false);
		}
	}
</script>

<AlertDialog.Root open={cp.confirmOpen} onOpenChange={handleOpenChange}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{cp.confirmConfig.title}</AlertDialog.Title>
			<AlertDialog.Description class="whitespace-pre-line">
				{cp.confirmConfig.description}
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel onclick={() => cp.resolveConfirm(false)}>
				{cp.confirmConfig.cancelText || 'Batal'}
			</AlertDialog.Cancel>
			<AlertDialog.Action
				variant={cp.confirmConfig.destructive ? 'destructive' : 'default'}
				onclick={() => cp.resolveConfirm(true)}
			>
				{cp.confirmConfig.confirmText || 'Lanjutkan'}
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
