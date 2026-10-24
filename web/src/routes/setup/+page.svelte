<script lang="ts">
	import '$lib/forms.css';
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { formatCode } from '$lib/codes';
	import { brand, logoSrc, pageTitle } from '$lib/brand.svelte';
	import { step as slide } from '$lib/motion';

	/*
	 * The short setup: the setup code from the logs, then a Keystone account.
	 * Everything else — domain, Active Directory, passkeys, the emergency
	 * account, recovery codes — is set later in Instellingen.
	 */
	type Step = 'loading' | 'unlock' | 'account';

	let current = $state<Step>('loading');
	let dir = $state(1);
	let busy = $state(false);
	let error = $state('');

	let unlockCode = $state('');
	let username = $state('');
	let password = $state('');
	let repeat = $state('');
	let userInput = $state<HTMLInputElement | null>(null);

	const MIN_PASSWORD = 12;
	const mismatch = $derived(repeat.length > 0 && repeat !== password);

	async function go(next: Step) {
		dir = next === 'account' ? 1 : -1;
		error = '';
		current = next;
		if (next === 'account') {
			await tick();
			userInput?.focus();
		}
	}

	onMount(async () => {
		try {
			const s = await api.get<{ needed: boolean; step: string }>('/setup/status');
			if (!s.needed) return void goto('/', { replaceState: true });
			go(s.step === 'unlock' ? 'unlock' : 'account');
		} catch (e) {
			current = 'unlock';
			fail(e);
		}
	});

	function fail(e: unknown) {
		if (!(e instanceof ApiError)) {
			error = 'Er ging iets mis. Probeer het opnieuw.';
			return;
		}
		if (e.code === 'setup_done') return void goto('/', { replaceState: true });
		if (e.code === 'setup_locked') {
			go('unlock');
			error = 'Je setup-sessie is verlopen. Vul de setupcode opnieuw in.';
			return;
		}
		error = e.message;
	}

	async function run(fn: () => Promise<unknown>): Promise<boolean> {
		if (busy) return false;
		busy = true;
		error = '';
		try {
			await fn();
			return true;
		} catch (e) {
			fail(e);
			return false;
		} finally {
			busy = false;
		}
	}

	async function unlock(e: SubmitEvent) {
		e.preventDefault();
		if (await run(() => api.send('POST', '/setup/unlock', { code: unlockCode }))) {
			unlockCode = '';
			go('account');
		}
	}

	async function createAccount(e: SubmitEvent) {
		e.preventDefault();
		if (password.length < MIN_PASSWORD) return void (error = `Kies een wachtwoord van minstens ${MIN_PASSWORD} tekens.`);
		if (password !== repeat) return void (error = 'De wachtwoorden zijn niet gelijk.');
		if (await run(() => api.send('POST', '/setup/account', { username, password }))) {
			location.assign('/');
		}
	}
</script>

<svelte:head>
	<title>{pageTitle('Installatie')}</title>
</svelte:head>

{#snippet busyLabel(text: string)}
	{#if busy}<span class="spinner" aria-hidden="true"></span>{/if}{text}
{/snippet}

{#snippet errorBox()}
	{#if error}<p class="error" role="alert">{error}</p>{/if}
{/snippet}

<main class="auth-page">
	<div class="auth-card">
		<div class="auth-brand"><img class="mark" src={logoSrc()} alt="" width="64" height="64" /><span>{brand.name}</span></div>

		<div class="auth-stage">
			{#key current}
				<section in:slide={{ dir }} out:slide={{ dir, out: true }}>
					{#if current === 'loading'}
						<p class="visually-hidden" role="status">Laden…</p>
					{:else if current === 'unlock'}
						<h1>Welkom</h1>
						<p class="lede">
							Vul de setupcode in. Je vindt hem met
							<code>docker compose logs core</code>.
						</p>
						<form class="form" onsubmit={unlock} novalidate>
							<!-- svelte-ignore a11y_autofocus -->
							<input
								class="input code"
								aria-label="Setupcode"
								autocomplete="one-time-code"
								autocapitalize="characters"
								spellcheck="false"
								placeholder="XXXX-XXXX-XXXX-XXXX"
								autofocus
								value={unlockCode}
								oninput={(e) => (unlockCode = e.currentTarget.value = formatCode(e.currentTarget.value))}
							/>
							{@render errorBox()}
							<button class="btn btn-primary block" disabled={busy || unlockCode.length < 19}>
								{@render busyLabel('Verder')}
							</button>
						</form>
					{:else}
						<h1>Maak je account</h1>
						<p class="lede">Hiermee log je in bij Keystone. De rest stel je later in.</p>
						<form class="form" onsubmit={createAccount} novalidate>
							<input
								class="input"
								aria-label="Gebruikersnaam"
								placeholder="Gebruikersnaam"
								autocomplete="username"
								autocapitalize="none"
								spellcheck="false"
								bind:this={userInput}
								bind:value={username}
							/>
							<input
								class="input"
								type="password"
								aria-label="Wachtwoord"
								placeholder="Wachtwoord (minstens {MIN_PASSWORD} tekens)"
								autocomplete="new-password"
								bind:value={password}
							/>
							<input
								class="input"
								type="password"
								aria-label="Herhaal wachtwoord"
								placeholder="Herhaal wachtwoord"
								autocomplete="new-password"
								aria-invalid={mismatch ? 'true' : undefined}
								bind:value={repeat}
							/>
							{@render errorBox()}
							<button
								class="btn btn-primary block"
								disabled={busy || !username.trim() || password.length < MIN_PASSWORD || password !== repeat}
							>
								{@render busyLabel('Account maken')}
							</button>
						</form>
					{/if}
				</section>
			{/key}
		</div>
	</div>
</main>

<style>
	.lede code {
		font-size: 0.8125rem;
		padding: 1px 6px;
		border-radius: var(--radius-xs);
		background: var(--color-bg);
		color: var(--color-text);
		white-space: nowrap;
	}
</style>
