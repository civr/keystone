<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { initials } from '$lib/nav';
	import { strongPassword } from '$lib/vault';
	import { toast } from '$lib/toast.svelte';
	import Sheet from '$lib/components/Sheet.svelte';

	interface User {
		id: string;
		username: string;
		display_name: string;
		email: string;
		role: 'owner' | 'admin' | 'member';
		disabled: boolean;
		created_at: string;
	}
	type Form = { username: string; display_name: string; email: string; password: string; role: 'admin' | 'member'; disabled: boolean };

	let users = $state<User[] | null>(null);
	let loadError = $state('');

	async function load() {
		loadError = '';
		try {
			users = (await api.get<{ users: User[] }>('/admin/users')).users;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'The users could not be loaded.';
		}
	}
	onMount(load);

	let open = $state(false);
	let editing = $state<User | null>(null);
	let form = $state<Form>({ username: '', display_name: '', email: '', password: '', role: 'member', disabled: false });
	let errors = $state<Partial<Record<keyof Form, string>>>({});
	let saving = $state(false);
	let confirmDelete = $state(false);

	function startAdd() {
		editing = null;
		form = { username: '', display_name: '', email: '', password: strongPassword(), role: 'member', disabled: false };
		errors = {};
		open = true;
	}

	function startEdit(u: User) {
		if (u.role === 'owner') return;
		editing = u;
		form = { username: u.username, display_name: u.display_name, email: u.email, password: '', role: u.role, disabled: u.disabled };
		errors = {};
		confirmDelete = false;
		open = true;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (saving) return;
		errors = {};
		saving = true;
		try {
			if (editing) {
				const body: Record<string, unknown> = { display_name: form.display_name, email: form.email, role: form.role, disabled: form.disabled };
				if (form.password) body.password = form.password;
				await api.send('PUT', `/admin/users/${editing.id}`, body);
				toast('Saved');
			} else {
				const u = await api.send<User>('POST', '/admin/users', form);
				toast(`${u.display_name || u.username} added`);
			}
			open = false;
			await load();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'invalid' && typeof err.data.field === 'string')
				errors = { [err.data.field]: err.message };
			else toast(err instanceof ApiError ? err.message : 'The account could not be saved.', 'error');
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!editing) return;
		if (!confirmDelete) return void (confirmDelete = true);
		saving = true;
		try {
			await api.send('DELETE', `/admin/users/${editing.id}`);
			toast(`${editing.display_name || editing.username} deleted`);
			open = false;
			await load();
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'The account could not be deleted.', 'error');
		} finally {
			saving = false;
		}
	}

	const ROLE: Record<User['role'], string> = { owner: 'Owner', admin: 'Admin', member: 'Member' };
</script>

<div class="pane-head">
	<h1 class="pane-title">Users</h1>
	<button type="button" class="btn btn-primary btn-sm" onclick={startAdd}>Add</button>
</div>
<p class="pane-intro">People who sign in to Keystone. Admins manage settings and apps; members use the apps they are given.</p>

{#if loadError}
	<p class="error" role="alert">{loadError}</p>
{:else if !users}
	<p class="loading-note" role="status">Loading…</p>
{:else}
	<ul class="card list">
		{#each users as u (u.id)}
			<li>
				<button type="button" class="srow row" onclick={() => startEdit(u)} disabled={u.role === 'owner'}>
					<span class="avatar" aria-hidden="true">{initials(u.display_name || u.username)}</span>
					<span class="text">
						<span class="label">{u.display_name || u.username}</span>
						<span class="help">{u.username}{u.email ? ` · ${u.email}` : ''}</span>
					</span>
					{#if u.disabled}<span class="chip warn">Disabled</span>{/if}
					<span class="chip" class:accent={u.role !== 'member'}>{ROLE[u.role]}</span>
					{#if u.role !== 'owner'}
						<svg class="chev" viewBox="0 0 20 20" aria-hidden="true"><path d="M8 5l5 5-5 5" /></svg>
					{/if}
				</button>
			</li>
		{/each}
	</ul>
	<p class="foot-note">The owner signs in with the account made during setup and is changed under General.</p>
{/if}

<Sheet bind:open title={editing ? editing.display_name || editing.username : 'New user'}>
	<form class="form" onsubmit={save} novalidate>
		{#if !editing}
			<label class="field">
				<span>Username</span>
				<input class="input" bind:value={form.username} autocapitalize="none" spellcheck="false" autocomplete="off" aria-invalid={errors.username ? 'true' : undefined} />
				{#if errors.username}<small class="field-error" role="alert">{errors.username}</small>{/if}
			</label>
		{/if}
		<label class="field">
			<span>Name</span>
			<input class="input" bind:value={form.display_name} placeholder="Anna de Vries" aria-invalid={errors.display_name ? 'true' : undefined} />
			{#if errors.display_name}<small class="field-error" role="alert">{errors.display_name}</small>{/if}
		</label>
		<label class="field">
			<span>E-mail</span>
			<input class="input" type="email" bind:value={form.email} aria-invalid={errors.email ? 'true' : undefined} />
			{#if errors.email}<small class="field-error" role="alert">{errors.email}</small>{/if}
		</label>
		<div class="field">
			<label class="field-label" for="user-password">{editing ? 'New password' : 'Password'}</label>
			<div class="pw-row">
				<input
					id="user-password"
					class="input mono"
					bind:value={form.password}
					autocomplete="new-password"
					spellcheck="false"
					placeholder={editing ? 'Leave empty to keep it' : ''}
					aria-invalid={errors.password ? 'true' : undefined}
				/>
				<button type="button" class="btn btn-secondary btn-sm" onclick={() => (form.password = strongPassword())}>Generate</button>
			</div>
			{#if errors.password}<small class="field-error" role="alert">{errors.password}</small>{:else}<small>At least 12 characters. Share it with them; they can change it under Account.</small>{/if}
		</div>
		<fieldset class="field">
			<legend class="field-label">Role</legend>
			<div class="segmented">
				<label><input type="radio" bind:group={form.role} value="member" /><span>Member</span></label>
				<label><input type="radio" bind:group={form.role} value="admin" /><span>Admin</span></label>
			</div>
		</fieldset>
		{#if editing}
			<label class="check">
				<input type="checkbox" bind:checked={form.disabled} />
				<span>Disabled<small>Cannot sign in; signed out everywhere.</small></span>
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
	.list {
		margin: 0;
	}
	.row {
		width: 100%;
		border: 0;
		background: none;
		font: inherit;
		color: inherit;
		text-align: left;
		cursor: pointer;
	}
	.row:hover:not(:disabled) {
		background: var(--c-hover);
	}
	.row:disabled {
		cursor: default;
	}
	.row .help {
		display: block;
		margin: 0;
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
	.foot-note {
		margin: 8px 2px 0;
		color: var(--color-text-muted);
		font-size: 0.8125rem;
	}
	.pw-row {
		display: flex;
		gap: 8px;
		align-items: center;
	}
	fieldset {
		margin: 0;
		padding: 0;
		border: 0;
	}
</style>
