<script lang="ts" module>
	export interface Question {
		title: string;
		message: string;
		confirm: string;
		/** Red confirm button for destructive or security-lowering actions. */
		danger?: boolean;
		/** Optional detail lines, e.g. each weakened setting: old → new. */
		items?: { title: string; detail: string }[];
	}
</script>

<script lang="ts">
	import Sheet from '$lib/components/Sheet.svelte';

	let open = $state(false);
	let q = $state<Question | null>(null);
	let resolve: ((ok: boolean) => void) | null = null;

	/** Opens the sheet; resolves true on confirm, false on cancel, Esc or backdrop. */
	export function ask(question: Question): Promise<boolean> {
		resolve?.(false);
		q = question;
		open = true;
		return new Promise((r) => (resolve = r));
	}

	function answer(ok: boolean) {
		const r = resolve;
		resolve = null;
		open = false;
		r?.(ok);
	}

	// Closed by Esc, the backdrop or the close button: that is a "no".
	$effect(() => {
		if (!open && resolve) answer(false);
	});
</script>

<Sheet bind:open title={q?.title ?? ''}>
	{#if q}
		<p class="msg">{q.message}</p>
		{#if q.items?.length}
			<ul class="change-list">
				{#each q.items as it}
					<li><strong>{it.title}</strong><span>{it.detail}</span></li>
				{/each}
			</ul>
		{/if}
		<div class="sheet-actions">
			<button type="button" class="btn" onclick={() => answer(false)}>Annuleren</button>
			<button type="button" class="btn {q.danger ? 'danger filled' : 'primary'}" onclick={() => answer(true)}>
				{q.confirm}
			</button>
		</div>
	{/if}
</Sheet>

<style>
	.msg {
		margin: 0;
		color: var(--text-2);
		font-size: 0.9375rem;
		line-height: 1.45;
		text-wrap: pretty;
	}
</style>
