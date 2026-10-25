<script lang="ts">
	import { securityGroups } from '$lib/settingsMeta';
	import SettingsEditor from '$lib/components/settings/SettingsEditor.svelte';

	const tabs = [
		{ id: 'vault', label: 'Policy', groups: ['vault'] }
	];
	let tab = $state(tabs[0].id);
	const shown = $derived(tabs.find((t) => t.id === tab)!.groups);
</script>

<div class="pane-head"><h1 class="pane-title">Vault &amp; secrets</h1></div>
<p class="pane-intro">How passwords in the vault may be used. Passwords are sealed with the vault key, which is kept outside the database.</p>

<SettingsEditor groups={securityGroups} visible={shown} />

