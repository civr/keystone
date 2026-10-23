<script lang="ts">
	let {
		value = $bindable(''),
		placeholder = 'Zoek een app…',
		onsubmit
	}: { value?: string; placeholder?: string; onsubmit?: () => void } = $props();

	let input: HTMLInputElement;
	const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.userAgent);

	function typing(target: EventTarget | null): boolean {
		const el = target as HTMLElement | null;
		return !!el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));
	}

	// ⌘K / Ctrl+K anywhere, or "/" when not already typing, focuses the search.
	function onWindowKey(e: KeyboardEvent) {
		if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'k') {
			e.preventDefault();
			input.focus();
			input.select();
		} else if (e.key === '/' && !e.metaKey && !e.ctrlKey && !e.altKey && !typing(e.target)) {
			e.preventDefault();
			input.focus();
		} else if (e.key === 'Escape' && value && e.target !== input && !typing(e.target)) {
			value = '';
		}
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'Enter' && !e.isComposing) {
			e.preventDefault();
			onsubmit?.();
		} else if (e.key === 'Escape') {
			e.preventDefault();
			if (value) value = '';
			else input.blur();
		}
	}
</script>

<svelte:window onkeydown={onWindowKey} />

<label class="search">
	<span class="visually-hidden">Zoek een app</span>
	<svg class="lens" viewBox="0 0 20 20" aria-hidden="true">
		<circle cx="8.5" cy="8.5" r="5.75" />
		<path d="M13 13l4.25 4.25" />
	</svg>
	<input
		bind:this={input}
		bind:value
		onkeydown={onKey}
		type="search"
		enterkeyhint="go"
		autocomplete="off"
		autocapitalize="off"
		spellcheck="false"
		{placeholder}
	/>
	{#if value}
		<button type="button" class="clear" aria-label="Wis zoekopdracht" onclick={() => ((value = ''), input.focus())}>
			<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M5 5l6 6M11 5l-6 6" /></svg>
		</button>
	{:else}
		<kbd aria-hidden="true">{isMac ? '⌘K' : 'Ctrl K'}</kbd>
	{/if}
</label>

<style>
	/* A Spotlight-style field: a grey capsule-ish fill, no stroke, a soft ring on focus. */
	.search {
		display: flex;
		align-items: center;
		gap: 7px;
		width: 380px;
		max-width: 100%;
		height: 36px;
		padding: 0 5px 0 11px;
		border-radius: var(--radius-control);
		background: var(--topbar-field);
		color: var(--topbar-ink-2);
		cursor: text;
		transition:
			background-color var(--dur-2) var(--ease),
			box-shadow var(--dur-2) var(--ease);
	}
	.search:focus-within {
		background: #fff;
		box-shadow:
			0 0 0 3.5px rgba(0, 122, 255, 0.25),
			0 0 0 1px var(--primary);
	}
	.lens {
		flex: none;
		width: 16px;
		height: 16px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.9;
		stroke-linecap: round;
	}
	input {
		flex: 1;
		min-width: 0;
		height: 100%;
		padding: 0;
		border: 0;
		outline: 0;
		background: transparent;
		color: var(--topbar-ink);
		font-size: 0.9375rem;
	}
	input::placeholder {
		color: var(--topbar-ink-2);
		opacity: 1;
	}
	input::-webkit-search-cancel-button {
		display: none;
	}
	kbd {
		flex: none;
		margin-right: 3px;
		padding: 1px 6px;
		border-radius: 5px;
		background: rgba(0, 0, 0, 0.06);
		color: var(--topbar-ink-2);
		font: 500 0.75rem/1.5 var(--font);
		letter-spacing: 0.02em;
	}
	@media (hover: none) {
		kbd {
			display: none;
		}
	}
	.clear {
		flex: none;
		display: grid;
		place-items: center;
		width: 22px;
		height: 22px;
		margin-right: 2px;
		border: 0;
		border-radius: 50%;
		background: #aeaeb2;
		color: #fff;
		cursor: pointer;
	}
	.clear:hover {
		background: #8e8e93;
	}
	.clear svg {
		width: 9px;
		height: 9px;
		stroke: currentColor;
		stroke-width: 2.4;
		stroke-linecap: round;
	}
	@media (pointer: coarse) {
		input {
			font-size: 16px;
		}
		.search {
			height: 40px;
		}
	}
</style>
