<script lang="ts">
	import QRCode from 'qrcode';
	import { cn } from '#lib/utils.js';

	interface Props {
		value: string;
		size?: number;
		label?: string;
		class?: string;
	}

	let { value, size = 188, label = 'QR Code', class: className }: Props = $props();

	// Bangun SVG path deterministik langsung dari BitMatrix QR sehingga selalu tajam di semua DPI & bebas XSS {@html}.
	const qrMatrix = $derived.by(() => {
		const trimmed = value.trim();
		if (!trimmed) return null;
		try {
			const qr = QRCode.create(trimmed, { errorCorrectionLevel: 'M' });
			const modSize = qr.modules.size;
			const margin = 2;
			const total = modSize + margin * 2;
			const parts: string[] = [];
			for (let r = 0; r < modSize; r++) {
				for (let c = 0; c < modSize; c++) {
					if (qr.modules.get(r, c)) {
						parts.push(`M${c + margin},${r + margin}h1v1h-1z`);
					}
				}
			}
			return { total, path: parts.join('') };
		} catch {
			return null;
		}
	});
</script>

{#if qrMatrix}
	<svg
		viewBox="0 0 {qrMatrix.total} {qrMatrix.total}"
		width={size}
		height={size}
		role="img"
		aria-label={label}
		shape-rendering="crispEdges"
		class={cn('block shrink-0 select-none', className)}
	>
		<rect width={qrMatrix.total} height={qrMatrix.total} fill="#ffffff" />
		<path d={qrMatrix.path} fill="#09090b" />
	</svg>
{/if}
