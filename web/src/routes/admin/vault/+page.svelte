<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { securityGroups } from '$lib/settingsMeta';
	import { toast } from '$lib/toast.svelte';
	import SettingsEditor from '$lib/components/settings/SettingsEditor.svelte';
	import Sheet from '$lib/components/Sheet.svelte';

	interface Provider {
		id: string;
		name: string;
		url: string;
		access: string[] | null;
	}

	let providers = $state<Provider[] | null>(null);
	let canProviders = $state(false);
	let people = $state<{ id: string; label: string }[]>([]);
	let open = $state(false);
	let form = $state({ name: '1Password', url: 'http://op-connect:8080', token: '', access: [] as string[] });
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);

	async function load() {
		providers = (await api.get<{ providers: Provider[] }>('/admin/vault/providers').catch(() => ({ providers: [] }))).providers;
	}
	onMount(async () => {
		const me = await api.get<{ permissions: string[] }>('/me').catch(() => ({ permissions: [] as string[] }));
		canProviders = me.permissions.includes('*') || me.permissions.includes('vault.write');
		if (!canProviders) return;
		load();
		const [u, g] = await Promise.all([
			api.get<{ users: { id: string; username: string; display_name: string }[] }>('/admin/users').catch(() => ({ users: [] })),
			api.get<{ groups: { id: string; name: string }[] }>('/admin/groups').catch(() => ({ groups: [] }))
		]);
		people = [
			...g.groups.map((x) => ({ id: `group:${x.id}`, label: `${x.name} (group)` })),
			...u.users.map((x) => ({ id: x.id, label: x.display_name || x.username }))
		];
	});

	async function add(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		errors = {};
		try {
			await api.send('POST', '/admin/vault/providers', { ...form, access: form.access.length ? form.access : null });
			toast(`${form.name} connected`);
			open = false;
			form.token = '';
			await load();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'invalid' && typeof err.data.field === 'string') errors = { [err.data.field]: err.message };
			else toast(err instanceof ApiError ? err.message : 'The provider could not be added.', 'error');
		} finally {
			saving = false;
		}
	}

	async function remove(p: Provider) {
		try {
			await api.send('DELETE', `/admin/vault/providers/${p.id}`);
			toast(`${p.name} removed`);
			await load();
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'The provider could not be removed.', 'error');
		}
	}
</script>

<div class="pane-head"><h1 class="pane-title">Vault &amp; secrets</h1></div>
<p class="pane-intro">How vault passwords may be used, and where secrets may come from.</p>

{#if canProviders}
<section class="group" aria-labelledby="g-prov">
	<div class="group-head">
		<h2 id="g-prov">Credential providers</h2>
		<button type="button" class="btn btn-secondary btn-sm" onclick={() => (open = true)}>Connect 1Password</button>
	</div>
	{#if providers && providers.length}
		<ul class="card list">
			{#each providers as p (p.id)}
				<li class="srow">
					<div class="text">
						<span class="label">{p.name}</span>
						<p class="help">{p.url} · {p.access ? `${p.access.length} people or groups may link items` : 'Admins only'}</p>
					</div>
					<div class="control"><button type="button" class="btn btn-secondary btn-sm danger-ink" onclick={() => remove(p)}>Remove</button></div>
				</li>
			{/each}
		</ul>
	{:else}
		<p class="empty-note">
			None yet. With 1Password Connect, people link items into their vault; Keystone keeps only the link and reads the secret when it is used.
		</p>
	{/if}
	<p class="foot">The Connect token is sealed with the vault key. Its reach is what you gave it in 1Password: give it only the vaults Keystone needs.</p>
</section>
{/if}

<SettingsEditor groups={securityGroups} visible={['vault']} />

<Sheet bind:open title="Connect 1Password">
	<form class="form" onsubmit={add} novalidate>
		<p class="sheet-msg">Run a 1Password Connect server next to Keystone and create a token for it in 1Password (Developer › Connect).</p>
		<label class="field">
			<span>Name</span>
			<input class="input" bind:value={form.name} aria-invalid={errors.name ? 'true' : undefined} />
			{#if errors.name}<small class="field-error" role="alert">{errors.name}</small>{/if}
		</label>
		<label class="field">
			<span>Connect address</span>
			<input class="input mono" type="url" bind:value={form.url} aria-invalid={errors.url ? 'true' : undefined} />
			{#if errors.url}<small class="field-error" role="alert">{errors.url}</small>{/if}
		</label>
		<label class="field">
			<span>Access token</span>
			<input class="input mono" type="password" autocomplete="off" bind:value={form.token} aria-invalid={errors.token ? 'true' : undefined} />
			{#if errors.token}<small class="field-error" role="alert">{errors.token}</small>{:else}<small>Checked with Connect before it is kept.</small>{/if}
		</label>
		<fieldset class="field">
			<legend class="field-label">Who may link items</legend>
			<div class="checks">
				{#each people as p (p.id)}
					<label class="check">
						<input type="checkbox" checked={form.access.includes(p.id)} onchange={(e) => (form.access = e.currentTarget.checked ? [...form.access, p.id] : form.access.filter((x) => x !== p.id))} />
						<span>{p.label}</span>
					</label>
				{/each}
			</div>
			<small>Nobody chosen: admins with the vault permission only.</small>
		</fieldset>
		<div class="sheet-actions">
			<button class="btn btn-primary" disabled={saving || !form.token}>
				{#if saving}<span class="spinner" aria-hidden="true"></span>{/if}Connect
			</button>
		</div>
	</form>
</Sheet>

<style>
	.list {
		margin: 0;
	}
	.form {
		display: flex;
		flex-direction: column;
		gap: 16px;
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
		margin: 6px 0;
	}
</style>
