<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { flip } from 'svelte/animate';
	import { fade } from 'svelte/transition';
	import { api, ApiError } from '$lib/api';
	import { ceremony, passkeyError, supported } from '$lib/webauthn';
	import { describeUA, relativeTime } from '$lib/format';
	import { spring } from '$lib/motion';
	import { toast } from '$lib/toast.svelte';
	import Confirm from '$lib/components/settings/Confirm.svelte';

	interface Session {
		id: string;
		created_at: string;
		last_seen_at: string;
		ip: string;
		user_agent: string;
		emergency: boolean;
		current: boolean;
	}
	interface Passkey {
		id: string;
		name: string;
		created_at: string;
		last_used_at: string | null;
	}

	let sessions = $state<Session[] | null>(null);
	let passkeys = $state<Passkey[] | null>(null);
	let loadError = $state('');
	let busy = $state('');
	let renaming = $state<string | null>(null);
	let newName = $state('');
	let renameInput = $state<HTMLInputElement | null>(null);
	let confirmer: Confirm;
	let now = $state(new Date());

	async function load() {
		loadError = '';
		try {
			const [s, p] = await Promise.all([
				api.get<{ sessions: Session[] }>('/admin/sessions'),
				api.get<{ passkeys: Passkey[] }>('/admin/passkeys')
			]);
			sessions = s.sessions;
			passkeys = p.passkeys;
			now = new Date();
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Apparaten konden niet worden geladen.';
		}
	}
	onMount(load);

	const err = (e: unknown, fallback: string) => toast(e instanceof ApiError ? e.message : fallback, 'error');

	async function revoke(s: Session) {
		const ok = await confirmer.ask(
			s.current
				? {
						title: 'Hier afmelden?',
						message: 'Je wordt op dit apparaat afgemeld en gaat naar de inlogpagina.',
						confirm: 'Afmelden',
						danger: true
					}
				: {
						title: 'Sessie afmelden?',
						message: `${describeUA(s.user_agent)} (${s.ip}) moet daarna opnieuw inloggen.`,
						confirm: 'Afmelden',
						danger: true
					}
		);
		if (!ok) return;
		busy = s.id;
		try {
			await api.send('DELETE', `/admin/sessions/${encodeURIComponent(s.id)}`);
			if (s.current) return void location.assign('/login');
			sessions = sessions!.filter((x) => x.id !== s.id);
			toast('Afgemeld');
		} catch (e) {
			err(e, 'Afmelden is niet gelukt.');
		} finally {
			busy = '';
		}
	}

	async function addPasskey() {
		busy = 'add';
		try {
			await ceremony('/admin/passkeys/begin', '/admin/passkeys/finish');
			passkeys = (await api.get<{ passkeys: Passkey[] }>('/admin/passkeys')).passkeys;
			toast('Passkey toegevoegd');
		} catch (e) {
			toast(passkeyError(e), 'error');
		} finally {
			busy = '';
		}
	}

	async function startRename(p: Passkey) {
		renaming = p.id;
		newName = p.name;
		await tick();
		renameInput?.select();
	}

	async function saveRename(p: Passkey) {
		const name = newName.trim();
		if (!name || name === p.name) return void (renaming = null);
		busy = p.id;
		try {
			await api.send('PATCH', `/admin/passkeys/${encodeURIComponent(p.id)}`, { name });
			p.name = name;
			renaming = null;
		} catch (e) {
			err(e, 'Hernoemen is niet gelukt.');
		} finally {
			busy = '';
		}
	}

	async function removePasskey(p: Passkey) {
		const ok = await confirmer.ask({
			title: `“${p.name}” verwijderen?`,
			message: 'Je kunt daarna niet meer met deze passkey inloggen.',
			confirm: 'Verwijderen',
			danger: true
		});
		if (!ok) return;
		busy = p.id;
		try {
			await api.send('DELETE', `/admin/passkeys/${encodeURIComponent(p.id)}`);
			passkeys = passkeys!.filter((x) => x.id !== p.id);
			toast('Passkey verwijderd');
		} catch (e) {
			err(e, 'Verwijderen is niet gelukt.');
		} finally {
			busy = '';
		}
	}

	const dateFmt = new Intl.DateTimeFormat('nl-NL', { day: 'numeric', month: 'long', year: 'numeric' });
</script>

<div class="pane-head"><h2 class="pane-title">Apparaten</h2></div>
<p class="pane-intro">Waar je bent aangemeld en met welke passkeys je kunt inloggen.</p>

{#if loadError}
	<div class="empty-note">
		<p>{loadError}</p>
		<button type="button" class="btn small" onclick={load}>Opnieuw proberen</button>
	</div>
{:else if !sessions || !passkeys}
	<div class="loading-note" aria-label="Laden"><span class="spinner"></span></div>
{:else}
	<section class="group" aria-labelledby="g-sessions">
		<h3 id="g-sessions">Aangemeld</h3>
		<ul class="card list">
			{#each sessions as s (s.id)}
				<li class="srow wrap-phone" animate:flip={{ duration: 400, easing: spring }} out:fade={{ duration: 160 }}>
					<span class="glyph" aria-hidden="true">
						<svg viewBox="0 0 20 20"><path d="M2.5 4.5h11v8h-11zM6 15.5h4M15 7.5h3v8.5h-3z" /></svg>
					</span>
					<div class="text">
						<span class="label">
							{describeUA(s.user_agent)}
							{#if s.current}<span class="badge">Dit apparaat</span>{/if}
							{#if s.emergency}<span class="badge warn">Nood-account</span>{/if}
						</span>
						<p class="help">{s.ip} · actief {relativeTime(s.last_seen_at, now)} · sinds {dateFmt.format(new Date(s.created_at))}</p>
					</div>
					<div class="control">
						<button type="button" class="btn small danger" disabled={busy === s.id} onclick={() => revoke(s)}>
							{s.current ? 'Hier afmelden' : 'Afmelden'}
						</button>
					</div>
				</li>
			{/each}
		</ul>
	</section>

	<section class="group" aria-labelledby="g-passkeys">
		<div class="group-head">
			<h3 id="g-passkeys">Passkeys</h3>
			{#if supported()}
				<button type="button" class="btn small" onclick={addPasskey} disabled={busy === 'add'}>
					{#if busy === 'add'}<span class="spinner" aria-hidden="true"></span>{:else}
						<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 4.5v11M4.5 10h11" /></svg>
					{/if}
					Passkey toevoegen
				</button>
			{/if}
		</div>
		<ul class="card list">
			{#each passkeys as p (p.id)}
				<li class="srow wrap-phone" animate:flip={{ duration: 400, easing: spring }} out:fade={{ duration: 160 }}>
					<span class="glyph" aria-hidden="true">
						<svg viewBox="0 0 20 20"
							><path d="M7.5 3.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7zM10.3 9.3l6.2 6.2M14 13l1.8-1.8M12.4 11.4l1.6-1.6" /></svg
						>
					</span>
					<div class="text">
						{#if renaming === p.id}
							<form class="rename" onsubmit={(e) => (e.preventDefault(), saveRename(p))}>
								<label class="visually-hidden" for="rn-{p.id}">Naam van de passkey</label>
								<input
									id="rn-{p.id}"
									class="input"
									maxlength="64"
									bind:this={renameInput}
									bind:value={newName}
									onkeydown={(e) => e.key === 'Escape' && (e.stopPropagation(), (renaming = null))}
								/>
								<button class="btn small primary" disabled={busy === p.id}>Bewaar</button>
							</form>
						{:else}
							<span class="label">{p.name}</span>
						{/if}
						<p class="help">
							Toegevoegd {dateFmt.format(new Date(p.created_at))} · laatst gebruikt {relativeTime(p.last_used_at, now)}
						</p>
					</div>
					{#if renaming !== p.id}
						<div class="control row-actions">
							<button type="button" class="btn small" onclick={() => startRename(p)}>Wijzig naam</button>
							<button
								type="button"
								class="btn small danger"
								disabled={busy === p.id || passkeys.length === 1}
								title={passkeys.length === 1 ? 'Je laatste passkey kan niet worden verwijderd.' : undefined}
								onclick={() => removePasskey(p)}>Verwijder</button
							>
						</div>
					{/if}
				</li>
			{/each}
		</ul>
		{#if passkeys.length === 1}
			<p class="foot">Je laatste passkey kan niet worden verwijderd. Voeg eerst een tweede toe, bijvoorbeeld op je telefoon.</p>
		{/if}
	</section>
{/if}

<Confirm bind:this={confirmer} />

<style>
	.list {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.glyph {
		flex: none;
		display: grid;
		place-items: center;
		width: 36px;
		height: 36px;
		border-radius: 10px;
		background: var(--seg-bg);
		color: var(--text-2);
	}
	.glyph svg,
	.group-head svg {
		width: 20px;
		height: 20px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.6;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.group-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		margin: 0 0 8px;
	}
	.group-head h3 {
		margin: 0 0 0 16px;
		font-size: 0.8125rem;
		font-weight: 600;
		letter-spacing: 0.02em;
		text-transform: uppercase;
		color: var(--text-2);
	}
	.rename {
		display: flex;
		gap: 8px;
		align-items: center;
	}
	.rename .input {
		height: 36px;
		max-width: 260px;
	}
	.label .badge {
		margin-left: 6px;
	}
</style>
