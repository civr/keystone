<script lang="ts">
	import type { FieldMeta } from '$lib/settingsMeta';
	import { parseList } from '$lib/appEdit';

	let {
		path,
		meta,
		value,
		error = '',
		weakened = false,
		onchange
	}: {
		path: string;
		meta: FieldMeta;
		value: unknown;
		error?: string;
		/** The pending change lowers security: shown inline, before saving. */
		weakened?: boolean;
		onchange: (v: unknown) => void;
	} = $props();

	const id = $derived('f-' + path.replace(/\./g, '-'));
	const helpId = $derived(id + '-help');
	const stacked = $derived(meta.kind === 'cidrs' || meta.kind === 'text');

	// Typed text for numbers and lists, kept while the user edits so "1" → "" → "12" works.
	let text = $state('');
	let editing = false;
	$effect(() => {
		const v = value;
		if (editing) return;
		text = meta.kind === 'cidrs' ? ((v as string[]) ?? []).join('\n') : String(v ?? '');
	});

	function onText(t: string) {
		editing = true;
		text = t;
		if (meta.kind === 'int') onchange(t.trim() === '' ? NaN : Number(t));
		else if (meta.kind === 'cidrs') onchange(parseList(t));
		else onchange(t);
	}
</script>

<div class="srow" class:stack={stacked} class:wrap-phone={meta.kind === 'enum'} data-path={path}>
	<div class="text">
		<label class="label" for={id}>{meta.label}</label>
		<p class="help" id={helpId}>{meta.help}</p>
		{#if error}
			<p class="field-error" role="alert">{error}</p>
		{:else if weakened}
			<span class="chip warn">
				<svg viewBox="0 0 20 20" aria-hidden="true" width="12" height="12"
					><path d="M10 3.2 17.6 16.5H2.4z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" /></svg
				>
				Verlaagt de beveiliging
			</span>
		{/if}
	</div>
	<div class="control">
		{#if meta.kind === 'bool'}
			<input
				{id}
				class="switch"
				type="checkbox"
				role="switch"
				aria-describedby={helpId}
				checked={value === true}
				onchange={(e) => onchange(e.currentTarget.checked)}
			/>
		{:else if meta.kind === 'enum'}
			<div class="segmented" role="radiogroup" aria-labelledby={id} aria-describedby={helpId}>
				{#each meta.options ?? [] as o, i}
					<label>
						<input
							id={i === 0 ? id : undefined}
							type="radio"
							name={id}
							value={o.value}
							checked={value === o.value}
							onchange={() => onchange(o.value)}
						/>
						<span>{o.label}</span>
					</label>
				{/each}
			</div>
		{:else if meta.kind === 'int'}
			<input
				{id}
				class="input num"
				style:width="{String(meta.max).length + 5}ch"
				type="number"
				inputmode="numeric"
				min={meta.min}
				max={meta.max}
				step="1"
				aria-describedby={helpId}
				aria-invalid={error ? 'true' : undefined}
				value={text}
				oninput={(e) => onText(e.currentTarget.value)}
				onblur={() => (editing = false)}
			/>
			{#if meta.unit}<span class="unit">{meta.unit}</span>{/if}
		{:else if meta.kind === 'cidrs'}
			<textarea
				{id}
				class="input mono"
				rows="3"
				placeholder={meta.placeholder ? `${meta.placeholder}\n(one per line)` : 'One per line'}
				autocapitalize="none"
				spellcheck="false"
				aria-describedby={helpId}
				aria-invalid={error ? 'true' : undefined}
				value={text}
				oninput={(e) => onText(e.currentTarget.value)}
				onblur={() => (editing = false)}
			></textarea>
		{:else}
			<input
				{id}
				class="input"
				placeholder={meta.placeholder}
				autocapitalize="none"
				spellcheck="false"
				aria-describedby={helpId}
				aria-invalid={error ? 'true' : undefined}
				value={text}
				oninput={(e) => onText(e.currentTarget.value)}
				onblur={() => (editing = false)}
			/>
		{/if}
	</div>
</div>
