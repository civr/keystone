<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import Sheet from '$lib/components/Sheet.svelte';

	interface Role {
		id: string;
		name: string;
		description: string;
		permissions: string[];
		builtin: boolean;
	}
	interface Permission {
		ID: string;
		Label: string;
	}

	let me = $state<Me | null>(null);
	let roles = $state<Role[] | null>(null);
	let perms = $state<Permission[]>([]);
	let loadError = $state('');
	const canEdit = $derived(!!me && (me.permissions.includes('*') || me.permissions.includes('roles.write')));
	const mine = (p: string) => !!me && (me.permissions.includes('*') || me.permissions.includes(p));

	async function load() {
		loadError = '';
		try {
			me = await api.get<Me>('/me');
			const r = await api.get<{ roles: Role[]; permissions: Permission[] }>('/admin/roles');
			roles = r.roles;
			perms = r.permissions;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'The roles could not be loaded.';
		}
	}
	onMount(load);

	const label = (p: string) => (p === '*' ? 'Everything' : (perms.find((x) => x.ID === p)?.Label ?? p));

	let open = $state(false);
	let editing = $state<Role | null>(null);
	let viewOnly = $state(false);
	let form = $state({ name: '', description: '', permissions: [] as string[] });
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);
	let confirmDelete = $state(false);

	function edit(r: Role | null) {
		editing = r;
		viewOnly = !!r?.builtin || !canEdit;
		form = { name: r?.name ?? '', description: r?.description ?? '', permissions: [...(r?.permissions ?? [])] };
		errors = {};
		confirmDelete = false;
		open = true;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (saving || viewOnly) return;
		saving = true;
		errors = {};
		try {
			if (editing) await api.send('PUT', `/admin/roles/${editing.id}`, form);
			else await api.send('POST', '/admin/roles', form);
			toast(editing ? 'Role saved' : `${form.name} added`);
			open = false;
			await load();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'invalid' && typeof err.data.field === 'string') errors = { [err.data.field]: err.message };
			else toast(err instanceof ApiError ? err.message : 'The role could not be saved.', 'error');
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!editing) return;
		if (!confirmDelete) return void (confirmDelete = true);
		saving = true;
		try {
			await api.send('DELETE', `/admin/roles/${editing.id}`);
			toast(`${editing.name} deleted`);
			open = false;
			await load();
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'The role could not be deleted.', 'error');
		} finally {
			saving = false;
		}
	}
</script>

<div class="pane-head">
	<h1 class="pane-title">Roles</h1>
	{#if canEdit}<button type="button" class="btn btn-primary btn-sm" onclick={() => edit(null)}>Add role</button>{/if}
</div>
<p class="pane-intro">
	A role is a set of permissions. Give roles to people under Users, or to a group. Nobody can hand out permissions they do not have
	themselves.
</p>

{#if loadError}
	<p class="error" role="alert">{loadError}</p>
{:else if !roles}
	<p class="loading-note" role="status">Loading…</p>
{:else}
	<ul class="card list">
		{#each roles as r (r.id)}
			<li>
				<button type="button" class="srow row" onclick={() => edit(r)}>
					<span class="text">
						<span class="label">{r.name}</span>
						<span class="help">{r.description || r.permissions.map(label).join(', ')}</span>
					</span>
					{#if r.builtin}<span class="chip">Built in</span>{/if}
				</button>
			</li>
		{/each}
	</ul>
{/if}

<Sheet bind:open title={editing ? editing.name : 'New role'}>
	<form class="form" onsubmit={save} novalidate>
		{#if viewOnly}
			<p class="sheet-msg">{editing?.builtin ? 'Built-in roles cannot be changed; make a new role to adjust them.' : 'You can see this role but not change it.'}</p>
		{:else}
			<label class="field">
				<span>Name</span>
				<input class="input" bind:value={form.name} aria-invalid={errors.name ? 'true' : undefined} />
				{#if errors.name}<small class="field-error" role="alert">{errors.name}</small>{/if}
			</label>
			<label class="field">
				<span>Description</span>
				<input class="input" bind:value={form.description} />
			</label>
		{/if}
		<fieldset class="field">
			<legend class="field-label">Permissions</legend>
			{#if form.permissions.includes('*')}
				<p class="help">Everything, including roles and super admins.</p>
			{:else}
				<div class="checks">
					{#each perms as p (p.ID)}
						<label class="check">
							<input
								type="checkbox"
								disabled={viewOnly || !mine(p.ID)}
								checked={form.permissions.includes(p.ID)}
								onchange={(e) =>
									(form.permissions = e.currentTarget.checked ? [...form.permissions, p.ID] : form.permissions.filter((x) => x !== p.ID))}
							/>
							<span>{p.Label}<small class="mono">{p.ID}</small></span>
						</label>
					{/each}
				</div>
			{/if}
			{#if errors.permissions}<small class="field-error" role="alert">{errors.permissions}</small>{/if}
		</fieldset>
		{#if !viewOnly}
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
		{/if}
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
	.row:hover {
		background: var(--c-hover);
	}
	.row .help {
		display: block;
		margin: 0;
	}
	fieldset {
		margin: 0;
		padding: 0;
		border: 0;
	}
	.checks {
		display: grid;
		gap: 10px;
		margin-top: 6px;
	}
	.form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.help {
		margin: 4px 0 0;
		color: var(--color-text-muted);
	}
</style>
