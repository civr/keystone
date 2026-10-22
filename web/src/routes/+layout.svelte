<script lang="ts">
	import '$lib/theme.css';
	import Toast from '$lib/components/Toast.svelte';
	import { enableStepUp } from '$lib/stepup';

	enableStepUp();

	let { children } = $props();
</script>

{@render children()}
<Toast />
