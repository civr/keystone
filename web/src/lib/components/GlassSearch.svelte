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

<label class="search glass">
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
	.search {
		display: flex;
		align-items: center;
		gap: 10px;
		height: 48px;
		padding: 0 8px 0 14px;
		border-radius: var(--radius-m);
		cursor: text;
		transition:
			box-shadow 240ms ease,
			background-color 240ms ease;
	}
	.search:focus-within {
		background: var(--glass-strong);
		box-shadow:
			0 0 0 4px color-mix(in srgb, var(--accent) 22%, transparent),
			var(--glass-shadow);
	}
	.lens {
		flex: none;
		width: 18px;
		height: 18px;
		fill: none;
		stroke: var(--text-2);
		stroke-width: 1.8;
		stroke-linecap: round;
	}
	input {
		flex: 1;
		min-width: 0;
		height: 100%;
		border: 0;
		outline: 0;
		background: transparent;
		font-size: 1.0625rem;
		letter-spacing: -0.01em;
	}
	input::placeholder {
		color: var(--text-2);
		opacity: 1;
	}
	input::-webkit-search-cancel-button {
		display: none;
	}
	kbd {
		flex: none;
		font: 500 0.75rem/1 var(--font);
		color: var(--text-3);
		padding: 5px 7px;
		border-radius: 7px;
		border: 1px solid var(--hairline);
		margin-right: 4px;
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
		width: 32px;
		height: 32px;
		border: 0;
		border-radius: 50%;
		background: transparent;
		color: var(--text-2);
		cursor: pointer;
	}
	.clear:hover {
		background: var(--hover);
	}
	.clear svg {
		width: 14px;
		height: 14px;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
	}
</style>
