<script lang="ts">
	import type { Snippet } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, type Me } from '$lib/api';
	import { firstName } from '$lib/greeting';
	import { NAV, isActive, searchHref } from '$lib/nav';
	import { toast } from '$lib/toast.svelte';
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

{#snippet menu(collapsed: boolean, onpick?: () => void)}
	<nav aria-label="Hoofdmenu">
		{#each NAV as g, gi (gi)}
			{#if g.title}
				{#if collapsed}
					<div class="nav-rule" role="presentation"></div>
				{:else}
					<p class="nav-group" id="nav-g-{gi}">{g.title}</p>
				{/if}
			{/if}
			<ul aria-labelledby={g.title && !collapsed ? `nav-g-${gi}` : undefined}>
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
							<svg viewBox="0 0 20 20" aria-hidden="true"><path d={item.icon} /></svg>
							{#if !collapsed}<span>{item.label}</span>{/if}
						</a>
					</li>
				{/each}
			</ul>
		{/each}
	</nav>
{/snippet}

<div class="shell" class:icons={iconsOnly}>
	<header class="topbar">
		<button type="button" class="topbar-btn menu-btn" aria-label="Menu openen" aria-expanded={drawerOpen} onclick={openDrawer}>
			<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3.5 5.5h13M3.5 10h13M3.5 14.5h13" /></svg>
		</button>
		<a class="brand" href="/" aria-label="Keystone, mijn apps">
			<span class="brand-mark" aria-hidden="true">◆</span>
			<span class="brand-name">Keystone</span>
		</a>
		<div class="bar">
			<div class="search">
				<TopbarSearch bind:value={query} onsubmit={search} />
			</div>
			{#if me}
				<UserMenu name={me.display_name?.trim() || firstName(me)} username={me.username} onlogout={logout} />
			{:else}
				<div class="user-ghost" aria-hidden="true"><span class="ghost"></span></div>
			{/if}
		</div>
	</header>

	<aside class="rail">
		<div class="rail-inner">
			{@render menu(iconsOnly)}
			<button
				type="button"
				class="nav-foot"
				aria-label={iconsOnly ? 'Menu uitklappen' : 'Menu inklappen'}
				aria-pressed={iconsOnly}
				onclick={toggleNav}
			>
				<svg viewBox="0 0 20 20" aria-hidden="true">
					<path d={iconsOnly ? 'M8 5l5 5-5 5' : 'M12 5l-5 5 5 5'} />
				</svg>
				{#if !iconsOnly}<span>Inklappen</span>{/if}
			</button>
		</div>
	</aside>

	<main class="content">
		<div class="inner">{@render children()}</div>
	</main>
</div>

<dialog class="drawer" bind:this={drawer} aria-label="Menu" onclose={() => (drawerOpen = false)} onclick={(e) => e.target === drawer && closeDrawer()}>
	<div class="drawer-panel">
		<div class="drawer-head">
			<span class="brand">
				<span class="brand-mark" aria-hidden="true">◆</span>
				<span class="brand-name">Keystone</span>
			</span>
			<button type="button" class="topbar-btn" aria-label="Menu sluiten" onclick={closeDrawer}>
				<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 5l10 10M15 5L5 15" /></svg>
			</button>
		</div>
		{@render menu(false, closeDrawer)}
	</div>
</dialog>

<style>
	.shell {
		--menu-w: var(--nav-w);
		display: grid;
		grid-template-columns: var(--menu-w) minmax(0, 1fr);
		grid-template-rows: 64px auto;
		min-height: 100dvh;
		background: var(--color-bg);
	}
	.shell.icons {
		--menu-w: var(--nav-w-icons);
	}

	/* ── Bar: 64px on the canvas; the logo block is painted in the menu colour
	   at the menu's width, so the white column runs from the top. ── */
	.topbar {
		grid-column: 1 / -1;
		display: grid;
		grid-template-columns: var(--menu-w) minmax(0, 1fr);
		height: 64px;
		background: var(--topbar-bg);
		color: var(--topbar-ink);
		position: sticky;
		top: 0;
		z-index: var(--z-sticky);
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 10px;
		height: 64px;
		padding: 0 24px;
		background: var(--nav-bg);
		color: var(--nav-ink);
		text-decoration: none;
		overflow: hidden;
	}
	.brand:hover {
		color: var(--nav-ink);
	}
	.brand-mark {
		display: grid;
		place-items: center;
		flex: none;
		width: 32px;
		height: 32px;
		border-radius: var(--radius-md);
		background: var(--primary);
		color: #fff;
		font-size: 0.9375rem;
		line-height: 1;
	}
	.brand-name {
		font-size: 1.125rem;
		font-weight: 800;
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
	.bar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 16px;
		min-width: 0;
		padding: 0 20px 0 24px;
	}
	.search {
		min-width: 0;
	}
	.user-ghost {
		display: grid;
		place-items: center;
		width: 48px;
		height: 48px;
	}
	.user-ghost .ghost {
		width: 36px;
		height: 36px;
		border-radius: 50%;
	}

	.topbar-btn {
		display: grid;
		place-items: center;
		flex: none;
		width: 40px;
		height: 40px;
		border: 0;
		border-radius: var(--radius-md);
		background: transparent;
		color: var(--topbar-ink-2);
		cursor: pointer;
		transition:
			background-color var(--dur-2) var(--ease),
			color var(--dur-2) var(--ease);
	}
	.topbar-btn:hover {
		background: var(--topbar-hover);
		color: var(--topbar-ink);
	}
	.topbar-btn svg {
		width: 20px;
		height: 20px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
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

	/* ── Menu: a white column with no rule on its edge ── */
	.rail {
		background: var(--nav-bg);
		color: var(--nav-ink);
	}
	.rail-inner {
		display: flex;
		flex-direction: column;
		justify-content: space-between;
		position: sticky;
		top: 64px;
		height: calc(100dvh - 64px);
		overflow-y: auto;
		overflow-x: hidden;
		padding: 4px 0 0;
	}
	nav ul {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.nav-group {
		margin: 0;
		padding: 16px 26px 4px;
		font-size: 0.75rem;
		font-weight: 600;
		line-height: 1.45;
		color: var(--nav-ink-2);
	}
	.nav-rule {
		height: 1px;
		margin: 12px 20px;
		background: var(--c-border2);
	}
	/* Pills inset 12px; active = solid accent pill, white ink, 600, soft glow. */
	.nav-link {
		display: flex;
		align-items: center;
		gap: 12px;
		margin: 2px 12px;
		padding: 8px 14px;
		border-radius: var(--radius-md);
		color: var(--nav-ink-2);
		font-size: 0.875rem;
		font-weight: 500;
		line-height: 1.45;
		text-decoration: none;
		white-space: nowrap;
		transition:
			background-color var(--dur-2) var(--ease),
			color var(--dur-2) var(--ease),
			box-shadow var(--dur-2) var(--ease);
	}
	.nav-link:hover {
		background: var(--nav-hover);
		color: var(--nav-ink);
	}
	.nav-link.active,
	.nav-link.active:hover {
		background: var(--nav-active);
		color: var(--nav-active-ink);
		font-weight: 600;
		box-shadow: var(--nav-active-shadow);
	}
	.nav-link.icon-only {
		justify-content: center;
		gap: 0;
		height: 40px;
		margin: 3px 16px;
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
		gap: 12px;
		margin: 8px 12px 12px;
		padding: 8px 14px;
		border: 0;
		border-radius: var(--radius-md);
		background: transparent;
		color: var(--nav-ink-2);
		font-size: 0.8125rem;
		font-weight: 500;
		cursor: pointer;
		transition:
			background-color var(--dur-2) var(--ease),
			color var(--dur-2) var(--ease);
	}
	.nav-foot:hover {
		background: var(--nav-hover);
		color: var(--nav-ink);
	}
	.icons .nav-foot {
		justify-content: center;
		margin: 8px 16px 12px;
		padding: 0;
		height: 40px;
	}

	.content {
		min-width: 0;
		padding: 8px 32px 64px;
	}
	.inner {
		max-width: 1120px;
	}

	/* ── Phone and small tablet: the menu becomes a drawer from the bar ── */
	@media (max-width: 767px) {
		.shell,
		.shell.icons {
			grid-template-columns: minmax(0, 1fr);
		}
		.topbar {
			display: flex;
			align-items: center;
			gap: 4px;
			padding: 0 8px 0 max(8px, env(safe-area-inset-left));
		}
		.menu-btn {
			display: grid;
		}
		.topbar > .brand {
			display: none;
		}
		.bar {
			flex: 1;
			gap: 6px;
			padding: 0;
		}
		.search {
			flex: 1;
		}
		.search :global(.search) {
			width: 100%;
		}
		.rail {
			display: none;
		}
		.content {
			padding: 8px max(16px, env(safe-area-inset-right)) 56px max(16px, env(safe-area-inset-left));
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
		animation: overlay-in var(--dur-3) var(--ease-out) both;
	}
	.drawer-panel {
		height: 100%;
		overflow-y: auto;
		padding: 0 0 max(16px, env(safe-area-inset-bottom));
		background: var(--nav-bg);
		box-shadow: var(--shadow-lg);
	}
	.drawer[open] .drawer-panel {
		animation: drawer-in var(--dur-4) var(--ease-out) both;
	}
	.drawer-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		height: 64px;
		padding: 0 8px 0 0;
	}
	.drawer-head .brand {
		padding: 0 20px;
	}
	@keyframes overlay-in {
		from {
			opacity: 0;
		}
	}
	@keyframes drawer-in {
		from {
			transform: translateX(-16px);
			opacity: 0;
		}
	}
</style>
