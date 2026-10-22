<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import type { App } from '$lib/apps';
	import { BYPASS_PRESETS, PROTOCOLS, connectionInfo, slugify, type ProtocolForm } from '$lib/protocols';
	import { toast } from '$lib/toast.svelte';

	let {
		form = $bindable(),
		vault = $bindable(),
		app,
		name,
		errors
	}: {
		form: ProtocolForm;
		/** Vault credentials to save with the app; password '' keeps the stored one. */
		vault: { username: string; password: string; hasPassword: boolean };
		app: App | null;
		name: string;
		errors: Record<string, string>;
	} = $props();

	let secret = $state('');
	let making = $state(false);

	const info = $derived(app && app.protocol === form.protocol ? connectionInfo(location.origin, app) : []);
	const help = $derived(PROTOCOLS.find((p) => p.value === form.protocol)?.help ?? '');

	function choose() {
		if (form.protocol === 'ldap' && !form.ldap.slug) form.ldap.slug = slugify(name);
		if (form.protocol === 'gate' && !form.gate.host && app) {
			try {
				form.gate.host = new URL(app.url).hostname;
			} catch {
				/* no host to suggest */
			}
		}
	}

	async function copy(v: string) {
		try {
			await navigator.clipboard.writeText(v);
			toast('Gekopieerd');
		} catch {
			toast('Kopiëren is niet gelukt.', 'error');
		}
	}

	async function newSecret() {
		if (!app || making) return;
		making = true;
		try {
			secret = (await api.send<{ secret: string }>('POST', `/apps/${app.id}/secret`)).secret;
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Er kon geen geheim worden gemaakt.', 'error');
		} finally {
			making = false;
		}
	}
</script>

<label class="field">
	<span>Inloggen</span>
	<select id="app-protocol" class="input" bind:value={form.protocol} onchange={choose}>
		{#each PROTOCOLS as p}
			<option value={p.value}>{p.label}</option>
		{/each}
	</select>
	<small>{help}</small>
</label>

{#if form.protocol === 'saml'}
	<label class="field">
		<span>Entity ID van de app</span>
		<input id="config.saml.entity_id" class="input mono" spellcheck="false" autocapitalize="none" placeholder="https://bastion.home.lan/api/auth/saml/metadata" aria-invalid={errors['config.saml.entity_id'] ? 'true' : undefined} bind:value={form.saml.entity_id} />
		{#if errors['config.saml.entity_id']}<small class="field-error" role="alert">{errors['config.saml.entity_id']}</small>{/if}
	</label>
	<label class="field">
		<span>ACS-URL</span>
		<input id="config.saml.acs_url" class="input mono" type="url" spellcheck="false" autocapitalize="none" placeholder="https://bastion.home.lan/api/auth/saml/acs" aria-invalid={errors['config.saml.acs_url'] ? 'true' : undefined} bind:value={form.saml.acs_url} />
		{#if errors['config.saml.acs_url']}<small class="field-error" role="alert">{errors['config.saml.acs_url']}</small>{:else}<small>Hier stuurt Keystone de ondertekende SAML-respons heen.</small>{/if}
	</label>
	<label class="check">
		<input type="checkbox" bind:checked={form.saml.idp_initiated} />
		<span>Tegel logt direct in (IdP-initiated)</span>
		<small>Uit: de tegel opent het adres hierboven en de app vraagt Keystone zelf om in te loggen.</small>
	</label>
{:else if form.protocol === 'oidc'}
	<label class="field">
		<span>Redirect-URI's</span>
		<textarea id="config.oidc.redirect_uris" class="input mono" rows="2" spellcheck="false" autocapitalize="none" placeholder="https://nimbus.home.lan/api/auth/sso/callback" aria-invalid={errors['config.oidc.redirect_uris'] ? 'true' : undefined} bind:value={form.oidc.redirect_uris}></textarea>
		{#if errors['config.oidc.redirect_uris']}<small class="field-error" role="alert">{errors['config.oidc.redirect_uris']}</small>{:else}<small>Eén per regel, precies zoals de app ze meestuurt.</small>{/if}
	</label>
{:else if form.protocol === 'ldap'}
	<label class="field">
		<span>Naam in LDAP</span>
		<input id="config.ldap.slug" class="input mono" spellcheck="false" autocapitalize="none" placeholder="jellyfin" aria-invalid={errors['config.ldap.slug'] ? 'true' : undefined} bind:value={form.ldap.slug} />
		{#if errors['config.ldap.slug']}<small class="field-error" role="alert">{errors['config.ldap.slug']}</small>{:else}<small>De app krijgt een eigen tak: ou={form.ldap.slug || '…'},dc=keystone.</small>{/if}
	</label>
{:else if form.protocol === 'gate'}
	<label class="field">
		<span>Hostnaam van de app</span>
		<input id="config.gate.host" class="input mono" spellcheck="false" autocapitalize="none" placeholder="ha.home.lan" aria-invalid={errors['config.gate.host'] ? 'true' : undefined} bind:value={form.gate.host} />
		{#if errors['config.gate.host']}<small class="field-error" role="alert">{errors['config.gate.host']}</small>{/if}
	</label>
	<label class="field">
		<span>Uitzonderingen</span>
		<textarea id="config.gate.bypass_paths" class="input mono" rows="3" spellcheck="false" autocapitalize="none" placeholder="/api/" aria-invalid={errors['config.gate.bypass_paths'] ? 'true' : undefined} bind:value={form.gate.bypass}></textarea>
		{#if errors['config.gate.bypass_paths']}
			<small class="field-error" role="alert">{errors['config.gate.bypass_paths']}</small>
		{:else}
			<small>
				Paden die de poortwachter doorlaat, zodat apps op je telefoon met hun eigen token werken. Voorstel:
				{#each BYPASS_PRESETS as p, i}
					{#if i}·{/if}
					<button type="button" class="link" onclick={() => (form.gate.bypass = p.paths.join('\n'))}>{p.name}</button>
				{/each}
			</small>
		{/if}
	</label>
{:else if form.protocol === 'vault'}
	<label class="field">
		<span>Gebruikersnaam in de app</span>
		<input class="input" autocomplete="off" spellcheck="false" autocapitalize="none" bind:value={vault.username} />
	</label>
	<label class="field">
		<span>Wachtwoord</span>
		<input class="input" type="password" autocomplete="new-password" placeholder={vault.hasPassword ? '•••••••• (opgeslagen)' : ''} bind:value={vault.password} />
		<small>{vault.hasPassword ? 'Laat leeg om het opgeslagen wachtwoord te houden.' : 'Versleuteld bewaard met de kluissleutel, niet in de database.'}</small>
	</label>
{/if}

{#if form.protocol !== 'link' && form.protocol !== 'vault'}
	<fieldset class="field account">
		<legend class="field-label">Account in de app</legend>
		<small>Leeg = je eigen AD-gegevens.</small>
		<input class="input" aria-label="Gebruikersnaam in de app" placeholder="Gebruikersnaam" autocomplete="off" spellcheck="false" autocapitalize="none" bind:value={form.account.username} />
		<input class="input" aria-label="E-mailadres in de app" placeholder="E-mailadres" type="email" autocomplete="off" bind:value={form.account.email} />
		<input class="input" aria-label="Weergavenaam in de app" placeholder="Weergavenaam" autocomplete="off" bind:value={form.account.display_name} />
	</fieldset>
{/if}

{#if info.length || ((form.protocol === 'oidc' || form.protocol === 'ldap') && app?.protocol === form.protocol)}
	<div class="field">
		<span class="field-label">Voor de instellingen van de app</span>
		<dl class="conn">
			{#each info as row}
				<dt>{row.label}</dt>
				<dd><code class="mono">{row.value}</code> <button type="button" class="btn btn-flat btn-sm" onclick={() => copy(row.value)}>Kopieer</button></dd>
			{/each}
			{#if form.protocol === 'oidc' || form.protocol === 'ldap'}
				<dt>{form.protocol === 'oidc' ? 'Client-secret' : 'Bind-wachtwoord'}</dt>
				<dd>
					{#if secret}
						<code class="mono">{secret}</code> <button type="button" class="btn btn-flat btn-sm" onclick={() => copy(secret)}>Kopieer</button>
						<small>Alleen nu zichtbaar. Bewaar het in de app.</small>
					{:else}
						<span class="muted">{app?.has_secret ? 'Ingesteld' : 'Nog niet gemaakt'}</span>
						<button type="button" class="btn btn-secondary btn-sm" disabled={making} onclick={newSecret}>{app?.has_secret ? 'Vervang' : 'Maak aan'}</button>
					{/if}
				</dd>
			{/if}
		</dl>
	</div>
{:else if form.protocol !== 'link' && form.protocol !== 'vault' && (!app || app.protocol !== form.protocol)}
	<p class="muted small">Na opslaan zie je hier de gegevens voor de app.</p>
{/if}

<style>
	.account {
		display: grid;
		gap: 8px;
	}
	.conn {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 6px 12px;
		margin: 4px 0 0;
		font-size: 0.8125rem;
	}
	.conn dt {
		color: var(--color-text-muted);
		font-weight: 600;
		padding-top: 4px;
	}
	.conn dd {
		margin: 0;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px;
		min-width: 0;
	}
	.conn code {
		overflow-wrap: anywhere;
		user-select: all;
	}
	.muted {
		color: var(--color-text-muted);
	}
	.small {
		font-size: 0.8125rem;
		margin: 0;
	}
	@media (max-width: 479px) {
		.conn {
			grid-template-columns: 1fr;
		}
	}
</style>
