<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		title,
		children
	}: { open?: boolean; title: string; children: Snippet } = $props();

	let dialog: HTMLDialogElement;
	const uid = $props.id();
	const titleId = `dlg-${uid}`;

	// Native <dialog>: focus trap, Esc, inert background and focus restore for free.
	$effect(() => {
		if (open && !dialog.open) dialog.showModal();
		else if (!open && dialog.open) dialog.close();
	});

	function onBackdrop(e: MouseEvent) {
		if (e.target === dialog) open = false;
	}
</script>

<!-- A sheet: centred on wide screens, rising from the bottom edge on phones. -->
<dialog bind:this={dialog} aria-labelledby={titleId} onclose={() => (open = false)} onclick={onBackdrop}>
	<div class="panel">
		<header>
			<h2 id={titleId}>{title}</h2>
			<button type="button" class="close" aria-label="Sluiten" onclick={() => (open = false)}>
				<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" /></svg>
			</button>
		</header>
		<div class="body">{@render children()}</div>
	</div>
</dialog>

<style>
	/* Desktop: a centred sheet that grows out of the page. Phone: a sheet that
	   rises from the bottom edge and leaves the same way. */
	dialog {
		padding: 0;
		border: 0;
		background: transparent;
		color: inherit;
		max-width: none;
		max-height: none;
		width: min(540px, 100vw - 32px);
		overflow: visible;
	}
	dialog::backdrop {
		background: var(--scrim);
	}
	.panel {
		display: flex;
		flex-direction: column;
		max-height: min(86dvh, 760px);
		border-radius: var(--radius-sheet);
		background: var(--c-bg-card);
		box-shadow:
			0 0 0 0.5px rgba(0, 0, 0, 0.08),
			var(--shadow-lg);
	}
	dialog[open]::backdrop {
		animation: overlay-in var(--dur-3) var(--ease) both;
	}
	dialog[open] .panel {
		animation: sheet-in var(--dur-4) var(--ease-out) both;
	}
	header {
		position: relative;
		display: flex;
		align-items: center;
		justify-content: center;
		min-height: 56px;
		padding: 14px 52px 6px;
	}
	h2 {
		margin: 0;
		font-size: 1.0625rem;
		font-weight: 600;
		line-height: 1.3;
		letter-spacing: -0.015em;
		text-align: center;
	}
	.body {
		padding: 10px 24px 24px;
		overflow: auto;
	}
	/* iOS close button: a grey disc with a small cross. */
	.close {
		position: absolute;
		top: 14px;
		right: 14px;
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border: 0;
		border-radius: 50%;
		background: rgba(118, 118, 128, 0.12);
		color: #8e8e93;
		cursor: pointer;
		transition: background-color var(--dur-1) var(--ease);
	}
	.close:hover {
		background: rgba(118, 118, 128, 0.2);
		color: var(--color-text-muted);
	}
	.close:active {
		background: rgba(118, 118, 128, 0.28);
	}
	.close svg {
		width: 11px;
		height: 11px;
		stroke: currentColor;
		stroke-width: 2.4;
		stroke-linecap: round;
	}
	@media (max-width: 599px) {
		dialog {
			width: 100vw;
			margin: auto 0 0;
		}
		.panel {
			max-height: 92dvh;
			border-radius: var(--radius-sheet) var(--radius-sheet) 0 0;
			padding-bottom: env(safe-area-inset-bottom);
		}
		dialog[open] .panel {
			animation-name: sheet-up;
		}
		header {
			padding: 14px 52px 4px;
		}
		.body {
			padding: 8px 18px 20px;
		}
	}
	@keyframes overlay-in {
		from {
			opacity: 0;
		}
	}
	@keyframes sheet-in {
		from {
			opacity: 0;
			transform: scale(0.96);
		}
	}
	@keyframes sheet-up {
		from {
			transform: translateY(100%);
		}
	}
</style>
