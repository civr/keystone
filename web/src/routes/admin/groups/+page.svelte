<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import Sheet from '$lib/components/Sheet.svelte';

	interface Assignment {
		role: string;
		app?: string;
	}
	interface Group {
		id: string;
		name: string;
		description: string;
		members: string[];
		roles: Assignment[];
	}
	interface Person {
		id: string;
		username: string;
		display_name: string;
	}
	interface Role {
		id: string;
		name: string;
	}

	let me = $state<Me | null>(null);
	let groups = $state<Group[] | null>(null);
	let people = $state<Person[]>([]);
	let roles = $state<Role[]>([]);
	let loadError = $state('');

	const canEdit = $derived(!!me && (me.permissions.includes('*') || me.permissions.includes('groups.write')));
	const canRoles = $derived(!!me && (me.permissions.includes('*') || me.permissions.includes('roles.write')));

	async function load() {
		loadError = '';
		try {
			[me, groups] = await Promise.all([api.get<Me>('/me'), api.get<{ groups: Group[] }>('/admin/groups').then((r) => r.groups)]);
			people = (await api.get<{ users: Person[] }>('/admin/users')).users;
			roles = (await api.get<{ roles: Role[] }>('/admin/roles')).roles;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'The groups could not be loaded.';
		}
	}
	onMount(load);

	const nameOf = (id: string) => {
		const p = people.find((x) => x.id === id);
		return p ? p.display_name || p.username : id;
	};

	let open = $state(false);
	let editing = $state<Group | null>(null);
	let form = $state({ name: '', description: '', members: [] as string[], roles: [] as string[] });
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);
	let confirmDelete = $state(false);

	function edit(g: Group | null) {
		editing = g;
		form = {
			name: g?.name ?? '',
			description: g?.description ?? '',
			members: [...(g?.members ?? [])],
			roles: (g?.roles ?? []).filter((a) => !a.app).map((a) => a.role)
		};
		errors = {};
		confirmDelete = false;
		open = true;
	}

	function toggle(list: string[], id: string, on: boolean) {
		return on ? [...list, id] : list.filter((x) => x !== id);
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (saving) return;
		saving = true;
		errors = {};
		try {
			let id = editing?.id;
			const body = { name: form.name, description: form.description, members: form.members };
			if (editing) await api.send('PUT', `/admin/groups/${id}`, body);
			else id = (await api.send<{ id: string }>('POST', '/admin/groups', body)).id;
			if (canRoles) {
				const scoped = (editing?.roles ?? []).filter((a) => a.app);
				await api.send('PUT', `/admin/groups/${id}/roles`, { roles: [...form.roles.map((role) => ({ role })), ...scoped] });
			}
			toast(editing ? 'Group saved' : `${form.name} added`);
			open = false;
			await load();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'invalid' && typeof err.data.field === 'string') errors = { [err.data.field]: err.message };
			else toast(err instanceof ApiError ? err.message : 'The group could not be saved.', 'error');
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!editing) return;
		if (!confirmDelete) return void (confirmDelete = true);
		saving = true;
		try {
			await api.send('DELETE', `/admin/groups/${editing.id}`);
			toast(`${editing.name} deleted`);
			open = false;
			await load();
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'The group could not be deleted.', 'error');
		} finally {
			saving = false;
		}
	}
</script>

<div class="pane-head">
	<h1 class="pane-title">Groups</h1>
	{#if canEdit}<button type="button" class="btn btn-primary btn-sm" onclick={() => edit(null)}>Add group</button>{/if}
</div>
<p class="pane-intro">Put people together to give them apps and roles at once. A role given to a group is given to everyone in it.</p>

{#if loadError}
	<p class="error" role="alert">{loadError}</p>
{:else if !groups}
	<p class="loading-note" role="status">Loading…</p>
{:else if groups.length === 0}
	<div class="empty-note">
		<p>No groups yet. A group such as “Family” or “Admins” saves choosing people one by one.</p>
		{#if canEdit}<button type="button" class="btn btn-primary btn-sm" onclick={() => edit(null)}>Add group</button>{/if}
	</div>
{:else}
	<ul class="card list">
		{#each groups as g (g.id)}
			<li>
				<button type="button" class="srow row" onclick={() => edit(g)} disabled={!canEdit}>
					<span class="text">
						<span class="label">{g.name}</span>
						<span class="help">{g.members.length} {g.members.length === 1 ? 'member' : 'members'}{g.description ? ` · ${g.description}` : ''}</span>
					</span>
					{#each g.roles.filter((a) => !a.app) as a}
						<span class="chip accent">{roles.find((r) => r.id === a.role)?.name ?? a.role}</span>
					{/each}
				</button>
			</li>
		{/each}
	</ul>
{/if}

<Sheet bind:open title={editing ? editing.name : 'New group'}>
	<form class="form" onsubmit={save} novalidate>
		<label class="field">
			<span>Name</span>
			<input class="input" bind:value={form.name} placeholder="Family" aria-invalid={errors.name ? 'true' : undefined} />
			{#if errors.name}<small class="field-error" role="alert">{errors.name}</small>{/if}
		</label>
		<label class="field">
			<span>Description</span>
			<input class="input" bind:value={form.description} />
		</label>
		<fieldset class="field">
			<legend class="field-label">Members</legend>
			<div class="checks">
				{#each people as p (p.id)}
					<label class="check">
						<input type="checkbox" checked={form.members.includes(p.id)} onchange={(e) => (form.members = toggle(form.members, p.id, e.currentTarget.checked))} />
						<span>{nameOf(p.id)}</span>
					</label>
				{/each}
			</div>
			{#if errors.members}<small class="field-error" role="alert">{errors.members}</small>{/if}
		</fieldset>
		{#if canRoles}
			<fieldset class="field">
				<legend class="field-label">Roles for everyone in the group</legend>
				<div class="checks">
					{#each roles as r (r.id)}
						<label class="check">
							<input type="checkbox" checked={form.roles.includes(r.id)} onchange={(e) => (form.roles = toggle(form.roles, r.id, e.currentTarget.checked))} />
							<span>{r.name}</span>
						</label>
					{/each}
				</div>
			</fieldset>
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
	fieldset {
		margin: 0;
		padding: 0;
		border: 0;
	}
	.checks {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
		gap: 8px 16px;
		margin-top: 6px;
	}
	.form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
</style>
