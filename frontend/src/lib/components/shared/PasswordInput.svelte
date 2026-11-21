<script lang="ts">
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { cn } from '#lib/utils.js';
	import { toast } from 'svelte-sonner';
	import EyeIcon from '@lucide/svelte/icons/eye';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import CheckIcon from '@lucide/svelte/icons/check';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import type { FullAutoFill } from 'svelte/elements';

	let {
		id,
		name,
		value = $bindable(''),
		placeholder = 'Minimal 8 karakter...',
		disabled = false,
		required = false,
		autocomplete = 'new-password',
		canGenerate = true,
		generateLength = 16,
		generateLabel = 'Generate',
		onGenerate,
		class: className = ''
	}: {
		id?: string;
		name?: string;
		value?: string;
		placeholder?: string;
		disabled?: boolean;
		required?: boolean;
		autocomplete?: FullAutoFill | null;
		canGenerate?: boolean;
		generateLength?: number;
		generateLabel?: string;
		onGenerate?: (password: string) => void;
		class?: string;
	} = $props();

	let revealed = $state(false);
	let copied = $state(false);
	let copyTimer: ReturnType<typeof setTimeout> | null = null;

	function generateStrongPassword(len: number): string {
		const upper = 'ABCDEFGHJKLMNPQRSTUVWXYZ';
		const lower = 'abcdefghijkmnopqrstuvwxyz';
		const digits = '23456789';
		const symbols = '!@#$%*-_';
		const all = upper + lower + digits + symbols;

		const length = Math.max(12, len);
		const bytes = new Uint32Array(length);
		if (typeof globalThis.crypto !== 'undefined' && globalThis.crypto.getRandomValues) {
			globalThis.crypto.getRandomValues(bytes);
		} else {
			for (let i = 0; i < length; i++) {
				bytes[i] = Math.floor(Math.random() * 0xffffffff);
			}
		}

		// Pastikan minimal ada huruf besar, huruf kecil, angka, dan simbol aman.
		const chars: string[] = [
			upper[bytes[0] % upper.length],
			lower[bytes[1] % lower.length],
			digits[bytes[2] % digits.length],
			symbols[bytes[3] % symbols.length]
		];
		for (let i = 4; i < length; i++) {
			chars.push(all[bytes[i] % all.length]);
		}

		// Fisher-Yates shuffle
		const shuffleBytes = new Uint32Array(length);
		if (typeof globalThis.crypto !== 'undefined' && globalThis.crypto.getRandomValues) {
			globalThis.crypto.getRandomValues(shuffleBytes);
		} else {
			for (let i = 0; i < length; i++) {
				shuffleBytes[i] = Math.floor(Math.random() * 0xffffffff);
			}
		}
		for (let i = chars.length - 1; i > 0; i--) {
			const j = shuffleBytes[i] % (i + 1);
			const tmp = chars[i];
			chars[i] = chars[j];
			chars[j] = tmp;
		}
		return chars.join('');
	}

	function handleGenerate() {
		const pwd = generateStrongPassword(generateLength);
		value = pwd;
		revealed = true;
		copied = false;
		onGenerate?.(pwd);
	}

	async function handleCopy() {
		if (!value) return;
		try {
			await navigator.clipboard.writeText(value);
			copied = true;
			toast.success('Password disalin ke clipboard');
			if (copyTimer) clearTimeout(copyTimer);
			copyTimer = setTimeout(() => {
				copied = false;
			}, 1500);
		} catch {
			toast.error('Gagal menyalin password ke clipboard');
		}
	}
</script>

<div class={cn('flex items-center gap-2', className)}>
	<InputGroup.Root class="min-w-0 flex-1">
		<InputGroup.Input
			{id}
			{name}
			type={revealed ? 'text' : 'password'}
			{autocomplete}
			{placeholder}
			{disabled}
			{required}
			autocorrect="off"
			autocapitalize="none"
			spellcheck={false}
			bind:value
		/>
		<InputGroup.Addon align="inline-end">
			{#if value}
				<InputGroup.Button
					size="icon-xs"
					title="Salin password"
					aria-label="Salin password"
					{disabled}
					onclick={handleCopy}
				>
					{#if copied}
						<CheckIcon class="text-success" />
					{:else}
						<CopyIcon />
					{/if}
				</InputGroup.Button>
			{/if}
			<InputGroup.Button
				size="icon-xs"
				title={revealed ? 'Sembunyikan password' : 'Tampilkan password'}
				aria-label={revealed ? 'Sembunyikan password' : 'Tampilkan password'}
				{disabled}
				onclick={() => (revealed = !revealed)}
			>
				{#if revealed}
					<EyeOffIcon />
				{:else}
					<EyeIcon />
				{/if}
			</InputGroup.Button>
		</InputGroup.Addon>
	</InputGroup.Root>

	{#if canGenerate}
		<Button
			type="button"
			variant="outline"
			size="sm"
			class="shrink-0"
			{disabled}
			title="Buat password acak yang kuat"
			onclick={handleGenerate}
		>
			<SparklesIcon data-icon="inline-start" />
			{generateLabel}
		</Button>
	{/if}
</div>
