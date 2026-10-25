<script lang="ts">
	import '$lib/forms.css';
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { safeNext } from '$lib/nav';
	import { api, ApiError, type Me } from '$lib/api';
	import { formatCode } from '$lib/codes';
	import { brand as site, logoSrc, pageTitle, loginBackground } from '$lib/brand.svelte';
	import { step } from '$lib/motion';
	import {
		authenticate,
		ceremony,
		conditionalSupported,
		passkeyError,
		supported,
		type OptionsJSON
	} from '$lib/webauthn';

	type Mode = 'loading' | 'password' | 'emergency' | 'passkey' | 'register' | 'recovery';
	const ORDER: Mode[] = ['loading', 'password', 'emergency', 'passkey', 'register', 'recovery'];
	const USER_KEY = 'keystone.username';

	let mode = $state<Mode>('loading');
	let dir = $state(1);
	let username = $state(readUser());
	let password = $state('');
	let emergencyPassword = $state('');
	let code = $state('');
	let busy = $state(false);
	let error = $state('');
	let notice = $state('');
	let trusted = $state(false); // this device may sign in with a passkey only
	let emergencyOffered = $state(false); // AD down and the emergency account is enabled
	let wantRecovery = $state(false); // after the password, ask for a recovery code
	let forced = $state(false); // signed in with a recovery code: new passkey first

	let conditional: AbortController | null = null;
	let userInput = $state<HTMLInputElement | null>(null);
	let passwordInput = $state<HTMLInputElement | null>(null);

	function readUser(): string {
		try {
			return localStorage.getItem(USER_KEY) ?? '';
		} catch {
			return '';
		}
	}
	function saveUser() {
		if (!username.trim()) return; // passkey-only sign-in: keep what was remembered
		try {
			localStorage.setItem(USER_KEY, username.trim());
		} catch {
			/* private mode: not remembered */
		}
	}

	function go(next: Mode) {
		dir = ORDER.indexOf(next) >= ORDER.indexOf(mode) ? 1 : -1;
		error = '';
		mode = next;
	}

	function fail(e: unknown) {
		if (e instanceof ApiError) {
			if (e.code === 'setup_needed') return void goto('/setup', { replaceState: true });
			if (e.code === 'password_required') {
				// The 5-minute pending login expired: start over.
				go('password');
				error = 'Your sign-in timed out. Sign in again.';
				return;
			}
			error = e.message;
			if (e.code === 'locked' && typeof e.data.retry_after === 'number') {
				const min = Math.max(1, Math.ceil(e.data.retry_after / 60));
				error += ` Try again in ${min} ${min === 1 ? 'minute' : 'minutes'}.`;
			}
			if (e.code === 'ad_unavailable') emergencyOffered = e.data.emergency === true;
			return;
		}
		error = passkeyError(e);
	}

	async function finished() {
		saveUser();
		await leave();
	}

	/** Back to the app sign-in that sent us here (a server route), else home. */
	async function leave() {
		const next = safeNext(page.url.searchParams.get('next'));
		if (next !== '/') return location.replace(next);
		await goto('/', { replaceState: true });
	}

	onMount(() => {
		start();
		return () => conditional?.abort();
	});

	async function start() {
		const [me, status] = await Promise.all([
			api.get<Me>('/me').catch(() => null),
			api.get<{ needed: boolean }>('/setup/status').catch(() => null)
		]);
		if (status?.needed) return void goto('/setup', { replaceState: true });
		if (me?.must_register_passkey) {
			forced = true;
			go('register');
			return;
		}
		if (me) return void leave();
		// Behind an identity-aware proxy that already signed us in.
		try {
			await api.send('POST', '/auth/proxy');
			return void (await finished());
		} catch {
			/* not set up, or not through the proxy: sign in here */
		}
		go('password');
		await tick();
		(username ? passwordInput : userInput)?.focus(); // remembered user: straight to the password
		if (supported()) await probeDevice();
	}

	/** A trusted device gets passkey options without a password. */
	async function probeDevice() {
		let opts: OptionsJSON;
		try {
			opts = await api.send<OptionsJSON>('POST', '/auth/passkey/begin');
		} catch (e) {
			if (e instanceof ApiError && e.code === 'setup_needed') fail(e);
			return; // password_required: not a trusted device
		}
		trusted = true;
		if (await conditionalSupported()) armConditional(opts);
	}

	/** Offers the passkey in the username autofill: no typing needed. */
	async function armConditional(opts: OptionsJSON) {
		conditional?.abort();
		const ctl = new AbortController();
		conditional = ctl;
		let body;
		try {
			body = await authenticate(opts, { conditional: true, signal: ctl.signal });
		} catch {
			return; // aborted, dismissed or unavailable: the button and password still work
		}
		busy = true;
		error = '';
		try {
			await api.send('POST', '/auth/passkey/finish', body);
			await finished();
		} catch (e) {
			fail(e);
		} finally {
			busy = false;
		}
	}

	function stopConditional() {
		conditional?.abort();
		conditional = null;
	}

	async function passkeyOnly() {
		stopConditional();
		busy = true;
		error = '';
		try {
			await ceremony('/auth/passkey/begin', '/auth/passkey/finish');
			await finished();
		} catch (e) {
			fail(e);
		} finally {
			busy = false;
		}
	}

	async function afterPassword(next: string) {
		if (next === 'done') return finished();
		saveUser();
		if (wantRecovery) return go('recovery');
		go(next === 'register_passkey' ? 'register' : 'passkey');
		await runPasskey();
	}

	async function submitPassword(e: SubmitEvent) {
		e.preventDefault();
		if (busy) return;
		stopConditional();
		busy = true;
		error = '';
		emergencyOffered = false;
		try {
			const res = await api.send<{ next: string }>('POST', '/auth/password', {
				username: username.trim(),
				password
			});
			password = '';
			busy = false;
			await afterPassword(res.next);
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}

	async function submitEmergency(e: SubmitEvent) {
		e.preventDefault();
		if (busy) return;
		busy = true;
		error = '';
		try {
			const res = await api.send<{ next: string }>('POST', '/auth/emergency', { password: emergencyPassword });
			emergencyPassword = '';
			busy = false;
			await afterPassword(res.next);
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}

	/** Passkey after the password: sign in, or register when none exists yet. */
	async function runPasskey() {
		if (busy) return;
		busy = true;
		error = '';
		try {
			await ceremony('/auth/passkey/begin', '/auth/passkey/finish');
			await finished();
		} catch (e) {
			fail(e);
		} finally {
			busy = false;
		}
	}

	async function submitRecovery(e: SubmitEvent) {
		e.preventDefault();
		if (busy) return;
		busy = true;
		error = '';
		try {
			await api.send('POST', '/auth/recovery', { code });
			code = '';
			forced = true;
			busy = false;
			go('register');
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}

	function chooseRecovery() {
		if (mode === 'passkey' || mode === 'register') return go('recovery');
		wantRecovery = true;
		notice = 'Sign in with your password first; then enter a recovery code.';
	}

	function restart() {
		wantRecovery = false;
		notice = '';
		go('password');
	}

	const titles: Record<Mode, string> = {
		loading: site.name,
		password: 'Sign in',
		emergency: 'Emergency account',
		passkey: 'Confirm with your passkey',
		register: 'Create a passkey',
		recovery: 'Recovery code'
	};
</script>

<svelte:head>
	<title>{pageTitle('Sign in')}</title>
</svelte:head>

{#snippet brand()}
	<div class="auth-brand"><img class="mark" src={logoSrc()} alt="" width="64" height="64" /><span>{site.name}</span></div>
{/snippet}

{#snippet busyLabel(text: string)}
	{#if busy}<span class="spinner" aria-hidden="true"></span>{/if}{text}
{/snippet}

<main class="auth-page" style:background={loginBackground() || undefined}>
	<div class="auth-card">
		{@render brand()}
		<div class="auth-stage">
			{#key mode}
				<section in:step={{ dir }} out:step={{ dir, out: true }} aria-labelledby="title">
					{#if mode === 'loading'}
						<p class="visually-hidden" role="status">Loading…</p>
					{:else}
						<h1 id="title">{titles[mode]}</h1>
					{/if}

					{#if mode === 'password'}
						<form class="form" onsubmit={submitPassword} novalidate>
							{#if trusted}
								<button type="button" class="btn btn-primary block" onclick={passkeyOnly} disabled={busy}>
									<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="9" cy="8" r="4" /><path d="M2 21v-1a6 6 0 0 1 9.5-4.9" /><circle cx="17.5" cy="15.5" r="2.5" /><path d="M17.5 18v4M17.5 20h2" /></svg>
									Sign in with a passkey
								</button>
								<div class="divider"><span>or with your password</span></div>
							{/if}
							{#if notice}<p class="notice" role="status">{notice}</p>{/if}
							<label class="field">
								<span>Username</span>
								<input
									bind:this={userInput}
									class="input"
									name="username"
									autocomplete="username webauthn"
									autocapitalize="none"
									spellcheck="false"
									required
									bind:value={username}
								/>
							</label>
							<label class="field">
								<span>Password</span>
								<input
									class="input"
									bind:this={passwordInput}
									type="password"
									name="password"
									autocomplete="current-password webauthn"
									required
									bind:value={password}
									aria-invalid={error ? 'true' : undefined}
								/>
							</label>
							{#if error}<p class="error" role="alert">{error}</p>{/if}
							<button
								class="btn block {trusted ? 'btn-secondary' : 'btn-primary'}"
								disabled={busy || !username.trim() || !password}
							>
								{@render busyLabel('Sign in')}
							</button>
						</form>
						<div class="links">
							{#if !wantRecovery}
								<button type="button" class="link" onclick={chooseRecovery}>Use a recovery code</button>
							{:else}
								<button type="button" class="link" onclick={restart}>Cancel</button>
							{/if}
							{#if emergencyOffered}
								<button type="button" class="link" onclick={() => go('emergency')}>Use the emergency account</button>
							{/if}
						</div>
					{:else if mode === 'emergency'}
						<p class="lede">Active Directory cannot be reached. Sign in with the emergency password, then confirm with your passkey.</p>
						<form class="form" onsubmit={submitEmergency} novalidate>
							<label class="field">
								<span>Emergency password</span>
								<!-- svelte-ignore a11y_autofocus -->
								<input
									class="input"
									type="password"
									autocomplete="current-password"
									autofocus
									required
									bind:value={emergencyPassword}
								/>
							</label>
							{#if error}<p class="error" role="alert">{error}</p>{/if}
							<button class="btn btn-primary block" disabled={busy || !emergencyPassword}>
								{@render busyLabel('Continue')}
							</button>
						</form>
						<div class="links">
							<button type="button" class="link" onclick={restart}>Back</button>
						</div>
					{:else if mode === 'passkey' || mode === 'register'}
						<p class="lede">
							{#if mode === 'passkey'}
								Use Face ID, Touch ID, Windows Hello or your security key.
							{:else if forced}
								You are in with a recovery code. Create a new passkey for this device now.
							{:else}
								There is no passkey yet. Create one on this device to continue.
							{/if}
						</p>
						<div class="form">
							{#if error}<p class="error" role="alert">{error}</p>{/if}
							<button type="button" class="btn btn-primary block" onclick={runPasskey} disabled={busy}>
								{@render busyLabel(mode === 'passkey' ? 'Use passkey' : 'Create passkey')}
							</button>
						</div>
						{#if !forced}
							<div class="links">
								{#if mode === 'passkey'}
									<button type="button" class="link" onclick={chooseRecovery}>Use a recovery code</button>
								{/if}
								<button type="button" class="link" onclick={restart}>Other account</button>
							</div>
						{/if}
					{:else if mode === 'recovery'}
						<p class="lede">Enter one of your recovery codes. Then create a new passkey.</p>
						<form class="form" onsubmit={submitRecovery} novalidate>
							<label class="field">
								<span>Recovery code</span>
								<!-- svelte-ignore a11y_autofocus -->
								<input
									class="input code"
									autocomplete="one-time-code"
									autocapitalize="characters"
									spellcheck="false"
									placeholder="XXXX-XXXX-XXXX-XXXX"
									autofocus
									value={code}
									oninput={(e) => (code = e.currentTarget.value = formatCode(e.currentTarget.value))}
								/>
							</label>
							{#if error}<p class="error" role="alert">{error}</p>{/if}
							<button class="btn btn-primary block" disabled={busy || code.length < 19}>
								{@render busyLabel('Continue')}
							</button>
						</form>
						<div class="links">
							<button type="button" class="link" onclick={restart}>Back</button>
						</div>
					{/if}
				</section>
			{/key}
		</div>
	</div>
</main>

<style>
	.divider {
		display: flex;
		align-items: center;
		gap: 12px;
		color: var(--color-text-faint);
		font-size: 0.75rem;
		font-weight: 600;
	}
	.divider::before,
	.divider::after {
		content: '';
		flex: 1;
		height: 1px;
		background: var(--c-border);
	}
</style>
