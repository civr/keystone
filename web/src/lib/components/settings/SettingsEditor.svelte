<script lang="ts">
	import { onMount, tick, type Snippet } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import {
		settingsMeta,
		changedPaths,
		fieldError,
		getPath,
		setPath,
		weakenedPaths,
		type Settings,
		type SettingsGroup
	} from '$lib/settingsMeta';
	import { saveSettings } from '$lib/settingsSave';
	import { formatValue } from '$lib/format';
	import { rise } from '$lib/motion';
	import { toast } from '$lib/toast.svelte';
	import Confirm from './Confirm.svelte';
	import Field from './Field.svelte';

	let {
		groups,
		visible,
		after
	}: {
		/** Groups of settings paths to edit, in display order. */
		groups: SettingsGroup[];
		/** Ids of the groups shown now (a tab); all when absent. Hidden groups keep their edits. */
		visible?: string[];
		/** Extra content below the groups (e.g. Recovery on Security). */
		after?: Snippet;
	} = $props();

	let saved = $state.raw<Settings | null>(null);
	let draft = $state.raw<Settings | null>(null);
	let loadError = $state('');
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);
	let confirmer: Confirm;

	const mine = $derived(new Set(groups.flatMap((g) => g.paths)));
	const dirty = $derived(saved && draft ? changedPaths(saved, draft).filter((p) => mine.has(p)) : []);
	const weakened = $derived(saved && draft ? new Set(weakenedPaths(saved, draft)) : new Set<string>());
	const clientErrors = $derived(
		draft
			? Object.fromEntries(
					[...mine].map((p) => [p, fieldError(settingsMeta[p], getPath(draft, p))]).filter(([, e]) => e)
				)
			: {}
	);

	/** The client address as Keystone sees it, for groups with showClientIP. */
	let clientIP = $state('');

	async function load() {
		loadError = '';
		if (groups.some((g) => g.showClientIP))
			api
				.get<Me>('/me')
				.then((me) => (clientIP = me.client_ip))
				.catch(() => {});
		try {
			saved = await api.get<Settings>('/admin/settings');
			draft = saved;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Settings could not be loaded.';
		}
	}
	onMount(load);

	function set(path: string, v: unknown) {
		if (!draft) return;
		draft = setPath(draft, path, v);
		if (errors[path]) delete errors[path];
	}

	function reset() {
		draft = saved;
		errors = {};
	}

	async function focusField(path: string) {
		await tick();
		const row = document.querySelector<HTMLElement>(`[data-path="${CSS.escape(path)}"]`);
		row?.scrollIntoView({ block: 'center', behavior: 'smooth' });
		row?.querySelector<HTMLElement>('input, textarea')?.focus({ preventScroll: true });
	}

	async function save() {
		if (!saved || !draft || busy) return;
		const firstBad = Object.keys(clientErrors)[0];
		if (firstBad) return focusField(firstBad);
		busy = true;
		const base = saved;
		// Only this page's fields change; everything else stays as stored.
		const next = dirty.reduce((s, p) => setPath(s, p, getPath(draft, p)), base);
		try {
			const res = await saveSettings(base, next, {
				weakened: (paths) =>
					confirmer.ask({
						title: 'This lowers security',
						message:
							paths.length === 1
								? 'This change makes Keystone less strict. Save it anyway?'
								: 'These changes make Keystone less strict. Save them anyway?',
						confirm: 'Save anyway',
						danger: true,
						items: paths.map((p) => ({
							title: settingsMeta[p].label,
							detail: `${formatValue(settingsMeta[p], getPath(base, p))} → ${formatValue(settingsMeta[p], getPath(next, p))}`
						}))
					}),
				rpChange: () =>
					confirmer.ask({
						title: 'Change the public address?',
						message:
							'Passkeys belong to the address they were made on. On a new host name you sign in with your password and add a new passkey there.',
						confirm: 'Change address',
						danger: true
					})
			});
			if (res.status === 'saved') {
				saved = res.settings;
				draft = res.settings;
				errors = {};
				toast('Saved');
			} else if (res.status === 'invalid') {
				if (res.field && mine.has(res.field)) {
					errors = { [res.field]: res.message };
					focusField(res.field);
				} else toast(res.message, 'error');
			}
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Saving failed.', 'error');
		} finally {
			busy = false;
		}
	}
</script>

{#if loadError}
	<div class="empty-note">
		<p>{loadError}</p>
		<button type="button" class="btn btn-secondary btn-sm" onclick={load}>Try again</button>
	</div>
{:else if !draft}
	<div class="loading-note" aria-label="Loading"><span class="spinner"></span></div>
{:else}
	<form onsubmit={(e) => (e.preventDefault(), save())} novalidate>
		<div class="cols">
		{#each groups.filter((g) => !visible || visible.includes(g.id)) as g (g.id)}
			<section class="group" aria-labelledby="g-{g.id}">
				<h2 id="g-{g.id}">{g.title}</h2>
				<div class="card">
					{#each g.paths as p (p)}
						<Field
							path={p}
							meta={settingsMeta[p]}
							value={getPath(draft, p)}
							error={errors[p] ?? (dirty.includes(p) ? (clientErrors[p] ?? '') : '')}
							weakened={weakened.has(p)}
							onchange={(v) => set(p, v)}
						/>
					{/each}
				</div>
				{#if g.showClientIP && clientIP}
					<p class="foot">Your address as Keystone sees it: <span class="mono">{clientIP}</span></p>
				{/if}
				{#if g.foot}<p class="foot">{g.foot}</p>{/if}
			</section>
		{/each}

		</div>

		{#if dirty.length}
			<div class="savebar" transition:rise={{ y: 8, duration: 200 }}>
				<span class="count">{dirty.length === 1 ? '1 change' : `${dirty.length} changes`}</span>
				<button type="button" class="btn btn-secondary" onclick={reset} disabled={busy}>Revert</button>
				<button type="submit" class="btn btn-primary" disabled={busy}>
					{#if busy}<span class="spinner" aria-hidden="true"></span>{/if}
					Save
				</button>
			</div>
		{/if}
	</form>
{/if}

{@render after?.()}

<Confirm bind:this={confirmer} />

<style>
	/* Groups flow into two columns on a wide pane, so a page fits the window. */
	/* Groups side by side on a wide pane; a lone group fills the width. */
	.cols {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(440px, 1fr));
		gap: 0 20px;
		align-items: start;
	}
	.cols > :global(.group) {
		break-inside: avoid;
	}
</style>
