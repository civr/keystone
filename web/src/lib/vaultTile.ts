import { api, ApiError } from './api';
import type { App } from './apps';
import { toast } from './toast.svelte';
import { showCredentials } from './credentials.svelte';

let clearSeconds: number | null = null;

async function clipboardSeconds(): Promise<number> {
	if (clearSeconds === null) {
		try {
			clearSeconds = (await api.get<{ vault: { clipboard_clear_seconds: number } }>('/admin/settings')).vault.clipboard_clear_seconds;
		} catch {
			return 30;
		}
	}
	return clearSeconds;
}

/**
 * One click on a vault tile: a sheet with the username and the password
 * (never shown, only copied) and a button to open the app. The password is
 * loaded first (after a confirmation when the last one is too old), so the
 * copy button works inside its own click, on http as well as https.
 */
export async function openVaultApp(app: App, href: string): Promise<void> {
	try {
		const [entry, secret] = await Promise.all([
			api.get<{ username: string }>(`/apps/${app.id}/vault`),
			api.send<{ password: string }>('POST', `/apps/${app.id}/vault/password`)
		]);
		showCredentials({ title: app.name, username: entry.username, password: secret.password, href, clearAfter: await clipboardSeconds() });
	} catch (err) {
		if (err instanceof ApiError && err.code === 'no_password') toast(`No password is stored for ${app.name}.`, 'error');
		else if (err instanceof ApiError) toast(err.message, 'error');
		else if (!(err instanceof Error && err.message === 'cancelled')) toast('The password could not be loaded.', 'error');
	}
}
