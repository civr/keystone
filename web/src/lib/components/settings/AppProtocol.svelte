<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import type { App } from '$lib/apps';
	import { BYPASS_PRESETS, PROTOCOLS, connectionInfo, slugify, type DetectedLogin, type KnownApp, type ProtocolForm } from '$lib/protocols';
	import { copyText } from '$lib/copy';
	import { proxyConfig, type ProxyKind } from '$lib/proxyConfig';
	import { toast } from '$lib/toast.svelte';

	let {
		form = $bindable(),
		vault = $bindable(),
		app,
		name,
		appUrl = '',
		errors
	}: {
		form: ProtocolForm;
		/** Vault credentials to save with the app; password '' keeps the stored one. */
		vault: { username: string; password: string; hasPassword: boolean };
		app: App | null;
		name: string;
		/** The tile's address, where the login form is looked for first. */
		appUrl?: string;
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

	let detecting = $state(false);
	type Found = { text: string; notes: string[]; error: boolean; app: KnownApp | null; base: string; target: 'vault' | 'gate' };
	let found = $state<Found | null>(null);

	function fieldsText(d: DetectedLogin): string {
		const who = d.user_field ? `${d.user_field} and ${d.pass_field}` : `only ${d.pass_field}`;
		const n = d.extra?.length ?? 0;
		const more = n ? `, plus ${n === 1 ? 'one other field' : `${n} other fields`} Keystone sends along` : '';
		return `Found the login form: ${who}${more}.`;
	}

	function origin(u: string): string {
		try {
			return new URL(u).origin;
		} catch {
			return '';
		}
	}

	const gateModes = ['basic', 'header', 'form', 'json', 'jellyfin'];

	/** Fills in the sign-in a recognised app needs, for the protocol now chosen. */
	function applyApp(a: KnownApp, base: string) {
		if (form.protocol === 'gate') {
			if (gateModes.includes(a.mode)) form.gate.sign_in = a.mode as ProtocolForm['gate']['sign_in'];
			if (!form.gate.upstream) form.gate.upstream = origin(base);
			if (a.login_path) form.gate.login_path = a.login_path;
			if (a.user_field) form.gate.user_field = a.user_field;
			if (a.pass_field) form.gate.pass_field = a.pass_field;
		} else if (form.protocol === 'vault') {
			form.vault.mode = a.mode as ProtocolForm['vault']['mode'];
			if (a.login_path) form.vault.login_url = origin(base) + a.login_path;
			if (a.user_field) form.vault.user_field = a.user_field;
			if (a.pass_field) form.vault.pass_field = a.pass_field;
		}
	}

	/** Whether the app's sign-in fits the protocol now chosen. */
	function fits(a: KnownApp): boolean {
		return a.protocol === form.protocol || (a.protocol === 'oidc' && form.protocol === 'vault' && a.mode === 'copy' && form.vault.mode === 'copy');
	}

	/** Switches to the protocol the recognised app needs. */
	function useApp(a: KnownApp, base: string) {
		const host = (() => {
			try {
				return new URL(appUrl || base).hostname;
			} catch {
				return '';
			}
		})();
		if (a.protocol === 'oidc') {
			form.protocol = 'oidc';
		} else {
			form.protocol = a.protocol;
			if (a.protocol === 'gate' && !form.gate.host) form.gate.host = host;
			applyApp(a, base);
		}
		if (found) found = { ...found, text: `Recognised ${a.name}. Settings filled in.`, app: null, target: a.protocol === 'gate' ? 'gate' : 'vault' };
	}

	/** Looks for the login form, or recognises the app, and fills in the sign-in. */
	async function detect(target: 'vault' | 'gate', quiet = false) {
		if (detecting) return;
		let url = '';
		if (target === 'vault') url = (form.vault.login_page || appUrl).trim();
		else {
			const base = form.gate.upstream.trim().replace(/\/+$/, '') || origin(appUrl);
			if (base) url = base + (form.gate.sign_in === 'form' && form.gate.login_path.trim() ? form.gate.login_path.trim() : '/');
		}
		if (!url) {
			if (!quiet) toast(target === 'vault' ? 'Enter the app’s address or login page first.' : 'Enter the app’s address first.', 'error');
			return;
		}
		detecting = true;
		try {
			const d = await api.send<DetectedLogin>('POST', '/apps/login-form', { url });
			const a = d.app;
			const notes = [...(a?.note ? [a.note] : []), ...(d.notes ?? [])];
			// A form found on the page beats the catalog: it has the app's exact fields.
			const useForm = !!d.login_url && (target === 'gate' ? form.gate.sign_in === 'form' || !form.gate.sign_in : form.vault.mode !== 'json');
			if (useForm && d.login_url) {
				if (target === 'vault') {
					if (form.vault.mode === 'copy') form.vault.mode = 'form';
					form.vault.login_url = d.login_url;
					form.vault.login_page = d.page_url;
					form.vault.user_field = d.user_field || 'username';
					form.vault.pass_field = d.pass_field || 'password';
				} else {
					const u = new URL(d.login_url);
					form.gate.sign_in = 'form';
					if (!form.gate.upstream) form.gate.upstream = origin(d.page_url);
					form.gate.login_path = u.pathname + u.search;
					form.gate.user_field = d.user_field || 'username';
					form.gate.pass_field = d.pass_field || 'password';
				}
			} else if (a && fits(a)) {
				applyApp(a, d.page_url);
			}
			let text = a ? `Recognised ${a.name}.` : '';
			if (useForm) text = (text ? text + ' ' : '') + fieldsText(d);
			else if (a && fits(a)) text += ' Settings filled in.';
			found = { text, notes, error: false, app: a && !useForm && !fits(a) ? a : null, base: d.page_url, target };
		} catch (e) {
			const msg = e instanceof ApiError ? e.message : 'Keystone could not look for the login form.';
			found = { text: msg, notes: [], error: true, app: null, base: '', target };
			if (!quiet) toast(msg, 'error');
		} finally {
			detecting = false;
		}
	}

	const useLabel = (a: KnownApp) =>
		a.protocol === 'gate' ? 'Use the gate' : a.protocol === 'oidc' ? 'Use OIDC' : a.mode === 'copy' ? 'Copy the password instead' : 'Use these settings';

	function vaultMode() {
		found = null;
		if (form.vault.mode === 'form' && !form.vault.login_url) void detect('vault', true);
	}

	function gateMode() {
		found = null;
		if (form.gate.sign_in === 'form' && form.gate.upstream && !form.gate.login_path) void detect('gate', true);
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

{#snippet foundBox()}
	{#if found}
		<div class="found" class:error={found.error} role="status">
			{#if found.text}<p>{found.text}</p>{/if}
			{#each found.notes as n}<p class="muted">{n}</p>{/each}
			{#if found.app}
				{@const a = found.app}
				<p><button type="button" class="btn btn-secondary btn-sm" onclick={() => useApp(a, found!.base)}>{useLabel(a)}</button></p>
			{/if}
		</div>
	{/if}
{/snippet}

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
		<select class="input" bind:value={form.gate.sign_in} onchange={gateMode}>
			<option value="">Off</option>
			<option value="basic">Basic auth</option>
			<option value="header">Header with API key</option>
			<option value="form">Login form</option>
			<option value="json">JSON login</option>
			<option value="jellyfin">Jellyfin</option>
		</select>
		<small>{signInHelp[form.gate.sign_in] ?? 'Off: the app asks for its own login.'}</small>
	</label>
	<div class="field">
		<div><button type="button" class="btn btn-secondary btn-sm" disabled={detecting} onclick={() => detect('gate')}>{detecting ? 'Looking…' : 'Find sign-in automatically'}</button></div>
		<small>Keystone opens the app, recognises it or finds its login form, and fills in how the gate signs in.</small>
	</div>
	{#if found?.target === 'gate'}{@render foundBox()}{/if}
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
			<div class="field">
				<label class="field-label" for="config.gate.sign_in.login_path">Login path</label>
				<div class="row">
					<input id="config.gate.sign_in.login_path" class="input mono" spellcheck="false" autocapitalize="none" placeholder="/login" aria-invalid={errors['config.gate.sign_in.login_path'] ? 'true' : undefined} bind:value={form.gate.login_path} />
				</div>
				{#if errors['config.gate.sign_in.login_path']}
					<small class="field-error" role="alert">{errors['config.gate.sign_in.login_path']}</small>
				{:else if form.gate.sign_in === 'form'}
					<small>The page with the login form. The gate opens it first and also sends its hidden fields and security token.</small>
				{/if}
			</div>
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
			<label><input type="radio" bind:group={form.vault.mode} value="copy" onchange={vaultMode} /><span>Copy the password</span></label>
			<label><input type="radio" bind:group={form.vault.mode} value="form" onchange={vaultMode} /><span>Sign in for me</span></label>
			<label><input type="radio" bind:group={form.vault.mode} value="json" onchange={vaultMode} /><span>Sign in (JSON)</span></label>
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
	{#if form.vault.mode === 'form'}
		<div class="field">
			<label class="field-label" for="config.vault.login_page">Login page</label>
			<div class="row">
				<input
					id="config.vault.login_page"
					class="input mono"
					type="url"
					spellcheck="false"
					autocapitalize="none"
					placeholder={appUrl || 'http://192.168.1.1/'}
					bind:value={form.vault.login_page}
				/>
				<button type="button" class="btn btn-secondary btn-sm" disabled={detecting} onclick={() => detect('vault')}>{detecting ? 'Looking…' : 'Find automatically'}</button>
			</div>
			<small>Keystone opens this page, finds the login form and fills in the fields below. At each sign-in it also sends the form’s hidden fields. Empty: the tile’s address.</small>
		</div>
	{/if}
	{#if form.vault.mode === 'json'}
		<div class="field">
			<div><button type="button" class="btn btn-secondary btn-sm" disabled={detecting} onclick={() => detect('vault')}>{detecting ? 'Looking…' : 'Find sign-in automatically'}</button></div>
		</div>
	{/if}
	{#if found?.target === 'vault'}{@render foundBox()}{/if}
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
	.row {
		display: flex;
		gap: 8px;
		align-items: center;
	}
	.row .input {
		flex: 1;
		min-width: 0;
	}
	.row .btn {
		flex: none;
	}
	.found {
		font-size: 0.8125rem;
		padding: 8px 12px;
		border-radius: 8px;
		background: var(--color-surface-muted, rgba(0, 0, 0, 0.04));
	}
	.found.error {
		color: var(--color-danger-text);
		background: var(--color-danger-tint);
	}
	.found p {
		margin: 0;
	}
	.found p + p {
		margin-top: 4px;
	}
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
