<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { filterApps, safeHref, type App } from '$lib/apps';
	import { firstName, greeting } from '$lib/greeting';
	import { toast } from '$lib/toast.svelte';
	import GlassSearch from '$lib/components/GlassSearch.svelte';
	import AppGrid from '$lib/components/AppGrid.svelte';
	import Avatar from '$lib/components/Avatar.svelte';

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
			const [m, a] = await Promise.all([api.get<Me>('/me'), api.get<App[]>('/apps')]);
			me = m;
			apps = a;
		} catch (e) {
			if (e instanceof ApiError && e.status === 401) return; // api.ts is redirecting
			loadError = e instanceof ApiError ? e.message : 'Er ging iets mis bij het laden.';
		}
	}

	onMount(() => {
		load();
		// Keep the greeting right if the page stays open across a boundary.
		const t = setInterval(() => (now = new Date()), 60_000);
		return () => clearInterval(t);
	});

	function openFirst() {
		const href = matches[0] && safeHref(matches[0].url);
		if (href) location.assign(href);
	}

	async function logout() {
		try {
			await api.send('POST', '/auth/logout');
			location.assign('/login');
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Uitloggen is niet gelukt.', 'error');
		}
	}
</script>

<svelte:head>
	<title>Keystone</title>
</svelte:head>

<main>
	{#if me?.emergency}
		<div class="banner" role="status">
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

	<header>
		<div class="hello">
			<p class="date">{today}</p>
			<h1>
				{hello}{#if me}<span class="name">, {firstName(me)}</span>{/if}
			</h1>
		</div>
		{#if me}
			<Avatar name={me.display_name?.trim() || firstName(me)} username={me.username} onlogout={logout} />
		{:else}
			<div class="avatar-skeleton" aria-hidden="true"></div>
		{/if}
	</header>

	<div class="search">
		<GlassSearch bind:value={query} onsubmit={openFirst} />
	</div>

	{#if loadError}
		<div class="state">
			<p>{loadError}</p>
			<button type="button" class="pill" onclick={load}>Opnieuw proberen</button>
		</div>
	{:else if apps === null}
		<ul class="skeleton" aria-hidden="true">
			{#each { length: 10 } as _}
				<li><span></span><i></i></li>
			{/each}
		</ul>
		<p class="visually-hidden" role="status">Apps worden geladen…</p>
	{:else if apps.length === 0}
		<div class="state empty">
			<div class="empty-icon" aria-hidden="true">
				<svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14" /></svg>
			</div>
			<p class="empty-title">Nog geen apps — voeg je eerste app toe</p>
			<a class="pill primary" href="/instellingen/apps">Naar Instellingen › Apps</a>
		</div>
	{:else}
		<AppGrid apps={matches} highlightFirst={query.trim() !== '' && matches.length > 0} />
		{#if matches.length === 0}
			<div class="state">
				<p>Geen app gevonden voor “{query.trim()}”.</p>
			</div>
		{/if}
	{/if}
</main>

<style>
	main {
		width: min(820px, 100%);
		margin: 0 auto;
		padding: max(28px, env(safe-area-inset-top)) max(16px, env(safe-area-inset-right)) 72px
			max(16px, env(safe-area-inset-left));
	}
	@media (min-width: 600px) {
		main {
			padding-top: clamp(40px, 9vh, 96px);
			padding-inline: 32px;
		}
	}

	header {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: 16px;
	}
	.date {
		margin: 0 0 2px;
		font-size: 0.8125rem;
		font-weight: 600;
		letter-spacing: 0.02em;
		text-transform: uppercase;
		color: var(--text-2);
	}
	h1 {
		margin: 0;
		font-size: clamp(1.75rem, 1.2rem + 2.4vw, 2.5rem);
		font-weight: 700;
		line-height: 1.08;
		letter-spacing: -0.025em;
		font-optical-sizing: auto;
		text-wrap: balance;
	}
	.name {
		animation: appear 420ms ease both;
	}
	.avatar-skeleton {
		width: 40px;
		height: 40px;
		flex: none;
		border-radius: 50%;
		background: var(--skeleton);
	}

	.search {
		margin: 20px 0 28px;
	}
	@media (min-width: 600px) {
		.search {
			margin: 24px 0 36px;
		}
	}

	.banner {
		display: flex;
		gap: 12px;
		align-items: flex-start;
		margin: 0 0 24px;
		padding: 12px 16px;
		border-radius: var(--radius-m);
		background: var(--warn-bg);
		border: 1px solid var(--warn-border);
		color: var(--text);
		font-size: 0.9375rem;
		line-height: 1.4;
		-webkit-backdrop-filter: var(--glass-blur);
		backdrop-filter: var(--glass-blur);
	}
	.banner p {
		margin: 0;
	}
	.banner strong {
		color: var(--warn-text);
		font-weight: 600;
	}
	.banner svg {
		flex: none;
		width: 20px;
		height: 20px;
		margin-top: 1px;
		fill: none;
		stroke: var(--warn-text);
		stroke-width: 1.7;
		stroke-linecap: round;
		stroke-linejoin: round;
	}

	/* Placeholder tiles appear only if loading takes longer than a blink. */
	.skeleton {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 14px 6px;
		animation: appear 300ms ease 180ms both;
	}
	.skeleton li {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 12px;
		padding: 10px 4px 8px;
	}
	.skeleton span {
		width: clamp(60px, 15vw, 76px);
		aspect-ratio: 1;
		border-radius: 22.5%;
		background: var(--skeleton);
	}
	.skeleton i {
		width: 56%;
		height: 9px;
		border-radius: 5px;
		background: var(--skeleton);
	}
	@media (min-width: 600px) {
		.skeleton {
			grid-template-columns: repeat(4, minmax(0, 1fr));
			gap: 18px 10px;
		}
	}
	@media (min-width: 900px) {
		.skeleton {
			grid-template-columns: repeat(5, minmax(0, 1fr));
			gap: 22px 12px;
		}
	}

	.state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 14px;
		padding: 32px 16px;
		text-align: center;
		color: var(--text-2);
		animation: appear 300ms ease both;
	}
	.state p {
		margin: 0;
	}
	.empty {
		padding-top: 48px;
	}
	.empty-icon {
		display: grid;
		place-items: center;
		width: 76px;
		height: 76px;
		border-radius: 22.5%;
		border: 1.5px dashed color-mix(in srgb, var(--text-2) 45%, transparent);
		background: var(--glass);
	}
	.empty-icon svg {
		width: 28px;
		height: 28px;
		fill: none;
		stroke: var(--text-2);
		stroke-width: 1.8;
		stroke-linecap: round;
	}
	.empty-title {
		font-size: 1.0625rem;
		font-weight: 600;
		color: var(--text);
		letter-spacing: -0.01em;
	}
	.pill {
		display: inline-flex;
		align-items: center;
		height: 38px;
		padding: 0 18px;
		border-radius: 999px;
		border: 1px solid var(--glass-border);
		background: var(--glass);
		color: var(--text);
		font-size: 0.9375rem;
		font-weight: 500;
		text-decoration: none;
		cursor: pointer;
		transition: transform var(--spring-dur) var(--spring-bounce);
	}
	.pill.primary {
		background: var(--accent);
		border-color: transparent;
		color: var(--accent-text);
	}
	.pill:active {
		transform: scale(0.96);
		transition: transform 90ms ease-out;
	}

	@keyframes appear {
		from {
			opacity: 0;
		}
	}
</style>
