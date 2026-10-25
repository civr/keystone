<script lang="ts">
	import { securityGroups } from '$lib/settingsMeta';
	import SettingsEditor from '$lib/components/settings/SettingsEditor.svelte';

	const tabs = [
		{ id: 'sessions', label: 'Sessions', groups: ['sessions'] },
		{ id: 'sign-in', label: 'Sign-in', groups: ['sign-in', 'brute-force'] }
	];
	let tab = $state(tabs[0].id);
	const shown = $derived(tabs.find((t) => t.id === tab)!.groups);
</script>

<div class="pane-head"><h1 class="pane-title">Sign-in &amp; MFA</h1></div>
<p class="pane-intro">How people sign in, how long they stay signed in, and how guessing is stopped. Changes ask you to confirm and are logged.</p>

<div class="segmented tabs" role="tablist" aria-label="Sections">
	{#each tabs as t (t.id)}
		<label><input type="radio" name="tab" value={t.id} bind:group={tab} /><span>{t.label}</span></label>
	{/each}
</div>

<SettingsEditor groups={securityGroups} visible={shown} />

<style>
	.tabs {
		margin: 0 0 20px;
	}
</style>
