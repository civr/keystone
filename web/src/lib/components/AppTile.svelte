<script lang="ts">
	import { hostOf, tileHref, type App } from '$lib/apps';
	import { openVaultApp } from '$lib/vaultTile';

	let { app, highlighted = false }: { app: App; highlighted?: boolean } = $props();

	const href = $derived(tileHref(app));

	function click(e: MouseEvent) {
		// Vault apps: copy the stored password first, then open the app.
		if (app.protocol !== 'vault' || !href || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
		e.preventDefault();
		openVaultApp(app, href);
	}
	// Icons are only ever data:image URLs rendered via <img src> — never inline markup.
	const icon = $derived(app.icon?.startsWith('data:image/') ? app.icon : null);
	const initial = $derived(Array.from(app.name.trim())[0]?.toUpperCase() ?? '?');
	const host = $derived(hostOf(app.url) || app.url);
</script>

<!-- An app icon, as on the Home Screen: a rounded square in the app's colour, the name beneath. -->
<a
	class="tile"
	class:highlighted
	class:disabled={!href}
	href={href ?? undefined}
	aria-disabled={!href || undefined}
	title={host}
	onclick={click}
>
	<span class="icon" class:image={!!icon} style:--tile-color={app.color}>
		{#if icon}
			<img src={icon} alt="" draggable="false" decoding="async" />
		{:else}
			<span aria-hidden="true">{initial}</span>
		{/if}
		{#if app.protocol === 'vault'}
			<span class="badge" title="Opens with the vault password on your clipboard">
				<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M6 9V7a4 4 0 0 1 8 0v2M5 9h10v7H5z" /></svg>
				<span class="visually-hidden">Vault</span>
			</span>
		{/if}
	</span>
	<span class="name">{app.name}</span>
	<span class="visually-hidden">{host}</span>
</a>

<style>
	.tile {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		padding: 10px 4px 6px;
		border-radius: var(--radius-lg);
		color: var(--color-text);
		text-decoration: none;
		-webkit-touch-callout: none;
		outline-offset: 2px;
	}
	.tile:hover {
		text-decoration: none;
	}
	.icon {
		position: relative;
		display: grid;
		place-items: center;
		width: 64px;
		height: 64px;
		border-radius: var(--radius-icon);
		/* Without its own icon an app gets a flat tile in its colour. */
		background: var(--tile-color, #8e8e93);
		color: #fff;
		font-family: var(--font-display);
		font-size: 1.75rem;
		font-weight: 500;
		letter-spacing: -0.02em;
		transition: transform var(--dur-1) var(--ease);
	}
	.icon.image {
		background: #fff;
		box-shadow: inset 0 0 0 1px var(--c-border);
	}
	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		border-radius: inherit;
	}
	.name {
		max-width: 100%;
		font-size: 0.8125rem;
		line-height: 1.25;
		text-align: center;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
		overflow-wrap: anywhere;
	}
	.badge {
		position: absolute;
		right: -6px;
		bottom: -6px;
		display: grid;
		place-items: center;
		width: 26px;
		height: 26px;
		border-radius: 50%;
		background: #fff;
		color: var(--color-text-muted);
		box-shadow: 0 0 0 1px var(--c-border);
	}
	.badge svg {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	/* Pressed: the icon darkens a touch. */
	.tile:active .icon {
		filter: brightness(0.85);
	}
	/* The first search match (Enter opens it) gets the focus-ring look. */
	.highlighted .icon,
	.tile:focus-visible .icon {
		box-shadow:
			0 0 0 3px var(--color-bg),
			0 0 0 6px var(--primary);
	}
	.tile:focus-visible {
		outline: none;
	}
	.highlighted .name {
		color: var(--primary);
		font-weight: 500;
	}
	.disabled {
		opacity: 0.4;
		pointer-events: none;
	}
	@media (max-width: 520px) {
		.icon {
			width: 58px;
			height: 58px;
			font-size: 1.5rem;
		}
	}
</style>
