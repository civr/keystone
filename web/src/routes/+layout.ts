import { redirect } from '@sveltejs/kit';
import { MOVED } from '$lib/nav';

// Pure SPA: keystone-core serves build/200.html for every non-API path.
export const ssr = false;
export const prerender = false;

// Bookmarks of the old (Dutch) paths land on the new pages.
export function load({ url }) {
	const to = MOVED[url.pathname.replace(/\/+$/, '') || '/'];
	if (to) redirect(308, to + url.search);
}
