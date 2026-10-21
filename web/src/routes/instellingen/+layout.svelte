<script lang="ts">
	// Everything under /instellingen is its own lazily loaded chunk: none of
	// this (or forms.css/settings.css) ships with the home page.
	import '$lib/forms.css';
	import '$lib/settings.css';
	import { page } from '$app/state';
	import { fade } from 'svelte/transition';

	let { children } = $props();

	const sections = [
		{ href: '/instellingen', label: 'Algemeen', icon: 'M10 2.8v2M10 15.2v2M2.8 10h2M15.2 10h2M4.9 4.9l1.4 1.4M13.7 13.7l1.4 1.4M4.9 15.1l1.4-1.4M13.7 6.3l1.4-1.4M10 6.8a3.2 3.2 0 1 0 0 6.4 3.2 3.2 0 0 0 0-6.4z' },
		{ href: '/instellingen/ad', label: 'Active Directory', icon: 'M3 5.5h14v4H3zM3 10.5h14v4H3zM6 7.5h.01M6 12.5h.01' },
		{ href: '/instellingen/apps', label: 'Apps', icon: 'M3.5 3.5h5v5h-5zM11.5 3.5h5v5h-5zM3.5 11.5h5v5h-5zM11.5 11.5h5v5h-5z' },
		{ href: '/instellingen/beveiliging', label: 'Beveiliging', icon: 'M10 2.5 16 5v4.6c0 3.7-2.6 6.6-6 7.9-3.4-1.3-6-4.2-6-7.9V5z' },
		{ href: '/instellingen/apparaten', label: 'Apparaten', icon: 'M2.5 4.5h11v8h-11zM6 15.5h4M15 7.5h3v8.5h-3z' },
		{ href: '/instellingen/activiteit', label: 'Activiteit', icon: 'M10 3.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13zM10 6.5V10l2.5 1.5' }
	];

	const path = $derived(page.url.pathname.replace(/\/$/, '') || '/');
	const current = $derived(sections.find((s) => s.href === path) ?? sections[0]);

	// Phone: keep the active segment visible in the scrolling strip.
	let nav = $state<HTMLElement | null>(null);
	$effect(() => {
		current;
		nav?.querySelector('[aria-current="page"]')?.scrollIntoView({ inline: 'nearest', block: 'nearest' });
	});
</script>

<svelte:head>
	<title>{current.label} · Instellingen · Keystone</title>
</svelte:head>

<div class="page">
	<header class="top">
		<a class="back" href="/">
			<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M12.5 4 6.5 10l6 6" /></svg>
			Apps
		</a>
		<h1>Instellingen</h1>
	</header>

	<div class="sheet glass">
		<nav aria-label="Instellingen" bind:this={nav}>
			<ul>
				{#each sections as s (s.href)}
					<li>
						<a href={s.href} aria-current={s === current ? 'page' : undefined}>
							<svg viewBox="0 0 20 20" aria-hidden="true"><path d={s.icon} /></svg>
							<span>{s.label}</span>
						</a>
					</li>
				{/each}
			</ul>
		</nav>
		<main class="pane">
			{#key current.href}
				<div in:fade={{ duration: 160 }}>{@render children()}</div>
			{/key}
		</main>
	</div>
</div>

<style>
	.page {
		width: min(1040px, 100%);
		margin: 0 auto;
		padding: max(20px, env(safe-area-inset-top)) max(16px, env(safe-area-inset-right)) 40px
			max(16px, env(safe-area-inset-left));
	}
	.top {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 4px;
		margin: 0 0 16px;
	}
	.back {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		margin-left: -6px;
		padding: 4px 8px 4px 2px;
		border-radius: 8px;
		font-size: 1rem;
		font-weight: 500;
		text-decoration: none;
	}
	.back svg {
		width: 20px;
		height: 20px;
		fill: none;
		stroke: currentColor;
		stroke-width: 2.2;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	h1 {
		margin: 0;
		font-size: clamp(1.75rem, 1.2rem + 2.4vw, 2.25rem);
		font-weight: 700;
		line-height: 1.08;
		letter-spacing: -0.025em;
	}

	.sheet {
		display: grid;
		grid-template-columns: 232px minmax(0, 1fr);
		border-radius: var(--radius-l);
		background: var(--glass);
		box-shadow: var(--popover-shadow);
		min-height: min(720px, calc(100dvh - 140px));
	}
	nav {
		padding: 14px 10px;
		border-right: 1px solid var(--hairline);
	}
	nav ul {
		position: sticky;
		top: 14px;
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	nav a {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px 10px;
		border-radius: 10px;
		color: var(--text);
		font-size: 0.9375rem;
		font-weight: 500;
		text-decoration: none;
		transition: background-color 120ms ease;
	}
	nav a:hover {
		background: var(--hover);
	}
	nav a:active {
		background: var(--press);
	}
	nav a[aria-current='page'] {
		background: var(--accent);
		color: var(--accent-text);
	}
	nav svg {
		flex: none;
		width: 20px;
		height: 20px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.6;
		stroke-linecap: round;
		stroke-linejoin: round;
		opacity: 0.85;
	}
	.pane {
		min-width: 0;
		padding: 26px 32px 32px;
	}

	/* Phone and small tablets: the section list becomes a segmented strip on top. */
	@media (max-width: 759px) {
		.sheet {
			display: block;
			min-height: 0;
			background: none;
			border: 0;
			box-shadow: none;
			-webkit-backdrop-filter: none;
			backdrop-filter: none;
		}
		nav {
			position: sticky;
			top: max(8px, env(safe-area-inset-top));
			z-index: 10;
			margin: 0 -4px 18px;
			padding: 4px;
			border: 1px solid var(--glass-border);
			border-radius: 14px;
			background: var(--menu-bg);
			-webkit-backdrop-filter: var(--glass-blur);
			backdrop-filter: var(--glass-blur);
			box-shadow: var(--glass-shadow);
			overflow-x: auto;
			scrollbar-width: none;
			-webkit-overflow-scrolling: touch;
		}
		nav::-webkit-scrollbar {
			display: none;
		}
		nav ul {
			position: static;
			flex-direction: row;
			gap: 2px;
			width: max-content;
		}
		nav a {
			padding: 7px 12px;
			border-radius: 10px;
			font-size: 0.875rem;
			white-space: nowrap;
		}
		nav svg {
			display: none;
		}
		.pane {
			padding: 0;
		}
	}
</style>
