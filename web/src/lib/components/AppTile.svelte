<script lang="ts">
	import { hostOf, tileHref, type App } from '$lib/apps';
	import { openVaultApp } from '$lib/vaultTile';

	let { app, highlighted = false }: { app: App; highlighted?: boolean } = $props();

	const href = $derived(tileHref(app));

	function click(e: MouseEvent) {
		if (!href || e.button !== 0) return;
		// A plain link is recorded for Recently used; SSO apps record themselves.
		if (app.protocol === 'link')
			fetch(`/api/apps/${app.id}/opened`, { method: 'POST', keepalive: true, credentials: 'same-origin' }).catch(() => {});
		// Vault apps: the sheet with the stored details, or the sign-in for you.
		if (app.protocol !== 'vault' || e.metaKey || e.ctrlKey || e.shiftKey) return;
		e.preventDefault();
		openVaultApp(app, href);
	}
	// Icons are only ever data:image URLs rendered via <img src> — never inline markup.
	const icon = $derived(app.icon?.startsWith('data:image/') ? app.icon : null);
	const initial = $derived(Array.from(app.name.trim())[0]?.toUpperCase() ?? '?');
	const host = $derived(hostOf(app.url) || app.url);
</script>

<!-- An app card, as in a launcher: the app's logo large on a white card, its name below. -->
<a
	class="tile"
	class:highlighted
	class:disabled={!href}
	href={href ?? undefined}
	aria-disabled={!href || undefined}
	title={host}
	onclick={click}
>
	<span class="logo" class:image={!!icon} style:--tile-color={app.color}>
		{#if icon}
			<img src={icon} alt="" draggable="false" decoding="async" />
		{:else}
			<span aria-hidden="true">{initial}</span>
		{/if}
	</span>
	<span class="name">{app.name}</span>
	{#if app.protocol === 'vault'}
		<span class="badge" title="Signs in with details from the vault">
			<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M6 9V7a4 4 0 0 1 8 0v2M5 9h10v7H5z" /></svg>
			<span class="visually-hidden">Vault</span>
		</span>
	{/if}
	<span class="visually-hidden">{host}</span>
</a>

<style>
	.tile {
		position: relative;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 12px;
		height: 148px;
		padding: 18px 12px 14px;
		border: 1px solid var(--c-border);
		border-radius: 14px;
		background: var(--c-bg-card);
		color: var(--color-text);
		text-decoration: none;
		-webkit-touch-callout: none;
		transition:
			border-color var(--dur-2) var(--ease),
			transform var(--dur-2) var(--ease),
			box-shadow var(--dur-2) var(--ease);
	}
	.tile:hover {
		text-decoration: none;
		border-color: color-mix(in srgb, var(--primary) 40%, var(--c-border));
		box-shadow: 0 6px 18px rgba(0, 0, 0, 0.06);
		transform: translateY(-1px);
	}
	.tile:active {
		transform: scale(0.985);
		transition-duration: var(--dur-1);
	}
	.logo {
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
		font-weight: 600;
		letter-spacing: -0.02em;
		overflow: hidden;
	}
	.logo.image {
		background: transparent;
	}
	img {
		width: 100%;
		height: 100%;
		object-fit: contain;
	}
	.name {
		max-width: 100%;
		font-size: 0.9375rem;
		font-weight: 500;
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
		top: 10px;
		right: 10px;
		display: grid;
		place-items: center;
		width: 24px;
		height: 24px;
		border-radius: 50%;
		background: var(--c-bg-sunken);
		color: var(--color-text-muted);
	}
	.badge svg {
		width: 13px;
		height: 13px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	/* The first search match (Enter opens it) gets the focus-ring look. */
	.highlighted,
	.tile:focus-visible {
		outline: none;
		border-color: var(--primary);
		box-shadow: 0 0 0 3px var(--focus-ring);
	}
	.disabled {
		opacity: 0.4;
		pointer-events: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.tile:hover,
		.tile:active {
			transform: none;
		}
	}
	@media (max-width: 520px) {
		.tile {
			height: 124px;
			gap: 10px;
		}
		.logo {
			width: 52px;
			height: 52px;
			font-size: 1.4rem;
		}
	}
</style>
