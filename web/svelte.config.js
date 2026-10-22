import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
export default {
	preprocess: vitePreprocess(),
	kit: {
		// SPA: core serves 200.html for every non-API path; Caddy serves _app/.
		adapter: adapter({ fallback: '200.html' }),
		// Deterministic build: core (embedded 200.html) and web (/srv assets)
		// are built separately and must reference the same asset names. The
		// default version name is Date.now(), which differs between builds.
		version: { name: process.env.KEYSTONE_VERSION ?? 'dev' }
		// kit.csp deliberately unset: keystone-core sets the CSP and allows the
		// inline bootstrap script by its SHA-256 hash.
	}
};
