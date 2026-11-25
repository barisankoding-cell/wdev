<script lang="ts">
	import { untrack } from 'svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import { modeStore } from '#lib/stores/mode.svelte.js';
	import type { ComposeProject, ZoneInfo } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import GlobeIcon from '@lucide/svelte/icons/globe';

	let {
		open = $bindable(false),
		project = null,
		port = 8080,
		serviceName = '',
		onExposed
	}: {
		open: boolean;
		project: ComposeProject | null;
		port: number;
		serviceName?: string;
		onExposed?: () => void | Promise<void>;
	} = $props();

	let domainMode = $state<'custom' | 'subdomain'>('custom');
	let domain = $state('');
	let subdomainPrefix = $state('');
	let parentDomain = $state('');
	let zones = $state<ZoneInfo[]>([]);
	let sslEnabled = $state(true);
	let exposeEdge = $state(true);
	let edgeSSL = $state(true);
	let addHosts = $state(false);
	let submitting = $state(false);
	let formError = $state('');
	let prevOpen = false;

	const isVPS = $derived(modeStore.mode === 'vps');
	const isEdge = $derived(modeStore.mode === 'edge');
	const isLocal = $derived(modeStore.mode === 'local');
	const supportsDNS = $derived(isVPS || isEdge);

	const pdnsZones = $derived(
		Array.from(
			new Set(
				zones
					.map((z) => (z.name || '').replace(/\.$/, '').trim().toLowerCase())
					.filter((name) => Boolean(name) && !name.endsWith('.arpa'))
			)
		).sort()
	);

	const effectiveDomain = $derived.by(() => {
		if (domainMode === 'subdomain' && supportsDNS && parentDomain) {
			const pref = subdomainPrefix.trim().toLowerCase().replace(/\.+$/, '');
			return pref ? `${pref}.${parentDomain}` : parentDomain;
		}
		return domain.trim().toLowerCase().replace(/\.$/, '');
	});

	$effect(() => {
		const isOpen = open;
		if (isOpen && !prevOpen) {
			prevOpen = true;
			untrack(() => {
				formError = '';
				const base = (project?.name || 'app').replace(/[^a-z0-9-]/gi, '-').toLowerCase();
				domainMode = isEdge ? 'subdomain' : 'custom';
				domain = isLocal ? `${base}.test` : '';
				subdomainPrefix = base;
				sslEnabled = true;
				exposeEdge = isEdge;
				edgeSSL = true;
				addHosts = false;
				if (supportsDNS) {
					void api
						.listPDNSZones()
						.then((res) => {
							zones = res ?? [];
							const list = Array.from(
								new Set(
									zones
										.map((z) => (z.name || '').replace(/\.$/, '').trim().toLowerCase())
										.filter((n) => Boolean(n) && !n.endsWith('.arpa'))
								)
							).sort();
							if (list.length > 0 && !parentDomain) {
								parentDomain = list[0];
							}
						})
						.catch(() => {});
				}
			});
		} else if (!isOpen) {
			prevOpen = false;
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!project) return;
		formError = '';
		const dom = effectiveDomain;
		if (!dom) {
			formError = t('compose.exposeModal.errDomainRequired');
			return;
		}

		submitting = true;
		try {
			const res = await api.exposeComposeService(project.name, {
				domain: dom,
				proxy_port: port,
				ssl_enabled: isVPS ? false : sslEnabled,
				expose_edge: isEdge ? exposeEdge : undefined,
				edge_domain: isEdge && exposeEdge ? dom : undefined,
				edge_ssl: isEdge && exposeEdge ? edgeSSL : undefined,
				add_hosts: isLocal ? undefined : addHosts
			});

			if (res.edge_error) {
				toast.warning(t('compose.exposeModal.edgeErrorToast', { domain: dom, error: res.edge_error }));
			} else if (res.dns_synced && res.dns_message) {
				toast.success(res.dns_message);
			} else {
				toast.success(t('compose.exposeModal.createdToast', { domain: dom, port }));
			}

			// Pada mode VPS dengan SSL aktif, terbitkan sertifikat Let's Encrypt
			if (isVPS && sslEnabled) {
				const sslRes = await cp.runWithSudoRetry(
					t('compose.exposeModal.sslSudoPrompt', { domain: dom }),
					(sudoPass) => api.enableSiteSSL(dom, '', sudoPass)
				);
				if (sslRes?.task_id) {
					cp.openTerminal(t('compose.exposeModal.sslTerminalTitle', { domain: dom }));
				}
			}

			open = false;
			await onExposed?.();
		} catch (err) {
			formError = err instanceof Error ? err.message : t('compose.exposeModal.exposeFailed');
		} finally {
			submitting = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="flex max-h-[90dvh] max-w-[calc(100vw-1.5rem)] flex-col gap-0 overflow-hidden p-4 sm:max-w-[min(calc(100vw-2rem),32rem)]">
		<Dialog.Header class="shrink-0 border-b border-border/50 pb-3 pr-8">
			<Dialog.Title class="leading-snug">{t('compose.exposeModal.title')}</Dialog.Title>
			<Dialog.Description class="text-xs break-words sm:text-sm">
				{t('compose.exposeModal.desc1')}<code class="font-mono">{serviceName || project?.name}</code>{t('compose.exposeModal.desc2')}<code class="font-mono">http://127.0.0.1:{port}</code>{t('compose.exposeModal.desc3')}
			</Dialog.Description>
		</Dialog.Header>

		<div class="-mx-1 min-h-0 flex-1 overflow-y-auto overscroll-contain px-1 py-4">
			<form id="expose-compose-form" onsubmit={handleSubmit} class="flex flex-col gap-4">
				{#if formError}
					<Alert.Root variant="destructive">
						<AlertCircleIcon />
						<Alert.Title>{t('compose.exposeModal.errorTitle')}</Alert.Title>
						<Alert.Description class="break-words">{formError}</Alert.Description>
					</Alert.Root>
				{/if}

				<Field.FieldGroup class="gap-4">
					{#if supportsDNS && pdnsZones.length > 0}
						<Field.Field class="min-w-0">
							<Field.FieldTitle id="exp-domain-mode-label">{t('compose.exposeModal.domainSourceLabel')}</Field.FieldTitle>
							<ToggleGroup.Root
								type="single"
								variant="outline"
								spacing={2}
								aria-labelledby="exp-domain-mode-label"
								value={domainMode}
								onValueChange={(v) => {
									if (v === 'custom' || v === 'subdomain') domainMode = v;
								}}
								class="grid w-full grid-cols-2"
							>
								<ToggleGroup.Item
									value="subdomain"
									class="h-auto min-h-10 py-1.5 text-center text-xs leading-tight whitespace-normal sm:min-h-8 sm:text-sm"
								>
									{t('compose.exposeModal.sourceSubdomain')}
								</ToggleGroup.Item>
								<ToggleGroup.Item
									value="custom"
									class="h-auto min-h-10 py-1.5 text-center text-xs leading-tight whitespace-normal sm:min-h-8 sm:text-sm"
								>
									{t('compose.exposeModal.sourceCustom')}
								</ToggleGroup.Item>
							</ToggleGroup.Root>
						</Field.Field>
					{/if}

					{#if domainMode === 'subdomain' && supportsDNS && pdnsZones.length > 0}
						<Field.FieldGroup class="grid gap-3 sm:grid-cols-2">
							<Field.Field class="min-w-0">
								<Field.FieldLabel for="exp-prefix">{t('compose.exposeModal.subdomainLabel')}</Field.FieldLabel>
								<Input
									id="exp-prefix"
									bind:value={subdomainPrefix}
									placeholder="app"
									class="h-10 font-mono sm:h-8"
									autocorrect="off"
									autocapitalize="none"
									spellcheck={false}
									required
								/>
							</Field.Field>
							<Field.Field class="min-w-0">
								<Field.FieldLabel for="exp-zone">{t('compose.exposeModal.zoneLabel')}</Field.FieldLabel>
								<Select.Root
									type="single"
									value={parentDomain}
									onValueChange={(v) => {
										if (v) parentDomain = v;
									}}
								>
									<Select.Trigger id="exp-zone" class="h-10 w-full min-w-0 font-mono sm:h-8">
										<span class="truncate">{parentDomain || t('compose.exposeModal.selectZonePlaceholder')}</span>
									</Select.Trigger>
									<Select.Content>
										<Select.Group>
											{#each pdnsZones as z (z)}
												<Select.Item value={z} class="font-mono">{z}</Select.Item>
											{/each}
										</Select.Group>
									</Select.Content>
								</Select.Root>
							</Field.Field>
						</Field.FieldGroup>
					{:else}
						<Field.Field class="min-w-0">
							<Field.FieldLabel for="exp-domain">{t('compose.exposeModal.domainNameLabel')}</Field.FieldLabel>
							<Input
								id="exp-domain"
								bind:value={domain}
								placeholder={isLocal ? t('compose.exposeModal.domainPlaceholderLocal') : t('compose.exposeModal.domainPlaceholderPublic')}
								class="h-10 font-mono sm:h-8"
								autocorrect="off"
								autocapitalize="none"
								spellcheck={false}
								required
							/>
						</Field.Field>
					{/if}

					<Card.Root size="sm">
						<Card.Content class="flex flex-col gap-2 font-mono text-xs">
							<div class="flex flex-col gap-0.5 sm:flex-row sm:items-center sm:justify-between sm:gap-2">
								<span class="shrink-0 text-muted-foreground">{t('compose.exposeModal.previewDomain')}</span>
								<span class="min-w-0 font-semibold break-all sm:truncate">{effectiveDomain || '-'}</span>
							</div>
							<div class="flex flex-col gap-0.5 sm:flex-row sm:items-center sm:justify-between sm:gap-2">
								<span class="shrink-0 text-muted-foreground">{t('compose.exposeModal.previewUpstream')}</span>
								<span class="break-all">http://127.0.0.1:{port}</span>
							</div>
						</Card.Content>
					</Card.Root>

					<Field.FieldSet>
						<Field.FieldLegend variant="label">{t('compose.exposeModal.sslLegend')}</Field.FieldLegend>
						<Field.FieldGroup class="gap-3">
							{#if isEdge}
								<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
									<Checkbox id="exp-edge-toggle" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={exposeEdge} />
									<Field.FieldLabel for="exp-edge-toggle" class="cursor-pointer text-xs leading-snug font-normal">
										{t('compose.exposeModal.exposeEdgeToggle')}
									</Field.FieldLabel>
								</Field.Field>
								{#if exposeEdge}
									<Field.Field orientation="horizontal" class="items-start gap-2.5 pl-6 sm:items-center">
										<Checkbox id="exp-edge-ssl" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={edgeSSL} />
										<Field.FieldLabel for="exp-edge-ssl" class="cursor-pointer text-xs leading-snug font-normal">
											{t('compose.exposeModal.edgeSslToggle')}
										</Field.FieldLabel>
									</Field.Field>
								{/if}
							{:else}
								<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
									<Checkbox id="exp-ssl-toggle" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={sslEnabled} />
									<Field.FieldLabel for="exp-ssl-toggle" class="cursor-pointer text-xs leading-snug font-normal">
										{isVPS
											? t('compose.exposeModal.sslToggleVps')
											: t('compose.exposeModal.sslToggleLocal')}
									</Field.FieldLabel>
								</Field.Field>
							{/if}

							{#if !isLocal}
								<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
									<Checkbox id="exp-add-hosts" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={addHosts} />
									<Field.FieldLabel for="exp-add-hosts" class="cursor-pointer text-xs leading-snug font-normal">
										<span>{t('compose.exposeModal.addHosts1')}<code>/etc/hosts</code></span>
									</Field.FieldLabel>
								</Field.Field>
							{/if}
						</Field.FieldGroup>
					</Field.FieldSet>
				</Field.FieldGroup>
			</form>
		</div>

		<Dialog.Footer class="shrink-0 pb-[max(1rem,env(safe-area-inset-bottom,0px))]">
			<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (open = false)}>
				{t('compose.exposeModal.cancel')}
			</Button>
			<Button type="submit" form="expose-compose-form" class="min-h-10 sm:min-h-0" disabled={submitting}>
				{#if submitting}
					<Spinner data-icon="inline-start" />
					{t('compose.exposeModal.saving')}
				{:else}
					<GlobeIcon data-icon="inline-start" />
					{t('compose.exposeModal.submit')}
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
