<script lang="ts">
	import { safeHref, type App } from '$lib/apps';

	let { app, highlighted = false }: { app: App; highlighted?: boolean } = $props();

	const href = $derived(safeHref(app.url));
	// Icons are only ever data:image URLs rendered via <img src> — never inline markup.
	const icon = $derived(app.icon?.startsWith('data:image/') ? app.icon : null);
	const initial = $derived(Array.from(app.name.trim())[0]?.toUpperCase() ?? '?');
</script>

<a class="tile" class:highlighted class:disabled={!href} href={href ?? undefined} aria-disabled={!href || undefined} title={app.name}>
	<span class="icon" style:--tile-color={app.color}>
		{#if icon}
			<img src={icon} alt="" draggable="false" decoding="async" />
		{:else}
			<span class="letter" aria-hidden="true">{initial}</span>
		{/if}
	</span>
	<span class="name">{app.name}</span>
</a>

<style>
	.tile {
		--size: clamp(60px, 15vw, 76px);
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 9px;
		padding: 10px 4px 8px;
		border-radius: var(--radius-l);
		color: var(--text);
		text-decoration: none;
		outline: none;
		-webkit-touch-callout: none;
		user-select: none;
	}
	.icon {
		position: relative;
		display: grid;
		place-items: center;
		width: var(--size);
		height: var(--size);
		border-radius: 22.5%;
		background: var(--tile-color, #8e8e93);
		/* Light catching the top edge of the squircle. */
		background-image: linear-gradient(180deg, rgba(255, 255, 255, 0.18), rgba(255, 255, 255, 0) 55%);
		box-shadow: var(--tile-shadow);
		overflow: hidden;
		transition:
			transform var(--spring-dur) var(--spring-bounce),
			box-shadow 300ms ease;
		will-change: transform;
	}
	@supports (corner-shape: squircle) {
		.icon {
			border-radius: 30%;
			corner-shape: squircle;
		}
	}
	.icon::after {
		content: '';
		position: absolute;
		inset: 0;
		border-radius: inherit;
		box-shadow: inset 0 0 0 0.5px rgba(0, 0, 0, 0.08);
		pointer-events: none;
	}
	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.letter {
		color: #fff;
		font-size: calc(var(--size) * 0.42);
		font-weight: 600;
		letter-spacing: -0.02em;
		text-shadow: 0 1px 2px rgba(0, 0, 0, 0.12);
	}
	.name {
		max-width: 100%;
		font-size: 0.8125rem;
		font-weight: 500;
		letter-spacing: 0;
		line-height: 1.25;
		text-align: center;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	@media (hover: hover) {
		.tile:hover .icon {
			transform: translateY(-2px) scale(1.04);
			box-shadow: var(--tile-shadow-hover);
		}
	}
	/* Instant feedback on press; the spring brings it back on release. */
	.tile:active .icon {
		transform: scale(0.92);
		transition: transform 90ms ease-out;
	}
	.tile:focus-visible .icon,
	.highlighted .icon {
		box-shadow:
			0 0 0 3px var(--bg-base),
			0 0 0 6px var(--focus-ring),
			var(--tile-shadow);
	}
	.disabled {
		cursor: not-allowed;
		opacity: 0.55;
	}
	@media (prefers-reduced-motion: reduce) {
		.tile:hover .icon,
		.tile:active .icon {
			transform: none;
		}
	}
</style>
