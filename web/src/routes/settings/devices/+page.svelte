<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { flip } from 'svelte/animate';
	import { fade } from 'svelte/transition';
	import { api, ApiError } from '$lib/api';
	import { ceremony, passkeyError, supported } from '$lib/webauthn';
	import { describeUA, relativeTime } from '$lib/format';
	import { easeOut, reducedMotion } from '$lib/motion';
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
			loadError = e instanceof ApiError ? e.message : 'Devices could not be loaded.';
		}
	}
	onMount(load);

	const err = (e: unknown, fallback: string) => toast(e instanceof ApiError ? e.message : fallback, 'error');

	async function revoke(s: Session) {
		const ok = await confirmer.ask(
			s.current
				? {
						title: 'Sign out here?',
						message: 'You are signed out on this device and go to the sign-in page.',
						confirm: 'Sign out',
						danger: true
					}
				: {
						title: 'Sign out this session?',
						message: `${describeUA(s.user_agent)} (${s.ip}) has to sign in again.`,
						confirm: 'Sign out',
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
			err(e, 'Signing out failed.');
		} finally {
			busy = '';
		}
	}

	async function addPasskey() {
		busy = 'add';
		try {
			await ceremony('/admin/passkeys/begin', '/admin/passkeys/finish');
			passkeys = (await api.get<{ passkeys: Passkey[] }>('/admin/passkeys')).passkeys;
			toast('Passkey added');
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
			err(e, 'Renaming failed.');
		} finally {
			busy = '';
		}
	}

	async function removePasskey(p: Passkey) {
		const ok = await confirmer.ask({
			title: `Remove “${p.name}”?`,
			message: 'You can no longer sign in with this passkey.',
			confirm: 'Remove',
			danger: true
		});
		if (!ok) return;
		busy = p.id;
		try {
			await api.send('DELETE', `/admin/passkeys/${encodeURIComponent(p.id)}`);
			passkeys = passkeys!.filter((x) => x.id !== p.id);
			toast('Passkey removed');
		} catch (e) {
			err(e, 'Removing failed.');
		} finally {
			busy = '';
		}
	}

	const still = reducedMotion();
	const dateFmt = new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'long', year: 'numeric' });
</script>

<div class="pane-head"><h1 class="pane-title">Devices</h1></div>
<p class="pane-intro">Where you are signed in, and the passkeys you can sign in with.</p>

{#if loadError}
	<div class="empty-note">
		<p>{loadError}</p>
		<button type="button" class="btn btn-secondary btn-sm" onclick={load}>Try again</button>
	</div>
{:else if !sessions || !passkeys}
	<div class="loading-note" aria-label="Loading"><span class="spinner"></span></div>
{:else}
	<section class="group" aria-labelledby="g-sessions">
		<h2 id="g-sessions">Signed in</h2>
		<ul class="card list">
			{#each sessions as s (s.id)}
				<li class="srow wrap-phone" animate:flip={{ duration: still ? 0 : 200, easing: easeOut }} out:fade={{ duration: still ? 0 : 150 }}>
					<span class="glyph" aria-hidden="true">
						<svg viewBox="0 0 20 20"><path d="M2.5 4.5h11v8h-11zM6 15.5h4M15 7.5h3v8.5h-3z" /></svg>
					</span>
					<div class="text">
						<span class="label">
							{describeUA(s.user_agent)}
							{#if s.current}<span class="chip accent">This device</span>{/if}
							{#if s.emergency}<span class="chip warn">Emergency account</span>{/if}
						</span>
						<p class="help">{s.ip} · active {relativeTime(s.last_seen_at, now)} · since {dateFmt.format(new Date(s.created_at))}</p>
					</div>
					<div class="control">
						<button type="button" class="btn btn-secondary btn-sm danger-ink" disabled={busy === s.id} onclick={() => revoke(s)}>
							{s.current ? 'Sign out here' : 'Sign out'}
						</button>
					</div>
				</li>
			{/each}
		</ul>
	</section>

	<section class="group" aria-labelledby="g-passkeys">
		<div class="group-head">
			<h2 id="g-passkeys">Passkeys</h2>
			{#if supported()}
				<button type="button" class="btn btn-secondary btn-sm" onclick={addPasskey} disabled={busy === 'add'}>
					{#if busy === 'add'}<span class="spinner" aria-hidden="true"></span>{:else}
						<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 4.5v11M4.5 10h11" /></svg>
					{/if}
					Add passkey
				</button>
			{/if}
		</div>
		<ul class="card list">
			{#each passkeys as p (p.id)}
				<li class="srow wrap-phone" animate:flip={{ duration: still ? 0 : 200, easing: easeOut }} out:fade={{ duration: still ? 0 : 150 }}>
					<span class="glyph" aria-hidden="true">
						<svg viewBox="0 0 20 20"
							><path d="M7.5 3.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7zM10.3 9.3l6.2 6.2M14 13l1.8-1.8M12.4 11.4l1.6-1.6" /></svg
						>
					</span>
					<div class="text">
						{#if renaming === p.id}
							<form class="rename" onsubmit={(e) => (e.preventDefault(), saveRename(p))}>
								<label class="visually-hidden" for="rn-{p.id}">Passkey name</label>
								<input
									id="rn-{p.id}"
									class="input"
									maxlength="64"
									bind:this={renameInput}
									bind:value={newName}
									onkeydown={(e) => e.key === 'Escape' && (e.stopPropagation(), (renaming = null))}
								/>
								<button class="btn btn-primary btn-sm" disabled={busy === p.id}>Save</button>
							</form>
						{:else}
							<span class="label">{p.name}</span>
						{/if}
						<p class="help">
							Added {dateFmt.format(new Date(p.created_at))} · last used {relativeTime(p.last_used_at, now)}
						</p>
					</div>
					{#if renaming !== p.id}
						<div class="control row-actions">
							<button type="button" class="btn btn-secondary btn-sm" onclick={() => startRename(p)}>Rename</button>
							<button
								type="button"
								class="btn btn-secondary btn-sm danger-ink"
								disabled={busy === p.id || passkeys.length === 1}
								title={passkeys.length === 1 ? 'Your last passkey cannot be removed.' : undefined}
								onclick={() => removePasskey(p)}>Remove</button
							>
						</div>
					{/if}
				</li>
			{/each}
		</ul>
		{#if passkeys.length === 1}
			<p class="foot">Your last passkey cannot be removed. Add a second one first, for example on your phone.</p>
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
	/* A small grey rounded-square glyph, as in Settings rows. */
	.glyph {
		flex: none;
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border-radius: 7px;
		background: linear-gradient(180deg, #a5abb8, #858994);
		color: #fff;
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
	.rename {
		display: flex;
		gap: 8px;
		align-items: center;
	}
	.rename .input {
		max-width: 260px;
	}
	.label .chip {
		margin-left: 6px;
	}
</style>
