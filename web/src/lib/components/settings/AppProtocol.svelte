<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import type { App } from '$lib/apps';
	import { BYPASS_PRESETS, PROTOCOLS, connectionInfo, slugify, type ProtocolForm } from '$lib/protocols';
	import { copyText } from '$lib/copy';
	import { proxyConfig, type ProxyKind } from '$lib/proxyConfig';
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
	let proxyKind = $state<ProxyKind>('npm');
	let gateAddr = $state(`${location.hostname}:8081`);
	const proxyText = $derived(
		form.gate.host
			? proxyConfig(proxyKind, { host: form.gate.host, gate: gateAddr || `${location.hostname}:8081`, keystone: location.origin, signIn: form.gate.sign_in, header: form.gate.header })
			: ''
	);

	const info = $derived(app && app.protocol === form.protocol ? connectionInfo(location.origin, app) : []);
	const signInHelp: Record<string, string> = {
		basic: 'The gate adds the stored username and password as Basic auth to every request it lets through.',
		header: 'The gate adds the stored password in a header, for apps that take an API key.',
		form: 'The gate posts the app’s login form for you and hands your browser the session cookie.',
		json: 'The gate sends the details as JSON to the app’s login API and hands your browser the session cookie.',
		jellyfin: 'The gate signs in to Jellyfin for you; the web app opens already signed in.'
	};
	const serverSignIn = $derived(['form', 'json', 'jellyfin'].includes(form.gate.sign_in));
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
			await copyText(v);
			toast('Copied');
		} catch {
			toast('Copying failed.', 'error');
		}
	}

	async function newSecret() {
		if (!app || making) return;
		making = true;
		try {
			secret = (await api.send<{ secret: string }>('POST', `/apps/${app.id}/secret`)).secret;
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'No secret could be made.', 'error');
		} finally {
			making = false;
		}
	}
</script>

<label class="field">
	<span>Sign-in</span>
	<select id="app-protocol" class="input" bind:value={form.protocol} onchange={choose}>
		{#each PROTOCOLS as p}
			<option value={p.value}>{p.label}</option>
		{/each}
	</select>
	<small>{help}</small>
</label>

{#if form.protocol === 'saml'}
	<label class="field">
		<span>App’s entity ID</span>
		<input id="config.saml.entity_id" class="input mono" spellcheck="false" autocapitalize="none" placeholder="https://bastion.home.lan/api/auth/saml/metadata" aria-invalid={errors['config.saml.entity_id'] ? 'true' : undefined} bind:value={form.saml.entity_id} />
		{#if errors['config.saml.entity_id']}<small class="field-error" role="alert">{errors['config.saml.entity_id']}</small>{/if}
	</label>
	<label class="field">
		<span>ACS URL</span>
		<input id="config.saml.acs_url" class="input mono" type="url" spellcheck="false" autocapitalize="none" placeholder="https://bastion.home.lan/api/auth/saml/acs" aria-invalid={errors['config.saml.acs_url'] ? 'true' : undefined} bind:value={form.saml.acs_url} />
		{#if errors['config.saml.acs_url']}<small class="field-error" role="alert">{errors['config.saml.acs_url']}</small>{:else}<small>Where Keystone sends the signed SAML response.</small>{/if}
	</label>
	<label class="check">
		<input type="checkbox" bind:checked={form.saml.idp_initiated} />
		<span>Tile signs in directly (IdP-initiated)</span>
		<small>Off: the tile opens the address above and the app asks Keystone to sign you in.</small>
	</label>
{:else if form.protocol === 'oidc'}
	<label class="field">
		<span>Redirect URIs</span>
		<textarea id="config.oidc.redirect_uris" class="input mono" rows="2" spellcheck="false" autocapitalize="none" placeholder="https://nimbus.home.lan/api/auth/sso/callback" aria-invalid={errors['config.oidc.redirect_uris'] ? 'true' : undefined} bind:value={form.oidc.redirect_uris}></textarea>
		{#if errors['config.oidc.redirect_uris']}<small class="field-error" role="alert">{errors['config.oidc.redirect_uris']}</small>{:else}<small>One per line, exactly as the app sends them.</small>{/if}
	</label>
{:else if form.protocol === 'ldap'}
	<label class="field">
		<span>Name in LDAP</span>
		<input id="config.ldap.slug" class="input mono" spellcheck="false" autocapitalize="none" placeholder="jellyfin" aria-invalid={errors['config.ldap.slug'] ? 'true' : undefined} bind:value={form.ldap.slug} />
		{#if errors['config.ldap.slug']}<small class="field-error" role="alert">{errors['config.ldap.slug']}</small>{:else}<small>The app gets its own branch: ou={form.ldap.slug || '…'},dc=keystone.</small>{/if}
	</label>
{:else if form.protocol === 'gate'}
	<label class="field">
		<span>App’s host name</span>
		<input id="config.gate.host" class="input mono" spellcheck="false" autocapitalize="none" placeholder="ha.home.lan" aria-invalid={errors['config.gate.host'] ? 'true' : undefined} bind:value={form.gate.host} />
		{#if errors['config.gate.host']}<small class="field-error" role="alert">{errors['config.gate.host']}</small>{/if}
	</label>
	<label class="field">
		<span>Exceptions</span>
		<textarea id="config.gate.bypass_paths" class="input mono" rows="3" spellcheck="false" autocapitalize="none" placeholder="/api/" aria-invalid={errors['config.gate.bypass_paths'] ? 'true' : undefined} bind:value={form.gate.bypass}></textarea>
		{#if errors['config.gate.bypass_paths']}
			<small class="field-error" role="alert">{errors['config.gate.bypass_paths']}</small>
		{:else}
			<small>
				Paths the gate lets through, so phone apps can use their own token. Suggested:
				{#each BYPASS_PRESETS as p, i}
					{#if i}·{/if}
					<button type="button" class="link" onclick={() => (form.gate.bypass = p.paths.join('\n'))}>{p.name}</button>
				{/each}
			</small>
		{/if}
	</label>
	<label class="field">
		<span>Sign in for me</span>
		<select class="input" bind:value={form.gate.sign_in}>
			<option value="">Off</option>
			<option value="basic">Basic auth</option>
			<option value="header">Header with API key</option>
			<option value="form">Login form</option>
			<option value="json">JSON login</option>
			<option value="jellyfin">Jellyfin</option>
		</select>
		<small>{signInHelp[form.gate.sign_in] ?? 'Off: the app asks for its own login.'}</small>
	</label>
	{#if form.gate.sign_in}
		{#if form.gate.sign_in === 'header'}
			<label class="field">
				<span>Header name</span>
				<input id="config.gate.sign_in.header" class="input mono" spellcheck="false" autocapitalize="none" placeholder="X-Api-Key" aria-invalid={errors['config.gate.sign_in.header'] ? 'true' : undefined} bind:value={form.gate.header} />
				{#if errors['config.gate.sign_in.header']}<small class="field-error" role="alert">{errors['config.gate.sign_in.header']}</small>{/if}
			</label>
		{/if}
		{#if serverSignIn}
			<label class="field">
				<span>Address Keystone uses to reach the app</span>
				<input id="config.gate.sign_in.upstream" class="input mono" type="url" spellcheck="false" autocapitalize="none" placeholder="http://jellyfin:8096" aria-invalid={errors['config.gate.sign_in.upstream'] ? 'true' : undefined} bind:value={form.gate.upstream} />
				{#if errors['config.gate.sign_in.upstream']}<small class="field-error" role="alert">{errors['config.gate.sign_in.upstream']}</small>{:else}<small>keystone-gate calls this itself, so it must be reachable from the gate (for example a container name).</small>{/if}
			</label>
		{/if}
		{#if form.gate.sign_in === 'form' || form.gate.sign_in === 'json'}
			<label class="field">
				<span>Login path</span>
				<input id="config.gate.sign_in.login_path" class="input mono" spellcheck="false" autocapitalize="none" placeholder="/login" aria-invalid={errors['config.gate.sign_in.login_path'] ? 'true' : undefined} bind:value={form.gate.login_path} />
				{#if errors['config.gate.sign_in.login_path']}<small class="field-error" role="alert">{errors['config.gate.sign_in.login_path']}</small>{/if}
			</label>
			<div class="pair">
				<label class="field">
					<span>Username field</span>
					<input class="input mono" spellcheck="false" autocapitalize="none" bind:value={form.gate.user_field} />
				</label>
				<label class="field">
					<span>Password field</span>
					<input class="input mono" spellcheck="false" autocapitalize="none" bind:value={form.gate.pass_field} />
				</label>
			</div>
		{/if}
		<label class="field">
			<span>Username in the app</span>
			<input class="input" autocomplete="off" spellcheck="false" autocapitalize="none" bind:value={vault.username} />
		</label>
		<label class="field">
			<span>Password</span>
			<input class="input" type="password" autocomplete="new-password" placeholder={vault.hasPassword ? 'Saved' : ''} bind:value={vault.password} />
			<small>{vault.hasPassword ? 'Leave empty to keep the stored password.' : 'Kept encrypted with the vault key, outside the database.'}</small>
		</label>
	{/if}
	{#if proxyText}
		<div class="field">
			<span class="field-label">Reverse proxy config</span>
			<select class="input" aria-label="Reverse proxy" bind:value={proxyKind}>
				<option value="npm">Nginx Proxy Manager</option>
				<option value="nginx">nginx</option>
				<option value="caddy">Caddy</option>
				<option value="traefik">Traefik</option>
			</select>
			<label class="field">
				<span>Gate address</span>
				<input class="input mono" spellcheck="false" autocapitalize="none" placeholder="192.168.1.10:8081" bind:value={gateAddr} />
			</label>
			<textarea class="input mono" rows="10" readonly spellcheck="false" aria-label="Generated config" value={proxyText}></textarea>
			<div><button type="button" class="btn btn-secondary btn-sm" onclick={() => copy(proxyText)}>Copy</button></div>
			<small>Paste this into your proxy. Also add the proxy’s address under Network › Trusted proxies.{proxyKind === 'npm' ? ' Paste into the proxy host’s Advanced tab; keep Details pointing at the app.' : ''}</small>
		</div>
	{/if}
{:else if form.protocol === 'vault'}
	<label class="field">
		<span>Username in the app</span>
		<input class="input" autocomplete="off" spellcheck="false" autocapitalize="none" bind:value={vault.username} />
	</label>
	<label class="field">
		<span>Password</span>
		<input class="input" type="password" autocomplete="new-password" placeholder={vault.hasPassword ? '•••••••• (stored)' : ''} bind:value={vault.password} />
		<small>{vault.hasPassword ? 'Leave empty to keep the stored password.' : 'Kept encrypted with the vault key, outside the database.'}</small>
	</label>
	<fieldset class="field">
		<legend class="field-label">When the tile is opened</legend>
		<div class="segmented">
			<label><input type="radio" bind:group={form.vault.mode} value="copy" /><span>Copy the password</span></label>
			<label><input type="radio" bind:group={form.vault.mode} value="form" /><span>Sign in for me</span></label>
			<label><input type="radio" bind:group={form.vault.mode} value="json" /><span>Sign in (JSON)</span></label>
		</div>
		<small>
			{#if form.vault.mode === 'copy'}
				Shows the username and copies the password; you paste it into the app.
			{:else if form.vault.mode === 'form'}
				Keystone sends the app’s login form for you. Works with classic login pages (routers, NAS).
			{:else}
				Keystone sends the details as JSON to the app’s login API. Try it; not every app accepts it.
			{/if}
		</small>
	</fieldset>
	{#if form.vault.mode !== 'copy'}
		<label class="field">
			<span>Login form address</span>
			<input
				id="config.vault.login_url"
				class="input mono"
				type="url"
				spellcheck="false"
				autocapitalize="none"
				placeholder="http://192.168.1.1/login"
				aria-invalid={errors['config.vault.login_url'] ? 'true' : undefined}
				bind:value={form.vault.login_url}
			/>
			{#if errors['config.vault.login_url']}<small class="field-error" role="alert">{errors['config.vault.login_url']}</small>{:else}<small>Where the app’s login form posts to: its <code>action</code>, or the login API.</small>{/if}
		</label>
		<div class="pair">
			<label class="field">
				<span>Username field</span>
				<input class="input mono" spellcheck="false" autocapitalize="none" bind:value={form.vault.user_field} />
			</label>
			<label class="field">
				<span>Password field</span>
				<input class="input mono" spellcheck="false" autocapitalize="none" bind:value={form.vault.pass_field} />
			</label>
		</div>
	{/if}
{/if}

{#if form.protocol !== 'link' && form.protocol !== 'ldap'}
	<label class="check">
		<input type="checkbox" bind:checked={form.requireMFA} />
		<span>Require a second factor<small>Only people who signed in with a passkey or an authenticator code can open it.</small></span>
	</label>
{/if}

{#if form.protocol !== 'link' && form.protocol !== 'vault'}
	<fieldset class="field account">
		<legend class="field-label">Owner’s account in the app</legend>
		<small>Empty = the owner’s own details. Other people always use their own.</small>
		<input class="input" aria-label="Username in the app" placeholder="Username" autocomplete="off" spellcheck="false" autocapitalize="none" bind:value={form.account.username} />
		<input class="input" aria-label="E-mail address in the app" placeholder="E-mail address" type="email" autocomplete="off" bind:value={form.account.email} />
		<input class="input" aria-label="Display name in the app" placeholder="Display name" autocomplete="off" bind:value={form.account.display_name} />
	</fieldset>
{/if}

{#if info.length || ((form.protocol === 'oidc' || form.protocol === 'ldap') && app?.protocol === form.protocol)}
	<div class="field">
		<span class="field-label">For the app’s settings</span>
		<dl class="conn">
			{#each info as row}
				<dt>{row.label}</dt>
				<dd><code class="mono">{row.value}</code> <button type="button" class="btn btn-flat btn-sm" onclick={() => copy(row.value)}>Copy</button></dd>
			{/each}
			{#if form.protocol === 'oidc' || form.protocol === 'ldap'}
				<dt>{form.protocol === 'oidc' ? 'Client secret' : 'Bind password'}</dt>
				<dd>
					{#if secret}
						<code class="mono">{secret}</code> <button type="button" class="btn btn-flat btn-sm" onclick={() => copy(secret)}>Copy</button>
						<small>Shown only now. Store it in the app.</small>
					{:else}
						<span class="muted">{app?.has_secret ? 'Set' : 'Not made yet'}</span>
						<button type="button" class="btn btn-secondary btn-sm" disabled={making} onclick={newSecret}>{app?.has_secret ? 'Replace' : 'Make one'}</button>
					{/if}
				</dd>
			{/if}
		</dl>
	</div>
{:else if form.protocol !== 'link' && form.protocol !== 'vault' && (!app || app.protocol !== form.protocol)}
	<p class="muted small">After saving, the details for the app appear here.</p>
{/if}

<style>
	.pair {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
	}
	fieldset.field {
		margin: 0;
		padding: 0;
		border: 0;
	}
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
