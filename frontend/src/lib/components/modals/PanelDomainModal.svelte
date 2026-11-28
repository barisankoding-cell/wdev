<script lang="ts">
	import { api, ApiError } from '#lib/api/client.js';
	import { i18n, t } from '#lib/i18n/index.svelte.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { Field, FieldDescription, FieldLabel } from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as NativeSelect from '#lib/components/ui/native-select/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { authStore } from '#lib/stores/auth.svelte.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { CFHostCheckResult, CFZoneInfo, PanelDomainStatus } from '#lib/types/api.js';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import CheckCircle2Icon from '@lucide/svelte/icons/check-circle-2';
	import CloudIcon from '@lucide/svelte/icons/cloud';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import GlobeIcon from '@lucide/svelte/icons/globe';
	import LockIcon from '@lucide/svelte/icons/lock';
	import PlayIcon from '@lucide/svelte/icons/play';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import { onDestroy, untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	let loading = $state(false);
	let busy = $state(false);
	let errorMsg = $state<string | null>(null);
	let status = $state<PanelDomainStatus | null>(null);

	// Form state
	let domainInputMode = $state<'subdomain' | 'custom'>('subdomain');
	let rootZone = $state('');
	let cfZoneRoot = $state('');
	let subPrefix = $state('panel');
	let customDomain = $state('');
	let sslMode = $state<'letsencrypt' | 'edge' | 'cloudflare' | 'local' | 'external' | 'none'>('letsencrypt');
	let sslEmail = $state('');
	let blockDirectIP = $state(true);
	let syncDNS = $state(true);
	let pendingTargetURL = $state('');

	// Cloudflare Tunnel state
	let selectedCFProfile = $state('');
	let cfZones = $state<CFZoneInfo[]>([]);
	let cfZonesLoading = $state(false);
	let cfZonesError = $state('');
	let cfStartingTunnel = $state(false);
	let cfHostCheck = $state<CFHostCheckResult | null>(null);
	let cfHostChecking = $state(false);
	let cfHostCheckTimer: ReturnType<typeof setTimeout> | null = null;
	let cfHostCheckSeq = 0;

	const activeExistingDomain = $derived(status?.domain || status?.detected_proxy_domain || '');
	const cfProfiles = $derived(status?.cf_profiles ?? []);
	const cfReadyProfiles = $derived(cfProfiles.filter((p) => p.ready));
	const cfHasAPI = $derived(Boolean(status?.cf_has_api && cfReadyProfiles.length > 0));
	const selectedCFProfileInfo = $derived(cfProfiles.find((p) => p.name === selectedCFProfile) ?? null);
	const availableCFZoneNames = $derived(
		Array.from(new Set(cfZones.map((z) => z.name.trim().toLowerCase()).filter(Boolean)))
	);
	const activeRootDomains = $derived(
		sslMode === 'cloudflare' ? availableCFZoneNames : (status?.available_domains ?? [])
	);
	const activeRootZone = $derived(sslMode === 'cloudflare' ? cfZoneRoot : rootZone);

	const resolvedDomain = $derived.by(() => {
		if (domainInputMode === 'subdomain' && activeRootDomains.length > 0 && activeRootZone) {
			const cleanPrefix = subPrefix.trim().toLowerCase().replace(/\.+$/, '');
			return cleanPrefix ? `${cleanPrefix}.${activeRootZone}` : activeRootZone;
		}
		return customDomain.trim().toLowerCase().replace(/\.+$/, '');
	});

	const hasCFHostConflict = $derived.by(() => {
		if (sslMode !== 'cloudflare' || !cfHostCheck || cfHostCheck.available) return false;
		const detHost = status?.detected_cf_exposure?.public_host?.toLowerCase();
		if (detHost && detHost === resolvedDomain) return false;
		return true;
	});

	onDestroy(() => {
		if (cfHostCheckTimer) {
			clearTimeout(cfHostCheckTimer);
			cfHostCheckTimer = null;
		}
	});

	async function loadCFZones(profName: string) {
		if (!profName) {
			cfZones = [];
			cfZoneRoot = '';
			cfZonesError = '';
			return;
		}
		cfZonesLoading = true;
		cfZonesError = '';
		try {
			const res = await api.listCFProfileZones(profName);
			if (selectedCFProfile !== profName) return;
			cfZones = res.zones ?? [];
			const names = Array.from(new Set(cfZones.map((z) => z.name.trim().toLowerCase()).filter(Boolean)));
			if (names.length > 0) {
				if (!cfZoneRoot || !names.includes(cfZoneRoot)) {
					cfZoneRoot = names[0];
				}
				if (sslMode === 'cloudflare' && !customDomain) {
					domainInputMode = 'subdomain';
				}
			} else {
				cfZoneRoot = '';
				if (sslMode === 'cloudflare') {
					domainInputMode = 'custom';
				}
			}
		} catch (err) {
			if (selectedCFProfile !== profName) return;
			cfZones = [];
			cfZoneRoot = '';
			cfZonesError = err instanceof ApiError || err instanceof Error ? err.message : '';
			if (sslMode === 'cloudflare') {
				domainInputMode = 'custom';
			}
		} finally {
			if (selectedCFProfile === profName) {
				cfZonesLoading = false;
			}
		}
	}

	async function loadStatus() {
		loading = true;
		errorMsg = null;
		try {
			const res = await api.getPanelDomain();
			status = res;

			const profs = res.cf_profiles ?? [];
			const readyProfs = profs.filter((p) => p.ready);
			if (readyProfs.length > 0) {
				if (!selectedCFProfile || !readyProfs.some((p) => p.name === selectedCFProfile)) {
					selectedCFProfile = readyProfs[0].name;
				}
			} else if (profs.length > 0) {
				if (!selectedCFProfile || !profs.some((p) => p.name === selectedCFProfile)) {
					selectedCFProfile = profs[0].name;
				}
			} else {
				selectedCFProfile = '';
			}

			if (res.available_domains.length > 0) {
				if (!rootZone || !res.available_domains.includes(rootZone)) {
					rootZone = res.available_domains[0];
				}
			}

			customDomain = res.domain || res.detected_proxy_domain || '';
			if (res.server_mode === 'edge') {
				sslMode = res.edge_configured ? 'edge' : res.cf_has_api ? 'cloudflare' : 'external';
			} else if (res.cf_has_api) {
				sslMode = 'cloudflare';
			} else if (res.server_mode === 'vps') {
				sslMode = 'letsencrypt';
			} else {
				sslMode = 'local';
			}

			if (sslMode === 'cloudflare') {
				if (selectedCFProfile) {
					void loadCFZones(selectedCFProfile);
				}
			} else if (res.available_domains.length === 0) {
				domainInputMode = 'custom';
			} else {
				domainInputMode = 'subdomain';
			}
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : t('modals.panelDomain.loadErrorFallback');
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		if (authStore.panelDomainOpen) {
			untrack(() => {
				pendingTargetURL = '';
				void loadStatus();
			});
		}
	});

	$effect(() => {
		const isOpen = authStore.panelDomainOpen;
		const existing = activeExistingDomain;
		const mode = sslMode;
		const profName = selectedCFProfileInfo?.has_api_token ? selectedCFProfileInfo.name : '';
		const host = resolvedDomain;

		if (cfHostCheckTimer) {
			clearTimeout(cfHostCheckTimer);
			cfHostCheckTimer = null;
		}
		if (!isOpen || existing || mode !== 'cloudflare' || !profName || !host || !host.includes('.')) {
			cfHostCheckSeq += 1;
			cfHostCheck = null;
			cfHostChecking = false;
			return;
		}

		const seq = ++cfHostCheckSeq;
		cfHostChecking = true;
		cfHostCheckTimer = setTimeout(async () => {
			try {
				const chk = await api.checkCFHost(profName, host, host);
				if (seq !== cfHostCheckSeq) return;
				cfHostCheck = chk;
			} catch {
				if (seq !== cfHostCheckSeq) return;
				cfHostCheck = null;
			} finally {
				if (seq === cfHostCheckSeq) {
					cfHostChecking = false;
				}
			}
		}, 350);
	});

	function handleCFProfileChange(nextProf: string) {
		selectedCFProfile = nextProf;
		cfZoneRoot = '';
		cfZones = [];
		void loadCFZones(nextProf);
	}

	function handleSSLModeChange(nextMode: typeof sslMode) {
		sslMode = nextMode;
		if (nextMode === 'cloudflare') {
			if (selectedCFProfile && cfZones.length === 0 && !cfZonesLoading) {
				void loadCFZones(selectedCFProfile);
			}
			if (availableCFZoneNames.length > 0) {
				domainInputMode = 'subdomain';
			} else {
				domainInputMode = 'custom';
			}
		} else if ((status?.available_domains.length ?? 0) === 0) {
			domainInputMode = 'custom';
		}
	}

	async function startCFTunnel(profileName: string) {
		if (!profileName) return;
		cfStartingTunnel = true;
		errorMsg = null;
		try {
			await api.controlCFProfile(profileName, 'start');
			toast.success(t('modals.panelDomain.cfStartedToast', { profile: profileName }));
			await loadStatus();
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : t('modals.panelDomain.cfStartFailed');
		} finally {
			cfStartingTunnel = false;
		}
	}

	async function bindPanelURL(
		target: string,
		targetSSLMode: typeof sslMode,
		targetCFProfile?: string
	) {
		busy = true;
		errorMsg = null;
		try {
			const res = await cp.runWithSudoRetry(
				t('modals.panelDomain.sudoPrompt'),
				(sudoPass) =>
					api.bindPanelDomain({
						domain: target,
						ssl_mode: targetSSLMode,
						cf_profile: targetSSLMode === 'cloudflare' ? targetCFProfile || undefined : undefined,
						email: targetSSLMode === 'letsencrypt' ? sslEmail.trim() : undefined,
						block_direct_ip: blockDirectIP,
						sync_dns: syncDNS,
						sudo_pass: sudoPass
					})
			);
			if (!res) return;

			if (res.status === 'started') {
				const targetURL = res.target_url || '';
				pendingTargetURL = '';
				cp.setPendingPanelDomainURL(targetURL);
				authStore.closePanelDomain();
				cp.openTerminal(t('modals.panelDomain.bindTaskLabel', { domain: target }));
				toast.success(t('modals.panelDomain.bindStartedTitle'), {
					description: t('modals.panelDomain.bindStartedDesc')
				});
			} else {
				const entrancePath = status?.entrance_path || '';
				const scheme = targetSSLMode === 'none' ? 'http' : 'https';
				const boundURL = res.domain_url || res.target_url || `${scheme}://${target}${entrancePath}`;
				toast.success(t('modals.panelDomain.bindSaved', { domain: target }));
				if (res.dns_warning) {
					toast.warning(res.dns_warning);
				}
				if (blockDirectIP && status) {
					pendingTargetURL = boundURL;
					status = {
						...status,
						domain: target,
						bound: true,
						domain_url: boundURL,
						ssl_enabled: targetSSLMode !== 'none',
						ssl_provider:
							res.ssl_provider || (targetSSLMode === 'cloudflare' ? 'cloudflare' : status.ssl_provider),
						block_direct_ip: true,
						detected_cf_exposure:
							targetSSLMode === 'cloudflare'
								? {
										profile: res.cf_profile || targetCFProfile || '',
										local_domain: target,
										public_host: target,
										service_url: `http://127.0.0.1:${status.panel_port}`,
										url: boundURL,
										active: selectedCFProfileInfo?.active ?? Boolean(status.cf_active)
									}
								: status.detected_cf_exposure
					};
				} else {
					await loadStatus();
				}
			}
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : t('modals.panelDomain.bindErrorFallback');
		} finally {
			busy = false;
		}
	}

	async function handleBindSubmit(e: SubmitEvent) {
		e.preventDefault();
		const target = resolvedDomain;
		if (!target) {
			errorMsg = t('modals.panelDomain.domainRequired');
			return;
		}
		await bindPanelURL(target, sslMode, selectedCFProfile);
	}

	async function toggleBlockIP(nextBlock: boolean) {
		busy = true;
		errorMsg = null;
		try {
			await api.setPanelBlockIP(nextBlock);
			toast.success(nextBlock ? t('modals.panelDomain.blockIpOn') : t('modals.panelDomain.blockIpOff'));
			await loadStatus();
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : t('modals.panelDomain.blockIpError');
		} finally {
			busy = false;
		}
	}

	async function togglePanelSSL(nextSSL: boolean) {
		busy = true;
		errorMsg = null;
		try {
			const hosts = window.location.hostname ? [window.location.hostname] : [];
			const res = await api.setPanelSSL(nextSSL, hosts);
			toast.success(nextSSL ? t('modals.panelDomain.sslEnabledToast') : t('modals.panelDomain.sslDisabledToast'));
			if (nextSSL && res.redirect_url && window.location.protocol === 'http:') {
				pendingTargetURL = res.redirect_url;
				window.location.href = res.redirect_url;
				return;
			}
			await loadStatus();
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : t('modals.panelDomain.sslErrorFallback');
		} finally {
			busy = false;
		}
	}

	async function handleUnbind() {
		const targetName =
			activeExistingDomain || status?.detected_cf_exposure?.public_host || 'panel domain';
		const ok = await cp.confirm({
			title: t('modals.panelDomain.unbindConfirmTitle'),
			description: t('modals.panelDomain.unbindConfirmDesc', {
				domain: targetName,
				port: status?.panel_port ?? ''
			}),
			confirmText: t('modals.panelDomain.unbindConfirmBtn'),
			destructive: true
		});
		if (!ok) return;

		busy = true;
		errorMsg = null;
		try {
			const res = await api.unbindPanelDomain(true);
			pendingTargetURL = '';
			cp.clearPendingPanelDomainURL();
			const dnsNote =
				res.dns_records_deleted && res.dns_records_deleted > 0
					? t('modals.panelDomain.unbindDnsNote', { count: res.dns_records_deleted })
					: '';
			toast.success(t('modals.panelDomain.unbindSuccess', { dnsNote }));
			await loadStatus();
		} catch (err) {
			errorMsg = err instanceof ApiError || err instanceof Error ? err.message : t('modals.panelDomain.unbindErrorFallback');
		} finally {
			busy = false;
		}
	}

	async function copyText(val: string) {
		try {
			await navigator.clipboard.writeText(val);
			toast.success(t('modals.panelDomain.urlCopied'));
		} catch {
			toast.error(t('modals.panelDomain.urlCopyFailed'));
		}
	}

	function formatExpiry(iso?: string): string {
		if (!iso) return '';
		return i18n.formatDate(iso, { day: '2-digit', month: 'short', year: 'numeric' });
	}
</script>

<Dialog.Root
	open={authStore.panelDomainOpen}
	onOpenChange={(open) => {
		if (!open) authStore.closePanelDomain();
	}}
>
	<Dialog.Content class="flex max-h-[90dvh] flex-col gap-4 overflow-y-auto sm:max-w-xl">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2">
				<GlobeIcon class="size-4 text-primary" />
				<span>{t('modals.panelDomain.title')}</span>
			</Dialog.Title>
			<Dialog.Description>
				{t('modals.panelDomain.description')}
			</Dialog.Description>
		</Dialog.Header>

		{#if errorMsg}
			<Alert.Root variant="destructive" class="py-2.5">
				<AlertCircleIcon class="size-4" />
				<Alert.Description class="text-xs">{errorMsg}</Alert.Description>
			</Alert.Root>
		{/if}

		{#if pendingTargetURL}
			<Alert.Root class="border-emerald-500/40 bg-emerald-500/10 py-3">
				<ShieldCheckIcon class="size-4 text-emerald-600 dark:text-emerald-400" />
				<Alert.Title class="text-sm font-semibold">{t('modals.panelDomain.newUrlTitle')}</Alert.Title>
				<Alert.Description class="mt-1 space-y-2 text-xs">
					<p>{t('modals.panelDomain.newUrlDesc')}</p>
					<div class="flex flex-wrap items-center gap-2">
						<code class="bg-background rounded border px-2 py-1 font-mono text-xs">{pendingTargetURL}</code>
						<Button size="sm" variant="outline" class="h-7 px-2 text-xs" onclick={() => copyText(pendingTargetURL)}>
							<CopyIcon class="size-3.5" />
							{t('common.actions.copy')}
						</Button>
						<Button size="sm" class="h-7 px-2 text-xs" href={pendingTargetURL} target="_blank" rel="noopener">
							<ExternalLinkIcon class="size-3.5" />
							{t('modals.panelDomain.openNewPanelBtn')}
						</Button>
					</div>
				</Alert.Description>
			</Alert.Root>
		{/if}

		{#if loading && !status}
			<div class="flex items-center justify-center py-6 text-sm">
				<Spinner class="mr-2 size-4" /> {t('modals.panelDomain.loadingStatus')}
			</div>
		{:else if status}
			<div class="bg-muted/30 space-y-2.5 rounded-lg border p-3.5 text-xs">
				<div class="flex flex-wrap items-center justify-between gap-2">
					<div class="flex items-center gap-2">
						<LockIcon class="size-4 text-primary" />
						<span class="font-semibold">{t('modals.panelDomain.directSslTitle', { port: status.panel_port })}</span>
					</div>
					<Badge variant={status.panel_ssl_enabled ? 'default' : 'secondary'} class="text-[11px]">
						{status.panel_ssl_enabled ? t('modals.panelDomain.directSslBadgeHttps') : t('modals.panelDomain.directSslBadgeHttp')}
					</Badge>
				</div>
				<p class="text-muted-foreground leading-relaxed">
					{t('modals.panelDomain.directSslDesc', { port: status.panel_port })}
				</p>
				{#if status.panel_ssl_expires_at}
					<p class="text-muted-foreground">
						{t('modals.panelDomain.certExpires')} <strong class="text-foreground">{formatExpiry(status.panel_ssl_expires_at)}</strong>
						{#if typeof status.panel_ssl_days_left === 'number'}
							{t('modals.panelDomain.daysRemaining', { days: status.panel_ssl_days_left })}
						{/if}
					</p>
				{/if}
				<div class="flex flex-wrap items-center gap-2 pt-0.5">
					<Button
						size="sm"
						variant={status.panel_ssl_enabled ? 'outline' : 'default'}
						class="h-7 text-xs"
						disabled={busy}
						onclick={() => togglePanelSSL(!status?.panel_ssl_enabled)}
					>
						{#if busy}
							<Spinner class="mr-1.5 size-3.5" />
						{/if}
						{status.panel_ssl_enabled ? t('modals.panelDomain.disableDirectSsl') : t('modals.panelDomain.enableDirectSsl')}
					</Button>
				</div>
			</div>

			{#if activeExistingDomain}
				{@const isFormalBound = Boolean(status.domain)}
				{@const cfExp = status.detected_cf_exposure}
				{@const cardURL = status.domain_url || status.detected_proxy_url || ''}
				{@const rawProvider = status.ssl_provider || status.detected_proxy_ssl_provider || ''}
				{@const cardProvider =
					cfExp && (rawProvider === 'cloudflare' || rawProvider === '')
						? t('modals.panelDomain.cfProviderTag', { profile: cfExp.profile || 'default' })
						: rawProvider}
				{@const cardHasSSL = isFormalBound ? status.ssl_enabled : Boolean(cardProvider || cardURL.startsWith('https://'))}
				<div class="bg-muted/40 space-y-3 rounded-lg border p-3.5 text-xs">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<div class="flex items-center gap-2">
							<ShieldCheckIcon class="size-4 text-emerald-500" />
							<span class="font-semibold">
								{isFormalBound
									? t('modals.panelDomain.boundDomainLabel', { domain: status.domain })
									: t('modals.panelDomain.detectedDomainLabel', {
											port: status.panel_port,
											domain: status.detected_proxy_domain ?? ''
										})}
							</span>
						</div>
						<div class="flex flex-wrap items-center gap-1.5">
							<Badge variant={cardHasSSL ? 'default' : 'secondary'} class="text-[11px]">
								{cardHasSSL ? `HTTPS (${cardProvider || 'SSL'})` : 'HTTP'}
							</Badge>
							{#if cfExp}
								<Badge variant={cfExp.active ? 'default' : 'outline'} class="text-[11px]">
									{cfExp.active ? t('modals.panelDomain.cfTunnelActiveBadge') : t('modals.panelDomain.cfTunnelInactiveBadge')}
								</Badge>
							{/if}
							{#if isFormalBound}
								<Badge variant={status.block_direct_ip ? 'destructive' : 'outline'} class="text-[11px]">
									{status.block_direct_ip ? t('modals.panelDomain.ipBlockedBadge') : t('modals.panelDomain.ipAllowedBadge')}
								</Badge>
							{:else}
								<Badge variant="outline" class="text-[11px]">
									{t('modals.panelDomain.proxyPortBadge', { port: status.panel_port })}
								</Badge>
							{/if}
						</div>
					</div>

					{#if cardURL}
						<div class="flex flex-wrap items-center gap-2">
							<code class="bg-background min-w-0 flex-1 truncate rounded border px-2 py-1 font-mono">{cardURL}</code>
							<Button size="sm" variant="outline" class="h-7 px-2 text-xs" onclick={() => copyText(cardURL)}>
								<CopyIcon class="size-3.5" />
								{t('common.actions.copy')}
							</Button>
							<Button size="sm" class="h-7 px-2 text-xs" href={cardURL} target="_blank" rel="noopener">
								<ExternalLinkIcon class="size-3.5" />
								{t('common.actions.open')}
							</Button>
						</div>
					{/if}

					{#if status.ssl_expires_at}
						<p class="text-muted-foreground">
							{t('modals.panelDomain.sslExpires')} <strong class="text-foreground">{formatExpiry(status.ssl_expires_at)}</strong>
							{#if typeof status.ssl_days_left === 'number'}
								{t('modals.panelDomain.daysRemaining', { days: status.ssl_days_left })}
							{/if}
						</p>
					{/if}

					{#if cfExp && !cfExp.active}
						<Alert.Root class="border-amber-500/40 bg-amber-500/10 py-2.5">
							<AlertCircleIcon class="size-4 text-amber-600 dark:text-amber-400" />
							<Alert.Description class="flex flex-wrap items-center justify-between gap-2 text-xs leading-relaxed">
								<span>{t('modals.panelDomain.cfInactiveWarning', { profile: cfExp.profile })}</span>
								{#if cfExp.profile}
									<Button
										type="button"
										size="sm"
										variant="outline"
										class="h-7 text-xs"
										disabled={cfStartingTunnel}
										onclick={() => void startCFTunnel(cfExp.profile)}
									>
										{#if cfStartingTunnel}
											<Spinner class="size-3.5" />
										{:else}
											<PlayIcon class="size-3.5" />
										{/if}
										{t('modals.panelDomain.cfStartTunnelBtn')}
									</Button>
								{/if}
							</Alert.Description>
						</Alert.Root>
					{:else}
						<Alert.Root class="border-amber-500/40 bg-amber-500/10 py-2.5">
							<AlertCircleIcon class="size-4 text-amber-600 dark:text-amber-400" />
							<Alert.Description class="text-xs leading-relaxed">
								{t('modals.panelDomain.portInUseAlert', { port: status.panel_port, domain: activeExistingDomain })}
							</Alert.Description>
						</Alert.Root>
					{/if}

					<div class="flex flex-wrap items-center justify-between gap-2 pt-1">
						{#if isFormalBound}
							<Button
								type="button"
								variant="outline"
								size="sm"
								class="h-7 text-xs"
								disabled={busy}
								onclick={() => toggleBlockIP(!status!.block_direct_ip)}
							>
								<LockIcon class="size-3.5" />
								{status.block_direct_ip ? t('modals.panelDomain.allowIpTempBtn') : t('modals.panelDomain.blockIpBtn')}
							</Button>
						{:else}
							<span></span>
						{/if}
						<Button
							type="button"
							variant="destructive"
							size="sm"
							class="h-7 text-xs"
							disabled={busy}
							onclick={handleUnbind}
						>
							<Trash2Icon class="size-3.5" />
							{t('modals.panelDomain.removeDomainDnsBtn')}
						</Button>
					</div>
				</div>

				<Dialog.Footer>
					<Button type="button" variant="outline" onclick={() => authStore.closePanelDomain()} disabled={busy}>
						{t('common.actions.close')}
					</Button>
				</Dialog.Footer>
			{:else if status.detected_cf_exposure}
				{@const detCF = status.detected_cf_exposure}
				<div class="bg-muted/40 space-y-3 rounded-lg border p-3.5 text-xs">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<div class="flex items-center gap-2">
							<CloudIcon class="size-4 text-sky-500" />
							<span class="font-semibold">
								{t('modals.panelDomain.cfDetectedRouteTitle', {
									port: status.panel_port,
									domain: detCF.public_host
								})}
							</span>
						</div>
						<div class="flex flex-wrap items-center gap-1.5">
							<Badge variant="default" class="text-[11px]">
								{t('modals.panelDomain.cfProviderTag', { profile: detCF.profile || 'default' })}
							</Badge>
							<Badge variant={detCF.active ? 'default' : 'secondary'} class="text-[11px]">
								{detCF.active ? t('modals.panelDomain.cfTunnelActiveBadge') : t('modals.panelDomain.cfTunnelInactiveBadge')}
							</Badge>
						</div>
					</div>

					<p class="text-muted-foreground leading-relaxed">
						{t('modals.panelDomain.cfDetectedRouteDesc', {
							profile: detCF.profile || 'default',
							domain: detCF.public_host,
							service: detCF.service_url || `http://127.0.0.1:${status.panel_port}`
						})}
					</p>

					<div class="flex flex-wrap items-center gap-2">
						<code class="bg-background min-w-0 flex-1 truncate rounded border px-2 py-1 font-mono">{detCF.url}</code>
						<Button size="sm" variant="outline" class="h-7 px-2 text-xs" onclick={() => copyText(detCF.url)}>
							<CopyIcon class="size-3.5" />
							{t('common.actions.copy')}
						</Button>
						<Button size="sm" variant="outline" class="h-7 px-2 text-xs" href={detCF.url} target="_blank" rel="noopener">
							<ExternalLinkIcon class="size-3.5" />
							{t('common.actions.open')}
						</Button>
					</div>

					{#if !detCF.active && detCF.profile}
						<Alert.Root class="border-amber-500/40 bg-amber-500/10 py-2.5">
							<AlertCircleIcon class="size-4 text-amber-600 dark:text-amber-400" />
							<Alert.Description class="flex flex-wrap items-center justify-between gap-2 text-xs leading-relaxed">
								<span>{t('modals.panelDomain.cfInactiveWarning', { profile: detCF.profile })}</span>
								<Button
									type="button"
									size="sm"
									variant="outline"
									class="h-7 text-xs"
									disabled={cfStartingTunnel}
									onclick={() => void startCFTunnel(detCF.profile)}
								>
									{#if cfStartingTunnel}
										<Spinner class="size-3.5" />
									{:else}
										<PlayIcon class="size-3.5" />
									{/if}
									{t('modals.panelDomain.cfStartTunnelBtn')}
								</Button>
							</Alert.Description>
						</Alert.Root>
					{/if}

					<label class="flex items-center gap-2 pt-0.5">
						<Checkbox bind:checked={blockDirectIP} />
						<span>{t('modals.panelDomain.blockDirectIpCheck')}</span>
					</label>

					<div class="flex flex-wrap items-center justify-between gap-2 pt-1">
						<Button
							type="button"
							variant="destructive"
							size="sm"
							class="h-7 text-xs"
							disabled={busy}
							onclick={handleUnbind}
						>
							<Trash2Icon class="size-3.5" />
							{t('modals.panelDomain.cfRemoveDetectedBtn')}
						</Button>
						<Button
							type="button"
							size="sm"
							class="h-7 text-xs"
							disabled={busy}
							onclick={() => void bindPanelURL(detCF.public_host, 'external')}
						>
							{#if busy}
								<Spinner class="size-3.5" />
							{:else}
								<ShieldCheckIcon class="size-3.5" />
							{/if}
							{t('modals.panelDomain.cfBindDetectedBtn')}
						</Button>
					</div>
				</div>

				<Dialog.Footer>
					<Button type="button" variant="outline" onclick={() => authStore.closePanelDomain()} disabled={busy}>
						{t('common.actions.close')}
					</Button>
				</Dialog.Footer>
			{:else}
			<form onsubmit={handleBindSubmit} class="space-y-4">
				{#if cfProfiles.length > 0}
					<div
						class={`space-y-2 rounded-lg border p-3 text-xs ${
							cfHasAPI
								? 'border-sky-500/35 bg-sky-500/5'
								: 'border-amber-500/35 bg-amber-500/5'
						}`}
					>
						<div class="flex flex-wrap items-center justify-between gap-2">
							<div class="flex items-center gap-2 font-semibold">
								<CloudIcon class={`size-4 ${cfHasAPI ? 'text-sky-500' : 'text-amber-500'}`} />
								<span>{t('modals.panelDomain.cfPanelDetectTitle')}</span>
							</div>
							<div class="flex flex-wrap items-center gap-1.5">
								<Badge variant={cfHasAPI ? 'default' : 'secondary'} class="text-[11px]">
									{cfHasAPI ? t('modals.panelDomain.cfDetectedReadyBadge') : t('modals.panelDomain.cfDetectedNoApiBadge')}
								</Badge>
								{#if selectedCFProfileInfo}
									<Badge variant={selectedCFProfileInfo.active ? 'default' : 'outline'} class="text-[11px]">
										{selectedCFProfileInfo.active
											? t('modals.panelDomain.cfTunnelActiveBadge')
											: t('modals.panelDomain.cfTunnelInactiveBadge')}
									</Badge>
								{/if}
							</div>
						</div>
						{#if cfHasAPI}
							<p class="text-muted-foreground leading-relaxed">
								{t('modals.panelDomain.cfPanelReadyDesc')}
							</p>
						{:else}
							<div class="text-muted-foreground flex flex-wrap items-center justify-between gap-2 leading-relaxed">
								<span>
									{t('modals.panelDomain.cfPanelNoApiDesc', {
										profiles: cfProfiles.map((p) => p.name).join(', ')
									})}
								</span>
								<Button
									size="sm"
									variant="outline"
									class="h-7 shrink-0 text-xs"
									href="/cloudflare"
									onclick={() => authStore.closePanelDomain()}
								>
									<ExternalLinkIcon class="size-3.5" />
									{t('modals.panelDomain.cfOpenSettingsLink')}
								</Button>
							</div>
						{/if}
					</div>
				{/if}

				<Field>
					<FieldLabel>{t('modals.panelDomain.sslModeLabel')}</FieldLabel>
					<NativeSelect.Root
						value={sslMode}
						class="w-full"
						onchange={(e) => handleSSLModeChange(e.currentTarget.value as typeof sslMode)}
					>
						{#if cfHasAPI}
							<NativeSelect.Option value="cloudflare">
								{t('modals.panelDomain.sslModeCloudflare')}
							</NativeSelect.Option>
						{/if}
						{#if status.server_mode === 'edge'}
							<NativeSelect.Option value="edge">
								{t('modals.panelDomain.sslModeEdge')}
							</NativeSelect.Option>
						{:else}
							<NativeSelect.Option value="letsencrypt">
								{t('modals.panelDomain.sslModeLetsencrypt')}
							</NativeSelect.Option>
						{/if}
						<NativeSelect.Option value="local">
							{t('modals.panelDomain.sslModeLocal')}
						</NativeSelect.Option>
						<NativeSelect.Option value="external">
							{t('modals.panelDomain.sslModeExternal')}
						</NativeSelect.Option>
						<NativeSelect.Option value="none">
							{t('modals.panelDomain.sslModeNone')}
						</NativeSelect.Option>
					</NativeSelect.Root>
					{#if status.server_mode === 'edge' && sslMode === 'edge'}
						<FieldDescription>
							{#if status.edge_configured}
								{t('modals.panelDomain.sslEdgeHelpConfigured', { ip: status.public_ip || 'Gateway' })}
							{:else}
								{t('modals.panelDomain.sslEdgeHelpUnconfigured')}
							{/if}
						</FieldDescription>
					{/if}
				</Field>

				{#if sslMode === 'cloudflare' && cfReadyProfiles.length > 0}
					<Field>
						<FieldLabel>{t('modals.panelDomain.cfProfileLabel')}</FieldLabel>
						<div class="flex flex-wrap items-center gap-2">
							<NativeSelect.Root
								value={selectedCFProfile}
								class="min-w-0 flex-1"
								onchange={(e) => handleCFProfileChange(e.currentTarget.value)}
							>
								{#each cfReadyProfiles as prof (prof.name)}
									<NativeSelect.Option value={prof.name}>
										{prof.name}{prof.tunnel_name ? ` (${prof.tunnel_name})` : ''} · {prof.active ? t('modals.panelDomain.cfTunnelActiveBadge') : t('modals.panelDomain.cfTunnelInactiveBadge')}
									</NativeSelect.Option>
								{/each}
							</NativeSelect.Root>
							{#if selectedCFProfileInfo && !selectedCFProfileInfo.active}
								<Button
									type="button"
									size="sm"
									variant="outline"
									class="h-8 shrink-0 text-xs"
									disabled={cfStartingTunnel}
									onclick={() => void startCFTunnel(selectedCFProfileInfo.name)}
								>
									{#if cfStartingTunnel}
										<Spinner class="size-3.5" />
									{:else}
										<PlayIcon class="size-3.5" />
									{/if}
									{t('modals.panelDomain.cfStartTunnelBtn')}
								</Button>
							{/if}
						</div>
						{#if cfZonesLoading}
							<p class="text-muted-foreground mt-1 flex items-center gap-1.5 text-[11px]">
								<Spinner class="size-3" /> {t('modals.panelDomain.cfZonesLoading')}
							</p>
						{:else if cfZonesError}
							<p class="text-amber-600 dark:text-amber-400 mt-1 text-[11px]">{cfZonesError}</p>
						{:else if availableCFZoneNames.length === 0}
							<p class="text-muted-foreground mt-1 text-[11px]">{t('modals.panelDomain.cfZonesEmpty')}</p>
						{/if}
					</Field>

					{#if selectedCFProfileInfo && !selectedCFProfileInfo.active}
						<Alert.Root class="border-amber-500/40 bg-amber-500/10 py-2.5">
							<AlertCircleIcon class="size-4 text-amber-600 dark:text-amber-400" />
							<Alert.Description class="text-xs leading-relaxed">
								{t('modals.panelDomain.cfInactiveWarning', { profile: selectedCFProfileInfo.name })}
							</Alert.Description>
						</Alert.Root>
					{/if}
				{/if}

				{#if activeRootDomains.length > 0}
					<div class="grid grid-cols-2 gap-1.5 rounded-lg border bg-muted/30 p-1">
						<button
							type="button"
							class={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
								domainInputMode === 'subdomain' ? 'bg-background shadow-xs text-foreground' : 'text-muted-foreground hover:text-foreground'
							}`}
							onclick={() => (domainInputMode = 'subdomain')}
						>
							{t('modals.panelDomain.tabSubdomain')}
						</button>
						<button
							type="button"
							class={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
								domainInputMode === 'custom' ? 'bg-background shadow-xs text-foreground' : 'text-muted-foreground hover:text-foreground'
							}`}
							onclick={() => (domainInputMode = 'custom')}
						>
							{t('modals.panelDomain.tabCustom')}
						</button>
					</div>
				{/if}

				{#if domainInputMode === 'subdomain' && activeRootDomains.length > 0}
					<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
						<Field>
							<FieldLabel for="pd-sub-prefix">{t('modals.panelDomain.subPrefixLabel')}</FieldLabel>
							<Input
								id="pd-sub-prefix"
								bind:value={subPrefix}
								placeholder="panel"
								class="font-mono text-xs"
								required
							/>
						</Field>
						<Field>
							<FieldLabel>{t('modals.panelDomain.parentDomainLabel')}</FieldLabel>
							{#if sslMode === 'cloudflare'}
								<NativeSelect.Root bind:value={cfZoneRoot} class="w-full">
									{#each activeRootDomains as dom (dom)}
										<NativeSelect.Option value={dom}>{dom}</NativeSelect.Option>
									{/each}
								</NativeSelect.Root>
							{:else}
								<NativeSelect.Root bind:value={rootZone} class="w-full">
									{#each activeRootDomains as dom (dom)}
										<NativeSelect.Option value={dom}>{dom}</NativeSelect.Option>
									{/each}
								</NativeSelect.Root>
							{/if}
						</Field>
					</div>
				{:else}
					<Field>
						<FieldLabel for="pd-custom-domain">{t('modals.panelDomain.customDomainLabel')}</FieldLabel>
						<Input
							id="pd-custom-domain"
							bind:value={customDomain}
							placeholder={t('modals.panelDomain.customDomainPlaceholder')}
							class="font-mono text-xs"
							required
						/>
						<FieldDescription>
							{t('modals.panelDomain.customDomainHelp')}
						</FieldDescription>
					</Field>
				{/if}

				{#if resolvedDomain}
					<div class="bg-muted/30 space-y-1.5 rounded-md border px-3 py-2 text-xs">
						<div>
							{sslMode === 'cloudflare' ? t('modals.panelDomain.cfRouteTargetPreview') : t('modals.panelDomain.forwardingTarget')}
							<code class="font-mono font-semibold">{sslMode === 'none' ? 'http' : 'https'}://{resolvedDomain}{status.entrance_path}</code>
							→ <code class="font-mono">http://127.0.0.1:{status.panel_port}</code>
						</div>
						{#if sslMode === 'cloudflare'}
							{#if cfHostChecking}
								<div class="text-muted-foreground flex items-center gap-1.5 text-[11px]">
									<Spinner class="size-3" /> {t('modals.panelDomain.cfHostChecking')}
								</div>
							{:else if cfHostCheck}
								<div
									class={`flex items-start gap-1.5 text-[11px] ${
										hasCFHostConflict
											? 'text-destructive'
											: 'text-emerald-600 dark:text-emerald-400'
									}`}
								>
									{#if hasCFHostConflict}
										<AlertCircleIcon class="mt-0.5 size-3.5 shrink-0" />
									{:else}
										<CheckCircle2Icon class="mt-0.5 size-3.5 shrink-0" />
									{/if}
									<span>{cfHostCheck.check_error || cfHostCheck.message}</span>
								</div>
							{/if}
						{/if}
					</div>
				{/if}

				{#if sslMode === 'letsencrypt'}
					<Field>
						<FieldLabel for="pd-ssl-email">{t('modals.panelDomain.sslEmailLabel')}</FieldLabel>
						<Input
							id="pd-ssl-email"
							type="email"
							bind:value={sslEmail}
							placeholder="admin@yourdomain.com"
							class="text-xs"
						/>
					</Field>
				{/if}

				<div class="space-y-2 text-xs">
					<label class="flex items-center gap-2">
						<Checkbox bind:checked={blockDirectIP} />
						<span>{t('modals.panelDomain.blockDirectIpCheck')}</span>
					</label>
					{#if status.server_mode === 'vps' && sslMode !== 'external' && sslMode !== 'cloudflare'}
						<label class="flex items-center gap-2">
							<Checkbox bind:checked={syncDNS} />
							<span>{t('modals.panelDomain.syncDnsCheck', { ip: status.public_ip || 'VPS IP' })}</span>
						</label>
					{/if}
				</div>

				<p class="text-muted-foreground text-[11px]">
					{t('modals.panelDomain.emergencyRecovery')}
				</p>

				<Dialog.Footer>
					<Button type="button" variant="outline" onclick={() => authStore.closePanelDomain()} disabled={busy}>
						{t('common.actions.close')}
					</Button>
					<Button type="submit" disabled={busy || !resolvedDomain || hasCFHostConflict}>
						{#if busy}
							<Spinner class="size-4" />
							{t('modals.panelDomain.preparingBtn')}
						{:else}
							{t('modals.panelDomain.bindApplyBtn')}
						{/if}
					</Button>
				</Dialog.Footer>
			</form>
			{/if}
		{/if}
	</Dialog.Content>
</Dialog.Root>
