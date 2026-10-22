<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { flip } from 'svelte/animate';
	import { fade } from 'svelte/transition';
	import { api, ApiError } from '$lib/api';
	import { hostOf, type App } from '$lib/apps';
	import { appFormError, iconError, moveItem, targetIndex, ICON_TYPES, type AppForm } from '$lib/appEdit';
	import { reducedMotion, spring } from '$lib/motion';
	import { toast } from '$lib/toast.svelte';
	import { latestQueue } from '$lib/serial';
	import Sheet from '$lib/components/Sheet.svelte';

	const COLORS = ['#007aff', '#5856d6', '#af52de', '#ff2d55', '#ff3b30', '#ff9500', '#ffcc00', '#34c759', '#30b0c7', '#8e8e93'];

	let apps = $state<App[] | null>(null);
	let loadError = $state('');
	let announce = $state('');

	async function load() {
		loadError = '';
		try {
			apps = (await api.get<{ apps: App[] }>('/apps')).apps;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'De apps konden niet worden geladen.';
		}
	}
	onMount(load);

	// ---------- Reorder ----------

	// Saves run one at a time in move order, each with the order as it was at
	// that move. Only a failure of the latest save is shown; the list is then
	// reloaded so it matches what Keystone stored.
	const orderQueue = latestQueue();
	function saveOrder() {
		const ids = apps!.map((a) => a.id);
		orderQueue(
			() => api.send('PUT', '/apps/order', { ids }),
			(e) => {
				toast(e instanceof ApiError ? e.message : 'De volgorde kon niet worden opgeslagen.', 'error');
				load();
			}
		);
	}

	/** Keyboard and button path: move one step and keep focus on the moved row's control. */
	async function move(i: number, to: number, focusSel?: string) {
		if (!apps || to < 0 || to >= apps.length || to === i) return;
		const app = apps[i];
		apps = moveItem(apps, i, to);
		announce = `${app.name} staat nu op plaats ${to + 1} van ${apps.length}.`;
		if (focusSel) {
			await tick();
			document.querySelector<HTMLElement>(`[data-id="${app.id}"] ${focusSel}`)?.focus();
		}
		saveOrder();
	}

	let list = $state<HTMLElement | null>(null);
	let drag = $state<{ from: number; to: number; dy: number; rowH: number; id: string } | null>(null);
	let settling = $state(false);
	let committing = $state(false);
	let mids: number[] = [];
	let startY = 0;

	function dragStart(e: PointerEvent, i: number) {
		if (!apps || e.button !== 0 || settling) return;
		e.preventDefault();
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		const rows = [...list!.querySelectorAll<HTMLElement>(':scope > li')];
		mids = rows.map((r) => {
			const b = r.getBoundingClientRect();
			return b.top + b.height / 2;
		});
		const rowH = rows.length > 1 ? mids[1] - mids[0] : rows[0].offsetHeight;
		startY = e.clientY;
		drag = { from: i, to: i, dy: 0, rowH, id: apps[i].id };
	}

	function dragMove(e: PointerEvent) {
		if (!drag || settling) return;
		// 1:1 with the finger, with rubber-band resistance past the ends of the list.
		const raw = e.clientY - startY;
		const min = mids[0] - mids[drag.from];
		const max = mids[mids.length - 1] - mids[drag.from];
		const band = (o: number) => (o * drag!.rowH * 0.55) / (drag!.rowH + 0.55 * Math.abs(o));
		drag.dy = raw < min ? min + band(raw - min) : raw > max ? max + band(raw - max) : raw;
		drag.to = targetIndex(mids, drag.from, mids[drag.from] + drag.dy);
	}

	async function dragEnd() {
		if (!drag || settling) return;
		const { from, to, rowH } = drag;
		// Glide into the new slot on the spring, then commit the order without animation.
		settling = true;
		drag.dy = (to - from) * rowH;
		await new Promise((r) => setTimeout(r, reducedMotion() ? 0 : 320));
		committing = true;
		apps = moveItem(apps!, from, to);
		drag = null;
		settling = false;
		await tick();
		requestAnimationFrame(() => (committing = false));
		if (from !== to) {
			announce = `${apps[to].name} staat nu op plaats ${to + 1} van ${apps.length}.`;
			saveOrder();
		}
	}

	function shift(i: number): number {
		if (!drag || i === drag.from) return 0;
		if (drag.from < drag.to && i > drag.from && i <= drag.to) return -drag.rowH;
		if (drag.to < drag.from && i >= drag.to && i < drag.from) return drag.rowH;
		return 0;
	}

	function handleKey(e: KeyboardEvent, i: number) {
		if (e.key === 'ArrowUp') (e.preventDefault(), move(i, i - 1, '.handle'));
		if (e.key === 'ArrowDown') (e.preventDefault(), move(i, i + 1, '.handle'));
	}

	// ---------- Add / edit ----------

	let sheetOpen = $state(false);
	let editing = $state<App | null>(null);
	let form = $state<AppForm & { icon: string | null }>({ name: '', url: '', color: COLORS[0], icon: null });
	let fieldErrors = $state<Partial<Record<'name' | 'url' | 'color' | 'icon', string>>>({});
	let formError = $state('');
	let saving = $state(false);
	let confirmDelete = $state(false);
	let fileInput = $state<HTMLInputElement | null>(null);

	function openEditor(app: App | null) {
		editing = app;
		form = app
			? { name: app.name, url: app.url, color: app.color, icon: app.icon || null }
			: { name: '', url: 'https://', color: COLORS[(apps?.length ?? 0) % COLORS.length], icon: null };
		fieldErrors = {};
		formError = '';
		confirmDelete = false;
		sheetOpen = true;
	}

	function pickIcon(e: Event & { currentTarget: HTMLInputElement }) {
		const file = e.currentTarget.files?.[0];
		e.currentTarget.value = '';
		if (!file) return;
		const bad = iconError(file);
		if (bad) return void (fieldErrors.icon = bad);
		const reader = new FileReader();
		reader.onload = () => {
			const url = String(reader.result);
			const tooBig = iconError(file, url);
			if (tooBig) fieldErrors.icon = tooBig;
			else {
				form.icon = url;
				delete fieldErrors.icon;
			}
		};
		reader.onerror = () => (fieldErrors.icon = 'Dit bestand kon niet worden gelezen.');
		reader.readAsDataURL(file);
	}

	async function saveApp(e: SubmitEvent) {
		e.preventDefault();
		if (saving) return;
		const bad = appFormError(form);
		fieldErrors = bad ? { [bad.field]: bad.message } : {};
		formError = '';
		if (bad) return void document.getElementById(`app-${bad.field}`)?.focus();
		saving = true;
		const body = { name: form.name.trim(), url: form.url.trim(), color: form.color.toLowerCase(), icon: form.icon ?? '' };
		try {
			if (editing) {
				const updated = await api.send<App>('PUT', `/apps/${editing.id}`, body);
				apps = apps!.map((a) => (a.id === updated.id ? updated : a));
				toast('Opgeslagen');
			} else {
				const created = await api.send<App>('POST', '/apps', body);
				apps = [...apps!, created];
				toast(`${created.name} toegevoegd`);
			}
			sheetOpen = false;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'invalid' && typeof err.data.field === 'string')
				fieldErrors = { [err.data.field]: err.message };
			else formError = err instanceof ApiError ? err.message : 'Opslaan is niet gelukt.';
		} finally {
			saving = false;
		}
	}

	async function deleteApp() {
		if (!editing) return;
		if (!confirmDelete) return void (confirmDelete = true);
		saving = true;
		try {
			await api.send('DELETE', `/apps/${editing.id}`);
			apps = apps!.filter((a) => a.id !== editing!.id);
			toast(`${editing.name} verwijderd`);
			sheetOpen = false;
		} catch (err) {
			formError = err instanceof ApiError ? err.message : 'Verwijderen is niet gelukt.';
		} finally {
			saving = false;
		}
	}

	const initial = (name: string) => Array.from(name.trim())[0]?.toUpperCase() ?? '?';
	const iconSrc = (icon: string | null | undefined) => (icon?.startsWith('data:image/') ? icon : null);
</script>

{#snippet tile(name: string, color: string, icon: string | null | undefined, size: number)}
	<span class="tile" style:--tile-color={color} style:--size="{size}px" aria-hidden="true">
		{#if iconSrc(icon)}
			<!-- Icons are only ever rendered through <img src>, never inlined. -->
			<img src={iconSrc(icon)} alt="" draggable="false" />
		{:else}
			<span>{initial(name)}</span>
		{/if}
	</span>
{/snippet}

<div class="pane-head">
	<h2 class="pane-title">Apps</h2>
	{#if apps}
		<button type="button" class="btn small primary" onclick={() => openEditor(null)}>
			<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 4.5v11M4.5 10h11" /></svg>
			Nieuwe app
		</button>
	{/if}
</div>
<p class="pane-intro">De tegels op je startpagina. Sleep aan de greep om de volgorde te wijzigen.</p>

{#if loadError}
	<div class="empty-note">
		<p>{loadError}</p>
		<button type="button" class="btn small" onclick={load}>Opnieuw proberen</button>
	</div>
{:else if !apps}
	<div class="loading-note" aria-label="Laden"><span class="spinner"></span></div>
{:else if apps.length === 0}
	<div class="empty">
		<p class="empty-title">Nog geen apps</p>
		<p>Voeg je eerste app toe; hij verschijnt meteen op je startpagina.</p>
		<button type="button" class="btn primary" onclick={() => openEditor(null)}>Eerste app toevoegen</button>
	</div>
{:else}
	<ul class="card list" class:dragging={drag} class:committing bind:this={list} aria-describedby="reorder-help">
		{#each apps as app, i (app.id)}
			<li
				data-id={app.id}
				class="app-row"
				class:lifted={drag?.id === app.id}
				class:settling={settling && drag?.id === app.id}
				style:transform={drag?.id === app.id
					? `translateY(${drag.dy}px)${settling ? '' : ' scale(1.02)'}`
					: shift(i)
						? `translateY(${shift(i)}px)`
						: undefined}
				animate:flip={{ duration: committing ? 0 : 450, easing: spring }}
				out:fade={{ duration: 160 }}
			>
				<button
					type="button"
					class="handle"
					aria-label="Verplaats {app.name} (pijltjes omhoog/omlaag)"
					onpointerdown={(e) => dragStart(e, i)}
					onpointermove={dragMove}
					onpointerup={dragEnd}
					onpointercancel={dragEnd}
					onkeydown={(e) => handleKey(e, i)}
				>
					<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 7h10M5 10h10M5 13h10" /></svg>
				</button>
				<button type="button" class="open" onclick={() => openEditor(app)}>
					{@render tile(app.name, app.color, app.icon, 40)}
					<span class="text">
						<span class="label">{app.name}</span>
						<span class="host">{hostOf(app.url) || app.url}</span>
					</span>
				</button>
				<span class="steps">
					<button type="button" class="step up" aria-label="{app.name} omhoog" disabled={i === 0} onclick={() => move(i, i - 1, '.up')}>
						<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5.5 12.5 10 8l4.5 4.5" /></svg>
					</button>
					<button
						type="button"
						class="step down"
						aria-label="{app.name} omlaag"
						disabled={i === apps.length - 1}
						onclick={() => move(i, i + 1, '.down')}
					>
						<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5.5 7.5 10 12l4.5-4.5" /></svg>
					</button>
				</span>
				<svg class="chev" viewBox="0 0 20 20" aria-hidden="true"><path d="M8 5l5 5-5 5" /></svg>
			</li>
		{/each}
	</ul>
	<p class="foot" id="reorder-help">Tip: met het toetsenbord verplaats je een app met de pijltjestoetsen op de greep.</p>
{/if}
<p class="visually-hidden" role="status" aria-live="polite">{announce}</p>

<Sheet bind:open={sheetOpen} title={editing ? 'App wijzigen' : 'Nieuwe app'}>
	<form class="form" onsubmit={saveApp} novalidate>
		<div class="preview">
			{@render tile(form.name || '?', form.color, form.icon, 76)}
			<span class="preview-name">{form.name.trim() || 'Naam'}</span>
		</div>

		<label class="field">
			<span>Naam</span>
			<input
				id="app-name"
				class="input"
				maxlength="40"
				placeholder="Jellyfin"
				aria-invalid={fieldErrors.name ? 'true' : undefined}
				bind:value={form.name}
			/>
			{#if fieldErrors.name}<small class="field-error" role="alert">{fieldErrors.name}</small>{/if}
		</label>
		<label class="field">
			<span>Adres</span>
			<input
				id="app-url"
				class="input"
				type="url"
				inputmode="url"
				autocapitalize="none"
				spellcheck="false"
				placeholder="https://jellyfin.home.lan"
				aria-invalid={fieldErrors.url ? 'true' : undefined}
				bind:value={form.url}
			/>
			{#if fieldErrors.url}<small class="field-error" role="alert">{fieldErrors.url}</small>{/if}
		</label>

		<fieldset class="field">
			<legend class="field-label">Kleur</legend>
			<div class="swatches">
				{#each COLORS as c}
					<label class="swatch" style:--c={c} title={c}>
						<input type="radio" name="color" value={c} bind:group={form.color} aria-label="Kleur {c}" />
					</label>
				{/each}
				<label class="swatch custom" title="Eigen kleur" style:--c={COLORS.includes(form.color) ? 'transparent' : form.color}>
					<input id="app-color" type="color" bind:value={form.color} aria-label="Eigen kleur" />
				</label>
			</div>
			{#if fieldErrors.color}<small class="field-error" role="alert">{fieldErrors.color}</small>{/if}
		</fieldset>

		<div class="field">
			<span class="field-label">Icoon</span>
			<div class="icon-row">
				<button type="button" class="btn small" onclick={() => fileInput?.click()}>
					{form.icon ? 'Ander icoon kiezen' : 'Icoon kiezen'}
				</button>
				{#if form.icon}
					<button type="button" class="btn small danger" onclick={() => (form.icon = null)}>Verwijder icoon</button>
				{/if}
				<input
					id="app-icon"
					class="visually-hidden"
					type="file"
					accept={ICON_TYPES.join(',')}
					bind:this={fileInput}
					onchange={pickIcon}
					tabindex="-1"
				/>
			</div>
			{#if fieldErrors.icon}
				<small class="field-error" role="alert">{fieldErrors.icon}</small>
			{:else}
				<small>PNG, SVG of WebP, maximaal 64 kB. Zonder icoon toont de tegel de eerste letter.</small>
			{/if}
		</div>

		{#if formError}<p class="error" role="alert">{formError}</p>{/if}

		<div class="sheet-actions">
			{#if editing}
				<button type="button" class="btn danger" class:filled={confirmDelete} onclick={deleteApp} disabled={saving}>
					{confirmDelete ? 'Zeker weten?' : 'Verwijder'}
				</button>
			{/if}
			<button class="btn primary" disabled={saving}>
				{#if saving}<span class="spinner" aria-hidden="true"></span>{/if}
				{editing ? 'Bewaar' : 'Voeg toe'}
			</button>
		</div>
	</form>
</Sheet>

<style>
	.pane-head .btn svg {
		width: 18px;
		height: 18px;
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
	}
	.list {
		margin: 0;
		padding: 0;
		list-style: none;
		overflow: visible;
	}
	.app-row {
		display: flex;
		align-items: center;
		gap: 4px;
		min-height: 64px;
		padding: 0 10px 0 4px;
		background: var(--card-bg);
		transition: transform var(--spring-dur) var(--spring);
	}
	.app-row:first-child {
		border-radius: var(--radius-m) var(--radius-m) 0 0;
	}
	.app-row:last-child {
		border-radius: 0 0 var(--radius-m) var(--radius-m);
	}
	.app-row:only-child {
		border-radius: var(--radius-m);
	}
	.app-row.lifted {
		z-index: 2;
		transition: none;
		border-radius: var(--radius-m);
		background: var(--menu-bg);
		box-shadow: var(--popover-shadow);
	}
	.app-row.lifted::before,
	.dragging .app-row::before {
		display: none;
	}
	.app-row.settling {
		transition: transform 320ms var(--spring);
	}
	.committing .app-row {
		transition: none;
	}

	.handle {
		flex: none;
		display: grid;
		place-items: center;
		width: 40px;
		height: 48px;
		border: 0;
		border-radius: 10px;
		background: none;
		color: var(--text-3);
		cursor: grab;
		touch-action: none;
	}
	.handle:active,
	.lifted .handle {
		cursor: grabbing;
		color: var(--text);
	}
	.handle svg,
	.step svg,
	.chev {
		width: 20px;
		height: 20px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.open {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 4px;
		border: 0;
		background: none;
		text-align: left;
		cursor: pointer;
		border-radius: 10px;
	}
	.text {
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.label {
		font-size: 1rem;
		font-weight: 500;
		letter-spacing: -0.006em;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.host {
		font-size: 0.8125rem;
		color: var(--text-2);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.chev {
		flex: none;
		color: var(--text-3);
		width: 16px;
		height: 16px;
	}

	/* Move buttons: for keyboard and switch users; appear when the row has focus. */
	.steps {
		display: flex;
		gap: 2px;
		opacity: 0;
		width: 0;
		overflow: hidden;
	}
	.app-row:focus-within .steps {
		opacity: 1;
		width: auto;
	}
	.step {
		display: grid;
		place-items: center;
		width: 32px;
		height: 32px;
		border: 0;
		border-radius: 50%;
		background: var(--hover);
		color: var(--text-2);
		cursor: pointer;
	}
	.step:disabled {
		opacity: 0.3;
		cursor: default;
	}

	.tile {
		position: relative;
		flex: none;
		display: grid;
		place-items: center;
		width: var(--size);
		height: var(--size);
		border-radius: 22.5%;
		background: var(--tile-color);
		background-image: linear-gradient(180deg, rgba(255, 255, 255, 0.18), rgba(255, 255, 255, 0) 55%);
		box-shadow: 0 2px 6px rgba(0, 0, 0, 0.12);
		overflow: hidden;
		color: #fff;
		font-size: calc(var(--size) * 0.42);
		font-weight: 600;
		transition: background-color 200ms ease;
	}
	@supports (corner-shape: squircle) {
		.tile {
			border-radius: 30%;
			corner-shape: squircle;
		}
	}
	.tile img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}

	.empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		padding: 40px 16px;
		text-align: center;
		color: var(--text-2);
	}
	.empty p {
		margin: 0;
	}
	.empty-title {
		color: var(--text);
		font-size: 1.0625rem;
		font-weight: 600;
	}
	.empty .btn {
		margin-top: 10px;
	}
	.foot {
		margin: 8px 16px 0;
		font-size: 0.8125rem;
		color: var(--text-3);
	}
	@media (pointer: coarse) {
		.foot {
			display: none;
		}
	}

	.preview {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		padding: 6px 0 4px;
	}
	.preview-name {
		font-size: 0.8125rem;
		font-weight: 500;
		max-width: 160px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	fieldset {
		margin: 0;
		padding: 0;
		border: 0;
		min-width: 0;
	}
	legend {
		padding: 0;
		margin-bottom: 6px;
	}
	.swatches {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
	}
	.swatch {
		position: relative;
		width: 30px;
		height: 30px;
		border-radius: 50%;
		background: var(--c);
		box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.08);
		cursor: pointer;
		transition: transform 200ms var(--spring-bounce);
	}
	.swatch:has(input:checked) {
		box-shadow:
			0 0 0 2px var(--bg-base),
			0 0 0 4px var(--c);
	}
	.swatch:has(input:focus-visible) {
		outline: 3px solid var(--focus-ring);
		outline-offset: 3px;
	}
	.swatch:active {
		transform: scale(0.9);
		transition-duration: 90ms;
	}
	.swatch input {
		position: absolute;
		inset: 0;
		opacity: 0;
		margin: 0;
		cursor: pointer;
	}
	.swatch.custom {
		background:
			radial-gradient(circle, var(--c) 0 42%, transparent 44%),
			conic-gradient(#ff3b30, #ffcc00, #34c759, #30b0c7, #007aff, #af52de, #ff3b30);
	}
	.swatch.custom:not([style*='transparent']) {
		box-shadow:
			0 0 0 2px var(--bg-base),
			0 0 0 4px var(--c);
	}
	.icon-row {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
</style>
