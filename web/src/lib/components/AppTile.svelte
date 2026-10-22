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

<!-- Nimbus browseable tile (.vdc-card): white, resting on --shadow-sm, lifts 2px on hover. -->
<a class="tile" class:highlighted class:disabled={!href} href={href ?? undefined} aria-disabled={!href || undefined} onclick={click}>
	<span class="icon" style:--tile-color={app.color}>
		{#if icon}
			<img src={icon} alt="" draggable="false" decoding="async" />
		{:else}
			<span aria-hidden="true">{initial}</span>
		{/if}
	</span>
	<span class="name">{app.name}</span>
	<span class="host">{host}</span>
	{#if app.protocol === 'vault'}<span class="badge" title="Wachtwoord uit de kluis wordt gekopieerd">Kluis</span>{/if}
</a>

<style>
	.badge {
		position: absolute;
		top: 12px;
		right: 12px;
		padding: 1px 8px;
		border-radius: var(--radius-pill, 9999px);
		background: var(--c-bg-section);
		color: var(--color-text-muted);
		font-size: 0.6875rem;
		font-weight: 600;
	}
	.tile {
		position: relative;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 2px;
		height: 100%;
		padding: 16px;
		border: 1px solid var(--c-border2);
		border-radius: var(--radius-md);
		background: var(--c-bg-card);
		box-shadow: var(--shadow-sm);
		color: var(--color-text);
		text-decoration: none;
		transition:
			border-color var(--dur-3) var(--ease),
			box-shadow var(--dur-3) var(--ease-out),
			transform var(--dur-3) var(--ease-out);
	}
	.icon {
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		margin-bottom: 12px;
		border-radius: var(--radius-md);
		/* A tint of the app's colour, its initial in a dark step of the same hue. */
		background: color-mix(in srgb, var(--tile-color, #64748b) 14%, white);
		color: color-mix(in oklch, var(--tile-color, #64748b) 72%, black);
		font-size: 1.125rem;
		font-weight: 800;
		letter-spacing: -0.02em;
		overflow: hidden;
	}
	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.name {
		max-width: 100%;
		font-size: 0.9375rem;
		font-weight: 700;
		line-height: 1.4;
		letter-spacing: -0.01em;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		transition: color var(--dur-2) var(--ease);
	}
	.host {
		max-width: 100%;
		font-size: 0.8125rem;
		font-weight: 500;
		line-height: 1.5;
		color: var(--color-text-faint);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	@media (hover: hover) {
		.tile:hover {
			border-color: var(--c-border);
			box-shadow: var(--shadow-hover);
			transform: translateY(-2px);
		}
	}
	.tile:hover .name,
	.tile:focus-visible .name,
	.highlighted .name {
		color: var(--primary);
	}
	.tile:focus-visible,
	.highlighted {
		border-color: var(--primary);
	}
	.highlighted {
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--primary) 16%, transparent), var(--shadow-sm);
	}
	.tile:active {
		transform: scale(0.98);
		transition-duration: var(--dur-1);
	}
	.disabled {
		cursor: not-allowed;
		opacity: 0.55;
	}
</style>
