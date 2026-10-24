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
			loadError = e instanceof ApiError ? e.message : 'De AD-instellingen konden niet worden geladen.';
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
		else error = e instanceof ApiError ? e.message : 'Er ging iets mis.';
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
				? 'Voer het bind-wachtwoord opnieuw in als je de AD-server wijzigt.'
				: 'Vul het wachtwoord van het service-account in.';
			return;
		}
		const weakened = adWeakenings(saved, form!);
		if (
			weakened.length &&
			!(await confirmer.ask({
				title: 'Dit verlaagt de beveiliging',
				message:
					weakened.length === 1
						? 'Deze wijziging maakt de verbinding met AD minder veilig. Weet je het zeker?'
						: 'Deze wijzigingen maken de verbinding met AD minder veilig. Weet je het zeker?',
				confirm: 'Toch opslaan',
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
			toast('Opgeslagen');
		} catch (err) {
			fail(err);
		} finally {
			busy = '';
		}
	}
</script>

<div class="pane-head"><h1 class="pane-title">Active Directory</h1></div>
<p class="pane-intro">Keystone controleert je wachtwoord bij je domeincontroller.</p>

{#if loadError}
	<div class="empty-note">
		<p>{loadError}</p>
		<button type="button" class="btn btn-secondary btn-sm" onclick={load}>Opnieuw proberen</button>
	</div>
{:else if !form}
	<div class="loading-note" aria-label="Laden"><span class="spinner"></span></div>
{:else}
	<form onsubmit={save} novalidate>
		<section class="group" aria-labelledby="g-conn">
			<h2 id="g-conn">Verbinding</h2>
			<div class="card form-card">
				<label class="field">
					<span>Server</span>
					<input class="input" placeholder="ldaps://dc.home.lan" autocapitalize="none" spellcheck="false" bind:value={form.url} />
				</label>
				<label class="field">
					<span>Service-account (bind DN)</span>
					<input
						class="input"
						placeholder="CN=keystone,OU=Service,DC=home,DC=lan"
						autocapitalize="none"
						spellcheck="false"
						bind:value={form.bind_dn}
					/>
				</label>
				<label class="field">
					<span>Wachtwoord service-account</span>
					<input
						class="input"
						type="password"
						autocomplete="off"
						placeholder={needsPassword ? '' : 'Ongewijzigd'}
						aria-invalid={passwordError ? 'true' : undefined}
						bind:value={password}
					/>
					{#if passwordError}
						<small class="field-error" role="alert">{passwordError}</small>
					{:else if passwordSet && !needsPassword}
						<small>Laat leeg om het opgeslagen wachtwoord te houden.</small>
					{:else if passwordSet}
						<small>Nieuwe server: vul het wachtwoord opnieuw in.</small>
					{/if}
				</label>
				<label class="field">
					<span>Zoekbasis (base DN)</span>
					<input class="input" placeholder="DC=home,DC=lan" autocapitalize="none" spellcheck="false" bind:value={form.base_dn} />
				</label>
			</div>
		</section>

		<section class="group" aria-labelledby="g-adv">
			<h2 id="g-adv">Geavanceerd</h2>
			<div class="card">
				<div class="srow">
					<div class="text">
						<label class="label" for="ad-starttls">StartTLS gebruiken</label>
						<p class="help">Voor ldap:// op poort 389.</p>
					</div>
					<input id="ad-starttls" class="switch" type="checkbox" role="switch" bind:checked={form.start_tls} />
				</div>
				<div class="srow wrap-phone">
					<div class="text">
						<span class="label" id="ad-tls">Minimale TLS-versie</span>
					</div>
					<div class="segmented" role="radiogroup" aria-labelledby="ad-tls">
						{#each ['1.2', '1.3'] as v}
							<label><input type="radio" name="min_tls" value={v} bind:group={form.min_tls} /><span>TLS {v}</span></label>
						{/each}
					</div>
				</div>
				<div class="srow">
					<div class="text">
						<label class="label" for="ad-timeout">Time-out</label>
						<p class="help">Daarna meldt de inlogpagina dat AD niet bereikbaar is.</p>
					</div>
					<div class="control">
						<input id="ad-timeout" class="input num" type="number" min="1" max="60" bind:value={form.timeout_seconds} />
						<span class="unit">seconden</span>
					</div>
				</div>
				<div class="srow stack">
					<div class="text">
						<label class="label" for="ad-filter">Gebruikersfilter</label>
						<p class="help">%s wordt vervangen door de gebruikersnaam.</p>
					</div>
					<input id="ad-filter" class="input mono" autocapitalize="none" spellcheck="false" bind:value={form.user_filter} />
				</div>
				<div class="srow stack">
					<div class="text">
						<label class="label" for="ad-ca">CA-certificaat (PEM)</label>
						<p class="help">Alleen nodig als je domeincontroller een eigen CA gebruikt.</p>
					</div>
					<textarea id="ad-ca" class="input" placeholder="-----BEGIN CERTIFICATE-----" bind:value={form.ca_pem}></textarea>
				</div>
				<div class="srow">
					<div class="text">
						<label class="label" for="ad-skip">Certificaat niet controleren</label>
						<p class="help">Onveilig — alleen om te testen.</p>
					</div>
					<input id="ad-skip" class="switch" type="checkbox" role="switch" bind:checked={form.insecure_skip_verify} />
				</div>
				<div class="srow">
					<div class="text">
						<label class="label" for="ad-plain">Onversleuteld ldap:// toestaan</label>
						<p class="help">Wachtwoorden gaan dan leesbaar over het netwerk.</p>
					</div>
					<input id="ad-plain" class="switch" type="checkbox" role="switch" bind:checked={form.allow_plaintext} />
				</div>
			</div>
		</section>

		{#if test}
			<p class={test.ok ? 'notice ok' : 'error'} role="status">
				{#if test.ok}
					Verbinding gelukt — {test.users_found}
					{test.users_found === 1 ? 'gebruiker' : 'gebruikers'} gevonden.
				{:else}
					Verbinding mislukt: {test.message ?? 'onbekende fout'}
				{/if}
			</p>
		{/if}
		{#if error}<p class="error" role="alert">{error}</p>{/if}

		<div class="actions">
			<button type="button" class="btn btn-secondary" onclick={runTest} disabled={busy !== ''}>
				{#if busy === 'test'}<span class="spinner" aria-hidden="true"></span>{/if}
				Test verbinding
			</button>
			<button class="btn btn-primary" disabled={busy !== ''}>
				{#if busy === 'save'}<span class="spinner" aria-hidden="true"></span>{/if}
				Opslaan
			</button>
		</div>
	</form>
{/if}

<Confirm bind:this={confirmer} />

<style>
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
