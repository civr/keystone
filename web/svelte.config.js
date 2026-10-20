import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
export default {
	preprocess: vitePreprocess(),
	kit: {
		// SPA: core serves 200.html for every non-API path; Caddy serves _app/.
		adapter: adapter({ fallback: '200.html' })
		// kit.csp deliberately unset: keystone-core sets the CSP and allows the
		// inline bootstrap script by its SHA-256 hash.
	}
};
