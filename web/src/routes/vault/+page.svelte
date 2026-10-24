<script lang="ts">
	import '$lib/forms.css';
	import '$lib/settings.css';
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { pageTitle } from '$lib/brand.svelte';
	import { copyText } from '$lib/copy';
	import { safeHref } from '$lib/apps';
	import { filterItems, strongPassword, type VaultItem } from '$lib/vault';
	import { toast } from '$lib/toast.svelte';
	import AppShell from '$lib/components/AppShell.svelte';
	import Sheet from '$lib/components/Sheet.svelte';

	let me = $state<Me | null>(null);
	let items = $state<VaultItem[] | null>(null);
	let loadError = $state('');
	let query = $state('');

	const shown = $derived(items ? filterItems(items, query) : []);

	async function load() {
		loadError = '';
		try {
			items = (await api.get<{ items: VaultItem[] }>('/vault')).items;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'The vault could not be loaded.';
		}
	}
	onMount(() => {
		api.get<Me>('/me').then((m) => (me = m), () => {});
		load();
	});

	// ── Detail ──
	let detailOpen = $state(false);
	let current = $state<VaultItem | null>(null);
	let secret = $state<{ password: string; note: string } | null>(null);
	let revealing = $state(false);

	// The password is loaded when the item opens, so Copy works inside its own
	// click (on plain http too). It is never shown.
	function openItem(it: VaultItem) {
		current = it;
		secret = null;
		detailOpen = true;
		if (it.has_password || it.has_note) reveal();
	}

	async function reveal(): Promise<{ password: string; note: string } | null> {
		if (secret) return secret;
		if (!current || revealing) return null;
		revealing = true;
		try {
			secret = await api.send<{ password: string; note: string }>('POST', `/vault/${current.id}/reveal`);
			return secret;
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'The password could not be loaded.', 'error');
			return null;
		} finally {
			revealing = false;
		}
	}

	async function copy(text: string | null | undefined, what: string) {
		if (!text) return;
		try {
			await copyText(text);
			toast(`${what} copied`);
		} catch {
			toast(`${what} could not be copied.`, 'error');
		}
	}

	function copyPassword() {
		if (secret) copy(secret.password, 'Password');
	}

	// ── Edit ──
	type Form = { title: string; url: string; username: string; password: string; note: string };
	let editOpen = $state(false);
	let editing = $state<VaultItem | null>(null);
	let form = $state<Form>({ title: '', url: '', username: '', password: '', note: '' });
	let errors = $state<Partial<Record<keyof Form, string>>>({});
	let saving = $state(false);
	let confirmDelete = $state(false);

	function startAdd() {
		editing = null;
		form = { title: '', url: '', username: '', password: strongPassword(), note: '' };
		errors = {};
		confirmDelete = false;
		editOpen = true;
	}

	function startEdit() {
		if (!current) return;
		editing = current;
		// The password is not filled in: empty keeps the stored one.
		form = { title: current.title, url: current.url, username: current.username, password: '', note: secret?.note ?? '' };
		errors = {};
		confirmDelete = false;
		detailOpen = false;
		editOpen = true;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (saving) return;
		errors = {};
		if (!form.title.trim()) return void (errors.title = 'Give the item a name.');
		saving = true;
		try {
			const body: Record<string, string> = { title: form.title, url: form.url, username: form.username, note: form.note };
			if (!editing || form.password) body.password = form.password;
			const saved = editing
				? await api.send<VaultItem>('PUT', `/vault/${editing.id}`, body)
				: await api.send<VaultItem>('POST', '/vault', body);
			toast(editing ? 'Saved' : `${saved.title} added`);
			editOpen = false;
			await load();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'invalid' && typeof err.data.field === 'string')
				errors = { [err.data.field]: err.message };
			else toast(err instanceof ApiError ? err.message : 'The item could not be saved.', 'error');
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!editing) return;
		if (!confirmDelete) return void (confirmDelete = true);
		saving = true;
		try {
			await api.send('DELETE', `/vault/${editing.id}`);
			toast(`${editing.title} deleted`);
			editOpen = false;
			await load();
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'The item could not be deleted.', 'error');
		} finally {
			saving = false;
		}
	}

	function initial(t: string) {
		return Array.from(t.trim())[0]?.toUpperCase() ?? '?';
	}
	function host(u: string) {
		try {
			return new URL(u).host;
		} catch {
			return '';
		}
	}
</script>

<svelte:head>
	<title>{pageTitle('Vault')}</title>
</svelte:head>

<AppShell {me} bind:query onsearch={() => shown[0] && openItem(shown[0])}>
	<div class="pane">
		<div class="pane-head">
			<h1 class="pane-title">Vault</h1>
			<button type="button" class="btn btn-primary btn-sm" onclick={startAdd}>Add</button>
		</div>
		<p class="pane-intro">Passwords for anything, kept encrypted. Search with the bar at the top.</p>

		{#if loadError}
			<div class="empty-note">
				<p>{loadError}</p>
				<button type="button" class="btn btn-secondary btn-sm" onclick={load}>Try again</button>
			</div>
		{:else if items === null}
			<p class="loading-note" role="status">Loading…</p>
		{:else if items.length === 0}
			<div class="empty-note">
				<p>Nothing in the vault yet.</p>
				<button type="button" class="btn btn-primary btn-sm" onclick={startAdd}>Add a password</button>
			</div>
		{:else if shown.length === 0}
			<div class="empty-note"><p>Nothing matches “{query.trim()}”.</p></div>
		{:else}
			<ul class="card list">
				{#each shown as it (it.id)}
					<li>
						<button type="button" class="srow item" onclick={() => openItem(it)}>
							<span class="mark" aria-hidden="true">{initial(it.title)}</span>
							<span class="text">
								<span class="label">{it.title}</span>
								<span class="help">{it.username || host(it.url) || '—'}</span>
							</span>
							<svg class="chev" viewBox="0 0 20 20" aria-hidden="true"><path d="M8 5l5 5-5 5" /></svg>
						</button>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</AppShell>

<Sheet bind:open={detailOpen} title={current?.title ?? ''}>
	{#if current}
		{@const it = current}
		<div class="card">
			<div class="srow">
				<div class="text"><span class="help">Username</span><span class="label value">{it.username || '—'}</span></div>
				{#if it.username}<button type="button" class="btn btn-secondary btn-sm" onclick={() => copy(it.username, 'Username')}>Copy</button>{/if}
			</div>
			<div class="srow">
				<div class="text">
					<span class="help">Password</span>
					<span class="label value">{it.has_password ? '••••••••••••' : '—'}</span>
				</div>
				{#if it.has_password}
					<button type="button" class="btn btn-secondary btn-sm" onclick={copyPassword} disabled={!secret}>
						{#if revealing}<span class="spinner" aria-hidden="true"></span>{/if}Copy
					</button>
				{/if}
			</div>
			{#if it.url}
				<div class="srow">
					<div class="text"><span class="help">Website</span><span class="label value">{it.url}</span></div>
					{#if safeHref(it.url)}
						<a class="btn btn-secondary btn-sm" href={safeHref(it.url)} target="_blank" rel="noopener noreferrer">Open</a>
					{/if}
				</div>
			{/if}
			{#if it.has_note}
				<div class="srow stack">
					<div class="text"><span class="help">Note</span></div>
					{#if secret}<p class="note">{secret.note}</p>{:else}<p class="note help">…</p>{/if}
				</div>
			{/if}
		</div>
		<div class="sheet-actions">
			<button type="button" class="btn btn-secondary" onclick={startEdit}>Edit</button>
		</div>
	{/if}
</Sheet>

<Sheet bind:open={editOpen} title={editing ? 'Edit' : 'New password'}>
	<form class="form" onsubmit={save} novalidate>
		<label class="field">
			<span>Name</span>
			<input class="input" bind:value={form.title} placeholder="Router" aria-invalid={errors.title ? 'true' : undefined} />
			{#if errors.title}<small class="field-error" role="alert">{errors.title}</small>{/if}
		</label>
		<label class="field">
			<span>Website</span>
			<input class="input" type="url" bind:value={form.url} placeholder="https://" aria-invalid={errors.url ? 'true' : undefined} />
			{#if errors.url}<small class="field-error" role="alert">{errors.url}</small>{/if}
		</label>
		<label class="field">
			<span>Username</span>
			<input class="input" bind:value={form.username} autocomplete="off" autocapitalize="none" spellcheck="false" />
			{#if errors.username}<small class="field-error" role="alert">{errors.username}</small>{/if}
		</label>
		<div class="field">
			<label class="field-label" for="vault-password">Password</label>
			<div class="pw-row">
				<input
					id="vault-password"
					class="input"
					type="password"
					bind:value={form.password}
					autocomplete="new-password"
					placeholder={editing ? 'Leave empty to keep it' : ''}
				/>
				<button type="button" class="btn btn-secondary btn-sm" onclick={() => (form.password = strongPassword())}>Generate</button>
			</div>
			{#if form.password && !errors.password}<small>{form.password === '' ? '' : 'Saved encrypted. It is never shown, only copied.'}</small>{/if}
			{#if errors.password}<small class="field-error" role="alert">{errors.password}</small>{/if}
		</div>
		<label class="field">
			<span>Note</span>
			<textarea class="input note-input" bind:value={form.note} rows="3"></textarea>
			{#if errors.note}<small class="field-error" role="alert">{errors.note}</small>{/if}
		</label>
		<div class="sheet-actions">
			{#if editing}
				<button type="button" class="btn {confirmDelete ? 'btn-danger' : 'btn-secondary danger-ink'}" onclick={remove} disabled={saving}>
					{confirmDelete ? 'Delete for good?' : 'Delete'}
				</button>
			{/if}
			<button class="btn btn-primary" disabled={saving}>
				{#if saving}<span class="spinner" aria-hidden="true"></span>{/if}
				{editing ? 'Save' : 'Add'}
			</button>
		</div>
	</form>
</Sheet>

<style>
	.pane {
		max-width: 760px;
		margin: 0 auto;
	}
	.list {
		margin: 0;
	}
	.item {
		width: 100%;
		border: 0;
		background: none;
		font: inherit;
		color: inherit;
		text-align: left;
		cursor: pointer;
	}
	.item:hover {
		background: var(--c-hover);
	}
	.item .help {
		display: block;
		margin: 0;
	}
	.mark {
		display: grid;
		place-items: center;
		flex: none;
		width: 32px;
		height: 32px;
		border-radius: var(--radius-sm);
		background: var(--c-selected-mid);
		color: var(--primary-dark);
		font-weight: 600;
	}
	.chev {
		flex: none;
		width: 16px;
		height: 16px;
		fill: none;
		stroke: var(--c-400);
		stroke-width: 2;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.value {
		overflow-wrap: anywhere;
	}
	.note {
		margin: 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.pw-row {
		display: flex;
		gap: 8px;
		align-items: center;
	}
	.note-input {
		font-family: var(--font);
		font-size: 0.9375rem;
	}
</style>
