<script lang="ts">
	import type { Snippet } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, type Me } from '$lib/api';
	import { firstName } from '$lib/greeting';
	import { NAV, isActive, searchHref } from '$lib/nav';
	import { toast } from '$lib/toast.svelte';
	import { brand, logoSrc } from '$lib/brand.svelte';
	import TopbarSearch from './TopbarSearch.svelte';
	import UserMenu from './UserMenu.svelte';

	let {
		me,
		query = $bindable(''),
		onsearch,
		children
	}: {
		me: Me | null;
		/** The top-bar search text; home binds it to filter the grid. */
		query?: string;
		/** Enter in the search. Default: go to home with the query. */
		onsearch?: () => void;
		children: Snippet;
	} = $props();

	const NAV_KEY = 'keystone.nav';
	let iconsOnly = $state(readNav());
	let drawer: HTMLDialogElement;
	let drawerOpen = $state(false);

	function readNav(): boolean {
		try {
			return localStorage.getItem(NAV_KEY) === 'icons';
		} catch {
			return false;
		}
	}
	function toggleNav() {
		iconsOnly = !iconsOnly;
		try {
			localStorage.setItem(NAV_KEY, iconsOnly ? 'icons' : 'full');
		} catch {
			/* not remembered */
		}
	}

	function openDrawer() {
		drawerOpen = true;
		drawer.showModal();
	}
	function closeDrawer() {
		if (drawer.open) drawer.close();
	}

	// Widening past the phone breakpoint closes the drawer.
	$effect(() => {
		const mq = matchMedia('(min-width: 768px)');
		const onchange = () => mq.matches && closeDrawer();
		mq.addEventListener('change', onchange);
		return () => mq.removeEventListener('change', onchange);
	});

	// A navigation closes the phone drawer.
	$effect(() => {
		page.url.pathname;
		if (drawer?.open) drawer.close();
	});

	function search() {
		if (onsearch) onsearch();
		else goto(searchHref(query));
	}

	async function logout() {
		try {
			await api.send('POST', '/auth/logout');
			location.assign('/login');
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Uitloggen is niet gelukt.', 'error');
		}
	}

	const path = $derived(page.url.pathname);
</script>

{#snippet menu(collapsed: boolean, id: string, onpick?: () => void)}
	<nav aria-label="Hoofdmenu">
		{#each NAV as g, gi (gi)}
			{#if g.title}
				{#if collapsed}
					<div class="nav-rule" role="presentation"></div>
				{:else}
					<p class="nav-group" id="nav-{id}-g-{gi}">{g.title}</p>
				{/if}
			{/if}
			<ul aria-labelledby={g.title && !collapsed ? `nav-${id}-g-${gi}` : undefined}>
				{#each g.items as item (item.href)}
					{@const active = isActive(path, item.href)}
					<li>
						<a
							class="nav-link"
							class:active
							class:icon-only={collapsed}
							href={item.href}
							aria-current={active ? 'page' : undefined}
							aria-label={collapsed ? item.label : undefined}
							title={collapsed ? item.label : undefined}
							onclick={onpick}
						>
							<span class="glyph" aria-hidden="true">
								<svg viewBox="0 0 20 20"><path d={item.icon} /></svg>
							</span>
							{#if !collapsed}<span>{item.label}</span>{/if}
						</a>
					</li>
				{/each}
			</ul>
		{/each}
	</nav>
{/snippet}

<div class="shell" class:icons={iconsOnly}>
	<aside class="rail">
		<div class="rail-inner">
			<a class="brand" href="/" aria-label="{brand.name}, mijn apps">
				<img class="brand-mark" src={logoSrc()} alt="" width="28" height="28" />
				<span class="brand-name">{brand.name}</span>
			</a>
			{@render menu(iconsOnly, 'rail')}
			<button
				type="button"
				class="nav-foot"
				aria-label={iconsOnly ? 'Menu uitklappen' : 'Menu inklappen'}
				aria-pressed={iconsOnly}
				onclick={toggleNav}
			>
				<svg viewBox="0 0 20 20" aria-hidden="true">
					<path d="M3.5 4.5h13v11h-13zM8 4.5v11" />
				</svg>
				{#if !iconsOnly}<span>Zijbalk verbergen</span>{/if}
			</button>
		</div>
	</aside>

	<div class="main">
		<header class="topbar">
			<button type="button" class="topbar-btn menu-btn" aria-label="Menu openen" aria-expanded={drawerOpen} onclick={openDrawer}>
				<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3.5 5.5h13M3.5 10h13M3.5 14.5h13" /></svg>
			</button>
			<div class="search">
				<TopbarSearch bind:value={query} onsubmit={search} />
			</div>
			{#if me}
				<UserMenu name={me.display_name?.trim() || firstName(me)} username={me.username} onlogout={logout} />
			{:else}
				<div class="user-ghost" aria-hidden="true"><span class="ghost"></span></div>
			{/if}
		</header>

		<main class="content">
			<div class="inner">{@render children()}</div>
		</main>
	</div>
</div>

<dialog class="drawer" bind:this={drawer} aria-label="Menu" onclose={() => (drawerOpen = false)} onclick={(e) => e.target === drawer && closeDrawer()}>
	<div class="drawer-panel">
		<div class="drawer-head">
			<span class="brand">
				<img class="brand-mark" src={logoSrc()} alt="" width="28" height="28" />
				<span class="brand-name">{brand.name}</span>
			</span>
			<button type="button" class="topbar-btn" aria-label="Menu sluiten" onclick={closeDrawer}>
				<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 5l10 10M15 5L5 15" /></svg>
			</button>
		</div>
		{@render menu(false, 'drawer', closeDrawer)}
	</div>
</dialog>

<style>
	/* A white sidebar the full height on the left and a white bar on top,
	   each edged by a hairline; the content sits on the canvas. */
	.shell {
		--menu-w: var(--nav-w);
		--bar-h: 52px;
		display: grid;
		grid-template-columns: var(--menu-w) minmax(0, 1fr);
		min-height: 100dvh;
		background: var(--color-bg);
	}
	.shell.icons {
		--menu-w: var(--nav-w-icons);
	}
	.main {
		min-width: 0;
		display: flex;
		flex-direction: column;
	}

	/* ── Toolbar ── */
	.topbar {
		position: sticky;
		top: 0;
		z-index: var(--z-sticky);
		display: flex;
		align-items: center;
		gap: 12px;
		height: var(--bar-h);
		padding: 0 16px 0 24px;
		background: var(--topbar-bg);
		border-bottom: 1px solid var(--topbar-stroke);
		color: var(--topbar-ink);
	}
	.search {
		flex: 1;
		min-width: 0;
		display: flex;
		justify-content: center;
	}
	.user-ghost {
		display: grid;
		place-items: center;
		width: 40px;
		height: 40px;
	}
	.user-ghost .ghost {
		width: 30px;
		height: 30px;
		border-radius: 50%;
	}

	.topbar-btn {
		display: grid;
		place-items: center;
		flex: none;
		width: 36px;
		height: 36px;
		border: 0;
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--topbar-ink-2);
		cursor: pointer;
		transition: background-color var(--dur-2) var(--ease);
	}
	.topbar-btn:hover {
		background: var(--topbar-hover);
		color: var(--topbar-ink);
	}
	.topbar-btn:active {
		background: var(--press);
	}
	.topbar-btn svg {
		width: 20px;
		height: 20px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.7;
		stroke-linecap: round;
	}
	@media (pointer: coarse) {
		.topbar-btn {
			width: 44px;
			height: 44px;
		}
	}
	.menu-btn {
		display: none;
	}

	/* ── Sidebar ── */
	.rail {
		background: var(--nav-bg);
		border-right: 1px solid var(--c-border);
		color: var(--nav-ink);
	}
	.rail-inner {
		display: flex;
		flex-direction: column;
		position: sticky;
		top: 0;
		height: 100dvh;
		overflow-y: auto;
		overflow-x: hidden;
		padding: 0 0 8px;
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 10px;
		height: var(--bar-h);
		padding: 0 18px;
		color: var(--nav-ink);
		text-decoration: none;
		overflow: hidden;
		flex: none;
	}
	.brand:hover {
		text-decoration: none;
	}
	.brand-mark {
		flex: none;
		width: 28px;
		height: 28px;
		object-fit: contain;
	}
	.brand-name {
		font-family: var(--font-display);
		font-size: 1.0625rem;
		font-weight: 600;
		letter-spacing: -0.02em;
		white-space: nowrap;
	}
	.icons .brand {
		justify-content: center;
		padding: 0;
	}
	.icons .brand-name {
		display: none;
	}
	nav {
		flex: 1;
	}
	nav ul {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.nav-group {
		margin: 0;
		padding: 18px 20px 4px;
		font-size: 0.75rem;
		font-weight: 600;
		line-height: 1.4;
		color: var(--nav-ink-2);
	}
	.nav-rule {
		height: 1px;
		margin: 12px 18px;
		background: var(--c-border);
	}
	/* Rows inset 10px; the selected row is tinted with the accent. */
	.nav-link {
		display: flex;
		align-items: center;
		gap: 9px;
		margin: 1px 10px;
		padding: 0 10px;
		height: 30px;
		border-radius: var(--radius-sm);
		color: var(--nav-ink);
		font-size: 0.875rem;
		font-weight: 400;
		text-decoration: none;
		white-space: nowrap;
		transition: background-color var(--dur-1) var(--ease);
	}
	.nav-link:hover {
		background: var(--nav-hover);
		text-decoration: none;
	}
	.nav-link:active {
		background: var(--press);
	}
	/* A plain line icon in the row's ink, quieter than the label. */
	.glyph {
		display: grid;
		place-items: center;
		flex: none;
		width: 20px;
		height: 20px;
		color: var(--nav-ink-2);
	}
	.nav-link.active,
	.nav-link.active:hover {
		background: var(--nav-active);
		color: var(--nav-active-ink);
		font-weight: 500;
	}
	.nav-link.active .glyph {
		color: inherit;
	}
	.nav-link.icon-only {
		justify-content: center;
		gap: 0;
		height: 36px;
		margin: 2px 12px;
		padding: 0;
	}
	.nav-link svg,
	.nav-foot svg {
		flex: none;
		width: 18px;
		height: 18px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.7;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.nav-foot {
		display: flex;
		align-items: center;
		gap: 9px;
		margin: 8px 10px 4px;
		padding: 0 10px;
		height: 32px;
		border: 0;
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--nav-ink-2);
		font-size: 0.8125rem;
		cursor: pointer;
		transition: background-color var(--dur-1) var(--ease);
	}
	.nav-foot:hover {
		background: var(--nav-hover);
		color: var(--nav-ink);
	}
	.icons .nav-foot {
		justify-content: center;
		margin: 8px 12px 4px;
		padding: 0;
	}

	.content {
		min-width: 0;
		padding: 28px 40px 72px;
	}
	.inner {
		max-width: 1080px;
		margin: 0 auto;
	}

	/* ── Phone and small tablet: the sidebar becomes a sheet from the bar ── */
	@media (max-width: 767px) {
		.shell,
		.shell.icons {
			grid-template-columns: minmax(0, 1fr);
		}
		.topbar {
			gap: 6px;
			padding: 0 max(10px, env(safe-area-inset-right)) 0 max(6px, env(safe-area-inset-left));
		}
		.menu-btn {
			display: grid;
		}
		.search :global(.search) {
			width: 100%;
		}
		.rail {
			display: none;
		}
		.content {
			padding: 20px max(16px, env(safe-area-inset-right)) 56px max(16px, env(safe-area-inset-left));
		}
	}

	.drawer {
		margin: 0;
		padding: 0;
		border: 0;
		width: min(84vw, 300px);
		max-width: none;
		height: 100dvh;
		max-height: none;
		background: transparent;
		color: inherit;
	}
	.drawer::backdrop {
		background: var(--scrim);
	}
	.drawer[open]::backdrop {
		animation: overlay-in var(--dur-3) var(--ease) both;
	}
	.drawer-panel {
		height: 100%;
		overflow-y: auto;
		padding: 0 0 max(16px, env(safe-area-inset-bottom));
		background: #fff;
		border-right: 1px solid var(--c-border);
	}
	/* Enters from the left and leaves the same way. */
	.drawer[open] .drawer-panel {
		animation: drawer-in var(--dur-4) var(--ease-out) both;
	}
	.drawer-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		height: var(--bar-h);
		padding: 0 6px 0 0;
	}
	@keyframes overlay-in {
		from {
			opacity: 0;
		}
	}
	@keyframes drawer-in {
		from {
			transform: translateX(-100%);
		}
	}
</style>
