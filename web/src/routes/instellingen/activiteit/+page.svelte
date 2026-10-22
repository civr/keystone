<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { auditLabel, auditSummary } from '$lib/format';
	import { toast } from '$lib/toast.svelte';

	interface Entry {
		id: number;
		at: string;
		actor: string;
		action: string;
		ip: string | null;
		detail: unknown;
	}

	const PAGE = 50;
	let entries = $state<Entry[]>([]);
	let next = $state<number | null>(null);
	let loaded = $state(false);
	let loading = $state(false);
	let error = $state('');
	let sentinel = $state<HTMLElement | null>(null);

	async function more() {
		if (loading) return;
		loading = true;
		error = '';
		try {
			const q = new URLSearchParams({ limit: String(PAGE) });
			if (next !== null) q.set('before', String(next));
			const r = await api.get<{ entries: Entry[]; next_before: number | null }>(`/admin/audit?${q}`);
			entries = [...entries, ...r.entries];
			next = r.next_before;
			loaded = true;
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'De activiteit kon niet worden geladen.';
		} finally {
			loading = false;
		}
	}
	onMount(more);

	// Load the next page when the end of the list scrolls into view.
	$effect(() => {
		if (!sentinel) return;
		const io = new IntersectionObserver((es) => es[0].isIntersecting && next !== null && !error && more(), {
			rootMargin: '400px'
		});
		io.observe(sentinel);
		return () => io.disconnect();
	});

	const dayFmt = new Intl.DateTimeFormat('nl-NL', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
	const timeFmt = new Intl.DateTimeFormat('nl-NL', { hour: '2-digit', minute: '2-digit' });

	const days = $derived.by(() => {
		const out: { day: string; items: Entry[] }[] = [];
		for (const e of entries) {
			const day = dayFmt.format(new Date(e.at));
			if (out.at(-1)?.day !== day) out.push({ day, items: [] });
			out.at(-1)!.items.push(e);
		}
		return out;
	});

	// Fetched through api (not a plain link) so an expired session goes to
	// /login and errors show as a toast instead of a broken download.
	let exporting = $state(false);
	async function exportLog() {
		if (exporting) return;
		exporting = true;
		try {
			const blob = await api.blob('/admin/audit/export');
			const url = URL.createObjectURL(blob);
			const a = Object.assign(document.createElement('a'), { href: url, download: 'keystone-audit.ndjson' });
			document.body.append(a);
			a.click();
			a.remove();
			setTimeout(() => URL.revokeObjectURL(url), 1000);
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'De export is mislukt.', 'error');
		} finally {
			exporting = false;
		}
	}

	const ACTORS: Record<string, string> = { owner: 'jij', anonymous: 'onbekend', cli: 'opdrachtregel', system: 'systeem' };
	const tone = (a: string) =>
		/failure|failed|locked|not_allowed|denied/.test(a) ? 'bad' : /emergency|recovery|network_reset/.test(a) ? 'warn' : '';
</script>

<div class="pane-head">
	<h1 class="pane-title">Activiteit</h1>
	<button type="button" class="btn btn-secondary btn-sm" onclick={exportLog} disabled={exporting} aria-busy={exporting}>
		<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 3.5v9M6 9l4 4 4-4M4 16.5h12" /></svg>
		Exporteren
	</button>
</div>
<p class="pane-intro">Alles wat er met je account en Keystone gebeurt, nieuwste eerst.</p>

{#if !loaded && error}
	<div class="empty-note">
		<p>{error}</p>
		<button type="button" class="btn btn-secondary btn-sm" onclick={more}>Opnieuw proberen</button>
	</div>
{:else if !loaded}
	<div class="loading-note" aria-label="Laden"><span class="spinner"></span></div>
{:else if entries.length === 0}
	<p class="empty-note">Nog geen activiteit.</p>
{:else}
	{#each days as d (d.day)}
		<section class="group">
			<h2>{d.day}</h2>
			<ul class="card list">
				{#each d.items as e (e.id)}
					{@const lines = auditSummary(e)}
					<li class="srow entry">
						<span class="dot {tone(e.action)}" aria-hidden="true"></span>
						<div class="text">
							<span class="label">{auditLabel(e.action)}</span>
							<p class="help">
								{timeFmt.format(new Date(e.at))} · {ACTORS[e.actor] ?? e.actor}{e.ip ? ` · ${e.ip}` : ''}
							</p>
							{#if lines.length}
								<ul class="detail">
									{#each lines as l}<li>{l}</li>{/each}
								</ul>
							{/if}
						</div>
					</li>
				{/each}
			</ul>
		</section>
	{/each}
	{#if error}
		<div class="empty-note">
			<p>{error}</p>
			<button type="button" class="btn btn-secondary btn-sm" onclick={more}>Opnieuw proberen</button>
		</div>
	{:else if next !== null}
		<div class="end" bind:this={sentinel}>
			<button type="button" class="btn btn-secondary btn-sm" onclick={more} disabled={loading}>
				{#if loading}<span class="spinner" aria-hidden="true"></span>{/if}
				Meer laden
			</button>
		</div>
	{/if}
{/if}

<style>
	.list {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.entry {
		align-items: flex-start;
		min-height: 0;
	}
	/* Status dot: neutral by default, warning and danger fills where it matters. */
	.dot {
		flex: none;
		width: 7px;
		height: 7px;
		margin-top: 8px;
		border-radius: 50%;
		background: var(--c-400);
	}
	.dot.bad {
		background: var(--color-danger);
	}
	.dot.warn {
		background: var(--color-warning);
	}
	.detail {
		margin: 6px 0 0;
		padding: 0;
		list-style: none;
		font-size: 0.8125rem;
		line-height: 1.5;
		color: var(--color-text-muted);
		font-variant-numeric: tabular-nums;
		overflow-wrap: anywhere;
	}
	.end {
		display: flex;
		justify-content: center;
		padding: 0 0 16px;
	}
</style>
