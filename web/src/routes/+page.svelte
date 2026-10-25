<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { filterApps, safeHref, type App } from '$lib/apps';
	import AppShell from '$lib/components/AppShell.svelte';
	import { brand } from '$lib/brand.svelte';
	import AppGrid from '$lib/components/AppGrid.svelte';

	let me = $state<Me | null>(null);
	let apps = $state<App[] | null>(null);
	let recentIds = $state<string[]>([]);
	let loadError = $state<string | null>(null);
	let query = $state('');

	const matches = $derived(apps ? filterApps(apps, query) : []);

	async function load() {
		loadError = null;
		try {
			// Both requests in parallel: one round-trip of latency for the whole page.
			const [m, a, r] = await Promise.all([
				api.get<Me>('/me'),
				api.get<{ apps: App[] }>('/apps?mine=1'),
				api.get<{ ids: string[] }>('/apps/recent').catch(() => ({ ids: [] }))
			]);
			recentIds = r.ids;
			me = m;
			apps = a.apps;
		} catch (e) {
			if (e instanceof ApiError && e.status === 401) return; // api.ts is redirecting
			// Signed in with a recovery code: a new passkey comes first (on /login).
			if (e instanceof ApiError && e.code === 'must_register_passkey') return void location.assign('/login');
			loadError = e instanceof ApiError ? e.message : 'Something went wrong while loading.';
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
				<strong>Emergency access is on.</strong>
				You signed in with the emergency account because Active Directory could not be reached. Restore the connection soon.
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
				<strong>No passkey yet.</strong>
				You sign in with only your password. Open Keystone over https and add a passkey under
				<a class="link" href="/account/security">Sign-in &amp; devices</a>.
			</p>
		</div>
	{/if}

	<div class="head">
		<h1>My apps</h1>
	</div>

	{#if loadError}
		<div class="surface state">
			<p>{loadError}</p>
			<button type="button" class="btn btn-secondary" onclick={load}>Try again</button>
		</div>
	{:else if apps === null}
		<ul class="skeleton" aria-hidden="true">
			{#each { length: 8 } as _}
				<li><span class="ghost icon"></span><span class="ghost line"></span><span class="ghost line short"></span></li>
			{/each}
		</ul>
		<p class="visually-hidden" role="status">Loading apps…</p>
	{:else if apps.length === 0}
		<div class="surface state">
			<p class="state-title">No apps yet</p>
			{#if me?.admin}
				<p>Add your first app; it shows up here right away.</p>
				<a class="btn btn-primary" href="/admin/apps">Add an app</a>
			{:else}
				<p>An admin has not given you any apps yet.</p>
			{/if}
		</div>
	{:else}
		{@const recent = query.trim() ? [] : recentIds.map((id) => apps!.find((a) => a.id === id)).filter((a): a is App => !!a)}
		{#if recent.length}
			<h2 class="section">Recently used</h2>
			<AppGrid apps={recent} />
			<h2 class="section">All apps</h2>
		{/if}
		<AppGrid apps={matches} highlightFirst={query.trim() !== '' && matches.length > 0} />
		{#if matches.length === 0}
			<div class="surface state">
				<p>No app matches “{query.trim()}”.</p>
			</div>
		{/if}
	{/if}
</AppShell>

<style>
	.head h1 {
		margin: 4px 0 18px;
		font-size: 1.75rem;
	}
	.section {
		margin: 22px 0 12px;
		font-size: 1rem;
		font-weight: 600;
		color: var(--color-text-muted);
	}
	.section:first-of-type {
		margin-top: 0;
	}
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
		grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
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
