<script lang="ts">
	import '$lib/theme.css';
	import Toast from '$lib/components/Toast.svelte';
	import StepUpPassword from '$lib/components/StepUpPassword.svelte';
	import CredentialSheet from '$lib/components/CredentialSheet.svelte';
	import { enableStepUp } from '$lib/stepup';

	enableStepUp();

	let { children } = $props();
</script>

{@render children()}
<Toast />
<StepUpPassword />
<CredentialSheet />
