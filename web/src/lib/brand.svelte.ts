/**
 * The huisstijl (Instellingen › Huisstijl). keystone-core writes it into the
 * page itself (internal/httpapi/branding.go): the name in <meta name="ks-brand">,
 * the logo as the favicon, and the accent and background as CSS tokens. This
 * module reads it from there and, after a save, writes it back the same way.
 */
export interface Branding {
	name: string;
	/** data:image/… URL; '' is Keystone's own mark. */
	logo: string;
	accent: string;
	background: string;
	/** Sign-in background photo (data:image/… URL); '' is the accent gradient. */
	login_image: string;
}

export const DEFAULT_BRAND: Branding = {
	name: 'Keystone',
	logo: '',
	accent: '#007aff',
	background: '#f5f5f7',
	login_image: ''
};
export const DEFAULT_LOGO = '/favicon.svg';

function fromPage(): { name: string; logo: string; loginImage: string } {
	if (typeof document === 'undefined') return { name: DEFAULT_BRAND.name, logo: '', loginImage: '' };
	const name = document.querySelector<HTMLMetaElement>('meta[name="ks-brand"]')?.content.trim();
	const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]')?.getAttribute('href') ?? '';
	return {
		name: name || DEFAULT_BRAND.name,
		logo: icon.startsWith('data:image/') ? icon : '',
		loginImage: document.querySelector<HTMLMetaElement>('meta[name="ks-login-image"]')?.content ?? ''
	};
}

/** The name and logo every page shows. */
export const brand = $state(fromPage());

/** The logo's image source: the uploaded logo or Keystone's own mark. */
export function logoSrc(logo: string = brand.logo): string {
	return logo || DEFAULT_LOGO;
}

/** CSS background for the sign-in and setup pages; '' is the default gradient. */
export function loginBackground(): string {
	return brand.loginImage ? `url("${brand.loginImage}") center / cover no-repeat` : '';
}

/** Page title: "Inloggen · Huis" or just the name. */
export function pageTitle(...parts: string[]): string {
	return [...parts, brand.name].join(' · ');
}

/** Applies a saved huisstijl to this page without a reload. */
export function applyBrand(b: Branding): void {
	brand.name = b.name;
	brand.logo = b.logo;
	brand.loginImage = b.login_image;
	const head = document.head;
	let meta = head.querySelector<HTMLMetaElement>('meta[name="ks-brand"]');
	if (!meta) {
		meta = Object.assign(document.createElement('meta'), { name: 'ks-brand' });
		head.append(meta);
	}
	meta.content = b.name;
	let style = head.querySelector<HTMLStyleElement>('style#ks-brand');
	if (!style) {
		style = Object.assign(document.createElement('style'), { id: 'ks-brand' });
		head.append(style);
	}
	style.textContent = `:root{--primary:${b.accent};--color-bg:${b.background}}`;
	const icon = head.querySelector<HTMLLinkElement>('link[rel="icon"]');
	if (icon) {
		icon.href = logoSrc(b.logo);
		icon.removeAttribute('type');
	}
	head.querySelector<HTMLMetaElement>('meta[name="theme-color"]')?.setAttribute('content', b.background);
}

/** WCAG relative luminance of #rrggbb (mirrors internal/branding). */
export function luminance(hex: string): number {
	const c = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
	const [r, g, b] = c.map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
	return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** The same checks the server makes, for an immediate message in the form. */
export function brandError(b: Branding): Partial<Record<keyof Branding, string>> {
	const errors: Partial<Record<keyof Branding, string>> = {};
	const name = b.name.trim();
	if (!name) errors.name = 'Enter a name.';
	else if (Array.from(name).length > 40) errors.name = 'The name may be at most 40 characters.';
	const hex = /^#[0-9a-f]{6}$/i;
	if (!hex.test(b.accent)) errors.accent = 'Choose a colour.';
	else if (1.05 / (luminance(b.accent) + 0.05) < 3)
		errors.accent = 'This colour is too light for white button text. Choose a darker one.';
	if (!hex.test(b.background)) errors.background = 'Choose a colour.';
	else if (luminance(b.background) < 0.75) errors.background = 'Choose a light background; text on it is dark.';
	return errors;
}
