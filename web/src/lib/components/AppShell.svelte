<script lang="ts">
	import type { Snippet } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, type Me } from '$lib/api';
	import { firstName } from '$lib/greeting';
	import { ADMIN_NAV, USER_NAV, navFor, isActive, searchHref } from '$lib/nav';
	import { toast } from '$lib/toast.svelte';
	import { brand, logoSrc } from '$lib/brand.svelte';
	import TopbarSearch from './TopbarSearch.svelte';
	import UserMenu from './UserMenu.svelte';

	let {
		me,
		query = $bindable(''),
		onsearch,
		full = false,
		portal = 'user',
		children
	}: {
		/** Which portal: the user portal (light menu) or /admin (dark menu). */
		portal?: 'user' | 'admin';
		/** The page fills the window edge to edge (no padding, no max width). */
		full?: boolean;
		me: Me | null;
		/** The top-bar search text; home binds it to filter the grid. */
		query?: string;
		/** Enter in the search. Default: go to home with the query. */
		onsearch?: () => void;
		children: Snippet;
	} = $props();

	const groups = $derived(
		navFor(portal === 'admin' ? ADMIN_NAV : USER_NAV, { permissions: me?.permissions ?? [], owner: me?.role === 'owner' })
	);

	// Admin groups fold away; the one with the current page stays open.
	const FOLD_KEY = 'keystone.admin.folded';
	let folded = $state<string[]>(readFolded());
	function readFolded(): string[] {
		try {
			return JSON.parse(localStorage.getItem(FOLD_KEY) ?? '[]');
		} catch {
			return [];
		}
	}
	function toggleGroup(title: string) {
		folded = folded.includes(title) ? folded.filter((t) => t !== title) : [...folded, title];
		try {
			localStorage.setItem(FOLD_KEY, JSON.stringify(folded));
		} catch {
			/* not remembered */
		}
	}

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
			toast(e instanceof ApiError ? e.message : 'Signing out failed.', 'error');
		}
	}

	const path = $derived(page.url.pathname);
</script>

{#snippet menu(collapsed: boolean, id: string, onpick?: () => void)}
	<nav aria-label="Main menu">
		{#each groups as g, gi (gi)}
			{@const open = !g.title || collapsed || portal !== 'admin' || !folded.includes(g.title) || g.items.some((i) => isActive(path, i.href))}
			{#if g.title}
				{#if collapsed}
					<div class="nav-rule" role="presentation"></div>
				{:else if portal === 'admin'}
					<button type="button" class="nav-group fold" id="nav-{id}-g-{gi}" aria-expanded={open} onclick={() => toggleGroup(g.title!)}>
						{g.title}
						<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M7 8l3 3 3-3" /></svg>
					</button>
				{:else}
					<p class="nav-group" id="nav-{id}-g-{gi}">{g.title}</p>
				{/if}
			{/if}
			<ul aria-labelledby={g.title && !collapsed ? `nav-${id}-g-${gi}` : undefined} hidden={!open}>
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
							<span class="glyph" style:--tint={item.tint} aria-hidden="true">
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

<div class="shell {portal}" class:icons={iconsOnly}>
	<aside class="rail">
		<div class="rail-inner">
			<a class="brand" href="/" aria-label="{brand.name}, my apps">
				<img class="brand-mark" src={logoSrc()} alt="" width="40" height="40" />
				<span class="brand-name">{brand.name}</span>
			</a>
			{@render menu(iconsOnly, 'rail')}
			<button
				type="button"
				class="nav-foot"
				aria-label={iconsOnly ? 'Expand menu' : 'Collapse menu'}
				aria-pressed={iconsOnly}
				onclick={toggleNav}
			>
				<svg viewBox="0 0 20 20" aria-hidden="true">
					<path d="M3.5 4.5h13v11h-13zM8 4.5v11" />
				</svg>
				{#if !iconsOnly}<span>Hide sidebar</span>{/if}
			</button>
		</div>
	</aside>

	<div class="main">
		<header class="topbar">
			<button type="button" class="topbar-btn menu-btn" aria-label="Open menu" aria-expanded={drawerOpen} onclick={openDrawer}>
				<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3.5 5.5h13M3.5 10h13M3.5 14.5h13" /></svg>
			</button>
			<div class="search">
				<TopbarSearch bind:value={query} onsubmit={search} />
			</div>
			{#if me?.admin}
				<a class="btn btn-secondary btn-sm switch-portal" href={portal === 'admin' ? '/' : '/admin'}>
					{portal === 'admin' ? 'User portal' : 'Admin'}
				</a>
			{/if}
			{#if me}
				<UserMenu name={me.display_name?.trim() || firstName(me)} username={me.username} settingsHref="/account" onlogout={logout} />
			{:else}
				<div class="user-ghost" aria-hidden="true"><span class="ghost"></span></div>
			{/if}
		</header>

		<main class="content" class:full>
			<div class="inner">{@render children()}</div>
		</main>
	</div>
</div>

<dialog class="drawer" bind:this={drawer} aria-label="Menu" onclose={() => (drawerOpen = false)} onclick={(e) => e.target === drawer && closeDrawer()}>
	<div class="drawer-panel">
		<div class="drawer-head">
			<span class="brand">
				<img class="brand-mark" src={logoSrc()} alt="" width="40" height="40" />
				<span class="brand-name">{brand.name}</span>
			</span>
			<button type="button" class="topbar-btn" aria-label="Close menu" onclick={closeDrawer}>
				<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 5l10 10M15 5L5 15" /></svg>
			</button>
		</div>
		{@render menu(false, 'drawer', closeDrawer)}
	</div>
</dialog>

<style>
	/* A window, as a Mac app: the sidebar and the bar stay put and only the
	   content pane scrolls, and only when it must. The page never scrolls. */
	.shell {
		--menu-w: var(--nav-w);
		--bar-h: 52px;
		display: grid;
		grid-template-columns: var(--menu-w) minmax(0, 1fr);
		height: 100dvh;
		overflow: hidden;
		background: var(--color-bg);
	}
	.shell.icons {
		--menu-w: var(--nav-w-icons);
	}
	.main {
		min-width: 0;
		min-height: 0;
		display: flex;
		flex-direction: column;
	}

	/* ── Toolbar ── */
	.topbar {
		flex: none;
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

	/* The user portal has a light menu (Okta-like launcher); the admin
	   portal keeps the dark one. */
	.shell.user {
		--nav-bg: var(--c-bg-card);
		--nav-ink: var(--color-text);
		--nav-ink-2: var(--color-text-muted);
		--nav-hover: var(--c-hover);
		--nav-active: var(--c-selected-mid);
		--nav-active-ink: var(--primary-dark);
		--nav-rule: var(--c-border);
	}
	.shell.user .rail {
		border-right: 1px solid var(--c-border);
	}
	.shell.user .nav-link.active,
	.shell.user .nav-link.active:hover {
		box-shadow: none;
		font-weight: 600;
	}
	/* The admin menu is longer: denser rows so it fits the window. */
	.shell.admin .nav-link {
		height: 34px;
		margin: 1px 12px;
		font-size: 0.9375rem;
	}
	.shell.admin .glyph {
		width: 22px;
		height: 22px;
		border-radius: 6px;
	}
	.shell.admin .nav-link .glyph svg {
		width: 14px;
		height: 14px;
	}
	.shell.admin .nav-group {
		padding-top: 14px;
	}
	.shell.admin .brand {
		height: 64px;
	}
	.switch-portal {
		flex: none;
	}
	.nav-group.fold {
		display: flex;
		align-items: center;
		justify-content: space-between;
		width: 100%;
		border: 0;
		background: none;
		text-align: left;
		cursor: pointer;
		font: inherit;
		font-size: 0.8125rem;
		font-weight: 600;
		color: var(--nav-ink-2);
	}
	.nav-group.fold svg {
		width: 16px;
		height: 16px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		transition: transform var(--dur-2) var(--ease);
	}
	.nav-group.fold[aria-expanded='false'] svg {
		transform: rotate(-90deg);
	}
	ul[hidden] {
		display: none;
	}

	/* ── Sidebar ── */
	.rail {
		background: var(--nav-bg);
		color: var(--nav-ink);
	}
	.rail-inner {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow-y: auto;
		overflow-x: hidden;
		padding: 0 0 8px;
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 12px;
		height: 84px;
		padding: 0 22px;
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
		width: 40px;
		height: 40px;
		object-fit: contain;
	}
	.brand-name {
		font-family: var(--font-display);
		font-size: 1.25rem;
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
		padding: 22px 24px 6px;
		font-size: 0.8125rem;
		font-weight: 600;
		line-height: 1.4;
		color: var(--nav-ink-2);
	}
	.nav-rule {
		height: 1px;
		margin: 12px 18px;
		background: var(--nav-rule);
	}
	/* Rows inset 10px; the selected row is tinted with the accent. */
	.nav-link {
		display: flex;
		align-items: center;
		gap: 9px;
		margin: 2px 12px;
		padding: 0 10px;
		height: 40px;
		border-radius: 8px;
		color: var(--nav-ink);
		font-size: 1.0625rem;
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
	/* Each item has a small coloured tile with a white symbol, as in System Settings. */
	.glyph {
		display: grid;
		place-items: center;
		flex: none;
		width: 26px;
		height: 26px;
		border-radius: 7px;
		background: var(--tint);
		color: #fff;
	}
	.nav-link .glyph svg {
		width: 16px;
		height: 16px;
		stroke-width: 2;
	}
	.nav-link.active,
	.nav-link.active:hover {
		background: var(--nav-active);
		box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.16);
		color: var(--nav-active-ink);
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
		flex: 1;
		min-width: 0;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		padding: 20px 40px 20px;
	}
	.inner {
		max-width: 1080px;
		margin: 0 auto;
	}
	.content.full {
		padding: 0;
		overflow: hidden;
	}
	.content.full .inner {
		max-width: none;
		height: 100%;
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
		background: var(--nav-bg);
		color: var(--nav-ink);
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
