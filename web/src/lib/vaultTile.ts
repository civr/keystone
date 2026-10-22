import { api, ApiError } from './api';
import type { App } from './apps';
import { toast } from './toast.svelte';
import { copyThenClear } from './vaultClipboard';

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
 * One click on a vault tile (spec §5): the password goes to the clipboard
 * (after a passkey confirmation when the last one is too old) and is wiped
 * later; then the app opens in a new tab. When the browser blocks that tab
 * because the passkey prompt used up the click, a toast offers the link.
 */
export async function openVaultApp(app: App, href: string): Promise<void> {
	const password = api.send<{ password: string }>('POST', `/apps/${app.id}/vault/password`).then((r) => r.password);
	try {
		await copyThenClear(password, await clipboardSeconds());
	} catch (e) {
		const err = await password.then(() => e).catch((pe) => pe);
		if (err instanceof ApiError && err.code === 'no_password') toast(`Voor ${app.name} is geen wachtwoord opgeslagen.`, 'error');
		else if (err instanceof ApiError) toast(err.message, 'error');
		else toast('Kopiëren naar het klembord is niet gelukt.', 'error');
		return;
	}
	const w = window.open(href, '_blank');
	if (w) {
		w.opener = null;
		toast(`Wachtwoord gekopieerd — wordt over ${await clipboardSeconds()} s gewist`);
	} else {
		toast(`Wachtwoord gekopieerd. Open ${app.name} via de tegel.`);
	}
}
