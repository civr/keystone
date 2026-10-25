<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { describeUA, relativeTime } from '$lib/format';
	import { toast } from '$lib/toast.svelte';

	interface Session {
		id: string;
		account: string;
		name: string;
		created_at: string;
		last_seen_at: string;
		ip: string;
		user_agent: string;
		emergency: boolean;
		current: boolean;
	}

	let sessions = $state<Session[] | null>(null);
	let loadError = $state('');
	let query = $state('');
	let busy = $state('');

	async function load() {
		loadError = '';
		try {
			sessions = (await api.get<{ sessions: Session[] }>('/admin/sessions/all')).sessions;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'The sessions could not be loaded.';
		}
	}
	onMount(load);

	const shown = $derived(
		(sessions ?? []).filter((s) => {
			const q = query.trim().toLowerCase();
			return !q || [s.name, s.ip, describeUA(s.user_agent)].some((v) => v.toLowerCase().includes(q));
		})
	);

	async function end(s: Session) {
		busy = s.id;
		try {
			await api.send('DELETE', `/admin/sessions/all/${encodeURIComponent(s.id)}`);
			toast(`Signed ${s.name} out on ${describeUA(s.user_agent)}`);
			await load();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'The session could not be ended.', 'error');
		} finally {
			busy = '';
		}
	}
</script>

<div class="pane-head"><h1 class="pane-title">Sessions</h1></div>
<p class="pane-intro">Everyone signed in to Keystone right now. Ending a session signs that browser out at once, including apps behind the gate.</p>

{#if loadError}
	<p class="error" role="alert">{loadError}</p>
{:else if !sessions}
	<p class="loading-note" role="status">Loading…</p>
{:else}
	<input class="input filter" type="search" placeholder="Filter by person, device or address" bind:value={query} aria-label="Filter sessions" />
	{#if shown.length === 0}
		<p class="empty-note">No sessions match.</p>
	{:else}
		<ul class="card list">
			{#each shown as s (s.id)}
				<li class="srow wrap-phone">
					<div class="text">
						<span class="label">
							{s.name}
							{#if s.current}<span class="chip accent">You, here</span>{/if}
							{#if s.emergency}<span class="chip warn">Emergency account</span>{/if}
						</span>
						<p class="help">{describeUA(s.user_agent)} · {s.ip} · active {relativeTime(s.last_seen_at)}</p>
					</div>
					{#if !s.current}
						<div class="control">
							<button type="button" class="btn btn-secondary btn-sm danger-ink" disabled={busy === s.id} onclick={() => end(s)}>Sign out</button>
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
{/if}

<style>
	.filter {
		max-width: 380px;
		margin-bottom: 16px;
	}
	.list {
		margin: 0;
	}
</style>
