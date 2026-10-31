<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { ICON_TYPES, iconError } from '$lib/appEdit';
	import { applyBrand, brandError, DEFAULT_BRAND, logoSrc, type Branding } from '$lib/brand.svelte';
	import { toast } from '$lib/toast.svelte';

	const ACCENTS = ['#0f6cbd', '#0a7a3f', '#5e5ce6', '#bf3989', '#d70015', '#c2410c', '#0e7490', '#1d1d1f'];
	const BACKGROUNDS = ['#f5f5f5', '#ffffff', '#f7f6f2', '#f2f6f3', '#f1f4f9', '#f7f3f6'];

	let saved = $state<Branding | null>(null);
	let form = $state<Branding>({ ...DEFAULT_BRAND });
	let errors = $state<Partial<Record<keyof Branding, string>>>({});
	let loadError = $state('');
	let saving = $state(false);
	let fileInput = $state<HTMLInputElement | null>(null);

	const dirty = $derived(saved !== null && JSON.stringify(saved) !== JSON.stringify(form));
	const isDefault = $derived(JSON.stringify(form) === JSON.stringify(DEFAULT_BRAND));

	onMount(async () => {
		try {
			saved = await api.get<Branding>('/admin/branding');
			form = { ...saved };
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'The branding could not be loaded.';
		}
	});

	function pickLogo(e: Event & { currentTarget: HTMLInputElement }) {
		const file = e.currentTarget.files?.[0];
		e.currentTarget.value = '';
		if (!file) return;
		const bad = iconError(file);
		if (bad) return void (errors.logo = 'Choose a PNG, SVG or WebP image of at most 64 kB.');
		const reader = new FileReader();
		reader.onload = () => {
			const url = String(reader.result);
			if (iconError(file, url)) errors.logo = 'The logo may be at most 64 kB. Use a smaller image, for example 256 × 256 px.';
			else {
				form.logo = url;
				delete errors.logo;
			}
		};
		reader.onerror = () => (errors.logo = 'This file could not be read.');
		reader.readAsDataURL(file);
	}

	let photoInput = $state<HTMLInputElement | null>(null);
	let photoBusy = $state(false);

	/** Scales a photo to at most 2560 px wide and stores it as JPEG (well under 3 MB). */
	async function pickPhoto(e: Event & { currentTarget: HTMLInputElement }) {
		const file = e.currentTarget.files?.[0];
		e.currentTarget.value = '';
		if (!file) return;
		if (!/^image\/(jpeg|png|webp)$/.test(file.type)) return void (errors.login_image = 'Choose a JPEG, PNG or WebP image.');
		photoBusy = true;
		try {
			const bmp = await createImageBitmap(file);
			const scale = Math.min(1, 2560 / bmp.width);
			const canvas = document.createElement('canvas');
			canvas.width = Math.round(bmp.width * scale);
			canvas.height = Math.round(bmp.height * scale);
			canvas.getContext('2d')!.drawImage(bmp, 0, 0, canvas.width, canvas.height);
			let q = 0.85;
			let url = canvas.toDataURL('image/jpeg', q);
			while (url.length > 3_000_000 && q > 0.4) url = canvas.toDataURL('image/jpeg', (q -= 0.15));
			form.login_image = url;
			delete errors.login_image;
		} catch {
			errors.login_image = 'This image could not be read.';
		} finally {
			photoBusy = false;
		}
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (saving) return;
		errors = brandError(form);
		if (Object.keys(errors).length) return;
		saving = true;
		try {
			saved = await api.send<Branding>('PUT', '/admin/branding', form);
			form = { ...saved };
			applyBrand(saved);
			toast('Branding saved');
		} catch (err) {
			if (err instanceof ApiError && err.code === 'invalid' && typeof err.data.field === 'string')
				errors = { [err.data.field]: err.message };
			else toast(err instanceof ApiError ? err.message : 'The branding could not be saved.', 'error');
		} finally {
			saving = false;
		}
	}
</script>

<div class="pane-head"><h1 class="pane-title">Branding</h1></div>
<p class="pane-intro">The name, logo and colours on every page, the sign-in page included.</p>

{#if loadError}
	<p class="error" role="alert">{loadError}</p>
{:else if !saved}
	<p class="loading-note" role="status">Loading…</p>
{:else}
	<form onsubmit={save} novalidate>
		<div class="cols">
		<section class="group" aria-labelledby="b-identity">
			<h2 id="b-identity">Name and logo</h2>
			<div class="card">
				<div class="srow stack">
					<label class="field">
						<span>Name</span>
						<input class="input" maxlength="40" bind:value={form.name} aria-invalid={errors.name ? 'true' : undefined} />
						{#if errors.name}<small class="field-error" role="alert">{errors.name}</small>{/if}
					</label>
				</div>
				<div class="srow">
					<img class="logo" src={logoSrc(form.logo)} alt="" width="48" height="48" />
					<div class="text">
						<span class="label">Logo</span>
						<p class="help">PNG, SVG or WebP, at most 64 kB. Also the browser tab icon.</p>
						{#if errors.logo}<small class="field-error" role="alert">{errors.logo}</small>{/if}
					</div>
					<div class="control">
						{#if form.logo}
							<button type="button" class="btn btn-secondary btn-sm" onclick={() => (form.logo = '')}>Use default</button>
						{/if}
						<button type="button" class="btn btn-secondary btn-sm" onclick={() => fileInput?.click()}>Choose file</button>
						<input class="visually-hidden" type="file" accept={ICON_TYPES.join(',')} bind:this={fileInput} onchange={pickLogo} tabindex="-1" />
					</div>
				</div>
			</div>
		</section>

		<section class="group" aria-labelledby="b-colours">
			<h2 id="b-colours">Colours</h2>
			<div class="card">
				{@render picker('accent', 'Accent', 'Buttons, links and the selected menu item.', ACCENTS)}
				{@render picker('background', 'Background', 'The page behind the white boxes.', BACKGROUNDS)}
			</div>
		</section>

		<section class="group" aria-labelledby="b-login">
			<h2 id="b-login">Sign-in page</h2>
			<div class="card">
				<div class="srow stack">
					<div
						class="login-thumb"
						style:--primary={form.accent}
						style:background={form.login_image ? `url("${form.login_image}") center / cover no-repeat` : undefined}
					>
						<span class="mini-card"><img src={logoSrc(form.logo)} alt="" width="18" height="18" />{form.name.trim() || DEFAULT_BRAND.name}</span>
					</div>
					<div class="thumb-actions">
						<span class="help">{form.login_image ? 'Your photo.' : 'A soft gradient in the accent colour.'}</span>
						<span class="control">
							{#if form.login_image}
								<button type="button" class="btn btn-secondary btn-sm" onclick={() => (form.login_image = '')}>Use gradient</button>
							{/if}
							<button type="button" class="btn btn-secondary btn-sm" onclick={() => photoInput?.click()} disabled={photoBusy}>
								{#if photoBusy}<span class="spinner" aria-hidden="true"></span>{/if}Choose photo
							</button>
						</span>
						<input class="visually-hidden" type="file" accept="image/jpeg,image/png,image/webp" bind:this={photoInput} onchange={pickPhoto} tabindex="-1" />
					</div>
					{#if errors.login_image}<small class="field-error" role="alert">{errors.login_image}</small>{/if}
				</div>
			</div>
		</section>

		<section class="group" aria-labelledby="b-preview">
			<h2 id="b-preview">Preview</h2>
			<div class="preview" style:--primary={form.accent} style:--color-bg={form.background}>
				<div class="pv-bar">
					<img src={logoSrc(form.logo)} alt="" width="22" height="22" />
					<strong>{form.name.trim() || DEFAULT_BRAND.name}</strong>
				</div>
				<div class="pv-body">
					<div class="pv-nav"><span class="pv-item active">My apps</span><span class="pv-item">Settings</span></div>
					<div class="pv-box">
						<p>Sign in to continue. <a href="#preview" onclick={(e) => e.preventDefault()}>Forgot password?</a></p>
						<span class="btn btn-primary btn-sm" aria-hidden="true">Sign in</span>
					</div>
				</div>
			</div>
		</section>

		</div>

		<div class="actions">
			{#if !isDefault}
				<button type="button" class="btn btn-secondary" onclick={() => ((form = { ...DEFAULT_BRAND }), (errors = {}))}>Reset to default</button>
			{/if}
			<button class="btn btn-primary" disabled={saving || !dirty}>
				{#if saving}<span class="spinner" aria-hidden="true"></span>{/if}
				Save
			</button>
		</div>
	</form>
{/if}

{#snippet picker(key: 'accent' | 'background', label: string, help: string, presets: string[])}
	<div class="srow stack">
		<div class="text">
			<span class="label" id="lbl-{key}">{label}</span>
			<p class="help">{help}</p>
		</div>
		<div class="swatches" role="radiogroup" aria-labelledby="lbl-{key}">
			{#each presets as c}
				<label class="swatch" style:--c={c} title={c}>
					<input type="radio" name={key} value={c} bind:group={form[key]} aria-label={c} />
				</label>
			{/each}
			<label class="swatch custom" class:on={!presets.includes(form[key])} style:--c={form[key]} title="Custom colour">
				<input type="color" bind:value={form[key]} aria-label="Custom {label.toLowerCase()} colour" />
			</label>
			<span class="hex mono">{form[key]}</span>
		</div>
		{#if errors[key]}<small class="field-error" role="alert">{errors[key]}</small>{/if}
	</div>
{/snippet}

<style>
	.login-thumb {
		display: grid;
		place-items: center;
		height: 150px;
		border-radius: var(--radius-card);
		background:
			radial-gradient(120% 90% at 15% 10%, color-mix(in srgb, var(--primary) 55%, #fff) 0%, transparent 60%),
			radial-gradient(110% 90% at 90% 100%, color-mix(in srgb, var(--primary) 70%, #1d1d1f) 0%, transparent 65%),
			color-mix(in srgb, var(--primary) 35%, #f5f5f5);
	}
	.mini-card {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 10px 16px;
		border-radius: 8px;
		background: var(--c-bg-card);
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.18);
		font-size: 0.875rem;
		font-weight: 600;
	}
	.mini-card img {
		object-fit: contain;
	}
	.thumb-actions {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
	}
	/* Groups side by side on a wide pane; a lone group fills the width. */
	.cols {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(440px, 1fr));
		gap: 0 20px;
		align-items: start;
	}
	.cols > .group {
		break-inside: avoid;
	}
	.logo {
		flex: none;
		width: 48px;
		height: 48px;
		object-fit: contain;
	}
	.swatches {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
	}
	.swatch {
		position: relative;
		width: 28px;
		height: 28px;
		border-radius: var(--radius-sm);
		background: var(--c);
		box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.12);
		cursor: pointer;
	}
	.swatch:has(input:checked),
	.swatch.custom.on {
		box-shadow:
			0 0 0 2px var(--c-bg-card),
			0 0 0 4px var(--color-text);
	}
	.swatch:has(input:focus-visible) {
		outline: 2px solid var(--focus-ring);
		outline-offset: 5px;
	}
	.swatch input {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		margin: 0;
		opacity: 0;
		cursor: pointer;
	}
	.swatch.custom:not(.on) {
		background: conic-gradient(#ff3b30, #ffcc00, #34c759, #00c7be, #007aff, #af52de, #ff3b30);
	}
	.hex {
		color: var(--color-text-muted);
		font-size: 0.8125rem;
	}

	/* A miniature of the app in the chosen colours. */
	.preview {
		overflow: hidden;
		border: 1px solid var(--c-border);
		border-radius: var(--radius-card);
		background: var(--color-bg);
	}
	.pv-bar {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 10px 14px;
		border-bottom: 1px solid var(--c-border);
		background: var(--c-bg-card);
		font-size: 0.875rem;
	}
	.pv-bar img {
		object-fit: contain;
	}
	.pv-body {
		display: flex;
		gap: 16px;
		padding: 16px;
	}
	.pv-nav {
		display: flex;
		flex-direction: column;
		gap: 2px;
		width: 120px;
		flex: none;
	}
	.pv-item {
		padding: 5px 8px;
		border-radius: var(--radius-sm);
		font-size: 0.8125rem;
	}
	.pv-item.active {
		background: var(--c-selected-mid);
		color: color-mix(in srgb, var(--primary) 86%, #000);
		font-weight: 500;
	}
	.pv-box {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 10px;
		padding: 14px;
		border: 1px solid var(--c-border);
		border-radius: var(--radius-card);
		background: var(--c-bg-card);
		font-size: 0.875rem;
	}
	.pv-box p {
		margin: 0;
	}
	.pv-box a {
		color: var(--primary);
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
	}
	@media (max-width: 520px) {
		.pv-nav {
			display: none;
		}
	}
</style>
