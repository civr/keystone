<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { auditLabel, relativeTime } from '$lib/format';

	interface Day {
		day: string;
		successes: number;
		failures: number;
		app_opens: number;
	}
	interface Overview {
		users: number;
		groups: number;
		apps: number;
		sessions?: number;
		days?: Day[];
		top_apps?: { app: string; opens: number }[];
	}
	interface Entry {
		id: number;
		at: string;
		actor: string;
		action: string;
		ip: string | null;
	}

	let me = $state<Me | null>(null);
	let ov = $state<Overview | null>(null);
	let events = $state<Entry[] | null>(null);
	let error = $state('');

	onMount(async () => {
		try {
			me = await api.get<Me>('/me');
			ov = await api.get<Overview>('/admin/overview');
			if (me.permissions.includes('*') || me.permissions.includes('audit.read'))
				events = (await api.get<{ entries: Entry[] }>('/admin/audit?limit=6')).entries;
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'The overview could not be loaded.';
		}
	});

	const peak = $derived(Math.max(1, ...(ov?.days ?? []).map((d) => d.successes + d.failures)));
	const topPeak = $derived(Math.max(1, ...(ov?.top_apps ?? []).map((a) => a.opens)));
	const weekday = (d: string) => new Date(d + 'T12:00:00').toLocaleDateString('en-GB', { weekday: 'short' });

	// Things to look at, in order of risk.
	const attention = $derived.by(() => {
		const out: { text: string; href: string }[] = [];
		if (typeof location !== 'undefined' && location.protocol === 'http:')
			out.push({ text: 'Keystone is opened over http. Put it behind https for passkeys and secure cookies.', href: '/admin/system' });
		if (me?.passkey_missing) out.push({ text: 'You have no passkey yet. Add one for phishing-proof sign-in.', href: '/account/security' });
		return out;
	});
</script>

<div class="pane-head">
	<div>
		<h1 class="pane-title">Welcome{me?.display_name ? `, ${me.display_name}` : ''}</h1>
		<p class="pane-intro sub">An overview of Keystone.</p>
	</div>
</div>

{#if error}
	<p class="error" role="alert">{error}</p>
{:else if !ov}
	<p class="loading-note" role="status">Loading…</p>
{:else}
	<div class="stats">
		<a class="stat" href="/admin/users"><span class="n">{ov.users}</span><span class="l">Users</span></a>
		<a class="stat" href="/admin/groups"><span class="n">{ov.groups}</span><span class="l">Groups</span></a>
		<a class="stat" href="/admin/apps"><span class="n">{ov.apps}</span><span class="l">Applications</span></a>
		<a class="stat" href="/admin/sessions"><span class="n">{ov.sessions ?? '—'}</span><span class="l">Active sessions</span></a>
	</div>

	{#if attention.length}
		<section class="box attention" aria-labelledby="att">
			<h2 id="att">Needs attention</h2>
			<ul>
				{#each attention as a}
					<li><span class="dot" aria-hidden="true"></span><a href={a.href}>{a.text}</a></li>
				{/each}
			</ul>
		</section>
	{/if}

	<div class="grid">
		{#if ov.days}
			<section class="box chart" aria-labelledby="signins">
				<div class="box-head">
					<h2 id="signins">Sign-ins this week</h2>
					<span class="legend"
						><span class="key ok"></span>Succeeded <span class="key bad"></span>Failed</span
					>
				</div>
				<div class="bars" role="img" aria-label="Sign-ins per day over the last seven days">
					{#each ov.days as d (d.day)}
						<div class="col" title="{weekday(d.day)}: {d.successes} succeeded, {d.failures} failed, {d.app_opens} app opens">
							<div class="stack" style:height="{((d.successes + d.failures) / peak) * 100}%">
								{#if d.failures}<span class="seg bad" style:flex={d.failures}></span>{/if}
								{#if d.successes}<span class="seg ok" style:flex={d.successes}></span>{/if}
							</div>
							<span class="x">{weekday(d.day)}</span>
						</div>
					{/each}
				</div>
				<div class="visually-hidden">
				<table>
					<caption>Sign-ins per day</caption>
					<thead><tr><th>Day</th><th>Succeeded</th><th>Failed</th></tr></thead>
					<tbody>
						{#each ov.days as d}<tr><td>{d.day}</td><td>{d.successes}</td><td>{d.failures}</td></tr>{/each}
					</tbody>
				</table>
				</div>
			</section>
		{/if}

		{#if ov.top_apps}
			<section class="box" aria-labelledby="top">
				<h2 id="top">Most used apps</h2>
				{#if ov.top_apps.length === 0}
					<p class="muted">No app has been opened this week.</p>
				{:else}
					<ul class="top">
						{#each ov.top_apps as a (a.app)}
							<li>
								<span class="name">{a.app}</span>
								<span class="track"><span class="fill" style:width="{(a.opens / topPeak) * 100}%"></span></span>
								<span class="num">{a.opens}</span>
							</li>
						{/each}
					</ul>
				{/if}
			</section>
		{/if}
	</div>

	{#if events}
		<section class="box" aria-labelledby="recent">
			<div class="box-head">
				<h2 id="recent">Recent events</h2>
				<a href="/admin/audit">Audit log</a>
			</div>
			<ul class="events">
				{#each events as e (e.id)}
					<li>
						<span class="what">{auditLabel(e.action)}</span>
						<span class="who">{e.actor}</span>
						<span class="when">{relativeTime(e.at)}</span>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
{/if}

<style>
	.sub {
		margin: 4px 0 0;
	}
	.stats {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		gap: 16px;
		margin-bottom: 20px;
	}
	.stat {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 18px 20px;
		border: 1px solid var(--c-border);
		border-radius: var(--radius-card);
		background: var(--c-bg-card);
		color: var(--color-text);
		text-decoration: none;
	}
	.stat:hover {
		border-color: var(--primary);
		text-decoration: none;
	}
	.n {
		font-size: 2rem;
		font-weight: 650;
		letter-spacing: -0.02em;
		font-variant-numeric: tabular-nums;
	}
	.l {
		color: var(--color-text-muted);
		font-size: 0.9375rem;
	}
	.box {
		padding: 18px 20px;
		margin-bottom: 20px;
		border: 1px solid var(--c-border);
		border-radius: var(--radius-card);
		background: var(--c-bg-card);
	}
	.grid > .box {
		margin-bottom: 0;
		align-self: start;
	}
	.grid {
		margin-bottom: 20px;
	}
	.box h2 {
		margin: 0 0 12px;
		font-size: 1.0625rem;
	}
	.box-head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 12px;
	}
	.grid {
		display: grid;
		grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);
		gap: 20px;
	}
	.legend {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		color: var(--color-text-muted);
		font-size: 0.8125rem;
	}
	.key {
		width: 10px;
		height: 10px;
		border-radius: 3px;
		margin-left: 8px;
	}
	.key.ok,
	.seg.ok {
		background: var(--primary);
	}
	.key.bad,
	.seg.bad {
		background: var(--color-danger);
	}
	.bars {
		display: flex;
		align-items: stretch;
		gap: 14px;
		height: 180px;
		padding-top: 8px;
		border-bottom: 1px solid var(--c-border);
	}
	.col {
		flex: 1;
		display: flex;
		flex-direction: column;
		justify-content: flex-end;
		align-items: center;
		position: relative;
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: 2px;
		width: 100%;
		max-width: 36px;
		min-height: 2px;
	}
	.seg {
		border-radius: 4px;
		min-height: 2px;
	}
	.x {
		position: absolute;
		bottom: -22px;
		color: var(--color-text-muted);
		font-size: 0.8125rem;
	}
	.chart {
		padding-bottom: 34px;
	}
	.top {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
		gap: 10px;
	}
	.top li {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 40% 36px;
		align-items: center;
		gap: 10px;
		font-size: 0.9375rem;
	}
	.name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.track {
		height: 6px;
		border-radius: 3px;
		background: var(--c-bg-sunken);
		overflow: hidden;
	}
	.fill {
		display: block;
		height: 100%;
		border-radius: 3px;
		background: var(--primary);
	}
	.num {
		text-align: right;
		font-variant-numeric: tabular-nums;
		color: var(--color-text-muted);
	}
	.events {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.events li {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 160px 120px;
		gap: 12px;
		padding: 10px 0;
		border-top: 1px solid var(--c-border2);
		font-size: 0.9375rem;
	}
	.who,
	.when {
		color: var(--color-text-muted);
	}
	.attention ul {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
		gap: 8px;
	}
	.attention li {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.attention .dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--color-warning);
		flex: none;
	}
	.muted {
		color: var(--color-text-muted);
		margin: 0;
	}
	@media (max-width: 900px) {
		.stats {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
		.grid {
			grid-template-columns: minmax(0, 1fr);
		}
		.events li {
			grid-template-columns: minmax(0, 1fr) auto;
		}
		.events .who {
			display: none;
		}
	}
</style>
