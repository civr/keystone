<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import { adWeakenings } from '$lib/adSecurity';
	import Confirm from '$lib/components/settings/Confirm.svelte';

	interface ADConfig {
		url: string;
		start_tls: boolean;
		insecure_skip_verify: boolean;
		ca_pem: string;
		bind_dn: string;
		base_dn: string;
		user_filter: string;
		min_tls: string;
		timeout_seconds: number;
		allow_plaintext: boolean;
	}

	const blank: ADConfig = {
		url: '',
		start_tls: false,
		insecure_skip_verify: false,
		ca_pem: '',
		bind_dn: '',
		base_dn: '',
		user_filter: '(&(objectClass=user)(sAMAccountName=%s))',
		min_tls: '1.2',
		timeout_seconds: 5,
		allow_plaintext: false
	};

	let form = $state<ADConfig | null>(null);
	/** The configuration as stored; null when none is stored yet. */
	let saved: ADConfig | null = null;
	let confirmer: Confirm;
	let storedURL = $state('');
	let passwordSet = $state(false);
	let password = $state('');
	let loadError = $state('');
	let busy = $state<'' | 'test' | 'save'>('');
	let error = $state('');
	let passwordError = $state('');
	let test = $state<{ ok: boolean; users_found: number; message?: string } | null>(null);

	const needsPassword = $derived(!passwordSet || (form !== null && form.url.trim() !== storedURL));

	async function load() {
		loadError = '';
		try {
			const r = await api.get<ADConfig & { bind_password_set: boolean }>('/admin/ad');
			const { bind_password_set, ...cfg } = r;
			form = { ...blank, ...cfg };
			saved = { ...form };
			storedURL = cfg.url;
			passwordSet = bind_password_set;
		} catch (e) {
			if (e instanceof ApiError && e.status === 404) {
				form = { ...blank };
				saved = null;
				return;
			}
			loadError = e instanceof ApiError ? e.message : 'The Active Directory settings could not be loaded.';
		}
	}
	onMount(load);

	function body() {
		const b: Record<string, unknown> = { ...form!, url: form!.url.trim(), timeout_seconds: Number(form!.timeout_seconds) };
		if (password) b.bind_password = password;
		return b;
	}

	function fail(e: unknown) {
		if (e instanceof ApiError && e.code === 'bind_password_required') passwordError = e.message;
		else error = e instanceof ApiError ? e.message : 'Something went wrong.';
	}

	async function runTest() {
		busy = 'test';
		error = passwordError = '';
		test = null;
		try {
			test = await api.send('POST', '/admin/ad/test', body());
		} catch (e) {
			fail(e);
		} finally {
			busy = '';
		}
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (needsPassword && !password) {
			passwordError = passwordSet
				? 'Enter the bind password again when you change the server.'
				: 'Enter the service account’s password.';
			return;
		}
		const weakened = adWeakenings(saved, form!);
		if (
			weakened.length &&
			!(await confirmer.ask({
				title: 'This lowers security',
				message:
					weakened.length === 1
						? 'This change makes the Active Directory connection less safe. Save it anyway?'
						: 'These changes make the Active Directory connection less safe. Save them anyway?',
				confirm: 'Save anyway',
				danger: true,
				items: weakened
			}))
		)
			return;
		busy = 'save';
		error = passwordError = '';
		try {
			await api.send('PUT', '/admin/ad', body());
			saved = { ...form!, url: form!.url.trim() };
			storedURL = form!.url.trim();
			passwordSet = true;
			password = '';
			toast('Saved');
		} catch (err) {
			fail(err);
		} finally {
			busy = '';
		}
	}
</script>

<div class="pane-head"><h1 class="pane-title">Active Directory</h1></div>
<p class="pane-intro">Optional: the owner can sign in with their Active Directory password instead of a Keystone password.</p>

{#if loadError}
	<div class="empty-note">
		<p>{loadError}</p>
		<button type="button" class="btn btn-secondary btn-sm" onclick={load}>Try again</button>
	</div>
{:else if !form}
	<div class="loading-note" aria-label="Loading"><span class="spinner"></span></div>
{:else}
	<form onsubmit={save} novalidate>
		<div class="cols">
		<section class="group" aria-labelledby="g-conn">
			<h2 id="g-conn">Connection</h2>
			<div class="card form-card">
				<label class="field">
					<span>Server</span>
					<input class="input" placeholder="ldaps://dc.home.lan" autocapitalize="none" spellcheck="false" bind:value={form.url} />
				</label>
				<label class="field">
					<span>Service account (bind DN)</span>
					<input
						class="input"
						placeholder="CN=keystone,OU=Service,DC=home,DC=lan"
						autocapitalize="none"
						spellcheck="false"
						bind:value={form.bind_dn}
					/>
				</label>
				<label class="field">
					<span>Service account password</span>
					<input
						class="input"
						type="password"
						autocomplete="off"
						placeholder={needsPassword ? '' : 'Unchanged'}
						aria-invalid={passwordError ? 'true' : undefined}
						bind:value={password}
					/>
					{#if passwordError}
						<small class="field-error" role="alert">{passwordError}</small>
					{:else if passwordSet && !needsPassword}
						<small>Leave empty to keep the stored password.</small>
					{:else if passwordSet}
						<small>New server: enter the password again.</small>
					{/if}
				</label>
				<label class="field">
					<span>Search base (base DN)</span>
					<input class="input" placeholder="DC=home,DC=lan" autocapitalize="none" spellcheck="false" bind:value={form.base_dn} />
				</label>
			</div>
		</section>

		<section class="group" aria-labelledby="g-adv">
			<h2 id="g-adv">Advanced</h2>
			<div class="card">
				<div class="srow">
					<div class="text">
						<label class="label" for="ad-starttls">Use StartTLS</label>
						<p class="help">For ldap:// on port 389.</p>
					</div>
					<input id="ad-starttls" class="switch" type="checkbox" role="switch" bind:checked={form.start_tls} />
				</div>
				<div class="srow wrap-phone">
					<div class="text">
						<span class="label" id="ad-tls">Minimum TLS version</span>
					</div>
					<div class="segmented" role="radiogroup" aria-labelledby="ad-tls">
						{#each ['1.2', '1.3'] as v}
							<label><input type="radio" name="min_tls" value={v} bind:group={form.min_tls} /><span>TLS {v}</span></label>
						{/each}
					</div>
				</div>
				<div class="srow">
					<div class="text">
						<label class="label" for="ad-timeout">Timeout</label>
						<p class="help">After this the sign-in page says Active Directory cannot be reached.</p>
					</div>
					<div class="control">
						<input id="ad-timeout" class="input num" type="number" min="1" max="60" bind:value={form.timeout_seconds} />
						<span class="unit">seconds</span>
					</div>
				</div>
				<div class="srow stack">
					<div class="text">
						<label class="label" for="ad-filter">User filter</label>
						<p class="help">%s is replaced by the username.</p>
					</div>
					<input id="ad-filter" class="input mono" autocapitalize="none" spellcheck="false" bind:value={form.user_filter} />
				</div>
				<div class="srow stack">
					<div class="text">
						<label class="label" for="ad-ca">CA certificate (PEM)</label>
						<p class="help">Only needed when your domain controller uses its own CA.</p>
					</div>
					<textarea id="ad-ca" class="input" placeholder="-----BEGIN CERTIFICATE-----" bind:value={form.ca_pem}></textarea>
				</div>
				<div class="srow">
					<div class="text">
						<label class="label" for="ad-skip">Do not check the certificate</label>
						<p class="help">Unsafe; for testing only.</p>
					</div>
					<input id="ad-skip" class="switch" type="checkbox" role="switch" bind:checked={form.insecure_skip_verify} />
				</div>
				<div class="srow">
					<div class="text">
						<label class="label" for="ad-plain">Allow unencrypted ldap://</label>
						<p class="help">Passwords then cross the network readable.</p>
					</div>
					<input id="ad-plain" class="switch" type="checkbox" role="switch" bind:checked={form.allow_plaintext} />
				</div>
			</div>
		</section>

		</div>

		{#if test}
			<p class={test.ok ? 'notice ok' : 'error'} role="status">
				{#if test.ok}
					Connected. Found {test.users_found}
					{test.users_found === 1 ? 'user' : 'users'}.
				{:else}
					Connection failed: {test.message ?? 'unknown error'}
				{/if}
			</p>
		{/if}
		{#if error}<p class="error" role="alert">{error}</p>{/if}

		<div class="actions">
			<button type="button" class="btn btn-secondary" onclick={runTest} disabled={busy !== ''}>
				{#if busy === 'test'}<span class="spinner" aria-hidden="true"></span>{/if}
				Test connection
			</button>
			<button class="btn btn-primary" disabled={busy !== ''}>
				{#if busy === 'save'}<span class="spinner" aria-hidden="true"></span>{/if}
				Save
			</button>
		</div>
	</form>
{/if}

<Confirm bind:this={confirmer} />

<style>
	/* Groups side by side on a wide pane; a lone group fills the width. */
	.cols {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(440px, 1fr));
		gap: 0 20px;
		align-items: start;
	}
	.cols > .group {
		break-inside: avoid;
	}
	.form-card {
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding: 16px;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
		margin-top: 8px;
	}
	.notice,
	.error {
		margin-bottom: 8px;
	}
	@media (max-width: 599px) {
		.actions > .btn {
			flex: 1;
		}
	}
</style>
