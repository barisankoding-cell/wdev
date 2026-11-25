<script lang="ts">
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Button, type ButtonSize, type ButtonVariant } from '#lib/components/ui/button/index.js';
	import { i18n, t, type Locale } from '#lib/i18n/index.svelte.js';
	import LanguagesIcon from '@lucide/svelte/icons/languages';
	import CheckIcon from '@lucide/svelte/icons/check';

	let {
		showLabel = false,
		variant = 'outline',
		size = 'sm',
		class: className = ''
	}: {
		showLabel?: boolean;
		variant?: ButtonVariant;
		size?: ButtonSize;
		class?: string;
	} = $props();

	const options: { value: Locale; labelKey: 'common.language.en' | 'common.language.id'; badge: string }[] = [
		{ value: 'en', labelKey: 'common.language.en', badge: 'EN' },
		{ value: 'id', labelKey: 'common.language.id', badge: 'ID' }
	];
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<Button
				{variant}
				{size}
				class={className}
				aria-label={t('common.language.ariaLabel')}
				title={t('common.language.label')}
				{...props}
			>
				<LanguagesIcon data-icon="inline-start" />
				<span class="font-mono text-xs font-semibold uppercase">
					{i18n.locale === 'id' ? t('common.language.idShort') : t('common.language.enShort')}
				</span>
				{#if showLabel}
					<span class="hidden sm:inline">{t('common.language.label')}</span>
				{/if}
			</Button>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end">
		<DropdownMenu.Group>
			<DropdownMenu.Label>{t('common.language.label')}</DropdownMenu.Label>
			<DropdownMenu.Separator />
			{#each options as opt (opt.value)}
				<DropdownMenu.Item onclick={() => i18n.setLocale(opt.value)}>
					<span class="font-mono text-xs font-semibold text-muted-foreground">{opt.badge}</span>
					<span>{t(opt.labelKey)}</span>
					{#if i18n.locale === opt.value}
						<CheckIcon class="ml-auto" />
					{/if}
				</DropdownMenu.Item>
			{/each}
		</DropdownMenu.Group>
	</DropdownMenu.Content>
</DropdownMenu.Root>
