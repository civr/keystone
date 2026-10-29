<script lang="ts">
	import '$lib/forms.css';
	import '$lib/settings.css';
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { pageTitle } from '$lib/brand.svelte';
	import { copyText } from '$lib/copy';
	import { safeHref } from '$lib/apps';
	import { filterItems, kindOf, KINDS, strongPassword, type VaultItem, type VaultKind } from '$lib/vault';
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
		refreshStatus();
		api.get<{ providers: { id: string; name: string }[] }>('/vault/providers').then((r) => (providers = r.providers), () => {});
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
		detailOpen = matchMedia('(max-width: 767px)').matches;
		if (it.has_password || it.has_note) reveal();
	}

	async function reveal(): Promise<{ password: string; note: string } | null> {
		if (secret) return secret;
		if (!current || revealing) return null;
		revealing = true;
		try {
			secret = await api.send<{ password: string; note: string }>('POST', `/vault/${current.id}/reveal`);
			refreshStatus();
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
	type Form = { title: string; url: string; username: string; password: string; note: string; kind: VaultKind };
	let editOpen = $state(false);
	let editing = $state<VaultItem | null>(null);
	let form = $state<Form>({ title: '', url: '', username: '', password: '', note: '', kind: 'login' });
	const fk = $derived(kindOf(form.kind));

	// Linking from a credential provider (1Password Connect) instead of storing.
	let providers = $state<{ id: string; name: string }[]>([]);
	let source = $state<'keystone' | string>('keystone');
	let pItems = $state<{ Ref: string; Title: string; Vault: string }[] | null>(null);
	let pQuery = $state('');
	let pRef = $state('');
	async function loadProviderItems(id: string) {
		pItems = null;
		pRef = '';
		try {
			pItems = (await api.get<{ items: { Ref: string; Title: string; Vault: string }[] }>(`/vault/providers/${id}/items`)).items;
		} catch (e) {
			toast(e instanceof ApiError ? e.message : '1Password could not be reached.', 'error');
			source = 'keystone';
		}
	}

	// Lock state, shown in the list header.
	let status = $state<{ open: boolean; seconds_left: number } | null>(null);
	async function refreshStatus() {
		status = await api.get<{ open: boolean; seconds_left: number }>('/vault/status').catch(() => null);
	}
	async function lockNow() {
		await api.send('POST', '/vault/lock').catch(() => {});
		secret = null;
		await refreshStatus();
		toast('Vault locked');
	}
	let errors = $state<Partial<Record<keyof Form, string>>>({});
	let saving = $state(false);
	let confirmDelete = $state(false);

	function startAdd() {
		editing = null;
		form = { title: '', url: '', username: '', password: strongPassword(), note: '', kind: 'login' };
		source = 'keystone';
		pItems = null;
		pRef = '';
		errors = {};
		confirmDelete = false;
		editOpen = true;
	}

	function startEdit() {
		if (!current) return;
		editing = current;
		// The password is not filled in: empty keeps the stored one.
		form = { title: current.title, url: current.url, username: current.username, password: '', note: secret?.note ?? '', kind: current.kind };
		source = current.provider ? current.provider : 'keystone';
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
			const body: Record<string, string> = { title: form.title, url: form.url, username: form.username, kind: form.kind };
			if (source !== 'keystone' && !editing) {
				if (!pRef) throw new ApiError('invalid', 'Choose an item from 1Password.', 400, { field: 'title' });
				body.provider = source;
				body.ref = pRef;
			} else if (source === 'keystone') {
				body.note = form.note;
				if (!editing || form.password) body.password = form.password;
			}
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

	/** A stable colour per item, as app icons have. */
	function hue(t: string) {
		let h = 0;
		for (const c of t) h = (h * 31 + c.charCodeAt(0)) % 360;
		return h;
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

<AppShell {me} full bind:query onsearch={() => shown[0] && openItem(shown[0])}>
	<div class="vault">
		<section class="list-pane" aria-label="Passwords">
			<div class="list-head">
				<span class="count">{items ? `${shown.length} ${shown.length === 1 ? 'item' : 'items'}` : ''}</span>
				{#if status?.open}
					<button type="button" class="btn btn-flat btn-sm lock" onclick={lockNow} title="Unlocked for {Math.ceil(status.seconds_left / 60)} more minutes">
						<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M6 9V7a4 4 0 0 1 8 0v2M5 9h10v7H5z" /></svg>Lock
					</button>
				{:else if status}
					<span class="locked"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M6 9V7a4 4 0 0 1 8 0v2M5 9h10v7H5z" /></svg>Locked</span>
				{/if}
				<button type="button" class="btn btn-secondary btn-sm add" onclick={startAdd} aria-label="Add a password">
					<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 4.5v11M4.5 10h11" /></svg>
				</button>
			</div>
			{#if loadError}
				<div class="pane-note">
					<p>{loadError}</p>
					<button type="button" class="btn btn-secondary btn-sm" onclick={load}>Try again</button>
				</div>
			{:else if items === null}
				<p class="pane-note" role="status">Loading…</p>
			{:else if items.length === 0}
				<div class="pane-note">
					<p>Nothing in the vault yet.</p>
					<button type="button" class="btn btn-primary btn-sm" onclick={startAdd}>Add a password</button>
				</div>
			{:else if shown.length === 0}
				<p class="pane-note">Nothing matches “{query.trim()}”.</p>
			{:else}
				<ul class="rows">
					{#each shown as it (it.id)}
						<li>
							<button type="button" class="row" class:selected={current?.id === it.id} onclick={() => openItem(it)}>
								<span class="mark" style:--hue={hue(it.title)} aria-hidden="true">{initial(it.title)}</span>
								<span class="text">
									<span class="title">{it.title}</span>
									<span class="sub">{it.username || host(it.url) || '—'}</span>
								</span>
							</button>
						</li>
					{/each}
				</ul>
			{/if}
		</section>

		<section class="detail-pane" aria-label="Details">
			{#if current}
				{@render detail(current)}
			{:else}
				<p class="pane-note empty">Choose an item to see its details.</p>
			{/if}
		</section>
	</div>
</AppShell>

{#snippet detail(it: VaultItem)}
	<div class="detail">
		<header class="detail-head">
			<span class="mark big" style:--hue={hue(it.title)} aria-hidden="true">{initial(it.title)}</span>
			<h1>{it.title}</h1>
			<button type="button" class="btn btn-secondary btn-sm edit" onclick={startEdit}>Edit</button>
		</header>
		<div class="fields">
			<div class="fld">
				<span class="k">{(kindOf(it.kind).user ?? 'username').toLowerCase()}</span>
				<div class="v-row">
					<span class="v">{it.username || '—'}</span>
					{#if it.username}<button type="button" class="btn btn-flat btn-sm" onclick={() => copy(it.username, 'Username')}>Copy</button>{/if}
				</div>
			</div>
			<div class="fld">
				<span class="k">{(kindOf(it.kind).secret || 'secret').toLowerCase()}{#if it.provider} · from 1Password{/if}</span>
				<div class="v-row">
					<span class="v dots">{it.has_password ? '••••••••••••' : '—'}</span>
					{#if it.has_password}
						<button type="button" class="btn btn-flat btn-sm" onclick={copyPassword} disabled={!secret}>
							{#if revealing}<span class="spinner" aria-hidden="true"></span>{/if}Copy
						</button>
					{/if}
				</div>
			</div>
		</div>
		{#if it.url}
			<div class="fld loose">
				<span class="k">website</span>
				{#if safeHref(it.url)}
					<a class="v link-v" href={safeHref(it.url)} target="_blank" rel="noopener noreferrer">{it.url}</a>
				{:else}<span class="v">{it.url}</span>{/if}
			</div>
		{/if}
		{#if it.has_note}
			<div class="fld loose">
				<span class="k">note</span>
				<p class="v note">{secret ? secret.note : '…'}</p>
			</div>
		{/if}
		<div class="fld loose">
			<span class="k">last modified</span>
			<span class="v muted">{new Date(it.updated_at).toLocaleString('en-GB', { dateStyle: 'medium', timeStyle: 'short' })}</span>
		</div>
	</div>
{/snippet}

<!-- On a phone the details open as a sheet over the list. -->
<Sheet bind:open={detailOpen} title={current?.title ?? ''}>
	{#if current}{@render detail(current)}{/if}
</Sheet>

<Sheet bind:open={editOpen} title={editing ? 'Edit' : 'New item'}>
	<form class="form" onsubmit={save} novalidate>
		{#if !editing && providers.length}
			<fieldset class="field">
				<legend class="field-label">Where the secret lives</legend>
				<div class="segmented">
					<label><input type="radio" bind:group={source} value="keystone" /><span>In Keystone</span></label>
					{#each providers as p (p.id)}
						<label><input type="radio" bind:group={source} value={p.id} onchange={() => loadProviderItems(p.id)} /><span>{p.name}</span></label>
					{/each}
				</div>
			</fieldset>
			{#if source !== 'keystone'}
				<div class="field">
					<span class="field-label">Item</span>
					{#if pItems === null}
						<p class="help">Loading items…</p>
					{:else}
						<input class="input" type="search" placeholder="Search 1Password" bind:value={pQuery} />
						<ul class="pick">
							{#each pItems.filter((i) => !pQuery.trim() || i.Title.toLowerCase().includes(pQuery.trim().toLowerCase())).slice(0, 50) as i (i.Ref)}
								<li>
									<button type="button" class:on={pRef === i.Ref} onclick={() => ((pRef = i.Ref), (form.title = form.title || i.Title))}>
										{i.Title}<small>{i.Vault}</small>
									</button>
								</li>
							{/each}
						</ul>
					{/if}
					<small>Keystone keeps only a link; the secret is read from 1Password each time it is used.</small>
				</div>
			{/if}
		{/if}
		<fieldset class="field">
			<legend class="field-label">Kind</legend>
			<select class="input" bind:value={form.kind}>
				{#each KINDS as k (k.id)}<option value={k.id}>{k.label}</option>{/each}
			</select>
		</fieldset>
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
		{#if fk.user && source === 'keystone'}
		<label class="field">
			<span>{fk.user}</span>
			<input class="input" bind:value={form.username} autocomplete="off" autocapitalize="none" spellcheck="false" />
			{#if errors.username}<small class="field-error" role="alert">{errors.username}</small>{/if}
		</label>
		{/if}
		{#if fk.secret && source === 'keystone'}
		<div class="field">
			<label class="field-label" for="vault-password">{fk.secret}</label>
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
		{/if}
		{#if source === 'keystone'}
		<label class="field">
			<span>Note</span>
			<textarea class="input note-input" bind:value={form.note} rows="3"></textarea>
			{#if errors.note}<small class="field-error" role="alert">{errors.note}</small>{/if}
		</label>
		{/if}
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
	/* 1Password-style: a white list beside the details, filling the window. */
	.vault {
		display: grid;
		grid-template-columns: minmax(280px, 360px) minmax(0, 1fr);
		height: 100%;
		background: var(--c-bg-card);
	}
	.list-pane {
		display: flex;
		flex-direction: column;
		min-height: 0;
		border-right: 1px solid var(--c-border);
	}
	.list-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 10px 12px 10px 18px;
		border-bottom: 1px solid var(--c-border);
		color: var(--color-text-muted);
		font-size: 0.9375rem;
	}
	.add {
		width: 36px;
		padding: 0;
	}
	.rows {
		flex: 1;
		overflow-y: auto;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 14px;
		width: 100%;
		padding: 10px 18px;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.row:hover {
		background: var(--c-hover);
	}
	.row.selected,
	.row.selected:hover {
		background: var(--primary);
		color: #fff;
	}
	.row .text {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.title {
		font-size: 1.0625rem;
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.sub {
		color: var(--color-text-muted);
		font-size: 0.9375rem;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.row.selected .sub {
		color: rgba(255, 255, 255, 0.85);
	}
	.mark {
		display: grid;
		place-items: center;
		flex: none;
		width: 40px;
		height: 40px;
		border-radius: 9px;
		background: hsl(var(--hue) 62% 50%);
		color: #fff;
		font-size: 1.125rem;
		font-weight: 600;
	}
	.mark.big {
		width: 72px;
		height: 72px;
		border-radius: 16px;
		font-size: 2rem;
	}
	.detail-pane {
		min-height: 0;
		overflow-y: auto;
	}
	.pane-note {
		margin: 0;
		padding: 28px 20px;
		color: var(--color-text-muted);
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 10px;
	}
	.pane-note p {
		margin: 0;
	}
	.pane-note.empty {
		height: 100%;
		align-items: center;
		justify-content: center;
	}
	.detail {
		max-width: 680px;
		padding: 32px 40px;
	}
	.detail-head {
		display: flex;
		align-items: center;
		gap: 18px;
		margin-bottom: 26px;
	}
	.detail-head h1 {
		flex: 1;
		min-width: 0;
		margin: 0;
		font-size: 1.875rem;
	}
	.fields {
		display: grid;
		gap: 16px;
		padding: 20px 22px;
		border: 1px solid var(--c-border);
		border-radius: 12px;
		margin-bottom: 20px;
	}
	.fld {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.fld.loose {
		padding: 0 22px;
		margin-bottom: 16px;
	}
	.k {
		color: var(--color-text-muted);
		font-size: 0.9375rem;
	}
	.v-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.v {
		font-size: 1.0625rem;
		overflow-wrap: anywhere;
	}
	.v.dots {
		letter-spacing: 0.12em;
	}
	.v.muted {
		color: var(--color-text-muted);
	}
	.v.note {
		margin: 0;
		white-space: pre-wrap;
	}
	.link-v {
		color: var(--primary);
	}
	.lock,
	.locked {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		margin-left: auto;
		margin-right: 8px;
		font-size: 0.875rem;
	}
	.locked {
		color: var(--color-text-muted);
	}
	.lock svg,
	.locked svg {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
	}
	.pick {
		max-height: 220px;
		overflow-y: auto;
		margin: 6px 0 0;
		padding: 0;
		list-style: none;
		border: 1px solid var(--c-border);
		border-radius: var(--radius-control);
	}
	.pick button {
		display: flex;
		justify-content: space-between;
		width: 100%;
		padding: 8px 12px;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.pick button:hover {
		background: var(--c-hover);
	}
	.pick button.on {
		background: var(--c-selected-mid);
	}
	.pick small {
		color: var(--color-text-muted);
	}
	fieldset.field {
		margin: 0;
		padding: 0;
		border: 0;
	}
	.pw-row {
		display: flex;
		gap: 8px;
		align-items: center;
	}
	.note-input {
		font-family: var(--font);
		font-size: 1rem;
	}
	@media (max-width: 767px) {
		.vault {
			grid-template-columns: minmax(0, 1fr);
		}
		.detail-pane {
			display: none;
		}
		.detail {
			padding: 0;
		}
		.fld.loose {
			padding: 0 4px;
		}
	}
</style>
