<script lang="ts">
	import '$lib/forms.css';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { passkeysNeedHttps } from '$lib/publicUrl';
	import { codesFile, download, formatCode } from '$lib/codes';
	import { step as slide } from '$lib/motion';
	import { ceremony, passkeyError, supported } from '$lib/webauthn';

	type Step = 'loading' | 'unlock' | 'domain' | 'ad' | 'owner' | 'passkey' | 'emergency' | 'codes' | 'done';
	const STEPS: { id: Step; label: string }[] = [
		{ id: 'unlock', label: 'Ontgrendelen' },
		{ id: 'domain', label: 'Domein' },
		{ id: 'ad', label: 'Active Directory' },
		{ id: 'owner', label: 'Jouw account' },
		{ id: 'passkey', label: 'Passkey' },
		{ id: 'emergency', label: 'Nood-account' },
		{ id: 'codes', label: 'Herstelcodes' },
		{ id: 'done', label: 'Klaar' }
	];
	const index = (s: Step) => STEPS.findIndex((x) => x.id === s);

	let current = $state<Step>('loading');
	let dir = $state(1);
	let busy = $state(false);
	let error = $state('');
	const moved = page.url.searchParams.has('verplaatst');

	// Unlock
	let unlockCode = $state('');
	// Domain
	let domain = $state('');
	let publicURL = $state('');
	let publicTouched = $state(false);
	let movingTo = $state('');
	// AD
	let adv = $state(false);
	let adForm = $state({
		url: '',
		start_tls: false,
		insecure_skip_verify: false,
		ca_pem: '',
		bind_dn: '',
		bind_password: '',
		base_dn: '',
		user_filter: '(&(objectClass=user)(sAMAccountName=%s))',
		min_tls: '1.2',
		timeout_seconds: 5,
		allow_plaintext: false
	});
	let adTest = $state<{ ok: boolean; error: string; users_found: number } | null>(null);
	// Owner
	let ownerUser = $state('');
	let ownerPassword = $state('');
	let ownerName = $state('');
	// Emergency
	let emPassword = $state('');
	let emConfirm = $state('');
	// Recovery codes
	let codes = $state<string[] | null>(null);
	let saved = $state(false);

	const UI_STEP: Record<string, Step> = {
		unlock: 'unlock',
		domain: 'domain',
		ad: 'ad',
		owner: 'owner',
		passkey: 'passkey',
		emergency: 'emergency',
		finish: 'codes'
	};

	function go(next: Step) {
		dir = index(next) >= index(current) ? 1 : -1;
		error = '';
		current = next;
	}

	async function loadStatus() {
		try {
			const s = await api.get<{ needed: boolean; step: string }>('/setup/status');
			if (!s.needed) return void goto('/', { replaceState: true });
			go(UI_STEP[s.step] ?? 'unlock');
		} catch (e) {
			current = 'unlock';
			fail(e);
		}
	}

	onMount(() => {
		prefillDomain();
		loadStatus();
		// Leaving before the recovery codes are saved loses them for good.
		const guard = (e: BeforeUnloadEvent) => {
			if (codes && !saved) e.preventDefault();
		};
		addEventListener('beforeunload', guard);
		return () => removeEventListener('beforeunload', guard);
	});

	function fail(e: unknown) {
		if (!(e instanceof ApiError)) {
			error = passkeyError(e);
			return;
		}
		switch (e.code) {
			case 'setup_done':
				return void goto('/', { replaceState: true });
			case 'setup_locked':
				go('unlock');
				error = 'Je setup-sessie is verlopen. Vul de setupcode opnieuw in.';
				return;
			case 'step_order':
				error = e.message;
				loadStatus();
				return;
			case 'bad_origin':
				error = `${e.message} Open Keystone via het publieke adres dat je bij Domein hebt ingevuld.`;
				return;
		}
		error = e.message;
	}

	/** Runs one request with busy/error handling; returns undefined on failure. */
	async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
		if (busy) return;
		busy = true;
		error = '';
		try {
			return await fn();
		} catch (e) {
			fail(e);
		} finally {
			busy = false;
		}
	}

	// --- unlock ---
	async function unlock(e: SubmitEvent) {
		e.preventDefault();
		const res = await run(() => api.send<{ step: string }>('POST', '/setup/unlock', { code: unlockCode }));
		if (res) {
			unlockCode = '';
			go(UI_STEP[res.step] ?? 'domain');
		}
	}

	// --- domain ---
	// The public address starts as the address in use now, so setup goes on
	// without a reverse proxy; it can be changed later under Instellingen.
	function prefillDomain() {
		const h = location.hostname;
		publicURL = location.origin;
		publicTouched = true;
		const isIP = /^[\d.]+$/.test(h) || h.includes(':') || !h.includes('.');
		if (!isIP) domain = h.split('.').slice(1).join('.');
	}
	function onDomainInput() {
		if (!publicTouched && !publicURL.trim()) publicURL = location.origin;
	}
	async function saveDomain(e: SubmitEvent) {
		e.preventDefault();
		const res = await run(() => api.send('POST', '/setup/domain', { domain, public_url: publicURL }));
		if (res === undefined) return;
		if (!adForm.base_dn) adForm.base_dn = baseDN(domain);
		let origin = '';
		try {
			origin = new URL(publicURL.trim()).origin;
		} catch {
			/* the server accepted it, so this does not happen */
		}
		if (origin && origin !== location.origin) {
			// Ruling R12: continue on the final address; the setup cookie is
			// host-only, so the code is asked once more there.
			movingTo = origin;
			setTimeout(() => location.assign(`${origin}/setup?verplaatst=1`), 1200);
			return;
		}
		go('ad');
	}
	const baseDN = (d: string) =>
		d
			.trim()
			.toLowerCase()
			.split('.')
			.filter(Boolean)
			.map((p) => `DC=${p}`)
			.join(',');

	// --- AD ---
	async function testAD() {
		adTest = null;
		const res = await run(() => api.send<{ ok: boolean; error: string; users_found: number }>('POST', '/setup/ad/test', adForm));
		if (res) adTest = res;
	}
	async function saveAD(e: SubmitEvent) {
		e.preventDefault();
		if ((await run(() => api.send('POST', '/setup/ad', adForm))) !== undefined) go('owner');
	}
	const adReady = $derived(!!(adForm.url.trim() && adForm.bind_dn.trim() && adForm.bind_password && adForm.base_dn.trim()));

	// --- owner ---
	async function saveOwner(e: SubmitEvent) {
		e.preventDefault();
		const res = await run(() =>
			api.send<{ username: string; display_name: string }>('POST', '/setup/owner', {
				username: ownerUser.trim(),
				password: ownerPassword
			})
		);
		if (!res) return;
		ownerPassword = '';
		ownerName = res.display_name || res.username;
		try {
			localStorage.setItem('keystone.username', res.username);
		} catch {
			/* not remembered */
		}
		go('passkey');
	}

	// --- passkey ---
	async function createPasskey() {
		if ((await run(() => ceremony('/setup/passkey/begin', '/setup/passkey/finish'))) !== undefined) go('emergency');
	}

	// --- emergency + finish ---
	const emValid = $derived([...emPassword].length >= 16 && emPassword === emConfirm);
	async function skipPasskey() {
		if ((await run(() => api.send('POST', '/setup/passkey/skip'))) !== undefined) go('emergency');
	}

	async function saveEmergency(e: SubmitEvent) {
		e.preventDefault();
		const ok = await run(() => api.send('POST', '/setup/emergency', { password: emPassword }));
		if (ok === undefined) return;
		emPassword = emConfirm = '';
		await finish();
	}
	async function finish() {
		const res = await run(() => api.send<{ recovery_codes: string[] }>('POST', '/setup/finish'));
		if (!res) return;
		codes = res.recovery_codes;
		go('codes');
	}

	function downloadCodes() {
		if (codes) download('keystone-herstelcodes.txt', codesFile(codes, location.origin));
	}
</script>

<svelte:head>
	<title>Installatie · Keystone</title>
</svelte:head>

{#snippet busyLabel(text: string)}
	{#if busy}<span class="spinner" aria-hidden="true"></span>{/if}{text}
{/snippet}

{#snippet errorBox()}
	{#if error}<p class="error" role="alert">{error}</p>{/if}
{/snippet}

<main class="auth-page">
	<div class="auth-card wide">
		<div class="auth-brand"><img class="mark" src="/favicon.svg" alt="" width="64" height="64" /><span>Keystone</span></div>
		{#if current !== 'loading'}
			{@const i = index(current)}
			<nav class="stepper" aria-label="Voortgang">
				<ol>
					{#each STEPS as s, n (s.id)}
						<li class:done={n < i} class:now={n === i} aria-current={n === i ? 'step' : undefined}>
							<span class="visually-hidden">{s.label}{n < i ? ' (klaar)' : ''}</span>
						</li>
					{/each}
				</ol>
				<p class="chip accent">Stap {i + 1} van {STEPS.length} · {STEPS[i].label}</p>
			</nav>
		{/if}

		<div class="auth-stage">
			{#key current}
				<section in:slide={{ dir }} out:slide={{ dir, out: true }}>
					{#if current === 'loading'}
						<p class="visually-hidden" role="status">Laden…</p>
					{:else if current === 'unlock'}
						<h1>Welkom bij Keystone</h1>
						<p class="lede">
							Vul de setupcode in. Je vindt hem in de logs:
							<code>docker compose logs keystone-core</code>
						</p>
						<form class="form" onsubmit={unlock} novalidate>
							{#if moved}
								<p class="notice" role="status">
									Je bent nu op je definitieve adres; vul de setup-code nog één keer in.
								</p>
							{/if}
							<label class="field">
								<span>Setupcode</span>
								<!-- svelte-ignore a11y_autofocus -->
								<input
									class="input code"
									autocomplete="one-time-code"
									autocapitalize="characters"
									spellcheck="false"
									placeholder="XXXX-XXXX-XXXX-XXXX"
									autofocus
									value={unlockCode}
									oninput={(e) => (unlockCode = e.currentTarget.value = formatCode(e.currentTarget.value))}
								/>
							</label>
							{@render errorBox()}
							<button class="btn btn-primary block" disabled={busy || unlockCode.length < 19}>
								{@render busyLabel('Ontgrendelen')}
							</button>
						</form>
					{:else if current === 'domain'}
						<h1>Domein</h1>
						<p class="lede">Onder welk domein draait Keystone, en op welk adres open je het?</p>
						{#if movingTo}
							<div class="moving" role="status">
								<span class="spinner" aria-hidden="true"></span>
								<p>
									Opgeslagen. Je gaat nu verder op
									<a href={`${movingTo}/setup?verplaatst=1`}>{movingTo}</a>
								</p>
							</div>
						{:else}
							<form class="form" onsubmit={saveDomain} novalidate>
								<label class="field">
									<span>Domein</span>
									<input
										class="input"
										placeholder="home.lan"
										autocapitalize="none"
										spellcheck="false"
										bind:value={domain}
										oninput={onDomainInput}
									/>
								</label>
								<label class="field">
									<span>Publiek adres van Keystone</span>
									<input
										class="input"
										type="url"
										inputmode="url"
										placeholder="https://auth.home.lan"
										autocapitalize="none"
										spellcheck="false"
										bind:value={publicURL}
										oninput={() => (publicTouched = true)}
									/>
									<small>
										Kies zelf de naam, bijvoorbeeld https://auth.home.lan. Nog geen reverse proxy? Laat het huidige
										adres staan; je wijzigt het later onder Instellingen › Algemeen.
									</small>
								</label>
								{#if passkeysNeedHttps(publicURL)}
									<p class="notice warn" role="status">
										Dit adres gebruikt geen https. Browsers bieden passkeys alleen aan via https (of localhost),
										dus je slaat de passkey-stap straks over en logt eerst in met je AD-wachtwoord. Zet Keystone
										later achter een reverse proxy met TLS en voeg dan een passkey toe.
									</p>
								{/if}
								{@render errorBox()}
								<button class="btn btn-primary block" disabled={busy || !domain.trim() || !publicURL.trim()}>
									{@render busyLabel('Volgende')}
								</button>
							</form>
						{/if}
					{:else if current === 'ad'}
						<h1>Active Directory</h1>
						<p class="lede">Keystone controleert je wachtwoord bij je domeincontroller.</p>
						<form class="form" onsubmit={saveAD} novalidate>
							<label class="field">
								<span>Server</span>
								<input class="input" placeholder="ldaps://dc.home.lan" autocapitalize="none" spellcheck="false" bind:value={adForm.url} />
							</label>
							<label class="field">
								<span>Service-account (bind DN)</span>
								<input
									class="input"
									placeholder="CN=keystone,OU=Service,DC=home,DC=lan"
									autocapitalize="none"
									spellcheck="false"
									bind:value={adForm.bind_dn}
								/>
							</label>
							<label class="field">
								<span>Wachtwoord service-account</span>
								<input class="input" type="password" autocomplete="off" bind:value={adForm.bind_password} />
							</label>
							<label class="field">
								<span>Zoekbasis (base DN)</span>
								<input class="input" placeholder="DC=home,DC=lan" autocapitalize="none" spellcheck="false" bind:value={adForm.base_dn} />
							</label>

							<details bind:open={adv}>
								<summary>Geavanceerd</summary>
								<div class="form adv">
									<label class="check">
										<input type="checkbox" bind:checked={adForm.start_tls} />
										<span>StartTLS gebruiken<small>Voor ldap:// op poort 389.</small></span>
									</label>
									<label class="field">
										<span>Minimale TLS-versie</span>
										<select class="input" bind:value={adForm.min_tls}>
											<option value="1.2">TLS 1.2</option>
											<option value="1.3">TLS 1.3</option>
										</select>
									</label>
									<label class="field">
										<span>CA-certificaat (PEM)</span>
										<textarea class="input" placeholder="-----BEGIN CERTIFICATE-----" bind:value={adForm.ca_pem}></textarea>
										<small>Alleen nodig als je domeincontroller een eigen CA gebruikt.</small>
									</label>
									<label class="field">
										<span>Gebruikersfilter</span>
										<input class="input" autocapitalize="none" spellcheck="false" bind:value={adForm.user_filter} />
									</label>
									<label class="field">
										<span>Time-out (seconden)</span>
										<input class="input" type="number" min="1" max="60" bind:value={adForm.timeout_seconds} />
									</label>
									<label class="check">
										<input type="checkbox" bind:checked={adForm.insecure_skip_verify} />
										<span>Certificaat niet controleren<small>Onveilig — alleen om te testen.</small></span>
									</label>
									<label class="check">
										<input type="checkbox" bind:checked={adForm.allow_plaintext} />
										<span>Onversleuteld ldap:// toestaan<small>Wachtwoorden gaan dan leesbaar over het netwerk.</small></span>
									</label>
								</div>
							</details>

							{#if adTest}
								<p class={adTest.ok ? 'notice ok' : 'error'} role="status">
									{#if adTest.ok}
										Verbinding gelukt — {adTest.users_found}
										{adTest.users_found === 1 ? 'gebruiker' : 'gebruikers'} gevonden.
									{:else}
										Verbinding mislukt: {adTest.error}
									{/if}
								</p>
							{/if}
							{@render errorBox()}
							<div class="row">
								<button type="button" class="btn btn-secondary" onclick={testAD} disabled={busy || !adReady}>Test verbinding</button>
								<button class="btn btn-primary" disabled={busy || !adReady}>{@render busyLabel('Volgende')}</button>
							</div>
						</form>
						<div class="links">
							<button type="button" class="link" onclick={() => go('domain')}>Vorige</button>
						</div>
					{:else if current === 'owner'}
						<h1>Jouw account</h1>
						<p class="lede">Alleen dit AD-account kan straks inloggen bij Keystone.</p>
						<form class="form" onsubmit={saveOwner} novalidate>
							<label class="field">
								<span>Gebruikersnaam</span>
								<!-- svelte-ignore a11y_autofocus -->
								<input class="input" autocomplete="username" autocapitalize="none" spellcheck="false" autofocus bind:value={ownerUser} />
							</label>
							<label class="field">
								<span>Wachtwoord</span>
								<input class="input" type="password" autocomplete="current-password" bind:value={ownerPassword} />
							</label>
							{@render errorBox()}
							<button class="btn btn-primary block" disabled={busy || !ownerUser.trim() || !ownerPassword}>
								{@render busyLabel('Volgende')}
							</button>
						</form>
						<div class="links">
							<button type="button" class="link" onclick={() => go('ad')}>Vorige</button>
						</div>
					{:else if current === 'passkey'}
						<h1>{ownerName ? `Hoi ${ownerName}` : 'Passkey'}</h1>
						<p class="lede">
							Maak een passkey op dit apparaat. Daarmee bevestig je voortaan elke login — met Face ID, Touch ID,
							Windows Hello of een beveiligingssleutel.
						</p>
						<div class="form">
							{#if !supported()}
								<p class="notice warn" role="status">
									{window.isSecureContext
										? 'Deze browser ondersteunt geen passkeys.'
										: 'Passkeys werken alleen via https (of localhost), en dit adres is http.'}
									Ga verder zonder passkey: je logt dan in met je AD-wachtwoord en voegt later via https een passkey toe
									onder Instellingen › Beveiliging.
								</p>
							{/if}
							{@render errorBox()}
							{#if supported()}
								<button type="button" class="btn btn-primary block" onclick={createPasskey} disabled={busy}>
									{@render busyLabel('Passkey maken')}
								</button>
							{/if}
							<button type="button" class="btn {supported() ? 'btn-secondary' : 'btn-primary'} block" onclick={skipPasskey} disabled={busy}>
								Later, verder zonder passkey
							</button>
						</div>
					{:else if current === 'emergency'}
						<h1>Nood-account</h1>
						<p class="lede">
							Een noodwachtwoord werkt alleen als AD onbereikbaar is, en ook dan vraagt Keystone daarna je passkey.
						</p>
						<form class="form" onsubmit={saveEmergency} novalidate>
							<label class="field">
								<span>Noodwachtwoord</span>
								<input class="input" type="password" autocomplete="new-password" bind:value={emPassword} />
								<small>Minstens 16 tekens{emPassword ? ` — nu ${[...emPassword].length}` : ''}.</small>
							</label>
							<label class="field">
								<span>Herhaal noodwachtwoord</span>
								<input
									class="input"
									type="password"
									autocomplete="new-password"
									bind:value={emConfirm}
									aria-invalid={emConfirm && emConfirm !== emPassword ? 'true' : undefined}
								/>
							</label>
							{@render errorBox()}
							<div class="row">
								<button type="button" class="btn btn-secondary" onclick={finish} disabled={busy}>Overslaan</button>
								<button class="btn btn-primary" disabled={busy || !emValid}>{@render busyLabel('Opslaan')}</button>
							</div>
						</form>
					{:else if current === 'codes'}
						<h1>Herstelcodes</h1>
						{#if codes}
							<p class="lede">
								Kwijt je je passkey, dan kom je met een van deze codes weer binnen. Elke code werkt één keer. Bewaar ze
								op een veilige plek — je ziet ze hierna niet meer.
							</p>
							<div class="form">
								<ol class="codes">
									{#each codes as c}
										<li><code>{c}</code></li>
									{/each}
								</ol>
								<button type="button" class="btn btn-secondary block" onclick={downloadCodes}>
									<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4v11M7 10l5 5 5-5M5 20h14" /></svg>
									Download als tekstbestand
								</button>
								<label class="check">
									<input type="checkbox" bind:checked={saved} />
									<span>Ik heb ze bewaard</span>
								</label>
								<button type="button" class="btn btn-primary block" disabled={!saved} onclick={() => go('done')}>Klaar</button>
							</div>
						{:else}
							<p class="lede">Rond de installatie af; je krijgt dan je herstelcodes te zien.</p>
							<div class="form">
								{@render errorBox()}
								<button type="button" class="btn btn-primary block" onclick={finish} disabled={busy}>
									{@render busyLabel('Herstelcodes maken')}
								</button>
							</div>
						{/if}
					{:else if current === 'done'}
						<div class="done-mark" aria-hidden="true">
							<svg viewBox="0 0 24 24"><path d="M5 12.5l4.5 4.5L19 7.5" /></svg>
						</div>
						<h1>Keystone is klaar</h1>
						<p class="lede">Je bent ingelogd. Voeg je eerste apps toe op je startpagina.</p>
						<button type="button" class="btn btn-primary block" onclick={() => goto('/', { replaceState: true })}>
							Naar je startpagina
						</button>
					{/if}
				</section>
			{/key}
		</div>
	</div>
</main>

<style>
	/* Progress as page dots; the current one stretches into a capsule. */
	.stepper {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 12px;
		margin: -8px 0 24px;
	}
	.stepper ol {
		display: flex;
		justify-content: center;
		gap: 7px;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.stepper li {
		width: 7px;
		height: 7px;
		border-radius: var(--radius-full);
		background: #d1d1d6;
		transition:
			background-color var(--dur-3) var(--ease),
			width var(--dur-3) var(--ease-out);
	}
	.stepper li.done {
		background: #8e8e93;
	}
	.stepper li.now {
		width: 22px;
		background: var(--primary);
	}
	.stepper p {
		margin: 0;
	}

	.lede code {
		font-size: 0.8125rem;
		padding: 1px 6px;
		border-radius: var(--radius-xs);
		background: var(--c-bg-sunken);
		color: var(--color-text);
		white-space: nowrap;
	}

	details {
		box-shadow: 0 -0.5px 0 #c6c6c8;
		padding-top: 12px;
	}
	summary {
		cursor: pointer;
		font-size: 0.9375rem;
		font-weight: 400;
		color: var(--primary);
		width: fit-content;
		border-radius: var(--radius-xs);
	}
	.adv {
		margin-top: 16px;
	}

	.moving {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 12px 14px;
		border-radius: var(--radius-md);
		background: var(--color-info-tint);
		color: var(--primary-dark);
	}
	.moving p {
		margin: 0;
		color: var(--color-text);
		line-height: 1.5;
		overflow-wrap: anywhere;
	}

	.codes {
		display: grid;
		grid-template-columns: repeat(2, 1fr);
		gap: 6px 16px;
		margin: 0;
		padding: 14px 16px;
		border-radius: var(--radius-md);
		background: var(--color-bg);
		list-style: none;
		counter-reset: code;
	}
	.codes li {
		display: flex;
		align-items: baseline;
		gap: 8px;
		counter-increment: code;
	}
	.codes li::before {
		content: counter(code);
		width: 1.4em;
		text-align: right;
		color: var(--color-text-faint);
		font-size: 0.75rem;
		font-variant-numeric: tabular-nums;
	}
	.codes code {
		font: 500 0.875rem/1.7 var(--font-mono);
		letter-spacing: 0.02em;
		user-select: all;
	}
	@media (max-width: 420px) {
		.codes {
			grid-template-columns: 1fr;
		}
	}

	/* Done: a filled green check, as Apple confirms a finished setup. */
	.done-mark {
		display: grid;
		place-items: center;
		width: 64px;
		height: 64px;
		margin: 0 auto 18px;
		border-radius: 50%;
		background: var(--color-success);
		color: #fff;
		box-shadow: 0 8px 20px -6px rgba(52, 199, 89, 0.55);
	}
	.done-mark svg {
		width: 28px;
		height: 28px;
		fill: none;
		stroke: currentColor;
		stroke-width: 2.4;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
</style>
