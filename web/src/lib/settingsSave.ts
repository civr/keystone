import { api, ApiError } from './api';
import { weakenedPaths, type Settings } from './settingsMeta';

export type SaveResult =
	| { status: 'saved'; settings: Settings }
	| { status: 'cancelled' }
	| { status: 'invalid'; field: string; message: string };

export interface SaveQuestions {
	/** "Dit verlaagt de beveiliging" — resolve true to save anyway. */
	weakened(paths: string[]): Promise<boolean>;
	/** The new public address breaks existing passkeys — resolve true to go ahead. */
	rpChange(): Promise<boolean>;
}

/**
 * Saves the full settings object. Asks first when a change lowers security,
 * and again when the server warns that passkeys will stop working
 * (409 confirm_required → retry with ?confirm=1). A 400 invalid answer comes
 * back as the field to mark; other errors are thrown. Step-up is handled by api.ts.
 */
export async function saveSettings(saved: Settings, draft: Settings, ask: SaveQuestions): Promise<SaveResult> {
	const weakened = weakenedPaths(saved, draft);
	if (weakened.length && !(await ask.weakened(weakened))) return { status: 'cancelled' };
	try {
		return { status: 'saved', settings: await put(draft, false) };
	} catch (e) {
		if (!(e instanceof ApiError)) throw e;
		if (e.code === 'invalid') return invalid(e);
		if (e.code !== 'confirm_required') throw e;
		if (!(await ask.rpChange())) return { status: 'cancelled' };
		try {
			return { status: 'saved', settings: await put(draft, true) };
		} catch (e2) {
			if (e2 instanceof ApiError && e2.code === 'invalid') return invalid(e2);
			throw e2;
		}
	}
}

const put = (s: Settings, confirm: boolean) =>
	api.send<Settings>('PUT', '/admin/settings' + (confirm ? '?confirm=1' : ''), s);

const invalid = (e: ApiError): SaveResult => ({
	status: 'invalid',
	field: typeof e.data.field === 'string' ? e.data.field : '',
	message: e.message
});
