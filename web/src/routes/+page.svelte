<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { filterApps, safeHref, type App } from '$lib/apps';
	import { firstName, greeting } from '$lib/greeting';
	import AppShell from '$lib/components/AppShell.svelte';
	import AppGrid from '$lib/components/AppGrid.svelte';

	let now = $state(new Date());
	let me = $state<Me | null>(null);
	let apps = $state<App[] | null>(null);
	let loadError = $state<string | null>(null);
	let query = $state('');

	const matches = $derived(apps ? filterApps(apps, query) : []);
	const hello = $derived(greeting(now));
	const today = $derived(
		new Intl.DateTimeFormat('nl-NL', { weekday: 'long', day: 'numeric', month: 'long' }).format(now)
	);

	async function load() {
		loadError = null;
		try {
			// Both requests in parallel: one round-trip of latency for the whole page.
			const [m, a] = await Promise.all([api.get<Me>('/me'), api.get<{ apps: App[] }>('/apps')]);
			me = m;
			apps = a.apps;
		} catch (e) {
			if (e instanceof ApiError && e.status === 401) return; // api.ts is redirecting
			// Signed in with a recovery code: a new passkey comes first (on /login).
			if (e instanceof ApiError && e.code === 'must_register_passkey') return void location.assign('/login');
			loadError = e instanceof ApiError ? e.message : 'Er ging iets mis bij het laden.';
		}
	}

	onMount(() => {
		// Arriving from the search on another page: /?q=…
		query = new URLSearchParams(location.search).get('q') ?? '';
		load();
		// Keep the greeting right if the page stays open across a boundary.
		const t = setInterval(() => (now = new Date()), 60_000);
		return () => clearInterval(t);
	});

	function openFirst() {
		if (!query.trim()) return;
		const href = matches[0] && safeHref(matches[0].url);
		if (href) location.assign(href);
	}
</script>

<svelte:head>
	<title>Keystone</title>
</svelte:head>

<AppShell {me} bind:query onsearch={openFirst}>
	{#if me?.emergency}
		<div class="notice warn banner" role="status">
			<svg viewBox="0 0 20 20" aria-hidden="true">
				<path d="M10 3.2 17.6 16.5H2.4z" />
				<path d="M10 8.3v3.6M10 14.2v.01" />
			</svg>
			<p>
				<strong>Noodtoegang actief.</strong>
				Je bent aangemeld met het noodaccount omdat AD niet bereikbaar was. Herstel de AD-verbinding zo snel mogelijk.
			</p>
		</div>
	{/if}

	<header class="page-head">
		<h1>{hello}{#if me}<span class="name">, {firstName(me)}</span>{/if}</h1>
		<p class="sub">{today}</p>
	</header>

	{#if loadError}
		<div class="surface state">
			<p>{loadError}</p>
			<button type="button" class="btn btn-secondary" onclick={load}>Opnieuw proberen</button>
		</div>
	{:else if apps === null}
		<ul class="skeleton" aria-hidden="true">
			{#each { length: 8 } as _}
				<li><span class="ghost icon"></span><span class="ghost line"></span><span class="ghost line short"></span></li>
			{/each}
		</ul>
		<p class="visually-hidden" role="status">Apps worden geladen…</p>
	{:else if apps.length === 0}
		<div class="surface state">
			<p class="state-title">Nog geen apps</p>
			<p>Voeg je eerste app toe; hij verschijnt meteen hier.</p>
			<a class="btn btn-primary" href="/instellingen/apps">Naar Instellingen › Apps</a>
		</div>
	{:else}
		<AppGrid apps={matches} highlightFirst={query.trim() !== '' && matches.length > 0} />
		{#if matches.length === 0}
			<div class="surface state">
				<p>Geen app gevonden voor “{query.trim()}”.</p>
			</div>
		{/if}
	{/if}
</AppShell>

<style>
	/* Page header on the canvas: 1.5rem/800 title, muted subtitle, no rule. */
	.page-head {
		margin: 12px 0 24px;
	}
	h1 {
		margin: 0;
		font-size: 1.5rem;
		font-weight: 800;
		line-height: 1.3;
		letter-spacing: -0.02em;
		text-wrap: balance;
	}
	.name {
		animation: appear var(--dur-3) var(--ease-out) both;
	}
	.sub {
		margin: 2px 0 0;
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}
	.sub::first-letter {
		text-transform: uppercase;
	}

	.banner {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		margin: 12px 0 0;
		font-size: 0.875rem;
	}
	.banner p {
		margin: 0;
	}
	.banner strong {
		color: var(--color-warning-text);
		font-weight: 700;
	}
	.banner svg {
		flex: none;
		width: 18px;
		height: 18px;
		margin-top: 2px;
		fill: none;
		stroke: var(--color-warning-text);
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
	}

	/* Ghost tiles appear only if loading takes longer than a blink. */
	.skeleton {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(196px, 1fr));
		gap: 16px;
		animation: appear var(--dur-3) var(--ease) 180ms both;
	}
	.skeleton li {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 16px;
		border: 1px solid var(--c-border2);
		border-radius: var(--radius-md);
		background: var(--c-bg-card);
		box-shadow: var(--shadow-sm);
	}
	.skeleton .icon {
		width: 44px;
		height: 44px;
		margin-bottom: 6px;
		border-radius: var(--radius-md);
	}
	.skeleton .line {
		width: 60%;
		height: 10px;
	}
	.skeleton .short {
		width: 40%;
	}
	@media (max-width: 520px) {
		.skeleton {
			grid-template-columns: repeat(2, minmax(0, 1fr));
			gap: 12px;
		}
	}

	.state {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 10px;
		padding: 20px;
		color: var(--color-text-muted);
	}
	.state p {
		margin: 0;
	}
	.state .btn {
		margin-top: 6px;
	}
	.state-title {
		color: var(--color-text);
		font-size: 1rem;
		font-weight: 700;
	}
	.surface + :global(.grid),
	:global(.grid) + .surface {
		margin-top: 16px;
	}

	@keyframes appear {
		from {
			opacity: 0;
		}
	}
</style>
