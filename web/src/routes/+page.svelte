<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { filterApps, safeHref, type App } from '$lib/apps';
	import AppShell from '$lib/components/AppShell.svelte';
	import { brand } from '$lib/brand.svelte';
	import AppGrid from '$lib/components/AppGrid.svelte';

	let me = $state<Me | null>(null);
	let apps = $state<App[] | null>(null);
	let loadError = $state<string | null>(null);
	let query = $state('');

	const matches = $derived(apps ? filterApps(apps, query) : []);

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
	});

	function openFirst() {
		if (!query.trim()) return;
		const href = matches[0] && safeHref(matches[0].url);
		if (href) location.assign(href);
	}
</script>

<svelte:head>
	<title>{brand.name}</title>
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
	{#if me?.passkey_missing}
		<div class="notice warn banner" role="status">
			<svg viewBox="0 0 20 20" aria-hidden="true">
				<path d="M10 3.2 17.6 16.5H2.4z" />
				<path d="M10 8.3v3.6M10 14.2v.01" />
			</svg>
			<p>
				<strong>Nog geen passkey.</strong>
				Je logt nu in met alleen je AD-wachtwoord. Open Keystone via https en voeg een passkey toe onder
				<a class="link" href="/instellingen/apparaten">Instellingen</a>.
			</p>
		</div>
	{/if}

	<h1 class="visually-hidden">Mijn apps</h1>

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
	.banner {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		margin: 12px 0 0;
		font-size: 0.875rem;
		margin-bottom: 4px;
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
		grid-template-columns: repeat(auto-fill, minmax(108px, 1fr));
		gap: 22px 12px;
		animation: appear var(--dur-3) var(--ease) 180ms both;
	}
	.skeleton li {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 10px;
		padding: 10px 4px 6px;
	}
	.skeleton .icon {
		width: 64px;
		height: 64px;
		border-radius: var(--radius-icon);
	}
	.skeleton .line {
		width: 56px;
		height: 9px;
		border-radius: 5px;
	}
	.skeleton .short {
		display: none;
	}
	@media (max-width: 520px) {
		.skeleton {
			grid-template-columns: repeat(4, minmax(0, 1fr));
			gap: 18px 4px;
		}
		.skeleton .icon {
			width: 64px;
			height: 64px;
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
		font-size: 1.0625rem;
		font-weight: 600;
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
