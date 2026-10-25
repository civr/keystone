/**
 * Light or dark: "system" follows prefers-color-scheme (theme.css); a chosen
 * theme sets data-theme on <html>. app.html applies the stored choice before
 * the first paint.
 */
export type Theme = 'system' | 'light' | 'dark';

export const THEMES: { id: Theme; label: string }[] = [
	{ id: 'system', label: 'Automatic' },
	{ id: 'light', label: 'Light' },
	{ id: 'dark', label: 'Dark' }
];

const KEY = 'keystone.theme';

export function readTheme(): Theme {
	try {
		const t = localStorage.getItem(KEY);
		return t === 'light' || t === 'dark' ? t : 'system';
	} catch {
		return 'system';
	}
}

export function applyTheme(t: Theme): void {
	try {
		if (t === 'system') localStorage.removeItem(KEY);
		else localStorage.setItem(KEY, t);
	} catch {
		/* not remembered: still applied for this page */
	}
	if (t === 'system') delete document.documentElement.dataset.theme;
	else document.documentElement.dataset.theme = t;
}
