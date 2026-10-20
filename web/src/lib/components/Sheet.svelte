<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		title,
		children
	}: { open?: boolean; title: string; children: Snippet } = $props();

	let dialog: HTMLDialogElement;

	// Native <dialog>: focus trap, Esc and inert background for free.
	$effect(() => {
		if (open && !dialog.open) dialog.showModal();
		else if (!open && dialog.open) dialog.close();
	});

	function onBackdrop(e: MouseEvent) {
		if (e.target === dialog) open = false;
	}
</script>

<dialog bind:this={dialog} aria-label={title} onclose={() => (open = false)} onclick={onBackdrop}>
	<div class="sheet glass">
		<div class="grabber" aria-hidden="true"></div>
		<header>
			<h2>{title}</h2>
			<button type="button" class="close" aria-label="Sluiten" onclick={() => (open = false)}>
				<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" /></svg>
			</button>
		</header>
		<div class="body">{@render children()}</div>
	</div>
</dialog>

<style>
	dialog {
		padding: 0;
		border: 0;
		background: transparent;
		color: inherit;
		max-width: none;
		max-height: none;
		width: min(560px, 100vw - 32px);
		overflow: visible;
	}
	dialog::backdrop {
		background: var(--scrim);
		-webkit-backdrop-filter: blur(2px);
		backdrop-filter: blur(2px);
	}
	.sheet {
		border-radius: var(--radius-l);
		background: var(--glass-strong);
		box-shadow: var(--popover-shadow);
		max-height: min(80dvh, 720px);
		display: flex;
		flex-direction: column;
	}
	dialog[open] .sheet {
		animation: rise var(--spring-dur) var(--spring);
	}
	dialog[open]::backdrop {
		animation: dim 240ms ease;
	}
	.grabber {
		display: none;
	}
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 16px 12px 4px 22px;
	}
	h2 {
		margin: 0;
		font-size: 1.25rem;
		font-weight: 700;
		letter-spacing: -0.015em;
	}
	.body {
		padding: 8px 22px 22px;
		overflow: auto;
	}
	.close {
		display: grid;
		place-items: center;
		width: 32px;
		height: 32px;
		border: 0;
		border-radius: 50%;
		background: var(--hover);
		color: var(--text-2);
		cursor: pointer;
	}
	.close svg {
		width: 12px;
		height: 12px;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
	}
	/* Phone: a bottom sheet that rises from the edge it belongs to. */
	@media (max-width: 599px) {
		dialog {
			width: 100vw;
			margin: auto 0 0;
		}
		.sheet {
			border-radius: var(--radius-l) var(--radius-l) 0 0;
			padding-bottom: env(safe-area-inset-bottom);
			max-height: 88dvh;
		}
		.grabber {
			display: block;
			width: 36px;
			height: 5px;
			margin: 8px auto 0;
			border-radius: 3px;
			background: var(--hairline);
		}
		dialog[open] .sheet {
			animation-name: slide;
		}
	}
	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(12px) scale(0.96);
		}
	}
	@keyframes slide {
		from {
			transform: translateY(100%);
		}
	}
	@keyframes dim {
		from {
			opacity: 0;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		dialog[open] .sheet {
			animation: dim 160ms ease;
		}
	}
</style>
