<script lang="ts">
	import { tick } from 'svelte';
	import { fade } from 'svelte/transition';
	import { rise } from '$lib/motion';
	import { initials } from '$lib/nav';

	let {
		name,
		username,
		onlogout
	}: { name: string; username: string; onlogout: () => void } = $props();

	let open = $state(false);
	let root: HTMLDivElement;
	let button: HTMLButtonElement;
	let menu = $state<HTMLDivElement>();

	function items(): HTMLElement[] {
		return Array.from(menu?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? []);
	}

	async function toggle() {
		open = !open;
		if (open) {
			await tick();
			items()[0]?.focus();
		}
	}

	function close(refocus = true) {
		if (!open) return;
		open = false;
		if (refocus) button.focus();
	}

	function onMenuKey(e: KeyboardEvent) {
		const list = items();
		const i = list.indexOf(document.activeElement as HTMLElement);
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			e.preventDefault();
			const d = e.key === 'ArrowDown' ? 1 : -1;
			list[(i + d + list.length) % list.length]?.focus();
		} else if (e.key === 'Home' || e.key === 'End') {
			e.preventDefault();
			list[e.key === 'Home' ? 0 : list.length - 1]?.focus();
		} else if (e.key === 'Escape') {
			e.preventDefault();
			e.stopPropagation();
			close();
		} else if (e.key === 'Tab') {
			close(false);
		}
	}

	function onWindowPointer(e: PointerEvent) {
		if (open && !root.contains(e.target as Node)) close(false);
	}
</script>

<svelte:window onpointerdown={onWindowPointer} />

<div class="wrap" bind:this={root}>
	<button
		bind:this={button}
		type="button"
		class="trigger"
		aria-haspopup="menu"
		aria-expanded={open}
		aria-label="Account van {name}"
		onclick={toggle}
	>
		<span class="avatar" aria-hidden="true">{initials(name)}</span>
		<span class="who" aria-hidden="true">
			<strong>{name}</strong>
			<span>{username}</span>
		</span>
		<svg class="caret" viewBox="0 0 20 20" aria-hidden="true"><path d="M6 8l4 4 4-4" /></svg>
	</button>

	{#if open}
		<div
			bind:this={menu}
			class="menu"
			role="menu"
			tabindex="-1"
			aria-label="Account"
			onkeydown={onMenuKey}
			in:rise={{ y: -4, scale: 0.98, duration: 200 }}
			out:fade={{ duration: 100 }}
		>
			<div class="head">
				<strong>{name}</strong>
				<span>{username}</span>
			</div>
			<a role="menuitem" href="/instellingen" onclick={() => close(false)}>
				<svg viewBox="0 0 20 20" aria-hidden="true">
					<circle cx="10" cy="10" r="2.6" />
					<path
						d="M10 2.5v2M10 15.5v2M2.5 10h2M15.5 10h2M4.7 4.7l1.4 1.4M13.9 13.9l1.4 1.4M4.7 15.3l1.4-1.4M13.9 6.1l1.4-1.4"
					/>
				</svg>
				Instellingen
			</a>
			<button
				role="menuitem"
				type="button"
				onclick={() => {
					close(false);
					onlogout();
				}}
			>
				<svg viewBox="0 0 20 20" aria-hidden="true">
					<path d="M8 3.5H5A1.5 1.5 0 0 0 3.5 5v10A1.5 1.5 0 0 0 5 16.5h3M13 13.5L16.5 10 13 6.5M16.5 10H8" />
				</svg>
				Uitloggen
			</button>
		</div>
	{/if}
</div>

<style>
	.wrap {
		position: relative;
		flex: none;
	}
	.trigger {
		display: flex;
		align-items: center;
		gap: 8px;
		height: 36px;
		padding: 0 8px 0 3px;
		border: 0;
		border-radius: var(--radius-pill);
		background: transparent;
		color: var(--topbar-ink);
		text-align: left;
		cursor: pointer;
		transition: background-color var(--dur-1) var(--ease);
	}
	.trigger:hover,
	.trigger[aria-expanded='true'] {
		background: var(--topbar-hover);
	}
	.trigger:active {
		background: var(--press);
	}
	.who {
		display: flex;
		flex-direction: column;
		min-width: 0;
		max-width: 180px;
		line-height: 1.2;
	}
	.who strong {
		font-size: 0.875rem;
		font-weight: 500;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.who span {
		display: none;
	}
	.caret {
		width: 14px;
		height: 14px;
		fill: none;
		stroke: var(--topbar-ink-2);
		stroke-width: 2;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	@media (max-width: 767px) {
		.who,
		.caret {
			display: none;
		}
		.trigger {
			padding: 0 3px;
		}
	}

	/* A macOS menu: frosted, 12px corners, the hovered item fills with the accent. */
	.menu {
		position: absolute;
		top: calc(100% + 6px);
		right: 0;
		z-index: var(--z-dropdown);
		min-width: 240px;
		padding: 5px;
		border-radius: var(--radius-md);
		background: rgba(246, 246, 248, 0.86);
		-webkit-backdrop-filter: var(--material-blur);
		backdrop-filter: var(--material-blur);
		box-shadow:
			0 0 0 0.5px rgba(0, 0, 0, 0.12),
			var(--shadow-md);
		transform-origin: top right;
	}
	@media (prefers-reduced-transparency: reduce) {
		.menu {
			background: #f6f6f8;
		}
	}
	.head {
		display: flex;
		flex-direction: column;
		padding: 7px 10px 9px;
		margin-bottom: 5px;
		box-shadow: 0 0.5px 0 rgba(0, 0, 0, 0.12);
		line-height: 1.35;
	}
	.head strong {
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.head span {
		font-size: 0.8125rem;
		color: var(--color-text-muted);
	}
	[role='menuitem'] {
		display: flex;
		align-items: center;
		gap: 9px;
		width: 100%;
		height: 30px;
		padding: 0 10px;
		border: 0;
		border-radius: 6px;
		background: transparent;
		color: var(--color-text);
		font-size: 0.875rem;
		text-align: left;
		text-decoration: none;
		cursor: default;
	}
	[role='menuitem']:hover,
	[role='menuitem']:focus-visible {
		background: var(--primary);
		color: #fff;
		text-decoration: none;
		outline: none;
	}
	[role='menuitem']:hover svg,
	[role='menuitem']:focus-visible svg {
		stroke: #fff;
	}
	[role='menuitem'] svg {
		width: 16px;
		height: 16px;
		fill: none;
		stroke: var(--color-text-muted);
		stroke-width: 1.6;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
</style>
