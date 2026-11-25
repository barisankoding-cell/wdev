<script lang="ts">
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import QrCode from '#lib/components/shared/QrCode.svelte';
	import StatusBadge from '#lib/components/shared/StatusBadge.svelte';
	import { api } from '#lib/api/client.js';
	import { t } from '#lib/i18n/index.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type {
		OrcaPairingAddressMode,
		OrcaPairingRotateScope,
		WorkspaceStatus
	} from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import MonitorIcon from '@lucide/svelte/icons/monitor';
	import QrCodeIcon from '@lucide/svelte/icons/qr-code';
	import RotateCwIcon from '@lucide/svelte/icons/rotate-cw';
	import SmartphoneIcon from '@lucide/svelte/icons/smartphone';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import ZoomInIcon from '@lucide/svelte/icons/zoom-in';
	import ZoomOutIcon from '@lucide/svelte/icons/zoom-out';
	import { toast } from 'svelte-sonner';

	let {
		open = $bindable(false),
		workspace = $bindable(null),
		onCopy
	}: {
		open: boolean;
		workspace: WorkspaceStatus | null;
		onCopy: (value: string, label: string) => void;
	} = $props();

	let activeTab = $state<'mobile' | 'desktop'>('mobile');
	let pairAddressMode = $state<OrcaPairingAddressMode>('auto');
	let pairCustomAddress = $state('');
	let pairSaving = $state(false);
	let pairRotating = $state(false);
	let pairError = $state('');
	let qrEnlarged = $state(false);
	let wasOpen = false;

	$effect(() => {
		if (open && !wasOpen) {
			pairAddressMode = workspace?.orca.pairing_address_mode || 'auto';
			pairCustomAddress = workspace?.orca.pairing_custom_address || '';
			pairError = workspace?.orca.pairing_error || '';
			qrEnlarged = false;
		}
		if (!open && wasOpen) {
			qrEnlarged = false;
		}
		wasOpen = open;
	});

	function isBareDomainWithoutScheme(raw: string): boolean {
		const trimmed = raw.trim();
		if (!trimmed || trimmed.includes('://') || trimmed.includes(':')) return false;
		if (/^\d{1,3}(\.\d{1,3}){3}$/.test(trimmed)) return false;
		return trimmed.includes('.');
	}

	const showBareDomainWarning = $derived(
		pairAddressMode === 'custom' && isBareDomainWithoutScheme(pairCustomAddress)
	);

	async function applyOrcaPairing(
		mode: OrcaPairingAddressMode,
		rotate = false,
		rotateScope: OrcaPairingRotateScope = 'all'
	) {
		const customTrimmed = pairCustomAddress.trim();
		if (mode === 'custom' && !customTrimmed) {
			pairError = t('workspace.pairModal.customRequired');
			return;
		}
		if (rotate) pairRotating = true;
		else pairSaving = true;
		pairError = '';

		try {
			const res = await api.saveOrcaPairing({
				mode,
				custom_address: customTrimmed,
				rotate,
				rotate_scope: rotate ? rotateScope : undefined
			});
			if (workspace) {
				workspace = {
					...workspace,
					orca: {
						...workspace.orca,
						pairing_address_mode: res.mode,
						pairing_custom_address: res.custom_address,
						pairing_address: res.target_address,
						pairing_url: res.pairing_url,
						web_client_url: res.web_client_url,
						endpoint: res.endpoint || workspace.orca.endpoint,
						pairing_error: res.error || '',
						mobile_pairing_url: res.mobile_pairing_url,
						mobile_endpoint: res.mobile_endpoint,
						mobile_connection_mode: res.mobile_connection_mode,
						mobile_error: res.mobile_error || '',
						local_ip: res.local_ip || workspace.orca.local_ip,
						vpn_ip: res.vpn_ip ?? workspace.orca.vpn_ip
					}
				};
			}
			pairAddressMode = res.mode;
			if (res.custom_address) {
				pairCustomAddress = res.custom_address;
			}
			toast.success(
				rotate ? t('workspace.pairModal.linkRotated') : t('workspace.pairModal.addressSaved')
			);
		} catch (err) {
			const msg = err instanceof Error ? err.message : t('workspace.pairModal.saveFailed');
			pairError = msg;
			toast.error(msg);
		} finally {
			pairSaving = false;
			pairRotating = false;
		}
	}
</script>

{#if workspace}
	{@const orca = workspace.orca}
	{@const fallbackWebUrl = `http://${typeof window !== 'undefined' ? window.location.hostname : 'localhost'}:${orca.port || 6768}`}
	{@const webUrl = orca.web_client_url || (orca.active && !pairError ? fallbackWebUrl : '')}
	{@const mobileUrl = orca.mobile_pairing_url || ''}
	{@const mobileEndpoint = orca.mobile_endpoint || orca.endpoint || ''}

	<Dialog.Root bind:open>
		<Dialog.Content
			class="flex max-h-[90dvh] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col gap-0 overflow-hidden p-4 sm:w-full sm:max-w-[min(calc(100vw-2rem),56rem)] sm:p-6"
		>
			<Dialog.Header class="shrink-0 border-b border-border/50 pb-3">
				<div class="flex flex-wrap items-center gap-2 pr-7">
					<Dialog.Title>{t('workspace.pairModal.title')}</Dialog.Title>
					<StatusBadge
						tone={orca.active ? 'success' : 'neutral'}
						label={orca.active
							? t('workspace.pairModal.statusRunning')
							: t('workspace.pairModal.statusStopped')}
					/>
				</div>
				<Dialog.Description>
					{t('workspace.pairModal.description')}
				</Dialog.Description>
			</Dialog.Header>

			<div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto py-4">
				<!-- Konfigurasi Alamat Pairing (berlaku untuk Mobile maupun Web & Desktop) -->
				<Field.FieldGroup class="gap-3">
					<Field.Field>
						<div class="flex items-center justify-between gap-2">
							<Field.FieldLabel>{t('workspace.pairModal.addressModeLabel')}</Field.FieldLabel>
							{#if pairSaving}
								<Spinner class="size-3.5 text-muted-foreground" />
							{/if}
						</div>
						<ToggleGroup.Root
							type="single"
							value={pairAddressMode}
							variant="outline"
							spacing={2}
							class="grid w-full grid-cols-2 sm:grid-cols-4"
							disabled={pairSaving || pairRotating}
							onValueChange={(val) => {
								if (!val) return;
								const next = val as OrcaPairingAddressMode;
								pairAddressMode = next;
								pairError = '';
								if (next !== 'custom') {
									void applyOrcaPairing(next);
								}
							}}
						>
							<ToggleGroup.Item value="auto" class="w-full">
								{t('workspace.pairModal.addressModeAuto')}
							</ToggleGroup.Item>
							<ToggleGroup.Item value="local" class="w-full">
								{t('workspace.pairModal.addressModeLocal')}
							</ToggleGroup.Item>
							<ToggleGroup.Item
								value="vpn"
								disabled={!orca.vpn_ip && pairAddressMode !== 'vpn'}
								class="w-full"
							>
								{t('workspace.pairModal.addressModeVpn')}
							</ToggleGroup.Item>
							<ToggleGroup.Item value="custom" class="w-full">
								{t('workspace.pairModal.addressModeCustom')}
							</ToggleGroup.Item>
						</ToggleGroup.Root>
						<Field.FieldDescription>
							{#if pairAddressMode === 'local'}
								{t('workspace.pairModal.addressHintLocal', { ip: orca.local_ip || '127.0.0.1' })}
							{:else if pairAddressMode === 'vpn'}
								{#if orca.vpn_ip}
									{t('workspace.pairModal.addressHintVpn', { ip: orca.vpn_ip })}
								{:else}
									{t('workspace.pairModal.addressHintVpnUnavailable')}
								{/if}
							{:else if pairAddressMode === 'custom'}
								{t('workspace.pairModal.addressHintCustom')}
							{:else}
								{t('workspace.pairModal.addressHintAuto')}
							{/if}
						</Field.FieldDescription>
						{#if orca.local_ip || orca.vpn_ip}
							<div class="flex flex-wrap items-center gap-1.5 pt-0.5">
								{#if orca.local_ip}
									<Badge variant="outline" class="font-mono text-[11px] font-normal">
										{t('workspace.pairModal.addressModeLocal')}: {orca.local_ip}
									</Badge>
								{/if}
								{#if orca.vpn_ip}
									<Badge variant="outline" class="font-mono text-[11px] font-normal">
										{t('workspace.pairModal.addressModeVpn')}: {orca.vpn_ip}
									</Badge>
								{/if}
							</div>
						{/if}
					</Field.Field>

					{#if pairAddressMode === 'custom'}
						<Field.Field data-invalid={pairError ? true : undefined}>
							<Field.FieldLabel for="orca-modal-custom-address">
								{t('workspace.pairModal.customAddressLabel')}
							</Field.FieldLabel>
							<form
								class="flex items-center gap-2"
								onsubmit={(e) => {
									e.preventDefault();
									void applyOrcaPairing('custom');
								}}
							>
								<Input
									id="orca-modal-custom-address"
									bind:value={pairCustomAddress}
									placeholder={t('workspace.pairModal.customAddressPlaceholder')}
									aria-invalid={pairError ? true : undefined}
									disabled={pairSaving || pairRotating}
									class="flex-1 font-mono text-xs"
								/>
								<Button
									type="submit"
									size="sm"
									class="shrink-0"
									disabled={pairSaving || pairRotating}
								>
									{#if pairSaving}
										<Spinner data-icon="inline-start" />
										{t('workspace.pairModal.applyingBtn')}
									{:else}
										{t('workspace.pairModal.applyBtn')}
									{/if}
								</Button>
							</form>
						</Field.Field>

						{#if showBareDomainWarning}
							<Alert.Root>
								<AlertCircleIcon />
								<Alert.Description class="flex flex-wrap items-center justify-between gap-2">
									<span>{t('workspace.pairModal.bareDomainWarn')}</span>
									<Button
										type="button"
										variant="outline"
										size="xs"
										onclick={() => {
											pairCustomAddress = `wss://${pairCustomAddress.trim()}`;
										}}
									>
										{t('workspace.pairModal.useWssBtn')}
									</Button>
								</Alert.Description>
							</Alert.Root>
						{/if}
					{/if}

					{#if pairError}
						<Alert.Root variant="destructive">
							<AlertCircleIcon />
							<Alert.Description>{pairError}</Alert.Description>
						</Alert.Root>
					{/if}
				</Field.FieldGroup>

				<Separator />

				<!-- Tab Tujuan Klien: Aplikasi Mobile (QR + scope: mobile) vs Web & Desktop (scope: runtime) -->
				<Tabs.Root bind:value={activeTab} class="flex flex-col gap-3">
					<Tabs.List class="grid w-full grid-cols-2">
						<Tabs.Trigger value="mobile">
							<SmartphoneIcon data-icon="inline-start" />
							{t('workspace.pairModal.tabMobile')}
						</Tabs.Trigger>
						<Tabs.Trigger value="desktop">
							<MonitorIcon data-icon="inline-start" />
							{t('workspace.pairModal.tabDesktop')}
						</Tabs.Trigger>
					</Tabs.List>

					<Tabs.Content value="mobile" class="mt-0 flex flex-col gap-3.5">
						{#if !orca.active}
							<Empty.Root class="border py-6">
								<Empty.Header>
									<Empty.Media variant="icon">
										<QrCodeIcon />
									</Empty.Media>
									<Empty.Title>{t('workspace.pairModal.serviceStoppedTitle')}</Empty.Title>
									<Empty.Description>
										{t('workspace.pairModal.serviceStoppedDesc')}
									</Empty.Description>
								</Empty.Header>
							</Empty.Root>
						{:else if orca.mobile_error && !mobileUrl}
							<Alert.Root variant="destructive">
								<AlertCircleIcon />
								<Alert.Description>{orca.mobile_error}</Alert.Description>
							</Alert.Root>
						{:else if mobileUrl}
							<div class="grid gap-4 md:grid-cols-[auto_minmax(0,1fr)] md:items-stretch">
								<!-- Kartu QR Code Barcode (latar putih wajib agar kamera HP mudah membaca meski tema gelap aktif) -->
								<div
									class="flex flex-col items-center justify-center gap-3 rounded-lg border bg-muted/30 p-4 text-center"
								>
									<button
										type="button"
										onclick={() => (qrEnlarged = !qrEnlarged)}
										aria-label={qrEnlarged
											? t('workspace.pairModal.zoomOutQr')
											: t('workspace.pairModal.zoomInQr')}
										class="group relative cursor-pointer rounded-xl border border-border bg-white p-3 text-zinc-950 shadow-xs transition-transform hover:scale-[1.01] focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
									>
										<QrCode
											value={mobileUrl}
											size={qrEnlarged ? 256 : 188}
											label={t('workspace.pairModal.mobileQrAlt')}
										/>
									</button>

									<Button
										type="button"
										variant="ghost"
										size="xs"
										onclick={() => (qrEnlarged = !qrEnlarged)}
									>
										{#if qrEnlarged}
											<ZoomOutIcon data-icon="inline-start" />
											{t('workspace.pairModal.zoomOutQr')}
										{:else}
											<ZoomInIcon data-icon="inline-start" />
											{t('workspace.pairModal.zoomInQr')}
										{/if}
									</Button>
								</div>

								<div class="flex min-w-0 flex-col justify-between gap-4 rounded-lg border bg-muted/20 p-4">
									<div class="flex flex-col gap-1.5">
										<div class="flex flex-wrap items-center gap-1.5">
											<span class="text-sm font-medium">{t('workspace.pairModal.mobileQrTitle')}</span>
											{#if mobileEndpoint}
												<Badge
													variant="secondary"
													class="max-w-full truncate font-mono text-[11px] font-normal"
												>
													{mobileEndpoint}
												</Badge>
											{/if}
										</div>
										<p class="text-xs leading-relaxed text-muted-foreground">
											{t('workspace.pairModal.mobileQrHint')}
										</p>
									</div>

									<Separator />

									<Field.Field>
										<div class="flex items-center justify-between gap-2">
											<Field.FieldLabel for="orca-modal-mobile-paircode">
												{t('workspace.pairModal.mobileUriLabel')}
											</Field.FieldLabel>
											<Button
												type="button"
												variant="ghost"
												size="xs"
												disabled={pairSaving || pairRotating}
												onclick={() => void applyOrcaPairing(pairAddressMode, true, 'mobile')}
											>
												{#if pairRotating}
													<Spinner data-icon="inline-start" />
													{t('workspace.pairModal.rotatingBtn')}
												{:else}
													<RotateCwIcon data-icon="inline-start" />
													{t('workspace.pairModal.rotateBtn')}
												{/if}
											</Button>
										</div>
										<InputGroup.Root>
											<InputGroup.Input
												id="orca-modal-mobile-paircode"
												value={mobileUrl}
												readonly
												class="font-mono text-xs"
											/>
											<InputGroup.Addon align="inline-end">
												<InputGroup.Button
													onclick={() =>
														onCopy(mobileUrl, t('workspace.pairModal.mobileUriCopyLabel'))}
												>
													<CopyIcon data-icon="inline-start" />
													{t('common.actions.copy')}
												</InputGroup.Button>
											</InputGroup.Addon>
										</InputGroup.Root>
										<Field.FieldDescription>
											{t('workspace.pairModal.mobileUriDesc')}
										</Field.FieldDescription>
									</Field.Field>
								</div>
							</div>
						{/if}
					</Tabs.Content>

					<Tabs.Content value="desktop" class="mt-0 flex flex-col gap-3.5">
						<Field.Field>
							<div class="flex items-center justify-between gap-2">
								<Field.FieldLabel for="orca-modal-weburl">
									{t('workspace.pairModal.webUrlLabel')}
								</Field.FieldLabel>
								{#if webUrl}
									<Button
										variant="ghost"
										size="xs"
										href={webUrl}
										target="_blank"
										rel="noopener noreferrer"
									>
										<ExternalLinkIcon data-icon="inline-start" />
										{t('workspace.pairModal.openWebBtn')}
									</Button>
								{/if}
							</div>
							<InputGroup.Root>
								<InputGroup.Input
									id="orca-modal-weburl"
									value={webUrl || '-'}
									readonly
									class="font-mono text-xs"
								/>
								<InputGroup.Addon align="inline-end">
									<InputGroup.Button
										disabled={!webUrl}
										onclick={() => onCopy(webUrl, t('workspace.pairModal.webUrlCopyLabel'))}
									>
										<CopyIcon data-icon="inline-start" />
										{t('common.actions.copy')}
									</InputGroup.Button>
								</InputGroup.Addon>
							</InputGroup.Root>
						</Field.Field>

						<Field.Field>
							<div class="flex items-center justify-between gap-2">
								<Field.FieldLabel for="orca-modal-paircode">
									{t('workspace.pairModal.pairingUriLabel')}
								</Field.FieldLabel>
								{#if orca.active}
									<Button
										type="button"
										variant="ghost"
										size="xs"
										disabled={pairSaving || pairRotating}
										onclick={() => void applyOrcaPairing(pairAddressMode, true, 'runtime')}
									>
										{#if pairRotating}
											<Spinner data-icon="inline-start" />
											{t('workspace.pairModal.rotatingBtn')}
										{:else}
											<RotateCwIcon data-icon="inline-start" />
											{t('workspace.pairModal.rotateBtn')}
										{/if}
									</Button>
								{/if}
							</div>
							<InputGroup.Root>
								<InputGroup.Input
									id="orca-modal-paircode"
									value={orca.pairing_url || '-'}
									readonly
									class="font-mono text-xs"
								/>
								<InputGroup.Addon align="inline-end">
									<InputGroup.Button
										disabled={!orca.pairing_url}
										onclick={() =>
											onCopy(orca.pairing_url || '', t('workspace.pairModal.pairingUriCopyLabel'))}
									>
										<CopyIcon data-icon="inline-start" />
										{t('common.actions.copy')}
									</InputGroup.Button>
								</InputGroup.Addon>
							</InputGroup.Root>
							<Field.FieldDescription>
								{t('workspace.pairModal.pairingDesc')}
							</Field.FieldDescription>
						</Field.Field>
					</Tabs.Content>
				</Tabs.Root>

				<!-- Ringkasan Status & Endpoint -->
				<div class="flex flex-col gap-2 rounded-lg border bg-muted/40 p-3 text-xs">
					<div class="flex items-center justify-between gap-3">
						<span class="shrink-0 text-muted-foreground">
							{t('workspace.pairModal.activeEndpoint')}
						</span>
						<span class="truncate font-mono font-medium">{orca.endpoint || '-'}</span>
					</div>
					<Separator />
					<div class="grid grid-cols-2 gap-3">
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">{t('workspace.pairModal.serviceMode')}</span>
							<span class="font-medium">
								{orca.mode === 'desktop'
									? t('workspace.orca.modeDesktopLabel')
									: t('workspace.orca.modeShortHeadless')}
							</span>
						</div>
						<div class="flex items-center justify-between gap-2">
							<span class="text-muted-foreground">{t('workspace.orca.wsPort')}</span>
							<span class="font-mono font-medium">{orca.port || 6768}</span>
						</div>
					</div>
				</div>
			</div>

			<Dialog.Footer class="shrink-0 border-t border-border/50 pt-3 sm:items-center sm:justify-between">
				<Button
					type="button"
					variant="outline"
					size="sm"
					onclick={() => {
						open = false;
						cp.triggerInstall('orca-pair');
					}}
				>
					<TerminalIcon data-icon="inline-start" />
					{t('workspace.pairModal.showQrBtn')}
				</Button>
				<div class="flex flex-col-reverse gap-2 sm:flex-row sm:items-center">
					<Button type="button" variant="outline" size="sm" onclick={() => (open = false)}>
						{t('common.actions.close')}
					</Button>
					{#if webUrl}
						<Button
							size="sm"
							href={webUrl}
							target="_blank"
							rel="noopener noreferrer"
						>
							<ExternalLinkIcon data-icon="inline-start" />
							{t('workspace.orca.openWebClient')}
						</Button>
					{/if}
				</div>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>
{/if}
