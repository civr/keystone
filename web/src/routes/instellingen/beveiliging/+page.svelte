<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { securityGroups } from '$lib/settingsMeta';
	import { codesFile, download } from '$lib/codes';
	import { toast } from '$lib/toast.svelte';
	import SettingsEditor from '$lib/components/settings/SettingsEditor.svelte';
	import Sheet from '$lib/components/Sheet.svelte';
	import Confirm from '$lib/components/settings/Confirm.svelte';

	let confirmer: Confirm;

	// --- Herstelcodes ---
	let codes = $state<string[] | null>(null);
	let codesOpen = $state(false);
	let regenBusy = $state(false);

	async function regenerate() {
		const ok = await confirmer.ask({
			title: 'Nieuwe herstelcodes maken?',
			message: 'Je krijgt 10 nieuwe codes. Alle oude herstelcodes werken daarna niet meer.',
			confirm: 'Nieuwe codes maken'
		});
		if (!ok) return;
		regenBusy = true;
		try {
			const res = await api.send<{ recovery_codes: string[] }>('POST', '/admin/recovery/regenerate');
			codes = res.recovery_codes;
			codesOpen = true;
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Nieuwe codes maken is niet gelukt.', 'error');
		} finally {
			regenBusy = false;
		}
	}

	// --- Nood-account ---
	let emergency = $state<{ enabled: boolean; password_set: boolean } | null>(null);
	let emBusy = $state(false);
	let pwOpen = $state(false);
	let pw = $state('');
	let pw2 = $state('');
	let pwError = $state('');
	let enableAfterPassword = $state(false);

	onMount(() => {
		api.get<{ enabled: boolean; password_set: boolean }>('/admin/emergency').then(
			(r) => (emergency = r),
			() => toast('De status van het nood-account kon niet worden geladen.', 'error')
		);
	});

	async function putEmergency(body: { enabled: boolean; password?: string }) {
		emBusy = true;
		try {
			await api.send('PUT', '/admin/emergency', body);
			emergency = { enabled: body.enabled, password_set: emergency!.password_set || !!body.password };
			return true;
		} catch (e) {
			if (e instanceof ApiError && e.code === 'weak_password') pwError = e.message;
			else toast(e instanceof ApiError ? e.message : 'Opslaan is niet gelukt.', 'error');
			return false;
		} finally {
			emBusy = false;
		}
	}

	async function toggleEmergency(e: Event & { currentTarget: HTMLInputElement }) {
		const on = e.currentTarget.checked;
		e.currentTarget.checked = emergency!.enabled; // follows the server, not the click
		if (on && !emergency!.password_set) return openPassword(true);
		if (await putEmergency({ enabled: on })) toast(on ? 'Nood-account aan' : 'Nood-account uit');
	}

	function openPassword(enable: boolean) {
		enableAfterPassword = enable;
		pw = '';
		pw2 = '';
		pwError = '';
		pwOpen = true;
	}

	async function savePassword(e: SubmitEvent) {
		e.preventDefault();
		if (pw.length < 16) return void (pwError = 'Gebruik minstens 16 tekens.');
		if (pw !== pw2) return void (pwError = 'De wachtwoorden zijn niet gelijk.');
		pwError = '';
		const enabled = enableAfterPassword || emergency!.enabled;
		if (await putEmergency({ enabled, password: pw })) {
			pwOpen = false;
			toast(enableAfterPassword ? 'Nood-account aan' : 'Noodwachtwoord gewijzigd');
		}
	}
</script>

<div class="pane-head"><h2 class="pane-title">Beveiliging</h2></div>
<p class="pane-intro">
	Elke beveiligingsmaatregel van Keystone. Wijzigingen vragen om je passkey en komen in de activiteitenlog.
</p>

<SettingsEditor groups={securityGroups}>
	{#snippet after()}
		<section class="group" aria-labelledby="g-herstel">
			<h3 id="g-herstel">Herstel</h3>
			<div class="card">
				<div class="srow wrap-phone">
					<div class="text">
						<span class="label">Herstelcodes</span>
						<p class="help">Voor als je je passkey kwijt bent. Elke code werkt één keer.</p>
					</div>
					<div class="control">
						<button type="button" class="btn small" onclick={regenerate} disabled={regenBusy}>Nieuwe codes maken</button>
					</div>
				</div>
				<div class="srow">
					<div class="text">
						<label class="label" for="em-switch">Nood-account</label>
						<p class="help" id="em-help">
							Inloggen met een apart wachtwoord als AD niet bereikbaar is. Een passkey blijft nodig.
							{#if emergency?.enabled}<span class="badge warn">Aan</span>{/if}
						</p>
						{#if emergency?.password_set}
							<button type="button" class="link pw-link" onclick={() => openPassword(false)}>Noodwachtwoord wijzigen</button>
						{/if}
					</div>
					<div class="control">
						<input
							id="em-switch"
							class="switch"
							type="checkbox"
							role="switch"
							aria-describedby="em-help"
							checked={emergency?.enabled ?? false}
							disabled={!emergency || emBusy}
							onchange={toggleEmergency}
						/>
					</div>
				</div>
			</div>
		</section>
	{/snippet}
</SettingsEditor>

<Sheet bind:open={codesOpen} title="Nieuwe herstelcodes">
	{#if codes}
		<p class="sheet-msg">Bewaar ze op een veilige plek, bijvoorbeeld in je wachtwoordmanager. De oude codes werken niet meer.</p>
		<ul class="codes">
			{#each codes as c}<li>{c}</li>{/each}
		</ul>
		<div class="sheet-actions">
			<button type="button" class="btn" onclick={() => download('keystone-herstelcodes.txt', codesFile(codes!, location.origin))}>
				Download
			</button>
			<button type="button" class="btn primary" onclick={() => (codesOpen = false)}>Ik heb ze bewaard</button>
		</div>
	{/if}
</Sheet>

<Sheet bind:open={pwOpen} title={enableAfterPassword ? 'Nood-account aanzetten' : 'Noodwachtwoord wijzigen'}>
	<form class="form" onsubmit={savePassword} novalidate>
		<p class="sheet-msg">Kies een lang wachtwoord dat je nergens anders gebruikt (minstens 16 tekens).</p>
		<label class="field">
			<span>Noodwachtwoord</span>
			<input class="input" type="password" autocomplete="new-password" bind:value={pw} />
		</label>
		<label class="field">
			<span>Herhaal</span>
			<input class="input" type="password" autocomplete="new-password" bind:value={pw2} />
		</label>
		{#if pwError}<p class="error" role="alert">{pwError}</p>{/if}
		<div class="sheet-actions">
			<button type="button" class="btn" onclick={() => (pwOpen = false)}>Annuleren</button>
			<button class="btn primary" disabled={emBusy}>Opslaan</button>
		</div>
	</form>
</Sheet>

<Confirm bind:this={confirmer} />

<style>
	.sheet-msg {
		margin: 0;
		color: var(--text-2);
		font-size: 0.9375rem;
		line-height: 1.45;
	}
	.pw-link {
		margin: 4px 0 0 -4px;
		font-size: 0.875rem;
	}
	.badge {
		margin-left: 4px;
	}
</style>
