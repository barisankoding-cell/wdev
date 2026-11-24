<script lang="ts">
	import { untrack } from 'svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import FolderPickerDialog from '#lib/components/modals/FolderPickerDialog.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { api } from '#lib/api/client.js';
	import { cp } from '#lib/stores/control-plane.svelte.js';
	import type { ComposeProject, ComposeTemplateSpec } from '#lib/types/api.js';
	import { toast } from 'svelte-sonner';
	import AlertCircleIcon from '@lucide/svelte/icons/alert-circle';
	import FolderOpenIcon from '@lucide/svelte/icons/folder-open';
	import PlusIcon from '@lucide/svelte/icons/plus';

	let {
		open = $bindable(false),
		templates = [],
		initialDir = '',
		onCreated
	}: {
		open: boolean;
		templates: ComposeTemplateSpec[];
		initialDir?: string;
		onCreated?: (proj: ComposeProject, taskId?: number) => void | Promise<void>;
	} = $props();

	let modeTab = $state<'template' | 'register'>('template');
	let projectName = $state('');
	let dirPath = $state('');
	let dirTouched = $state(false);
	let composeFile = $state('compose.yaml');
	let description = $state('');
	let selectedTemplate = $state('nginx-static');
	let composeContent = $state('');
	let envContent = $state('');
	let overwrite = $state(false);
	let startAfter = $state(true);
	let submitting = $state(false);
	let formError = $state('');
	let folderPickerOpen = $state(false);
	let prevOpen = false;

	const selectedTemplateMeta = $derived(templates.find((t) => t.id === selectedTemplate) ?? null);

	function sanitizeSlug(raw: string): string {
		return raw
			.trim()
			.toLowerCase()
			.replace(/[^a-z0-9_-]+/g, '-')
			.replace(/^-+|-+$/g, '')
			.slice(0, 63);
	}

	function applyTemplateContent(tplId: string, name: string) {
		const found = templates.find((t) => t.id === tplId) ?? templates[0];
		if (!found) return;
		const safeName = sanitizeSlug(name) || 'myapp';
		composeContent = found.compose_content.replaceAll('{{PROJECT_NAME}}', safeName);
		envContent = found.env_content.replaceAll('{{PROJECT_NAME}}', safeName);
	}

	$effect(() => {
		const isOpen = open;
		if (isOpen && !prevOpen) {
			prevOpen = true;
			untrack(() => {
				formError = '';
				description = '';
				overwrite = false;
				const cleanInitDir = initialDir.trim();
				if (cleanInitDir) {
					modeTab = 'register';
					dirPath = cleanInitDir;
					dirTouched = true;
					const folderName = cleanInitDir.split('/').filter(Boolean).pop() || 'stack';
					projectName = sanitizeSlug(folderName) || 'stack';
					composeFile = '';
					startAfter = false;
				} else {
					modeTab = 'template';
					projectName = '';
					dirPath = '';
					dirTouched = false;
					composeFile = 'compose.yaml';
					selectedTemplate = templates[0]?.id || 'nginx-static';
					startAfter = true;
					applyTemplateContent(selectedTemplate, 'myapp');
				}
			});
		} else if (!isOpen) {
			prevOpen = false;
		}
	});

	$effect(() => {
		if (modeTab === 'template' && !dirTouched) {
			const slug = sanitizeSlug(projectName);
			dirPath = slug ? `/var/www/${slug}` : '';
		}
	});

	function handleTemplateChange(nextId: string) {
		selectedTemplate = nextId;
		applyTemplateContent(nextId, projectName);
	}

	function handleProjectNameInput() {
		if (modeTab === 'template') {
			applyTemplateContent(selectedTemplate, projectName);
		}
	}

	function handleFolderPicked(picked: string) {
		dirPath = picked;
		dirTouched = true;
		if (!projectName.trim()) {
			const seg = picked.split('/').filter(Boolean).pop() || '';
			projectName = sanitizeSlug(seg);
		}
	}

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		formError = '';

		const cleanName = sanitizeSlug(projectName);
		if (!cleanName) {
			formError = 'Nama proyek wajib diisi (huruf kecil, angka, "-" atau "_").';
			return;
		}
		const cleanDir = dirPath.trim();
		if (!cleanDir || !cleanDir.startsWith('/')) {
			formError = 'Direktori proyek harus berupa path absolut (mis. /var/www/myapp).';
			return;
		}

		submitting = true;
		try {
			const res = await api.createComposeProject({
				name: cleanName,
				dir_path: cleanDir,
				compose_file: composeFile.trim() || undefined,
				description: description.trim() || undefined,
				...(modeTab === 'template'
					? {
							template: selectedTemplate,
							compose_content: composeContent,
							env_content: envContent,
							overwrite
						}
					: {}),
				start_after: startAfter
			});

			toast.success(`Proyek Compose "${res.project.name}" berhasil disimpan.`);
			open = false;
			if (res.task_id) {
				cp.openTerminal(`Compose Up: ${res.project.name}`);
			}
			await onCreated?.(res.project, res.task_id);
		} catch (err) {
			formError = err instanceof Error ? err.message : 'Gagal menyimpan proyek Compose';
		} finally {
			submitting = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="flex max-h-[90dvh] max-w-[calc(100vw-1.5rem)] flex-col gap-0 overflow-hidden p-4 sm:max-w-[min(calc(100vw-2rem),42rem)]">
		<Dialog.Header class="shrink-0 border-b border-border/50 pb-3 pr-8">
			<Dialog.Title class="leading-snug">Tambah / Daftarkan Stack Docker Compose v5</Dialog.Title>
			<Dialog.Description class="text-xs sm:text-sm">
				Buat proyek Compose baru dari template modern (tanpa kunci <code>version:</code> usang) atau daftarkan folder yang sudah ada.
			</Dialog.Description>
		</Dialog.Header>

		<div class="-mx-1 min-h-0 flex-1 overflow-y-auto overscroll-contain px-1 py-4">
			<form id="create-compose-form" onsubmit={handleSubmit} class="flex flex-col gap-5">
				{#if formError}
					<Alert.Root variant="destructive">
						<AlertCircleIcon />
						<Alert.Title>Gagal Memproses Stack</Alert.Title>
						<Alert.Description>{formError}</Alert.Description>
					</Alert.Root>
				{/if}

				<Field.FieldGroup class="gap-4">
					<Field.Field class="min-w-0">
						<Field.FieldTitle id="compose-create-mode-label">Metode Pembuatan</Field.FieldTitle>
						<ToggleGroup.Root
							type="single"
							variant="outline"
							spacing={2}
							aria-labelledby="compose-create-mode-label"
							value={modeTab}
							onValueChange={(v) => {
								if (v === 'template' || v === 'register') modeTab = v;
							}}
							class="grid w-full grid-cols-2"
						>
							<ToggleGroup.Item
								value="template"
								class="h-auto min-h-10 py-1.5 text-center text-xs leading-tight whitespace-normal sm:min-h-8 sm:text-sm"
							>
								Buat dari Template
							</ToggleGroup.Item>
							<ToggleGroup.Item
								value="register"
								class="h-auto min-h-10 py-1.5 text-center text-xs leading-tight whitespace-normal sm:min-h-8 sm:text-sm"
							>
								Daftarkan Folder
							</ToggleGroup.Item>
						</ToggleGroup.Root>
					</Field.Field>

					<Field.FieldGroup class="grid gap-4 sm:grid-cols-2">
						<Field.Field class="min-w-0">
							<Field.FieldLabel for="compose-proj-name">Nama Proyek</Field.FieldLabel>
							<Input
								id="compose-proj-name"
								bind:value={projectName}
								oninput={handleProjectNameInput}
								placeholder="myapp-stack"
								class="h-10 font-mono sm:h-8"
								required
							/>
							<Field.FieldDescription>Huruf kecil a-z, angka 0-9, '-' atau '_'.</Field.FieldDescription>
						</Field.Field>

						<Field.Field class="min-w-0">
							<Field.FieldLabel for="compose-file-name">Nama File Compose</Field.FieldLabel>
							<Input
								id="compose-file-name"
								bind:value={composeFile}
								placeholder={modeTab === 'register' ? 'otomatis (compose.yaml)' : 'compose.yaml'}
								class="h-10 font-mono sm:h-8"
							/>
							<Field.FieldDescription>Kosongkan untuk deteksi otomatis saat registrasi.</Field.FieldDescription>
						</Field.Field>
					</Field.FieldGroup>

					<Field.Field class="min-w-0">
						<Field.FieldLabel for="compose-dir-path">Direktori Proyek</Field.FieldLabel>
						<InputGroup.Root class="h-10 sm:h-8">
							<InputGroup.Input
								id="compose-dir-path"
								bind:value={dirPath}
								oninput={() => (dirTouched = true)}
								placeholder="/var/www/myapp-stack"
								class="font-mono"
								required
							/>
							<InputGroup.Addon align="inline-end">
								<InputGroup.Button
									variant="outline"
									class="min-h-7 sm:min-h-0"
									aria-label="Pilih folder proyek"
									onclick={() => (folderPickerOpen = true)}
								>
									<FolderOpenIcon data-icon="inline-start" />
									<span class="hidden sm:inline">Pilih Folder</span>
									<span class="sm:hidden">Pilih</span>
								</InputGroup.Button>
							</InputGroup.Addon>
						</InputGroup.Root>
						<Field.FieldDescription>
							Lokasi yang diizinkan: <code>/var/www</code>, <code>/home</code>, <code>/srv</code>, <code>/opt</code>, atau <code>/mnt</code>.
						</Field.FieldDescription>
					</Field.Field>
				</Field.FieldGroup>

				<Separator />

				{#if modeTab === 'template'}
					<Field.FieldGroup class="gap-4">
						<Field.Field class="min-w-0">
							<Field.FieldLabel for="compose-template">Template Starter Compose v5</Field.FieldLabel>
							<Select.Root
								type="single"
								value={selectedTemplate}
								onValueChange={(v) => v && handleTemplateChange(v)}
							>
								<Select.Trigger id="compose-template" class="h-10 w-full min-w-0 sm:h-8">
									<span class="truncate">
										{selectedTemplateMeta
											? `${selectedTemplateMeta.name} (${selectedTemplateMeta.category})`
											: 'Pilih template Compose'}
									</span>
								</Select.Trigger>
								<Select.Content>
									<Select.Group>
										{#each templates as tpl (tpl.id)}
											<Select.Item value={tpl.id}>
												{tpl.name} ({tpl.category})
											</Select.Item>
										{/each}
									</Select.Group>
								</Select.Content>
							</Select.Root>
							{#if selectedTemplateMeta}
								<Field.FieldDescription>{selectedTemplateMeta.description}</Field.FieldDescription>
							{/if}
						</Field.Field>

						<Field.Field class="min-w-0">
							<Field.FieldLabel for="compose-yaml-preview">
								<span>Isi <code>compose.yaml</code></span>
							</Field.FieldLabel>
							<Textarea
								id="compose-yaml-preview"
								bind:value={composeContent}
								rows={9}
								class="max-h-48 min-h-36 resize-y overflow-y-auto font-mono text-xs sm:max-h-64"
								spellcheck={false}
							/>
						</Field.Field>

						<Field.Field class="min-w-0">
							<Field.FieldLabel for="compose-env-preview">
								<span>Isi <code>.env</code> (opsional, izin 0600)</span>
							</Field.FieldLabel>
							<Textarea
								id="compose-env-preview"
								bind:value={envContent}
								rows={3}
								class="max-h-28 min-h-20 resize-y overflow-y-auto font-mono text-xs sm:max-h-36"
								spellcheck={false}
							/>
						</Field.Field>

						<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
							<Checkbox id="compose-overwrite-files" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={overwrite} />
							<Field.FieldLabel for="compose-overwrite-files" class="cursor-pointer text-xs leading-snug font-normal">
								<span>
									Timpa <code>compose.yaml</code> &amp; <code>.env</code> jika berkas sudah ada di folder target
								</span>
							</Field.FieldLabel>
						</Field.Field>
					</Field.FieldGroup>
				{:else}
					<Field.FieldGroup class="gap-4">
						<Alert.Root>
							<Alert.Title>Registrasi Direktori Existing</Alert.Title>
							<Alert.Description class="text-xs">
								<code>wdev</code> akan membaca berkas <code>compose.yaml</code> / <code>docker-compose.yml</code> serta <code>.env</code> yang sudah ada di folder tanpa mengubah isinya.
							</Alert.Description>
						</Alert.Root>

						<Field.Field class="min-w-0">
							<Field.FieldLabel for="compose-desc">Catatan / Deskripsi (opsional)</Field.FieldLabel>
							<Input
								id="compose-desc"
								bind:value={description}
								class="h-10 sm:h-8"
								placeholder="Contoh: Backend API & Redis staging"
							/>
						</Field.Field>
					</Field.FieldGroup>
				{/if}

				<Field.FieldSet class="rounded-lg border bg-muted/30 p-3">
					<Field.Field orientation="horizontal" class="items-start gap-2.5 sm:items-center">
						<Checkbox id="compose-start-after" class="mt-0.5 shrink-0 sm:mt-0" bind:checked={startAfter} />
						<Field.FieldLabel for="compose-start-after" class="cursor-pointer text-xs leading-snug font-normal">
							<span>
								Langsung jalankan stack (<code>docker compose up -d</code>) setelah disimpan
							</span>
						</Field.FieldLabel>
					</Field.Field>
				</Field.FieldSet>
			</form>
		</div>

		<Dialog.Footer class="shrink-0 pb-[max(1rem,env(safe-area-inset-bottom,0px))]">
			<Button type="button" variant="outline" class="min-h-10 sm:min-h-0" onclick={() => (open = false)}>
				Batal
			</Button>
			<Button type="submit" form="create-compose-form" class="min-h-10 sm:min-h-0" disabled={submitting}>
				{#if submitting}
					<Spinner data-icon="inline-start" />
					Menyimpan...
				{:else}
					<PlusIcon data-icon="inline-start" />
					{modeTab === 'template' ? 'Buat Stack Compose' : 'Daftarkan Stack'}
				{/if}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<FolderPickerDialog
	bind:open={folderPickerOpen}
	initialPath={dirPath || '/var/www'}
	title="Pilih Direktori Proyek Compose"
	description="Telusuri atau buat folder proyek Docker Compose."
	onSelect={handleFolderPicked}
/>
