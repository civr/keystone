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
	/* Nimbus .topbar-search: a white 12px-cornered box with a magnifier and the shortcut key. */
	.search {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 300px;
		max-width: 100%;
		height: 42px;
		padding: 0 6px 0 14px;
		border: 1px solid var(--topbar-field-stroke);
		border-radius: var(--radius-md);
		background: var(--topbar-field);
		color: var(--topbar-ink-2);
		cursor: text;
		transition:
			border-color var(--dur-2) var(--ease),
			box-shadow var(--dur-2) var(--ease);
	}
	.search:hover {
		border-color: var(--topbar-stroke);
		box-shadow: var(--shadow-sm);
	}
	.search:focus-within {
		border-color: var(--primary);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--primary) 16%, transparent);
	}
	.lens {
		flex: none;
		width: 16px;
		height: 16px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
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
		font-size: 0.8125rem;
		font-weight: 500;
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
		margin-right: 2px;
		padding: 2px 7px;
		border-radius: var(--radius-xs);
		background: var(--topbar-hover);
		color: var(--topbar-ink-2);
		font: 600 0.6875rem/1.45 var(--font);
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
		width: 28px;
		height: 28px;
		border: 0;
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--topbar-ink-2);
		cursor: pointer;
	}
	.clear:hover {
		background: var(--topbar-hover);
		color: var(--topbar-ink);
	}
	.clear svg {
		width: 12px;
		height: 12px;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
	}
	@media (pointer: coarse) {
		input {
			font-size: 16px;
		}
	}
</style>
